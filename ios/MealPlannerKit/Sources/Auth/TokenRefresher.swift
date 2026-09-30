import API

/// Single-flights concurrent refresh attempts so that N requests racing an expired access
/// token trigger exactly one `POST /auth/refresh` call, matching the backend rule that
/// replaying an already-used refresh token revokes the whole session family
/// (`backend/CLAUDE.md`): if every caller refreshed independently, the first response would
/// invalidate the token the others are about to present, and they would all be signed out.
public actor TokenRefresher {
    private let refreshClient: Client
    private let tokenStore: any TokenStore
    private var inFlight: Task<String, Error>?

    public init(refreshClient: Client, tokenStore: any TokenStore) {
        self.refreshClient = refreshClient
        self.tokenStore = tokenStore
    }

    public func currentAccessToken() -> String? {
        tokenStore.accessToken
    }

    public func refreshAccessToken() async throws -> String {
        if let inFlight {
            return try await inFlight.value
        }
        let task = Task { try await performRefresh() }
        inFlight = task
        defer { inFlight = nil }
        return try await task.value
    }

    private func performRefresh() async throws -> String {
        guard let refreshToken = tokenStore.refreshToken else {
            throw AuthError.signedOut
        }
        let response = try await refreshClient.refreshSession(
            .init(body: .json(.init(refreshToken: refreshToken)))
        )
        switch response {
        case .ok(let ok):
            let auth = try ok.body.json
            tokenStore.save(accessToken: auth.accessToken, refreshToken: auth.refreshToken)
            return auth.accessToken
        case .badRequest, .unauthorized:
            tokenStore.clear()
            throw AuthError.signedOut
        case .tooManyRequests:
            throw AuthError.rateLimited
        case .internalServerError:
            throw AuthError.server("The server had a problem refreshing the session.")
        case .undocumented(let statusCode, _):
            throw AuthError.server("Unexpected response (\(statusCode)) while refreshing the session.")
        }
    }
}
