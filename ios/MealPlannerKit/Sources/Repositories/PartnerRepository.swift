import API
import Persistence

/// The partnership. `status()` is used by the Meals, Plan, Shopping and Profile tabs; when a `ProfileCache` is given it
/// also remembers the answer, so Profile opens with the last known state. Writes are online-only.
public struct PartnerRepository: Sendable {
    private let client: Client
    private let cache: ProfileCache?

    public init(client: Client, cache: ProfileCache? = nil) {
        self.client = client
        self.cache = cache
    }

    /// The caller's partnership, or `nil` on `404 partner_not_linked` (no partner, no pending invite,
    /// or an expired one). Callers treat only `status == .active` as linked.
    public func status() async throws -> Components.Schemas.Partnership? {
        let response = try await unwrapping { try await client.getPartner(.init()) }
        switch response {
        case .ok(let ok):
            let partnership = try ok.body.json
            await cache?.store(partnership: partnership)
            return partnership
        case .notFound:
            await cache?.store(partnership: nil)
            return nil
        case .unauthorized: throw MealsError.unauthorized
        case .tooManyRequests: throw MealsError.rateLimited
        case .internalServerError: throw MealsError.server("The server had a problem loading your partnership.")
        case .undocumented(let status, _): throw MealsError.unexpected(status)
        }
    }

    /// What was last known, without a request. `.unknown` when there is no cache or nothing was ever fetched.
    public func cachedStatus() async -> CachedPartnership { await cache?.partnership() ?? .unknown }

    /// Creates an invite. The code is returned once; the server keeps only its hash.
    public func createInvite() async throws -> Components.Schemas.PartnerInvite {
        let response = try await unwrapping { try await client.createPartnerInvite(.init()) }
        switch response {
        case .created(let created): return try created.body.json
        case .unauthorized: throw PartnerError.unauthorized
        case .conflict(let r): throw PartnerError.conflict(r.problem)
        case .tooManyRequests: throw PartnerError.rateLimited
        case .internalServerError: throw PartnerError.server("The server had a problem creating the invite.")
        case .undocumented(let status, _): throw PartnerError.unexpected(status)
        }
    }

    /// Links with whoever issued `code`. The server ignores case, spaces and dashes, so the text goes as given.
    public func accept(code: String) async throws -> Components.Schemas.Partnership {
        let response = try await unwrapping { try await client.acceptPartnerInvite(.init(body: .json(.init(code: code)))) }
        switch response {
        case .ok(let ok):
            let partnership = try ok.body.json
            await cache?.store(partnership: partnership)
            return partnership
        case .badRequest(let r): throw PartnerError.validation(r.problem)
        case .unauthorized: throw PartnerError.unauthorized
        case .notFound: throw PartnerError.inviteInvalid
        case .conflict(let r): throw PartnerError.conflict(r.problem)
        case .tooManyRequests: throw PartnerError.rateLimited
        case .internalServerError: throw PartnerError.server("The server had a problem linking your accounts.")
        case .undocumented(let status, _): throw PartnerError.unexpected(status)
        }
    }

    /// Ends the link or cancels a pending invite. Either side may do it. Nothing to end (`404`) counts as done.
    public func unlink() async throws {
        let response = try await unwrapping { try await client.unlinkPartner(.init()) }
        switch response {
        case .noContent, .notFound: await cache?.store(partnership: nil)
        case .unauthorized: throw PartnerError.unauthorized
        case .tooManyRequests: throw PartnerError.rateLimited
        case .internalServerError: throw PartnerError.server("The server had a problem ending the link.")
        case .undocumented(let status, _): throw PartnerError.unexpected(status)
        }
    }
}
