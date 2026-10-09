import API
import Foundation
import SwiftData

/// The shopping cache: list summaries (Mine / Partner's, reusing `MealScope`), the server's snapshot of each list, and
/// the offline queue. Takes and returns value types. Snapshots are best-effort (rebuildable from the API); the queue is
/// not, which is why intents are written in the same save as the change that produced them.
@ModelActor
public actor ShoppingCache {
    // MARK: Summaries

    public func summaries(scope: MealScope) -> [Components.Schemas.ShoppingListSummary] {
        summaryRows().filter { $0.scopeRaw == scope.rawValue }.sorted { $0.order < $1.order }
            .compactMap { try? JSONDecoder().decode(Components.Schemas.ShoppingListSummary.self, from: $0.json) }
    }

    /// The first page of a scope: upserts in the given order and deletes ids absent from `summaries` within `scope`.
    public func replaceSummaries(_ summaries: [Components.Schemas.ShoppingListSummary], scope: MealScope) {
        upsert(summaries, scope: scope, firstOrder: 0)
        let incoming = Set(summaries.map(\.id))
        for row in summaryRows() where row.scopeRaw == scope.rawValue && !incoming.contains(row.id) { modelContext.delete(row) }
        try? modelContext.save()
    }

    /// A later page ("Load more"): upserts after the rows already there, deletes nothing.
    public func mergeSummaries(_ summaries: [Components.Schemas.ShoppingListSummary], scope: MealScope) {
        let next = (summaryRows().filter { $0.scopeRaw == scope.rawValue }.map(\.order).max() ?? -1) + 1
        upsert(summaries, scope: scope, firstOrder: next)
        try? modelContext.save()
    }

    public func clear(scope: MealScope) {
        for row in summaryRows() where row.scopeRaw == scope.rawValue { modelContext.delete(row) }
        try? modelContext.save()
    }

    // MARK: Lists

    public func list(id: String) -> Components.Schemas.ShoppingList? {
        listRow(id).flatMap { try? JSONDecoder().decode(Components.Schemas.ShoppingList.self, from: $0.json) }
    }

    /// Stores the server's answer for a list, and brings an existing summary row up to date (rename, sharing).
    public func store(_ list: Components.Schemas.ShoppingList) {
        guard let json = try? JSONEncoder().encode(list) else { return }
        if let row = listRow(list.id) { row.json = json } else { modelContext.insert(CachedShoppingList(id: list.id, json: json)) }
        if let row = summaryRows().first(where: { $0.id == list.id }) {
            let summary = Components.Schemas.ShoppingListSummary(
                id: list.id, name: list.name, sharedWithPartner: list.sharedWithPartner, sourceFrom: list.sourceFrom,
                sourceTo: list.sourceTo, createdAt: list.createdAt, updatedAt: list.updatedAt
            )
            if let encoded = try? JSONEncoder().encode(summary) { row.json = encoded }
        }
        try? modelContext.save()
    }

    /// Takes in what the server said about one item, unless the snapshot already holds a newer version of it.
    public func applyItem(_ item: Components.Schemas.ShoppingItem, listID: String) {
        guard var list = list(id: listID) else { return }
        if let index = list.items.firstIndex(where: { $0.id == item.id }) {
            guard list.items[index].version <= item.version else { return }
            list.items[index] = item
        } else {
            list.items.append(item)
            list.items.sort { $0.position < $1.position }
        }
        store(list)
    }

    public func removeItem(id: String, listID: String) {
        guard var list = list(id: listID), list.items.contains(where: { $0.id == id }) else { return }
        list.items.removeAll { $0.id == id }
        store(list)
    }

    /// The list is gone (deleted, unshared, unlinked): its snapshot, its summary row and its pending changes.
    public func removeList(id: String) {
        if let row = listRow(id) { modelContext.delete(row) }
        for row in summaryRows() where row.id == id { modelContext.delete(row) }
        for row in intentRows() where row.listID == id { modelContext.delete(row) }
        try? modelContext.save()
    }

    // MARK: Queue

    /// Every pending change, in `sequence` order.
    public func intents() -> [ShoppingIntent] {
        intentRows().sorted { $0.sequence < $1.sequence }.compactMap(Self.intent)
    }

    public func intents(listID: String) -> [ShoppingIntent] {
        intents().filter { $0.listID == listID }
    }

    /// Adds a change, collapsing it against what is already pending (`IntentQueue`), in one save. Returns the
    /// list's pending changes afterwards.
    @discardableResult
    public func enqueue(kind: ShoppingIntent.Kind, listID: String, itemID: String, payload: ShoppingIntent.AddPayload?) -> [ShoppingIntent] {
        let all = intents()
        let next = ShoppingIntent(
            sequence: (all.map(\.sequence).max() ?? 0) + 1, kind: kind, listID: listID, itemID: itemID, payload: payload
        )
        let result = IntentQueue.enqueue(next, into: all)
        let keep = Set(result.map(\.id))
        let existing = Set(all.map(\.id))
        for row in intentRows() where !keep.contains(row.id) { modelContext.delete(row) }
        for intent in result where !existing.contains(intent.id) {
            modelContext.insert(CachedIntent(
                id: intent.id, sequence: intent.sequence, kindRaw: intent.kind.rawValue, listID: intent.listID,
                itemID: intent.itemID, payloadJSON: intent.payload.flatMap { try? JSONEncoder().encode($0) }
            ))
        }
        try? modelContext.save()
        return result.filter { $0.listID == listID }
    }

    public func removeIntent(id: UUID) {
        for row in intentRows() where row.id == id { modelContext.delete(row) }
        try? modelContext.save()
    }

    /// Forgets every pending change for one item (its add was refused, so nothing addressed to it can succeed).
    public func dropIntents(itemID: String) {
        for row in intentRows() where row.itemID == itemID { modelContext.delete(row) }
        try? modelContext.save()
    }

    /// An add succeeded: later changes addressed to `temp` now go to the server's id.
    public func rewriteTempID(_ temp: String, to real: String) {
        for row in intentRows() where row.itemID == temp { row.itemID = real }
        try? modelContext.save()
    }

    public func clearAll() {
        for row in summaryRows() { modelContext.delete(row) }
        for row in (try? modelContext.fetch(FetchDescriptor<CachedShoppingList>())) ?? [] { modelContext.delete(row) }
        for row in intentRows() { modelContext.delete(row) }
        try? modelContext.save()
    }

    // MARK: Private

    private func upsert(_ summaries: [Components.Schemas.ShoppingListSummary], scope: MealScope, firstOrder: Int) {
        let existing = Dictionary(summaryRows().map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        for (offset, summary) in summaries.enumerated() {
            guard let json = try? JSONEncoder().encode(summary) else { continue }
            if let row = existing[summary.id] {
                row.json = json
                row.scopeRaw = scope.rawValue
                row.order = firstOrder + offset
            } else {
                modelContext.insert(CachedShoppingSummary(id: summary.id, scopeRaw: scope.rawValue, order: firstOrder + offset, json: json))
            }
        }
    }

    private func summaryRows() -> [CachedShoppingSummary] {
        (try? modelContext.fetch(FetchDescriptor<CachedShoppingSummary>())) ?? []
    }

    private func listRow(_ id: String) -> CachedShoppingList? {
        (try? modelContext.fetch(FetchDescriptor<CachedShoppingList>()))?.first { $0.id == id }
    }

    private func intentRows() -> [CachedIntent] {
        (try? modelContext.fetch(FetchDescriptor<CachedIntent>())) ?? []
    }

    private static func intent(_ row: CachedIntent) -> ShoppingIntent? {
        guard let kind = ShoppingIntent.Kind(rawValue: row.kindRaw) else { return nil }
        return ShoppingIntent(
            id: row.id, sequence: row.sequence, kind: kind, listID: row.listID, itemID: row.itemID,
            payload: row.payloadJSON.flatMap { try? JSONDecoder().decode(ShoppingIntent.AddPayload.self, from: $0) }
        )
    }
}
