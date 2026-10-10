import API
import Foundation
import Persistence

/// What the screen shows: the server's last snapshot of a list with this device's pending changes laid over it.
/// Pure. A refetch replaces the snapshot and can never erase a pending change, because pending changes are not in it.
public enum PendingOverlay {
    public static func apply(
        _ list: Components.Schemas.ShoppingList, intents: [ShoppingIntent], userID: String?, now: Date = Date()
    ) -> Components.Schemas.ShoppingList {
        var result = list
        for intent in intents.filter({ $0.listID == list.id }).sorted(by: { $0.sequence < $1.sequence }) {
            switch intent.kind {
            case .check, .uncheck:
                let checked = intent.kind == .check
                result.items = result.items.map { item in
                    guard item.id == intent.itemID else { return item }
                    var item = item
                    item.checked = checked
                    item.checkedBy = checked ? userID : nil
                    return item
                }
            case .remove:
                result.items.removeAll { $0.id == intent.itemID }
            case .add:
                guard let payload = intent.payload, !result.items.contains(where: { $0.id == intent.itemID }) else { continue }
                result.items.append(.init(
                    id: intent.itemID, listId: list.id, ingredientId: payload.ingredientID, name: payload.name,
                    quantity: payload.quantity,
                    unit: payload.unit.flatMap { Components.Schemas.ShoppingItem.UnitPayload(rawValue: $0.rawValue) },
                    category: payload.category ?? .other, checked: false, checkedBy: nil,
                    position: (result.items.map(\.position).max() ?? -1) + 1, version: 1, origin: .manual,
                    createdAt: now, updatedAt: now
                ))
            }
        }
        return result
    }
}
