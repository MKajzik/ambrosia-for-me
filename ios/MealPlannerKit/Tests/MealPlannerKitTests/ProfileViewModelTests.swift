import API
import Foundation
import Persistence
import Testing
@testable import Features
@testable import Repositories

@MainActor
@Suite
struct ProfileViewModelTests {
    private let offlineText = "Can't reach the server. Check your connection and try again."

    @Test("appear loads the user and fills the draft from its targets; nothing to save yet")
    func appear() async throws {
        let h = try ProfileHarness(ProfileServer(ProfileFixtures.user(kcal: 2000, protein: 70)))
        let vm = h.makeViewModel()
        await vm.appear()
        #expect(vm.user?.targetKcal == 2000)
        #expect(vm.draft.calories == "2000")
        #expect(vm.draft.protein == "70")
        #expect(vm.draft.carbs == "")
        #expect(!vm.isStale)
        #expect(!vm.canSave)
        #expect(await h.profileCache.user()?.targetKcal == 2000)
    }

    @Test("Review focus 3: offline with a cache shows the saved profile, marked stale, with no error")
    func offlineWithCache() async throws {
        let h = try ProfileHarness()
        await h.profileCache.store(user: ProfileFixtures.user(kcal: 1500))
        h.server.setOffline(true)
        let vm = h.makeViewModel()
        await vm.appear()
        #expect(vm.user?.targetKcal == 1500)
        #expect(vm.draft.calories == "1500")
        #expect(vm.isStale)
        #expect(vm.loadError == nil)
    }

    @Test("Review focus 3: a first launch offline with no cache is an error state, not a blank form")
    func offlineNoCache() async throws {
        let h = try ProfileHarness()
        h.server.setOffline(true)
        let vm = h.makeViewModel()
        await vm.appear()
        #expect(vm.user == nil)
        #expect(vm.loadError == offlineText)
        #expect(!vm.canSave)
    }

    @Test("Save sends all four, stores the answer, and gives the plan cache the new targets so Today's rings follow")
    func save() async throws {
        let h = try ProfileHarness(ProfileServer(ProfileFixtures.user(kcal: 1800)))
        let vm = h.makeViewModel()
        await vm.appear()
        vm.draft.calories = "2000"
        vm.draft.protein = "70,5"
        #expect(vm.canSave)
        #expect(await vm.saveTargets())
        #expect(h.server.current.targetKcal == 2000)
        #expect(h.server.current.targetProteinG == 70.5)
        #expect(await h.profileCache.user()?.targetKcal == 2000)
        #expect(await h.planCache.targets()?.targetKcal == 2000)
        #expect(await h.planCache.targets()?.targetProteinG == 70.5)
        #expect(vm.draft.calories == "2000")
        #expect(vm.draft.protein == "70.5")
        #expect(!vm.canSave)
        #expect(vm.banner == nil)
    }

    @Test("Review focus 1: a blank field really clears the target on the server, in the cache and in the plan cache")
    func blankClears() async throws {
        let h = try ProfileHarness(ProfileServer(ProfileFixtures.user(kcal: 1800, protein: 70)))
        let vm = h.makeViewModel()
        await vm.appear()
        vm.draft.calories = ""
        #expect(await vm.saveTargets())
        #expect(h.server.current.targetKcal == nil)
        #expect(h.server.current.targetProteinG == 70) // untouched, and re-sent as 70
        #expect(vm.user?.targetKcal == nil)
        #expect(await h.profileCache.user()?.targetKcal == nil)
        #expect(await h.planCache.targets()?.targetKcal == nil)
    }

    @Test("Invalid input shows messages next to the fields and sends no request")
    func invalidSendsNothing() async throws {
        let h = try ProfileHarness()
        let vm = h.makeViewModel()
        await vm.appear()
        vm.draft.calories = "0"
        vm.draft.fat = "abc"
        #expect(await vm.saveTargets() == false)
        #expect(vm.fieldErrors[.calories] == "Calories must be more than 0 and at most 20000.")
        #expect(vm.fieldErrors[.fat] == "Enter a number, for example 70.")
        #expect(await h.transport.calls("PATCH /me").isEmpty)
    }

