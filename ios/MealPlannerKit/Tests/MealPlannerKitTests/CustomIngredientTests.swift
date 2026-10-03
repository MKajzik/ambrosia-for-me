import API
import Foundation
import Repositories
import Testing
@testable import Features

@Suite
@MainActor
struct CustomIngredientTests {
    private func form(_ configure: (inout CustomIngredientForm) -> Void) -> CustomIngredientForm.Validation {
        var form = CustomIngredientForm()
        form.name = "Jam"
        configure(&form)
        return form.validate()
    }

    private func request(_ validation: CustomIngredientForm.Validation) -> Components.Schemas.CreateIngredientRequest? {
        if case .valid(let request) = validation { request } else { nil }
    }

    private func errors(_ validation: CustomIngredientForm.Validation) -> [CustomIngredientForm.Field: String]? {
        if case .invalid(let errors) = validation { errors } else { nil }
    }

    @Test("A name is required and at most 200 characters")
    func name() {
        #expect(errors(form { $0.name = "  " })?[.name] == "Give the ingredient a name.")
        #expect(errors(form { $0.name = String(repeating: "a", count: 201) })?[.name] == "Use at most 200 characters.")
        #expect(request(form { _ in })?.name == "Jam")
    }

    @Test("Only the nutrients actually entered are sent: unknown is not zero")
    func onlyEnteredNutrients() {
        let none = request(form { _ in })
        #expect(none?.nutrients == nil)
        let some = request(form { $0.calories = "52"; $0.fat = "0,3" })
        #expect(some?.nutrients?.calories == 52)
        #expect(some?.nutrients?.fat == 0.3)
        #expect(some?.nutrients?.protein == nil)
        let zero = request(form { $0.protein = "0" })
        #expect(zero?.nutrients?.protein == 0)
    }

    @Test("Nutrient limits per 100 g, and non-numbers get the number hint")
    func nutrientLimits() {
        #expect(errors(form { $0.calories = "1001" })?[.calories] == "That is more than 1000 per 100 g.")
        #expect(request(form { $0.calories = "1000" }) != nil)
        #expect(errors(form { $0.protein = "100.1" })?[.protein] == "That is more than 100 per 100 g.")
        #expect(errors(form { $0.carbohydrates = "abc" })?[.carbohydrates] == "Enter a number, for example 12.5.")
        #expect(errors(form { $0.fat = "-1" })?[.fat] == "Enter a number, for example 12.5.")
    }

    @Test("Weight per piece is more than 0 and at most 10000; density more than 0 and at most 3")
    func conversions() {
        #expect(errors(form { $0.gramsPerPiece = "0" })?[.gramsPerPiece] == "Weight per piece must be more than 0 and at most 10000.")
        #expect(errors(form { $0.gramsPerPiece = "10001" })?[.gramsPerPiece] != nil)
        #expect(errors(form { $0.density = "3.1" })?[.density] == "Density must be more than 0 and at most 3.")
        let ok = request(form { $0.gramsPerPiece = "55"; $0.density = "1,2" })
        #expect(ok?.gramsPerPiece == 55)
        #expect(ok?.densityGPerMl == 1.2)
    }

    @Test("Category defaults to Other and is sent")
    func category() {
        #expect(request(form { _ in })?.category == .other)
        #expect(request(form { $0.category = .produce })?.category == .produce)
    }

    @Test("A server field path maps to the form field, ignoring the nutrients prefix")
    func apiPaths() {
        #expect(CustomIngredientForm.Field(apiPath: "nutrients.calories") == .calories)
        #expect(CustomIngredientForm.Field(apiPath: "grams_per_piece") == .gramsPerPiece)
        #expect(CustomIngredientForm.Field(apiPath: "density_g_per_ml") == .density)
        #expect(CustomIngredientForm.Field(apiPath: "name") == .name)
        #expect(CustomIngredientForm.Field(apiPath: "category") == nil)
    }

    private func viewModel(_ route: @escaping RoutingTransport.Route) -> (CustomIngredientViewModel, RoutingTransport) {
        let transport = RoutingTransport(route)
        let repository = IngredientsRepository(client: makeAuthlessClient(transport: transport))
        return (CustomIngredientViewModel(name: "Jam", repository: repository), transport)
    }

    @Test("The name is prefilled from the search text")
    func prefilled() {
        let (vm, _) = viewModel { _ in (500, "") }
        #expect(vm.form.name == "Jam")
    }

    @Test("Local validation errors never reach the network")
    func localValidation() async {
        let (vm, transport) = viewModel { _ in (500, "") }
        vm.form.calories = "abc"
        #expect(await vm.save() == nil)
        #expect(vm.fieldErrors[.calories] == "Enter a number, for example 12.5.")
        #expect(await transport.calls.isEmpty)
    }

    @Test("Saving returns the new ingredient")
    func success() async {
        let (vm, transport) = viewModel { _ in (201, Fixtures.json(Fixtures.ingredient(id: "new", name: "Jam", isCustom: true))) }
        vm.form.calories = "250"
        let made = await vm.save()
        #expect(made?.id == "new")
        #expect(vm.fieldErrors.isEmpty)
        #expect(vm.bannerError == nil)
        #expect(await transport.calls("POST /ingredients").count == 1)
    }

    @Test("A 400 naming a field shows inline on that field; one naming none shows as a banner")
    func serverValidation() async {
        let named = viewModel { _ in
            (400, Fixtures.problem(400, code: "validation_failed", errors: [("nutrients.calories", "invalid_value")]))
        }
        #expect(await named.0.save() == nil)
        #expect(named.0.fieldErrors[.calories] == "Nutrients calories is invalid.")
        #expect(named.0.bannerError == nil)

        let unnamed = viewModel { _ in (400, Fixtures.problem(400, code: "validation_failed", title: "Bad request")) }
        #expect(await unnamed.0.save() == nil)
        #expect(unnamed.0.fieldErrors.isEmpty)
        #expect(unnamed.0.bannerError == "Bad request")
    }

    @Test("A transport failure is a banner")
    func offline() async {
        let (vm, _) = viewModel { _ in throw URLError(.notConnectedToInternet) }
        #expect(await vm.save() == nil)
        #expect(vm.bannerError == "Can't reach the server. Check your connection and try again.")
        #expect(vm.isSaving == false)
    }
}
