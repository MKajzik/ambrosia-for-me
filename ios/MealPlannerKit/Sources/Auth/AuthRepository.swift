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
            throw AuthError.validationFailed(problem.detail ?? problem.title)
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
            throw AuthError.validationFailed(problem.detail ?? problem.title)
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

    public func logout() async {
        if let refreshToken = tokenStore.refreshToken {
            _ = try? await client.logoutUser(.init(body: .json(.init(refreshToken: refreshToken))))
        }
        tokenStore.clear()
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
