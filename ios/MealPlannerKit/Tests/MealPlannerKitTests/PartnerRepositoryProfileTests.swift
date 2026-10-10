import API
import Foundation
import Persistence
import Testing
@testable import Repositories

@Suite
struct PartnerRepositoryProfileTests {
    private func make(_ route: @escaping RoutingTransport.Route) throws -> (PartnerRepository, ProfileCache, RoutingTransport) {
        let transport = RoutingTransport(route)
        let cache = CacheStore.makeProfileCache(try CacheStore.inMemoryContainer())
        return (PartnerRepository(client: makeAuthlessClient(transport: transport), cache: cache), cache, transport)
    }

    @Test("status stores what the server said: active, pending, or none on 404")
    func statusStores() async throws {
        let mode = Locked("active")
        let (repo, _, _) = try make { _ in
            switch mode.value {
            case "active": return (200, Fixtures.json(ProfileFixtures.active("Alex")))
            case "pending": return (200, Fixtures.json(ProfileFixtures.pending()))
            default: return (404, Fixtures.problem(404, code: "partner_not_linked"))
            }
        }
        #expect(await repo.cachedStatus() == .unknown)
        _ = try await repo.status()
        #expect(await repo.cachedStatus() == .present(ProfileFixtures.active("Alex")))
        mode.set("pending")
        _ = try await repo.status()
        #expect(await repo.cachedStatus() == .present(ProfileFixtures.pending()))
        mode.set("none")
        #expect(try await repo.status() == nil)
        #expect(await repo.cachedStatus() == .none)
    }

    @Test("A failing status leaves the cached answer alone; without a cache nothing is stored and status is unknown")
    func statusFailureAndNoCache() async throws {
        let (repo, cache, _) = try make { _ in (500, Fixtures.problem(500, code: "internal")) }
        await cache.store(partnership: ProfileFixtures.active())
        await #expect(throws: MealsError.server("The server had a problem loading your partnership.")) { try await repo.status() }
        #expect(await repo.cachedStatus() == .present(ProfileFixtures.active()))

        let bare = PartnerRepository(client: makeAuthlessClient(transport: RoutingTransport { _ in (404, Fixtures.problem(404, code: "partner_not_linked")) }))
        #expect(try await bare.status() == nil)
        #expect(await bare.cachedStatus() == .unknown)
    }

    @Test("createInvite returns the one-time code; an existing partner is alreadyLinked")
    func createInvite() async throws {
        let mode = Locked(201)
        let (repo, _, transport) = try make { _ in
            mode.value == 201
                ? (201, Fixtures.json(ProfileFixtures.invite(code: "ABCD2345")))
                : (mode.value, Fixtures.problem(mode.value, code: mode.value == 409 ? "partner_already_linked" : "x"))
        }
        #expect(try await repo.createInvite().code == "ABCD2345")
        #expect(await transport.calls("POST /partner/invite").count == 1)
        mode.set(409)
        await #expect(throws: PartnerError.alreadyLinked) { try await repo.createInvite() }
        mode.set(429)
        await #expect(throws: PartnerError.rateLimited) { try await repo.createInvite() }
    }

    @Test("accept sends the code as given and stores the new link")
    func accept() async throws {
        let (repo, _, transport) = try make { _ in (200, Fixtures.json(ProfileFixtures.active("Alex"))) }
        let partnership = try await repo.accept(code: "ab-cd 2345")
        #expect(partnership.status == .active)
        #expect(partnership.displayName == "Alex")
        #expect(try #require(await transport.calls("POST /partner/accept").first).body.contains("\"code\":\"ab-cd 2345\""))
        #expect(await repo.cachedStatus() == .present(ProfileFixtures.active("Alex")))
    }

    @Test("Review focus 6: a wrong code, an existing partner, a rate limit and a bad request each read differently")
    func acceptErrors() async throws {
        let mode = Locked(404)
        let (repo, cache, _) = try make { _ in
            switch mode.value {
            case 404: return (404, Fixtures.problem(404, code: "invite_invalid"))
            case 409: return (409, Fixtures.problem(409, code: "partner_already_linked"))
            case 400: return (400, Fixtures.problem(400, code: "validation_failed", errors: [("code", "required")]))
            case 429: return (429, Fixtures.problem(429, code: "too_many_requests"))
            default: return (409, Fixtures.problem(409, code: "something_else", title: "Conflict"))
            }
        }
        await cache.store(partnership: nil)
        await #expect(throws: PartnerError.inviteInvalid) { try await repo.accept(code: "x") }
        mode.set(409)
        await #expect(throws: PartnerError.alreadyLinked) { try await repo.accept(code: "x") }
        mode.set(400)
        await #expect(throws: PartnerError.validationFailed("Code is required.")) { try await repo.accept(code: "") }
        mode.set(429)
        await #expect(throws: PartnerError.rateLimited) { try await repo.accept(code: "x") }
        mode.set(0)
        await #expect(throws: PartnerError.server("Conflict")) { try await repo.accept(code: "x") }
        #expect(await repo.cachedStatus() == .none) // nothing above changed the cache
    }

    @Test("unlink stores 'none'; a link that is already gone (404) is a success; other failures throw")
    func unlink() async throws {
        let mode = Locked(204)
        let (repo, cache, _) = try make { _ in
            mode.value == 204 ? (204, "") : (mode.value, Fixtures.problem(mode.value, code: mode.value == 404 ? "partner_not_linked" : "internal"))
        }
        await cache.store(partnership: ProfileFixtures.active())
        try await repo.unlink()
        #expect(await repo.cachedStatus() == .none)

        await cache.store(partnership: ProfileFixtures.pending())
        mode.set(404)
        try await repo.unlink()
        #expect(await repo.cachedStatus() == .none)

        await cache.store(partnership: ProfileFixtures.active())
        mode.set(500)
        await #expect(throws: PartnerError.server("The server had a problem ending the link.")) { try await repo.unlink() }
        #expect(await repo.cachedStatus() == .present(ProfileFixtures.active()))
    }
}
