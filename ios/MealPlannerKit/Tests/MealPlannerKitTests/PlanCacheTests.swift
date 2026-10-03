import API
import Foundation
import Testing
@testable import Persistence

@Suite
struct PlanCacheTests {
    private func makeCache() throws -> PlanCache {
        CacheStore.makePlanCache(try CacheStore.inMemoryContainer())
    }

    @Test("Days come back in date order, limited to the range")
    func rangeAndOrder() async throws {
        let cache = try makeCache()
        await cache.replace(days: [Fixtures.day("2026-10-07"), Fixtures.day("2026-10-05"), Fixtures.day("2026-10-12")], targets: Fixtures.targets())
        #expect(await cache.days(from: "2026-10-05", to: "2026-10-11").map(\.date) == ["2026-10-05", "2026-10-07"])
        #expect(await cache.days(from: "2026-10-12", to: "2026-10-12").map(\.date) == ["2026-10-12"])
    }

    @Test("A date that was never fetched is absent, not empty")
    func unfetchedIsAbsent() async throws {
        let cache = try makeCache()
        await cache.replace(days: [Fixtures.day("2026-10-05")], targets: Fixtures.targets())
        let days = await cache.days(from: "2026-10-05", to: "2026-10-07")
        #expect(days.map(\.date) == ["2026-10-05"])
    }

    @Test("A stored day reads back identical, entries and nutrition included")
    func roundTrip() async throws {
        let cache = try makeCache()
        let day = Fixtures.day(
            "2026-10-05",
            entries: [Fixtures.entry(date: "2026-10-05", slot: .breakfast, mealID: "m1", mealName: "Oats", portion: 1.5),
                      Fixtures.entry(date: "2026-10-05", slot: .snack, mealID: "m2", mealName: "Apple")],
            calories: 412.5
        )
        await cache.replace(days: [day], targets: Fixtures.targets())
        #expect(await cache.days(from: "2026-10-05", to: "2026-10-05") == [day])
    }

    @Test("Replacing a range updates its dates and leaves every other date alone")
    func replaceTouchesOnlyItsDates() async throws {
        let cache = try makeCache()
        await cache.replace(days: [Fixtures.day("2026-10-05", calories: 100), Fixtures.day("2026-10-06", calories: 200)], targets: Fixtures.targets())
        await cache.replace(days: [Fixtures.day("2026-10-06", calories: 999)], targets: Fixtures.targets())
        let days = await cache.days(from: "2026-10-05", to: "2026-10-06")
        #expect(days.map(\.nutritionPerDay.calories) == [100, 999])
    }

    @Test("A day emptied on the server replaces the cached entries")
    func emptiedDay() async throws {
        let cache = try makeCache()
        await cache.replace(days: [Fixtures.day("2026-10-05", entries: [Fixtures.entry()], calories: 100)], targets: Fixtures.targets())
        await cache.replace(days: [Fixtures.day("2026-10-05", entries: [], calories: 0)], targets: Fixtures.targets())
        #expect(await cache.days(from: "2026-10-05", to: "2026-10-05").first?.entries.isEmpty == true)
    }

    @Test("Targets are stored, updated, and absent until first stored")
    func targets() async throws {
        let cache = try makeCache()
        #expect(await cache.targets() == nil)
        await cache.replace(days: [], targets: Fixtures.targets(kcal: 2000))
        #expect(await cache.targets() == Fixtures.targets(kcal: 2000))
        await cache.replace(days: [], targets: Fixtures.targets(kcal: nil, protein: 150))
        #expect(await cache.targets() == Fixtures.targets(kcal: nil, protein: 150))
    }

    @Test("clearAll empties days and targets")
    func clearAll() async throws {
        let cache = try makeCache()
        await cache.replace(days: [Fixtures.day("2026-10-05")], targets: Fixtures.targets())
        await cache.clearAll()
        #expect(await cache.days(from: "2000-01-01", to: "2100-01-01").isEmpty)
        #expect(await cache.targets() == nil)
    }
}
