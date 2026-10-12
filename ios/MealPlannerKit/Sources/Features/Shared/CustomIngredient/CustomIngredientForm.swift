import API
import Foundation
import Repositories

/// The custom-ingredient form's text and validation, ported from web's `custom-ingredient.ts`. It asks for the four
/// macros only; editing an existing ingredient keeps every other nutrient it has (see `validateUpdate(preserving:)`).
public struct CustomIngredientForm: Equatable, Sendable {
    public enum Field: Hashable, Sendable {
        case name, calories, protein, carbohydrates, fat, gramsPerPiece, density

        /// A server error path (`name`, `grams_per_piece`, `nutrients.calories`) to the form field it names.
        public init?(apiPath: String) {
            switch apiPath.replacingOccurrences(of: "nutrients.", with: "") {
            case "name": self = .name
            case "calories": self = .calories
            case "protein": self = .protein
            case "carbohydrates": self = .carbohydrates
            case "fat": self = .fat
            case "grams_per_piece": self = .gramsPerPiece
            case "density_g_per_ml": self = .density
            default: return nil
            }
        }
    }

    public enum Validation: Equatable, Sendable {
        case valid(Components.Schemas.CreateIngredientRequest)
        case invalid([Field: String])
    }

    public enum UpdateValidation: Equatable, Sendable {
        case valid(IngredientUpdate)
        case invalid([Field: String])
    }

    public var name = ""
    public var category: Components.Schemas.IngredientCategory = .other
    public var calories = ""
    public var protein = ""
    public var carbohydrates = ""
    public var fat = ""
    public var gramsPerPiece = ""
    public var density = ""

    public init(name: String = "") { self.name = name }

    /// The form for editing an existing ingredient: every field prefilled from it.
    public init(editing ingredient: Components.Schemas.Ingredient) {
        name = ingredient.name
        category = ingredient.category
        calories = ingredient.nutrients.calories.map(plainNumber) ?? ""
        protein = ingredient.nutrients.protein.map(plainNumber) ?? ""
        carbohydrates = ingredient.nutrients.carbohydrates.map(plainNumber) ?? ""
        fat = ingredient.nutrients.fat.map(plainNumber) ?? ""
        gramsPerPiece = ingredient.gramsPerPiece.map(plainNumber) ?? ""
        density = ingredient.densityGPerMl.map(plainNumber) ?? ""
    }

    private static let numberHelp = "Enter a number, for example 12.5."

    public func validate() -> Validation {
        var errors: [Field: String] = [:]

        let trimmed = name.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed.isEmpty {
            errors[.name] = "Give the ingredient a name."
        } else if trimmed.count > 200 {
            errors[.name] = "Use at most 200 characters."
        }

        // Only the nutrients actually entered are sent: an omitted one stays unknown, not zero.
        var nutrients = Components.Schemas.NutrientAmountsInput()
        var anyNutrient = false
        let entries: [(Field, String, Double)] = [
            (.calories, calories, 1000), (.protein, protein, 100), (.carbohydrates, carbohydrates, 100), (.fat, fat, 100),
        ]
        for (field, text, limit) in entries {
            switch parseDecimal(text) {
            case .blank:
                continue
            case .invalid:
                errors[field] = Self.numberHelp
            case .value(let value) where value > limit:
                errors[field] = "That is more than \(plainNumber(limit)) per 100 g."
            case .value(let value):
                anyNutrient = true
                switch field {
                case .calories: nutrients.calories = value
                case .protein: nutrients.protein = value
                case .carbohydrates: nutrients.carbohydrates = value
                default: nutrients.fat = value
                }
            }
        }

        let piece = Self.positive(gramsPerPiece, limit: 10_000, what: "Weight per piece")
        if let message = piece.error { errors[.gramsPerPiece] = message }
        let densityValue = Self.positive(density, limit: 3, what: "Density")
        if let message = densityValue.error { errors[.density] = message }

        guard errors.isEmpty else { return .invalid(errors) }
        return .valid(.init(
            name: trimmed, category: category,
            gramsPerPiece: piece.value, densityGPerMl: densityValue.value,
            nutrients: anyNutrient ? nutrients : nil
        ))
    }

    /// Validates like `validate()`, then builds the update for `existing`: `nutrients` replaces the whole set on the API,
    /// so all 18 are sent. The four shown here come from the form (a blank one is left out, meaning unknown); the other
    /// 14 are copied from `existing`, so editing a name never erases the vitamins. A blank weight per piece or density
    /// is `nil`, which clears it.
    public func validateUpdate(preserving existing: Components.Schemas.Ingredient) -> UpdateValidation {
        switch validate() {
        case .invalid(let errors):
            return .invalid(errors)
        case .valid(let request):
            let shown = request.nutrients ?? Components.Schemas.NutrientAmountsInput()
            let kept = existing.nutrients
            let nutrients = Components.Schemas.NutrientAmountsInput(
                calories: shown.calories, protein: shown.protein, carbohydrates: shown.carbohydrates,
                sugar: kept.sugar, fibre: kept.fibre, fat: shown.fat, saturatedFat: kept.saturatedFat,
                sodium: kept.sodium, potassium: kept.potassium, calcium: kept.calcium, iron: kept.iron,
                magnesium: kept.magnesium, zinc: kept.zinc, vitaminA: kept.vitaminA, vitaminC: kept.vitaminC,
                vitaminD: kept.vitaminD, vitaminB12: kept.vitaminB12, folate: kept.folate
            )
            return .valid(IngredientUpdate(
                name: request.name, category: request.category, gramsPerPiece: request.gramsPerPiece,
                densityGPerMl: request.densityGPerMl, nutrients: nutrients
            ))
        }
    }

    private static func positive(_ text: String, limit: Double, what: String) -> (value: Double?, error: String?) {
        switch parseDecimal(text) {
        case .blank: (nil, nil)
        case .invalid: (nil, numberHelp)
        case .value(let value) where value <= 0 || value > limit:
            (nil, "\(what) must be more than 0 and at most \(plainNumber(limit)).")
        case .value(let value): (value, nil)
        }
    }
}
