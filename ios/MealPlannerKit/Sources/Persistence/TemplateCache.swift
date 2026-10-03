import API
import Foundation
import SwiftData

/// The diet-template cache. Mirrors `MealCache` (same Mine / Partner's scope type, `MealScope`, reused rather than
/// renamed). Takes and returns generated value types; best-effort, rebuildable from the API.
@ModelActor
public actor TemplateCache {
    public func summaries(scope: MealScope) -> [Components.Schemas.DietTemplateSummary] {
        allRows()
            .filter { $0.scopeRaw == scope.rawValue }
            .sorted { $0.name.localizedCaseInsensitiveCompare($1.name) == .orderedAscending }
            .map(\.summary)
    }

    /// The full template, or `nil` if it has only ever been seen as a list row.
    public func template(id: String) -> Components.Schemas.DietTemplate? {
        allRows().first { $0.id == id }?.template
    }

    /// Upserts by id keeping existing detail, inserts new rows, and deletes ids absent from `summaries` within
    /// `scope` only, in one save.
    public func replaceSummaries(_ summaries: [Components.Schemas.DietTemplateSummary], scope: MealScope) {
        let all = allRows()
        let byID = Dictionary(all.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        for summary in summaries {
            if let row = byID[summary.id] {
                row.apply(summary)
                row.scopeRaw = scope.rawValue
            } else {
                modelContext.insert(CachedTemplate(summary: summary, scopeRaw: scope.rawValue))
            }
        }
        let incoming = Set(summaries.map(\.id))
        for row in all where row.scopeRaw == scope.rawValue && !incoming.contains(row.id) {
            modelContext.delete(row)
        }
        try? modelContext.save()
    }

    /// Stores a full template (an API answer). A new row's scope follows ownership; an existing row keeps its scope.
    public func store(_ template: Components.Schemas.DietTemplate) {
        let summary = Components.Schemas.DietTemplateSummary(
            id: template.id, name: template.name, dayCount: template.dayCount,
            sharedWithPartner: template.sharedWithPartner, createdAt: template.createdAt, updatedAt: template.updatedAt
        )
        let row: CachedTemplate
        if let existing = allRows().first(where: { $0.id == template.id }) {
            row = existing
            row.apply(summary)
        } else {
            row = CachedTemplate(summary: summary, scopeRaw: (template.isOwner ? MealScope.mine : .partner).rawValue)
            modelContext.insert(row)
        }
        row.detailJSON = try? JSONEncoder().encode(template)
        try? modelContext.save()
    }

    public func remove(id: String) {
        for row in allRows() where row.id == id { modelContext.delete(row) }
        try? modelContext.save()
    }

    public func clear(scope: MealScope) {
        for row in allRows() where row.scopeRaw == scope.rawValue { modelContext.delete(row) }
        try? modelContext.save()
    }

    public func clearAll() {
        for row in allRows() { modelContext.delete(row) }
        try? modelContext.save()
    }

    private func allRows() -> [CachedTemplate] {
        (try? modelContext.fetch(FetchDescriptor<CachedTemplate>())) ?? []
    }
}
