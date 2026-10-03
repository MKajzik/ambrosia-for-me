import API
import Foundation
import Persistence
import Repositories
import Testing
@testable import Features

@Suite
@MainActor
struct PlanViewModelTests {
    private static let noon = ISO8601DateFormatter().date(from: "2026-10-05T12:00:00Z")!

    @MainActor
    struct Harness {
        let vm: PlanViewModel
        let server: PlanServer
        let transport: RoutingTransport
        let cache: PlanCache
        let now: Locked<Date>

        init(server: PlanServer = PlanServer(), range: PlanViewModel.DateRange? = nil) throws {
            self.server = server
            let now = Locked(PlanViewModelTests.noon)
            self.now = now
            transport = RoutingTransport { call in try await server.route(call) }
            cache = CacheStore.makePlanCache(try CacheStore.inMemoryContainer())
            let repository = PlanRepository(client: makeAuthlessClient(transport: transport), cache: cache)
            vm = PlanViewModel(plan: repository, localDay: LocalDay(timeZone: TimeZone(identifier: "UTC")!, now: { now.value }), range: range)
        }
    }

    @Test("A date that was never fetched is absent; a fetched empty day is present and empty")
    func unfetchedVersusEmpty() async throws {
        let h = try Harness()
        #expect(h.vm.days["2026-10-05"] == nil)
        await h.vm.load()
        #expect(h.vm.days["2026-10-05"]?.entries.isEmpty == true)
        #expect(h.vm.days["2026-10-06"] == nil)
    }

    @Test("load shows the cache at once, then the server's day, and clears the stale mark")
    func cacheFirstThenRefresh() async throws {
        let h = try Harness(server: PlanServer(entries: [Fixtures.entry(date: "2026-10-05", mealName: "Fresh")]))
        await h.cache.replace(days: [Fixtures.day("2026-10-05", entries: [Fixtures.entry(date: "2026-10-05", mealName: "Stale")], calories: 1)], targets: Fixtures.targets())
        await h.vm.load()
        #expect(h.vm.days["2026-10-05"]?.entries.map(\.mealName) == ["Fresh"])
        #expect(h.vm.targets == Fixtures.targets())
        #expect(h.vm.isStale == false)
        #expect(h.vm.loadError == nil)
        #expect(h.vm.isLoading == false)
    }

    @Test("A refresh failure keeps the cache on screen and marks it stale, with no error banner")
    func offlineKeepsCache() async throws {
        let server = PlanServer()
        server.setFailGET(true)
        let h = try Harness(server: server)
        await h.cache.replace(days: [Fixtures.day("2026-10-05", calories: 321)], targets: Fixtures.targets())
        await h.vm.load()
        #expect(h.vm.days["2026-10-05"]?.nutritionPerDay.calories == 321)
        #expect(h.vm.isStale)
        #expect(h.vm.loadError == nil)
    }

    @Test("A refresh failure with nothing cached shows an error state")
    func offlineWithNothingCached() async throws {
        let server = PlanServer()
        server.setFailGET(true)
        let h = try Harness(server: server)
        await h.vm.load()
        #expect(h.vm.days.isEmpty)
        #expect(h.vm.loadError == "Can't reach the server. Check your connection and try again.")
    }

    @Test("A swap is a write then a refresh of that one date; the server's totals come back")
    func setMealWritesThenRefreshes() async throws {
        let h = try Harness()
        await h.vm.load()
        #expect(await h.vm.setMeal(date: "2026-10-05", slot: .breakfast, mealID: "m1", portion: 1) == nil)
        let calls = await h.transport.calls
        #expect(calls.map(\.route) == ["GET /plan", "PUT /plan/2026-10-05/breakfast", "GET /plan"])
        let refresh = try #require(calls.last?.path)
        #expect(refresh.contains("from=2026-10-05") && refresh.contains("to=2026-10-05"))
        #expect(h.vm.days["2026-10-05"]?.entries.map(\.mealName) == ["Oats"])
        #expect(h.vm.nutrition(on: "2026-10-05")?.calories == 100)
        #expect(h.vm.entries(on: "2026-10-05", slot: .breakfast).count == 1)
        #expect(h.vm.isWriting == false)
    }

