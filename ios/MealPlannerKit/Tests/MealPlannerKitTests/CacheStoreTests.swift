import Foundation
import SwiftData
import Testing
@testable import Persistence

/// The offline queue (`CachedIntent`) is the one thing in the store the API cannot rebuild, so it lives in its own
/// file and nothing in `CacheStore` ever deletes that file.
@Suite
struct CacheStoreTests {
    private func directory() throws -> URL {
        let url = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
        try FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)
        return url
    }

    private func queued(_ container: ModelContainer) async -> [String] {
        await CacheStore.makeShoppingCache(container).intents().map(\.itemID)
    }

    @Test("A cache store that will not open is rebuilt, and the queued shopping changes survive it")
    func brokenCacheKeepsQueue() async throws {
        let dir = try directory()
        defer { try? FileManager.default.removeItem(at: dir) }
        let queue = dir.appending(path: "Queue.store")
        let cache = dir.appending(path: "Cache.store")

        let first = try CacheStore.persistentContainer(queueURL: queue, cacheURL: cache)
        _ = await CacheStore.makeShoppingCache(first).enqueue(kind: .check, listID: "l1", itemID: "i1", payload: nil)

        try Data("this is not a database".utf8).write(to: cache)
        let second = try CacheStore.persistentContainer(queueURL: queue, cacheURL: cache)
        #expect(await queued(second) == ["i1"])
        let meals = CacheStore.makeMealCache(second) // the rebuilt cache works
        #expect(await meals.summaries(scope: .mine).isEmpty)
    }

    @Test("A queue store that will not open is never deleted: the launch container throws instead")
    func brokenQueueIsKept() async throws {
        let dir = try directory()
        defer { try? FileManager.default.removeItem(at: dir) }
        let queue = dir.appending(path: "Queue.store")
        let junk = Data("this is not a database".utf8)
        try junk.write(to: queue)

        #expect(throws: (any Error).self) {
            try CacheStore.persistentContainer(queueURL: queue, cacheURL: dir.appending(path: "Cache.store"))
        }
        #expect(try Data(contentsOf: queue) == junk)
    }

    @Test("A store written by the previous schema (no Profile entities, everything in one file) keeps its queue")
    func previousSchemaMigrates() async throws {
        let dir = try directory()
        defer { try? FileManager.default.removeItem(at: dir) }
        let legacy = dir.appending(path: "MealPlannerCache.store")
        let previous = Schema([
            CachedMeal.self, CachedMealIngredient.self, CachedPlanDay.self, CachedTargets.self,
            CachedTemplate.self, CachedShoppingSummary.self, CachedShoppingList.self, CachedIntent.self,
        ])
        do {
            let old = try ModelContainer(for: previous, configurations: ModelConfiguration(url: legacy))
            _ = await CacheStore.makeShoppingCache(old).enqueue(kind: .add, listID: "l1", itemID: "temp:1", payload: .init(name: "Milk"))
            _ = await CacheStore.makeShoppingCache(old).enqueue(kind: .check, listID: "l1", itemID: "i2", payload: nil)
        }

        let current = try CacheStore.persistentContainer(queueURL: legacy, cacheURL: dir.appending(path: "Cache.store"))
        #expect(Set(await queued(current)) == ["temp:1", "i2"])
    }
}
