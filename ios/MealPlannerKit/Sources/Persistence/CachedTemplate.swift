import API
import Foundation
import SwiftData

/// Internal to this module. A template list row, plus the full generated `DietTemplate` as JSON once opened
/// (its slots are never queried, so they are not rows).
@Model
final class CachedTemplate {
    @Attribute(.unique) var id: String
    var scopeRaw: String
    var name: String
    var dayCount: Int
    var sharedWithPartner: Bool
    var createdAt: Date
    var updatedAt: Date
    /// `nil` means "summary only, not opened yet".
    var detailJSON: Data?

    init(summary: Components.Schemas.DietTemplateSummary, scopeRaw: String) {
        self.id = summary.id
        self.scopeRaw = scopeRaw
        self.name = summary.name
        self.dayCount = summary.dayCount
        self.sharedWithPartner = summary.sharedWithPartner
        self.createdAt = summary.createdAt
        self.updatedAt = summary.updatedAt
    }

    func apply(_ summary: Components.Schemas.DietTemplateSummary) {
        name = summary.name
        dayCount = summary.dayCount
        sharedWithPartner = summary.sharedWithPartner
        createdAt = summary.createdAt
        updatedAt = summary.updatedAt
    }

    var summary: Components.Schemas.DietTemplateSummary {
        .init(id: id, name: name, dayCount: dayCount, sharedWithPartner: sharedWithPartner, createdAt: createdAt, updatedAt: updatedAt)
    }

    /// The full template, or `nil` if it has not been opened (or its stored JSON can no longer be decoded, in which
    /// case refetching is the right answer).
    var template: Components.Schemas.DietTemplate? {
        guard let detailJSON else { return nil }
        return try? JSONDecoder().decode(Components.Schemas.DietTemplate.self, from: detailJSON)
    }
}
