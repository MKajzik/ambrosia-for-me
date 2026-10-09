import API
import Foundation
import Persistence
import Testing
@testable import Features
@testable import Repositories

typealias ListEventSource = ShoppingListViewModel.EventSource

@MainActor
@Suite
struct ShoppingListViewModelTests {
    private nonisolated static func quiet(_: String) -> AsyncThrowingStream<ListEvent, Error> { AsyncThrowingStream { $0.finish() } }

    private func seeded() -> ShoppingServer {
        ShoppingServer([ShoppingFixtures.list(items: [
            ShoppingFixtures.item(id: "i1", name: "Milk", category: .dairyEggs, position: 0),
            ShoppingFixtures.item(id: "i2", name: "Eggs", category: .dairyEggs, position: 1),
        ])])
    }

    private func make(
        _ h: ShoppingHarness, events: @escaping ListEventSource = { ShoppingListViewModelTests.quiet($0) },
        sleep: @escaping @Sendable (Duration) async throws -> Void = { _ in }
    ) -> ShoppingListViewModel {
        ShoppingListViewModel(listID: "l1", shopping: h.repository, sync: h.engine, events: events, userID: { "u1" }, sleep: sleep)
    }

    private func item(_ vm: ShoppingListViewModel, _ id: String) -> Components.Schemas.ShoppingItem? {
        vm.displayed?.items.first { $0.id == id }
    }

    @Test("load shows the server's list grouped by aisle with its progress")
    func load() async throws {
        let h = try ShoppingHarness(seeded())
        let vm = make(h)
        await vm.load()
        #expect(vm.groups.map(\.category) == [.dairyEggs])
        #expect(vm.progress == (done: 0, total: 2))
        #expect(vm.isOwner)
    }

