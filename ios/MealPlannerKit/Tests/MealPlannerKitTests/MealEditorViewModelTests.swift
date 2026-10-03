import API
import Foundation
import Persistence
import Repositories
import Testing
@testable import Features

@Suite
@MainActor
struct MealEditorViewModelTests {
    @MainActor
    struct Harness {
        let vm: MealEditorViewModel
        let transport: RoutingTransport
        let sleeper = TestSleeper()
        let cache: MealCache

        init(
            cached: Components.Schemas.Meal? = Fixtures.meal(lines: [Fixtures.line()]),
            partnerLinked: Bool = true,
            route: @escaping RoutingTransport.Route = { _ in (200, Fixtures.json(Fixtures.meal(lines: [Fixtures.line()]))) }
        ) async throws {
            transport = RoutingTransport(route)
            cache = CacheStore.makeMealCache(try CacheStore.inMemoryContainer())
            if let cached { await cache.store(cached) }
            let repository = MealsRepository(client: makeAuthlessClient(transport: transport), cache: cache)
            let sleeper = self.sleeper
            vm = MealEditorViewModel(mealID: "meal-1", repository: repository, partnerLinked: partnerLinked, sleep: { _ in try await sleeper.sleep() })
            await vm.load()
        }

        func patches() async -> [RoutingTransport.Call] { await transport.calls("PATCH /meals/meal-1") }
        func puts() async -> [RoutingTransport.Call] { await transport.calls("PUT /meals/meal-1/ingredients") }
    }

    @Test("Loading adopts the cached meal, then the server's while the draft has not been touched")
    func loadsCachedThenFresh() async throws {
        let fresh = Fixtures.meal(name: "New", lines: [Fixtures.line()], nutrition: Fixtures.nutrients(calories: 999))
        let h = try await Harness(cached: Fixtures.meal(name: "Old")) { _ in (200, Fixtures.json(fresh)) }
        #expect(h.vm.phase == .ready)
        #expect(h.vm.draft.name == "New")
        #expect(h.vm.meal?.nutritionPerServing.calories == 999)
        #expect(h.vm.statusText == "All changes saved")
    }

    @Test("A slow refresh never overwrites what the person is typing; it only feeds the nutrition panel")
    func refreshDoesNotOverwriteTyping() async throws {
        let gate = Gate()
        let fresh = Fixtures.meal(name: "Server", nutrition: Fixtures.nutrients(calories: 999))
        let transport = RoutingTransport { call in
            if call.method == "GET" { await gate.wait() }
            return (200, Fixtures.json(fresh))
        }
        let cache = CacheStore.makeMealCache(try CacheStore.inMemoryContainer())
        await cache.store(Fixtures.meal(name: "Cached"))
        let vm = MealEditorViewModel(
            mealID: "meal-1", repository: MealsRepository(client: makeAuthlessClient(transport: transport), cache: cache),
            partnerLinked: true, sleep: { _ in try await TestSleeper().sleep() }
        )
        let loading = Task { await vm.load() }
        #expect(await waitUntil { vm.phase == .ready })
        vm.draft.name = "Typed"
        await gate.release()
        await loading.value
        #expect(vm.draft.name == "Typed")
        #expect(vm.meal?.nutritionPerServing.calories == 999)
    }

    @Test("A meal that is gone shows as unavailable and leaves the cache")
    func unavailable() async throws {
        let h = try await Harness { _ in (404, Fixtures.problem(404, code: "not_found")) }
        #expect(h.vm.phase == .unavailable("This meal isn't available anymore."))
        #expect(await h.cache.meal(id: "meal-1") == nil)
    }

    @Test("With nothing cached, a failed load is an error state, not a blank editor")
    func failedFirstLoad() async throws {
        let h = try await Harness(cached: nil) { _ in throw URLError(.notConnectedToInternet) }
        #expect(h.vm.phase == .failed("Can't reach the server. Check your connection and try again."))
    }

