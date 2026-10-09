import API
import Foundation
import Persistence
import Testing
@testable import Features
@testable import Repositories

@MainActor
@Suite
struct ShoppingViewModelTests {
    private let utc = LocalDay(timeZone: TimeZone(identifier: "UTC")!, now: { Date(timeIntervalSince1970: 1_791_000_000) })

    private func make(_ harness: ShoppingHarness) -> ShoppingViewModel {
        ShoppingViewModel(shopping: harness.repository, partner: harness.partner, day: utc)
    }

    @Test("appear shows the cache at once, then the server's lists, and the Partner's segment when linked")
    func appear() async throws {
        let h = try ShoppingHarness(ShoppingServer([ShoppingFixtures.list(id: "a", name: "Week")]))
        await h.cache.replaceSummaries([ShoppingFixtures.summary(id: "old", name: "Old")], scope: .mine)
        let vm = make(h)
        await vm.appear()
        #expect(vm.lists.map(\.id) == ["a"])
        #expect(vm.showsPartnerSegment)
        #expect(!vm.isStale)
    }

    @Test("Without a partner the segment is hidden and a partner scope falls back to Mine")
    func noPartner() async throws {
        let h = try ShoppingHarness(ShoppingServer([ShoppingFixtures.list(id: "a")]), partnerActive: false)
        let vm = make(h)
        await vm.appear()
        #expect(!vm.showsPartnerSegment)
        #expect(vm.scope == .mine)
    }

    @Test("Offline with a cache keeps the lists and marks them stale; with no cache it shows an error")
    func offline() async throws {
        let server = ShoppingServer([ShoppingFixtures.list(id: "a")])
        let h = try ShoppingHarness(server)
        let vm = make(h)
        await vm.appear()
        server.setOffline(true)
        await vm.load()
        #expect(vm.lists.map(\.id) == ["a"])
        #expect(vm.isStale)
        #expect(vm.loadError == nil)

        let empty = try ShoppingHarness(ShoppingServer())
        empty.server.setOffline(true)
        let bare = make(empty)
        await bare.load()
        #expect(bare.lists.isEmpty)
        #expect(bare.loadError == "Can't reach the server. Check your connection and try again.")
    }

    @Test("Load more fetches the next page with its cursor and appends")
    func loadMore() async throws {
        let transport = RoutingTransport { call in
            if call.path.contains("cursor=c2") { return (200, ShoppingFixtures.page([ShoppingFixtures.summary(id: "c")])) }
            return (200, ShoppingFixtures.page([ShoppingFixtures.summary(id: "a"), ShoppingFixtures.summary(id: "b")], next: "c2"))
        }
        let client = makeAuthlessClient(transport: transport)
        let repo = ShoppingListsRepository(client: client, cache: CacheStore.makeShoppingCache(try CacheStore.inMemoryContainer()))
        let vm = ShoppingViewModel(shopping: repo, partner: PartnerRepository(client: client), day: utc)
        await vm.load()
        #expect(vm.hasMore)
        await vm.loadMore()
        #expect(vm.lists.map(\.id) == ["a", "b", "c"])
        #expect(!vm.hasMore)
    }

    @Test("createList trims, refuses a blank name without a request, and reports the new list")
    func createList() async throws {
        let h = try ShoppingHarness()
        let vm = make(h)
        #expect(await vm.createList(name: "   ", shared: false) == .failed("Give the list a name."))
        #expect(await h.transport.calls.isEmpty)
        let outcome = await vm.createList(name: "  Party  ", shared: true)
        guard case .opened(let id) = outcome else { Issue.record("expected opened, got \(outcome)"); return }
        #expect(h.server.list(id)?.name == "Party")
        #expect(h.server.list(id)?.sharedWithPartner == true)
        #expect(vm.lists.map(\.id) == [id])
    }

