import API
import Foundation

/// One pending offline change to a shopping item. Only item-level changes are queued; everything else needs a connection.
public struct ShoppingIntent: Equatable, Sendable, Identifiable {
    public enum Kind: String, Sendable {
        case check, uncheck, add, remove
    }

    /// What an offline `add` will send once there is a connection.
    public struct AddPayload: Equatable, Sendable, Codable {
        public var name: String
        public var ingredientID: String?
        public var quantity: Double?
        public var unit: Components.Schemas.Unit?
        public var category: Components.Schemas.IngredientCategory?

        public init(
            name: String, ingredientID: String? = nil, quantity: Double? = nil,
            unit: Components.Schemas.Unit? = nil, category: Components.Schemas.IngredientCategory? = nil
        ) {
            self.name = name
            self.ingredientID = ingredientID
            self.quantity = quantity
            self.unit = unit
            self.category = category
        }
    }

    public static let tempPrefix = "temp:"
    public static func newTempID() -> String { tempPrefix + UUID().uuidString }

    public var id: UUID
    /// Monotonic across all lists; the queue drains in this order.
    public var sequence: Int
    public var kind: Kind
    public var listID: String
    /// The server's item id, or `temp:<uuid>` for an item that exists only on this device (an unsynced add).
    public var itemID: String
    public var payload: AddPayload?
    public var isTemp: Bool { itemID.hasPrefix(Self.tempPrefix) }

    public init(id: UUID = UUID(), sequence: Int, kind: Kind, listID: String, itemID: String, payload: AddPayload? = nil) {
        self.id = id
        self.sequence = sequence
        self.kind = kind
        self.listID = listID
        self.itemID = itemID
        self.payload = payload
    }
}

/// The collapsing rules, pure. The queue passed in may hold every list's intents: item ids are globally unique.
public enum IntentQueue {
    public static func enqueue(_ intent: ShoppingIntent, into queue: [ShoppingIntent]) -> [ShoppingIntent] {
        var result = queue
        let sameItem = { (other: ShoppingIntent) in other.itemID == intent.itemID }
        let isCheck = { (other: ShoppingIntent) in other.kind == .check || other.kind == .uncheck }
        switch intent.kind {
        case .check, .uncheck:
            // An item that is about to be removed cannot be checked.
            if result.contains(where: { sameItem($0) && $0.kind == .remove }) { return queue }
            // One row per item holding the latest desired state (the API's checked-only PATCH is last-write-wins).
            result.removeAll { sameItem($0) && isCheck($0) }
            result.append(intent)
        case .remove:
            if intent.isTemp {
                // The server never heard of it: forget everything about it, send nothing.
                result.removeAll(where: sameItem)
                return result
            }
            result.removeAll { sameItem($0) && isCheck($0) }
            if !result.contains(where: { sameItem($0) && $0.kind == .remove }) { result.append(intent) }
        case .add:
            result.append(intent)
        }
        return result
    }
}
