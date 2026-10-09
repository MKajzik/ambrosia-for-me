import API
import Foundation
import Persistence

/// What the item edit sheet sends. A `nil` quantity or unit means "leave as it is": the generated optional cannot
/// encode an explicit `null` (the same limit as a meal's notes).
public struct ItemEdit: Equatable, Sendable {
    public var name: String
    public var quantity: Double?
    public var unit: Components.Schemas.Unit?
    public var category: Components.Schemas.IngredientCategory

    public init(name: String, quantity: Double?, unit: Components.Schemas.Unit?, category: Components.Schemas.IngredientCategory) {
        self.name = name
        self.quantity = quantity
        self.unit = unit
        self.category = category
    }
}

/// Shopping lists, cache-first (`cached…()` then `refresh…()`). Item `check`/`add`/`remove` only enqueue (the sync
/// engine sends them); every other write goes straight to the API and stores its answer.
public struct ShoppingListsRepository: Sendable {
    private let client: Client
    private let cache: ShoppingCache
    private static let pageSize = 50

    public init(client: Client, cache: ShoppingCache) {
        self.client = client
        self.cache = cache
    }

    // MARK: Reads

    public func cachedLists(_ scope: MealScope) async -> [Components.Schemas.ShoppingListSummary] {
        await cache.summaries(scope: scope)
    }

    public func cachedList(id: String) async -> Components.Schemas.ShoppingList? { await cache.list(id: id) }

    public func pendingIntents(listID: String) async -> [ShoppingIntent] { await cache.intents(listID: listID) }

    /// The lists with at least one queued change (the lists screen's syncing badge).
    public func pendingListIDs() async -> Set<String> { Set(await cache.intents().map(\.listID)) }

    /// One page of lists. `cursor == nil` is the first page and replaces the scope in the cache; a later page is merged.
    /// Returns the next cursor, or `nil` when there is no more. A failure leaves the cache untouched, except that
    /// `404 partner_not_linked` clears the partner scope.
    public func refreshLists(_ scope: MealScope, cursor: String?) async throws -> String? {
        let page: Components.Schemas.ShoppingListPage
        switch scope {
        case .mine:
            let response = try await unwrapping {
                try await client.listShoppingLists(.init(query: .init(cursor: cursor, limit: Self.pageSize)))
            }
            switch response {
            case .ok(let ok): page = try ok.body.json
            case .badRequest(let r): throw ShoppingError.validation(r.problem)
            case .unauthorized: throw ShoppingError.unauthorized
            case .tooManyRequests: throw ShoppingError.rateLimited
            case .internalServerError: throw ShoppingError.server("The server had a problem loading your lists.")
            case .undocumented(let status, _): throw ShoppingError.unexpected(status)
            }
        case .partner:
            let response = try await unwrapping {
                try await client.listPartnerShoppingLists(.init(query: .init(cursor: cursor, limit: Self.pageSize)))
            }
            switch response {
            case .ok(let ok): page = try ok.body.json
            case .badRequest(let r): throw ShoppingError.validation(r.problem)
            case .unauthorized: throw ShoppingError.unauthorized
            case .notFound:
                await cache.clear(scope: .partner)
                throw ShoppingError.partnerNotLinked
            case .tooManyRequests: throw ShoppingError.rateLimited
            case .internalServerError: throw ShoppingError.server("The server had a problem loading your partner's lists.")
            case .undocumented(let status, _): throw ShoppingError.unexpected(status)
            }
        }
        if cursor == nil {
            await cache.replaceSummaries(page.items, scope: scope)
        } else {
            await cache.mergeSummaries(page.items, scope: scope)
        }
        return page.nextCursor
    }

    /// `GET /shopping-lists/{id}`; the answer replaces the snapshot. `404` means access is gone: the list and its
    /// pending changes are removed from the cache.
    @discardableResult
    public func refreshList(id: String) async throws -> Components.Schemas.ShoppingList {
        let response = try await unwrapping { try await client.getShoppingList(.init(path: .init(id: id))) }
        switch response {
        case .ok(let ok): return try await keep(try ok.body.json)
        case .badRequest(let r): throw ShoppingError.validation(r.problem)
        case .unauthorized: throw ShoppingError.unauthorized
        case .notFound:
            await cache.removeList(id: id)
            throw ShoppingError.notFound
        case .tooManyRequests: throw ShoppingError.rateLimited
        case .internalServerError: throw ShoppingError.server("The server had a problem loading the list.")
        case .undocumented(let status, _): throw ShoppingError.unexpected(status)
        }
    }

    // MARK: Online writes

    public func createList(name: String, shared: Bool) async throws -> Components.Schemas.ShoppingList {
        let response = try await unwrapping {
            try await client.createShoppingList(.init(body: .json(.init(name: name, sharedWithPartner: shared))))
        }
        switch response {
        case .created(let created): return try await keep(try created.body.json)
        case .badRequest(let r): throw ShoppingError.validation(r.problem)
        case .unauthorized: throw ShoppingError.unauthorized
        case .tooManyRequests: throw ShoppingError.rateLimited
        case .internalServerError: throw ShoppingError.server("The server had a problem creating the list.")
        case .undocumented(let status, _): throw ShoppingError.unexpected(status)
        }
    }

