import Testing
import API
@testable import Auth
@testable import Features

@Suite
@MainActor
struct AuthViewModelTests {
    @Test("A successful sign-in reports the signed-in user and clears any error")
    func signInSuccess() async throws {
        let transport = StubTransport {
            (200, """
            {
              "access_token": "a1", "refresh_token": "r1", "token_type": "Bearer", "expires_in": 900,
              "user": {
                "id": "11111111-1111-1111-1111-111111111111", "email": "person@example.com",
                "display_name": "Person", "created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z"
              }
            }
            """)
        }
        let repository = AuthRepository(client: makeAuthlessClient(transport: transport), tokenStore: InMemoryTokenStore())
        let viewModel = AuthViewModel(authRepository: repository)

        viewModel.email = "person@example.com"
        viewModel.password = "correct-horse-battery"
        let user = await viewModel.signIn()

        #expect(user?.email == "person@example.com")
        #expect(viewModel.errorMessage == nil)
        #expect(viewModel.isSubmitting == false)
    }

    @Test("Invalid credentials set a generic error message, never distinguishing email from password")
    func signInInvalidCredentials() async throws {
        let transport = StubTransport {
            (401, #"{"type":"about:blank","title":"Unauthorized","status":401,"code":"unauthorized"}"#)
        }
        let repository = AuthRepository(client: makeAuthlessClient(transport: transport), tokenStore: InMemoryTokenStore())
        let viewModel = AuthViewModel(authRepository: repository)

        viewModel.email = "nobody@example.com"
        viewModel.password = "wrong"
        let user = await viewModel.signIn()

        #expect(user == nil)
        #expect(viewModel.errorMessage == "Invalid email or password.")
    }

    @Test("A taken email sets a distinct message on the register flow")
    func registerEmailTaken() async throws {
        let transport = StubTransport {
            (409, #"{"type":"about:blank","title":"Conflict","status":409,"code":"email_taken"}"#)
        }
        let repository = AuthRepository(client: makeAuthlessClient(transport: transport), tokenStore: InMemoryTokenStore())
        let viewModel = AuthViewModel(authRepository: repository)

        viewModel.email = "taken@example.com"
        viewModel.password = "correct-horse-battery"
        viewModel.displayName = "Person"
        let user = await viewModel.register()

        #expect(user == nil)
        #expect(viewModel.errorMessage == "That email is already registered.")
    }
}
