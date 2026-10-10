import API
import Foundation
import Observation
import Persistence
import Repositories

/// The partner connection on Profile. Reads render the cached state, await the refresh, re-read. Every change needs a
/// connection. The invite code is held here, in memory, only: the server returns it once and the app never stores it.
@Observable
@MainActor
public final class PartnerViewModel {
    public enum Phase: Equatable, Sendable {
        /// Nothing cached and no answer yet.
        case loading
        case none
        case pending(expiresAt: Date?)
        case linked(name: String, since: Date?)
    }

    public private(set) var phase: Phase = .loading
    public private(set) var invite: Components.Schemas.PartnerInvite?
    public private(set) var isStale = false
    public private(set) var loadError: String?
    public private(set) var isBusy = false
    /// The accept field's text. Kept after a failed attempt so a retry is one tap.
    public var codeText = ""
    public private(set) var acceptError: String?
    public var alertMessage: String?

    @ObservationIgnored private let partner: PartnerRepository

    public init(partner: PartnerRepository) {
        self.partner = partner
    }

    public func appear() async {
        switch await partner.cachedStatus() {
        case .unknown: break
        case .none: phase = .none
        case .present(let partnership): phase = Self.phase(for: partnership)
        }
        do {
            let status = try await partner.status()
            phase = status.map(Self.phase(for:)) ?? .none
            isStale = false
            loadError = nil
        } catch {
            isStale = true
            loadError = phase == .loading ? ErrorText.message(for: error) : nil
        }
    }

    public func createInvite() async {
        guard !isBusy else { return }
        isBusy = true
        defer { isBusy = false }
        do {
            let created = try await partner.createInvite()
            invite = created
            phase = .pending(expiresAt: created.expiresAt)
            // Keeps the cached status in step; the screen already shows what this answer means.
            _ = try? await partner.status()
        } catch {
            alertMessage = ErrorText.message(for: error)
        }
    }

    public func accept() async {
        guard !isBusy else { return }
        let code = codeText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !code.isEmpty else {
            acceptError = "Enter the code your partner sent you."
            return
        }
        acceptError = nil
        isBusy = true
        defer { isBusy = false }
        do {
            let linked = try await partner.accept(code: code)
            phase = Self.phase(for: linked)
            codeText = ""
            invite = nil
        } catch PartnerError.inviteInvalid {
            acceptError = ErrorText.message(for: PartnerError.inviteInvalid)
        } catch PartnerError.alreadyLinked {
            acceptError = ErrorText.message(for: PartnerError.alreadyLinked)
        } catch {
            alertMessage = ErrorText.message(for: error)
        }
    }

    /// Ends the link, or cancels a pending invite.
    public func unlink() async {
        guard !isBusy else { return }
        isBusy = true
        defer { isBusy = false }
        do {
            try await partner.unlink()
            phase = .none
            invite = nil
        } catch {
            alertMessage = ErrorText.message(for: error)
        }
    }

    private static func phase(for partnership: Components.Schemas.Partnership) -> Phase {
        switch partnership.status {
        case .active: .linked(name: partnership.displayName ?? "your partner", since: partnership.linkedAt)
        case .pending: .pending(expiresAt: partnership.expiresAt)
        }
    }
}
