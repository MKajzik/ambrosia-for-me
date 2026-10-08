import API
import Foundation

enum ShoppingFixtures {
    static func item(
        id: String = "i1", listID: String = "l1", name: String = "Milk",
        category: Components.Schemas.IngredientCategory = .dairyEggs, checked: Bool = false, checkedBy: String? = nil,
        version: Int = 1, position: Int = 0, quantity: Double? = nil,
        unit: Components.Schemas.ShoppingItem.UnitPayload? = nil
    ) -> Components.Schemas.ShoppingItem {
        .init(
            id: id, listId: listID, name: name, quantity: quantity, unit: unit, category: category, checked: checked,
            checkedBy: checkedBy, position: position, version: version, origin: .manual,
            createdAt: Fixtures.date, updatedAt: Fixtures.date
        )
    }

    static func list(
        id: String = "l1", name: String = "Week", items: [Components.Schemas.ShoppingItem] = [],
        isOwner: Bool = true, shared: Bool = false, from: String? = nil, to: String? = nil
    ) -> Components.Schemas.ShoppingList {
        .init(
            id: id, name: name, sharedWithPartner: shared, isOwner: isOwner, sourceFrom: from, sourceTo: to,
            items: items, createdAt: Fixtures.date, updatedAt: Fixtures.date
        )
    }

    static func summary(_ list: Components.Schemas.ShoppingList) -> Components.Schemas.ShoppingListSummary {
        .init(
            id: list.id, name: list.name, sharedWithPartner: list.sharedWithPartner, sourceFrom: list.sourceFrom,
            sourceTo: list.sourceTo, createdAt: list.createdAt, updatedAt: list.updatedAt
        )
    }

    static func summary(id: String = "l1", name: String = "Week", shared: Bool = false) -> Components.Schemas.ShoppingListSummary {
        summary(list(id: id, name: name, shared: shared))
    }

    static func page(_ items: [Components.Schemas.ShoppingListSummary], next: String? = nil) -> String {
        Fixtures.json(Components.Schemas.ShoppingListPage(items: items, nextCursor: next))
    }

    /// A `409 version_conflict` problem body carrying the item's current state.
    static func conflict(current: Components.Schemas.ShoppingItem) -> String {
        Fixtures.json(Components.Schemas.ShoppingItemConflict(
            _type: "about:blank", title: "Conflict", status: 409, code: "version_conflict", current: current
        ))
    }
}
