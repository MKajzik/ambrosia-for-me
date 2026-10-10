import API
import Foundation

/// One event of a shopping list's stream: what changed, never the new content.
public struct ListEvent: Equatable, Sendable {
    public enum Kind: String, Sendable {
        case itemChanged = "item_changed"
        case itemDeleted = "item_deleted"
        case listChanged = "list_changed"
        case listDeleted = "list_deleted"
    }

    public var kind: Kind
    public var listID: String
    public var itemID: String?
    public var version: Int?

    public init(kind: Kind, listID: String, itemID: String? = nil, version: Int? = nil) {
        self.kind = kind
        self.listID = listID
        self.itemID = itemID
        self.version = version
    }

    /// Reads an event's `data`. Anything that is not one of the API's events is `nil`, so a malformed one can never act.
    public static func parse(data: String) -> ListEvent? {
        guard let object = (try? JSONSerialization.jsonObject(with: Data(data.utf8))) as? [String: Any],
              let type = object["type"] as? String, let kind = Kind(rawValue: type),
              let listID = object["list_id"] as? String
        else { return nil }
        return ListEvent(kind: kind, listID: listID, itemID: object["item_id"] as? String, version: object["version"] as? Int)
    }
}

/// Turns the lines of a `text/event-stream` into events. Feed one line at a time, without its newline. Only `data:` lines
/// matter (the JSON carries its own `type`); comments (`: keep-alive`), `event:`/`id:`/`retry:` and CRLF are ignored.
/// A frame ends at a blank line; a frame cut off before one is never delivered.
public struct SSEFrameParser: Sendable {
    private var dataLines: [String] = []

    public init() {}

    public mutating func feed(_ line: String) -> ListEvent? {
        var line = line
        if line.hasSuffix("\r") { line.removeLast() }
        if line.isEmpty {
            defer { dataLines = [] }
            guard !dataLines.isEmpty else { return nil }
            return ListEvent.parse(data: dataLines.joined(separator: "\n"))
        }
        guard line.hasPrefix("data:") else { return nil }
        var value = String(line.dropFirst(5))
        if value.hasPrefix(" ") { value.removeFirst() }
        dataLines.append(value)
        return nil
    }
}

public enum ListEventAction: Equatable, Sendable {
    case ignore
    case refetch
    case removeItem(String)
    case accessLost
}

/// The client rules for an event, pure (web's `useListEvents`).
public enum ListEventReducer {
    public static func action(for event: ListEvent, cached: Components.Schemas.ShoppingList?) -> ListEventAction {
        switch event.kind {
        case .listDeleted:
            return .accessLost
        case .listChanged:
            return .refetch
        case .itemDeleted:
            guard let id = event.itemID, cached?.items.contains(where: { $0.id == id }) == true else { return .ignore }
            return .removeItem(id)
        case .itemChanged:
            guard let id = event.itemID, let version = event.version else { return .refetch }
            // At or below what is held: often this person's own change, already seen.
            if let held = cached?.items.first(where: { $0.id == id }), held.version >= version { return .ignore }
            // Newer, or an item never seen (a version gap is a refetch too): the event carries no content.
            return .refetch
        }
    }
}
