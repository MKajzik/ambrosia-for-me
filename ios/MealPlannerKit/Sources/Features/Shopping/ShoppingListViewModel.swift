import API
import Foundation
import Observation
import Persistence
import Repositories

/// One shopping list. The screen shows `PendingOverlay(snapshot, pending changes)`. Item check/add/remove only enqueue
/// and then ask the sync engine to drain, so they work offline; every other write goes straight to the API.
@Observable
@MainActor
public final class ShoppingListViewModel {
    public enum EditOutcome: Equatable, Sendable {
        case saved
        /// Someone else changed the item first: this is the server's current version, already on screen.
        case conflict(Components.Schemas.ShoppingItem)
        case failed(String)
    }

    public typealias EventSource = @Sendable (String) -> AsyncThrowingStream<ListEventStream.Update, Error>

    public let listID: String
    public private(set) var displayed: Components.Schemas.ShoppingList?
    public private(set) var isLoading = false
    public private(set) var isStale = false
    /// The list is gone or no longer visible: the screen says so, even while items are cached.
    public private(set) var accessLost = false
    public private(set) var loadError: String?
    public private(set) var pendingCount = 0
    public var alertMessage: String?

    public var isSyncing: Bool { pendingCount > 0 }
    public var isOwner: Bool { displayed?.isOwner ?? false }
    public var groups: [ItemGroup] { displayed.map { ItemGrouping.groups($0.items) } ?? [] }
    public var progress: (done: Int, total: Int) { displayed.map { ItemGrouping.progress($0.items) } ?? (0, 0) }

    @ObservationIgnored private let shopping: ShoppingListsRepository
    @ObservationIgnored private let sync: ShoppingSyncEngine
    @ObservationIgnored private let events: EventSource
    @ObservationIgnored private let userID: @MainActor () -> String?
    @ObservationIgnored private let sleep: @Sendable (Duration) async throws -> Void
    @ObservationIgnored private var snapshot: Components.Schemas.ShoppingList?
    /// Overlapping `load()` calls: `isLoading` stays true until the last one ends.
    @ObservationIgnored private var loadsInFlight = 0 { didSet { isLoading = loadsInFlight > 0 } }
    /// Bumped by every `reload()`; a reload whose reads were overtaken by a newer one reads again.
    @ObservationIgnored private var reloadGeneration = 0

    public init(
        listID: String, shopping: ShoppingListsRepository, sync: ShoppingSyncEngine, events: @escaping EventSource,
        userID: @escaping @MainActor () -> String?,
        sleep: @escaping @Sendable (Duration) async throws -> Void = { try await Task.sleep(for: $0) }
    ) {
        self.listID = listID
        self.shopping = shopping
        self.sync = sync
        self.events = events
        self.userID = userID
        self.sleep = sleep
    }

    /// The screen's lifetime: load, then keep the screen in step with the queue and with the partner's changes.
    public func run() async {
        await load()
        guard !accessLost else { return }
        await withTaskGroup(of: Void.self) { group in
            group.addTask { await self.observeSync() }
            group.addTask { await self.runLiveUpdates() }
        }
    }

    public func load() async {
        loadsInFlight += 1
        defer { loadsInFlight -= 1 }
        await reload()
        await refresh()
    }

    /// Re-reads the cache (the snapshot and the pending changes) and lays one over the other. When a newer reload
    /// starts while this one reads, this one reads again rather than apply an older answer or return without one:
    /// every call returns with the screen showing what the cache held when it was made (the next tap acts on it).
    public func reload() async {
        reloadGeneration += 1
        while true {
            let generation = reloadGeneration
            let cached = await shopping.cachedList(id: listID)
            let intents = await shopping.pendingIntents(listID: listID)
            guard generation == reloadGeneration else { continue }
            snapshot = accessLost ? nil : cached
            pendingCount = accessLost ? 0 : intents.count
            displayed = snapshot.map { PendingOverlay.apply($0, intents: intents, userID: userID()) }
            return
        }
    }

    private func refresh() async {
        do {
            try await shopping.refreshList(id: listID)
            isStale = false
            loadError = nil
        } catch ShoppingError.notFound {
            accessLost = true
            loadError = nil
        } catch {
            isStale = true
            loadError = snapshot == nil ? ErrorText.message(for: error) : nil
        }
        await reload()
    }

    // MARK: Offline-able changes

    public func toggle(_ item: Components.Schemas.ShoppingItem) async {
        await shopping.setChecked(!item.checked, itemID: item.id, listID: listID)
        await settle()
    }

    /// Returns false (and queues nothing) for a blank or over-long name.
    @discardableResult
    public func quickAdd(_ name: String) async -> Bool {
        let trimmed = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty, trimmed.count <= 200 else { return false }
        await shopping.addItem(.init(name: trimmed), listID: listID)
        await settle()
        return true
    }

