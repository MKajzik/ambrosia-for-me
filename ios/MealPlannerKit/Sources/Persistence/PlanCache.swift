import API
import Foundation
import SwiftData

/// The plan cache: one row per date. Takes and returns generated value types; best-effort like `MealCache`
/// (rebuildable from the API, so a failed save is dropped, not surfaced).
@ModelActor
public actor PlanCache {
    /// Bumped whenever the targets are set from outside the plan (`setTargets`). A `GET /plan` answer read before that
    /// carries older targets than the cache holds, so `replace` skips them if this moved while the request ran.
    private var targetsGeneration = 0

    public func targetsEpoch() -> Int { targetsGeneration }

    /// The cached dates in `[from, to]`, in date order. A date that was never fetched is absent.
    public func days(from: String, to: String) -> [Components.Schemas.DailyTotal] {
        allDays()
            .filter { $0.date >= from && $0.date <= to }
            .sorted { $0.date < $1.date }
            .compactMap { try? JSONDecoder().decode(Components.Schemas.DailyTotal.self, from: $0.json) }
    }

    public func targets() -> Components.Schemas.Targets? {
        guard let row = (try? modelContext.fetch(FetchDescriptor<CachedTargets>()))?.first else { return nil }
        return try? JSONDecoder().decode(Components.Schemas.Targets.self, from: row.json)
    }

    /// Writes only the targets row. The Today rings read targets from here; `PATCH /me` answers with the new ones, so
    /// they are stored without refetching the plan.
    public func setTargets(_ targets: Components.Schemas.Targets) {
        targetsGeneration += 1
        guard let json = try? JSONEncoder().encode(targets) else { return }
        if let row = (try? modelContext.fetch(FetchDescriptor<CachedTargets>()))?.first {
            row.json = json
        } else {
            modelContext.insert(CachedTargets(json: json))
        }
        try? modelContext.save()
    }

    /// Upserts each given date and the targets in one save. `GET /plan` returns every date in the range it was
    /// asked for, so a refresh replaces exactly that range; other dates are untouched. `targetsEpoch` is the value of
    /// `targetsEpoch()` from before the request; when `setTargets` ran since, the answer's targets are dropped.
    public func replace(days: [Components.Schemas.DailyTotal], targets: Components.Schemas.Targets, targetsEpoch: Int? = nil) {
        let existing = Dictionary(allDays().map { ($0.date, $0) }, uniquingKeysWith: { first, _ in first })
        for day in days {
            guard let json = try? JSONEncoder().encode(day) else { continue }
            if let row = existing[day.date] {
                row.json = json
            } else {
                modelContext.insert(CachedPlanDay(date: day.date, json: json))
            }
        }
        if targetsEpoch == nil || targetsEpoch == targetsGeneration, let json = try? JSONEncoder().encode(targets) {
            if let row = (try? modelContext.fetch(FetchDescriptor<CachedTargets>()))?.first {
                row.json = json
            } else {
                modelContext.insert(CachedTargets(json: json))
            }
        }
        try? modelContext.save()
    }

    public func clearAll() {
        targetsGeneration += 1
        for row in allDays() { modelContext.delete(row) }
        for row in (try? modelContext.fetch(FetchDescriptor<CachedTargets>())) ?? [] { modelContext.delete(row) }
        try? modelContext.save()
    }

    private func allDays() -> [CachedPlanDay] {
        (try? modelContext.fetch(FetchDescriptor<CachedPlanDay>())) ?? []
    }
}
