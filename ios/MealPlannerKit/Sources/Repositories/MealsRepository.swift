import API
import Foundation
import Persistence

public typealias MealScope = Persistence.MealScope

public struct MealsRepository: Sendable {
    private let client: Client
    private let cache: MealCache
    private static let pageSize = 100

    public init(client: Client, cache: MealCache) {
        self.client = client
        self.cache = cache
    }

    // MARK: Reads

    public func cachedMeals(_ scope: MealScope) async -> [Components.Schemas.MealSummary] {
        await cache.summaries(scope: scope)
    }

    public func cachedMeal(id: String) async -> Components.Schemas.Meal? {
        await cache.meal(id: id)
    }

    /// Walks every page and replaces the scope's cached summaries in one transaction. A failure on any
    /// page leaves the cache untouched. `404 partner_not_linked` clears the partner scope.
    public func refreshMeals(_ scope: MealScope) async throws {
        var all: [Components.Schemas.MealSummary] = []
        var cursor: String?
        repeat {
            let page = try await fetchPage(scope, cursor: cursor)
            all += page.items
            cursor = page.nextCursor
        } while cursor != nil
        await cache.replaceSummaries(all, scope: scope)
    }

    @discardableResult
    public func refreshMeal(id: String) async throws -> Components.Schemas.Meal {
        let response = try await unwrapping { try await client.getMeal(.init(path: .init(id: id))) }
        switch response {
        case .ok(let ok):
            let meal = try ok.body.json
            await cache.store(meal)
            return meal
        case .badRequest(let r): throw MealsError.validation(r.problem)
        case .unauthorized: throw MealsError.unauthorized
        case .notFound:
            await cache.remove(id: id)
            throw MealsError.notFound
        case .conflict(let r): throw MealsError.conflict(r.problem)
        case .tooManyRequests: throw MealsError.rateLimited
        case .internalServerError: throw MealsError.server("The server had a problem loading the meal.")
        case .undocumented(let status, _): throw MealsError.unexpected(status)
        }
    }

    // MARK: Writes (each answer is the full meal and replaces the cache entry)

    public func create(name: String, servings: Double) async throws -> Components.Schemas.Meal {
        let response = try await unwrapping {
            try await client.createMeal(.init(body: .json(.init(name: name, servings: servings))))
        }
        switch response {
        case .created(let created):
            let meal = try created.body.json
            await cache.store(meal)
            return meal
        case .badRequest(let r): throw MealsError.validation(r.problem)
        case .unauthorized: throw MealsError.unauthorized
        case .tooManyRequests: throw MealsError.rateLimited
        case .internalServerError: throw MealsError.server("The server had a problem creating the meal.")
        case .undocumented(let status, _): throw MealsError.unexpected(status)
        }
    }

    public func update(id: String, _ patch: Components.Schemas.UpdateMealRequest) async throws -> Components.Schemas.Meal {
        let response = try await unwrapping {
            try await client.updateMeal(.init(path: .init(id: id), body: .json(patch)))
        }
        switch response {
        case .ok(let ok):
            let meal = try ok.body.json
            await cache.store(meal)
            return meal
        case .badRequest(let r): throw MealsError.validation(r.problem)
        case .unauthorized: throw MealsError.unauthorized
        case .notFound:
            await cache.remove(id: id)
            throw MealsError.notFound
        case .conflict(let r): throw MealsError.conflict(r.problem)
        case .tooManyRequests: throw MealsError.rateLimited
        case .internalServerError: throw MealsError.server("The server had a problem saving the meal.")
        case .undocumented(let status, _): throw MealsError.unexpected(status)
        }
    }

    public func replaceIngredients(id: String, _ items: [Components.Schemas.MealIngredientInput]) async throws -> Components.Schemas.Meal {
        let response = try await unwrapping {
            try await client.replaceMealIngredients(.init(path: .init(id: id), body: .json(.init(items: items))))
        }
        switch response {
        case .ok(let ok):
            let meal = try ok.body.json
            await cache.store(meal)
            return meal
        case .badRequest(let r): throw MealsError.validation(r.problem)
        case .unauthorized: throw MealsError.unauthorized
        case .notFound:
            await cache.remove(id: id)
            throw MealsError.notFound
        case .conflict(let r): throw MealsError.conflict(r.problem)
        case .tooManyRequests: throw MealsError.rateLimited
        case .internalServerError: throw MealsError.server("The server had a problem saving the ingredients.")
        case .undocumented(let status, _): throw MealsError.unexpected(status)
        }
    }

    public func copy(id: String) async throws -> Components.Schemas.Meal {
        let response = try await unwrapping { try await client.copyMeal(.init(path: .init(id: id))) }
        switch response {
        case .created(let created):
            let meal = try created.body.json
            await cache.store(meal)
            return meal
        case .badRequest(let r): throw MealsError.validation(r.problem)
        case .unauthorized: throw MealsError.unauthorized
        case .notFound: throw MealsError.notFound
        case .conflict(let r): throw MealsError.conflict(r.problem)
        case .tooManyRequests: throw MealsError.rateLimited
        case .internalServerError: throw MealsError.server("The server had a problem copying the meal.")
        case .undocumented(let status, _): throw MealsError.unexpected(status)
        }
    }

    public func delete(id: String) async throws {
        let response = try await unwrapping { try await client.deleteMeal(.init(path: .init(id: id))) }
        switch response {
        case .noContent:
            await cache.remove(id: id)
        case .unauthorized: throw MealsError.unauthorized
        case .notFound:
            await cache.remove(id: id)
            throw MealsError.notFound
        case .conflict(let r): throw MealsError.conflict(r.problem)
        case .tooManyRequests: throw MealsError.rateLimited
        case .internalServerError: throw MealsError.server("The server had a problem deleting the meal.")
        case .undocumented(let status, _): throw MealsError.unexpected(status)
        }
    }

    // MARK: Cache control

    public func clearPartnerMeals() async { await cache.clear(scope: .partner) }

    /// Called whenever the session ends, so a second user on this device never sees the first user's meals.
    public func clearCaches() async { await cache.clearAll() }

    // MARK: Pages

    private func fetchPage(_ scope: MealScope, cursor: String?) async throws -> Components.Schemas.MealList {
        switch scope {
        case .mine:
            let response = try await unwrapping {
                try await client.listMeals(.init(query: .init(cursor: cursor, limit: Self.pageSize)))
            }
            switch response {
            case .ok(let ok): return try ok.body.json
            case .badRequest(let r): throw MealsError.validation(r.problem)
            case .unauthorized: throw MealsError.unauthorized
            case .tooManyRequests: throw MealsError.rateLimited
            case .internalServerError: throw MealsError.server("The server had a problem loading your meals.")
            case .undocumented(let status, _): throw MealsError.unexpected(status)
            }
        case .partner:
            let response = try await unwrapping {
                try await client.listPartnerMeals(.init(query: .init(cursor: cursor, limit: Self.pageSize)))
            }
            switch response {
            case .ok(let ok): return try ok.body.json
            case .badRequest(let r): throw MealsError.validation(r.problem)
            case .unauthorized: throw MealsError.unauthorized
            case .notFound:
                await cache.clear(scope: .partner)
                throw MealsError.partnerNotLinked
            case .tooManyRequests: throw MealsError.rateLimited
            case .internalServerError: throw MealsError.server("The server had a problem loading your partner's meals.")
            case .undocumented(let status, _): throw MealsError.unexpected(status)
            }
        }
    }
}
