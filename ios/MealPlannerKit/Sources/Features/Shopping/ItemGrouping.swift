import API
import Foundation

public struct ItemGroup: Equatable, Identifiable, Sendable {
    public let category: Components.Schemas.IngredientCategory
    public let items: [Components.Schemas.ShoppingItem]
    public var id: String { category.rawValue }
}

/// Aisle grouping and item text, as web's `items.ts`.
public enum ItemGrouping {
    /// The items by aisle in the API's category order. Within an aisle what is still to buy comes first.
    public static func groups(_ items: [Components.Schemas.ShoppingItem]) -> [ItemGroup] {
        Components.Schemas.IngredientCategory.allCases.compactMap { category in
            let inAisle = items.filter { $0.category == category }
            guard !inAisle.isEmpty else { return nil }
            let sorted = inAisle.sorted {
                if $0.checked != $1.checked { return !$0.checked }
                return $0.position < $1.position
            }
            return ItemGroup(category: category, items: sorted)
        }
    }

    public static func progress(_ items: [Components.Schemas.ShoppingItem]) -> (done: Int, total: Int) {
        (items.filter(\.checked).count, items.count)
    }

    /// "" for no quantity (never 0), "2 g", "1.5 ml", "1 piece", "3 pieces", "4". Fixed en-US.
    public static func quantityText(_ item: Components.Schemas.ShoppingItem) -> String {
        guard let quantity = item.quantity else { return "" }
        let amount = quantity.formatted(.number.precision(.fractionLength(0...2)).locale(Locale(identifier: "en_US")))
        guard let unit = item.unit else { return amount }
        if unit == .piece { return "\(amount) \(quantity == 1 ? "piece" : "pieces")" }
        return "\(amount) \(unit.rawValue)"
    }
}
