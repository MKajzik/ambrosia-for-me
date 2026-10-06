import API
import Foundation
import Persistence
import Repositories
import Testing
@testable import Features

@Suite
@MainActor
struct TemplatesViewModelTests {
    @MainActor
    struct Harness {
        let vm: TemplatesViewModel
        let cache: TemplateCache
        let transport: RoutingTransport

        init(_ route: @escaping RoutingTransport.Route) throws {
            transport = RoutingTransport(route)
            cache = CacheStore.makeTemplateCache(try CacheStore.inMemoryContainer())
            let client = makeAuthlessClient(transport: transport)
            vm = TemplatesViewModel(templates: TemplatesRepository(client: client, cache: cache), partner: PartnerRepository(client: client))
        }
    }

    /// `partner`: "active", "pending", "none" (404) or "offline" (the request throws).
    private static func route(
        partner: String = "active", mine: [Components.Schemas.DietTemplateSummary] = [], theirs: [Components.Schemas.DietTemplateSummary] = []
    ) -> RoutingTransport.Route {
        { call in
            switch call.route {
            case "GET /partner":
                switch partner {
                case "active": return (200, Fixtures.json(Components.Schemas.Partnership(status: .active, displayName: "Sam", linkedAt: Fixtures.date)))
                case "pending": return (200, Fixtures.json(Components.Schemas.Partnership(status: .pending, expiresAt: Fixtures.date)))
                case "none": return (404, Fixtures.problem(404, code: "partner_not_linked"))
                default: throw URLError(.notConnectedToInternet)
                }
            case "GET /diet-templates": return (200, Fixtures.templateList(mine))
            case "GET /partner/diet-templates":
                return partner == "none"
                    ? (404, Fixtures.problem(404, code: "partner_not_linked"))
                    : (200, Fixtures.templateList(theirs))
            default: return (500, Fixtures.problem(500, code: "internal"))
            }
        }
    }

    @Test("load shows the cache, then replaces it with the server's list and clears the stale mark")
    func loadsThenRefreshes() async throws {
        let h = try Harness(Self.route(mine: [Fixtures.templateSummary(id: "a", name: "A")]))
        await h.cache.replaceSummaries([Fixtures.templateSummary(id: "old", name: "Old")], scope: .mine)
        await h.vm.load()
        #expect(h.vm.templates.map(\.id) == ["a"])
        #expect(h.vm.isStale == false)
        #expect(h.vm.loadError == nil)
    }

    @Test("A refresh failure keeps the cache on screen, marked stale; with nothing cached it is an error state")
    func offline() async throws {
        let withCache = try Harness { _ in throw URLError(.notConnectedToInternet) }
        await withCache.cache.replaceSummaries([Fixtures.templateSummary(id: "old", name: "Old")], scope: .mine)
        await withCache.vm.load()
        #expect(withCache.vm.templates.map(\.id) == ["old"])
        #expect(withCache.vm.isStale)
        #expect(withCache.vm.loadError == nil)

        let empty = try Harness { _ in throw URLError(.notConnectedToInternet) }
        await empty.vm.load()
        #expect(empty.vm.loadError == "Can't reach the server. Check your connection and try again.")
    }

    @Test("The Partner's segment shows only while the partnership is active")
    func segmentRule() async throws {
        let active = try Harness(Self.route(partner: "active"))
        await active.vm.appear()
        #expect(active.vm.showsPartnerSegment)

        let pending = try Harness(Self.route(partner: "pending"))
        await pending.vm.appear()
        #expect(pending.vm.showsPartnerSegment == false)

        let none = try Harness(Self.route(partner: "none"))
        await none.cache.replaceSummaries([Fixtures.templateSummary(id: "p")], scope: .partner)
        await none.vm.appear()
        #expect(none.vm.showsPartnerSegment == false)
        #expect(await none.cache.summaries(scope: .partner).isEmpty)
    }

    @Test("Offline, the segment shows only if partner templates are already cached")
    func segmentOffline() async throws {
        let withCache = try Harness(Self.route(partner: "offline"))
        await withCache.cache.replaceSummaries([Fixtures.templateSummary(id: "p")], scope: .partner)
        await withCache.vm.appear()
        #expect(withCache.vm.showsPartnerSegment)
        let without = try Harness(Self.route(partner: "offline"))
        await without.vm.appear()
        #expect(without.vm.showsPartnerSegment == false)
    }

