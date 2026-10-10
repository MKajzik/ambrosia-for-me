import API
import Foundation
import Persistence

/// Drains the offline queue, oldest first, one request at a time. Triggers (launch, foreground, reconnect, after an
/// online change) all just call `drain()`; calls that arrive mid-drain coalesce into one more pass.
public actor ShoppingSyncEngine {
    public static let notice = "A change couldn't be saved."

    private enum Outcome {
        /// Sent (or already true on the server): delete the row.
        case done
        /// The server will never accept it (404, 400, 409): delete the row; `notify` says whether to tell the person.
        case drop(notify: Bool)
        /// Network, 429, 5xx, expired session: keep the row and stop draining this list for now.
        case retryLater
    }

    private let client: Client
    private let cache: ShoppingCache
    private var isDraining = false
    private var wantsAnotherPass = false
    private var pendingNotice: String?
    private var subscribers: [UUID: AsyncStream<Void>.Continuation] = [:]

    public init(client: Client, cache: ShoppingCache) {
        self.client = client
        self.cache = cache
    }

    /// One element after every queue step, so a view model knows to re-read the cache. One stream per caller.
    public func changes() -> AsyncStream<Void> {
        let id = UUID()
        let (stream, continuation) = AsyncStream<Void>.makeStream(bufferingPolicy: .bufferingNewest(1))
        subscribers[id] = continuation
        continuation.onTermination = { [weak self] _ in Task { await self?.unsubscribe(id) } }
        return stream
    }

    /// The text for a change that was refused, once.
    public func takeNotice() -> String? {
        defer { pendingNotice = nil }
        return pendingNotice
    }

    /// The session ended: forget a notice meant for the user who just signed out.
    public func reset() {
        pendingNotice = nil
    }

    public func drain() async {
        if isDraining {
            wantsAnotherPass = true
            return
        }
        isDraining = true
        repeat {
            wantsAnotherPass = false
            await drainOnce()
        } while wantsAnotherPass && !Task.isCancelled
        isDraining = false
    }

    private func drainOnce() async {
        var blocked: Set<String> = []
        while !Task.isCancelled, let next = await cache.intents().first(where: { !blocked.contains($0.listID) }) {
            switch await send(next) {
            case .done:
                await cache.removeIntent(id: next.id)
            case .drop(let notify):
                await cache.removeIntent(id: next.id)
                // Its later changes are addressed to an item that will never exist.
                if next.kind == .add { await cache.dropIntents(itemID: next.itemID) }
                if notify { pendingNotice = Self.notice }
            case .retryLater:
                blocked.insert(next.listID)
            }
            publish()
        }
    }

    private func send(_ intent: ShoppingIntent) async -> Outcome {
        do {
            switch intent.kind {
            case .check, .uncheck:
                // A check addressed to an item whose add never synced cannot be sent.
                guard !intent.isTemp else { return .drop(notify: false) }
                let response = try await client.updateShoppingItem(.init(
                        path: .init(id: intent.listID, itemId: intent.itemID),
                        body: .json(.init(checked: intent.kind == .check))
                    ))
                switch response {
                case .ok(let ok):
                    await cache.applyItem(try ok.body.json, listID: intent.listID)
                    return .done
                case .notFound:
                    await cache.removeItem(id: intent.itemID, listID: intent.listID)
                    return .drop(notify: false)
                case .badRequest, .conflict: return .drop(notify: true)
                case .unauthorized, .tooManyRequests, .internalServerError: return .retryLater
                case .undocumented(let status, _): return status >= 500 ? .retryLater : .drop(notify: true)
                }
            case .add:
                guard let payload = intent.payload else { return .drop(notify: true) }
                let response = try await client.createShoppingItem(.init(
                        path: .init(id: intent.listID),
                        body: .json(.init(
                            ingredientId: payload.ingredientID, name: payload.name, quantity: payload.quantity,
                            unit: payload.unit, category: payload.category
                        ))
                    ))
                switch response {
                case .created(let created):
                    let item = try created.body.json
                    await cache.rewriteTempID(intent.itemID, to: item.id)
                    await cache.applyItem(item, listID: intent.listID)
                    return .done
                case .notFound: return .drop(notify: false)
                case .badRequest: return .drop(notify: true)
                case .unauthorized, .tooManyRequests, .internalServerError: return .retryLater
                case .undocumented(let status, _): return status >= 500 ? .retryLater : .drop(notify: true)
                }
            case .remove:
                guard !intent.isTemp else { return .drop(notify: false) }
                let response = try await client.deleteShoppingItem(.init(path: .init(id: intent.listID, itemId: intent.itemID)))
                switch response {
                case .noContent, .notFound:
                    await cache.removeItem(id: intent.itemID, listID: intent.listID)
                    return .done
                case .badRequest: return .drop(notify: true)
                case .unauthorized, .tooManyRequests, .internalServerError: return .retryLater
                case .undocumented(let status, _): return status >= 500 ? .retryLater : .drop(notify: true)
                }
            }
        } catch {
            // A transport failure (URLError) or an unreadable answer: try again on the next trigger.
            return .retryLater
        }
    }

    private func publish() {
        for continuation in subscribers.values { continuation.yield() }
    }

    private func unsubscribe(_ id: UUID) { subscribers[id] = nil }
}
