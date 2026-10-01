import API
import Auth
import Observation

@Observable
@MainActor
public final class AuthViewModel {
    public var email = ""
    public var password = ""
    public var displayName = ""
    public private(set) var isSubmitting = false
    public private(set) var errorMessage: String?
    public private(set) var lastSignedInUser: Components.Schemas.User?

    private let authRepository: AuthRepository

    public init(authRepository: AuthRepository) {
        self.authRepository = authRepository
    }

    public func signIn() async -> Components.Schemas.User? {
        await submit { try await authRepository.login(email: email, password: password) }
    }

    public func register() async -> Components.Schemas.User? {
        await submit {
            try await authRepository.register(email: email, password: password, displayName: displayName)
        }
    }

    private func submit(_ action: () async throws -> Components.Schemas.User) async -> Components.Schemas.User? {
        isSubmitting = true
        errorMessage = nil
        defer { isSubmitting = false }
        do {
            let user = try await action()
            lastSignedInUser = user
            return user
        } catch let error as AuthError {
            errorMessage = message(for: error)
            return nil
        } catch {
            errorMessage = "Something went wrong. Please try again."
            return nil
        }
    }

    private func message(for error: AuthError) -> String {
        switch error {
        case .invalidCredentials:
            return "Invalid email or password."
        case .emailTaken:
            return "That email is already registered."
        case .validationFailed(let detail):
            return detail
        case .rateLimited:
            return "Too many attempts. Please wait a moment and try again."
        case .server(let detail):
            return detail
        case .signedOut:
            return "Please sign in again."
        }
    }
}
