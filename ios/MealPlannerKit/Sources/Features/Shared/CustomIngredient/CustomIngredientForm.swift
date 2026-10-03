import API
import Foundation

/// The custom-ingredient creation form's text and validation, ported from web's `custom-ingredient.ts`.
/// Only the four macros are asked for here; the full 18-nutrient editor belongs to the Profile plan.
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

    public var name = ""
    public var category: Components.Schemas.IngredientCategory = .other
    public var calories = ""
    public var protein = ""
    public var carbohydrates = ""
    public var fat = ""
    public var gramsPerPiece = ""
    public var density = ""

    public init(name: String = "") { self.name = name }

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
