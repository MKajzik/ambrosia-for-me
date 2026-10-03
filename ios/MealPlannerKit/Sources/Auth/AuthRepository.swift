import API

public struct AuthRepository: Sendable {
    private let client: Client
    private let tokenStore: any TokenStore

    public init(client: Client, tokenStore: any TokenStore) {
        self.client = client
        self.tokenStore = tokenStore
    }

    public func register(email: String, password: String, displayName: String) async throws -> Components.Schemas.User {
        let response = try await client.registerUser(
            .init(body: .json(.init(email: email, password: password, displayName: displayName)))
        )
        switch response {
        case .created(let created):
            let auth = try created.body.json
            tokenStore.save(accessToken: auth.accessToken, refreshToken: auth.refreshToken)
            return auth.user
        case .badRequest(let badRequest):
            let problem = try badRequest.body.applicationProblemJson
            throw AuthError.validationFailed(Self.validationMessage(for: problem))
        case .conflict:
            throw AuthError.emailTaken
        case .tooManyRequests:
            throw AuthError.rateLimited
        case .internalServerError:
            throw AuthError.server("The server had a problem creating the account.")
        case .undocumented(let statusCode, _):
            throw AuthError.server("Unexpected response (\(statusCode)) while creating the account.")
        }
    }

    public func login(email: String, password: String) async throws -> Components.Schemas.User {
        let response = try await client.loginUser(.init(body: .json(.init(email: email, password: password))))
        switch response {
        case .ok(let ok):
            let auth = try ok.body.json
            tokenStore.save(accessToken: auth.accessToken, refreshToken: auth.refreshToken)
            return auth.user
        case .badRequest(let badRequest):
            let problem = try badRequest.body.applicationProblemJson
            throw AuthError.validationFailed(Self.validationMessage(for: problem))
        case .unauthorized:
            // The API answers an unknown email and a wrong password identically
            // (backend/CLAUDE.md); the UI must never imply which one was wrong.
            throw AuthError.invalidCredentials
        case .tooManyRequests:
            throw AuthError.rateLimited
        case .internalServerError:
            throw AuthError.server("The server had a problem signing you in.")
        case .undocumented(let statusCode, _):
            throw AuthError.server("Unexpected response (\(statusCode)) while signing in.")
        }
    }

    /// Clears the stored tokens at once and returns the refresh token that was stored, so the caller can
    /// revoke it on the server without making the user wait for the network.
    public func endLocalSession() -> String? {
        let refreshToken = tokenStore.refreshToken
        tokenStore.clear()
        return refreshToken
    }

    /// Best-effort server-side revoke (`/auth/logout` needs no bearer token).
    public func revoke(refreshToken: String) async {
        _ = try? await client.logoutUser(.init(body: .json(.init(refreshToken: refreshToken))))
    }

    public func logout() async {
        if let refreshToken = endLocalSession() {
            await revoke(refreshToken: refreshToken)
        }
    }

    /// A `400 validation_failed` carries per-field `errors`, never a `detail` string (confirmed
    /// against the real API, not assumed from the schema) — `problem.detail ?? problem.title`
    /// alone showed the user a bare "Bad Request" for something as ordinary as a short password.
    /// Named fields match `openapi.yaml`'s request schemas (`email`, `password`, `display_name`).
    private static func validationMessage(for problem: Components.Schemas.Problem) -> String {
        guard let errors = problem.errors, !errors.isEmpty else {
            return problem.detail ?? problem.title
        }
        let messages = errors.map { fieldErrorMessage(field: $0.field, code: $0.code) }
        return messages.joined(separator: " ")
    }

    private static func fieldErrorMessage(field: String, code: String) -> String {
        switch (field, code) {
        case ("email", "required"): return "Enter your email address."
        case ("email", "invalid_format"), ("email", "invalid_type"), ("email", "invalid_value"):
            return "Enter a valid email address."
        case ("email", "too_long"): return "Email address is too long."
        case ("password", "required"): return "Enter a password."
        case ("password", "too_short"): return "Password must be at least 10 characters."
        case ("password", "too_long"): return "Password is too long."
        case ("display_name", "required"): return "Enter a display name."
        case ("display_name", "too_short"): return "Display name is too short."
        case ("display_name", "too_long"): return "Display name is too long."
        default: return "\(field.replacingOccurrences(of: "_", with: " ").capitalized) is invalid."
        }
    }

    public func currentUser() async throws -> Components.Schemas.User {
        let response = try await client.getMe(.init())
        switch response {
        case .ok(let ok):
            return try ok.body.json
        case .unauthorized:
            throw AuthError.signedOut
        case .tooManyRequests:
            throw AuthError.rateLimited
        case .internalServerError:
            throw AuthError.server("The server had a problem loading your profile.")
        case .undocumented(let statusCode, _):
            throw AuthError.server("Unexpected response (\(statusCode)) while loading your profile.")
        }
    }
}
