import API
import Foundation
import Persistence
import Testing
@testable import Repositories

@Suite
struct TemplatesRepositoryTests {
    private func make(_ route: @escaping RoutingTransport.Route) throws -> (TemplatesRepository, TemplateCache, RoutingTransport) {
        let transport = RoutingTransport(route)
        let cache = CacheStore.makeTemplateCache(try CacheStore.inMemoryContainer())
        return (TemplatesRepository(client: makeAuthlessClient(transport: transport), cache: cache), cache, transport)
    }

    @Test("Refresh walks every page at limit 100 and replaces the scope in one go")
    func walksPages() async throws {
        let (repo, cache, transport) = try make { call in
            switch (call.route, call.path.contains("cursor=c2")) {
            case ("GET /diet-templates", false):
                return (200, Fixtures.templateList([Fixtures.templateSummary(id: "a", name: "A"), Fixtures.templateSummary(id: "b", name: "B")], next: "c2"))
            case ("GET /diet-templates", true):
                return (200, Fixtures.templateList([Fixtures.templateSummary(id: "c", name: "C")]))
            default:
                return (500, Fixtures.problem(500, code: "internal"))
            }
        }
        try await repo.refreshTemplates(.mine)
        #expect(await cache.summaries(scope: .mine).map(\.id) == ["a", "b", "c"])
        let calls = await transport.calls("GET /diet-templates")
        #expect(calls.count == 2)
        #expect(calls.allSatisfy { $0.path.contains("limit=100") })
    }

