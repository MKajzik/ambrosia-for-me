import API
import Foundation
import Testing
@testable import Features
@testable import Repositories

@MainActor
@Suite
struct MyIngredientsViewModelTests {
    private func make(_ route: @escaping RoutingTransport.Route) -> (MyIngredientsViewModel, RoutingTransport) {
        let transport = RoutingTransport(route)
        return (MyIngredientsViewModel(repository: IngredientsRepository(client: makeAuthlessClient(transport: transport))), transport)
    }

    private nonisolated static func page() -> String {
        ProfileFixtures.ingredientPage([
            ProfileFixtures.customIngredient(id: "b", name: "banana bread mix"),
            Fixtures.ingredient(id: "g", name: "Rice", isCustom: false),
            ProfileFixtures.customIngredient(id: "a", name: "Apple jam"),
        ])
    }

    @Test("load keeps only my custom ingredients, sorted by name ignoring case")
    func load() async throws {
        let (vm, _) = make { _ in (200, Self.page()) }
        await vm.load()
        #expect(vm.all.map(\.id) == ["a", "b"])
        #expect(vm.loadError == nil)
        #expect(!vm.isLoading)
    }

    @Test("Search filters locally and ignores case; blank shows everything")
    func search() async throws {
        let (vm, transport) = make { _ in (200, Self.page()) }
        await vm.load()
        vm.searchText = "JAM"
        #expect(vm.shown.map(\.id) == ["a"])
        vm.searchText = "  "
        #expect(vm.shown.count == 2)
        vm.searchText = "zzz"
        #expect(vm.shown.isEmpty)
        #expect(await transport.calls("GET /ingredients").count == 1) // filtering made no request
    }

    @Test("A failed load is an error with nothing shown, and keeps the list it had when a later load fails")
    func loadFailure() async throws {
        let fail = Locked(true)
        let (vm, _) = make { _ in fail.value ? (500, Fixtures.problem(500, code: "internal")) : (200, Self.page()) }
        await vm.load()
        #expect(vm.all.isEmpty)
        #expect(vm.loadError == "The server had a problem loading ingredients.")
        fail.set(false)
        await vm.load()
        #expect(vm.all.count == 2)
        #expect(vm.loadError == nil)
        fail.set(true)
        await vm.load()
        #expect(vm.all.count == 2)
    }

    @Test("didSave replaces an edited row and inserts a new one, keeping the order")
    func didSave() async throws {
        let (vm, _) = make { _ in (200, Self.page()) }
        await vm.load()
        vm.didSave(ProfileFixtures.customIngredient(id: "a", name: "Zucchini jam"))
        vm.didSave(ProfileFixtures.customIngredient(id: "c", name: "Cherry jam"))
        #expect(vm.all.map(\.name) == ["banana bread mix", "Cherry jam", "Zucchini jam"])
    }

    @Test("delete removes the row; one already gone counts as deleted; one a meal uses stays with a message")
    func delete() async throws {
        let mode = Locked(204)
        let (vm, _) = make { call in
            if call.method == "GET" { return (200, Self.page()) }
            switch mode.value {
            case 204: return (204, "")
            case 404: return (404, Fixtures.problem(404, code: "not_found"))
            default: return (409, Fixtures.problem(409, code: "ingredient_in_use"))
            }
        }
        await vm.load()
        let apple = try #require(vm.all.first { $0.id == "a" })
        let banana = try #require(vm.all.first { $0.id == "b" })
        #expect(await vm.delete(apple) == nil)
        #expect(vm.all.map(\.id) == ["b"])

        mode.set(409)
        #expect(await vm.delete(banana) == "A meal still uses this ingredient. Remove it from those meals first.")
        #expect(vm.all.map(\.id) == ["b"])

        mode.set(404)
        #expect(await vm.delete(banana) == nil)
        #expect(vm.all.isEmpty)
    }
}
