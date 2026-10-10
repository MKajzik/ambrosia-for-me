import Foundation
import SwiftData

/// Internal to this module: `@Model` objects never leave `ShoppingCache`. Lists are stored as the generated JSON
/// because nothing queries inside them.

/// A row of the Mine / Partner's list of lists. `order` keeps the server's order.
@Model
final class CachedShoppingSummary {
    @Attribute(.unique) var id: String
    var scopeRaw: String
    var order: Int
    var json: Data

    init(id: String, scopeRaw: String, order: Int, json: Data) {
        self.id = id
        self.scopeRaw = scopeRaw
        self.order = order
        self.json = json
    }
}

/// The server's last snapshot of one list, items included. Pending changes are never written here.
@Model
final class CachedShoppingList {
    @Attribute(.unique) var id: String
    var json: Data

    init(id: String, json: Data) {
        self.id = id
        self.json = json
    }
}

/// One pending offline change (`ShoppingIntent`).
@Model
final class CachedIntent {
    @Attribute(.unique) var id: UUID
    var sequence: Int
    var kindRaw: String
    var listID: String
    var itemID: String
    var payloadJSON: Data?

    init(id: UUID, sequence: Int, kindRaw: String, listID: String, itemID: String, payloadJSON: Data?) {
        self.id = id
        self.sequence = sequence
        self.kindRaw = kindRaw
        self.listID = listID
        self.itemID = itemID
        self.payloadJSON = payloadJSON
    }
}
