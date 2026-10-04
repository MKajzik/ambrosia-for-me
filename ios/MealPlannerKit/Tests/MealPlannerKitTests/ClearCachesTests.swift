import API
import Persistence
import Repositories
import Testing
@testable import AppCore

@Suite
struct ClearCachesTests {
    @Test("Ending a session empties the meal, plan and template caches, so a second user never sees the first user's data")
    func clearsEverything() async throws {
        let container = try CacheStore.inMemoryContainer()
        let mealCache = CacheStore.makeMealCache(container)
        let planCache = CacheStore.makePlanCache(container)
        let templateCache = CacheStore.makeTemplateCache(container)
        let client = makeAuthlessClient(transport: RoutingTransport { _ in (500, "") })
        await mealCache.replaceSummaries([Fixtures.summary()], scope: .mine)
        await planCache.replace(days: [Fixtures.day("2026-10-05")], targets: Fixtures.targets())
        await templateCache.replaceSummaries([Fixtures.templateSummary()], scope: .mine)

        await clearAllCaches(
            meals: MealsRepository(client: client, cache: mealCache),
            plan: PlanRepository(client: client, cache: planCache),
            templates: TemplatesRepository(client: client, cache: templateCache)
        )

        #expect(await mealCache.summaries(scope: .mine).isEmpty)
        #expect(await planCache.days(from: "2000-01-01", to: "2100-01-01").isEmpty)
        #expect(await planCache.targets() == nil)
        #expect(await templateCache.summaries(scope: .mine).isEmpty)
    }
}