    @Test("The partner unlinking while Partner's is open hides the segment and falls back to Mine")
    func unlinkedWhileViewing() async throws {
        let linked = Locked(true)
        let h = try Harness { call in
            switch call.route {
            case "GET /partner": return (200, Fixtures.json(Components.Schemas.Partnership(status: .active, displayName: "Sam", linkedAt: Fixtures.date)))
            case "GET /partner/diet-templates":
                return linked.value
                    ? (200, Fixtures.templateList([Fixtures.templateSummary(id: "t")]))
                    : (404, Fixtures.problem(404, code: "partner_not_linked"))
            case "GET /diet-templates": return (200, Fixtures.templateList([Fixtures.templateSummary(id: "m", name: "Mine")]))
            default: return (500, Fixtures.problem(500, code: "internal"))
            }
        }
        await h.vm.appear()
        await h.vm.select(.partner)
        linked.set(false)
        await h.vm.load()
        #expect(h.vm.showsPartnerSegment == false)
        #expect(h.vm.scope == .mine)
        #expect(h.vm.templates.map(\.id) == ["m"])
        #expect(await h.cache.summaries(scope: .partner).isEmpty)
    }

    @Test("Creating validates locally, then opens the new template in the editor")
    func create() async throws {
        let h = try Harness { call in
            call.route == "POST /diet-templates"
                ? (201, Fixtures.json(Fixtures.template(id: "made", name: "Cut", dayCount: 5)))
                : (200, Fixtures.templateList([]))
        }
        #expect(await h.vm.createTemplate(name: "  ", dayCount: "7") == "Give the template a name.")
        #expect(await h.vm.createTemplate(name: "Cut", dayCount: "0") == "Day count must be between 1 and 31.")
        #expect(await h.vm.createTemplate(name: "Cut", dayCount: "32") == "Day count must be between 1 and 31.")
        #expect(await h.vm.createTemplate(name: "Cut", dayCount: "abc") == "Day count must be between 1 and 31.")
        #expect(await h.transport.calls.isEmpty)
        #expect(await h.vm.createTemplate(name: " Cut ", dayCount: " 5 ") == nil)
        #expect(h.vm.presentation == .edit("made"))
        let body = try #require(await h.transport.calls("POST /diet-templates").first?.body)
        #expect(body.contains("\"name\":\"Cut\"") && body.contains("\"day_count\":5"))
    }

    @Test("Delete removes the template and closes its editor; one already gone counts as deleted")
    func delete() async throws {
        let status = Locked(204)
        let h = try Harness { call in
            if call.route == "DELETE /diet-templates/a" {
                return status.value == 204 ? (204, "") : (404, Fixtures.problem(404, code: "not_found"))
            }
            return (200, Fixtures.templateList([Fixtures.templateSummary(id: "a", name: "A")]))
        }
        await h.vm.load()
        h.vm.presentation = .edit("a")
        #expect(await h.vm.delete(id: "a") == nil)
        #expect(h.vm.presentation == nil)
        #expect(h.vm.templates.isEmpty)

        status.set(404)
        await h.cache.replaceSummaries([Fixtures.templateSummary(id: "a")], scope: .mine)
        #expect(await h.vm.delete(id: "a") == nil)
    }

    @Test("A failed delete returns its text and keeps the sheet open; confirmDelete reports it through alertMessage")
    func deleteFailure() async throws {
        let h = try Harness { call in
            call.route == "DELETE /diet-templates/a"
                ? (500, Fixtures.problem(500, code: "internal"))
                : (200, Fixtures.templateList([Fixtures.templateSummary(id: "a")]))
        }
        await h.vm.load()
        h.vm.presentation = .edit("a")
        #expect(await h.vm.delete(id: "a") == "The server had a problem deleting the template.")
        #expect(h.vm.presentation == .edit("a"))
        h.vm.pendingDelete = Fixtures.templateSummary(id: "a")
        await h.vm.confirmDelete(Fixtures.templateSummary(id: "a"))
        #expect(h.vm.pendingDelete == nil)
        #expect(h.vm.alertMessage == "The server had a problem deleting the template.")
        #expect(TemplatesViewModel.deleteMessage(name: "Cut") == "\"Cut\" will be removed from your library. This can't be undone.")
    }

    @Test("Copying a partner template opens the copy in the editor and refreshes Mine")
    func copy() async throws {
        let h = try Harness { call in
            switch call.route {
            case "POST /diet-templates/theirs/copy": return (201, Fixtures.json(Fixtures.template(id: "copy", name: "Cut")))
            case "GET /diet-templates": return (200, Fixtures.templateList([Fixtures.templateSummary(id: "copy", name: "Cut")]))
            default: return (500, Fixtures.problem(500, code: "internal"))
            }
        }
        h.vm.presentation = .view("theirs")
        #expect(await h.vm.copyToLibrary(id: "theirs") == nil)
        #expect(h.vm.presentation == .edit("copy"))
        #expect(await h.cache.summaries(scope: .mine).map(\.id) == ["copy"])
    }

    @Test("A copy that fails is reported and leaves the sheet alone")
    func copyFails() async throws {
        let h = try Harness { _ in (404, Fixtures.problem(404, code: "not_found")) }
        h.vm.presentation = .view("theirs")
        #expect(await h.vm.copyToLibrary(id: "theirs") == "This template isn't available anymore.")
        #expect(h.vm.presentation == .view("theirs"))
    }
}
