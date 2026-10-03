import API
import Foundation
import Persistence
import Testing
@testable import Repositories

@Suite
struct MealsRepositoryTests {
    private func make(_ route: @escaping RoutingTransport.Route) throws -> (MealsRepository, MealCache, RoutingTransport) {
        let transport = RoutingTransport(route)
        let cache = CacheStore.makeMealCache(try CacheStore.inMemoryContainer())
        return (MealsRepository(client: makeAuthlessClient(transport: transport), cache: cache), cache, transport)
    }

    @Test("Refresh walks every page at limit 100 and replaces the scope in one go")
    func walksPages() async throws {
        let (repo, cache, transport) = try make { call in
            switch (call.route, call.path.contains("cursor=c2")) {
            case ("GET /meals", false):
                return (200, Fixtures.mealList([Fixtures.summary(id: "a", name: "A"), Fixtures.summary(id: "b", name: "B")], next: "c2"))
            case ("GET /meals", true):
                return (200, Fixtures.mealList([Fixtures.summary(id: "c", name: "C")]))
            default:
                return (500, Fixtures.problem(500, code: "internal"))
            }
        }
        try await repo.refreshMeals(.mine)
        #expect(await cache.summaries(scope: .mine).map(\.id) == ["a", "b", "c"])
        let calls = await transport.calls("GET /meals")
        #expect(calls.count == 2)
        #expect(calls.allSatisfy { $0.path.contains("limit=100") })
    }

    @Test("A failing second page leaves the cache untouched")
    func failedPageKeepsCache() async throws {
        let (repo, cache, _) = try make { call in
            call.path.contains("cursor=c2")
                ? (500, Fixtures.problem(500, code: "internal"))
                : (200, Fixtures.mealList([Fixtures.summary(id: "new", name: "New")], next: "c2"))
        }
        await cache.replaceSummaries([Fixtures.summary(id: "old", name: "Old")], scope: .mine)
        await #expect(throws: MealsError.server("The server had a problem loading your meals.")) {
            try await repo.refreshMeals(.mine)
        }
        #expect(await cache.summaries(scope: .mine).map(\.id) == ["old"])
    }

    @Test("Partner scope reads /partner/meals; 404 clears only that scope and says the partner is not linked")
    func partnerScope() async throws {
        let (repo, cache, transport) = try make { call in
            call.route == "GET /partner/meals"
                ? (404, Fixtures.problem(404, code: "partner_not_linked"))
                : (500, Fixtures.problem(500, code: "internal"))
        }
        await cache.replaceSummaries([Fixtures.summary(id: "p")], scope: .partner)
        await cache.replaceSummaries([Fixtures.summary(id: "m")], scope: .mine)
        await #expect(throws: MealsError.partnerNotLinked) { try await repo.refreshMeals(.partner) }
        #expect(await cache.summaries(scope: .partner).isEmpty)
        #expect(await cache.summaries(scope: .mine).map(\.id) == ["m"])
        #expect(await transport.calls("GET /partner/meals").count == 1)
    }

    @Test("refreshMeal caches the full meal; a 404 removes the entry and throws notFound")
    func refreshMeal() async throws {
        let live = Locked(true)
        let (repo, cache, _) = try make { _ in
            live.value
                ? (200, Fixtures.json(Fixtures.meal(lines: [Fixtures.line()])))
                : (404, Fixtures.problem(404, code: "not_found"))
        }
        let meal = try await repo.refreshMeal(id: "meal-1")
        #expect(meal.ingredients.count == 1)
        #expect(await repo.cachedMeal(id: "meal-1")?.name == "Pasta")
        live.set(false)
        await #expect(throws: MealsError.notFound) { try await repo.refreshMeal(id: "meal-1") }
        #expect(await cache.meal(id: "meal-1") == nil)
    }

    @Test("update sends only the patched fields and caches the answer")
    func update() async throws {
        let (repo, cache, transport) = try make { _ in (200, Fixtures.json(Fixtures.meal(name: "New"))) }
        let meal = try await repo.update(id: "meal-1", .init(name: "New"))
        #expect(meal.name == "New")
        let call = try #require(await transport.calls("PATCH /meals/meal-1").first)
        #expect(call.body.contains("\"name\":\"New\""))
        #expect(!call.body.contains("servings"))
        #expect(await cache.meal(id: "meal-1")?.name == "New")
    }

    @Test("replaceIngredients PUTs the whole list, an empty one included, and caches the answer")
    func replaceIngredients() async throws {
        let (repo, cache, transport) = try make { _ in (200, Fixtures.json(Fixtures.meal(lines: []))) }
        _ = try await repo.replaceIngredients(id: "meal-1", [])
        let call = try #require(await transport.calls("PUT /meals/meal-1/ingredients").first)
        #expect(call.body.contains("\"items\":[]"))
        #expect(await cache.meal(id: "meal-1") != nil)
    }

    @Test("create and copy return the full meal and cache it; a 400 becomes a readable validation message")
    func createAndCopy() async throws {
        let failing = Locked(false)
        let (repo, cache, transport) = try make { call in
            if failing.value { return (400, Fixtures.problem(400, code: "validation_failed", errors: [("name", "too_long")])) }
            return (201, Fixtures.json(Fixtures.meal(id: call.route == "POST /meals" ? "made" : "copy")))
        }
        let made = try await repo.create(name: "Soup", servings: 2)
        #expect(made.id == "made")
        #expect(await transport.calls("POST /meals").first?.body.contains("\"servings\":2") == true)
        let copied = try await repo.copy(id: "theirs")
        #expect(copied.id == "copy")
        #expect(await cache.meal(id: "copy") != nil)
        failing.set(true)
        await #expect(throws: MealsError.validationFailed("Name is too long.")) { try await repo.create(name: "x", servings: 1) }
    }

    @Test("delete: 204 removes the entry; 409 meal_in_use throws inUse and keeps it; 404 removes it and throws notFound")
    func delete() async throws {
        let status = Locked(204)
        let (repo, cache, _) = try make { _ in
            switch status.value {
            case 204: return (204, "")
            case 409: return (409, Fixtures.problem(409, code: "meal_in_use"))
            default: return (404, Fixtures.problem(404, code: "not_found"))
            }
        }
        await cache.store(Fixtures.meal())
        status.set(409)
        await #expect(throws: MealsError.inUse) { try await repo.delete(id: "meal-1") }
        #expect(await cache.meal(id: "meal-1") != nil)
        status.set(204)
        try await repo.delete(id: "meal-1")
        #expect(await cache.meal(id: "meal-1") == nil)
        await cache.store(Fixtures.meal())
        status.set(404)
        await #expect(throws: MealsError.notFound) { try await repo.delete(id: "meal-1") }
        #expect(await cache.meal(id: "meal-1") == nil)
    }

    @Test("A transport failure reaches the caller as a plain URLError, not a ClientError")
    func transportFailureIsUnwrapped() async throws {
        let (repo, _, _) = try make { _ in throw URLError(.notConnectedToInternet) }
        await #expect(throws: URLError.self) { try await repo.refreshMeals(.mine) }
        await #expect(throws: URLError.self) { try await repo.refreshMeal(id: "x") }
    }

    @Test("429 and 401 map to their own errors")
    func commonStatuses() async throws {
        let status = Locked(429)
        let (repo, _, _) = try make { _ in (status.value, Fixtures.problem(status.value, code: "x")) }
        await #expect(throws: MealsError.rateLimited) { try await repo.refreshMeals(.mine) }
        status.set(401)
        await #expect(throws: MealsError.unauthorized) { try await repo.refreshMeals(.mine) }
    }
}
