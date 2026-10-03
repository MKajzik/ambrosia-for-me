import API
import Foundation
import SwiftData
import Testing
@testable import Persistence

@Suite
struct MealCacheTests {
    private func makeCache() throws -> MealCache {
        CacheStore.makeMealCache(try CacheStore.inMemoryContainer())
    }

    @Test("Summaries come back alphabetical within their scope")
    func summariesSorted() async throws {
        let cache = try makeCache()
        await cache.replaceSummaries([Fixtures.summary(id: "b", name: "Soup"), Fixtures.summary(id: "a", name: "apple pie")], scope: .mine)
        #expect(await cache.summaries(scope: .mine).map(\.name) == ["apple pie", "Soup"])
        #expect(await cache.summaries(scope: .partner).isEmpty)
    }

    @Test("A stored meal reads back identical, nutrition included")
    func roundTrip() async throws {
        let cache = try makeCache()
        let meal = Fixtures.meal(
            notes: "Quick", servings: 2.5, shared: true,
            lines: [Fixtures.line(id: "l2", quantity: 50, unit: .piece, position: 1), Fixtures.line(id: "l1", name: "Oats", quantity: 120.5, position: 0)],
            nutrition: Fixtures.nutrients(calories: 412.5, protein: 12, carbohydrates: nil, fat: 3.25)
        )
        await cache.store(meal)
        let read = try #require(await cache.meal(id: "meal-1"))
        #expect(read.name == meal.name)
        #expect(read.notes == "Quick")
        #expect(read.nutritionPerServing == meal.nutritionPerServing)
        #expect(read.ingredients.map(\.id) == ["l1", "l2"])
        #expect(read.ingredients.map(\.quantity) == [120.5, 50])
        #expect(read.createdAt == Fixtures.date)
        #expect(read.isOwner)
    }

    @Test("A list row alone has no detail until the meal is opened")
    func summaryOnlyHasNoDetail() async throws {
        let cache = try makeCache()
        await cache.replaceSummaries([Fixtures.summary()], scope: .mine)
        #expect(await cache.meal(id: "meal-1") == nil)
    }

    @Test("Replacing summaries keeps a meal's cached detail and updates its list fields")
    func upsertKeepsDetail() async throws {
        let cache = try makeCache()
        await cache.store(Fixtures.meal(lines: [Fixtures.line()]))
        await cache.replaceSummaries([Fixtures.summary(name: "Renamed")], scope: .mine)
        let read = try #require(await cache.meal(id: "meal-1"))
        #expect(read.name == "Renamed")
        #expect(read.ingredients.count == 1)
    }

    @Test("Replacing one scope deletes only that scope's absent ids")
    func deletionIsScoped() async throws {
        let cache = try makeCache()
        await cache.replaceSummaries([Fixtures.summary(id: "a", name: "A"), Fixtures.summary(id: "b", name: "B")], scope: .mine)
        await cache.replaceSummaries([Fixtures.summary(id: "p", name: "P")], scope: .partner)
        await cache.replaceSummaries([Fixtures.summary(id: "a", name: "A")], scope: .mine)
        #expect(await cache.summaries(scope: .mine).map(\.id) == ["a"])
        #expect(await cache.summaries(scope: .partner).map(\.id) == ["p"])
        await cache.replaceSummaries([], scope: .partner)
        #expect(await cache.summaries(scope: .partner).isEmpty)
        #expect(await cache.summaries(scope: .mine).map(\.id) == ["a"])
    }

    @Test("A stored partner meal lands in the partner scope; a stored own meal in mine")
    func storeChoosesScopeByOwnership() async throws {
        let cache = try makeCache()
        await cache.store(Fixtures.meal(id: "theirs", isOwner: false))
        await cache.store(Fixtures.meal(id: "mine", isOwner: true))
        #expect(await cache.summaries(scope: .partner).map(\.id) == ["theirs"])
        #expect(await cache.summaries(scope: .mine).map(\.id) == ["mine"])
    }

    @Test("Storing a meal again replaces its ingredient lines")
    func storeReplacesLines() async throws {
        let cache = try makeCache()
        await cache.store(Fixtures.meal(lines: [Fixtures.line(id: "x")]))
        await cache.store(Fixtures.meal(lines: [Fixtures.line(id: "y"), Fixtures.line(id: "z", position: 1)]))
        #expect(try #require(await cache.meal(id: "meal-1")).ingredients.map(\.id) == ["y", "z"])
    }

    @Test("remove, clear(scope:) and clearAll")
    func clearing() async throws {
        let cache = try makeCache()
        await cache.replaceSummaries([Fixtures.summary(id: "a"), Fixtures.summary(id: "b")], scope: .mine)
        await cache.replaceSummaries([Fixtures.summary(id: "p")], scope: .partner)
        await cache.remove(id: "a")
        #expect(await cache.summaries(scope: .mine).map(\.id) == ["b"])
        await cache.clear(scope: .partner)
        #expect(await cache.summaries(scope: .partner).isEmpty)
        await cache.clearAll()
        #expect(await cache.summaries(scope: .mine).isEmpty)
    }

    @Test("An unopenable store is deleted and recreated, and the new one works")
    func unopenableStoreFallback() async throws {
        let directory = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let url = directory.appending(path: "Cache.store")
        try Data("this is not a database".utf8).write(to: url)

        let cache = CacheStore.makeMealCache(try CacheStore.persistentContainer(at: url))
        await cache.replaceSummaries([Fixtures.summary()], scope: .mine)
        #expect(await cache.summaries(scope: .mine).count == 1)
    }
}
