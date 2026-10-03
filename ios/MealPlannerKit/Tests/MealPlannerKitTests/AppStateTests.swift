import API
import Auth
import Foundation
import Testing
@testable import AppCore

@Suite
@MainActor
struct AppStateTests {
    @MainActor
    struct Harness {
        let state: AppState
        let store: InMemoryTokenStore
        let refresher: TokenRefresher
        let transport: RoutingTransport
        let clears: Locked<Int>

        init(tokens: Bool = true, route: @escaping RoutingTransport.Route) async {
            store = InMemoryTokenStore(accessToken: tokens ? "a" : nil, refreshToken: tokens ? "r" : nil)
            transport = RoutingTransport(route)
            refresher = TokenRefresher(refreshClient: makeAuthlessClient(transport: transport), tokenStore: store)
            let client = makeClient(transport: transport, middlewares: [BearerAuthMiddleware(refresher: refresher)])
            let counter = Locked(0)
            clears = counter
            state = AppState(
                authRepository: AuthRepository(client: client, tokenStore: store),
                tokenStore: store,
                clearCaches: { counter.mutate { $0 += 1 } }
            )
            await state.attach(to: refresher)
        }
    }

    private nonisolated static let rejectedRefresh = (401, Fixtures.problem(401, code: "unauthorized"))

    @Test("No stored session: signed out, no network, caches cleared")
    func noStoredSession() async {
        let h = await Harness(tokens: false) { _ in (500, "") }
        await h.state.restoreSession()
        #expect(h.state.session == .signedOut)
        #expect(h.state.isRestoringSession == false)
        #expect(await h.transport.calls.isEmpty)
        #expect(h.clears.value == 1)
    }

    @Test("A stored session the API confirms is signed in")
    func validSession() async {
        let h = await Harness { _ in (200, Fixtures.userJSON) }
        await h.state.restoreSession()
        guard case .signedIn(let user) = h.state.session else { Issue.record("expected signedIn"); return }
        #expect(user.email == "person@example.com")
        #expect(h.clears.value == 0)
    }

    @Test("Launching offline with a stored session keeps it, unverified, and does not clear caches")
    func offlineLaunchKeepsSession() async {
        let h = await Harness { _ in throw URLError(.notConnectedToInternet) }
        await h.state.restoreSession()
        #expect(h.state.session == .unverified)
        #expect(h.store.refreshToken == "r")
        #expect(h.state.isRestoringSession == false)
        #expect(h.clears.value == 0)
    }

    @Test("A 5xx at launch keeps the session too")
    func serverErrorKeepsSession() async {
        let h = await Harness { _ in (500, Fixtures.problem(500, code: "internal")) }
        await h.state.restoreSession()
        #expect(h.state.session == .unverified)
        #expect(h.store.refreshToken == "r")
    }

    @Test("A definitive rejection at launch (the refresh token is refused) signs out and clears caches once")
    func definitiveRejectionSignsOut() async {
        let h = await Harness { call in
            call.route == "POST /auth/refresh" ? Self.rejectedRefresh : (401, Fixtures.problem(401, code: "unauthorized"))
        }
        await h.state.restoreSession()
        #expect(h.state.session == .signedOut)
        #expect(h.store.refreshToken == nil)
        #expect(h.clears.value == 1)
    }

    @Test("An unverified session becomes signed in once the API is reachable again")
    func retryVerification() async {
        let online = Locked(false)
        let h = await Harness { _ in
            if !online.value { throw URLError(.notConnectedToInternet) }
            return (200, Fixtures.userJSON)
        }
        await h.state.restoreSession()
        #expect(h.state.session == .unverified)
        online.set(true)
        await h.state.retryVerification()
        guard case .signedIn = h.state.session else { Issue.record("expected signedIn"); return }
    }

    @Test("A refresh the API rejects mid-session flips to signed out immediately, once, and clears caches")
    func sessionEndsMidSession() async {
        let rejecting = Locked(false)
        let h = await Harness { call in
            if call.route == "POST /auth/refresh" { return Self.rejectedRefresh }
            return rejecting.value ? (401, Fixtures.problem(401, code: "unauthorized")) : (200, Fixtures.userJSON)
        }
        await h.state.restoreSession()
        guard case .signedIn = h.state.session else { Issue.record("expected signedIn"); return }
        rejecting.set(true)
        // Several requests race the expired token; single-flight means one rejection, one notification.
        async let first: String? = try? h.refresher.refreshAccessToken()
        async let second: String? = try? h.refresher.refreshAccessToken()
        _ = await (first, second)
        #expect(h.state.session == .signedOut)
        #expect(h.clears.value == 1)
        #expect(await h.transport.calls("POST /auth/refresh").count == 1)
    }

    @Test("Sign-out flips state, clears tokens and caches first, and revokes on the server afterwards")
    func signOutDoesNotWaitForTheServer() async throws {
        let gate = Gate()
        let h = await Harness { call in
            if call.route == "POST /auth/logout" {
                await gate.wait()
                return (204, "")
            }
            return (200, Fixtures.userJSON)
        }
        await h.state.restoreSession()
        await h.state.signOut() // returns although the logout request is still held by the gate
        #expect(h.state.session == .signedOut)
        #expect(h.store.refreshToken == nil)
        #expect(h.clears.value == 1)
        await gate.release()
        await h.state.pendingRevoke?.value
        let logout = try #require(await h.transport.calls("POST /auth/logout").first)
        #expect(logout.body.contains("\"refresh_token\":\"r\""))
    }

    @Test("Signing in after a sign-out clears caches again on the next sign-out")
    func clearsOnEveryEnd() async {
        let h = await Harness { _ in (200, Fixtures.userJSON) }
        await h.state.restoreSession()
        await h.state.signOut()
        h.store.save(accessToken: "a2", refreshToken: "r2")
        await h.state.restoreSession()
        await h.state.signOut()
        #expect(h.clears.value == 2)
    }
}
