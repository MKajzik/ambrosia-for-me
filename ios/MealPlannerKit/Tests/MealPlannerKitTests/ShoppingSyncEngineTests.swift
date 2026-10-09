import API
import Foundation
import Persistence
import Testing
@testable import Repositories

@Suite
struct ShoppingSyncEngineTests {
    private func make(_ server: ShoppingServer, gate: Gate? = nil) throws -> (ShoppingSyncEngine, ShoppingCache, RoutingTransport) {
        let transport = RoutingTransport { call in
            if let gate { await gate.wait() }
            return try await server.route(call)
        }
        let cache = CacheStore.makeShoppingCache(try CacheStore.inMemoryContainer())
        return (ShoppingSyncEngine(client: makeAuthlessClient(transport: transport), cache: cache), cache, transport)
    }

    private func waitForCalls(_ transport: RoutingTransport, _ count: Int) async {
        let deadline = ContinuousClock.now + .seconds(3)
        while await transport.calls.count < count, ContinuousClock.now < deadline { try? await Task.sleep(for: .milliseconds(5)) }
    }

    private func seeded() -> ShoppingServer {
        ShoppingServer([ShoppingFixtures.list(items: [ShoppingFixtures.item(id: "i1"), ShoppingFixtures.item(id: "i2", name: "Eggs", position: 1)])])
    }

    @Test("Replays in sequence order, rewrites a temp id after its add, and leaves the queue empty")
    func orderedReplay() async throws {
        let server = seeded()
        let (engine, cache, transport) = try make(server)
        await cache.store(try #require(server.list("l1")))
        let temp = ShoppingIntent.newTempID()
        await cache.enqueue(kind: .add, listID: "l1", itemID: temp, payload: .init(name: "Bread"))
        await cache.enqueue(kind: .check, listID: "l1", itemID: temp, payload: nil)
        await cache.enqueue(kind: .check, listID: "l1", itemID: "i1", payload: nil)

        await engine.drain()

        #expect(await transport.calls.map(\.route) == [
            "POST /shopping-lists/l1/items", "PATCH /shopping-lists/l1/items/srv-101", "PATCH /shopping-lists/l1/items/i1",
        ])
        #expect(await cache.intents().isEmpty)
        let items = try #require(await cache.list(id: "l1")).items
        let bread = try #require(items.first { $0.name == "Bread" })
        #expect(bread.id == "srv-101")
        #expect(bread.checked)
        #expect(items.first { $0.id == "i1" }?.checked == true)
        #expect(server.list("l1")?.items.first { $0.id == "i1" }?.checked == true)
    }

    @Test("A network failure keeps the rows and stops; the next drain finishes the job")
    func networkFailureKeepsRows() async throws {
        let server = seeded()
        let (engine, cache, _) = try make(server)
        await cache.store(try #require(server.list("l1")))
        await cache.enqueue(kind: .check, listID: "l1", itemID: "i1", payload: nil)
        await cache.enqueue(kind: .check, listID: "l1", itemID: "i2", payload: nil)
        server.setOffline(true)
        await engine.drain()
        #expect(await cache.intents().count == 2)
        server.setOffline(false)
        await engine.drain()
        #expect(await cache.intents().isEmpty)
        #expect(server.list("l1")?.items.allSatisfy(\.checked) == true)
    }

    @Test("A 5xx on one list blocks only that list; another list still drains")
    func perListBlocking() async throws {
        let server = ShoppingServer([
            ShoppingFixtures.list(id: "l1", items: [ShoppingFixtures.item(id: "i1", listID: "l1")]),
            ShoppingFixtures.list(id: "l2", items: [ShoppingFixtures.item(id: "j1", listID: "l2")]),
        ])
        server.force("PATCH /shopping-lists/l1/items/i1", status: 503)
        let (engine, cache, _) = try make(server)
        await cache.enqueue(kind: .check, listID: "l1", itemID: "i1", payload: nil)
        await cache.enqueue(kind: .check, listID: "l2", itemID: "j1", payload: nil)
        await engine.drain()
        #expect(await cache.intents().map(\.listID) == ["l1"])
        #expect(server.list("l2")?.items.first?.checked == true)
    }

    @Test("Review focus 3: a 404 (the partner deleted the item) drops the row and the item, silently")
    func deletedUnderneath() async throws {
        let server = seeded()
        let (engine, cache, _) = try make(server)
        await cache.store(try #require(server.list("l1")))
        await cache.enqueue(kind: .check, listID: "l1", itemID: "i1", payload: nil)
        server.deleteItem(listID: "l1", itemID: "i1")
        await engine.drain()
        #expect(await cache.intents().isEmpty)
        #expect(await cache.list(id: "l1")?.items.map(\.id) == ["i2"])
        #expect(await engine.takeNotice() == nil)
    }

    @Test("A remove of an item that is already gone counts as done")
    func removeAlreadyGone() async throws {
        let server = seeded()
        let (engine, cache, _) = try make(server)
        await cache.store(try #require(server.list("l1")))
        await cache.enqueue(kind: .remove, listID: "l1", itemID: "i1", payload: nil)
        server.deleteItem(listID: "l1", itemID: "i1")
        await engine.drain()
        #expect(await cache.intents().isEmpty)
        #expect(await cache.list(id: "l1")?.items.map(\.id) == ["i2"])
    }

    @Test("A refused add (400) drops the row and everything addressed to its temp id, and leaves a notice")
    func refusedAdd() async throws {
        let server = seeded()
        server.force("POST /shopping-lists/l1/items", status: 400)
        let (engine, cache, transport) = try make(server)
        let temp = ShoppingIntent.newTempID()
        await cache.enqueue(kind: .add, listID: "l1", itemID: temp, payload: .init(name: "Bread"))
        await cache.enqueue(kind: .check, listID: "l1", itemID: temp, payload: nil)
        await engine.drain()
        #expect(await cache.intents().isEmpty)
        #expect(await transport.calls.count == 1)
        #expect(await engine.takeNotice() == "A change couldn't be saved.")
        #expect(await engine.takeNotice() == nil)
    }

    @Test("Concurrent drains coalesce: the request is sent once")
    func coalesces() async throws {
        let server = seeded()
        let gate = Gate()
        let (engine, cache, transport) = try make(server, gate: gate)
        await cache.store(try #require(server.list("l1")))
        await cache.enqueue(kind: .check, listID: "l1", itemID: "i1", payload: nil)
        let first = Task { await engine.drain() }
        await waitForCalls(transport, 1)
        await engine.drain() // arrives while the first is mid-request: it must not start a second request
        await gate.release()
        await first.value
        #expect(await transport.calls("PATCH /shopping-lists/l1/items/i1").count == 1)
        #expect(await cache.intents().isEmpty)
    }

    @Test("changes() yields after queue steps, to every subscriber")
    func changesStream() async throws {
        let server = seeded()
        let (engine, cache, _) = try make(server)
        await cache.store(try #require(server.list("l1")))
        let a = await engine.changes()
        let b = await engine.changes()
        await cache.enqueue(kind: .check, listID: "l1", itemID: "i1", payload: nil)
        await engine.drain()
        var iteratorA = a.makeAsyncIterator()
        var iteratorB = b.makeAsyncIterator()
        #expect(await iteratorA.next() != nil)
        #expect(await iteratorB.next() != nil)
    }
}
