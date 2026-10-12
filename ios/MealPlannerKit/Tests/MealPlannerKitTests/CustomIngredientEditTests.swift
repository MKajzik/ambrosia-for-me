import API
import Repositories
import Testing
@testable import Features

@Suite
struct CustomIngredientEditTests {
    private func update(_ form: CustomIngredientForm, existing: Components.Schemas.Ingredient = ProfileFixtures.fullIngredient()) -> IngredientUpdate? {
        if case .valid(let update) = form.validateUpdate(preserving: existing) { update } else { nil }
    }

    @Test("The edit form is prefilled from the ingredient")
    func prefill() {
        let form = CustomIngredientForm(editing: ProfileFixtures.fullIngredient())
        #expect(form.name == "Apple")
        #expect(form.category == .produce)
        #expect(form.calories == "52")
        #expect(form.protein == "0.3")
        #expect(form.carbohydrates == "14")
        #expect(form.fat == "0.2")
        #expect(form.gramsPerPiece == "182")
        #expect(form.density == "")
    }

    @Test("Editing the name alone sends all 18 nutrients: the 14 the form does not show are copied from the ingredient")
    func keepsHiddenNutrients() {
        var form = CustomIngredientForm(editing: ProfileFixtures.fullIngredient())
        form.name = "Apple, raw"
        let result = update(form)
        #expect(result?.name == "Apple, raw")
        #expect(result?.nutrients == ProfileFixtures.fullInput())
    }

    @Test("The four shown nutrients come from the form; a blank one is left out (unknown), the hidden ones stay")
    func shownNutrients() {
        var form = CustomIngredientForm(editing: ProfileFixtures.fullIngredient())
        form.calories = "60"
        form.protein = ""
        let nutrients = update(form)?.nutrients
        #expect(nutrients?.calories == 60)
        #expect(nutrients?.protein == nil)
        #expect(nutrients?.carbohydrates == 14)
        #expect(nutrients?.vitaminC == 4.6)
        #expect(nutrients?.folate == 3)
    }

    @Test("A blank weight per piece or density is nil, which clears it; a value is kept")
    func conversions() {
        var form = CustomIngredientForm(editing: ProfileFixtures.fullIngredient())
        form.gramsPerPiece = ""
        form.density = "1,2"
        let result = update(form)
        #expect(result?.gramsPerPiece == nil)
        #expect(result?.densityGPerMl == 1.2)
    }

    @Test("A category change is carried")
    func category() {
        var form = CustomIngredientForm(editing: ProfileFixtures.fullIngredient())
        form.category = .beverages
        #expect(update(form)?.category == .beverages)
    }

    @Test("Invalid input is reported against its field and builds no update")
    func invalid() {
        var form = CustomIngredientForm(editing: ProfileFixtures.fullIngredient())
        form.name = "  "
        form.calories = "abc"
        guard case .invalid(let errors) = form.validateUpdate(preserving: ProfileFixtures.fullIngredient()) else {
            Issue.record("expected invalid")
            return
        }
        #expect(errors[.name] == "Give the ingredient a name.")
        #expect(errors[.calories] == "Enter a number, for example 12.5.")
    }
}
