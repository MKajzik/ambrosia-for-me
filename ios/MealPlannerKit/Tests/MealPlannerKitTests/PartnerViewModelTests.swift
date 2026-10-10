import API
import Foundation
import Persistence
import Testing
@testable import Features
@testable import Repositories

@MainActor
@Suite
struct PartnerViewModelTests {
    private func make(_ route: @escaping RoutingTransport.Route) throws -> (PartnerViewModel, PartnerRepository, ProfileCache, RoutingTransport) {
        let transport = RoutingTransport(route)
        let cache = CacheStore.makeProfileCache(try CacheStore.inMemoryContainer())
        let repo = PartnerRepository(client: makeAuthlessClient(transport: transport), cache: cache)
        return (PartnerViewModel(partner: repo), repo, cache, transport)
    }

    @Test("appear shows none, pending or linked from the server")
    func appear() async throws {
        let mode = Locked("none")
        let (vm, _, _, _) = try make { _ in
            switch mode.value {
            case "active": (200, Fixtures.json(ProfileFixtures.active("Alex")))
            case "pending": (200, Fixtures.json(ProfileFixtures.pending()))
            default: (404, Fixtures.problem(404, code: "partner_not_linked"))
            }
        }
        #expect(vm.phase == .loading)
        await vm.appear()
        #expect(vm.phase == .none)
        mode.set("active")
        await vm.appear()
        #expect(vm.phase == .linked(name: "Alex", since: Fixtures.date))
        mode.set("pending")
        await vm.appear()
        #expect(vm.phase == .pending(expiresAt: Fixtures.date))
        #expect(!vm.isStale)
    }

    @Test("Offline, the cached state is shown marked stale; with nothing cached it is an error")
    func offline() async throws {
        let (vm, _, cache, _) = try make { _ in throw URLError(.notConnectedToInternet) }
        await vm.appear()
        #expect(vm.phase == .loading)
        #expect(vm.loadError == "Can't reach the server. Check your connection and try again.")

        await cache.store(partnership: ProfileFixtures.active("Alex"))
        await vm.appear()
        #expect(vm.phase == .linked(name: "Alex", since: Fixtures.date))
        #expect(vm.isStale)
        #expect(vm.loadError == nil)
    }

    @Test("Creating an invite shows the code once and moves to pending")
    func createInvite() async throws {
        let (vm, _, _, transport) = try make { call in
            call.route == "POST /partner/invite" ? (201, Fixtures.json(ProfileFixtures.invite(code: "ABCD2345"))) : (200, Fixtures.json(ProfileFixtures.pending()))
        }
        await vm.createInvite()
        #expect(vm.invite?.code == "ABCD2345")
        #expect(vm.phase == .pending(expiresAt: Fixtures.date))
        #expect(await transport.calls("POST /partner/invite").count == 1)
    }

    @Test("Review focus 6: the code lives only in this view model: a new one shows pending but no code")
    func codeIsNotKept() async throws {
        let (vm, repo, _, _) = try make { call in
            call.route == "POST /partner/invite" ? (201, Fixtures.json(ProfileFixtures.invite())) : (200, Fixtures.json(ProfileFixtures.pending()))
        }
        await vm.createInvite()
        #expect(vm.invite != nil)
        let fresh = PartnerViewModel(partner: repo)
        await fresh.appear()
        #expect(fresh.phase == .pending(expiresAt: Fixtures.date))
        #expect(fresh.invite == nil)
    }

    @Test("A refused invite is an alert and changes nothing")
    func createInviteRefused() async throws {
        let (vm, _, _, _) = try make { _ in (409, Fixtures.problem(409, code: "partner_already_linked")) }
        await vm.createInvite()
        #expect(vm.alertMessage == "You're already linked with a partner.")
        #expect(vm.invite == nil)
        #expect(vm.phase == .loading)
    }

    @Test("Accepting a blank code is refused without a request")
    func acceptBlank() async throws {
        let (vm, _, _, transport) = try make { _ in (500, "") }
        vm.codeText = "   "
        await vm.accept()
        #expect(vm.acceptError == "Enter the code your partner sent you.")
        #expect(await transport.calls.isEmpty)
    }

    @Test("Review focus 6: messy input is sent trimmed as typed; success links and clears the field")
    func acceptMessy() async throws {
        let (vm, _, _, transport) = try make { _ in (200, Fixtures.json(ProfileFixtures.active("Alex"))) }
        vm.codeText = "  ab-cd 2345 "
        await vm.accept()
        #expect(try #require(await transport.calls("POST /partner/accept").first).body.contains("\"code\":\"ab-cd 2345\""))
        #expect(vm.phase == .linked(name: "Alex", since: Fixtures.date))
        #expect(vm.codeText == "")
        #expect(vm.acceptError == nil)
    }

    @Test("Review focus 6: a wrong code and an existing partner show inline; a rate limit is an alert; nothing links")
    func acceptErrors() async throws {
        let mode = Locked(404)
        let (vm, _, _, _) = try make { _ in
            switch mode.value {
            case 404: (404, Fixtures.problem(404, code: "invite_invalid"))
            case 409: (409, Fixtures.problem(409, code: "partner_already_linked"))
            default: (429, Fixtures.problem(429, code: "too_many_requests"))
            }
        }
        vm.codeText = "ZZZZ9999"
        await vm.accept()
        #expect(vm.acceptError == "That code didn't work. Check it, or ask your partner for a new one.")
        mode.set(409)
        await vm.accept()
        #expect(vm.acceptError == "You're already linked with a partner.")
        mode.set(429)
        await vm.accept()
        #expect(vm.acceptError == nil)
        #expect(vm.alertMessage == "Too many tries. Wait a minute and try again.")
        #expect(vm.phase == .loading)
        #expect(vm.codeText == "ZZZZ9999") // kept, so a retry is one tap
    }

    @Test("Unlinking moves to none, forgets the code, and treats an already-gone link as done; a failure changes nothing")
    func unlink() async throws {
        let mode = Locked(204)
        let (vm, repo, cache, _) = try make { call in
            if call.route == "GET /partner" { return (200, Fixtures.json(ProfileFixtures.active("Alex"))) }
            return mode.value == 204 ? (204, "") : (mode.value, Fixtures.problem(mode.value, code: mode.value == 404 ? "partner_not_linked" : "internal"))
        }
        await vm.appear()
        await vm.unlink()
        #expect(vm.phase == .none)
        #expect(await repo.cachedStatus() == .none)

        await vm.appear()
        mode.set(404)
        await vm.unlink()
        #expect(vm.phase == .none)

        await vm.appear()
        mode.set(500)
        await vm.unlink()
        #expect(vm.alertMessage == "The server had a problem ending the link.")
        #expect(vm.phase == .linked(name: "Alex", since: Fixtures.date))
        #expect(await cache.partnership() == .present(ProfileFixtures.active("Alex")))
    }

    @Test("A used code is not shown again once the link has ended from the other side")
    func staleCodeIsDropped() async throws {
        let mode = Locked("pending")
        let (vm, _, _, _) = try make { call in
            if call.route == "POST /partner/invite" { return (201, Fixtures.json(ProfileFixtures.invite())) }
            return mode.value == "pending" ? (200, Fixtures.json(ProfileFixtures.pending())) : (404, Fixtures.problem(404, code: "partner_not_linked"))
        }
        await vm.createInvite()
        #expect(vm.invite != nil)
        mode.set("gone")
        await vm.appear()
        #expect(vm.phase == .none)
        #expect(vm.invite == nil)
    }
}
