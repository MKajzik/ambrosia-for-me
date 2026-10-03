import API
import Auth
import Features
import Foundation
import SwiftUI

public struct RootView: View {
    @State private var appState: AppState
    // Separate instances, not one shared between both forms: each form's `TextField`/
    // `SecureField` binds directly to its view model's `email`/`password`, and sharing one
    // instance left the sign-in fields pre-populated with whatever was last typed into the
    // register form (or vice versa) when the user switched between them or returned to
    // sign-in after signing out from a freshly registered account.
    @State private var signInViewModel: AuthViewModel
    @State private var registerViewModel: AuthViewModel
    @State private var showingRegister = false

    public init(baseURL: URL = APIEnvironment.baseURL) {
        let tokenStore = KeychainTokenStore()
        let refresher = TokenRefresher(refreshClient: makeAuthlessClient(baseURL: baseURL), tokenStore: tokenStore)
        let client = makeClient(baseURL: baseURL, middlewares: [BearerAuthMiddleware(refresher: refresher)])
        let authRepository = AuthRepository(client: client, tokenStore: tokenStore)
        _appState = State(initialValue: AppState(authRepository: authRepository, tokenStore: tokenStore))
        _signInViewModel = State(initialValue: AuthViewModel(authRepository: authRepository))
        _registerViewModel = State(initialValue: AuthViewModel(authRepository: authRepository))
    }

    public var body: some View {
        Group {
            if appState.isRestoringSession {
                ProgressView()
            } else {
                switch appState.session {
                case .signedOut:
                    NavigationStack {
                        if showingRegister {
                            RegisterView(
                                viewModel: registerViewModel,
                                onRegistered: { appState.adoptSession(from: registerViewModel) },
                                onShowSignIn: { showingRegister = false }
                            )
                        } else {
                            SignInView(
                                viewModel: signInViewModel,
                                onSignedIn: { appState.adoptSession(from: signInViewModel) },
                                onShowRegister: { showingRegister = true }
                            )
                        }
                    }
                case .signedIn, .unverified:
                    TabShellView(appState: appState)
                }
            }
        }
        .task { await appState.restoreSession() }
        .onChange(of: isSignedOut) { _, nowSignedOut in
            // Otherwise a user who registered, then signed out, lands back on the register
            // screen instead of sign-in, because `showingRegister` is this view's own local
            // state and nothing else resets it when `AppState.signOut()` fires from the
            // Profile tab, several views away from here.
            if nowSignedOut {
                showingRegister = false
                signInViewModel.reset()
                registerViewModel.reset()
            }
        }
    }

    private var isSignedOut: Bool {
        if case .signedOut = appState.session { return true }
        return false
    }
}
