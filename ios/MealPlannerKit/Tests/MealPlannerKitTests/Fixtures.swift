import API
import Foundation

/// Builders for generated value types, and JSON for stub responses. Encoding the generated types
/// (rather than hand-writing JSON) keeps the stubs in step with the contract.
enum Fixtures {
    static let date = Date(timeIntervalSince1970: 1_700_000_000)

    static func nutrients(
        calories: Double? = nil, protein: Double? = nil, carbohydrates: Double? = nil, fat: Double? = nil
    ) -> Components.Schemas.NutrientAmounts {
        .init(calories: calories, protein: protein, carbohydrates: carbohydrates, fat: fat)
    }

    static func line(
        id: String = "line-1", ingredientID: String = "ing-1", name: String = "Rice",
        category: Components.Schemas.IngredientCategory = .grainsBread,
        quantity: Double = 100, unit: Components.Schemas.Unit = .g, position: Int = 0
    ) -> Components.Schemas.MealIngredient {
        .init(id: id, ingredientId: ingredientID, ingredientName: name, ingredientCategory: category,
              quantity: quantity, unit: unit, position: position)
    }

    static func meal(
        id: String = "meal-1", name: String = "Pasta", notes: String? = nil, servings: Double = 2,
        shared: Bool = false, isOwner: Bool = true, lines: [Components.Schemas.MealIngredient] = [],
        nutrition: Components.Schemas.NutrientAmounts = nutrients(calories: 400)
    ) -> Components.Schemas.Meal {
        .init(id: id, name: name, notes: notes, servings: servings, sharedWithPartner: shared, isOwner: isOwner,
              ingredients: lines, nutritionPerServing: nutrition, createdAt: date, updatedAt: date)
    }

    static func summary(id: String = "meal-1", name: String = "Pasta", servings: Double = 2, shared: Bool = false)
        -> Components.Schemas.MealSummary
    {
        .init(id: id, name: name, notes: nil, servings: servings, sharedWithPartner: shared, createdAt: date, updatedAt: date)
    }

    static func ingredient(
        id: String = "ing-1", name: String = "Rice", category: Components.Schemas.IngredientCategory = .grainsBread,
        isCustom: Bool = false, calories: Double? = 130
    ) -> Components.Schemas.Ingredient {
        .init(id: id, name: name, category: category, isCustom: isCustom, nutrients: nutrients(calories: calories),
              createdAt: date, updatedAt: date)
    }

    static func json<T: Encodable>(_ value: T) -> String {
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .custom { date, encoder in
            let formatter = ISO8601DateFormatter()
            formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
            var container = encoder.singleValueContainer()
            try container.encode(formatter.string(from: date))
        }
        return String(decoding: try! encoder.encode(value), as: UTF8.self)
    }

    static func mealList(_ items: [Components.Schemas.MealSummary], next: String? = nil) -> String {
        json(Components.Schemas.MealList(items: items, nextCursor: next))
    }

    static func ingredientList(_ items: [Components.Schemas.Ingredient]) -> String {
        json(Components.Schemas.IngredientList(items: items, nextCursor: nil))
    }

    /// An RFC 9457 problem body, as the API sends for every error.
    static func problem(_ status: Int, code: String, title: String = "Problem", errors: [(field: String, code: String)] = []) -> String {
        var body = #"{"type":"about:blank","title":"\#(title)","status":\#(status),"code":"\#(code)""#
        if !errors.isEmpty {
            let items = errors.map { "{\"field\":\"\($0.field)\",\"code\":\"\($0.code)\"}" }.joined(separator: ",")
            body += #","errors":[\#(items)]"#
        }
        return body + "}"
    }

    static let userJSON = """
    {"id":"11111111-1111-1111-1111-111111111111","email":"person@example.com","display_name":"Person",
     "created_at":"2026-01-01T00:00:00.000000Z","updated_at":"2026-01-01T00:00:00.000000Z"}
    """
}
