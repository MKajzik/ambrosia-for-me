import API

/// An edited custom ingredient as the repository sends it. `gramsPerPiece` and `densityGPerMl` are always sent: `nil`
/// clears them. `nutrients` is the whole set (the API replaces it): a nutrient left out is unknown.
public struct IngredientUpdate: Equatable, Sendable {
    public var name: String
    public var category: Components.Schemas.IngredientCategory
    public var gramsPerPiece: Double?
    public var densityGPerMl: Double?
    public var nutrients: Components.Schemas.NutrientAmountsInput

    public init(
        name: String, category: Components.Schemas.IngredientCategory, gramsPerPiece: Double?, densityGPerMl: Double?,
        nutrients: Components.Schemas.NutrientAmountsInput
    ) {
        self.name = name
        self.category = category
        self.gramsPerPiece = gramsPerPiece
        self.densityGPerMl = densityGPerMl
        self.nutrients = nutrients
    }
}
