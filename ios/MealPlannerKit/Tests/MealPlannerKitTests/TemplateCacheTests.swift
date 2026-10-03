import API
import Foundation
import Testing
@testable import Persistence

@Suite
struct TemplateCacheTests {
    private func makeCache() throws -> TemplateCache {
        CacheStore.makeTemplateCache(try CacheStore.inMemoryContainer())
    }

    @Test("Summaries come back alphabetical within their scope")
    func summariesSorted() async throws {
        let cache = try makeCache()
        await cache.replaceSummaries([Fixtures.templateSummary(id: "b", name: "Soup week"), Fixtures.templateSummary(id: "a", name: "bulk week")], scope: .mine)
        #expect(await cache.summaries(scope: .mine).map(\.name) == ["bulk week", "Soup week"])
        #expect(await cache.summaries(scope: .partner).isEmpty)
    }

    @Test("A stored template reads back identical, slots included")
    func roundTrip() async throws {
        let cache = try makeCache()
        let template = Fixtures.template(
            name: "Cut", dayCount: 3, shared: true,
            slots: [Fixtures.templateSlot(dayIndex: 0, slot: .breakfast, portion: 1.5), Fixtures.templateSlot(dayIndex: 2, slot: .snack, mealID: "m2", mealName: "Apple")]
        )
        await cache.store(template)
        #expect(await cache.template(id: "t1") == template)
    }

    @Test("A list row alone has no detail until the template is opened")
    func summaryOnlyHasNoDetail() async throws {
        let cache = try makeCache()
        await cache.replaceSummaries([Fixtures.templateSummary()], scope: .mine)
        #expect(await cache.template(id: "t1") == nil)
    }

    @Test("Replacing summaries keeps cached detail and updates the list fields")
    func upsertKeepsDetail() async throws {
        let cache = try makeCache()
        await cache.store(Fixtures.template(slots: [Fixtures.templateSlot()]))
        await cache.replaceSummaries([Fixtures.templateSummary(name: "Renamed")], scope: .mine)
        let read = try #require(await cache.template(id: "t1"))
        #expect(read.slots.count == 1)
        #expect(await cache.summaries(scope: .mine).first?.name == "Renamed")
    }

    @Test("Replacing one scope deletes only that scope's absent ids")
    func deletionIsScoped() async throws {
        let cache = try makeCache()
        await cache.replaceSummaries([Fixtures.templateSummary(id: "a", name: "A"), Fixtures.templateSummary(id: "b", name: "B")], scope: .mine)
        await cache.replaceSummaries([Fixtures.templateSummary(id: "p", name: "P")], scope: .partner)
        await cache.replaceSummaries([Fixtures.templateSummary(id: "a", name: "A")], scope: .mine)
        #expect(await cache.summaries(scope: .mine).map(\.id) == ["a"])
        #expect(await cache.summaries(scope: .partner).map(\.id) == ["p"])
    }

    @Test("A stored partner template lands in the partner scope; an own one in mine")
    func storeChoosesScopeByOwnership() async throws {
        let cache = try makeCache()
        await cache.store(Fixtures.template(id: "theirs", isOwner: false))
        await cache.store(Fixtures.template(id: "mine", isOwner: true))
        #expect(await cache.summaries(scope: .partner).map(\.id) == ["theirs"])
        #expect(await cache.summaries(scope: .mine).map(\.id) == ["mine"])
    }

    @Test("Storing again replaces the slots and updates the list row")
    func storeReplacesSlots() async throws {
        let cache = try makeCache()
        await cache.store(Fixtures.template(name: "Old", slots: [Fixtures.templateSlot(id: "x")]))
        await cache.store(Fixtures.template(name: "New", slots: [Fixtures.templateSlot(id: "y"), Fixtures.templateSlot(id: "z", dayIndex: 1)]))
        #expect(try #require(await cache.template(id: "t1")).slots.map(\.id) == ["y", "z"])
        #expect(await cache.summaries(scope: .mine).map(\.name) == ["New"])
    }

    @Test("remove, clear(scope:) and clearAll")
    func clearing() async throws {
        let cache = try makeCache()
        await cache.replaceSummaries([Fixtures.templateSummary(id: "a"), Fixtures.templateSummary(id: "b")], scope: .mine)
        await cache.replaceSummaries([Fixtures.templateSummary(id: "p")], scope: .partner)
        await cache.remove(id: "a")
        #expect(await cache.summaries(scope: .mine).map(\.id) == ["b"])
        await cache.clear(scope: .partner)
        #expect(await cache.summaries(scope: .partner).isEmpty)
        await cache.clearAll()
        #expect(await cache.summaries(scope: .mine).isEmpty)
    }
}
