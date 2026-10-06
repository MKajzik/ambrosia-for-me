import API
import Foundation
import Persistence
import Testing
@testable import Repositories

@Suite
struct PlanRepositoryTests {
    private func make(_ route: @escaping RoutingTransport.Route) throws -> (PlanRepository, PlanCache, RoutingTransport) {
        let transport = RoutingTransport(route)
        let cache = CacheStore.makePlanCache(try CacheStore.inMemoryContainer())
        return (PlanRepository(client: makeAuthlessClient(transport: transport), cache: cache), cache, transport)
    }

    @Test("refresh asks for the range, stores exactly those days and the targets")
    func refreshStoresRange() async throws {
        let (repo, cache, transport) = try make { _ in
            (200, Fixtures.planRange(from: "2026-10-05", to: "2026-10-06", days: [Fixtures.day("2026-10-05"), Fixtures.day("2026-10-06")], targets: Fixtures.targets(kcal: 1800)))
        }
        await cache.replace(days: [Fixtures.day("2026-10-20", calories: 7)], targets: Fixtures.targets())
        try await repo.refresh(from: "2026-10-05", to: "2026-10-06")
        let snapshot = await repo.cached(from: "2026-10-05", to: "2026-10-31")
        #expect(snapshot.days.map(\.date) == ["2026-10-05", "2026-10-06", "2026-10-20"])
        #expect(snapshot.targets == Fixtures.targets(kcal: 1800))
        let path = try #require(await transport.calls("GET /plan").first?.path)
        #expect(path.contains("from=2026-10-05"))
        #expect(path.contains("to=2026-10-06"))
    }

    @Test("A failed refresh leaves the cache untouched")
    func failedRefreshKeepsCache() async throws {
        let (repo, cache, _) = try make { _ in (500, Fixtures.problem(500, code: "internal")) }
        await cache.replace(days: [Fixtures.day("2026-10-05", calories: 7)], targets: Fixtures.targets())
        await #expect(throws: PlanError.server("The server had a problem loading the plan.")) {
            try await repo.refresh(from: "2026-10-05", to: "2026-10-05")
        }
        #expect(await repo.cached(from: "2026-10-05", to: "2026-10-05").days.first?.nutritionPerDay.calories == 7)
    }

    @Test("setEntry PUTs the meal and portion to the date and slot; a snack goes to the snack slot")
    func setEntry() async throws {
        let (repo, cache, transport) = try make { _ in (200, Fixtures.json(Fixtures.entry())) }
        try await repo.setEntry(date: "2026-10-05", slot: .breakfast, mealID: "m1", portion: 1.5)
        try await repo.setEntry(date: "2026-10-05", slot: .snack, mealID: "m2", portion: 1)
        let breakfast = try #require(await transport.calls("PUT /plan/2026-10-05/breakfast").first)
        #expect(breakfast.body.contains("\"meal_id\":\"m1\""))
        #expect(breakfast.body.contains("\"portion\":1.5"))
        #expect(await transport.calls("PUT /plan/2026-10-05/snack").count == 1)
        // Writes touch no cache.
        #expect(await cache.days(from: "2000-01-01", to: "2100-01-01").isEmpty)
    }

    @Test("A 400 on setEntry becomes a readable validation message")
    func setEntryValidation() async throws {
        let (repo, _, _) = try make { _ in (400, Fixtures.problem(400, code: "validation_failed", errors: [("portion", "invalid_value")])) }
        await #expect(throws: PlanError.validationFailed("Portion is invalid.")) {
            try await repo.setEntry(date: "2026-10-05", slot: .lunch, mealID: "m1", portion: 0)
        }
    }

    @Test("clearSlot DELETEs; clearing an already-empty slot (404) is a success")
    func clearSlot() async throws {
        let status = Locked(204)
        let (repo, _, transport) = try make { _ in
            status.value == 204 ? (204, "") : (status.value, Fixtures.problem(status.value, code: "not_found"))
        }
        try await repo.clearSlot(date: "2026-10-05", slot: .dinner)
        status.set(404)
        try await repo.clearSlot(date: "2026-10-05", slot: .snack)
        #expect(await transport.calls("DELETE /plan/2026-10-05/dinner").count == 1)
        #expect(await transport.calls("DELETE /plan/2026-10-05/snack").count == 1)
        status.set(500)
        await #expect(throws: PlanError.server("The server had a problem clearing the slot.")) {
            try await repo.clearSlot(date: "2026-10-05", slot: .dinner)
        }
    }

    @Test("apply POSTs the start date and overwrite flag; 409 plan_conflict is a conflict, 404 is not found")
    func apply() async throws {
        let status = Locked(204)
        let (repo, _, transport) = try make { _ in
            switch status.value {
            case 204: return (204, "")
            case 409: return (409, Fixtures.problem(409, code: "plan_conflict"))
            default: return (404, Fixtures.problem(404, code: "not_found"))
            }
        }
        try await repo.apply(templateID: "t1", startDate: "2026-10-05", overwrite: true)
        let call = try #require(await transport.calls("POST /diet-templates/t1/apply").first)
        #expect(call.body.contains("\"start_date\":\"2026-10-05\""))
        #expect(call.body.contains("\"overwrite\":true"))
        status.set(409)
        await #expect(throws: PlanError.conflict) { try await repo.apply(templateID: "t1", startDate: "2026-10-05", overwrite: false) }
        status.set(404)
        await #expect(throws: PlanError.notFound) { try await repo.apply(templateID: "t1", startDate: "2026-10-05", overwrite: false) }
    }

    @Test("A 409 that is not plan_conflict is a plain server error")
    func otherConflict() async throws {
        let (repo, _, _) = try make { _ in (409, Fixtures.problem(409, code: "something_else", title: "Conflict")) }
        await #expect(throws: PlanError.server("Conflict")) { try await repo.apply(templateID: "t1", startDate: "2026-10-05", overwrite: false) }
    }

    @Test("A transport failure reaches the caller as a plain URLError")
    func transportFailureIsUnwrapped() async throws {
        let (repo, _, _) = try make { _ in throw URLError(.notConnectedToInternet) }
        await #expect(throws: URLError.self) { try await repo.refresh(from: "2026-10-05", to: "2026-10-05") }
        await #expect(throws: URLError.self) { try await repo.setEntry(date: "2026-10-05", slot: .lunch, mealID: "m1", portion: 1) }
    }

    @Test("429 and 401 map to their own errors")
    func commonStatuses() async throws {
        let status = Locked(429)
        let (repo, _, _) = try make { _ in (status.value, Fixtures.problem(status.value, code: "x")) }
        await #expect(throws: PlanError.rateLimited) { try await repo.refresh(from: "2026-10-05", to: "2026-10-05") }
        status.set(401)
        await #expect(throws: PlanError.unauthorized) { try await repo.refresh(from: "2026-10-05", to: "2026-10-05") }
    }

    @Test("clearCaches empties the plan cache")
    func clearCaches() async throws {
        let (repo, cache, _) = try make { _ in (500, "") }
        await cache.replace(days: [Fixtures.day("2026-10-05")], targets: Fixtures.targets())
        await repo.clearCaches()
        #expect(await repo.cached(from: "2000-01-01", to: "2100-01-01").days.isEmpty)
        #expect(await repo.cached(from: "2000-01-01", to: "2100-01-01").targets == nil)
    }
}
