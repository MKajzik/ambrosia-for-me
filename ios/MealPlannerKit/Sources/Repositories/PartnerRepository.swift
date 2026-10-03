import API

public struct PartnerRepository: Sendable {
    private let client: Client

    public init(client: Client) { self.client = client }

    /// The caller's partnership, or `nil` on `404 partner_not_linked` (no partner, no pending invite,
    /// or an expired one). Callers treat only `status == .active` as linked.
    public func status() async throws -> Components.Schemas.Partnership? {
        let response = try await unwrapping { try await client.getPartner(.init()) }
        switch response {
        case .ok(let ok): return try ok.body.json
        case .notFound: return nil
        case .unauthorized: throw MealsError.unauthorized
        case .tooManyRequests: throw MealsError.rateLimited
        case .internalServerError: throw MealsError.server("The server had a problem loading your partnership.")
        case .undocumented(let status, _): throw MealsError.unexpected(status)
        }
    }
}
