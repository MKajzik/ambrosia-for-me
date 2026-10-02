import API
import Auth
import Features
import Observation

@Observable
@MainActor
public final class AppState {
    public enum Session {
        case signedOut
        case signedIn(Components.Schemas.User)
    }

    public private(set) var session: Session = .signedOut
    public private(set) var isRestoringSession = true

    private let authRepository: AuthRepository
    private let tokenStore: any TokenStore

    public init(authRepository: AuthRepository, tokenStore: any TokenStore) {
        self.authRepository = authRepository
        self.tokenStore = tokenStore
    }

    /// Called once at launch. If a refresh token is on disk, the app is online-first (spec §5):
    /// it fetches the current profile (transparently refreshing the access token if needed)
    /// before deciding whether the user is really still signed in.
    public func restoreSession() async {
        defer { isRestoringSession = false }
        guard tokenStore.refreshToken != nil else {
            session = .signedOut
            return
        }
        do {
            session = .signedIn(try await authRepository.currentUser())
        } catch {
            session = .signedOut
        }
    }

    public func signIn(email: String, password: String) async throws {
        session = .signedIn(try await authRepository.login(email: email, password: password))
    }

    public func register(email: String, password: String, displayName: String) async throws {
        session = .signedIn(try await authRepository.register(email: email, password: password, displayName: displayName))
    }

    public func signOut() async {
        await authRepository.logout()
        session = .signedOut
    }

    public func adoptSession(from viewModel: AuthViewModel) {
        guard let user = viewModel.lastSignedInUser else { return }
        session = .signedIn(user)
    }
}
