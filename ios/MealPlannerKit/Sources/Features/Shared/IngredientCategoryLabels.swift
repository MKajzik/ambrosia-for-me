import API

public extension Components.Schemas.IngredientCategory {
    /// The words a person would use, as web's `CATEGORY_LABELS`. `allCases` is in the API's order.
    var label: String {
        switch self {
        case .produce: "Produce"
        case .dairyEggs: "Dairy and eggs"
        case .meatSeafood: "Meat and seafood"
        case .grainsBread: "Grains and bread"
        case .legumesNutsSeeds: "Legumes, nuts and seeds"
        case .condimentsOils: "Condiments and oils"
        case .spicesHerbs: "Spices and herbs"
        case .beverages: "Beverages"
        case .sweetsSnacks: "Sweets and snacks"
        case .other: "Other"
        }
    }
}
