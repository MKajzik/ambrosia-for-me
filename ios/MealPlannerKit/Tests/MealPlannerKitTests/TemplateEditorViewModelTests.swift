import API
import Foundation
import Persistence
import Repositories
import Testing
@testable import Features

@Suite
@MainActor
struct TemplateEditorViewModelTests {
    @MainActor
    struct Harness {
        let vm: TemplateEditorViewModel
        let transport: RoutingTransport
        let sleeper = TestSleeper()
        let cache: TemplateCache

        init(
            cached: Components.Schemas.DietTemplate? = Fixtures.template(slots: [Fixtures.templateSlot(id: "s1")]),
            partnerLinked: Bool = true,
            route: @escaping RoutingTransport.Route = { _ in (200, Fixtures.json(Fixtures.template(slots: [Fixtures.templateSlot(id: "s1")]))) }
        ) async throws {
            transport = RoutingTransport(route)
            cache = CacheStore.makeTemplateCache(try CacheStore.inMemoryContainer())
            if let cached { await cache.store(cached) }
            let repository = TemplatesRepository(client: makeAuthlessClient(transport: transport), cache: cache)
            let sleeper = self.sleeper
            vm = TemplateEditorViewModel(templateID: "t1", repository: repository, partnerLinked: partnerLinked, sleep: { _ in try await sleeper.sleep() })
            await vm.load()
        }

        func patches() async -> [RoutingTransport.Call] { await transport.calls("PATCH /diet-templates/t1") }
        func puts() async -> [RoutingTransport.Call] { await transport.calls("PUT /diet-templates/t1/slots") }
    }

    @Test("Loading adopts the cached template, then the server's while the draft has not been touched")
    func loadsCachedThenFresh() async throws {
        let fresh = Fixtures.template(name: "New", slots: [Fixtures.templateSlot(id: "s1")])
        let h = try await Harness(cached: Fixtures.template(name: "Old")) { _ in (200, Fixtures.json(fresh)) }
        #expect(h.vm.phase == .ready)
        #expect(h.vm.draft.name == "New")
        #expect(h.vm.statusText == "All changes saved")
    }

    @Test("A template that is gone shows as unavailable and leaves the cache")
    func unavailable() async throws {
        let h = try await Harness { _ in (404, Fixtures.problem(404, code: "not_found")) }
        #expect(h.vm.phase == .unavailable("This template isn't available anymore."))
        #expect(await h.cache.template(id: "t1") == nil)
    }

    @Test("With nothing cached, a failed load is an error state")
    func failedFirstLoad() async throws {
        let h = try await Harness(cached: nil) { _ in throw URLError(.notConnectedToInternet) }
        #expect(h.vm.phase == .failed("Can't reach the server. Check your connection and try again."))
    }

    @Test("A partner's template is read-only: editing never schedules a save")
    func partnerTemplateIsReadOnly() async throws {
        let theirs = Fixtures.template(isOwner: false)
        let h = try await Harness(cached: theirs) { _ in (200, Fixtures.json(theirs)) }
        #expect(h.vm.isOwner == false)
        h.vm.draft.name = "Hacked"
        await settle()
        #expect(await h.sleeper.pendingCount == 0)
        #expect(await h.patches().isEmpty)
    }

    @Test("Renaming saves once, after the pause, as a PATCH of just the name")
    func renameDebounced() async throws {
        let h = try await Harness()
        h.vm.draft.name = "A"
        h.vm.draft.name = "AB"
        h.vm.draft.name = "ABC"
        #expect(h.vm.statusText == "Unsaved changes")
        await h.sleeper.fire()
        #expect(await waitUntil { h.vm.statusText == "All changes saved" })
        let patches = await h.patches()
        #expect(patches.count == 1)
        #expect(patches[0].body.contains("\"name\":\"ABC\""))
        #expect(await h.puts().isEmpty)
    }