    @Test("Online, a toggle is sent at once and the queue ends empty")
    func toggleOnline() async throws {
        let h = try ShoppingHarness(seeded())
        let vm = make(h)
        await vm.load()
        await vm.toggle(try #require(item(vm, "i1")))
        #expect(item(vm, "i1")?.checked == true)
        #expect(vm.pendingCount == 0)
        #expect(h.server.list("l1")?.items.first?.checked == true)
    }

    @Test("Offline, a toggle looks done at once, queues one row, and syncs when the connection returns")
    func toggleOffline() async throws {
        let h = try ShoppingHarness(seeded())
        let vm = make(h)
        await vm.load()
        h.server.setOffline(true)
        await vm.toggle(try #require(item(vm, "i1")))
        #expect(item(vm, "i1")?.checked == true)
        #expect(vm.isSyncing)
        #expect(h.server.list("l1")?.items.first?.checked == false)

        h.server.setOffline(false)
        await h.engine.drain()
        await vm.reload()
        #expect(!vm.isSyncing)
        #expect(h.server.list("l1")?.items.first?.checked == true)
    }

    @Test("Review focus 1: two quick taps offline leave one row and the last state on screen")
    func doubleToggleOffline() async throws {
        let h = try ShoppingHarness(seeded())
        let vm = make(h)
        await vm.load()
        h.server.setOffline(true)
        await vm.toggle(try #require(item(vm, "i1"))) // check
        await vm.toggle(try #require(item(vm, "i1"))) // uncheck
        await vm.toggle(try #require(item(vm, "i1"))) // check
        #expect(vm.pendingCount == 1)
        #expect(item(vm, "i1")?.checked == true)
    }

    @Test("An offline quick-add appears at once as pending, then becomes the server's item")
    func quickAddOffline() async throws {
        let h = try ShoppingHarness(seeded())
        let vm = make(h)
        await vm.load()
        h.server.setOffline(true)
        #expect(await vm.quickAdd("  Bread  "))
        let temp = try #require(vm.displayed?.items.last)
        #expect(temp.name == "Bread")
        #expect(temp.id.hasPrefix(ShoppingIntent.tempPrefix))

        h.server.setOffline(false)
        await h.engine.drain()
        await vm.reload()
        #expect(!vm.isSyncing)
        #expect(vm.displayed?.items.last?.id == "srv-101")
    }

    @Test("A blank quick-add is refused without queueing anything")
    func quickAddBlank() async throws {
        let h = try ShoppingHarness(seeded())
        let vm = make(h)
        await vm.load()
        #expect(await vm.quickAdd("   ") == false)
        #expect(vm.pendingCount == 0)
    }

    @Test("Removing an item that was never synced cancels its add and sends nothing")
    func removeUnsynced() async throws {
        let h = try ShoppingHarness(seeded())
        let vm = make(h)
        await vm.load()
        h.server.setOffline(true)
        _ = await vm.quickAdd("Bread")
        await vm.remove(try #require(vm.displayed?.items.last))
        #expect(vm.pendingCount == 0)
        #expect(vm.displayed?.items.count == 2)
    }

    @Test("An edit on a stale version is a conflict that carries the server's item, and the screen shows it")
    func editConflict() async throws {
        let h = try ShoppingHarness(seeded())
        let vm = make(h)
        await vm.load()
        let opened = try #require(item(vm, "i1"))
        h.server.mutate(listID: "l1", itemID: "i1") { $0.name = "Oat milk" }
        let outcome = await vm.edit(opened, name: "Mine", quantity: "", unit: nil, category: .dairyEggs)
        guard case .conflict(let current) = outcome else { Issue.record("expected conflict, got \(outcome)"); return }
        #expect(current.name == "Oat milk")
        #expect(item(vm, "i1")?.name == "Oat milk")
    }

    @Test("An edit saves a comma decimal; junk and blank names are refused before any request")
    func editValidation() async throws {
        let h = try ShoppingHarness(seeded())
        let vm = make(h)
        await vm.load()
        let milk = try #require(item(vm, "i1"))
        let before = await h.transport.calls.count
        #expect(await vm.edit(milk, name: " ", quantity: "", unit: nil, category: .dairyEggs) == .failed("Give the item a name."))
        #expect(await vm.edit(milk, name: "Milk", quantity: "abc", unit: nil, category: .dairyEggs) == .failed("Enter a valid quantity."))
        #expect(await h.transport.calls.count == before)
        #expect(await vm.edit(milk, name: "Milk", quantity: "1,5", unit: .ml, category: .dairyEggs) == .saved)
        #expect(item(vm, "i1")?.quantity == 1.5)
    }

    // MARK: Live updates

    @Test("An event at or below the cached version is ignored; a newer one refetches")
    func staleAndNewer() async throws {
        let h = try ShoppingHarness(seeded())
        let vm = make(h)
        await vm.load()
        let gets = { await h.transport.calls("GET /shopping-lists/l1").count }
        let base = await gets()
        await vm.handle(ListEvent(kind: .itemChanged, listID: "l1", itemID: "i1", version: 1))
        #expect(await gets() == base)
        h.server.mutate(listID: "l1", itemID: "i1") { $0.checked = true }
        await vm.handle(ListEvent(kind: .itemChanged, listID: "l1", itemID: "i1", version: 2))
        #expect(await gets() == base + 1)
        #expect(item(vm, "i1")?.checked == true)
    }

    @Test("A refetch keeps a pending change on screen (the overlay survives the echo)")
    func overlaySurvivesRefetch() async throws {
        let h = try ShoppingHarness(seeded())
        let vm = make(h)
        await vm.load()
        h.server.setOffline(true)
        await vm.toggle(try #require(item(vm, "i1")))
        h.server.setOffline(false)
        h.server.mutate(listID: "l1", itemID: "i2") { $0.checked = true } // the partner ticked Eggs meanwhile
        await vm.handle(ListEvent(kind: .listChanged, listID: "l1"))
        #expect(item(vm, "i1")?.checked == true) // still pending, still shown
        #expect(item(vm, "i2")?.checked == true) // and the partner's change arrived
    }

    @Test("item_deleted removes the item")
    func itemDeleted() async throws {
        let h = try ShoppingHarness(seeded())
        let vm = make(h)
        await vm.load()
        await vm.handle(ListEvent(kind: .itemDeleted, listID: "l1", itemID: "i1", version: 1))
        #expect(vm.displayed?.items.map(\.id) == ["i2"])
    }

    @Test("Review focus 4: list_deleted means access lost even though the list is cached, and pending changes are dropped")
    func listDeleted() async throws {
        let h = try ShoppingHarness(seeded())
        let vm = make(h)
        await vm.load()
        h.server.setOffline(true)
        await vm.toggle(try #require(item(vm, "i1")))
        await vm.handle(ListEvent(kind: .listDeleted, listID: "l1"))
        #expect(vm.accessLost)
        #expect(vm.displayed == nil)
        #expect(await h.repository.pendingIntents(listID: "l1").isEmpty)
    }

    @Test("A 404 on refetch is access lost, even with items cached")
    func refetch404() async throws {
        let h = try ShoppingHarness(seeded())
        let vm = make(h)
        await vm.load()
        h.server.deleteList("l1")
        await vm.handle(ListEvent(kind: .listChanged, listID: "l1"))
        #expect(vm.accessLost)
    }

    @Test("When the stream closes it waits, refetches and reconnects; a 404 on the stream ends it with access lost")
    func reconnect() async throws {
        let h = try ShoppingHarness(seeded())
        let opened = Locked(0)
        let sleeper = TestSleeper()
        let vm = make(
            h,
            events: { _ in
                opened.mutate { $0 += 1 }
                if opened.value == 1 { return AsyncThrowingStream { $0.finish() } }
                return AsyncThrowingStream { $0.finish(throwing: ListEventStream.StreamError.accessLost) }
            },
            sleep: { _ in try await sleeper.sleep() }
        )
        await vm.load()
        let task = Task { await vm.runLiveUpdates() }
        await sleeper.fire() // the first stream ended; release the backoff
        await task.value
        #expect(opened.value == 2)
        #expect(vm.accessLost)
    }

    @Test("A refused queued change surfaces the engine's notice once")
    func notice() async throws {
        let h = try ShoppingHarness(seeded())
        h.server.force("POST /shopping-lists/l1/items", status: 400)
        let vm = make(h)
        await vm.load()
        _ = await vm.quickAdd("Bread")
        #expect(vm.alertMessage == "A change couldn't be saved.")
    }
}
