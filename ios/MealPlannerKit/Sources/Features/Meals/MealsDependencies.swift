import Repositories

/// What the Meals tab needs, built once in `RootView` and handed down.
public struct MealsDependencies: Sendable {
    public let meals: MealsRepository
    public let ingredients: IngredientsRepository
    public let partner: PartnerRepository

    public init(meals: MealsRepository, ingredients: IngredientsRepository, partner: PartnerRepository) {
        self.meals = meals
        self.ingredients = ingredients
        self.partner = partner
    }
}
