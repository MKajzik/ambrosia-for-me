import API
import Foundation
import Testing
@testable import Features
@testable import Repositories

@MainActor
@Suite
struct CustomIngredientEditViewModelTests {
    private func make(_ route: @escaping RoutingTransport.Route) -> (IngredientsRepository, RoutingTransport) {
        let transport = RoutingTransport(route)
        return (IngredientsRepository(client: makeClient(transport: transport, middlewares: [NullSentinelMiddleware()])), transport)
    }

    @Test("Review focus 2: saving a renamed ingredient sends all 18 nutrients and clears a blank weight per piece")
    func rename() async throws {
        let (repo, transport) = make { _ in (200, Fixtures.json(ProfileFixtures.customIngredient(name: "Apple, raw"))) }
        let vm = CustomIngredientViewModel(editing: ProfileFixtures.fullIngredient(), repository: repo)
        vm.form.name = "Apple, raw"
        vm.form.gramsPerPiece = ""
        let saved = await vm.save()
        #expect(saved?.name == "Apple, raw")
        let body = try #require(await transport.calls("PATCH /ingredients/ing-1").first).body
        #expect(body.contains("\"name\":\"Apple, raw\""))
        #expect(body.contains("\"grams_per_piece\":null"))
        #expect(body.contains("\"calories\":52"))
        #expect(body.contains("\"sodium\":1"))
        #expect(body.contains("\"vitamin_a\":3"))
        #expect(body.contains("\"iron\":0.1"))
        #expect(await transport.calls("POST /ingredients").isEmpty)
    }

    @Test("Invalid input shows on the field and sends nothing")
    func invalid() async throws {
        let (repo, transport) = make { _ in (500, "") }
        let vm = CustomIngredientViewModel(editing: ProfileFixtures.fullIngredient(), repository: repo)
        vm.form.name = " "
        #expect(await vm.save() == nil)
        #expect(vm.fieldErrors[.name] == "Give the ingredient a name.")
        #expect(await transport.calls.isEmpty)
    }

    @Test("A server refusal that names a field shows inline; a meal still needing the conversion is a banner")
    func serverRefusals() async throws {
        let mode = Locked("field")
        let (repo, _) = make { _ in
            mode.value == "field"
                ? (400, Fixtures.problem(400, code: "validation_failed", errors: [("grams_per_piece", "invalid_value")]))
                : (409, Fixtures.problem(409, code: "unit_not_convertible"))
        }
        let vm = CustomIngredientViewModel(editing: ProfileFixtures.fullIngredient(), repository: repo)
        #expect(await vm.save() == nil)
        #expect(vm.fieldErrors[.gramsPerPiece] == "Grams per piece is invalid.")
        #expect(vm.bannerError == nil)

        mode.set("unit")
        #expect(await vm.save() == nil)
        #expect(vm.fieldErrors.isEmpty)
        #expect(vm.bannerError == "A meal uses this ingredient by piece or by volume, so its weight per piece or density can't be cleared.")
    }

    @Test("Creating still posts a new ingredient")
    func createStillWorks() async throws {
        let (repo, transport) = make { _ in (201, Fixtures.json(ProfileFixtures.customIngredient(name: "Jam"))) }
        let vm = CustomIngredientViewModel(name: "Jam", repository: repo)
        let created = await vm.save()
        #expect(created?.name == "Jam")
        #expect(await transport.calls("POST /ingredients").count == 1)
        #expect(await transport.calls("PATCH /ingredients/ing-1").isEmpty)
    }
}
