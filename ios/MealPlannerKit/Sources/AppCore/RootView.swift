import API
import Auth
import Features
import Foundation
import OpenAPIRuntime
import Persistence
import Repositories
import SwiftUI

/// Carries the signed-in user's id to `ShoppingDependencies`, which is built before `appState` exists.
@MainActor
final class UserBox {
    var id: String?
}

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
    @Environment(\.scenePhase) private var scenePhase
    private let refresher: TokenRefresher
    private let mealsDependencies: MealsDependencies
    private let planDependencies: PlanDependencies
    private let shoppingDependencies: ShoppingDependencies
    private let profileDependencies: ProfileDependencies
    private let userBox: UserBox

    public init(baseURL: URL = APIEnvironment.baseURL) {
        let tokenStore = KeychainTokenStore()
        let refresher = TokenRefresher(refreshClient: makeAuthlessClient(baseURL: baseURL), tokenStore: tokenStore)
        let networkSwitch: NetworkSwitch? = CommandLine.arguments.contains("-uiTesting") ? NetworkSwitch() : nil
        let client = makeClient(
            baseURL: baseURL,
            // The null sentinel is rewritten before the bearer middleware sees the request, so a retry after a 401
            // still has a replayable body.
            middlewares: (networkSwitch.map { [OfflineMiddleware($0) as any ClientMiddleware] } ?? [])
                + ([NullSentinelMiddleware(), BearerAuthMiddleware(refresher: refresher)] as [any ClientMiddleware])
        )
        let authRepository = AuthRepository(client: client, tokenStore: tokenStore)

        let container = CacheStore.launchContainer()
        let mealsRepository = MealsRepository(client: client, cache: CacheStore.makeMealCache(container))
        let planRepository = PlanRepository(client: client, cache: CacheStore.makePlanCache(container))
        let templatesRepository = TemplatesRepository(client: client, cache: CacheStore.makeTemplateCache(container))
        let profileCache = CacheStore.makeProfileCache(container)
        let profileRepository = ProfileRepository(client: client, cache: profileCache)
        let partnerRepository = PartnerRepository(client: client, cache: profileCache)
        let ingredientsRepository = IngredientsRepository(client: client)
        let shoppingCache = CacheStore.makeShoppingCache(container)
        let shoppingRepository = ShoppingListsRepository(client: client, cache: shoppingCache)
        let syncEngine = ShoppingSyncEngine(client: client, cache: shoppingCache)

        self.refresher = refresher
        self.mealsDependencies = MealsDependencies(
            meals: mealsRepository,
            ingredients: ingredientsRepository,
            partner: partnerRepository
        )
        self.planDependencies = PlanDependencies(
            plan: planRepository, templates: templatesRepository, meals: mealsRepository, partner: partnerRepository
        )
        self.profileDependencies = ProfileDependencies(
            profile: profileRepository, plan: planRepository, partner: partnerRepository, ingredients: ingredientsRepository
        )
        let userBox = UserBox()
        self.userBox = userBox
        self.shoppingDependencies = ShoppingDependencies(
            shopping: shoppingRepository, sync: syncEngine, partner: partnerRepository,
            events: ListEventStream(
                baseURL: baseURL,
                accessToken: { await refresher.currentAccessToken() },
                refreshToken: { try? await refresher.refreshAccessToken() },
                networkSwitch: networkSwitch
            ),
            monitor: NetworkMonitor(), networkSwitch: networkSwitch,
            currentUserID: { userBox.id }
        )
        _appState = State(initialValue: AppState(
            authRepository: authRepository,
            tokenStore: tokenStore,
            clearCaches: {
                await clearAllCaches(
                    meals: mealsRepository, plan: planRepository, templates: templatesRepository, shopping: shoppingRepository,
                    profile: profileRepository, sync: syncEngine
                )
            }
        ))
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
                    TabShellView(
                        appState: appState, mealsDependencies: mealsDependencies, planDependencies: planDependencies,
                        shoppingDependencies: shoppingDependencies, profileDependencies: profileDependencies
                    )
                }
            }
        }
        .task {
            await appState.attach(to: refresher)
            await appState.restoreSession()
        }
        .onChange(of: scenePhase) { _, phase in
            if phase == .active {
                Task { await appState.retryVerification() }
                Task { await shoppingDependencies.sync.drain() }
            }
        }
        .task(id: isSignedOut) {
            guard !isSignedOut else { return }
            await shoppingDependencies.runSyncLoop()
        }
        .onChange(of: appState.session) { _, session in
            if case .signedIn(let user) = session { userBox.id = user.id } else { userBox.id = nil }
        }
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
