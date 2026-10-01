import API
import Auth
import Features
import Foundation
import SwiftUI

public struct RootView: View {
    @State private var appState: AppState
    @State private var authViewModel: AuthViewModel
    @State private var showingRegister = false

    public init(baseURL: URL = APIEnvironment.baseURL) {
        let tokenStore = KeychainTokenStore()
        let refresher = TokenRefresher(refreshClient: makeAuthlessClient(baseURL: baseURL), tokenStore: tokenStore)
        let client = makeClient(baseURL: baseURL, middlewares: [BearerAuthMiddleware(refresher: refresher)])
        let authRepository = AuthRepository(client: client, tokenStore: tokenStore)
        _appState = State(initialValue: AppState(authRepository: authRepository, tokenStore: tokenStore))
        _authViewModel = State(initialValue: AuthViewModel(authRepository: authRepository))
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
                                viewModel: authViewModel,
                                onRegistered: { appState.adoptSession(from: authViewModel) }
                            )
                        } else {
                            SignInView(
                                viewModel: authViewModel,
                                onSignedIn: { appState.adoptSession(from: authViewModel) },
                                onShowRegister: { showingRegister = true }
                            )
                        }
                    }
                case .signedIn:
                    TabShellView(appState: appState)
                }
            }
        }
        .task { await appState.restoreSession() }
    }
}
