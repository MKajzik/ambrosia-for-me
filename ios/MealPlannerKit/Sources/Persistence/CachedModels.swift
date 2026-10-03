import API
import Foundation
import SwiftData

/// Internal to this module: `@Model` objects never leave `MealCache`.
@Model
final class CachedMeal {
    @Attribute(.unique) var id: String
    var scopeRaw: String
    var name: String
    var notes: String?
    var servings: Double
    var sharedWithPartner: Bool
    var createdAt: Date
    var updatedAt: Date
    /// `nil` means "summary only, not opened yet": a list row has no ownership flag, lines or nutrition.
    var isOwner: Bool?
    /// The generated `NutrientAmounts`, JSON-encoded with its own `Codable`, rather than 18 columns.
    var nutritionJSON: Data?
    @Relationship(deleteRule: .cascade, inverse: \CachedMealIngredient.meal)
    var ingredients: [CachedMealIngredient] = []

    init(summary: Components.Schemas.MealSummary, scopeRaw: String) {
        self.id = summary.id
        self.scopeRaw = scopeRaw
        self.name = summary.name
        self.notes = summary.notes
        self.servings = summary.servings
        self.sharedWithPartner = summary.sharedWithPartner
        self.createdAt = summary.createdAt
        self.updatedAt = summary.updatedAt
    }

    func apply(_ summary: Components.Schemas.MealSummary) {
        name = summary.name
        notes = summary.notes
        servings = summary.servings
        sharedWithPartner = summary.sharedWithPartner
        createdAt = summary.createdAt
        updatedAt = summary.updatedAt
    }

    var summary: Components.Schemas.MealSummary {
        .init(id: id, name: name, notes: notes, servings: servings, sharedWithPartner: sharedWithPartner,
              createdAt: createdAt, updatedAt: updatedAt)
    }

    /// The full meal, or `nil` if it has not been opened (or its stored nutrition can no longer be decoded,
    /// in which case refetching is the right answer).
    var meal: Components.Schemas.Meal? {
        guard let isOwner, let nutritionJSON,
              let nutrition = try? JSONDecoder().decode(Components.Schemas.NutrientAmounts.self, from: nutritionJSON)
        else { return nil }
        let lines = ingredients.sorted { $0.position < $1.position }.compactMap(\.line)
        return .init(id: id, name: name, notes: notes, servings: servings, sharedWithPartner: sharedWithPartner,
                     isOwner: isOwner, ingredients: lines, nutritionPerServing: nutrition,
                     createdAt: createdAt, updatedAt: updatedAt)
    }

    func apply(_ meal: Components.Schemas.Meal) {
        apply(.init(id: meal.id, name: meal.name, notes: meal.notes, servings: meal.servings,
                    sharedWithPartner: meal.sharedWithPartner, createdAt: meal.createdAt, updatedAt: meal.updatedAt))
        isOwner = meal.isOwner
        nutritionJSON = try? JSONEncoder().encode(meal.nutritionPerServing)
    }
}

@Model
final class CachedMealIngredient {
    var id: String
    var ingredientID: String
    var ingredientName: String
    var categoryRaw: String
    var quantity: Double
    var unitRaw: String
    var position: Int
    var meal: CachedMeal?

    init(line: Components.Schemas.MealIngredient) {
        self.id = line.id
        self.ingredientID = line.ingredientId
        self.ingredientName = line.ingredientName
        self.categoryRaw = line.ingredientCategory.rawValue
        self.quantity = line.quantity
        self.unitRaw = line.unit.rawValue
        self.position = line.position
    }

    var line: Components.Schemas.MealIngredient? {
        guard let category = Components.Schemas.IngredientCategory(rawValue: categoryRaw),
              let unit = Components.Schemas.Unit(rawValue: unitRaw)
        else { return nil }
        return .init(id: id, ingredientId: ingredientID, ingredientName: ingredientName,
                     ingredientCategory: category, quantity: quantity, unit: unit, position: position)
    }
}