    /// Builds a list from the plan between two dates, or regenerates `listID`. `404`: nothing is planned in the range.
    public func generate(from: String, to: String, name: String?, listID: String?) async throws -> Components.Schemas.ShoppingList {
        let response = try await unwrapping {
            try await client.generateShoppingList(.init(body: .json(.init(from: from, to: to, name: name, listId: listID))))
        }
        switch response {
        case .ok(let ok): return try await keep(try ok.body.json)
        case .created(let created): return try await keep(try created.body.json)
        case .badRequest(let r): throw ShoppingError.validation(r.problem)
        case .unauthorized: throw ShoppingError.unauthorized
        case .notFound: throw ShoppingError.notFound
        case .tooManyRequests: throw ShoppingError.rateLimited
        case .internalServerError: throw ShoppingError.server("The server had a problem generating the list.")
        case .undocumented(let status, _): throw ShoppingError.unexpected(status)
        }
    }

    public func updateList(id: String, name: String?, shared: Bool?) async throws -> Components.Schemas.ShoppingList {
        let response = try await unwrapping {
            try await client.updateShoppingList(.init(path: .init(id: id), body: .json(.init(name: name, sharedWithPartner: shared))))
        }
        switch response {
        case .ok(let ok): return try await keep(try ok.body.json)
        case .badRequest(let r): throw ShoppingError.validation(r.problem)
        case .unauthorized: throw ShoppingError.unauthorized
        case .notFound:
            await cache.removeList(id: id)
            throw ShoppingError.notFound
        case .tooManyRequests: throw ShoppingError.rateLimited
        case .internalServerError: throw ShoppingError.server("The server had a problem updating the list.")
        case .undocumented(let status, _): throw ShoppingError.unexpected(status)
        }
    }

    /// Deleting a list that is already gone (`404`) is a success: the goal is met.
    public func deleteList(id: String) async throws {
        let response = try await unwrapping { try await client.deleteShoppingList(.init(path: .init(id: id))) }
        switch response {
        case .noContent, .notFound: await cache.removeList(id: id)
        case .badRequest(let r): throw ShoppingError.validation(r.problem)
        case .unauthorized: throw ShoppingError.unauthorized
        case .tooManyRequests: throw ShoppingError.rateLimited
        case .internalServerError: throw ShoppingError.server("The server had a problem deleting the list.")
        case .undocumented(let status, _): throw ShoppingError.unexpected(status)
        }
    }

    /// Edits an item against the version the form was opened on. `409` throws `versionConflict(current:)` and also puts
    /// that current item in the cache, so the screen already shows what the server has.
    @discardableResult
    public func editItem(listID: String, itemID: String, version: Int, edit: ItemEdit) async throws -> Components.Schemas.ShoppingItem {
        let response = try await unwrapping {
            try await client.updateShoppingItem(.init(
                path: .init(id: listID, itemId: itemID),
                body: .json(.init(
                    version: version, name: edit.name, quantity: edit.quantity,
                    unit: edit.unit.flatMap { Components.Schemas.UpdateShoppingItemRequest.UnitPayload(rawValue: $0.rawValue) },
                    category: edit.category
                ))
            ))
        }
        switch response {
        case .ok(let ok):
            let item = try ok.body.json
            await cache.applyItem(item, listID: listID)
            return item
        case .conflict(let conflict):
            let current = try conflict.body.applicationProblemJson.current
            await cache.applyItem(current, listID: listID)
            throw ShoppingError.versionConflict(current: current)
        case .badRequest(let r): throw ShoppingError.validation(r.problem)
        case .unauthorized: throw ShoppingError.unauthorized
        case .notFound:
            await cache.removeItem(id: itemID, listID: listID)
            throw ShoppingError.notFound
        case .tooManyRequests: throw ShoppingError.rateLimited
        case .internalServerError: throw ShoppingError.server("The server had a problem saving the item.")
        case .undocumented(let status, _): throw ShoppingError.unexpected(status)
        }
    }

    // MARK: Offline-able writes (enqueue only; the sync engine sends them)

    public func setChecked(_ checked: Bool, itemID: String, listID: String) async {
        await cache.enqueue(kind: checked ? .check : .uncheck, listID: listID, itemID: itemID, payload: nil)
    }

    /// Queues an item for the list and returns its temporary id (shown at once, replaced by the server's id after sync).
    @discardableResult
    public func addItem(_ payload: ShoppingIntent.AddPayload, listID: String) async -> String {
        let temp = ShoppingIntent.newTempID()
        await cache.enqueue(kind: .add, listID: listID, itemID: temp, payload: payload)
        return temp
    }

    public func removeItem(itemID: String, listID: String) async {
        await cache.enqueue(kind: .remove, listID: listID, itemID: itemID, payload: nil)
    }

    // MARK: Cache upkeep

    public func removeCachedItem(itemID: String, listID: String) async { await cache.removeItem(id: itemID, listID: listID) }
    public func dropList(id: String) async { await cache.removeList(id: id) }
    public func clearPartnerLists() async { await cache.clear(scope: .partner) }

    /// Called whenever the session ends, so a second user on this device never sees (or sends) the first user's lists.
    public func clearCaches() async { await cache.clearAll() }

    private func keep(_ list: Components.Schemas.ShoppingList) async -> Components.Schemas.ShoppingList {
        await cache.store(list)
        return list
    }
}
