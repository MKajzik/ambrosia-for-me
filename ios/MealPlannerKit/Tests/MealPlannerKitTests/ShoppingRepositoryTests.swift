import API
import Foundation
import Persistence
import Testing
@testable import Repositories

@Suite
struct ShoppingRepositoryTests {
    private func make(_ route: @escaping RoutingTransport.Route) throws -> (ShoppingListsRepository, ShoppingCache, RoutingTransport) {
        let transport = RoutingTransport(route)
        let cache = CacheStore.makeShoppingCache(try CacheStore.inMemoryContainer())
        return (ShoppingListsRepository(client: makeAuthlessClient(transport: transport), cache: cache), cache, transport)
    }

    @Test("The first page replaces the scope and returns the next cursor; the next page merges")
    func paging() async throws {
        let (repo, _, transport) = try make { call in
            if call.path.contains("cursor=c2") { return (200, ShoppingFixtures.page([ShoppingFixtures.summary(id: "c")])) }
            return (200, ShoppingFixtures.page([ShoppingFixtures.summary(id: "a"), ShoppingFixtures.summary(id: "b")], next: "c2"))
        }
        let next = try await repo.refreshLists(.mine, cursor: nil)
        #expect(next == "c2")
        #expect(await repo.cachedLists(.mine).map(\.id) == ["a", "b"])
        let last = try await repo.refreshLists(.mine, cursor: "c2")
        #expect(last == nil)
        #expect(await repo.cachedLists(.mine).map(\.id) == ["a", "b", "c"])
        #expect(await transport.calls("GET /shopping-lists").count == 2)
    }

    @Test("The partner scope reads /partner/shopping-lists; a 404 clears it and says the partner is not linked")
    func partnerScope() async throws {
        let linked = Locked(true)
        let (repo, _, transport) = try make { _ in
            linked.value ? (200, ShoppingFixtures.page([ShoppingFixtures.summary(id: "p")])) : (404, Fixtures.problem(404, code: "partner_not_linked"))
        }
        _ = try await repo.refreshLists(.partner, cursor: nil)
        #expect(await repo.cachedLists(.partner).map(\.id) == ["p"])
        #expect(await transport.calls("GET /partner/shopping-lists").count == 1)
        linked.set(false)
        await #expect(throws: ShoppingError.partnerNotLinked) { try await repo.refreshLists(.partner, cursor: nil) }
        #expect(await repo.cachedLists(.partner).isEmpty)
    }

    @Test("refreshList stores the snapshot; a failed refresh leaves the cache alone")
    func refreshList() async throws {
        let fail = Locked(false)
        let list = ShoppingFixtures.list(items: [ShoppingFixtures.item()])
        let (repo, _, _) = try make { _ in fail.value ? (500, Fixtures.problem(500, code: "internal")) : (200, Fixtures.json(list)) }
        try await repo.refreshList(id: "l1")
        #expect(await repo.cachedList(id: "l1")?.items.count == 1)
        fail.set(true)
        await #expect(throws: ShoppingError.server("The server had a problem loading the list.")) { try await repo.refreshList(id: "l1") }
        #expect(await repo.cachedList(id: "l1")?.items.count == 1)
    }

    @Test("Review focus 4: a 404 on refresh removes the list and its pending changes")
    func refreshListGone() async throws {
        let (repo, cache, _) = try make { _ in (404, Fixtures.problem(404, code: "not_found")) }
        await cache.store(ShoppingFixtures.list(items: [ShoppingFixtures.item()]))
        await repo.setChecked(true, itemID: "i1", listID: "l1")
        await #expect(throws: ShoppingError.notFound) { try await repo.refreshList(id: "l1") }
        #expect(await repo.cachedList(id: "l1") == nil)
        #expect(await repo.pendingIntents(listID: "l1").isEmpty)
    }

    @Test("generate accepts 200 (regenerated) and 201 (new), and sends list_id when regenerating")
    func generate() async throws {
        let status = Locked(201)
        let (repo, _, transport) = try make { _ in (status.value, Fixtures.json(ShoppingFixtures.list(id: "g1", from: "2026-10-05", to: "2026-10-11"))) }
        let created = try await repo.generate(from: "2026-10-05", to: "2026-10-11", name: "Week", listID: nil)
        #expect(created.id == "g1")
        status.set(200)
        _ = try await repo.generate(from: "2026-10-05", to: "2026-10-11", name: nil, listID: "g1")
        let bodies = await transport.calls("POST /shopping-lists/generate").map(\.body)
        #expect(bodies[0].contains("\"name\":\"Week\""))
        #expect(!bodies[0].contains("list_id"))
        #expect(bodies[1].contains("\"list_id\":\"g1\""))
        #expect(await repo.cachedList(id: "g1") != nil)
    }