    public func remove(_ item: Components.Schemas.ShoppingItem) async {
        await shopping.removeItem(itemID: item.id, listID: listID)
        await settle()
    }

    /// Shows the change at once, then asks the engine to send it without waiting for the network: the caller (the
    /// quick-add field, a row) is free as soon as the change is on screen. The result is shown when the drain ends
    /// (and by `observeSync` while the screen runs).
    private func settle() async {
        await reload()
        Task {
            await sync.drain()
            await reload()
            if let notice = await sync.takeNotice() { alertMessage = notice }
        }
    }

    private func observeSync() async {
        let changes = await sync.changes()
        for await _ in changes {
            await reload()
            if let notice = await sync.takeNotice() { alertMessage = notice }
        }
    }

    // MARK: Online-only changes

    /// Edits against the version the sheet was opened on. Needs a connection.
    public func edit(
        _ item: Components.Schemas.ShoppingItem, name: String, quantity: String, unit: Components.Schemas.Unit?,
        category: Components.Schemas.IngredientCategory
    ) async -> EditOutcome {
        let trimmed = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return .failed("Give the item a name.") }
        guard trimmed.count <= 200 else { return .failed("The name is too long.") }
        let amount: Double?
        switch parseDecimal(quantity) {
        case .blank: amount = nil
        case .value(let value):
            guard value <= 100_000 else { return .failed("Quantity is too large.") }
            amount = value
        case .invalid: return .failed("Enter a valid quantity.")
        }
        do {
            try await shopping.editItem(
                listID: listID, itemID: item.id, version: item.version,
                edit: ItemEdit(name: trimmed, quantity: amount, unit: unit, category: category)
            )
            await reload()
            return .saved
        } catch ShoppingError.versionConflict(let current) {
            await reload()
            return .conflict(current)
        } catch ShoppingError.notFound {
            await refresh()
            return .failed("This item was removed.")
        } catch {
            return .failed(ErrorText.message(for: error))
        }
    }

    public func rename(_ name: String) async {
        let trimmed = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { alertMessage = "Give the list a name."; return }
        await write { _ = try await self.shopping.updateList(id: self.listID, name: trimmed, shared: nil) }
    }

    public func setShared(_ shared: Bool) async {
        await write { _ = try await self.shopping.updateList(id: self.listID, name: nil, shared: shared) }
    }

    /// Rebuilds a generated list from the plan dates it came from.
    public func regenerate() async {
        guard let from = snapshot?.sourceFrom, let to = snapshot?.sourceTo else { return }
        await write { _ = try await self.shopping.generate(from: from, to: to, name: nil, listID: self.listID) }
    }

    /// True once the list is gone, so the screen can close.
    public func delete() async -> Bool {
        do {
            try await shopping.deleteList(id: listID)
            accessLost = true
            await reload()
            return true
        } catch {
            alertMessage = ErrorText.message(for: error)
            return false
        }
    }

    private func write(_ body: () async throws -> Void) async {
        do {
            try await body()
            await reload()
        } catch ShoppingError.notFound {
            await markAccessLost()
        } catch {
            alertMessage = ErrorText.message(for: error)
        }
    }

    // MARK: Live updates

    public func handle(_ event: ListEvent) async {
        switch ListEventReducer.action(for: event, cached: snapshot) {
        case .ignore: break
        case .refetch: await refresh()
        case .removeItem(let id):
            await shopping.removeCachedItem(itemID: id, listID: listID)
            await reload()
        case .accessLost: await markAccessLost()
        }
    }

    private func markAccessLost() async {
        await shopping.dropList(id: listID)
        accessLost = true
        await reload()
    }

    /// Reads the list's event stream. Each time it opens, refetches the list (a change committed before the `200` is
    /// never sent as an event, as web refetches on `open`) and resets the backoff. When it closes or fails, waits (1 s,
    /// doubling to 30 s) and reconnects. Ends when the list is gone or the task is cancelled.
    public func runLiveUpdates() async {
        var delay = Duration.seconds(1)
        while !Task.isCancelled, !accessLost {
            do {
                for try await update in events(listID) {
                    switch update {
                    case .opened:
                        delay = .seconds(1)
                        await refresh()
                    case .event(let event):
                        await handle(event)
                    }
                    if accessLost { return }
                }
            } catch ListEventStream.StreamError.accessLost {
                await markAccessLost()
                return
            } catch is CancellationError {
                return
            } catch {
                // Offline, dropped, unavailable: fall through to the backoff and try again.
            }
            do { try await sleep(delay) } catch { return }
            delay = min(delay * 2, .seconds(30))
        }
    }
}
