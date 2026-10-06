import API
import Foundation
import Persistence

/// Diet templates, cache-first like `MealsRepository`. Each write's answer is the full template and replaces the
/// cache entry. Applying a template is `PlanRepository.apply`: it belongs to the plan, not the library.
public struct TemplatesRepository: Sendable {
    private let client: Client
    private let cache: TemplateCache
    private static let pageSize = 100

    public init(client: Client, cache: TemplateCache) {
        self.client = client
        self.cache = cache
    }

    // MARK: Reads

    public func cachedTemplates(_ scope: MealScope) async -> [Components.Schemas.DietTemplateSummary] {
        await cache.summaries(scope: scope)
    }

    public func cachedTemplate(id: String) async -> Components.Schemas.DietTemplate? {
        await cache.template(id: id)
    }

    /// Walks every page and replaces the scope's cached summaries in one transaction. A failure on any page leaves
    /// the cache untouched. `404 partner_not_linked` clears the partner scope.
    public func refreshTemplates(_ scope: MealScope) async throws {
        var all: [Components.Schemas.DietTemplateSummary] = []
        var cursor: String?
        repeat {
            let page = try await fetchPage(scope, cursor: cursor)
            all += page.items
            cursor = page.nextCursor
        } while cursor != nil
        await cache.replaceSummaries(all, scope: scope)
    }

    @discardableResult
    public func refreshTemplate(id: String) async throws -> Components.Schemas.DietTemplate {
        let response = try await unwrapping { try await client.getDietTemplate(.init(path: .init(id: id))) }
        switch response {
        case .ok(let ok):
            let template = try ok.body.json
            await cache.store(template)
            return template
        case .unauthorized: throw TemplatesError.unauthorized
        case .notFound:
            await cache.remove(id: id)
            throw TemplatesError.notFound
        case .tooManyRequests: throw TemplatesError.rateLimited
        case .internalServerError: throw TemplatesError.server("The server had a problem loading the template.")
        case .undocumented(let status, _): throw TemplatesError.unexpected(status)
        }
    }

    // MARK: Writes

    public func create(name: String, dayCount: Int) async throws -> Components.Schemas.DietTemplate {
        let response = try await unwrapping {
            try await client.createDietTemplate(.init(body: .json(.init(name: name, dayCount: dayCount))))
        }
        switch response {
        case .created(let created):
            let template = try created.body.json
            await cache.store(template)
            return template
        case .badRequest(let r): throw TemplatesError.validation(r.problem)
        case .unauthorized: throw TemplatesError.unauthorized
        case .tooManyRequests: throw TemplatesError.rateLimited
        case .internalServerError: throw TemplatesError.server("The server had a problem creating the template.")
        case .undocumented(let status, _): throw TemplatesError.unexpected(status)
        }
    }

    public func update(id: String, _ patch: Components.Schemas.UpdateDietTemplateRequest) async throws -> Components.Schemas.DietTemplate {
        let response = try await unwrapping {
            try await client.updateDietTemplate(.init(path: .init(id: id), body: .json(patch)))
        }
        switch response {
        case .ok(let ok):
            let template = try ok.body.json
            await cache.store(template)
            return template
        case .badRequest(let r): throw TemplatesError.validation(r.problem)
        case .unauthorized: throw TemplatesError.unauthorized
        case .notFound:
            await cache.remove(id: id)
            throw TemplatesError.notFound
        case .tooManyRequests: throw TemplatesError.rateLimited
        case .internalServerError: throw TemplatesError.server("The server had a problem saving the template.")
        case .undocumented(let status, _): throw TemplatesError.unexpected(status)
        }
    }

    public func replaceSlots(id: String, _ items: [Components.Schemas.TemplateSlotInput]) async throws -> Components.Schemas.DietTemplate {
        let response = try await unwrapping {
            try await client.replaceTemplateSlots(.init(path: .init(id: id), body: .json(.init(items: items))))
        }
        switch response {
        case .ok(let ok):
            let template = try ok.body.json
            await cache.store(template)
            return template
        case .badRequest(let r): throw TemplatesError.validation(r.problem)
        case .unauthorized: throw TemplatesError.unauthorized
        case .notFound:
            await cache.remove(id: id)
            throw TemplatesError.notFound
        case .conflict(let r): throw TemplatesError.conflict(r.problem)
        case .tooManyRequests: throw TemplatesError.rateLimited
        case .internalServerError: throw TemplatesError.server("The server had a problem saving the slots.")
        case .undocumented(let status, _): throw TemplatesError.unexpected(status)
        }
    }

    public func copy(id: String) async throws -> Components.Schemas.DietTemplate {
        let response = try await unwrapping { try await client.copyDietTemplate(.init(path: .init(id: id))) }
        switch response {
        case .created(let created):
            let template = try created.body.json
            await cache.store(template)
            return template
        case .unauthorized: throw TemplatesError.unauthorized
        case .notFound: throw TemplatesError.notFound
        case .tooManyRequests: throw TemplatesError.rateLimited
        case .internalServerError: throw TemplatesError.server("The server had a problem copying the template.")
        case .undocumented(let status, _): throw TemplatesError.unexpected(status)
        }
    }

    public func delete(id: String) async throws {
        let response = try await unwrapping { try await client.deleteDietTemplate(.init(path: .init(id: id))) }
        switch response {
        case .noContent:
            await cache.remove(id: id)
        case .unauthorized: throw TemplatesError.unauthorized
        case .notFound:
            await cache.remove(id: id)
            throw TemplatesError.notFound
        case .tooManyRequests: throw TemplatesError.rateLimited
        case .internalServerError: throw TemplatesError.server("The server had a problem deleting the template.")
        case .undocumented(let status, _): throw TemplatesError.unexpected(status)
        }
    }

    // MARK: Cache control

    public func clearPartnerTemplates() async { await cache.clear(scope: .partner) }

    /// Called whenever the session ends.
    public func clearCaches() async { await cache.clearAll() }

    // MARK: Pages

    private func fetchPage(_ scope: MealScope, cursor: String?) async throws -> Components.Schemas.DietTemplateList {
        switch scope {
        case .mine:
            let response = try await unwrapping {
                try await client.listDietTemplates(.init(query: .init(cursor: cursor, limit: Self.pageSize)))
            }
            switch response {
            case .ok(let ok): return try ok.body.json
            case .badRequest(let r): throw TemplatesError.validation(r.problem)
            case .unauthorized: throw TemplatesError.unauthorized
            case .tooManyRequests: throw TemplatesError.rateLimited
            case .internalServerError: throw TemplatesError.server("The server had a problem loading your templates.")
            case .undocumented(let status, _): throw TemplatesError.unexpected(status)
            }
        case .partner:
            let response = try await unwrapping {
                try await client.listPartnerDietTemplates(.init(query: .init(cursor: cursor, limit: Self.pageSize)))
            }
            switch response {
            case .ok(let ok): return try ok.body.json
            case .badRequest(let r): throw TemplatesError.validation(r.problem)
            case .unauthorized: throw TemplatesError.unauthorized
            case .notFound:
                await cache.clear(scope: .partner)
                throw TemplatesError.partnerNotLinked
            case .tooManyRequests: throw TemplatesError.rateLimited
            case .internalServerError: throw TemplatesError.server("The server had a problem loading your partner's templates.")
            case .undocumented(let status, _): throw TemplatesError.unexpected(status)
            }
        }
    }
}
