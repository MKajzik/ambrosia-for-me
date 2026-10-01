import Testing
import API
@testable import Auth

@Suite
struct AuthRepositoryTests {
    static func authResponseJSON(access: String = "a1", refresh: String = "r1") -> String {
        """
        {
          "access_token": "\(access)",
          "refresh_token": "\(refresh)",
          "token_type": "Bearer",
          "expires_in": 900,
          "user": {
            "id": "11111111-1111-1111-1111-111111111111",
            "email": "person@example.com",
            "display_name": "Person",
            "created_at": "2026-01-01T00:00:00.000000Z",
            "updated_at": "2026-01-01T00:00:00.000000Z"
          }
        }
        """
    }

    @Test("Registering saves the returned tokens and returns the user")
    func registerSuccess() async throws {
        let transport = StubTransport { (201, Self.authResponseJSON()) }
        let tokenStore = InMemoryTokenStore()
        let repository = AuthRepository(
            client: makeAuthlessClient(transport: transport),
            tokenStore: tokenStore
        )

        let user = try await repository.register(email: "person@example.com", password: "correct-horse-battery", displayName: "Person")

        #expect(user.email == "person@example.com")
        #expect(tokenStore.accessToken == "a1")
        #expect(tokenStore.refreshToken == "r1")
    }

    @Test("Registering with a taken email throws emailTaken")
    func registerEmailTaken() async throws {
        let transport = StubTransport {
            (409, #"{"type":"about:blank","title":"Conflict","status":409,"code":"email_taken"}"#)
        }
        let repository = AuthRepository(
            client: makeAuthlessClient(transport: transport),
            tokenStore: InMemoryTokenStore()
        )

        await #expect(throws: AuthError.emailTaken) {
            try await repository.register(email: "person@example.com", password: "correct-horse-battery", displayName: "Person")
        }
    }

    @Test("A too-short password maps the real errors[] shape to a friendly message, not the bare problem title")
    func registerValidationFailedTooShortPassword() async throws {
        // The real API's 400 validation_failed carries only `errors`, never a `detail` string
        // (confirmed against the running backend) — this is the exact shape it sends.
        let transport = StubTransport {
            (400, #"{"type":"about:blank","title":"Bad Request","status":400,"code":"validation_failed","errors":[{"field":"password","code":"too_short"}]}"#)
        }
        let repository = AuthRepository(
            client: makeAuthlessClient(transport: transport),
            tokenStore: InMemoryTokenStore()
        )

        await #expect(throws: AuthError.validationFailed("Password must be at least 10 characters.")) {
            try await repository.register(email: "person@example.com", password: "short", displayName: "Person")
        }
    }

    @Test("Multiple field errors are joined into one message")
    func registerValidationFailedMultipleFields() async throws {
        let transport = StubTransport {
            (400, #"{"type":"about:blank","title":"Bad Request","status":400,"code":"validation_failed","errors":[{"field":"email","code":"invalid_format"},{"field":"display_name","code":"required"}]}"#)
        }
        let repository = AuthRepository(
            client: makeAuthlessClient(transport: transport),
            tokenStore: InMemoryTokenStore()
        )

        await #expect(throws: AuthError.validationFailed("Enter a valid email address. Enter a display name.")) {
            try await repository.register(email: "not-an-email", password: "correct-horse-battery", displayName: "")
        }
    }

    @Test("A 400 with no errors array falls back to the problem's detail, then title")
    func registerValidationFailedNoErrorsArray() async throws {
        let transport = StubTransport {
            (400, #"{"type":"about:blank","title":"Bad Request","status":400,"detail":"malformed JSON body","code":"bad_request"}"#)
        }
        let repository = AuthRepository(
            client: makeAuthlessClient(transport: transport),
            tokenStore: InMemoryTokenStore()
        )

        await #expect(throws: AuthError.validationFailed("malformed JSON body")) {
            try await repository.register(email: "person@example.com", password: "short", displayName: "Person")
        }
    }

    @Test("Signing in saves the returned tokens and returns the user")
    func loginSuccess() async throws {
        let transport = StubTransport { (200, Self.authResponseJSON()) }
        let tokenStore = InMemoryTokenStore()
        let repository = AuthRepository(
            client: makeAuthlessClient(transport: transport),
            tokenStore: tokenStore
        )

        let user = try await repository.login(email: "person@example.com", password: "correct-horse-battery")

        #expect(user.displayName == "Person")
        #expect(tokenStore.accessToken == "a1")
    }

    @Test("Wrong password or unknown email both throw the same invalidCredentials error")
    func loginInvalidCredentials() async throws {
        let transport = StubTransport {
            (401, #"{"type":"about:blank","title":"Unauthorized","status":401,"code":"unauthorized"}"#)
        }
        let repository = AuthRepository(
            client: makeAuthlessClient(transport: transport),
            tokenStore: InMemoryTokenStore()
        )

        await #expect(throws: AuthError.invalidCredentials) {
            try await repository.login(email: "nobody@example.com", password: "wrong-password")
        }
    }

    @Test("Logging out revokes the session and clears the Keychain even if the network call fails")
    func logoutClearsLocalStateOnFailure() async throws {
        let transport = StubTransport { (500, #"{"type":"about:blank","title":"Error","status":500,"code":"internal_error"}"#) }
        let tokenStore = InMemoryTokenStore(accessToken: "a1", refreshToken: "r1")
        let repository = AuthRepository(
            client: makeAuthlessClient(transport: transport),
            tokenStore: tokenStore
        )

        await repository.logout()

        #expect(tokenStore.accessToken == nil)
        #expect(tokenStore.refreshToken == nil)
    }
}