    @Test("generate refuses 93 days without a request, defaults to the next seven days, and explains an empty plan")
    func generate() async throws {
        let h = try ShoppingHarness()
        let vm = make(h)
        let range = vm.defaultRange()
        #expect(range.from == utc.today())
        #expect(range.to == utc.addDays(utc.today(), 6))
        #expect(await vm.generate(from: "2026-01-01", to: "2026-04-03", name: nil) == .failed("Pick at most 92 days."))
        #expect(await h.transport.calls.isEmpty)
        let ok = await vm.generate(from: range.from, to: range.to, name: " ")
        guard case .opened = ok else { Issue.record("expected opened, got \(ok)"); return }
        // A blank name is sent as no name, so the server picks its default.
        let generateBody = try #require(await h.transport.calls("POST /shopping-lists/generate").first).body
        #expect(!generateBody.contains("\"name\""))

        h.server.force("POST /shopping-lists/generate", status: 404)
        #expect(await vm.generate(from: range.from, to: range.to, name: nil) == .failed("Nothing is planned on those days."))
    }

    private func makeVM(_ transport: RoutingTransport) throws -> ShoppingViewModel {
        let client = makeAuthlessClient(transport: transport)
        let repo = ShoppingListsRepository(client: client, cache: CacheStore.makeShoppingCache(try CacheStore.inMemoryContainer()))
        return ShoppingViewModel(shopping: repo, partner: PartnerRepository(client: client), day: utc)
    }

    @Test("A failed refresh for a new scope clears the previous scope's paging, so Load more does nothing")
    func pagingResetsOnScopeChange() async throws {
        let transport = RoutingTransport { call in
            if call.path.contains("/partner/") { throw URLError(.notConnectedToInternet) }
            return (200, ShoppingFixtures.page([ShoppingFixtures.summary(id: "a")], next: "c2"))
        }
        let vm = try makeVM(transport)
        await vm.load()
        #expect(vm.hasMore)
        await vm.select(.partner)
        #expect(!vm.hasMore)
        let before = await transport.calls.count
        await vm.loadMore()
        #expect(await transport.calls.count == before)
    }

    @Test("A failed first-page reload drops the old cursor")
    func failedReloadDropsCursor() async throws {
        let offline = Locked(false)
        let transport = RoutingTransport { _ in
            if offline.value { throw URLError(.notConnectedToInternet) }
            return (200, ShoppingFixtures.page([ShoppingFixtures.summary(id: "a")], next: "c2"))
        }
        let vm = try makeVM(transport)
        await vm.load()
        offline.set(true)
        await vm.load()
        #expect(!vm.hasMore)
    }

    @Test("isLoading stays true until every overlapping load finishes, and a stale cache read cannot replace the new scope's lists")
    func overlappingLoads() async throws {
        let gate = Gate()
        let transport = RoutingTransport { call in
            if call.path.contains("/partner/") { return (200, ShoppingFixtures.page([ShoppingFixtures.summary(id: "p")])) }
            await gate.wait()
            return (200, ShoppingFixtures.page([ShoppingFixtures.summary(id: "m")]))
        }
        let vm = try makeVM(transport)
        let first = Task { await vm.load() }
        while await transport.calls.isEmpty { await Task.yield() }
        await vm.select(.partner)
        #expect(vm.lists.map(\.id) == ["p"])
        #expect(vm.isLoading)
        await gate.release()
        await first.value
        #expect(!vm.isLoading)
        #expect(vm.scope == .partner)
        #expect(vm.lists.map(\.id) == ["p"])
    }

    @Test("After createList the paging state matches the refreshed first page")
    func createKeepsPaging() async throws {
        let transport = RoutingTransport { call in
            if call.route == "POST /shopping-lists" {
                return (201, Fixtures.json(ShoppingFixtures.list(id: "new", name: "New")))
            }
            if call.path.contains("cursor=c2") { return (200, ShoppingFixtures.page([ShoppingFixtures.summary(id: "b")])) }
            return (200, ShoppingFixtures.page([ShoppingFixtures.summary(id: "a")], next: "c2"))
        }
        let vm = try makeVM(transport)
        await vm.load()
        await vm.loadMore()
        #expect(!vm.hasMore)
        _ = await vm.createList(name: "New", shared: false)
        #expect(vm.hasMore)
        await vm.loadMore()
        #expect(await transport.calls.contains { $0.path.contains("cursor=c2") })
    }
}
