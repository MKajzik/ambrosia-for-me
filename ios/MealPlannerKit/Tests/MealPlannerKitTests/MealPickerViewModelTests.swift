import API
import Foundation
import Persistence
import Repositories
import Testing
@testable import Features

@Suite
@MainActor
struct MealPickerViewModelTests {
    @MainActor
    struct Harness {
        let vm: MealPickerViewModel
        let cache: MealCache

        init(_ route: @escaping RoutingTransport.Route) throws {
            let transport = RoutingTransport(route)
            cache = CacheStore.makeMealCache(try CacheStore.inMemoryContainer())
            vm = MealPickerViewModel(meals: MealsRepository(client: makeAuthlessClient(transport: transport), cache: cache))
        }
    }

    private nonisolated static let meals = [Fixtures.summary(id: "a", name: "Oat porridge"), Fixtures.summary(id: "b", name: "Chicken rice"), Fixtures.summary(id: "c", name: "Greek salad")]

    @Test("Shows all of my meals, then filters locally, ignoring case and surrounding spaces")
    func filters() async throws {
        let h = try Harness { _ in (200, Fixtures.mealList(Self.meals)) }
        await h.vm.load()
        #expect(h.vm.filtered.map(\.name) == ["Chicken rice", "Greek salad", "Oat porridge"])
        h.vm.query = "  RICE "
        #expect(h.vm.filtered.map(\.name) == ["Chicken rice"])
        h.vm.query = "zzz"
        #expect(h.vm.filtered.isEmpty)
        h.vm.query = "   "
        #expect(h.vm.filtered.count == 3)
    }

    @Test("Offline with a cached library still lists it, with no error")
    func offlineKeepsCache() async throws {
        let h = try Harness { _ in throw URLError(.notConnectedToInternet) }
        await h.cache.replaceSummaries(Self.meals, scope: .mine)
        await h.vm.load()
        #expect(h.vm.meals.count == 3)
        #expect(h.vm.errorMessage == nil)
    }

    @Test("Offline with nothing cached shows an error, not an empty-library message")
    func offlineWithNothingCached() async throws {
        let h = try Harness { _ in throw URLError(.notConnectedToInternet) }
        await h.vm.load()
        #expect(h.vm.errorMessage == "Can't reach the server. Check your connection and try again.")
        #expect(h.vm.isLibraryEmpty == false)
    }

    @Test("An empty library is reported as such")
    func emptyLibrary() async throws {
        let h = try Harness { _ in (200, Fixtures.mealList([])) }
        await h.vm.load()
        #expect(h.vm.isLibraryEmpty)
    }

    @Test("Only my own meals are listed: the partner's endpoint is never called")
    func onlyMine() async throws {
        let transport = Locked<[String]>([])
        let h = try Harness { call in
            transport.mutate { $0.append(call.route) }
            return (200, Fixtures.mealList([]))
        }
        await h.vm.load()
        #expect(transport.value == ["GET /meals"])
    }
}
