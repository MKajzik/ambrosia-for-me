import API
import Auth
import Features
import Foundation
import Observation

@Observable
@MainActor
public final class AppState {
    public enum Session: Equatable {
        case signedOut
        /// Tokens are stored but the API could not verify them (offline, 429 or 5xx at launch). The shell
        /// opens and shows cached data marked stale; verification retries on foreground.
        case unverified
        case signedIn(Components.Schemas.User)
    }

    public private(set) var session: Session = .signedOut
    public private(set) var isRestoringSession = true
    /// The server-side revoke started by the last `signOut()`. Tests await it.
    @ObservationIgnored public private(set) var pendingRevoke: Task<Void, Never>?

    private let authRepository: AuthRepository
    private let tokenStore: any TokenStore
    private let clearCaches: @Sendable () async -> Void
    @ObservationIgnored private var cachesAreClear = false

    public init(
        authRepository: AuthRepository,
        tokenStore: any TokenStore,
        clearCaches: @escaping @Sendable () async -> Void = {}
    ) {
        self.authRepository = authRepository
        self.tokenStore = tokenStore
        self.clearCaches = clearCaches
    }

    /// Lets the refresher tell this state the moment the API rejects the refresh token, so the UI flips
    /// to signed out immediately instead of at the next launch.
    public func attach(to refresher: TokenRefresher) async {
        await refresher.setSessionEndedHandler { [weak self] in await self?.endSession() }
    }

    /// Called once at launch. Online-first (spec §5): with a refresh token on disk it asks the API who the
    /// user is. Only a definitive failure signs out; see `verifySession()`.
    public func restoreSession() async {
        defer { isRestoringSession = false }
        guard tokenStore.refreshToken != nil else {
            await endSession()
            return
        }
        await verifySession()
    }

    /// Re-verifies an unverified session, on foreground or reconnect.
    public func retryVerification() async {
        guard session == .unverified else { return }
        await verifySession()
    }

    public func signIn(email: String, password: String) async throws {
        setActive(.signedIn(try await authRepository.login(email: email, password: password)))
    }

    public func register(email: String, password: String, displayName: String) async throws {
        setActive(.signedIn(try await authRepository.register(email: email, password: password, displayName: displayName)))
    }

    /// Flips state, clears the Keychain and every cache at once, then finishes the server revoke in the background.
    public func signOut() async {
        let refreshToken = authRepository.endLocalSession()
        await endSession()
        if let refreshToken {
            let repository = authRepository
            pendingRevoke = Task { await repository.revoke(refreshToken: refreshToken) }
        }
    }

    public func adoptSession(from viewModel: AuthViewModel) {
        guard let user = viewModel.lastSignedInUser else { return }
        setActive(.signedIn(user))
    }

    private func verifySession() async {
        do {
            setActive(.signedIn(try await authRepository.currentUser()))
        } catch {
            // `TokenRefresher` clears the stored tokens exactly when the API rejects them, so a refresh
            // token still on disk means the failure was transient (network, 429, 5xx, a refresh that could
            // not run): keep the session rather than bounce an offline user to the sign-in screen.
            if tokenStore.refreshToken == nil {
                await endSession()
            } else {
                setActive(.unverified)
            }
        }
    }

    private func setActive(_ next: Session) {
        cachesAreClear = false
        session = next
    }

    /// Whenever the session becomes signed out, by any path, caches are cleared so a second user on this
    /// device never sees the first user's meals. Idempotent: racing paths clear once.
    private func endSession() async {
        session = .signedOut
        guard !cachesAreClear else { return }
        cachesAreClear = true
        await clearCaches()
    }
}
