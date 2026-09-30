import Testing
import API
@testable import Auth

@Suite
struct TokenRefresherTests {
    static let authResponseJSON = """
    {
      "access_token": "new-access-token",
      "refresh_token": "new-refresh-token",
      "token_type": "Bearer",
      "expires_in": 900,
      "user": {
        "id": "11111111-1111-1111-1111-111111111111",
        "email": "person@example.com",
        "display_name": "Person",
        "created_at": "2026-01-01T00:00:00Z",
        "updated_at": "2026-01-01T00:00:00Z"
      }
    }
    """

    @Test("Concurrent refreshes make exactly one network call and all callers get the new token")
    func singleFlight() async throws {
        let transport = StubTransport { (200, Self.authResponseJSON) }
        let client = makeAuthlessClient(transport: transport)
        let tokenStore = InMemoryTokenStore(accessToken: "old-access-token", refreshToken: "old-refresh-token")
        let refresher = TokenRefresher(refreshClient: client, tokenStore: tokenStore)

        async let first = refresher.refreshAccessToken()
        async let second = refresher.refreshAccessToken()
        async let third = refresher.refreshAccessToken()
        let results = try await [first, second, third]

        #expect(results == ["new-access-token", "new-access-token", "new-access-token"])
        #expect(await transport.callCount == 1)
        #expect(tokenStore.accessToken == "new-access-token")
        #expect(tokenStore.refreshToken == "new-refresh-token")
    }

    @Test("A rejected refresh token clears the Keychain and reports signed out")
    func rejectedRefreshToken() async throws {
        let transport = StubTransport {
            (401, #"{"type":"about:blank","title":"Unauthorized","status":401,"code":"unauthorized"}"#)
        }
        let client = makeAuthlessClient(transport: transport)
        let tokenStore = InMemoryTokenStore(accessToken: "old-access-token", refreshToken: "used-refresh-token")
        let refresher = TokenRefresher(refreshClient: client, tokenStore: tokenStore)

        await #expect(throws: AuthError.signedOut) {
            try await refresher.refreshAccessToken()
        }
        #expect(tokenStore.accessToken == nil)
        #expect(tokenStore.refreshToken == nil)
    }

    @Test("No stored refresh token reports signed out without a network call")
    func noRefreshToken() async throws {
        let transport = StubTransport { (200, Self.authResponseJSON) }
        let client = makeAuthlessClient(transport: transport)
        let tokenStore = InMemoryTokenStore()
        let refresher = TokenRefresher(refreshClient: client, tokenStore: tokenStore)

        await #expect(throws: AuthError.signedOut) {
            try await refresher.refreshAccessToken()
        }
        #expect(await transport.callCount == 0)
    }
}