    @Test("Adding slots saves the whole list; several snacks on one day are all kept")
    func addSlotsAndSnacks() async throws {
        let h = try await Harness()
        h.vm.setMeal(dayIndex: 1, slot: .lunch, meal: Fixtures.summary(id: "m2", name: "Rice"))
        h.vm.setMeal(dayIndex: 1, slot: .snack, meal: Fixtures.summary(id: "m1", name: "Oats"))
        h.vm.setMeal(dayIndex: 1, slot: .snack, meal: Fixtures.summary(id: "m2", name: "Rice"))
        await h.sleeper.fire()
        #expect(await waitUntil { h.vm.statusText == "All changes saved" })
        let puts = await h.puts()
        #expect(puts.count == 1)
        #expect(puts[0].body.components(separatedBy: "\"slot\":\"snack\"").count - 1 == 2)
        #expect(puts[0].body.contains("\"slot\":\"lunch\""))
        #expect(await h.patches().isEmpty)
    }

    @Test("Choosing a meal for an occupied breakfast replaces it: one breakfast slot remains")
    func replacingOccupiedSlot() async throws {
        let h = try await Harness()
        h.vm.setMeal(dayIndex: 0, slot: .breakfast, meal: Fixtures.summary(id: "m2", name: "Rice"))
        #expect(h.vm.draft.rows(day: 0, slot: .breakfast).count == 1)
        #expect(h.vm.draft.rows(day: 0, slot: .breakfast).first?.mealName == "Rice")
    }

    @Test("Removing the last slot writes an empty list")
    func removingLastSlot() async throws {
        let h = try await Harness()
        h.vm.removeRow(id: "s1")
        await h.sleeper.fire()
        #expect(await waitUntil { h.vm.statusText == "All changes saved" })
        let puts = await h.puts()
        #expect(puts.count == 1)
        #expect(puts[0].body.contains("\"items\":[]"))
    }

    @Test("An invalid portion is never saved, and the status says what to do")
    func invalidNeverSaved() async throws {
        let h = try await Harness()
        h.vm.draft.rows[0].portion = "0"
        #expect(h.vm.statusText == "Fix the highlighted fields to save.")
        #expect(h.vm.validationErrors?.rows["s1"] == "The portion must be more than 0 and at most 100.")
        await settle()
        #expect(await h.sleeper.pendingCount == 0)
        #expect(await h.patches().isEmpty)
        #expect(await h.puts().isEmpty)
    }

    @Test("Fields commit on their own: a failed slot write after a good PATCH retries only the slots")
    func partialCommit() async throws {
        let putFails = Locked(true)
        let h = try await Harness { call in
            if call.route == "PUT /diet-templates/t1/slots", putFails.value { return (500, Fixtures.problem(500, code: "internal")) }
            return (200, Fixtures.json(Fixtures.template(slots: [Fixtures.templateSlot(id: "s1")])))
        }
        h.vm.draft.name = "Renamed"
        h.vm.setMeal(dayIndex: 2, slot: .dinner, meal: Fixtures.summary(id: "m2", name: "Rice"))
        await h.sleeper.fire()
        #expect(await waitUntil { h.vm.saveError != nil })
        #expect(await h.patches().count == 1)
        #expect(await h.puts().count == 1)

        putFails.set(false)
        await h.vm.retry()
        #expect(await h.patches().count == 1)
        #expect(await h.puts().count == 2)
        #expect(h.vm.statusText == "All changes saved")
    }

    @Test("Leaving the editor saves a pending edit without waiting for the pause")
    func flushOnLeave() async throws {
        let h = try await Harness()
        h.vm.draft.name = "Flushed"
        h.vm.flushOnLeave()
        #expect(await waitUntil { h.vm.statusText == "All changes saved" })
        #expect(await h.patches().count == 1)
    }

    @Test("Sharing is offered while the partner link is active or the template is already shared")
    func offersSharing() async throws {
        let unlinked = try await Harness(partnerLinked: false)
        #expect(unlinked.vm.offersSharing == false)
        let shared = Fixtures.template(shared: true)
        let alreadyShared = try await Harness(cached: shared, partnerLinked: false) { _ in (200, Fixtures.json(shared)) }
        #expect(alreadyShared.vm.offersSharing)
        let linked = try await Harness(partnerLinked: true)
        #expect(linked.vm.offersSharing)
    }
}
