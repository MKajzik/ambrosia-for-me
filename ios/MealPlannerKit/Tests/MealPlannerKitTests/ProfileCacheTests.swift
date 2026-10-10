import API
import Persistence
import SwiftData
import Testing

@Suite
struct ProfileCacheTests {
    private func make() throws -> (ProfileCache, ModelContainer) {
        let container = try CacheStore.inMemoryContainer()
        return (CacheStore.makeProfileCache(container), container)
    }

    @Test("Nothing cached is nil, and storing a user replaces the previous one")
    func user() async throws {
        let (cache, _) = try make()
        #expect(await cache.user() == nil)
        await cache.store(user: ProfileFixtures.user(email: "a@example.com", kcal: 1800))
        await cache.store(user: ProfileFixtures.user(email: "a@example.com", kcal: 2000))
        let user = try #require(await cache.user())
        #expect(user.targetKcal == 2000)
        #expect(user.email == "a@example.com")
    }

    @Test("The partnership is unknown until fetched, then none, active or pending")
    func partnership() async throws {
        let (cache, _) = try make()
        #expect(await cache.partnership() == .unknown)
        await cache.store(partnership: nil)
        #expect(await cache.partnership() == .none)
        await cache.store(partnership: ProfileFixtures.active("Alex"))
        #expect(await cache.partnership() == .present(ProfileFixtures.active("Alex")))
        await cache.store(partnership: ProfileFixtures.pending())
        #expect(await cache.partnership() == .present(ProfileFixtures.pending()))
        await cache.store(partnership: nil)
        #expect(await cache.partnership() == .none)
    }

    @Test("A new cache actor on the same store sees what the first one saved")
    func survivesRelaunch() async throws {
        let (cache, container) = try make()
        await cache.store(user: ProfileFixtures.user(kcal: 2000))
        await cache.store(partnership: ProfileFixtures.active())
        let relaunched = CacheStore.makeProfileCache(container)
        #expect(await relaunched.user()?.targetKcal == 2000)
        #expect(await relaunched.partnership() == .present(ProfileFixtures.active()))
    }

    @Test("clearAll forgets the user and the partnership, so a second user never sees them")
    func clearAll() async throws {
        let (cache, _) = try make()
        await cache.store(user: ProfileFixtures.user())
        await cache.store(partnership: ProfileFixtures.active())
        await cache.clearAll()
        #expect(await cache.user() == nil)
        #expect(await cache.partnership() == .unknown)
    }
}