    @Test("A partner's meal is read-only: editing never schedules a save")
    func partnerMealIsReadOnly() async throws {
        let theirs = Fixtures.meal(isOwner: false)
        let h = try await Harness(cached: theirs) { _ in (200, Fixtures.json(theirs)) }
        #expect(h.vm.isOwner == false)
        h.vm.draft.name = "Hacked"
        await settle()
        #expect(await h.sleeper.pendingCount == 0)
        #expect(await h.patches().isEmpty)
    }

    @Test("Several quick edits make one save, after the pause")
    func debounceCoalesces() async throws {
        let h = try await Harness()
        h.vm.draft.name = "A"
        h.vm.draft.name = "AB"
        h.vm.draft.name = "ABC"
        #expect(h.vm.statusText == "Unsaved changes")
        #expect(h.vm.hasUnsaved)
        await h.sleeper.fire()
        #expect(await waitUntil { h.vm.statusText == "All changes saved" })
        let patches = await h.patches()
        #expect(patches.count == 1)
        #expect(patches[0].body.contains("\"name\":\"ABC\""))
        #expect(await h.puts().isEmpty)
    }

    @Test("An invalid draft is never saved, and the status says what to do")
    func invalidNeverSaved() async throws {
        let h = try await Harness()
        h.vm.draft.servings = "0"
        #expect(h.vm.statusText == "Fix the highlighted fields to save.")
        #expect(h.vm.validationErrors?.servings == MealDraft.servingsMessage)
        await settle()
        #expect(await h.sleeper.pendingCount == 0)
        #expect(await h.patches().isEmpty)
    }

    @Test("One save at a time, and an edit made during a save is saved after it")
    func oneAtATime() async throws {
        let gate = Gate()
        let active = Locked(0)
        let peak = Locked(0)
        let h = try await Harness { call in
            if call.route == "PATCH /meals/meal-1" {
                active.mutate { $0 += 1 }
                peak.set(max(peak.value, active.value))
                await gate.wait()
                active.mutate { $0 -= 1 }
            }
            return (200, Fixtures.json(Fixtures.meal(lines: [Fixtures.line()])))
        }
        h.vm.draft.name = "One"
        await h.sleeper.fire()
        #expect(await waitUntil { h.vm.isSaving })
        h.vm.draft.notes = "Second"
        await gate.release()
        #expect(await waitUntil { !h.vm.isSaving })
        await h.sleeper.fire()
        #expect(await waitUntil { h.vm.statusText == "All changes saved" })
        let patches = await h.patches()
        #expect(patches.count == 2)
        #expect(patches[0].body.contains("\"name\":\"One\""))
        #expect(patches[1].body.contains("\"notes\":\"Second\""))
        #expect(!patches[1].body.contains("\"name\""))
        #expect(peak.value == 1)
    }

    @Test("A value that failed is not retried by itself; the next edit, Try again, or leaving tries again")
    func failedValueNotAutoRetried() async throws {
        let failing = Locked(true)
        let h = try await Harness { call in
            if call.route == "PATCH /meals/meal-1", failing.value { return (500, Fixtures.problem(500, code: "internal")) }
            return (200, Fixtures.json(Fixtures.meal(lines: [Fixtures.line()])))
        }
        h.vm.draft.name = "Boom"
        await h.sleeper.fire()
        #expect(await waitUntil { h.vm.saveError != nil })
        #expect(h.vm.bannerMessage == "The server had a problem saving the meal.")
        await settle()
        #expect(await h.patches().count == 1)
        #expect(await h.sleeper.pendingCount == 0)

        failing.set(false)
        h.vm.draft.name = "Boom 2"
        await h.sleeper.fire()
        #expect(await waitUntil { h.vm.statusText == "All changes saved" })
        #expect(await h.patches().count == 2)
        #expect(h.vm.bannerMessage == nil)
    }

