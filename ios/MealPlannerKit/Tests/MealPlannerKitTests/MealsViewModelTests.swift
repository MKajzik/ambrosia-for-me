import API
import Foundation
import Persistence
import Repositories
import Testing
@testable import Features

@Suite
@MainActor
struct MealsViewModelTests {
    @MainActor
    struct Harness {
        let vm: MealsViewModel
        let cache: MealCache
        let transport: RoutingTransport

        init(_ route: @escaping RoutingTransport.Route) throws {
            transport = RoutingTransport(route)
            cache = CacheStore.makeMealCache(try CacheStore.inMemoryContainer())
            let client = makeAuthlessClient(transport: transport)
            vm = MealsViewModel(meals: MealsRepository(client: client, cache: cache), partner: PartnerRepository(client: client))
        }
    }

    /// `partner`: "active", "pending", "none" (404) or "offline" (the request throws).
    private static func route(
        partner: String = "active", mine: [Components.Schemas.MealSummary] = [], theirs: [Components.Schemas.MealSummary] = []
    ) -> RoutingTransport.Route {
        { call in
            switch call.route {
            case "GET /partner":
                switch partner {
                case "active": return (200, Fixtures.json(Components.Schemas.Partnership(status: .active, displayName: "Sam", linkedAt: Fixtures.date)))
                case "pending": return (200, Fixtures.json(Components.Schemas.Partnership(status: .pending, expiresAt: Fixtures.date)))
                case "none": return (404, Fixtures.problem(404, code: "partner_not_linked"))
                default: throw URLError(.notConnectedToInternet)
                }
            case "GET /meals": return (200, Fixtures.mealList(mine))
            case "GET /partner/meals":
                return partner == "none"
                    ? (404, Fixtures.problem(404, code: "partner_not_linked"))
                    : (200, Fixtures.mealList(theirs))
            default: return (500, Fixtures.problem(500, code: "internal"))
            }
        }
    }

    @Test("load shows the cache, then replaces it with the server's list and clears the stale mark")
    func loadsThenRefreshes() async throws {
        let h = try Harness(Self.route(mine: [Fixtures.summary(id: "a", name: "A")]))
        await h.cache.replaceSummaries([Fixtures.summary(id: "old", name: "Old")], scope: .mine)
        await h.vm.load()
        #expect(h.vm.meals.map(\.id) == ["a"])
        #expect(h.vm.isStale == false)
        #expect(h.vm.loadError == nil)
        #expect(h.vm.isLoading == false)
    }

    @Test("A refresh failure keeps the cache on screen and marks it stale, with no error banner")
    func offlineKeepsCache() async throws {
        let h = try Harness { _ in throw URLError(.notConnectedToInternet) }
        await h.cache.replaceSummaries([Fixtures.summary(id: "old", name: "Old")], scope: .mine)
        await h.vm.load()
        #expect(h.vm.meals.map(\.id) == ["old"])
        #expect(h.vm.isStale)
        #expect(h.vm.loadError == nil)
    }

    @Test("A refresh failure with nothing cached shows an error state")
    func offlineWithNothingCached() async throws {
        let h = try Harness { _ in throw URLError(.notConnectedToInternet) }
        await h.vm.load()
        #expect(h.vm.meals.isEmpty)
        #expect(h.vm.loadError == "Can't reach the server. Check your connection and try again.")
    }

    @Test("The Partner's segment shows only while the partnership is active")
    func segmentRule() async throws {
        let active = try Harness(Self.route(partner: "active"))
        await active.vm.appear()
        #expect(active.vm.showsPartnerSegment)

        let pending = try Harness(Self.route(partner: "pending"))
        await pending.vm.appear()
        #expect(pending.vm.showsPartnerSegment == false)

        let none = try Harness(Self.route(partner: "none"))
        await none.cache.replaceSummaries([Fixtures.summary(id: "p")], scope: .partner)
        await none.vm.appear()
        #expect(none.vm.showsPartnerSegment == false)
        #expect(await none.cache.summaries(scope: .partner).isEmpty)
    }

    @Test("Offline, the segment shows only if partner meals are already cached")
    func segmentOffline() async throws {
        let withCache = try Harness(Self.route(partner: "offline"))
        await withCache.cache.replaceSummaries([Fixtures.summary(id: "p")], scope: .partner)
        await withCache.vm.appear()
        #expect(withCache.vm.showsPartnerSegment)

        let without = try Harness(Self.route(partner: "offline"))
        await without.vm.appear()
        #expect(without.vm.showsPartnerSegment == false)
    }

    @Test("Switching to Partner's loads the partner list")
    func selectPartner() async throws {
        let h = try Harness(Self.route(mine: [Fixtures.summary(id: "m", name: "Mine")], theirs: [Fixtures.summary(id: "t", name: "Theirs")]))
        await h.vm.appear()
        await h.vm.select(.partner)
        #expect(h.vm.scope == .partner)
        #expect(h.vm.meals.map(\.id) == ["t"])
    }