    @Test("A server refusal that names a field shows on that field; one that does not shows as a banner")
    func serverRefusals() async throws {
        let h = try ProfileHarness()
        let vm = h.makeViewModel()
        await vm.appear()
        h.server.rejectPatch(field: "target_protein_g")
        vm.draft.protein = "50"
        #expect(await vm.saveTargets() == false)
        #expect(vm.fieldErrors[.protein] == "Target protein g is invalid.")
        #expect(vm.banner == nil)

        h.server.rejectPatch(field: nil)
        h.server.force("PATCH /me", status: 500)
        #expect(await vm.saveTargets() == false)
        #expect(vm.fieldErrors.isEmpty)
        #expect(vm.banner == "The server had a problem saving your targets.")
    }

    @Test("Review focus 3: offline, Save fails with a retryable message; the draft stays and nothing changes")
    func offlineSave() async throws {
        let h = try ProfileHarness(ProfileServer(ProfileFixtures.user(kcal: 2000)))
        let vm = h.makeViewModel()
        await vm.appear()
        h.server.setOffline(true)
        vm.draft.calories = "1800"
        #expect(await vm.saveTargets() == false)
        #expect(vm.banner == offlineText)
        #expect(vm.draft.calories == "1800")
        #expect(vm.canSave) // retryable
        #expect(await h.profileCache.user()?.targetKcal == 2000)
        #expect(await h.planCache.targets() == nil)
    }

    @Test("Review focus 4: a refresh while the person is typing does not overwrite the draft")
    func refreshKeepsTyping() async throws {
        let h = try ProfileHarness(ProfileServer(ProfileFixtures.user(kcal: 1800)))
        let vm = h.makeViewModel()
        await vm.appear()
        vm.draft.calories = "1234"
        h.server.replace(ProfileFixtures.user(kcal: 2500))
        await vm.appear()
        #expect(vm.user?.targetKcal == 2500)
        #expect(vm.draft.calories == "1234")
        #expect(vm.canSave)
    }

    @Test("Review focus 5: deleting needs the matching email (any case, spaces trimmed) and sends nothing otherwise")
    func deleteNeedsEmail() async throws {
        let h = try ProfileHarness(ProfileServer(ProfileFixtures.user(email: "sam@example.com")))
        let vm = h.makeViewModel()
        await vm.appear()
        #expect(await vm.deleteAccount(typedEmail: "someone@else.com") == "Type your email address exactly to confirm.")
        #expect(await vm.deleteAccount(typedEmail: "") == "Type your email address exactly to confirm.")
        #expect(await h.transport.calls("DELETE /me").isEmpty)
        #expect(h.signOuts.value == 0)

        #expect(await vm.deleteAccount(typedEmail: "  SAM@Example.com ") == nil)
        #expect(h.server.isDeleted)
        #expect(h.signOuts.value == 1)
    }

    @Test("Review focus 5: a failed delete leaves the account and the session alone")
    func deleteFailure() async throws {
        let h = try ProfileHarness()
        let vm = h.makeViewModel()
        await vm.appear()
        h.server.force("DELETE /me", status: 500)
        #expect(await vm.deleteAccount(typedEmail: "sam@example.com") == "The server had a problem deleting your account.")
        #expect(h.signOuts.value == 0)
        #expect(vm.user != nil)
        #expect(await h.profileCache.user() != nil)
    }

    @Test("With no user loaded there is no email to confirm against, so nothing is deleted")
    func deleteWithoutUser() async throws {
        let h = try ProfileHarness()
        h.server.setOffline(true)
        let vm = h.makeViewModel()
        await vm.appear()
        #expect(await vm.deleteAccount(typedEmail: "sam@example.com") == "Type your email address exactly to confirm.")
        #expect(h.signOuts.value == 0)
    }

    @Test("A slow refresh that began before a save cannot undo it, on screen or in the cache")
    func lateRefreshDoesNotUndoSave() async throws {
        let h = try ProfileHarness(ProfileServer(ProfileFixtures.user(kcal: nil)))
        let vm = h.makeViewModel()
        await vm.appear()

        let gate = Gate()
        h.server.holdReads(until: gate)
        let refresh = Task { await vm.appear() }
        #expect(await waitUntil { h.server.readsWaiting.value == 1 }) // read the old profile, not yet answered

        vm.draft.calories = "2000"
        #expect(await vm.saveTargets())
        h.server.holdReads(until: nil)
        await gate.release()
        await refresh.value

        #expect(vm.user?.targetKcal == 2000)
        #expect(vm.draft.calories == "2000")
        #expect(await h.profileCache.user()?.targetKcal == 2000)
    }
}
