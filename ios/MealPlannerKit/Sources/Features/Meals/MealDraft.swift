import API
import Foundation

/// The editable text of a meal, with validation and the diff against what the server holds.
/// A port of web's `draft.ts`; every limit and message is the same.
public struct MealDraft: Equatable, Sendable {
    public typealias Unit = Components.Schemas.Unit
    public typealias Category = Components.Schemas.IngredientCategory

    public static let maxIngredients = 200
    public static let servingsMessage = "Servings must be more than 0 and at most 1000."

    /// One line of the meal as the person is editing it: `quantity` is the text they typed.
    public struct Row: Equatable, Identifiable, Sendable {
        public var id: String
        public var ingredientID: String
        public var name: String
        public var category: Category
        public var quantity: String
        public var unit: Unit
    }

    /// A draft that passed validation, in the shape the API takes. `notes` is `nil` when blank.
    public struct Valid: Equatable, Sendable {
        public var name: String
        public var notes: String?
        public var servings: Double
        public var shared: Bool
        public var items: [Components.Schemas.MealIngredientInput]
    }

    public struct Errors: Equatable, Sendable {
        public var name: String?
        public var notes: String?
        public var servings: String?
        public var ingredients: String?
        public var rows: [String: String] = [:]

        var isEmpty: Bool {
            name == nil && notes == nil && servings == nil && ingredients == nil && rows.isEmpty
        }
    }

    public enum Validation: Equatable, Sendable {
        case valid(Valid)
        case invalid(Errors)
    }

    /// The writes needed to bring the server from one `Valid` to another.
    public struct Changes: Equatable, Sendable {
        public var patch: Components.Schemas.UpdateMealRequest?
        public var items: [Components.Schemas.MealIngredientInput]?
        public var isEmpty: Bool { patch == nil && items == nil }
    }

    public var name = ""
    public var notes = ""
    public var servings = ""
    public var shared = false
    public var rows: [Row] = []

    public var canAddRow: Bool { rows.count < Self.maxIngredients }

    public init(name: String = "", notes: String = "", servings: String = "", shared: Bool = false, rows: [Row] = []) {
        self.name = name
        self.notes = notes
        self.servings = servings
        self.shared = shared
        self.rows = rows
    }

    public init(meal: Components.Schemas.Meal) {
        self.init(
            name: meal.name,
            notes: meal.notes ?? "",
            servings: plainNumber(meal.servings),
            shared: meal.sharedWithPartner,
            rows: meal.ingredients.sorted { $0.position < $1.position }.map {
                Row(id: $0.id, ingredientID: $0.ingredientId, name: $0.ingredientName,
                    category: $0.ingredientCategory, quantity: plainNumber($0.quantity), unit: $0.unit)
            }
        )
    }

    /// What the server holds, in the same shape validation produces, so the two can be compared.
    public static func saved(from meal: Components.Schemas.Meal) -> Valid {
        Valid(
            name: meal.name,
            notes: normalizedNotes(meal.notes ?? ""),
            servings: meal.servings,
            shared: meal.sharedWithPartner,
            items: meal.ingredients.sorted { $0.position < $1.position }.map {
                .init(ingredientId: $0.ingredientId, quantity: $0.quantity, unit: $0.unit)
            }
        )
    }

    /// A fresh line for an ingredient just picked: 100 g, so it is valid the moment it appears. A no-op at the cap.
    public mutating func addRow(for ingredient: Components.Schemas.Ingredient) {
        guard canAddRow else { return }
        rows.append(Row(id: UUID().uuidString, ingredientID: ingredient.id, name: ingredient.name,
                        category: ingredient.category, quantity: "100", unit: .g))
    }

    public static func nameError(_ raw: String) -> String? {
        let name = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        if name.isEmpty { return "Give the meal a name." }
        if name.count > 200 { return "Use at most 200 characters." }
        return nil
    }

    public static func servingsValue(_ raw: String) -> Double? {
        guard case .value(let value) = parseDecimal(raw), value > 0, value <= 1000 else { return nil }
        return value
    }

    public func validate() -> Validation {
        var errors = Errors()
        errors.name = Self.nameError(name)
        if notes.count > 2000 { errors.notes = "Use at most 2000 characters." }
        let servingsValue = Self.servingsValue(servings)
        if servingsValue == nil { errors.servings = Self.servingsMessage }
        if rows.count > Self.maxIngredients {
            errors.ingredients = "A meal can have at most \(Self.maxIngredients) ingredients."
        }

        var items: [Components.Schemas.MealIngredientInput] = []
        for row in rows {
            switch parseDecimal(row.quantity) {
            case .value(let quantity) where quantity > 0 && quantity <= 100_000:
                items.append(.init(ingredientId: row.ingredientID, quantity: quantity, unit: row.unit))
            case .value:
                errors.rows[row.id] = "The amount must be more than 0 and at most 100000."
            case .blank, .invalid:
                errors.rows[row.id] = "Enter an amount."
            }
        }

        guard errors.isEmpty, let servingsValue else { return .invalid(errors) }
        return .valid(Valid(
            name: name.trimmingCharacters(in: .whitespacesAndNewlines),
            notes: Self.normalizedNotes(notes),
            servings: servingsValue,
            shared: shared,
            items: items
        ))
    }

    /// A patch of changed fields, and the whole ingredient list when any line changed.
    /// Clearing notes sends `""`: the generated request type cannot encode an explicit `null`
    /// (see the plan's decision 1), and `saved(from:)` reads `""` back as no notes.
    public static func changes(from saved: Valid, to next: Valid) -> Changes {
        var patch = Components.Schemas.UpdateMealRequest()
        var changed = false
        if next.name != saved.name { patch.name = next.name; changed = true }
        if next.notes != saved.notes { patch.notes = next.notes ?? ""; changed = true }
        if next.servings != saved.servings { patch.servings = next.servings; changed = true }
        if next.shared != saved.shared { patch.sharedWithPartner = next.shared; changed = true }
        return Changes(patch: changed ? patch : nil, items: next.items == saved.items ? nil : next.items)
    }

    private static func normalizedNotes(_ raw: String) -> String? {
        let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.isEmpty ? nil : trimmed
    }
}