    @Test("Try again retries the failed value at once")
    func retry() async throws {
        let failing = Locked(true)
        let h = try await Harness { call in
            if call.route == "PATCH /meals/meal-1", failing.value { return (500, Fixtures.problem(500, code: "internal")) }
            return (200, Fixtures.json(Fixtures.meal(lines: [Fixtures.line()])))
        }
        h.vm.draft.name = "Boom"
        await h.sleeper.fire()
        #expect(await waitUntil { h.vm.saveError != nil })
        failing.set(false)
        await h.vm.retry()
        #expect(h.vm.saveError == nil)
        #expect(h.vm.statusText == "All changes saved")
        #expect(await h.patches().count == 2)
    }

    @Test("Putting the draft back to what the server holds clears the failure banner")
    func revertingClearsBanner() async throws {
        let h = try await Harness { call in
            call.route == "PATCH /meals/meal-1"
                ? (500, Fixtures.problem(500, code: "internal"))
                : (200, Fixtures.json(Fixtures.meal(lines: [Fixtures.line()])))
        }
        h.vm.draft.name = "Boom"
        await h.sleeper.fire()
        #expect(await waitUntil { h.vm.bannerMessage != nil })
        h.vm.draft.name = "Pasta"
        #expect(h.vm.bannerMessage == nil)
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

    @Test("Leaving with an invalid draft saves nothing")
    func flushInvalidDoesNothing() async throws {
        let h = try await Harness()
        h.vm.draft.servings = ""
        h.vm.flushOnLeave()
        await settle()
        #expect(await h.patches().isEmpty)
    }

    @Test("Fields commit on their own: a failed ingredient write after a good PATCH retries only the ingredients")
    func partialCommit() async throws {
        let putFails = Locked(true)
        let h = try await Harness { call in
            if call.route == "PUT /meals/meal-1/ingredients", putFails.value { return (500, Fixtures.problem(500, code: "internal")) }
            return (200, Fixtures.json(Fixtures.meal(lines: [Fixtures.line()])))
        }
        h.vm.draft.name = "Renamed"
        h.vm.addIngredient(Fixtures.ingredient(id: "ing-2", name: "Oats"))
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

    @Test("Removing the last ingredient writes an empty list")
    func removingLastIngredient() async throws {
        let h = try await Harness()
        h.vm.removeRows(at: [0])
        await h.sleeper.fire()
        #expect(await waitUntil { h.vm.statusText == "All changes saved" })
        let puts = await h.puts()
        #expect(puts.count == 1)
        #expect(puts[0].body.contains("\"items\":[]"))
        #expect(await h.patches().isEmpty)
    }

    @Test("Clearing the notes saves, and does not leave the editor stuck on Unsaved changes")
    func clearingNotes() async throws {
        let meal = Fixtures.meal(notes: "Quick", lines: [Fixtures.line()])
        let h = try await Harness(cached: meal) { _ in (200, Fixtures.json(meal)) }
        h.vm.draft.notes = ""
        await h.sleeper.fire()
        #expect(await waitUntil { h.vm.statusText == "All changes saved" })
        #expect(await h.patches().first?.body.contains("\"notes\":\"\"") == true)
    }

    @Test("The 201st ingredient cannot be added")
    func ingredientCap() async throws {
        let lines = (0..<200).map { Fixtures.line(id: "l\($0)", position: $0) }
        let meal = Fixtures.meal(lines: lines)
        let h = try await Harness(cached: meal) { _ in (200, Fixtures.json(meal)) }
        #expect(h.vm.canAddIngredient == false)
        h.vm.addIngredient(Fixtures.ingredient(id: "extra"))
        #expect(h.vm.draft.rows.count == 200)
        await settle()
        #expect(await h.sleeper.pendingCount == 0)
    }

    @Test("Sharing is offered while the partner link is active or the meal is already shared")
    func offersSharing() async throws {
        let unlinked = try await Harness(partnerLinked: false)
        #expect(unlinked.vm.offersSharing == false)
        let shared = Fixtures.meal(shared: true)
        let alreadyShared = try await Harness(cached: shared, partnerLinked: false) { _ in (200, Fixtures.json(shared)) }
        #expect(alreadyShared.vm.offersSharing)
        let linked = try await Harness(partnerLinked: true)
        #expect(linked.vm.offersSharing)
    }
}
