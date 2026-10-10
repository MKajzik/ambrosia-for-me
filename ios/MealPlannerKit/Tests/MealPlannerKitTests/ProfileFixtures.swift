import API
import Foundation

enum ProfileFixtures {
    static func user(
        id: String = "u1", email: String = "sam@example.com", name: String = "Sam",
        kcal: Double? = nil, protein: Double? = nil, carbs: Double? = nil, fat: Double? = nil
    ) -> Components.Schemas.User {
        .init(
            id: id, email: email, displayName: name, targetKcal: kcal, targetProteinG: protein, targetCarbsG: carbs,
            targetFatG: fat, createdAt: Fixtures.date, updatedAt: Fixtures.date
        )
    }

    static func active(_ name: String = "Alex") -> Components.Schemas.Partnership {
        .init(status: .active, displayName: name, linkedAt: Fixtures.date)
    }

    static func pending() -> Components.Schemas.Partnership {
        .init(status: .pending, expiresAt: Fixtures.date)
    }

    static func invite(code: String = "ABCD2345") -> Components.Schemas.PartnerInvite {
        .init(code: code, expiresAt: Fixtures.date)
    }

    /// All 18 nutrients known, so a test can see which ones an edit kept.
    static func fullNutrients() -> Components.Schemas.NutrientAmounts {
        .init(
            calories: 52, protein: 0.3, carbohydrates: 14, sugar: 10, fibre: 2.4, fat: 0.2, saturatedFat: 0.03,
            sodium: 1, potassium: 107, calcium: 6, iron: 0.1, magnesium: 5, zinc: 0.04, vitaminA: 3, vitaminC: 4.6,
            vitaminD: 0, vitaminB12: 0, folate: 3
        )
    }

    /// The same 18 values as the request type.
    static func fullInput() -> Components.Schemas.NutrientAmountsInput {
        .init(
            calories: 52, protein: 0.3, carbohydrates: 14, sugar: 10, fibre: 2.4, fat: 0.2, saturatedFat: 0.03,
            sodium: 1, potassium: 107, calcium: 6, iron: 0.1, magnesium: 5, zinc: 0.04, vitaminA: 3, vitaminC: 4.6,
            vitaminD: 0, vitaminB12: 0, folate: 3
        )
    }

    static func customIngredient(
        id: String = "ing-1", name: String = "Apple", category: Components.Schemas.IngredientCategory = .produce,
        gramsPerPiece: Double? = nil, density: Double? = nil,
        nutrients: Components.Schemas.NutrientAmounts = fullNutrients()
    ) -> Components.Schemas.Ingredient {
        .init(
            id: id, name: name, category: category, isCustom: true, gramsPerPiece: gramsPerPiece, densityGPerMl: density,
            nutrients: nutrients, createdAt: Fixtures.date, updatedAt: Fixtures.date
        )
    }

    /// A custom ingredient with all 18 nutrients and a weight per piece.
    static func fullIngredient() -> Components.Schemas.Ingredient {
        customIngredient(gramsPerPiece: 182)
    }

    static func ingredientPage(_ items: [Components.Schemas.Ingredient], next: String? = nil) -> String {
        Fixtures.json(Components.Schemas.IngredientList(items: items, nextCursor: next))
    }
}