    @Test("A failing second page leaves the cache untouched")
    func failedPageKeepsCache() async throws {
        let (repo, cache, _) = try make { call in
            call.path.contains("cursor=c2")
                ? (500, Fixtures.problem(500, code: "internal"))
                : (200, Fixtures.templateList([Fixtures.templateSummary(id: "new", name: "New")], next: "c2"))
        }
        await cache.replaceSummaries([Fixtures.templateSummary(id: "old", name: "Old")], scope: .mine)
        await #expect(throws: TemplatesError.server("The server had a problem loading your templates.")) {
            try await repo.refreshTemplates(.mine)
        }
        #expect(await cache.summaries(scope: .mine).map(\.id) == ["old"])
    }

    @Test("Partner scope reads /partner/diet-templates; 404 clears only that scope")
    func partnerScope() async throws {
        let (repo, cache, _) = try make { call in
            call.route == "GET /partner/diet-templates"
                ? (404, Fixtures.problem(404, code: "partner_not_linked"))
                : (500, Fixtures.problem(500, code: "internal"))
        }
        await cache.replaceSummaries([Fixtures.templateSummary(id: "p")], scope: .partner)
        await cache.replaceSummaries([Fixtures.templateSummary(id: "m")], scope: .mine)
        await #expect(throws: TemplatesError.partnerNotLinked) { try await repo.refreshTemplates(.partner) }
        #expect(await cache.summaries(scope: .partner).isEmpty)
        #expect(await cache.summaries(scope: .mine).map(\.id) == ["m"])
    }

    @Test("refreshTemplate caches the full template; a 404 removes the entry and throws notFound")
    func refreshTemplate() async throws {
        let live = Locked(true)
        let (repo, cache, _) = try make { _ in
            live.value
                ? (200, Fixtures.json(Fixtures.template(slots: [Fixtures.templateSlot()])))
                : (404, Fixtures.problem(404, code: "not_found"))
        }
        let template = try await repo.refreshTemplate(id: "t1")
        #expect(template.slots.count == 1)
        #expect(await repo.cachedTemplate(id: "t1")?.name == "Cut week")
        live.set(false)
        await #expect(throws: TemplatesError.notFound) { try await repo.refreshTemplate(id: "t1") }
        #expect(await cache.template(id: "t1") == nil)
    }

    @Test("create sends the name and day count and caches the answer")
    func create() async throws {
        let (repo, cache, transport) = try make { _ in (201, Fixtures.json(Fixtures.template(id: "made", name: "Cut", dayCount: 5))) }
        let made = try await repo.create(name: "Cut", dayCount: 5)
        #expect(made.id == "made")
        let body = try #require(await transport.calls("POST /diet-templates").first?.body)
        #expect(body.contains("\"name\":\"Cut\""))
        #expect(body.contains("\"day_count\":5"))
        #expect(await cache.template(id: "made") != nil)
    }

    @Test("A 400 becomes a readable validation message")
    func validation() async throws {
        let (repo, _, _) = try make { _ in (400, Fixtures.problem(400, code: "validation_failed", errors: [("name", "too_long")])) }
        await #expect(throws: TemplatesError.validationFailed("Name is too long.")) { try await repo.create(name: "x", dayCount: 3) }
    }

    @Test("update sends only the patched fields; replaceSlots PUTs the whole list, an empty one included")
    func updateAndReplace() async throws {
        let (repo, cache, transport) = try make { _ in (200, Fixtures.json(Fixtures.template(name: "New"))) }
        _ = try await repo.update(id: "t1", .init(name: "New"))
        let patch = try #require(await transport.calls("PATCH /diet-templates/t1").first)
        #expect(patch.body.contains("\"name\":\"New\""))
        #expect(!patch.body.contains("shared_with_partner"))
        _ = try await repo.replaceSlots(id: "t1", [])
        let put = try #require(await transport.calls("PUT /diet-templates/t1/slots").first)
        #expect(put.body.contains("\"items\":[]"))
        #expect(await cache.template(id: "t1")?.name == "New")
    }

    @Test("copy returns the full template and caches it")
    func copy() async throws {
        let (repo, cache, _) = try make { _ in (201, Fixtures.json(Fixtures.template(id: "copy"))) }
        #expect(try await repo.copy(id: "theirs").id == "copy")
        #expect(await cache.template(id: "copy") != nil)
    }

    @Test("delete: 204 removes the entry; 404 removes it and throws notFound")
    func delete() async throws {
        let status = Locked(204)
        let (repo, cache, _) = try make { _ in
            status.value == 204 ? (204, "") : (404, Fixtures.problem(404, code: "not_found"))
        }
        await cache.store(Fixtures.template())
        try await repo.delete(id: "t1")
        #expect(await cache.template(id: "t1") == nil)
        await cache.store(Fixtures.template())
        status.set(404)
        await #expect(throws: TemplatesError.notFound) { try await repo.delete(id: "t1") }
        #expect(await cache.template(id: "t1") == nil)
    }

    @Test("A transport failure reaches the caller as a plain URLError")
    func transportFailureIsUnwrapped() async throws {
        let (repo, _, _) = try make { _ in throw URLError(.notConnectedToInternet) }
        await #expect(throws: URLError.self) { try await repo.refreshTemplates(.mine) }
        await #expect(throws: URLError.self) { try await repo.refreshTemplate(id: "x") }
    }

    @Test("429 and 401 map to their own errors")
    func commonStatuses() async throws {
        let status = Locked(429)
        let (repo, _, _) = try make { _ in (status.value, Fixtures.problem(status.value, code: "x")) }
        await #expect(throws: TemplatesError.rateLimited) { try await repo.refreshTemplates(.mine) }
        status.set(401)
        await #expect(throws: TemplatesError.unauthorized) { try await repo.refreshTemplates(.mine) }
    }

    @Test("clearPartnerTemplates and clearCaches")
    func clearing() async throws {
        let (repo, cache, _) = try make { _ in (500, "") }
        await cache.replaceSummaries([Fixtures.templateSummary(id: "p")], scope: .partner)
        await cache.replaceSummaries([Fixtures.templateSummary(id: "m")], scope: .mine)
        await repo.clearPartnerTemplates()
        #expect(await repo.cachedTemplates(.partner).isEmpty)
        #expect(await repo.cachedTemplates(.mine).map(\.id) == ["m"])
        await repo.clearCaches()
        #expect(await repo.cachedTemplates(.mine).isEmpty)
    }
}