    @Test("Changing a portion doubles the server's calories: the app never computes them")
    func portionChange() async throws {
        let h = try Harness()
        await h.vm.load()
        _ = await h.vm.setMeal(date: "2026-10-05", slot: .lunch, mealID: "m1", portion: 1)
        _ = await h.vm.setMeal(date: "2026-10-05", slot: .lunch, mealID: "m1", portion: 2)
        #expect(h.vm.nutrition(on: "2026-10-05")?.calories == 200)
        #expect(h.vm.entries(on: "2026-10-05", slot: .lunch).map(\.portion) == [2])
    }

    @Test("A snack is added, not swapped; clearing snacks removes them all, and clearing none is a success")
    func snacks() async throws {
        let h = try Harness()
        await h.vm.load()
        _ = await h.vm.setMeal(date: "2026-10-05", slot: .snack, mealID: "m1", portion: 1)
        _ = await h.vm.setMeal(date: "2026-10-05", slot: .snack, mealID: "m2", portion: 1)
        #expect(h.vm.entries(on: "2026-10-05", slot: .snack).count == 2)
        #expect(await h.vm.clearSnacks(date: "2026-10-05") == nil)
        #expect(h.vm.entries(on: "2026-10-05", slot: .snack).isEmpty)
        #expect(await h.vm.clearSnacks(date: "2026-10-05") == nil)
    }

    @Test("Remove clears one slot; removing an already-empty slot is a success")
    func remove() async throws {
        let h = try Harness(server: PlanServer(entries: [Fixtures.entry(date: "2026-10-05", slot: .dinner)]))
        await h.vm.load()
        #expect(await h.vm.remove(date: "2026-10-05", slot: .dinner) == nil)
        #expect(h.vm.entries(on: "2026-10-05", slot: .dinner).isEmpty)
        #expect(await h.vm.remove(date: "2026-10-05", slot: .dinner) == nil)
    }

    @Test("A failed write returns its error text, refreshes nothing, and changes nothing")
    func writeFailure() async throws {
        let server = PlanServer()
        let h = try Harness(server: server)
        await h.vm.load()
        server.setFailPUT(true)
        #expect(await h.vm.setMeal(date: "2026-10-05", slot: .breakfast, mealID: "m1", portion: 1) == "The server had a problem saving the plan.")
        #expect(await h.transport.calls.map(\.route) == ["GET /plan", "PUT /plan/2026-10-05/breakfast"])
        #expect(h.vm.entries(on: "2026-10-05", slot: .breakfast).isEmpty)
        #expect(h.vm.isWriting == false)
    }

    @Test("A write that succeeds but whose refresh fails says so, marks the view stale, and is not repeated")
    func writeOkRefreshFails() async throws {
        let server = PlanServer()
        let h = try Harness(server: server)
        await h.vm.load()
        server.setFailGET(true)
        #expect(await h.vm.setMeal(date: "2026-10-05", slot: .breakfast, mealID: "m1", portion: 1) == nil)
        #expect(h.vm.notice == "Saved, but the totals could not be refreshed. Pull to refresh.")
        #expect(h.vm.isStale)
        #expect(await h.transport.calls("PUT /plan/2026-10-05/breakfast").count == 1)
        #expect(server.entries(on: "2026-10-05").count == 1)
    }

    @Test("One write at a time; only the written range is busy")
    func oneWriteAtATime() async throws {
        let server = PlanServer()
        let h = try Harness(server: server)
        await h.vm.load()
        let gate = Gate()
        server.setPutGate(gate)
        let first = Task { await h.vm.setMeal(date: "2026-10-05", slot: .breakfast, mealID: "m1", portion: 1) }
        #expect(await waitUntil { h.vm.isBusy("2026-10-05") })
        #expect(h.vm.isBusy("2026-10-06") == false)
        #expect(await h.vm.setMeal(date: "2026-10-06", slot: .lunch, mealID: "m2", portion: 1) == "Wait for the current change to finish.")
        await gate.release()
        #expect(await first.value == nil)
        #expect(h.vm.isWriting == false)
        #expect(server.entries(on: "2026-10-06").isEmpty)
    }

