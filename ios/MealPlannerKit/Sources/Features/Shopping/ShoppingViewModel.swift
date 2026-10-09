import API
import Foundation
import Observation
import Repositories

/// The Shopping tab's list of lists (Mine / Partner's). Reads render the cache, await the refresh, re-read; a failed
/// refresh keeps what is shown and marks it stale. Creating and generating are online-only.
@Observable
@MainActor
public final class ShoppingViewModel {
    public enum Outcome: Equatable, Sendable {
        case opened(String)
        case failed(String)
    }

    public private(set) var scope: MealScope = .mine
    public private(set) var lists: [Components.Schemas.ShoppingListSummary] = []
    /// True while any `load()` is running: loads can overlap (a scope switch, a refresh), so this counts them.
    public var isLoading: Bool { loadsInFlight > 0 }
    private var loadsInFlight = 0
    public private(set) var isStale = false
    public private(set) var loadError: String?
    public private(set) var showsPartnerSegment = false
    public private(set) var hasMore = false
    public private(set) var isLoadingMore = false
    public var alertMessage: String?

    @ObservationIgnored private let shopping: ShoppingListsRepository
    @ObservationIgnored private let partner: PartnerRepository
    @ObservationIgnored private let day: LocalDay
    @ObservationIgnored private var nextCursor: String?

    public init(shopping: ShoppingListsRepository, partner: PartnerRepository, day: LocalDay = LocalDay()) {
        self.shopping = shopping
        self.partner = partner
        self.day = day
    }

    /// The tab appears or the scene becomes active: re-read the partnership, then the lists.
    public func appear() async {
        await refreshPartnerSegment()
        await load()
    }

    public func load() async {
        let requested = scope
        loadsInFlight += 1
        defer { loadsInFlight -= 1 }
        let cached = await shopping.cachedLists(requested)
        if scope == requested { lists = cached }
        do {
            let next = try await shopping.refreshLists(requested, cursor: nil)
            guard scope == requested else { return }
            nextCursor = next
            hasMore = next != nil
            lists = await shopping.cachedLists(requested)
            isStale = false
            loadError = nil
        } catch ShoppingError.partnerNotLinked {
            showsPartnerSegment = false
            if scope == requested {
                scope = .mine
                await load()
            }
        } catch {
            guard scope == requested else { return }
            // The first page did not load, so the cursor of an earlier page no longer describes what is shown.
            nextCursor = nil
            hasMore = false
            isStale = true
            loadError = lists.isEmpty ? ErrorText.message(for: error) : nil
        }
    }

    public func loadMore() async {
        guard let cursor = nextCursor, !isLoadingMore else { return }
        let requested = scope
        isLoadingMore = true
        defer { isLoadingMore = false }
        do {
            let next = try await shopping.refreshLists(requested, cursor: cursor)
            guard scope == requested else { return }
            nextCursor = next
            hasMore = next != nil
            lists = await shopping.cachedLists(requested)
        } catch {
            if scope == requested { alertMessage = ErrorText.message(for: error) }
        }
    }

    public func select(_ next: MealScope) async {
        guard next != scope else { return }
        scope = next
        nextCursor = nil
        hasMore = false
        isStale = false
        loadError = nil
        await load()
    }

    /// The generate sheet's starting range: today and the six days after it.
    public func defaultRange() -> (from: String, to: String) {
        let today = day.today()
        return (today, day.addDays(today, 6))
    }

    public func createList(name: String, shared: Bool) async -> Outcome {
        let trimmed = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return .failed("Give the list a name.") }
        guard trimmed.count <= 200 else { return .failed("The name is too long.") }
        do {
            let list = try await shopping.createList(name: trimmed, shared: shared)
            await refreshMine()
            return .opened(list.id)
        } catch {
            return .failed(ErrorText.message(for: error))
        }
    }

    public func generate(from: String, to: String, name: String?) async -> Outcome {
        if let message = RangeValidation.error(from: from, to: to, day: day) { return .failed(message) }
        let trimmed = name?.trimmingCharacters(in: .whitespacesAndNewlines)
        do {
            let list = try await shopping.generate(from: from, to: to, name: (trimmed?.isEmpty ?? true) ? nil : trimmed, listID: nil)
            await refreshMine()
            return .opened(list.id)
        } catch ShoppingError.notFound {
            return .failed("Nothing is planned on those days.")
        } catch {
            return .failed(ErrorText.message(for: error))
        }
    }

    private func refreshMine() async {
        // `try?` would flatten "no next page" (`nil`) into "failed", so catch explicitly.
        let next: String?
        do { next = try await shopping.refreshLists(.mine, cursor: nil) } catch { return }
        guard scope == .mine else { return }
        nextCursor = next
        hasMore = next != nil
        lists = await shopping.cachedLists(.mine)
    }

    /// Shown only while `GET /partner` says `active`. If the request fails (offline) it shows only when partner lists
    /// are already cached. Anything but active clears the partner scope.
    private func refreshPartnerSegment() async {
        do {
            let partnership = try await partner.status()
            showsPartnerSegment = partnership?.status == .active
            if !showsPartnerSegment { await shopping.clearPartnerLists() }
        } catch {
            showsPartnerSegment = !(await shopping.cachedLists(.partner)).isEmpty
        }
        if !showsPartnerSegment, scope == .partner { scope = .mine }
    }
}
