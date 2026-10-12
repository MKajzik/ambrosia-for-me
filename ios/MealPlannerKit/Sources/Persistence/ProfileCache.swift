import API
import Foundation
import SwiftData

/// What the partnership cache knows. A pending invite is a partnership too (it is not "linked").
public enum CachedPartnership: Equatable, Sendable {
    /// Never fetched on this device.
    case unknown
    /// Fetched, and there is no partner and no pending invite.
    case none
    case present(Components.Schemas.Partnership)
}

/// The Profile tab's cache: the signed-in user and the partnership. Takes and returns generated value types;
/// best-effort like the other caches (rebuildable from the API, so a failed save is dropped, not surfaced).
@ModelActor
public actor ProfileCache {
    public func user() -> Components.Schemas.User? {
        guard let row = userRow() else { return nil }
        return try? JSONDecoder().decode(Components.Schemas.User.self, from: row.json)
    }

    public func store(user: Components.Schemas.User) {
        guard let json = try? JSONEncoder().encode(user) else { return }
        if let row = userRow() {
            row.json = json
        } else {
            modelContext.insert(CachedProfileUser(json: json))
        }
        try? modelContext.save()
    }

    public func partnership() -> CachedPartnership {
        guard let row = partnershipRow() else { return .unknown }
        guard let json = row.json else { return .none }
        guard let value = try? JSONDecoder().decode(Components.Schemas.Partnership.self, from: json) else { return .unknown }
        return .present(value)
    }

    /// `nil` records "fetched, and there is none".
    public func store(partnership: Components.Schemas.Partnership?) {
        let json = partnership.flatMap { try? JSONEncoder().encode($0) }
        if let row = partnershipRow() {
            row.json = json
        } else {
            modelContext.insert(CachedPartnershipRow(json: json))
        }
        try? modelContext.save()
    }

    public func clearAll() {
        for row in (try? modelContext.fetch(FetchDescriptor<CachedProfileUser>())) ?? [] { modelContext.delete(row) }
        for row in (try? modelContext.fetch(FetchDescriptor<CachedPartnershipRow>())) ?? [] { modelContext.delete(row) }
        try? modelContext.save()
    }

    private func userRow() -> CachedProfileUser? {
        (try? modelContext.fetch(FetchDescriptor<CachedProfileUser>()))?.first
    }

    private func partnershipRow() -> CachedPartnershipRow? {
        (try? modelContext.fetch(FetchDescriptor<CachedPartnershipRow>()))?.first
    }
}
