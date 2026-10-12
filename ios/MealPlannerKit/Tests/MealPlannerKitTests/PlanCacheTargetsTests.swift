import API
import Persistence
import Testing

@Suite
struct PlanCacheTargetsTests {
    @Test("setTargets replaces only the targets: cached days stay")
    func replacesTargetsOnly() async throws {
        let cache = CacheStore.makePlanCache(try CacheStore.inMemoryContainer())
        await cache.replace(days: [Fixtures.day("2026-10-05", calories: 7)], targets: Fixtures.targets(kcal: 1800))
        await cache.setTargets(.init(targetKcal: 2000, targetProteinG: nil, targetCarbsG: 250, targetFatG: 70))
        let targets = try #require(await cache.targets())
        #expect(targets.targetKcal == 2000)
        #expect(targets.targetProteinG == nil)
        #expect(targets.targetCarbsG == 250)
        #expect(await cache.days(from: "2026-10-05", to: "2026-10-05").count == 1)
    }

    @Test("setTargets creates the targets row when there is none yet")
    func createsRow() async throws {
        let cache = CacheStore.makePlanCache(try CacheStore.inMemoryContainer())
        #expect(await cache.targets() == nil)
        await cache.setTargets(.init(targetKcal: 1500))
        #expect(await cache.targets()?.targetKcal == 1500)
    }
}
