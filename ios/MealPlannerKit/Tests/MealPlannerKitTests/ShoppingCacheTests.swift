import API
import Persistence
import SwiftData
import Testing

@Suite
struct ShoppingCacheTests {
    private func make() throws -> (ShoppingCache, ModelContainer) {
        let container = try CacheStore.inMemoryContainer()
        return (CacheStore.makeShoppingCache(container), container)
    }

    @Test("Summaries keep the server's order within a scope, and the scopes are separate")
    func summaryOrderAndScopes() async throws {
        let (cache, _) = try make()
        await cache.replaceSummaries([ShoppingFixtures.summary(id: "b", name: "B"), ShoppingFixtures.summary(id: "a", name: "A")], scope: .mine)
        await cache.replaceSummaries([ShoppingFixtures.summary(id: "p", name: "P")], scope: .partner)
        #expect(await cache.summaries(scope: .mine).map(\.id) == ["b", "a"])
        #expect(await cache.summaries(scope: .partner).map(\.id) == ["p"])
    }

    @Test("replace deletes ids absent from the first page; merge appends without deleting")
    func replaceAndMerge() async throws {
        let (cache, _) = try make()
        await cache.replaceSummaries([ShoppingFixtures.summary(id: "a"), ShoppingFixtures.summary(id: "b")], scope: .mine)
        await cache.mergeSummaries([ShoppingFixtures.summary(id: "c")], scope: .mine)
        #expect(await cache.summaries(scope: .mine).map(\.id) == ["a", "b", "c"])
        await cache.replaceSummaries([ShoppingFixtures.summary(id: "b")], scope: .mine)
        #expect(await cache.summaries(scope: .mine).map(\.id) == ["b"])
    }

    @Test("storing a list updates its summary row's name and sharing")
    func storeUpdatesSummary() async throws {
        let (cache, _) = try make()
        await cache.replaceSummaries([ShoppingFixtures.summary(id: "l1", name: "Old")], scope: .mine)
        await cache.store(ShoppingFixtures.list(id: "l1", name: "New", shared: true))
        let summary = try #require(await cache.summaries(scope: .mine).first)
        #expect(summary.name == "New")
        #expect(summary.sharedWithPartner)
    }

    @Test("applyItem inserts by position and never replaces an item with an older version")
    func applyItem() async throws {
        let (cache, _) = try make()
        await cache.store(ShoppingFixtures.list(items: [ShoppingFixtures.item(id: "i1", name: "Milk", version: 3, position: 1)]))
        await cache.applyItem(ShoppingFixtures.item(id: "i1", name: "Stale", version: 2, position: 1), listID: "l1")
        await cache.applyItem(ShoppingFixtures.item(id: "i0", name: "First", version: 1, position: 0), listID: "l1")
        await cache.applyItem(ShoppingFixtures.item(id: "i1", name: "Fresh", version: 4, position: 1), listID: "l1")
        let items = try #require(await cache.list(id: "l1")).items
        #expect(items.map(\.name) == ["First", "Fresh"])
    }

    @Test("applyItem and removeItem do nothing for a list that is not cached")
    func unknownList() async throws {
        let (cache, _) = try make()
        await cache.applyItem(ShoppingFixtures.item(), listID: "nope")
        await cache.removeItem(id: "i1", listID: "nope")
        #expect(await cache.list(id: "nope") == nil)
    }

    @Test("enqueue numbers intents monotonically across lists and collapses a repeated check")
    func enqueueCollapses() async throws {
        let (cache, _) = try make()
        _ = await cache.enqueue(kind: .check, listID: "l1", itemID: "i1", payload: nil)
        _ = await cache.enqueue(kind: .check, listID: "l2", itemID: "j1", payload: nil)
        let after = await cache.enqueue(kind: .uncheck, listID: "l1", itemID: "i1", payload: nil)
        #expect(after.map(\.kind) == [.uncheck])
        let all = await cache.intents()
        #expect(all.map(\.itemID) == ["j1", "i1"])
        #expect(all.map(\.sequence) == [2, 3])
    }

    @Test("Review focus 2: queued intents survive the app being killed (a new cache on the same store)")
    func survivesRelaunch() async throws {
        let (cache, container) = try make()
        let temp = ShoppingIntent.newTempID()
        _ = await cache.enqueue(kind: .add, listID: "l1", itemID: temp, payload: .init(name: "Bread", quantity: 2, unit: .piece))
        _ = await cache.enqueue(kind: .check, listID: "l1", itemID: "i1", payload: nil)
        let relaunched = CacheStore.makeShoppingCache(container)
        let intents = await relaunched.intents()
        #expect(intents.map(\.kind) == [.add, .check])
        #expect(intents[0].payload == .init(name: "Bread", quantity: 2, unit: .piece))
        #expect(intents[0].itemID == temp)
    }

    @Test("rewriteTempID points later intents at the server id; dropIntents forgets an item's rows")
    func rewriteAndDrop() async throws {
        let (cache, _) = try make()
        let temp = ShoppingIntent.newTempID()
        _ = await cache.enqueue(kind: .add, listID: "l1", itemID: temp, payload: .init(name: "Bread"))
        _ = await cache.enqueue(kind: .check, listID: "l1", itemID: temp, payload: nil)
        await cache.rewriteTempID(temp, to: "srv-1")
        #expect(await cache.intents().map(\.itemID) == ["srv-1", "srv-1"])
        await cache.dropIntents(itemID: "srv-1")
        #expect(await cache.intents().isEmpty)
    }

    @Test("removeList deletes the list, its summary row and its intents (review focus 4)")
    func removeList() async throws {
        let (cache, _) = try make()
        await cache.replaceSummaries([ShoppingFixtures.summary(id: "l1")], scope: .mine)
        await cache.store(ShoppingFixtures.list(id: "l1", items: [ShoppingFixtures.item()]))
        _ = await cache.enqueue(kind: .check, listID: "l1", itemID: "i1", payload: nil)
        _ = await cache.enqueue(kind: .check, listID: "l2", itemID: "j1", payload: nil)
        await cache.removeList(id: "l1")
        #expect(await cache.list(id: "l1") == nil)
        #expect(await cache.summaries(scope: .mine).isEmpty)
        #expect(await cache.intents().map(\.listID) == ["l2"])
    }

    @Test("clearAll empties everything")
    func clearAll() async throws {
        let (cache, _) = try make()
        await cache.store(ShoppingFixtures.list())
        await cache.replaceSummaries([ShoppingFixtures.summary()], scope: .partner)
        _ = await cache.enqueue(kind: .check, listID: "l1", itemID: "i1", payload: nil)
        await cache.clearAll()
        #expect(await cache.list(id: "l1") == nil)
        #expect(await cache.summaries(scope: .partner).isEmpty)
        #expect(await cache.intents().isEmpty)
    }
}
