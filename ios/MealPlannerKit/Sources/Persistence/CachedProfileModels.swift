import Foundation
import SwiftData

/// Internal to this module: `@Model` objects never leave `ProfileCache`.

/// The signed-in user, one row, as the generated `User` JSON.
@Model
final class CachedProfileUser {
    @Attribute(.unique) var key: String
    var json: Data

    init(json: Data) {
        self.key = "me"
        self.json = json
    }
}

/// The partnership, one row. `json == nil` means "fetched, and there is none"; no row at all means "never fetched".
@Model
final class CachedPartnershipRow {
    @Attribute(.unique) var key: String
    var json: Data?

    init(json: Data?) {
        self.key = "me"
        self.json = json
    }
}
