import API
import Foundation

/// A tiny in-memory stand-in for the shopping endpoints, so tests read like the real round trip. Item `PATCH` honours
/// `version` (a mismatch is the real `409` with `current`), `checked` is unversioned, every change bumps `version`.
final class ShoppingServer: @unchecked Sendable {
    private let lock = NSLock()
    private var lists: [String: Components.Schemas.ShoppingList]
    private var offline = false
    private var partnerLinked = true
    private var forced: [String: Int] = [:]
    private var counter = 100

    init(_ lists: [Components.Schemas.ShoppingList] = []) {
        self.lists = Dictionary(uniqueKeysWithValues: lists.map { ($0.id, $0) })
    }

    func setOffline(_ value: Bool) { lock.lock(); offline = value; lock.unlock() }
    func setPartnerLinked(_ value: Bool) { lock.lock(); partnerLinked = value; lock.unlock() }

    /// Every request for this exact route (`"PATCH /shopping-lists/l1/items/i1"`) answers `status`.
    func force(_ route: String, status: Int) { lock.lock(); forced[route] = status; lock.unlock() }

    func list(_ id: String) -> Components.Schemas.ShoppingList? { lock.lock(); defer { lock.unlock() }; return lists[id] }

    /// Changes an item as another person would: its version goes up by one.
    func mutate(listID: String, itemID: String, _ change: (inout Components.Schemas.ShoppingItem) -> Void) {
        lock.lock(); defer { lock.unlock() }
        guard var list = lists[listID], let index = list.items.firstIndex(where: { $0.id == itemID }) else { return }
        change(&list.items[index])
        list.items[index].version += 1
        lists[listID] = list
    }

    func deleteItem(listID: String, itemID: String) {
        lock.lock(); defer { lock.unlock() }
        lists[listID]?.items.removeAll { $0.id == itemID }
    }

    func deleteList(_ id: String) { lock.lock(); lists[id] = nil; lock.unlock() }

    func route(_ call: RoutingTransport.Call) async throws -> (status: Int, body: String) { try handle(call) }

    private func notFound() -> (status: Int, body: String) { (404, Fixtures.problem(404, code: "not_found")) }

    private func handle(_ call: RoutingTransport.Call) throws -> (status: Int, body: String) {
        lock.lock(); defer { lock.unlock() }
        if offline { throw URLError(.notConnectedToInternet) }
        if let status = forced[call.route] { return (status, status >= 400 ? Fixtures.problem(status, code: "forced") : "") }
        let parts = call.route.split(separator: " ", maxSplits: 1).map(String.init)
        let method = parts[0]
        let segments = parts[1].split(separator: "/").map(String.init)
        let body = (try? JSONSerialization.jsonObject(with: Data(call.body.utf8))) as? [String: Any] ?? [:]

        if method == "GET", segments == ["shopping-lists"] {
            return (200, ShoppingFixtures.page(lists.values.sorted { $0.id < $1.id }.map(ShoppingFixtures.summary)))
        }
        if method == "GET", segments == ["partner", "shopping-lists"] {
            return partnerLinked ? (200, ShoppingFixtures.page([])) : (404, Fixtures.problem(404, code: "partner_not_linked"))
        }
        if method == "POST", segments == ["shopping-lists"] {
            counter += 1
            let list = ShoppingFixtures.list(
                id: "srv-\(counter)", name: body["name"] as? String ?? "List", shared: body["shared_with_partner"] as? Bool ?? false
            )
            lists[list.id] = list
            return (201, Fixtures.json(list))
        }
        if method == "POST", segments == ["shopping-lists", "generate"] {
            counter += 1
            let id = (body["list_id"] as? String) ?? "srv-\(counter)"
            let rice = ShoppingFixtures.item(id: "gen-\(counter)", listID: id, name: "Rice", category: .grainsBread, quantity: 200, unit: .g)
            let list = ShoppingFixtures.list(
                id: id, name: body["name"] as? String ?? "Shopping", items: [rice],
                from: body["from"] as? String, to: body["to"] as? String
            )
            let existed = lists[id] != nil
            lists[id] = list
            return (existed ? 200 : 201, Fixtures.json(list))
        }

        guard segments.first == "shopping-lists", segments.count >= 2, var list = lists[segments[1]] else { return notFound() }
        let listID = segments[1]

        if segments.count == 2 {
            switch method {
            case "GET": return (200, Fixtures.json(list))
            case "PATCH":
                if let name = body["name"] as? String { list.name = name }
                if let shared = body["shared_with_partner"] as? Bool { list.sharedWithPartner = shared }
                lists[listID] = list
                return (200, Fixtures.json(list))
            case "DELETE":
                lists[listID] = nil
                return (204, "")
            default: break
            }
        }
        if segments.count == 3, segments[2] == "items", method == "POST" {
            counter += 1
            var item = ShoppingFixtures.item(
                id: "srv-\(counter)", listID: listID, name: body["name"] as? String ?? "Item",
                position: (list.items.map(\.position).max() ?? -1) + 1
            )
            if let raw = body["category"] as? String, let category = Components.Schemas.IngredientCategory(rawValue: raw) { item.category = category }
            if let raw = body["unit"] as? String { item.unit = .init(rawValue: raw) }
            item.quantity = body["quantity"] as? Double
            list.items.append(item)
            lists[listID] = list
            return (201, Fixtures.json(item))
        }
        if segments.count == 4, segments[2] == "items" {
            guard let index = list.items.firstIndex(where: { $0.id == segments[3] }) else { return notFound() }
            switch method {
            case "DELETE":
                list.items.remove(at: index)
                lists[listID] = list
                return (204, "")
            case "PATCH":
                if let version = body["version"] as? Int, version != list.items[index].version {
                    return (409, ShoppingFixtures.conflict(current: list.items[index]))
                }
                if let checked = body["checked"] as? Bool { list.items[index].checked = checked }
                if let name = body["name"] as? String { list.items[index].name = name }
                if let quantity = body["quantity"] as? Double { list.items[index].quantity = quantity }
                list.items[index].version += 1
                lists[listID] = list
                return (200, Fixtures.json(list.items[index]))
            default: break
            }
        }
        return (500, Fixtures.problem(500, code: "unrouted"))
    }
}