    @Test("The partner unlinking while Partner's is open hides the segment and falls back to Mine")
    func unlinkedWhileViewing() async throws {
        let linked = Locked(true)
        let h = try Harness { call in
            switch call.route {
            case "GET /partner": return (200, Fixtures.json(Components.Schemas.Partnership(status: .active, displayName: "Sam", linkedAt: Fixtures.date)))
            case "GET /partner/meals":
                return linked.value
                    ? (200, Fixtures.mealList([Fixtures.summary(id: "t")]))
                    : (404, Fixtures.problem(404, code: "partner_not_linked"))
            case "GET /meals": return (200, Fixtures.mealList([Fixtures.summary(id: "m", name: "Mine")]))
            default: return (500, Fixtures.problem(500, code: "internal"))
            }
        }
        await h.vm.appear()
        await h.vm.select(.partner)
        linked.set(false)
        await h.vm.load()
        #expect(h.vm.showsPartnerSegment == false)
        #expect(h.vm.scope == .mine)
        #expect(h.vm.meals.map(\.id) == ["m"])
        #expect(await h.cache.summaries(scope: .partner).isEmpty)
    }

    @Test("Creating validates locally, then opens the new meal in the editor")
    func create() async throws {
        let h = try Harness { call in
            call.route == "POST /meals"
                ? (201, Fixtures.json(Fixtures.meal(id: "made", name: "Soup")))
                : (200, Fixtures.mealList([]))
        }
        #expect(await h.vm.createMeal(name: "  ", servings: "1") == "Give the meal a name.")
        #expect(await h.vm.createMeal(name: "Soup", servings: "0") == "Servings must be more than 0 and at most 1000.")
        #expect(await h.transport.calls.isEmpty)
        #expect(await h.vm.createMeal(name: "Soup", servings: "1,5") == nil)
        #expect(h.vm.presentation == .edit("made"))
        #expect(await h.transport.calls("POST /meals").first?.body.contains("\"servings\":1.5") == true)
    }

    @Test("Delete removes the meal and closes its editor; an in-use meal is refused with an explanation")
    func delete() async throws {
        let inUse = Locked(true)
        let h = try Harness { call in
            if call.route == "DELETE /meals/a" {
                return inUse.value ? (409, Fixtures.problem(409, code: "meal_in_use")) : (204, "")
            }
            return (200, Fixtures.mealList([Fixtures.summary(id: "a", name: "A")]))
        }
        await h.vm.load()
        h.vm.presentation = .edit("a")
        let refusal = await h.vm.delete(id: "a")
        #expect(refusal == "This meal is used in your plan or a diet template. Remove it there first.")
        #expect(h.vm.presentation == .edit("a"))
        #expect(h.vm.meals.map(\.id) == ["a"])
        inUse.set(false)
        #expect(await h.vm.delete(id: "a") == nil)
        #expect(h.vm.presentation == nil)
        #expect(h.vm.meals.isEmpty)
    }

    @Test("Deleting a meal that is already gone counts as deleted")
    func deleteAlreadyGone() async throws {
        let h = try Harness { _ in (404, Fixtures.problem(404, code: "not_found")) }
        await h.cache.replaceSummaries([Fixtures.summary(id: "a")], scope: .mine)
        #expect(await h.vm.delete(id: "a") == nil)
    }

    @Test("confirmDelete clears the pending meal and reports a refusal through alertMessage")
    func confirmDelete() async throws {
        let h = try Harness { call in
            call.route == "DELETE /meals/a"
                ? (409, Fixtures.problem(409, code: "meal_in_use"))
                : (200, Fixtures.mealList([Fixtures.summary(id: "a")]))
        }
        await h.vm.load()
        h.vm.pendingDelete = Fixtures.summary(id: "a")
        await h.vm.confirmDelete(Fixtures.summary(id: "a"))
        #expect(h.vm.pendingDelete == nil)
        #expect(h.vm.alertMessage == "This meal is used in your plan or a diet template. Remove it there first.")
        #expect(MealsViewModel.deleteMessage(name: "Soup") == "\"Soup\" will be removed from your library. This can't be undone.")
    }

    @Test("Copying a partner meal opens the copy in the editor and refreshes Mine")
    func copy() async throws {
        let h = try Harness { call in
            switch call.route {
            case "POST /meals/theirs/copy": return (201, Fixtures.json(Fixtures.meal(id: "copy", name: "Soup")))
            case "GET /meals": return (200, Fixtures.mealList([Fixtures.summary(id: "copy", name: "Soup")]))
            default: return (500, Fixtures.problem(500, code: "internal"))
            }
        }
        h.vm.presentation = .view("theirs")
        #expect(await h.vm.copyToLibrary(id: "theirs") == nil)
        #expect(h.vm.presentation == .edit("copy"))
        #expect(await h.cache.summaries(scope: .mine).map(\.id) == ["copy"])
    }

    @Test("A copy that fails is reported and leaves the sheet alone")
    func copyFails() async throws {
        let h = try Harness { _ in (404, Fixtures.problem(404, code: "not_found")) }
        h.vm.presentation = .view("theirs")
        #expect(await h.vm.copyToLibrary(id: "theirs") == "This meal isn't available anymore.")
        #expect(h.vm.presentation == .view("theirs"))
    }
}