    @Test("generate over an empty plan is a 404")
    func generateNothingPlanned() async throws {
        let (repo, _, _) = try make { _ in (404, Fixtures.problem(404, code: "not_found")) }
        await #expect(throws: ShoppingError.notFound) { try await repo.generate(from: "2026-10-05", to: "2026-10-06", name: nil, listID: nil) }
    }

    @Test("editItem sends the version and fields; the answer replaces the cached item")
    func editItem() async throws {
        let edited = ShoppingFixtures.item(name: "Oat milk", version: 2, quantity: 1.5, unit: .ml)
        let (repo, cache, transport) = try make { _ in (200, Fixtures.json(edited)) }
        await cache.store(ShoppingFixtures.list(items: [ShoppingFixtures.item()]))
        let item = try await repo.editItem(
            listID: "l1", itemID: "i1", version: 1, edit: .init(name: "Oat milk", quantity: 1.5, unit: .ml, category: .dairyEggs)
        )
        #expect(item == edited)
        let body = try #require(await transport.calls("PATCH /shopping-lists/l1/items/i1").first?.body)
        #expect(body.contains("\"version\":1"))
        #expect(body.contains("\"name\":\"Oat milk\""))
        #expect(body.contains("\"unit\":\"ml\""))
        #expect(await cache.list(id: "l1")?.items.first?.name == "Oat milk")
    }

    @Test("A 409 carries the item's current state, which also replaces the cached item")
    func editConflict() async throws {
        let current = ShoppingFixtures.item(name: "Someone else's", version: 5)
        let (repo, cache, _) = try make { _ in (409, ShoppingFixtures.conflict(current: current)) }
        await cache.store(ShoppingFixtures.list(items: [ShoppingFixtures.item(version: 3)]))
        await #expect(throws: ShoppingError.versionConflict(current: current)) {
            try await repo.editItem(listID: "l1", itemID: "i1", version: 3, edit: .init(name: "Mine", quantity: nil, unit: nil, category: .other))
        }
        #expect(await cache.list(id: "l1")?.items.first?.version == 5)
    }

    @Test("Rename and share patch the list; delete removes it, and deleting a list already gone is a success")
    func listWrites() async throws {
        let (repo, cache, transport) = try make { call in
            if call.method == "DELETE" { return (404, Fixtures.problem(404, code: "not_found")) }
            return (200, Fixtures.json(ShoppingFixtures.list(name: "Renamed", shared: true)))
        }
        let updated = try await repo.updateList(id: "l1", name: "Renamed", shared: true)
        #expect(updated.sharedWithPartner)
        #expect(try #require(await transport.calls("PATCH /shopping-lists/l1").first).body.contains("\"shared_with_partner\":true"))
        try await repo.deleteList(id: "l1")
        #expect(await cache.list(id: "l1") == nil)
    }

    @Test("The offline-able writes only enqueue, collapse, and send nothing")
    func enqueueOnly() async throws {
        let (repo, _, transport) = try make { _ in (500, "") }
        await repo.setChecked(true, itemID: "i1", listID: "l1")
        await repo.setChecked(false, itemID: "i1", listID: "l1")
        #expect(await repo.pendingIntents(listID: "l1").map(\.kind) == [.uncheck])
        let temp = await repo.addItem(.init(name: "Bread"), listID: "l1")
        #expect(temp.hasPrefix(ShoppingIntent.tempPrefix))
        await repo.removeItem(itemID: temp, listID: "l1")
        #expect(await repo.pendingIntents(listID: "l1").map(\.kind) == [.uncheck])
        #expect(await transport.calls.isEmpty)
    }

    @Test("A 400 becomes a readable validation message")
    func validation() async throws {
        let (repo, _, _) = try make { _ in (400, Fixtures.problem(400, code: "validation_failed", errors: [("name", "required")])) }
        await #expect(throws: ShoppingError.validationFailed("Name is required.")) { try await repo.createList(name: "", shared: false) }
    }
}
