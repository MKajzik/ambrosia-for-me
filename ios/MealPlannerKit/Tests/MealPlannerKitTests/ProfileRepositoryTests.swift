import API
import Foundation
import Persistence
import Testing
@testable import Repositories

@Suite
struct ProfileRepositoryTests {
    private func make(
        withSentinel: Bool = false, _ route: @escaping RoutingTransport.Route
    ) throws -> (ProfileRepository, ProfileCache, RoutingTransport) {
        let transport = RoutingTransport(route)
        let cache = CacheStore.makeProfileCache(try CacheStore.inMemoryContainer())
        let client = withSentinel
            ? makeClient(transport: transport, middlewares: [NullSentinelMiddleware()])
            : makeAuthlessClient(transport: transport)
        return (ProfileRepository(client: client, cache: cache), cache, transport)
    }

    @Test("refreshUser stores the user and cachedUser returns it")
    func refresh() async throws {
        let (repo, _, _) = try make { _ in (200, Fixtures.json(ProfileFixtures.user(kcal: 1800))) }
        #expect(await repo.cachedUser() == nil)
        let user = try await repo.refreshUser()
        #expect(user.targetKcal == 1800)
        #expect(await repo.cachedUser()?.targetKcal == 1800)
    }

    @Test("A failed refresh leaves the cache untouched and says what went wrong")
    func failedRefresh() async throws {
        let mode = Locked(500)
        let (repo, cache, _) = try make { _ in
            mode.value == 200 ? (200, Fixtures.json(ProfileFixtures.user(kcal: 1800))) : (mode.value, Fixtures.problem(mode.value, code: "x"))
        }
        mode.set(200)
        try await repo.refreshUser()
        mode.set(500)
        await #expect(throws: ProfileError.server("The server had a problem loading your profile.")) { try await repo.refreshUser() }
        mode.set(401)
        await #expect(throws: ProfileError.unauthorized) { try await repo.refreshUser() }
        mode.set(429)
        await #expect(throws: ProfileError.rateLimited) { try await repo.refreshUser() }
        #expect(await cache.user()?.targetKcal == 1800)
    }

    @Test("updateTargets sends all four fields; a cleared one is the sentinel when no middleware runs")
    func updateSendsAllFour() async throws {
        let (repo, _, transport) = try make { _ in (200, Fixtures.json(ProfileFixtures.user(kcal: 2000, protein: 70))) }
        _ = try await repo.updateTargets(TargetsUpdate(kcal: 2000, protein: 70, carbs: nil, fat: nil))
        let body = try #require(await transport.calls("PATCH /me").first).body
        #expect(body.contains("\"target_kcal\":2000"))
        #expect(body.contains("\"target_protein_g\":70"))
        #expect(body.contains("\"target_carbs_g\":-1"))
        #expect(body.contains("\"target_fat_g\":-1"))
    }

    @Test("Review focus 1: through the middleware a cleared target reaches the wire as null, and the answer is stored")
    func clearReachesTheWireAsNull() async throws {
        let (repo, cache, transport) = try make(withSentinel: true) { _ in (200, Fixtures.json(ProfileFixtures.user(kcal: nil, protein: 70))) }
        let saved = try await repo.updateTargets(TargetsUpdate(kcal: nil, protein: 70, carbs: nil, fat: nil))
        let body = try #require(await transport.calls("PATCH /me").first).body
        #expect(body.contains("\"target_kcal\":null"))
        #expect(body.contains("\"target_carbs_g\":null"))
        #expect(body.contains("\"target_protein_g\":70"))
        #expect(!body.contains("-1"))
        #expect(saved.targetKcal == nil)
        #expect(await cache.user()?.targetProteinG == 70)
    }

    @Test("A 400 becomes field messages keyed by the server's path, and the cache is untouched")
    func updateValidation() async throws {
        let mode = Locked(200)
        let (repo, cache, _) = try make { _ in
            mode.value == 200
                ? (200, Fixtures.json(ProfileFixtures.user(kcal: 1800)))
                : (400, Fixtures.problem(400, code: "validation_failed", errors: [("target_kcal", "invalid_value")]))
        }
        try await repo.refreshUser()
        mode.set(400)
        await #expect(throws: ProfileError.validationFailed(fields: ["target_kcal": "Target kcal is invalid."], message: "Target kcal is invalid.")) {
            try await repo.updateTargets(TargetsUpdate(kcal: 1, protein: nil, carbs: nil, fat: nil))
        }
        #expect(await cache.user()?.targetKcal == 1800)
    }

    @Test("deleteAccount succeeds on 204 and reports other answers; it never touches the cache itself")
    func delete() async throws {
        let mode = Locked(204)
        let (repo, cache, transport) = try make { _ in (mode.value, mode.value == 204 ? "" : Fixtures.problem(mode.value, code: "x")) }
        await cache.store(user: ProfileFixtures.user())
        try await repo.deleteAccount()
        #expect(await transport.calls("DELETE /me").count == 1)
        mode.set(500)
        await #expect(throws: ProfileError.server("The server had a problem deleting your account.")) { try await repo.deleteAccount() }
        mode.set(401)
        await #expect(throws: ProfileError.unauthorized) { try await repo.deleteAccount() }
        #expect(await cache.user() != nil)
    }

    @Test("clearCaches empties the profile cache")
    func clear() async throws {
        let (repo, cache, _) = try make { _ in (500, "") }
        await cache.store(user: ProfileFixtures.user())
        await repo.clearCaches()
        #expect(await cache.user() == nil)
    }

    @Test("PlanRepository.storeTargets feeds the plan cache from a saved user, leaving the cached days")
    func storeTargets() async throws {
        let planCache = CacheStore.makePlanCache(try CacheStore.inMemoryContainer())
        let plan = PlanRepository(client: makeAuthlessClient(transport: RoutingTransport { _ in (500, "") }), cache: planCache)
        await planCache.replace(days: [Fixtures.day("2026-10-05")], targets: Fixtures.targets(kcal: 1800))
        await plan.storeTargets(from: ProfileFixtures.user(kcal: 2000, protein: nil, carbs: 250, fat: 70))
        let snapshot = await plan.cached(from: "2026-10-05", to: "2026-10-05")
        #expect(snapshot.targets?.targetKcal == 2000)
        #expect(snapshot.targets?.targetProteinG == nil)
        #expect(snapshot.days.count == 1)
    }
}
