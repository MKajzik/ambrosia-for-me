import Foundation
import SwiftData

/// Internal to this module: `@Model` objects never leave `PlanCache`. A day is stored as the generated `DailyTotal`
/// JSON (its entries and the server's per-day nutrition), because nothing queries inside it.
@Model
final class CachedPlanDay {
    @Attribute(.unique) var date: String
    var json: Data

    init(date: String, json: Data) {
        self.date = date
        self.json = json
    }
}

/// The caller's targets, one row.
@Model
final class CachedTargets {
    @Attribute(.unique) var key: String
    var json: Data

    init(json: Data) {
        self.key = "me"
        self.json = json
    }
}