    @Test("Apply: a conflict asks first, overwrite then applies and refreshes the applied range only")
    func applyConflictThenOverwrite() async throws {
        let template = PlanServer.Template(dayCount: 3, slots: [
            .init(dayIndex: 0, slot: .breakfast, mealID: "m1", portion: 1), .init(dayIndex: 2, slot: .dinner, mealID: "m2", portion: 2),
        ])
        let server = PlanServer(entries: [Fixtures.entry(date: "2026-10-05", slot: .breakfast, mealName: "Old")], templates: ["t1": template])
        let h = try Harness(server: server)
        await h.vm.load()
        #expect(await h.vm.apply(templateID: "t1", dayCount: 3, startDate: "2026-10-05", overwrite: false) == .needsConfirmation)
        #expect(await h.transport.calls("GET /plan").count == 1) // a conflict refreshes nothing
        #expect(await h.vm.apply(templateID: "t1", dayCount: 3, startDate: "2026-10-05", overwrite: true) == .applied)
        let refresh = try #require(await h.transport.calls("GET /plan").last?.path)
        #expect(refresh.contains("from=2026-10-05") && refresh.contains("to=2026-10-07"))
        #expect(server.entries(on: "2026-10-05").map(\.mealName) == ["Oats"])
        #expect(server.entries(on: "2026-10-07").map(\.slot) == [.dinner])
    }

    @Test("A template of snacks never conflicts, even over existing snacks")
    func snackTemplateNeverConflicts() async throws {
        let template = PlanServer.Template(dayCount: 1, slots: [.init(dayIndex: 0, slot: .snack, mealID: "m1", portion: 1)])
        let server = PlanServer(entries: [Fixtures.entry(date: "2026-10-05", slot: .snack)], templates: ["t1": template])
        let h = try Harness(server: server)
        #expect(await h.vm.apply(templateID: "t1", dayCount: 1, startDate: "2026-10-05", overwrite: false) == .applied)
        #expect(server.entries(on: "2026-10-05").count == 2)
    }

    @Test("Applying an unknown template fails with a readable message")
    func applyUnknown() async throws {
        let h = try Harness()
        #expect(await h.vm.apply(templateID: "nope", dayCount: 1, startDate: "2026-10-05", overwrite: false) == .failed("That template isn't available anymore."))
    }

    @Test("Moving to a week loads that week")
    func weekNavigation() async throws {
        let h = try Harness()
        await h.vm.setRange(from: "2026-10-05", to: "2026-10-11")
        #expect(h.vm.range == .init(from: "2026-10-05", to: "2026-10-11"))
        #expect(h.vm.days.count == 7)
        await h.vm.setRange(from: "2026-10-12", to: "2026-10-18")
        #expect(Set(h.vm.days.keys) == Set(h.vm.localDay.weekDays(startingAt: "2026-10-12")))
    }

    @Test("Today follows the local date: after midnight the range moves, and it reloads either way")
    func followToday() async throws {
        let h = try Harness()
        #expect(h.vm.range == .init(from: "2026-10-05", to: "2026-10-05"))
        await h.vm.followToday()
        #expect(h.vm.range == .init(from: "2026-10-05", to: "2026-10-05"))
        #expect(await h.transport.calls("GET /plan").count == 1)
        h.now.set(ISO8601DateFormatter().date(from: "2026-10-06T00:30:00Z")!)
        await h.vm.followToday()
        #expect(h.vm.range == .init(from: "2026-10-06", to: "2026-10-06"))
        #expect(h.vm.days["2026-10-06"] != nil)
    }
}

@Suite
struct ErrorTextPlanTests {
    @Test("Plan and template errors have their own text")
    func messages() {
        #expect(ErrorText.message(for: PlanError.notFound) == "That template isn't available anymore.")
        #expect(ErrorText.message(for: PlanError.conflict) == "Meals are already planned for some of those days.")
        #expect(ErrorText.message(for: PlanError.validationFailed("Portion is invalid.")) == "Portion is invalid.")
        #expect(ErrorText.message(for: PlanError.rateLimited) == ErrorText.rateLimited)
        #expect(ErrorText.message(for: TemplatesError.notFound) == "This template isn't available anymore.")
        #expect(ErrorText.message(for: TemplatesError.partnerNotLinked) == "You're not linked with a partner.")
        #expect(ErrorText.message(for: TemplatesError.server("Boom")) == "Boom")
        #expect(ErrorText.message(for: TemplatesError.unauthorized) == "Please sign in again.")
    }
}
