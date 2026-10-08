import API
import Foundation
import Observation
import Repositories

/// The plan for a date range: Today is a one-day range, Plan a week, over the same cached rows. Reads render the
/// cache, await the refresh, re-read. A write (swap, portion, remove, clear snacks, apply) is the repository write
/// followed by a refresh of the affected dates, as two separate steps: totals always come from `GET /plan`, never
/// from the device, and a failed refresh after a good write is reported as exactly that.
@Observable
@MainActor
public final class PlanViewModel {
    public struct DateRange: Equatable, Sendable {
        public var from: String
        public var to: String

        public init(from: String, to: String) {
            self.from = from
            self.to = to
        }

        public func contains(_ day: String) -> Bool { day >= from && day <= to }
    }

    public enum ApplyOutcome: Equatable, Sendable {
        case applied
        case needsConfirmation
        case failed(String)
    }

    public static let refreshNotice = "Saved, but the totals could not be refreshed. Pull to refresh."
    public static let busyMessage = "Wait for the current change to finish."

    public let localDay: LocalDay
    public private(set) var range: DateRange
    /// The cached days of `range`, by date. A date that was never fetched is absent, not empty.
    public private(set) var days: [String: Components.Schemas.DailyTotal] = [:]
    public private(set) var targets: Components.Schemas.Targets?
    /// True while any `load()` is running: loads can overlap (a new range, a refresh, a foreground), so this counts them.
    public var isLoading: Bool { loadsInFlight > 0 }
    private var loadsInFlight = 0
    public private(set) var isStale = false
    public private(set) var loadError: String?
    public private(set) var notice: String?
    /// The dates a running write is about (and whose rings and totals dim); `nil` when idle.
    public private(set) var busyRange: DateRange?

    @ObservationIgnored private let plan: PlanRepository

    public init(plan: PlanRepository, localDay: LocalDay = LocalDay(), range: DateRange? = nil) {
        self.plan = plan
        self.localDay = localDay
        let today = localDay.today()
        self.range = range ?? DateRange(from: today, to: today)
    }

    public var isWriting: Bool { busyRange != nil }
    public func isBusy(_ day: String) -> Bool { busyRange?.contains(day) ?? false }

    public func entries(on day: String, slot: Components.Schemas.Slot) -> [Components.Schemas.PlanEntry] {
        (days[day]?.entries ?? []).filter { $0.slot == slot }
    }

    public func nutrition(on day: String) -> Components.Schemas.NutrientAmounts? {
        days[day]?.nutritionPerDay
    }

    // MARK: Reads

    /// Renders the cache at once, then awaits the refresh and re-reads. A failed refresh keeps what is shown and marks
    /// it stale; with nothing cached it shows an error state.
    public func load() async {
        let requested = range
        loadsInFlight += 1
        defer { loadsInFlight -= 1 }
        show(await plan.cached(from: requested.from, to: requested.to))
        do {
            try await plan.refresh(from: requested.from, to: requested.to)
            guard range == requested else { return }
            show(await plan.cached(from: requested.from, to: requested.to))
            isStale = false
            loadError = nil
            notice = nil
        } catch {
            guard range == requested else { return }
            isStale = true
            loadError = days.isEmpty ? ErrorText.message(for: error) : nil
        }
    }

    public func setRange(from: String, to: String) async {
        range = DateRange(from: from, to: to)
        await load()
    }

    /// Today's screen: moves to the local date if it changed (after midnight, or back on foreground), and reloads.
    public func followToday() async {
        let today = localDay.today()
        if range != DateRange(from: today, to: today) { range = DateRange(from: today, to: today) }
        await load()
    }

    // MARK: Writes (each returns the error text, or nil on success, so a sheet can show its own alert)

    public func setMeal(date: String, slot: Components.Schemas.Slot, mealID: String, portion: Double) async -> String? {
        let result = await write(refreshing: DateRange(from: date, to: date)) {
            try await plan.setEntry(date: date, slot: slot, mealID: mealID, portion: portion)
        }
        return failureText(result)
    }

    public func remove(date: String, slot: Components.Schemas.Slot) async -> String? {
        let result = await write(refreshing: DateRange(from: date, to: date)) {
            try await plan.clearSlot(date: date, slot: slot)
        }
        return failureText(result)
    }

    /// Removes every snack of the day (the API cannot address a single one).
    public func clearSnacks(date: String) async -> String? {
        await remove(date: date, slot: .snack)
    }

    /// Applies one of my templates. `.needsConfirmation` means `409 plan_conflict`: ask, then call again with
    /// `overwrite: true`. The applied range is refreshed afterwards.
    public func apply(templateID: String, dayCount: Int, startDate: String, overwrite: Bool) async -> ApplyOutcome {
        let end = localDay.addDays(startDate, max(dayCount, 1) - 1)
        let result = await write(refreshing: DateRange(from: startDate, to: end)) {
            try await plan.apply(templateID: templateID, startDate: startDate, overwrite: overwrite)
        }
        switch result {
        case .success:
            return .applied
        case .failure(let error):
            if let planError = error as? PlanError, planError == .conflict { return .needsConfirmation }
            return .failed(message(for: error))
        }
    }

    // MARK: Internals

    private struct WriteInProgress: Error {}

    private func write(refreshing target: DateRange, _ operation: () async throws -> Void) async -> Result<Void, Error> {
        guard busyRange == nil else { return .failure(WriteInProgress()) }
        busyRange = target
        notice = nil
        defer { busyRange = nil }
        do {
            try await operation()
        } catch {
            return .failure(error)
        }
        do {
            try await plan.refresh(from: target.from, to: target.to)
        } catch {
            isStale = true
            notice = Self.refreshNotice
        }
        show(await plan.cached(from: range.from, to: range.to))
        return .success(())
    }

    private func failureText(_ result: Result<Void, Error>) -> String? {
        if case .failure(let error) = result { return message(for: error) }
        return nil
    }

    private func message(for error: Error) -> String {
        error is WriteInProgress ? Self.busyMessage : ErrorText.message(for: error)
    }

    private func show(_ snapshot: PlanSnapshot) {
        days = Dictionary(snapshot.days.map { ($0.date, $0) }, uniquingKeysWith: { first, _ in first })
        targets = snapshot.targets
    }
}
