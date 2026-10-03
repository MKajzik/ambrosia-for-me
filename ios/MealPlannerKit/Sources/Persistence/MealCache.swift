import API
import Foundation
import SwiftData

/// The meal cache. Takes and returns generated value types; `@Model` objects never leave this actor.
/// Best-effort by design: it is rebuildable from the API, so a failed save is dropped, not surfaced.
@ModelActor
public actor MealCache {
    public func summaries(scope: MealScope) -> [Components.Schemas.MealSummary] {
        allRows()
            .filter { $0.scopeRaw == scope.rawValue }
            .sorted { $0.name.localizedCaseInsensitiveCompare($1.name) == .orderedAscending }
            .map(\.summary)
    }

    /// The full meal, or `nil` if it has only ever been seen as a list row.
    public func meal(id: String) -> Components.Schemas.Meal? {
        allRows().first { $0.id == id }?.meal
    }

    /// Upserts by id keeping existing detail, inserts new rows, and deletes ids absent from `summaries`
    /// within `scope` only, in one save. Mine and Partner's never collide.
    public func replaceSummaries(_ summaries: [Components.Schemas.MealSummary], scope: MealScope) {
        let all = allRows()
        let byID = Dictionary(all.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        for summary in summaries {
            if let row = byID[summary.id] {
                row.apply(summary)
                row.scopeRaw = scope.rawValue
            } else {
                modelContext.insert(CachedMeal(summary: summary, scopeRaw: scope.rawValue))
            }
        }
        let incoming = Set(summaries.map(\.id))
        for row in all where row.scopeRaw == scope.rawValue && !incoming.contains(row.id) {
            modelContext.delete(row)
        }
        save()
    }

    /// Stores a full meal (an API answer). A new row's scope follows ownership; an existing row keeps its scope.
    public func store(_ meal: Components.Schemas.Meal) {
        let row: CachedMeal
        if let existing = allRows().first(where: { $0.id == meal.id }) {
            row = existing
        } else {
            row = CachedMeal(
                summary: .init(id: meal.id, name: meal.name, notes: meal.notes, servings: meal.servings,
                               sharedWithPartner: meal.sharedWithPartner, createdAt: meal.createdAt, updatedAt: meal.updatedAt),
                scopeRaw: (meal.isOwner ? MealScope.mine : .partner).rawValue
            )
            modelContext.insert(row)
        }
        row.apply(meal)
        for old in row.ingredients { modelContext.delete(old) }
        row.ingredients = meal.ingredients.map { CachedMealIngredient(line: $0) }
        save()
    }

    public func remove(id: String) {
        for row in allRows() where row.id == id { modelContext.delete(row) }
        save()
    }

    public func clear(scope: MealScope) {
        for row in allRows() where row.scopeRaw == scope.rawValue { modelContext.delete(row) }
        save()
    }

    public func clearAll() {
        for row in allRows() { modelContext.delete(row) }
        save()
    }

    private func allRows() -> [CachedMeal] {
        (try? modelContext.fetch(FetchDescriptor<CachedMeal>())) ?? []
    }

    private func save() {
        try? modelContext.save()
    }
}
