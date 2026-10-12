import API
import Repositories

/// The Daily targets form's text and validation, ported from web's `targets.ts`.
public struct TargetsDraft: Equatable, Sendable {
    public enum Field: Hashable, Sendable {
        case calories, protein, carbs, fat

        /// A server error path (`target_kcal`, ...) to the form field it names.
        public init?(apiPath: String) {
            switch apiPath {
            case "target_kcal": self = .calories
            case "target_protein_g": self = .protein
            case "target_carbs_g": self = .carbs
            case "target_fat_g": self = .fat
            default: return nil
            }
        }
    }

    public enum Validation: Equatable, Sendable {
        case valid(TargetsUpdate)
        case invalid([Field: String])
    }

    public var calories = ""
    public var protein = ""
    public var carbs = ""
    public var fat = ""

    public init() {}

    /// The text for a user's saved targets: plain numbers, blank where no target is set.
    public init(user: Components.Schemas.User) {
        calories = user.targetKcal.map(plainNumber) ?? ""
        protein = user.targetProteinG.map(plainNumber) ?? ""
        carbs = user.targetCarbsG.map(plainNumber) ?? ""
        fat = user.targetFatG.map(plainNumber) ?? ""
    }

    private struct Rule {
        let max: Double
        let positive: Bool
        let over: String
        let example: String
    }

    // The API's limits (`UpdateProfileRequest`): calories above 0; macros from 0.
    private static func rule(for field: Field) -> Rule {
        switch field {
        case .calories: Rule(max: 20000, positive: true, over: "Calories must be more than 0 and at most 20000.", example: "2000")
        case .protein: Rule(max: 2000, positive: false, over: "Protein must be at most 2000 g.", example: "70")
        case .carbs: Rule(max: 5000, positive: false, over: "Carbohydrates must be at most 5000 g.", example: "250")
        case .fat: Rule(max: 2000, positive: false, over: "Fat must be at most 2000 g.", example: "70")
        }
    }

    public func validate() -> Validation {
        var errors: [Field: String] = [:]

        func read(_ field: Field, _ text: String) -> Double? {
            let rule = Self.rule(for: field)
            switch parseDecimal(text) {
            case .blank:
                return nil
            case .invalid:
                errors[field] = "Enter a number, for example \(rule.example)."
                return nil
            case .value(let value):
                if (rule.positive && value <= 0) || value > rule.max {
                    errors[field] = rule.over
                    return nil
                }
                return value
            }
        }

        let update = TargetsUpdate(
            kcal: read(.calories, calories), protein: read(.protein, protein),
            carbs: read(.carbs, carbs), fat: read(.fat, fat)
        )
        return errors.isEmpty ? .valid(update) : .invalid(errors)
    }
}
