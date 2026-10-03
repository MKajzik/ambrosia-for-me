import Foundation
import Testing
@testable import Features

@Suite
@MainActor
struct AutosaverTests {
    final class Probe {
        var server = 0
        var draft = 0
        var performed: [Int] = []
        var failing = false
        var gate: Gate?
    }

    struct Boom: Error {}

    @MainActor
    struct Harness {
        let probe = Probe()
        let sleeper = TestSleeper()
        let saver: Autosaver<Int>

        init() {
            let probe = self.probe
            let sleeper = self.sleeper
            saver = Autosaver<Int>(
                sleep: { _ in try await sleeper.sleep() },
                pending: { probe.draft != probe.server ? probe.draft : nil },
                perform: { value in
                    probe.performed.append(value)
                    if let gate = probe.gate { await gate.wait() }
                    if probe.failing { throw Boom() }
                    probe.server = value
                }
            )
        }
    }

    @Test("Several quick edits make one save, after the pause")
    func debounceCoalesces() async {
        let h = Harness()
        h.probe.draft = 1; h.saver.schedule()
        h.probe.draft = 2; h.saver.schedule()
        h.probe.draft = 3; h.saver.schedule()
        await h.sleeper.fire()
        #expect(await waitUntil { h.probe.performed == [3] && !h.saver.isSaving })
        #expect(h.probe.server == 3)
    }

    @Test("Nothing pending schedules nothing")
    func nothingPending() async {
        let h = Harness()
        h.saver.schedule()
        await settle()
        #expect(await h.sleeper.pendingCount == 0)
        #expect(h.probe.performed.isEmpty)
    }

    @Test("One save at a time, and an edit made during a save is saved after it")
    func oneAtATime() async {
        let h = Harness()
        let gate = Gate()
        h.probe.gate = gate
        h.probe.draft = 1; h.saver.schedule()
        await h.sleeper.fire()
        #expect(await waitUntil { h.saver.isSaving })
        h.probe.draft = 2; h.saver.schedule()
        await gate.release()
        #expect(await waitUntil { !h.saver.isSaving })
        await h.sleeper.fire()
        #expect(await waitUntil { h.probe.performed == [1, 2] && !h.saver.isSaving })
    }

    @Test("A value that failed is not retried by itself; Try again retries it at once")
    func failedValueNotAutoRetried() async {
        let h = Harness()
        h.probe.failing = true
        h.probe.draft = 5; h.saver.schedule()
        await h.sleeper.fire()
        #expect(await waitUntil { h.saver.saveError != nil })
        #expect(h.saver.saveError == "Something went wrong. Please try again.")
        await settle()
        #expect(h.probe.performed == [5])
        #expect(await h.sleeper.pendingCount == 0)

        h.probe.failing = false
        await h.saver.retry()
        #expect(h.probe.performed == [5, 5])
        #expect(h.saver.saveError == nil)
        #expect(h.probe.server == 5)
    }

    @Test("The next edit after a failure tries again")
    func nextEditRetries() async {
        let h = Harness()
        h.probe.failing = true
        h.probe.draft = 5; h.saver.schedule()
        await h.sleeper.fire()
        #expect(await waitUntil { h.saver.saveError != nil })
        h.probe.failing = false
        h.probe.draft = 6; h.saver.schedule()
        await h.sleeper.fire()
        #expect(await waitUntil { h.probe.performed == [5, 6] && !h.saver.isSaving })
        #expect(h.saver.saveError == nil)
    }

    @Test("forget() lets the same value be tried again")
    func forgetClearsTriedValue() async {
        let h = Harness()
        h.probe.failing = true
        h.probe.draft = 5; h.saver.schedule()
        await h.sleeper.fire()
        #expect(await waitUntil { h.saver.saveError != nil })
        h.saver.forget()
        h.saver.schedule()
        await h.sleeper.fire()
        #expect(await waitUntil { h.probe.performed == [5, 5] })
    }

    @Test("flush saves a pending edit at once, without the pause")
    func flushSavesNow() async {
        let h = Harness()
        h.probe.draft = 9
        h.saver.flush()
        #expect(await waitUntil { h.probe.performed == [9] && !h.saver.isSaving })
        #expect(await h.sleeper.pendingCount == 0)
    }

    @Test("flush with nothing pending does nothing")
    func flushWithNothingPending() async {
        let h = Harness()
        h.saver.flush()
        await settle()
        #expect(h.probe.performed.isEmpty)
    }
}
