import API
import Persistence
import Repositories
import Testing
@testable import AppCore

@Suite
struct ClearCachesTests {
    @Test("Ending a session empties the meal, plan, template, shopping and profile caches, so a second user never sees the first user's data")
    func clearsEverything() async throws {
        let container = try CacheStore.inMemoryContainer()
        let mealCache = CacheStore.makeMealCache(container)
        let planCache = CacheStore.makePlanCache(container)
        let templateCache = CacheStore.makeTemplateCache(container)
        let profileCache = CacheStore.makeProfileCache(container)
        let client = makeAuthlessClient(transport: RoutingTransport { _ in (500, "") })
        await mealCache.replaceSummaries([Fixtures.summary()], scope: .mine)
        await planCache.replace(days: [Fixtures.day("2026-10-05")], targets: Fixtures.targets())
        await templateCache.replaceSummaries([Fixtures.templateSummary()], scope: .mine)
        await profileCache.store(user: ProfileFixtures.user())
        await profileCache.store(partnership: ProfileFixtures.active())

        let shoppingCache = CacheStore.makeShoppingCache(container)
        await shoppingCache.store(ShoppingFixtures.list())
        await shoppingCache.enqueue(kind: .check, listID: "l1", itemID: "i1", payload: nil)
        // A refused change left the engine holding "A change couldn't be saved." for this user.
        let refusing = makeAuthlessClient(transport: RoutingTransport { _ in (400, Fixtures.problem(400, code: "validation_failed")) })
        let engine = ShoppingSyncEngine(client: refusing, cache: shoppingCache)
        await engine.drain()
        await shoppingCache.enqueue(kind: .check, listID: "l1", itemID: "i1", payload: nil)

        await clearAllCaches(
            meals: MealsRepository(client: client, cache: mealCache),
            plan: PlanRepository(client: client, cache: planCache),
            templates: TemplatesRepository(client: client, cache: templateCache),
            shopping: ShoppingListsRepository(client: client, cache: shoppingCache),
            profile: ProfileRepository(client: client, cache: profileCache),
            sync: engine
        )

        #expect(await mealCache.summaries(scope: .mine).isEmpty)
        #expect(await planCache.days(from: "2000-01-01", to: "2100-01-01").isEmpty)
        #expect(await planCache.targets() == nil)
        #expect(await templateCache.summaries(scope: .mine).isEmpty)
        #expect(await profileCache.user() == nil) // a second user never sees this user's email or targets
        #expect(await profileCache.partnership() == .unknown)
        #expect(await shoppingCache.list(id: "l1") == nil)
        #expect(await shoppingCache.intents().isEmpty) // the next user never drains this user's queue
        #expect(await engine.takeNotice() == nil) // nor sees this user's "couldn't be saved"
    }
}
