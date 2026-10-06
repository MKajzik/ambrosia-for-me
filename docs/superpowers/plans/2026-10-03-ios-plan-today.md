# iOS Plan and Today Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the Today and Plan tab placeholders with the real feature: macro rings and editable slot rows on Today, a Monday-to-Sunday week plan with totals and "apply template", and diet templates (list, partner's templates and copy, autosaving editor), all on the existing API with no contract change.

**Architecture:** The Meals plan's layers are reused unchanged. `Persistence` gains a per-day `PlanCache` (one row per date) and a `TemplateCache`; `Repositories` gains `PlanRepository` and `TemplatesRepository`; one range-parameterised `PlanViewModel` drives both Today (one day) and Plan (one week). Writes go straight to the API and are followed by a separate refresh of the affected dates, so totals always come from `GET /plan`. The meal editor's autosave logic is first extracted into a shared `Autosaver` that the template editor also uses.

**Tech Stack:** Swift 6.2 (Swift 6 mode), SwiftUI, SwiftData (`@ModelActor`), Observation, Swift Testing, XCTest/XCUITest, the existing Swift OpenAPI client. No new package dependencies; no change to `openapi.yaml`.

**Spec:** `docs/superpowers/specs/2026-10-03-ios-plan-today-design.md` (this plan implements all of it). Read also `docs/superpowers/specs/2026-10-02-ios-meals-design.md` and `docs/superpowers/plans/2026-10-03-ios-meals.md` (whose conventions this plan reuses), `ios/CLAUDE.md`, and the web parity code the spec cites: `web/src/features/plan/`, `web/src/lib/use-today.ts`, `web/src/lib/nutrition/progress.ts`, `web/src/lib/dates.ts`.

## Global Constraints

- iOS 26+ only; the package also declares macOS 15 so `swift test` runs natively. Any UIKit-only SwiftUI modifier or API (`.keyboardType`, `.navigationBarTitleDisplayMode`, `UIApplication`) sits behind `#if os(iOS)` (use the helpers in `Features/Shared/ViewHelpers.swift`).
- Swift 6 language mode, strict concurrency. View models are `@Observable @MainActor`. In `@MainActor` test suites, a `static` used from a `@Sendable` route closure must be `nonisolated`, and any nested `Harness` struct must be `@MainActor`.
- The generated `Components.Schemas.*` types are the domain types. Generated ids and dates (`format: date`) are `String`. Never hand-edit `Sources/API/GeneratedSources`.
- Only `Sources/API`, `Sources/Auth` and `Sources/Repositories` may `import OpenAPIRuntime` / `HTTPTypes`. Views and view models never touch SwiftData or an HTTP status. `@Model` objects never leave `Persistence`.
- Writes are never optimistic and never queued. Nutrition shown is always the API's answer; the only client arithmetic is summing and averaging the server's per-day totals for the week summary (a nutrient unknown on any day stays unknown, never 0).
- Dates are the device's local calendar day as `YYYY-MM-DD`, never UTC; weeks start Monday; date and number text is fixed en-US.
- Limits: portion more than 0 and at most 100; template name trimmed 1–200; template day count 1–31; template `dayIndex` within `0..<dayCount`. Autosave delay 700 ms (the shared engine's default). Template and plan list refresh walks every page at limit 100.
- Tests first. Small commits, one logical change each. Commit trailer: `Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>`.
- `swift test` runs from `ios/MealPlannerKit` (filter with `--filter <Suite>`). Compiling the UI-test target needs `xcodebuild build-for-testing` (`make build-ios` does not compile it). Local XCUITest is unreliable under load; GitHub CI is the signal for the `ui` job. The registration form's password field drops typed characters on CI: UI flows that are not about registration create their account through the API and sign in.

## Decisions this plan adds to the spec

These come from reading the generated client, the existing Meals code and the backend while planning. Challenge them at review.

1. **`MealScope` is reused for templates** (Mine / Partner's). Renaming it would touch merged code for no behavior change; the `TemplateCache` documents the reuse.
2. **Removing an already-empty slot is a success.** `DELETE /plan/{date}/{slot}` answers `404` when nothing is there; the repository treats that as "goal met", as `MealsViewModel.delete` does for a vanished meal.
3. **A write while another is running is refused with text, not queued.** `PlanViewModel` allows one write at a time ("Wait for the current change to finish."), which also makes the dimmed range unambiguous.
4. **The week summary appears only when all seven days are in the cache.** An unfetched date is absent, not empty, so showing a total from a partial week would be wrong.
5. **`Autosaver` is `@Observable` and the editors forward `isSaving`/`saveError` to it,** so existing views and tests keep reading the same properties.
6. **Template slots are saved in a canonical order** (day, then breakfast/lunch/dinner/snack, then insertion order) on both sides of the diff, so the server's ordering never reads as an edit.
7. **Slot actions are a visible "⋯" menu plus a long-press context menu; no swipe actions.** The four slots of a day are one grouped list row, so a single set of sheets (picker, portion, clear-snacks confirmation, error alert) serves the whole day. A sheet attached to each of four list rows would present four times. Spec §6 said "swipe actions and a context menu"; the docs task updates it.
8. **Task order puts the template screens (and the meal picker they share) before Today and Plan,** because Plan's toolbar pushes `TemplatesView`.

## Review Focus

Spec-implied conditions no spec test table names; each has a test in the owning task.

- **A date that was never fetched versus a day with nothing planned.** Unfetched is absent (no totals, no "0"); a fetched empty day shows empty slots. — Tasks 2 (`WeekTotals`), 3 (cache), 7 (view model).
- **Midnight and time zones.** Today must follow the local date (not UTC) after midnight and on foreground. — Tasks 2 (`LocalDay`), 7 (`followToday`).
- **Missing or zero targets.** No percent, never NaN or a divide by zero; one hint line. — Tasks 2 (`targetProgress`, ring speech), 12 (views).
- **Clearing an empty slot or all snacks when there are none.** Success, not an error. — Tasks 5, 7.
- **The partner unlinks or unshares while a partner template is open.** `404` shows "not available anymore", hides the segment, clears the cache. — Tasks 6, 10, 11.

## File Structure

```
ios/MealPlannerKit/
  Sources/
    Persistence/CachedPlanModels.swift                 (create)
    Persistence/PlanCache.swift                        (create)
    Persistence/CachedTemplate.swift                   (create)
    Persistence/TemplateCache.swift                    (create)
    Persistence/CacheStore.swift                       (modify: schema + factories)
    Repositories/PlanError.swift                       (create)
    Repositories/PlanRepository.swift                  (create)
    Repositories/TemplatesError.swift                  (create)
    Repositories/TemplatesRepository.swift             (create)
    Features/Shared/Autosaver.swift                    (create)
    Features/Shared/LocalDay.swift                     (create)
    Features/Shared/TargetProgress.swift               (create)
    Features/Shared/Portion.swift                      (create)
    Features/Shared/ErrorText.swift                    (modify)
    Features/Shared/ViewHelpers.swift                  (modify)
    Features/Shared/Nutrition/NutrientMath.swift       (create: sum, scale, WeekTotals)
    Features/Shared/Nutrition/NutritionFormat.swift    (modify: ring speech)
    Features/Shared/RingView.swift                     (create)
    Features/Shared/MealPicker/MealPickerViewModel.swift, MealPickerView.swift (create)
    Features/Shared/Slots/SlotRowView.swift, DaySlotsView.swift, PortionSheetView.swift (create)
    Features/Meals/MealEditorViewModel.swift           (modify: use Autosaver)
    Features/Plan/PlanDependencies.swift               (create)
    Features/Plan/PlanViewModel.swift                  (create)
    Features/Plan/ApplyTemplateViewModel.swift         (create)
    Features/Plan/PlanView.swift                       (replace placeholder)
    Features/Plan/WeekSummaryView.swift, ApplyTemplateView.swift (create)
    Features/Plan/Templates/TemplateDraft.swift, TemplatesViewModel.swift,
      TemplateEditorViewModel.swift, TemplatesView.swift, NewTemplateForm.swift,
      TemplateSheetView.swift, TemplateEditorView.swift, TemplateReadOnlyView.swift (create)
    Features/Today/TodayView.swift                     (replace placeholder)
    AppCore/RootView.swift, TabShellView.swift         (modify)
  Tests/MealPlannerKitTests/
    PlanFixtures.swift, TemplateFixtures.swift, PlanTestSupport.swift (create: shared test helpers)
    AutosaverTests, LocalDayTests, TargetProgressTests, PortionTests, NutrientMathTests,
    PlanCacheTests, TemplateCacheTests, PlanRepositoryTests, TemplatesRepositoryTests,
    PlanViewModelTests, MealPickerViewModelTests, ApplyTemplateViewModelTests, TemplateDraftTests,
    TemplatesViewModelTests, TemplateEditorViewModelTests                     (create)
ios/MealPlannerUITests/AppUITestCase.swift             (modify: API helpers)
ios/MealPlannerUITests/PlanTodayFlowUITests.swift      (create)
ios/CLAUDE.md                                          (modify)
```

---

## Task 1: Extract the shared `Autosaver` and move `MealEditorViewModel` onto it

**Files:**
- Create: `ios/MealPlannerKit/Sources/Features/Shared/Autosaver.swift`
- Replace: `ios/MealPlannerKit/Sources/Features/Meals/MealEditorViewModel.swift`
- Test: `ios/MealPlannerKit/Tests/MealPlannerKitTests/AutosaverTests.swift`

**Interfaces:**
- Consumes: `ErrorText.message(for:)`; test helpers `TestSleeper`, `Gate`, `waitUntil`, `settle` (`TestSupport.swift`).
- Produces: `Autosaver<Value: Equatable & Sendable>` (`@Observable @MainActor`): `init(sleep:pending:perform:)`, `isSaving`, `saveError`, `schedule()`, `retry() async`, `flush()`, `forget()`. `pending: @MainActor () -> Value?` returns the valid value that differs from what the server holds (or `nil`); `perform: @MainActor (Value) async throws -> Void` does the write. The existing `MealEditorViewModel` public API is unchanged; its 18 tests are the safety net and must pass unmodified.

- [ ] **Step 1: Write the failing tests** — `AutosaverTests.swift`

```swift
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
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd ios/MealPlannerKit && swift test --filter AutosaverTests`
Expected: FAIL to compile ("cannot find 'Autosaver' in scope").

- [ ] **Step 3: Implement `Autosaver.swift`**

```swift
import Observation

/// The autosave engine shared by the meal and template editors (a port of web's `useAutosave`).
/// Saves once the value from `pending` has stopped changing for 700 ms; one save at a time; an edit made during
/// a save is saved after it; a value that was tried (saved or failed) is not tried again by itself, so a failing
/// server is not hammered: the next edit, `retry()` or leaving the editor tries again.
///
/// The client owns what "the server holds": `pending` returns the valid draft value when it differs from that
/// (or `nil`), and `perform` writes it and records the new baseline.
@Observable
@MainActor
public final class Autosaver<Value: Equatable & Sendable> {
    public private(set) var isSaving = false
    public private(set) var saveError: String?

    @ObservationIgnored private var settled: Value?
    @ObservationIgnored private var debounceTask: Task<Void, Never>?
    @ObservationIgnored private var isRunning = false
    @ObservationIgnored private let pending: @MainActor () -> Value?
    @ObservationIgnored private let perform: @MainActor (Value) async throws -> Void
    @ObservationIgnored private let sleep: @Sendable (Duration) async throws -> Void

    public init(
        sleep: @escaping @Sendable (Duration) async throws -> Void = { try await Task.sleep(for: $0) },
        pending: @escaping @MainActor () -> Value?,
        perform: @escaping @MainActor (Value) async throws -> Void
    ) {
        self.sleep = sleep
        self.pending = pending
        self.perform = perform
    }

    /// Call after every edit: restarts the pause, unless a save is running or the value was already tried.
    public func schedule() {
        debounceTask?.cancel()
        debounceTask = nil
        guard !isSaving, let value = pending(), value != settled else { return }
        let sleep = self.sleep
        debounceTask = Task { [weak self] in
            do { try await sleep(.milliseconds(700)) } catch { return }
            guard !Task.isCancelled else { return }
            await self?.run()
        }
    }

    /// "Try again": forget that the value was tried, and save now.
    public func retry() async {
        settled = nil
        await run()
    }

    /// Leaving the editor (sheet dismissed, or the scene leaving the foreground) saves a pending edit in an
    /// unstructured task, so dismissing the view does not cancel it. A flush that fails after the sheet is gone
    /// is not surfaced; web has the same limit.
    public func flush() {
        debounceTask?.cancel()
        debounceTask = nil
        Task { await self.run() }
    }

    /// A new baseline was adopted (the editor loaded or reloaded the server's copy).
    public func forget() {
        settled = nil
    }

    private func run() async {
        guard !isRunning, let value = pending() else { return }
        isRunning = true
        isSaving = true
        saveError = nil
        do {
            try await perform(value)
        } catch {
            saveError = ErrorText.message(for: error)
        }
        settled = value
        isRunning = false
        isSaving = false
        schedule()
    }
}
```

- [ ] **Step 4: Run to verify the new tests pass**

Run: `cd ios/MealPlannerKit && swift test --filter AutosaverTests`
Expected: PASS (8 tests).

- [ ] **Step 5: Move `MealEditorViewModel` onto the engine**

Replace `ios/MealPlannerKit/Sources/Features/Meals/MealEditorViewModel.swift` with (the public API, strings and behaviour are unchanged; only the autosave machinery moved):

```swift
import API
import Foundation
import Observation
import Repositories

/// Loads one meal and, for the owner, edits it with autosave (the shared `Autosaver`: saves once the valid draft has
/// stopped changing for 700 ms and differs from what the server holds; fields (`PATCH`) then ingredients (`PUT`),
/// each step committed on its own).
@Observable
@MainActor
public final class MealEditorViewModel {
    public enum Phase: Equatable, Sendable {
        case loading
        case ready
        case unavailable(String)
        case failed(String)
    }

    public let mealID: String
    public let partnerLinked: Bool
    public private(set) var phase: Phase = .loading
    /// The server's latest answer. Feeds the nutrition panel; the draft is copied from it only once.
    public private(set) var meal: Components.Schemas.Meal?

    private var storedDraft = MealDraft()
    /// Set through here so every edit, from a binding or a method, schedules autosave. Reads and writes go
    /// through `storedDraft`, which is observed, so views update.
    public var draft: MealDraft {
        get { storedDraft }
        set {
            storedDraft = newValue
            autosaver.schedule()
        }
    }

    /// What the server holds, in the shape validation produces. `nil` until an owned meal has loaded.
    private var saved: MealDraft.Valid?

    @ObservationIgnored private var adoptedDraft: MealDraft?
    @ObservationIgnored private let repository: MealsRepository
    @ObservationIgnored private let sleep: @Sendable (Duration) async throws -> Void
    @ObservationIgnored private lazy var autosaver = Autosaver<MealDraft.Valid>(
        sleep: sleep,
        pending: { [weak self] in self?.pendingValue },
        perform: { [weak self] value in try await self?.save(value) }
    )

    public init(
        mealID: String,
        repository: MealsRepository,
        partnerLinked: Bool,
        sleep: @escaping @Sendable (Duration) async throws -> Void = { try await Task.sleep(for: $0) }
    ) {
        self.mealID = mealID
        self.repository = repository
        self.partnerLinked = partnerLinked
        self.sleep = sleep
    }

    // MARK: Derived state

    public var isOwner: Bool { meal?.isOwner ?? false }
    public var offersSharing: Bool { partnerLinked || draft.shared }
    public var canAddIngredient: Bool { draft.canAddRow }
    public var isSaving: Bool { autosaver.isSaving }
    public var saveError: String? { autosaver.saveError }

    public var validationErrors: MealDraft.Errors? {
        if case .invalid(let errors) = draft.validate() { errors } else { nil }
    }

    /// The valid draft, when it differs from what the server holds.
    private var pendingValue: MealDraft.Valid? {
        guard let saved, case .valid(let value) = draft.validate(), value != saved else { return nil }
        return value
    }

    public var hasUnsaved: Bool { saved != nil && (validationErrors != nil || pendingValue != nil) }

    public var statusText: String {
        if isSaving { return "Saving…" }
        if validationErrors != nil { return "Fix the highlighted fields to save." }
        if pendingValue != nil { return "Unsaved changes" }
        return "All changes saved"
    }

    /// The failure banner shows only while something is still unsaved.
    public var bannerMessage: String? { hasUnsaved ? saveError : nil }

    // MARK: Loading

    public func load() async {
        if phase == .loading, let cached = await repository.cachedMeal(id: mealID) { adopt(cached) }
        do {
            let fresh = try await repository.refreshMeal(id: mealID)
            // While nothing was typed, take the server's meal (a stale cached copy must never become the base
            // of a later write). Once the person has typed, a refresh only updates the nutrition panel.
            if phase != .ready || draft == adoptedDraft { adopt(fresh) } else { meal = fresh }
        } catch MealsError.notFound {
            phase = .unavailable(ErrorText.message(for: MealsError.notFound))
        } catch {
            if phase != .ready { phase = .failed(ErrorText.message(for: error)) }
        }
    }

    private func adopt(_ loaded: Components.Schemas.Meal) {
        meal = loaded
        phase = .ready
        guard loaded.isOwner else { return }
        let adopted = MealDraft(meal: loaded)
        storedDraft = adopted
        adoptedDraft = adopted
        saved = MealDraft.saved(from: loaded)
        autosaver.forget()
    }

    // MARK: Editing

    public func addIngredient(_ ingredient: Components.Schemas.Ingredient) {
        var next = draft
        next.addRow(for: ingredient)
        draft = next
    }

    public func removeRows(at offsets: IndexSet) {
        var next = draft
        next.rows = next.rows.enumerated().filter { !offsets.contains($0.offset) }.map(\.element)
        draft = next
    }

    // MARK: Autosave

    /// The two writes, each committed on its own: a failure in the second leaves the first counted as saved.
    private func save(_ value: MealDraft.Valid) async throws {
        guard var current = saved else { return }
        let changes = MealDraft.changes(from: current, to: value)
        if let patch = changes.patch {
            meal = try await repository.update(id: mealID, patch)
            current = MealDraft.Valid(name: value.name, notes: value.notes, servings: value.servings, shared: value.shared, items: current.items)
            saved = current
        }
        if let items = changes.items {
            meal = try await repository.replaceIngredients(id: mealID, items)
            current.items = value.items
            saved = current
        }
    }

    /// "Try again": forget that the value was tried, and save now.
    public func retry() async {
        await autosaver.retry()
    }

    /// Leaving the editor saves a pending edit in an unstructured task, so dismissing the view does not cancel it.
    public func flushOnLeave() {
        autosaver.flush()
    }
}
```

- [ ] **Step 6: Run the whole suite, existing editor tests included, unmodified**

Run: `cd ios/MealPlannerKit && swift test`
Expected: PASS for every suite, in particular all 18 `MealEditorViewModelTests` and all 8 `AutosaverTests`. Run `swift test --filter MealEditorViewModelTests` five times: the timing-sensitive tests must be stable. If one fails, the bug is in the extraction (a save that overlaps, `saved` not committed between `PATCH` and `PUT`, `settled` not reset by `forget()`), not in the test.

- [ ] **Step 7: Commit**

```bash
git add ios/MealPlannerKit/Sources/Features/Shared/Autosaver.swift ios/MealPlannerKit/Sources/Features/Meals/MealEditorViewModel.swift ios/MealPlannerKit/Tests/MealPlannerKitTests/AutosaverTests.swift
git commit -m "refactor(ios): extract the autosave engine from the meal editor" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

## Task 2: Pure modules: `LocalDay`, `TargetProgress`, `Portion`, `NutrientMath`, `WeekTotals`, ring speech

**Files:**
- Create: `ios/MealPlannerKit/Sources/Features/Shared/LocalDay.swift`, `TargetProgress.swift`, `Portion.swift`
- Create: `ios/MealPlannerKit/Sources/Features/Shared/Nutrition/NutrientMath.swift`
- Modify: `ios/MealPlannerKit/Sources/Features/Shared/Nutrition/NutritionFormat.swift` (add `ringSpoken`)
- Test: `LocalDayTests.swift`, `TargetProgressTests.swift`, `PortionTests.swift`, `NutrientMathTests.swift`, and an added test in `NutritionTests.swift`

**Interfaces:**
- Consumes: `parseDecimal` (Meals plan Task 1), `NutrientKey.amount(in:)`, `NutritionFormat`.
- Produces:
  - `LocalDay(timeZone:now:)`: `today()`, `addDays(_:_:)`, `startOfWeek(_:)`, `weekDays(startingAt:)`, `longDate(_:)`, `heading(_:)`, `weekRange(startingAt:)`, `date(_:) -> Date?`, `day(from:) -> String`. All take and return `YYYY-MM-DD` strings; an unparseable string is returned unchanged.
  - `TargetProgress` (`fraction`, `percent`, `over`) and `targetProgress(value:target:) -> TargetProgress?`.
  - `Portion.value(_:) -> Double?` and `Portion.message`.
  - `Components.Schemas.NutrientAmounts.init(values:)`, `NutrientMath.sum(_:)`, `NutrientMath.scaled(_:by:)`, `WeekTotals.make(days:dates:) -> WeekTotals?` (`total`, `perDayAverage`).
  - `NutritionFormat.ringSpoken(amount:target:unit:) -> String`.

- [ ] **Step 1: Write the failing tests**

`LocalDayTests.swift`:

```swift
import Foundation
import Testing
@testable import Features

@Suite
struct LocalDayTests {
    private let utc = TimeZone(identifier: "UTC")!
    private func day(_ zone: TimeZone? = nil, now: Date = Date(timeIntervalSince1970: 1_790_000_000)) -> LocalDay {
        LocalDay(timeZone: zone ?? utc, now: { now })
    }

    @Test("addDays crosses month, year and leap-day boundaries")
    func addDays() {
        let d = day()
        #expect(d.addDays("2026-10-31", 1) == "2026-11-01")
        #expect(d.addDays("2026-12-31", 1) == "2027-01-01")
        #expect(d.addDays("2028-02-28", 1) == "2028-02-29")
        #expect(d.addDays("2026-03-01", -1) == "2026-02-28")
        #expect(d.addDays("2026-10-05", 0) == "2026-10-05")
        #expect(d.addDays("2026-10-05", 30) == "2026-11-04")
    }

    @Test("An unparseable day comes back unchanged")
    func unparseable() {
        #expect(day().addDays("nonsense", 1) == "nonsense")
        #expect(day().startOfWeek("2026-13-45") == "2026-13-45")
    }

    @Test("Weeks start on Monday, for every weekday")
    func startOfWeek() {
        let d = day()
        // 2026-10-05 is a Monday.
        for offset in 0...6 {
            #expect(d.startOfWeek(d.addDays("2026-10-05", offset)) == "2026-10-05", "offset \(offset)")
        }
        #expect(d.startOfWeek("2026-10-12") == "2026-10-12")
    }

    @Test("weekDays is seven consecutive days")
    func weekDays() {
        #expect(day().weekDays(startingAt: "2026-10-26") == [
            "2026-10-26", "2026-10-27", "2026-10-28", "2026-10-29", "2026-10-30", "2026-10-31", "2026-11-01",
        ])
    }

    @Test("A week stays seven days across a daylight-saving change, in both directions")
    func daylightSaving() {
        let ny = TimeZone(identifier: "America/New_York")!
        let d = day(ny)
        // Spring forward 2026-03-08, fall back 2026-11-01.
        #expect(d.weekDays(startingAt: "2026-03-02") == [
            "2026-03-02", "2026-03-03", "2026-03-04", "2026-03-05", "2026-03-06", "2026-03-07", "2026-03-08",
        ])
        #expect(d.addDays("2026-03-08", 1) == "2026-03-09")
        #expect(d.addDays("2026-03-02", 7) == "2026-03-09")
        #expect(d.addDays("2026-11-01", 1) == "2026-11-02")
        #expect(d.weekDays(startingAt: "2026-10-26").last == "2026-11-01")
    }

    @Test("today() is the local calendar day, not the UTC one")
    func todayIsLocal() {
        let formatter = ISO8601DateFormatter()
        let instant = formatter.date(from: "2026-10-05T22:30:00Z")!
        #expect(day(TimeZone(identifier: "UTC"), now: instant).today() == "2026-10-05")
        #expect(day(TimeZone(identifier: "Pacific/Auckland"), now: instant).today() == "2026-10-06")
        #expect(day(TimeZone(identifier: "America/Los_Angeles"), now: instant).today() == "2026-10-05")
        let lateEvening = formatter.date(from: "2026-10-06T06:30:00Z")!
        #expect(day(TimeZone(identifier: "America/Los_Angeles"), now: lateEvening).today() == "2026-10-05")
    }

    @Test("A date picker's Date and a day string convert both ways in the local zone")
    func conversions() {
        let ny = TimeZone(identifier: "America/New_York")!
        let d = day(ny)
        let date = d.date("2026-10-05")
        #expect(date != nil)
        #expect(d.day(from: date ?? Date()) == "2026-10-05")
        #expect(d.date("nonsense") == nil)
        // 23:30 New York time is still the 5th there, though it is the 6th in UTC.
        let late = ISO8601DateFormatter().date(from: "2026-10-06T03:30:00Z")!
        #expect(d.day(from: late) == "2026-10-05")
    }

    @Test("Headings and the week range, fixed en-US")
    func formatting() {
        let d = day()
        #expect(d.longDate("2026-10-05") == "Monday, October 5")
        #expect(d.heading("2026-10-05") == "Mon, Oct 5")
        #expect(d.weekRange(startingAt: "2026-10-05") == "Oct 5 – 11")
        #expect(d.weekRange(startingAt: "2026-10-26") == "Oct 26 – Nov 1")
        #expect(d.weekRange(startingAt: "2026-12-28") == "Dec 28 – Jan 3")
    }
}
```

`TargetProgressTests.swift`:

```swift
import Testing
@testable import Features

@Suite
struct TargetProgressTests {
    @Test("The share of the target reached")
    func share() {
        #expect(targetProgress(value: 1200, target: 2000) == TargetProgress(fraction: 0.6, percent: 60, over: false))
        #expect(targetProgress(value: 0, target: 2000) == TargetProgress(fraction: 0, percent: 0, over: false))
        #expect(targetProgress(value: 2000, target: 2000) == TargetProgress(fraction: 1, percent: 100, over: false))
    }

    @Test("Over target fills the ring but keeps the real percent")
    func over() {
        #expect(targetProgress(value: 2500, target: 2000) == TargetProgress(fraction: 1, percent: 125, over: true))
    }

    @Test("No progress without a usable target or amount: never NaN or infinite")
    func noProgress() {
        #expect(targetProgress(value: 1200, target: nil) == nil)
        #expect(targetProgress(value: 1200, target: 0) == nil)
        #expect(targetProgress(value: 1200, target: -5) == nil)
        #expect(targetProgress(value: nil, target: 2000) == nil)
        #expect(targetProgress(value: nil, target: nil) == nil)
        #expect(targetProgress(value: .nan, target: 2000) == nil)
        #expect(targetProgress(value: 1200, target: .nan) == nil)
        #expect(targetProgress(value: 1200, target: .infinity) == nil)
        #expect(targetProgress(value: -1, target: 2000) == nil)
    }
}
```

`PortionTests.swift`:

```swift
import Testing
@testable import Features

@Suite
struct PortionTests {
    @Test(arguments: [("1", 1.0), ("0.5", 0.5), ("1,5", 1.5), ("100", 100.0), ("0.01", 0.01), (" 2 ", 2.0), ("2.", 2.0)])
    func valid(text: String, expected: Double) {
        #expect(Portion.value(text) == expected)
    }

    @Test(arguments: ["0", "100.01", "101", "-1", "", "abc", "1e2", "0,0"])
    func invalid(text: String) {
        #expect(Portion.value(text) == nil)
    }

    @Test("The message names both limits")
    func message() {
        #expect(Portion.message == "Portion must be more than 0 and at most 100.")
    }
}
```

`NutrientMathTests.swift`:

```swift
import API
import Testing
@testable import Features

@Suite
struct NutrientMathTests {
    private func n(_ calories: Double?, protein: Double? = 0) -> Components.Schemas.NutrientAmounts {
        Fixtures.nutrients(calories: calories, protein: protein)
    }

    @Test("The sum of nothing is zero for every nutrient")
    func emptySum() {
        let sum = NutrientMath.sum([])
        #expect(NutrientKey.allCases.allSatisfy { $0.amount(in: sum) == 0 })
    }

    @Test("Adds nutrient by nutrient")
    func adds() {
        let sum = NutrientMath.sum([n(300, protein: 10), n(450.5, protein: 5)])
        #expect(sum.calories == 750.5)
        #expect(sum.protein == 15)
    }

    @Test("A nutrient unknown in any item is unknown in the sum; the rest stay known")
    func unknownPropagates() {
        let sum = NutrientMath.sum([n(300), n(nil), n(100)])
        #expect(sum.calories == nil)
        #expect(sum.protein == 0)
    }

    @Test("Scaling multiplies known amounts and leaves unknown ones unknown")
    func scales() {
        let scaled = NutrientMath.scaled(n(700, protein: nil), by: 1.0 / 7.0)
        #expect(abs((scaled.calories ?? 0) - 100) < 1e-9)
        #expect(scaled.protein == nil)
    }

    @Test("A week total needs all seven days: an unfetched date is absent, not empty")
    func weekTotalsNeedEveryDay() {
        let dates = ["a", "b", "c", "d", "e", "f", "g"]
        var days: [String: Components.Schemas.DailyTotal] = [:]
        for (i, date) in dates.enumerated() { days[date] = Fixtures.day(date, calories: Double(100 * (i + 1))) }
        let week = WeekTotals.make(days: days, dates: dates)
        #expect(week?.total.calories == 2800)
        #expect(week?.perDayAverage.calories == 400)

        days.removeValue(forKey: "d")
        #expect(WeekTotals.make(days: days, dates: dates) == nil)
        #expect(WeekTotals.make(days: [:], dates: dates) == nil)
    }

    @Test("A day with unknown calories makes the week's calories unknown")
    func weekTotalsUnknown() {
        let dates = ["a", "b"]
        let days = ["a": Fixtures.day("a", calories: 100), "b": Fixtures.day("b", calories: nil)]
        #expect(WeekTotals.make(days: days, dates: dates)?.total.calories == nil)
    }
}
```

`Fixtures.day` is created in Task 3's `PlanFixtures.swift`; to keep this task self-contained, create that file now with just the builders below, and Task 3 extends it:

`PlanFixtures.swift`:

```swift
import API
import Foundation

extension Fixtures {
    static func entry(
        id: String = UUID().uuidString, date: String = "2026-10-05",
        slot: Components.Schemas.Slot = .breakfast, mealID: String = "m1", mealName: String = "Oats", portion: Double = 1
    ) -> Components.Schemas.PlanEntry {
        .init(id: id, date: date, slot: slot, mealId: mealID, mealName: mealName, portion: portion, createdAt: date0, updatedAt: date0)
    }

    /// A day as `GET /plan` returns it. `calories` is the day's total (nil means unknown).
    static func day(_ date: String, entries: [Components.Schemas.PlanEntry] = [], calories: Double? = 0) -> Components.Schemas.DailyTotal {
        .init(date: date, entries: entries, nutritionPerDay: nutrients(calories: calories))
    }

    static func targets(
        kcal: Double? = 2000, protein: Double? = 100, carbs: Double? = 250, fat: Double? = 70
    ) -> Components.Schemas.Targets {
        .init(targetKcal: kcal, targetProteinG: protein, targetCarbsG: carbs, targetFatG: fat)
    }

    static func planRange(from: String, to: String, days: [Components.Schemas.DailyTotal], targets: Components.Schemas.Targets = targets()) -> String {
        json(Components.Schemas.PlanRange(from: from, to: to, days: days, targets: targets))
    }

    private static var date0: Date { date }
}
```

Add to `NutritionTests.swift` (inside the suite):

```swift
    @Test("Ring speech says amount, target and percent; no target and unknown are spoken plainly")
    func ringSpoken() {
        #expect(NutritionFormat.ringSpoken(amount: 520, target: 2000, unit: .kcal) == "520 of 2,000 kilocalories, 26 percent")
        #expect(NutritionFormat.ringSpoken(amount: 2500, target: 2000, unit: .kcal) == "2,500 of 2,000 kilocalories, 125 percent")
        #expect(NutritionFormat.ringSpoken(amount: 520, target: nil, unit: .kcal) == "520 kilocalories, no target")
        #expect(NutritionFormat.ringSpoken(amount: 520, target: 0, unit: .kcal) == "520 kilocalories, no target")
        #expect(NutritionFormat.ringSpoken(amount: nil, target: 2000, unit: .kcal) == "no data")
        #expect(NutritionFormat.ringSpoken(amount: 38, target: 100, unit: .g) == "38 of 100 grams, 38 percent")
    }
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd ios/MealPlannerKit && swift test --filter LocalDayTests --filter TargetProgressTests --filter PortionTests --filter NutrientMathTests --filter NutritionTests`
Expected: FAIL to compile (`LocalDay`, `targetProgress`, `Portion`, `NutrientMath`, `WeekTotals`, `ringSpoken` do not exist).

- [ ] **Step 3: Implement `LocalDay.swift`**

```swift
import Foundation

/// The device's local calendar day as `YYYY-MM-DD` (the API's `date`), never UTC, with weeks starting Monday.
/// Arithmetic goes through `Calendar.date(byAdding: .day)` on noon of the day, so a week is seven days across a
/// daylight-saving change. Text is fixed en-US (as web). An unparseable day is returned unchanged.
public struct LocalDay: Sendable {
    private let timeZone: TimeZone
    private let clock: @Sendable () -> Date

    public init(timeZone: TimeZone = .current, now: @escaping @Sendable () -> Date = { Date() }) {
        self.timeZone = timeZone
        self.clock = now
    }

    private var calendar: Calendar {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = timeZone
        calendar.locale = Locale(identifier: "en_US")
        return calendar
    }

    public func today() -> String { string(from: clock()) }

    /// The day as a `Date` (noon local), for a date picker; `nil` if unparseable.
    public func date(_ day: String) -> Date? { date(from: day) }

    /// The local calendar day of a `Date`, e.g. what a date picker produced.
    public func day(from date: Date) -> String { string(from: date) }

    public func addDays(_ day: String, _ count: Int) -> String {
        guard let date = date(from: day), let moved = calendar.date(byAdding: .day, value: count, to: date) else { return day }
        return string(from: moved)
    }

    /// The Monday of the week containing `day`.
    public func startOfWeek(_ day: String) -> String {
        guard let date = date(from: day) else { return day }
        let weekday = calendar.component(.weekday, from: date) // 1 = Sunday, 2 = Monday
        return addDays(day, -((weekday + 5) % 7))
    }

    public func weekDays(startingAt monday: String) -> [String] {
        (0..<7).map { addDays(monday, $0) }
    }

    /// "Monday, October 5"
    public func longDate(_ day: String) -> String { format(day, "EEEE, MMMM d") }

    /// "Mon, Oct 5"
    public func heading(_ day: String) -> String { format(day, "EEE, MMM d") }

    /// "Oct 5 – 11", or "Oct 26 – Nov 1" when the week spans two months.
    public func weekRange(startingAt monday: String) -> String {
        let sunday = addDays(monday, 6)
        guard let start = date(from: monday), let end = date(from: sunday) else { return monday }
        let sameMonth = calendar.component(.month, from: start) == calendar.component(.month, from: end)
        let endText = sameMonth ? format(sunday, "d") : format(sunday, "MMM d")
        return "\(format(monday, "MMM d")) – \(endText)"
    }

    private func format(_ day: String, _ pattern: String) -> String {
        guard let date = date(from: day) else { return day }
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US")
        formatter.calendar = calendar
        formatter.timeZone = timeZone
        formatter.dateFormat = pattern
        return formatter.string(from: date)
    }

    private func string(from date: Date) -> String {
        let parts = calendar.dateComponents([.year, .month, .day], from: date)
        return String(format: "%04d-%02d-%02d", parts.year ?? 0, parts.month ?? 0, parts.day ?? 0)
    }

    /// Noon of the day, so adding days never lands in a daylight-saving gap.
    private func date(from day: String) -> Date? {
        let parts = day.split(separator: "-").compactMap { Int($0) }
        guard parts.count == 3, day.split(separator: "-").map(\.count) == [4, 2, 2] else { return nil }
        var components = DateComponents()
        components.year = parts[0]
        components.month = parts[1]
        components.day = parts[2]
        components.hour = 12
        // `Calendar` rolls an impossible date (month 13, day 45) over instead of failing: reject those.
        guard let date = calendar.date(from: components) else { return nil }
        let check = calendar.dateComponents([.year, .month, .day], from: date)
        guard check.year == parts[0], check.month == parts[1], check.day == parts[2] else { return nil }
        return date
    }
}
```

- [ ] **Step 4: Implement `TargetProgress.swift`, `Portion.swift`**

```swift
/// How far `value` is towards `target`, ported from web's `targetProgress`.
public struct TargetProgress: Equatable, Sendable {
    /// How much of the ring to fill, 0 to 1 (a value over target fills it).
    public let fraction: Double
    /// The real ratio as a rounded percent, so 125 means 25% over.
    public let percent: Int
    public let over: Bool
}

/// `nil` when either side is missing, not finite, or the target is not a positive number: never NaN or infinity.
public func targetProgress(value: Double?, target: Double?) -> TargetProgress? {
    guard let value, let target, value.isFinite, target.isFinite, target > 0, value >= 0 else { return nil }
    let ratio = value / target
    return TargetProgress(fraction: min(ratio, 1), percent: Int((ratio * 100).rounded()), over: ratio > 1)
}
```

```swift
/// A portion typed by a person: more than 0 and at most 100 (the API's limits for a plan entry and a template slot).
public enum Portion {
    public static let message = "Portion must be more than 0 and at most 100."

    public static func value(_ text: String) -> Double? {
        guard case .value(let portion) = parseDecimal(text), portion > 0, portion <= 100 else { return nil }
        return portion
    }
}
```

- [ ] **Step 5: Implement `Nutrition/NutrientMath.swift` and `ringSpoken`**

```swift
import API

public extension Components.Schemas.NutrientAmounts {
    /// Builds the 18-nutrient value from one lookup per key.
    init(values: (NutrientKey) -> Double?) {
        self.init(
            calories: values(.calories), protein: values(.protein), carbohydrates: values(.carbohydrates),
            sugar: values(.sugar), fibre: values(.fibre), fat: values(.fat), saturatedFat: values(.saturatedFat),
            sodium: values(.sodium), potassium: values(.potassium), calcium: values(.calcium), iron: values(.iron),
            magnesium: values(.magnesium), zinc: values(.zinc), vitaminA: values(.vitaminA), vitaminC: values(.vitaminC),
            vitaminD: values(.vitaminD), vitaminB12: values(.vitaminB12), folate: values(.folate)
        )
    }
}

/// The only arithmetic the app does on nutrition: summing and scaling the server's own totals. A nutrient is unknown
/// (`nil`) in a sum if it is unknown in any item: unknown is never treated as zero.
public enum NutrientMath {
    public static func sum(_ list: [Components.Schemas.NutrientAmounts]) -> Components.Schemas.NutrientAmounts {
        .init { key in
            var total = 0.0
            for item in list {
                guard let amount = key.amount(in: item) else { return nil }
                total += amount
            }
            return total
        }
    }

    public static func scaled(_ nutrition: Components.Schemas.NutrientAmounts, by factor: Double) -> Components.Schemas.NutrientAmounts {
        .init { key in key.amount(in: nutrition).map { $0 * factor } }
    }
}

/// A week's total and per-day average from the server's per-day totals (the average is the total over seven days,
/// as web). Needs every date in the cache: a date that was never fetched is absent, not empty.
public struct WeekTotals: Equatable, Sendable {
    public let total: Components.Schemas.NutrientAmounts
    public let perDayAverage: Components.Schemas.NutrientAmounts

    public static func make(days: [String: Components.Schemas.DailyTotal], dates: [String]) -> WeekTotals? {
        let present = dates.compactMap { days[$0] }
        guard !dates.isEmpty, present.count == dates.count else { return nil }
        let total = NutrientMath.sum(present.map(\.nutritionPerDay))
        return WeekTotals(total: total, perDayAverage: NutrientMath.scaled(total, by: 1.0 / Double(dates.count)))
    }
}
```

Add inside `NutritionFormat` (in `NutritionFormat.swift`), before the private helper:

```swift
    /// VoiceOver text for a ring: amount, target and percent, or the amount and "no target", or "no data".
    /// The unit word appears once, after the target ("520 of 2,000 kilocalories, 26 percent").
    public static func ringSpoken(amount: Double?, target: Double?, unit: NutrientUnit) -> String {
        guard let amount else { return "no data" }
        guard let progress = targetProgress(value: amount, target: target), let target else {
            return "\(spoken(amount, unit: unit)), no target"
        }
        let amountNumber = spoken(amount, unit: unit).split(separator: " ", maxSplits: 1).first.map(String.init) ?? ""
        return "\(amountNumber) of \(spoken(target, unit: unit)), \(progress.percent) percent"
    }
```

- [ ] **Step 6: Run to verify they pass**

Run: `cd ios/MealPlannerKit && swift test --filter LocalDayTests --filter TargetProgressTests --filter PortionTests --filter NutrientMathTests --filter NutritionTests`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add ios/MealPlannerKit/Sources/Features/Shared ios/MealPlannerKit/Tests/MealPlannerKitTests/LocalDayTests.swift ios/MealPlannerKit/Tests/MealPlannerKitTests/TargetProgressTests.swift ios/MealPlannerKit/Tests/MealPlannerKitTests/PortionTests.swift ios/MealPlannerKit/Tests/MealPlannerKitTests/NutrientMathTests.swift ios/MealPlannerKit/Tests/MealPlannerKitTests/NutritionTests.swift ios/MealPlannerKit/Tests/MealPlannerKitTests/PlanFixtures.swift
git commit -m "feat(ios): add local-day, target progress, portion and nutrient math" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

## Task 3: `PlanCache` (per-day cache) and `CacheStore` factories

**Files:**
- Create: `ios/MealPlannerKit/Sources/Persistence/CachedPlanModels.swift`, `PlanCache.swift`
- Modify: `ios/MealPlannerKit/Sources/Persistence/CacheStore.swift`
- Test: `ios/MealPlannerKit/Tests/MealPlannerKitTests/PlanCacheTests.swift`

**Interfaces:**
- Consumes: generated `DailyTotal`, `Targets`; `Fixtures.day/entry/targets` (Task 2).
- Produces: `actor PlanCache` with `days(from:to:) -> [DailyTotal]` (cached dates in the range, in date order; an unfetched date is absent), `targets() -> Targets?`, `replace(days:targets:)` (upserts the given dates and the targets in one save, touching no other date), `clearAll()`; `CacheStore.makePlanCache(_ container:) -> PlanCache`. `CacheStore`'s schema now includes the plan models.

- [ ] **Step 1: Write the failing tests** — `PlanCacheTests.swift`

```swift
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
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd ios/MealPlannerKit && swift test --filter PlanCacheTests`
Expected: FAIL to compile (`makePlanCache` / `PlanCache` do not exist).

- [ ] **Step 3: Implement `CachedPlanModels.swift`**

```swift
import Foundation
import SwiftData

/// Internal to this module: `@Model` objects never leave `PlanCache`. A day is stored as the generated `DailyTotal`
/// JSON (its entries and the server's per-day nutrition), because nothing queries inside it.
@Model
final class CachedPlanDay {
    @Attribute(.unique) var date: String
    var json: Data

    init(date: String, json: Data) {
        self.date = date
        self.json = json
    }
}

/// The caller's targets, one row.
@Model
final class CachedTargets {
    @Attribute(.unique) var key: String
    var json: Data

    init(json: Data) {
        self.key = "me"
        self.json = json
    }
}
```

- [ ] **Step 4: Implement `PlanCache.swift`**

```swift
import API
import Foundation
import SwiftData

/// The plan cache: one row per date. Takes and returns generated value types; best-effort like `MealCache`
/// (rebuildable from the API, so a failed save is dropped, not surfaced).
@ModelActor
public actor PlanCache {
    /// The cached dates in `[from, to]`, in date order. A date that was never fetched is absent.
    public func days(from: String, to: String) -> [Components.Schemas.DailyTotal] {
        allDays()
            .filter { $0.date >= from && $0.date <= to }
            .sorted { $0.date < $1.date }
            .compactMap { try? JSONDecoder().decode(Components.Schemas.DailyTotal.self, from: $0.json) }
    }

    public func targets() -> Components.Schemas.Targets? {
        guard let row = (try? modelContext.fetch(FetchDescriptor<CachedTargets>()))?.first else { return nil }
        return try? JSONDecoder().decode(Components.Schemas.Targets.self, from: row.json)
    }

    /// Upserts each given date and the targets in one save. `GET /plan` returns every date in the range it was
    /// asked for, so a refresh replaces exactly that range; other dates are untouched.
    public func replace(days: [Components.Schemas.DailyTotal], targets: Components.Schemas.Targets) {
        let existing = Dictionary(allDays().map { ($0.date, $0) }, uniquingKeysWith: { first, _ in first })
        for day in days {
            guard let json = try? JSONEncoder().encode(day) else { continue }
            if let row = existing[day.date] {
                row.json = json
            } else {
                modelContext.insert(CachedPlanDay(date: day.date, json: json))
            }
        }
        if let json = try? JSONEncoder().encode(targets) {
            if let row = (try? modelContext.fetch(FetchDescriptor<CachedTargets>()))?.first {
                row.json = json
            } else {
                modelContext.insert(CachedTargets(json: json))
            }
        }
        try? modelContext.save()
    }

    public func clearAll() {
        for row in allDays() { modelContext.delete(row) }
        for row in (try? modelContext.fetch(FetchDescriptor<CachedTargets>())) ?? [] { modelContext.delete(row) }
        try? modelContext.save()
    }

    private func allDays() -> [CachedPlanDay] {
        (try? modelContext.fetch(FetchDescriptor<CachedPlanDay>())) ?? []
    }
}
```

- [ ] **Step 5: Modify `CacheStore.swift`**

Change the schema line to include the plan models, and add the factory beside `makeMealCache`:

```swift
    private static let schema = Schema([
        CachedMeal.self, CachedMealIngredient.self, CachedPlanDay.self, CachedTargets.self,
    ])
```

```swift
    public static func makePlanCache(_ container: ModelContainer) -> PlanCache {
        PlanCache(modelContainer: container)
    }
```

(Task 4 adds `CachedTemplate.self` to the same array.) Adding models to an existing store is a lightweight migration SwiftData does itself; if a device's store cannot open, `persistentContainer` already deletes and recreates it (cache is rebuildable).

- [ ] **Step 6: Run to verify it passes**

Run: `cd ios/MealPlannerKit && swift test --filter PlanCacheTests --filter MealCacheTests`
Expected: PASS (the Meals cache tests still pass with the larger schema).

- [ ] **Step 7: Commit**

```bash
git add ios/MealPlannerKit/Sources/Persistence ios/MealPlannerKit/Tests/MealPlannerKitTests/PlanCacheTests.swift
git commit -m "feat(ios): add the per-day plan cache" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

## Task 4: `TemplateCache`

**Files:**
- Create: `ios/MealPlannerKit/Sources/Persistence/CachedTemplate.swift`, `TemplateCache.swift`
- Modify: `ios/MealPlannerKit/Sources/Persistence/CacheStore.swift`
- Create: `ios/MealPlannerKit/Tests/MealPlannerKitTests/TemplateFixtures.swift`
- Test: `ios/MealPlannerKit/Tests/MealPlannerKitTests/TemplateCacheTests.swift`

**Interfaces:**
- Consumes: generated `DietTemplate`, `DietTemplateSummary`, `TemplateSlot`; `MealScope` (reused for templates: Mine / Partner's).
- Produces: `actor TemplateCache` with `summaries(scope:)`, `template(id:) -> DietTemplate?` (nil until opened), `replaceSummaries(_:scope:)`, `store(_ template:)`, `remove(id:)`, `clear(scope:)`, `clearAll()`; `CacheStore.makeTemplateCache(_:)`. Test builders `Fixtures.templateSlot/template/templateSummary/templateList`.

- [ ] **Step 1: Create the test builders** — `TemplateFixtures.swift`

```swift
import API
import Foundation

extension Fixtures {
    static func templateSlot(
        id: String = UUID().uuidString, dayIndex: Int = 0, slot: Components.Schemas.Slot = .breakfast,
        mealID: String = "m1", mealName: String = "Oats", portion: Double = 1
    ) -> Components.Schemas.TemplateSlot {
        .init(id: id, dayIndex: dayIndex, slot: slot, mealId: mealID, mealName: mealName, portion: portion)
    }

    static func template(
        id: String = "t1", name: String = "Cut week", dayCount: Int = 7, shared: Bool = false, isOwner: Bool = true,
        slots: [Components.Schemas.TemplateSlot] = []
    ) -> Components.Schemas.DietTemplate {
        .init(id: id, name: name, dayCount: dayCount, sharedWithPartner: shared, isOwner: isOwner, slots: slots,
              createdAt: date, updatedAt: date)
    }

    static func templateSummary(
        id: String = "t1", name: String = "Cut week", dayCount: Int = 7, shared: Bool = false
    ) -> Components.Schemas.DietTemplateSummary {
        .init(id: id, name: name, dayCount: dayCount, sharedWithPartner: shared, createdAt: date, updatedAt: date)
    }

    static func templateList(_ items: [Components.Schemas.DietTemplateSummary], next: String? = nil) -> String {
        json(Components.Schemas.DietTemplateList(items: items, nextCursor: next))
    }
}
```

- [ ] **Step 2: Write the failing tests** — `TemplateCacheTests.swift`

```swift
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
```

- [ ] **Step 3: Run to verify it fails**

Run: `cd ios/MealPlannerKit && swift test --filter TemplateCacheTests`
Expected: FAIL to compile (`makeTemplateCache` / `TemplateCache` do not exist).

- [ ] **Step 4: Implement `CachedTemplate.swift`**

```swift
import API
import Foundation
import SwiftData

/// Internal to this module. A template list row, plus the full generated `DietTemplate` as JSON once opened
/// (its slots are never queried, so they are not rows).
@Model
final class CachedTemplate {
    @Attribute(.unique) var id: String
    var scopeRaw: String
    var name: String
    var dayCount: Int
    var sharedWithPartner: Bool
    var createdAt: Date
    var updatedAt: Date
    /// `nil` means "summary only, not opened yet".
    var detailJSON: Data?

    init(summary: Components.Schemas.DietTemplateSummary, scopeRaw: String) {
        self.id = summary.id
        self.scopeRaw = scopeRaw
        self.name = summary.name
        self.dayCount = summary.dayCount
        self.sharedWithPartner = summary.sharedWithPartner
        self.createdAt = summary.createdAt
        self.updatedAt = summary.updatedAt
    }

    func apply(_ summary: Components.Schemas.DietTemplateSummary) {
        name = summary.name
        dayCount = summary.dayCount
        sharedWithPartner = summary.sharedWithPartner
        createdAt = summary.createdAt
        updatedAt = summary.updatedAt
    }

    var summary: Components.Schemas.DietTemplateSummary {
        .init(id: id, name: name, dayCount: dayCount, sharedWithPartner: sharedWithPartner, createdAt: createdAt, updatedAt: updatedAt)
    }

    /// The full template, or `nil` if it has not been opened (or its stored JSON can no longer be decoded, in which
    /// case refetching is the right answer).
    var template: Components.Schemas.DietTemplate? {
        guard let detailJSON else { return nil }
        return try? JSONDecoder().decode(Components.Schemas.DietTemplate.self, from: detailJSON)
    }
}
```

- [ ] **Step 5: Implement `TemplateCache.swift`**

```swift
import API
import Foundation
import SwiftData

/// The diet-template cache. Mirrors `MealCache` (same Mine / Partner's scope type, `MealScope`, reused rather than
/// renamed). Takes and returns generated value types; best-effort, rebuildable from the API.
@ModelActor
public actor TemplateCache {
    public func summaries(scope: MealScope) -> [Components.Schemas.DietTemplateSummary] {
        allRows()
            .filter { $0.scopeRaw == scope.rawValue }
            .sorted { $0.name.localizedCaseInsensitiveCompare($1.name) == .orderedAscending }
            .map(\.summary)
    }

    /// The full template, or `nil` if it has only ever been seen as a list row.
    public func template(id: String) -> Components.Schemas.DietTemplate? {
        allRows().first { $0.id == id }?.template
    }

    /// Upserts by id keeping existing detail, inserts new rows, and deletes ids absent from `summaries` within
    /// `scope` only, in one save.
    public func replaceSummaries(_ summaries: [Components.Schemas.DietTemplateSummary], scope: MealScope) {
        let all = allRows()
        let byID = Dictionary(all.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        for summary in summaries {
            if let row = byID[summary.id] {
                row.apply(summary)
                row.scopeRaw = scope.rawValue
            } else {
                modelContext.insert(CachedTemplate(summary: summary, scopeRaw: scope.rawValue))
            }
        }
        let incoming = Set(summaries.map(\.id))
        for row in all where row.scopeRaw == scope.rawValue && !incoming.contains(row.id) {
            modelContext.delete(row)
        }
        try? modelContext.save()
    }

    /// Stores a full template (an API answer). A new row's scope follows ownership; an existing row keeps its scope.
    public func store(_ template: Components.Schemas.DietTemplate) {
        let summary = Components.Schemas.DietTemplateSummary(
            id: template.id, name: template.name, dayCount: template.dayCount,
            sharedWithPartner: template.sharedWithPartner, createdAt: template.createdAt, updatedAt: template.updatedAt
        )
        let row: CachedTemplate
        if let existing = allRows().first(where: { $0.id == template.id }) {
            row = existing
            row.apply(summary)
        } else {
            row = CachedTemplate(summary: summary, scopeRaw: (template.isOwner ? MealScope.mine : .partner).rawValue)
            modelContext.insert(row)
        }
        row.detailJSON = try? JSONEncoder().encode(template)
        try? modelContext.save()
    }

    public func remove(id: String) {
        for row in allRows() where row.id == id { modelContext.delete(row) }
        try? modelContext.save()
    }

    public func clear(scope: MealScope) {
        for row in allRows() where row.scopeRaw == scope.rawValue { modelContext.delete(row) }
        try? modelContext.save()
    }

    public func clearAll() {
        for row in allRows() { modelContext.delete(row) }
        try? modelContext.save()
    }

    private func allRows() -> [CachedTemplate] {
        (try? modelContext.fetch(FetchDescriptor<CachedTemplate>())) ?? []
    }
}
```

- [ ] **Step 6: Modify `CacheStore.swift`**

Add the model to the schema and the factory:

```swift
    private static let schema = Schema([
        CachedMeal.self, CachedMealIngredient.self, CachedPlanDay.self, CachedTargets.self, CachedTemplate.self,
    ])
```

```swift
    public static func makeTemplateCache(_ container: ModelContainer) -> TemplateCache {
        TemplateCache(modelContainer: container)
    }
```

- [ ] **Step 7: Run to verify it passes**

Run: `cd ios/MealPlannerKit && swift test --filter TemplateCacheTests --filter PlanCacheTests --filter MealCacheTests`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add ios/MealPlannerKit/Sources/Persistence ios/MealPlannerKit/Tests/MealPlannerKitTests/TemplateFixtures.swift ios/MealPlannerKit/Tests/MealPlannerKitTests/TemplateCacheTests.swift
git commit -m "feat(ios): add the diet template cache" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

## Task 5: `PlanRepository`

**Files:**
- Create: `ios/MealPlannerKit/Sources/Repositories/PlanError.swift`, `PlanRepository.swift`
- Test: `ios/MealPlannerKit/Tests/MealPlannerKitTests/PlanRepositoryTests.swift`

**Interfaces:**
- Consumes: `PlanCache`, `unwrapping`, `ProblemText`, `BadRequest.problem`/`Conflict.problem` (Meals plan Task 4); `RoutingTransport`, `Locked` (`TestSupport.swift`); `Fixtures.day/entry/targets/planRange` (Task 2).
- Produces:
  - `PlanError` (`notFound`, `conflict`, `validationFailed(String)`, `unauthorized`, `rateLimited`, `server(String)`).
  - `PlanSnapshot` (`days: [DailyTotal]`, `targets: Targets?`).
  - `PlanRepository(client:cache:)`: `cached(from:to:) async -> PlanSnapshot`, `refresh(from:to:) async throws`, `setEntry(date:slot:mealID:portion:) async throws`, `clearSlot(date:slot:) async throws`, `apply(templateID:startDate:overwrite:) async throws`, `clearCaches() async`. Writes touch no cache; clearing an already-empty slot is a success.

- [ ] **Step 1: Write the failing tests** — `PlanRepositoryTests.swift`

```swift
import API
import Foundation
import Persistence
import Testing
@testable import Repositories

@Suite
struct PlanRepositoryTests {
    private func make(_ route: @escaping RoutingTransport.Route) throws -> (PlanRepository, PlanCache, RoutingTransport) {
        let transport = RoutingTransport(route)
        let cache = CacheStore.makePlanCache(try CacheStore.inMemoryContainer())
        return (PlanRepository(client: makeAuthlessClient(transport: transport), cache: cache), cache, transport)
    }

    @Test("refresh asks for the range, stores exactly those days and the targets")
    func refreshStoresRange() async throws {
        let (repo, cache, transport) = try make { _ in
            (200, Fixtures.planRange(from: "2026-10-05", to: "2026-10-06", days: [Fixtures.day("2026-10-05"), Fixtures.day("2026-10-06")], targets: Fixtures.targets(kcal: 1800)))
        }
        await cache.replace(days: [Fixtures.day("2026-10-20", calories: 7)], targets: Fixtures.targets())
        try await repo.refresh(from: "2026-10-05", to: "2026-10-06")
        let snapshot = await repo.cached(from: "2026-10-05", to: "2026-10-31")
        #expect(snapshot.days.map(\.date) == ["2026-10-05", "2026-10-06", "2026-10-20"])
        #expect(snapshot.targets == Fixtures.targets(kcal: 1800))
        let path = try #require(await transport.calls("GET /plan").first?.path)
        #expect(path.contains("from=2026-10-05"))
        #expect(path.contains("to=2026-10-06"))
    }

    @Test("A failed refresh leaves the cache untouched")
    func failedRefreshKeepsCache() async throws {
        let (repo, cache, _) = try make { _ in (500, Fixtures.problem(500, code: "internal")) }
        await cache.replace(days: [Fixtures.day("2026-10-05", calories: 7)], targets: Fixtures.targets())
        await #expect(throws: PlanError.server("The server had a problem loading the plan.")) {
            try await repo.refresh(from: "2026-10-05", to: "2026-10-05")
        }
        #expect(await repo.cached(from: "2026-10-05", to: "2026-10-05").days.first?.nutritionPerDay.calories == 7)
    }

    @Test("setEntry PUTs the meal and portion to the date and slot; a snack goes to the snack slot")
    func setEntry() async throws {
        let (repo, cache, transport) = try make { _ in (200, Fixtures.json(Fixtures.entry())) }
        try await repo.setEntry(date: "2026-10-05", slot: .breakfast, mealID: "m1", portion: 1.5)
        try await repo.setEntry(date: "2026-10-05", slot: .snack, mealID: "m2", portion: 1)
        let breakfast = try #require(await transport.calls("PUT /plan/2026-10-05/breakfast").first)
        #expect(breakfast.body.contains("\"meal_id\":\"m1\""))
        #expect(breakfast.body.contains("\"portion\":1.5"))
        #expect(await transport.calls("PUT /plan/2026-10-05/snack").count == 1)
        // Writes touch no cache.
        #expect(await cache.days(from: "2000-01-01", to: "2100-01-01").isEmpty)
    }

    @Test("A 400 on setEntry becomes a readable validation message")
    func setEntryValidation() async throws {
        let (repo, _, _) = try make { _ in (400, Fixtures.problem(400, code: "validation_failed", errors: [("portion", "invalid_value")])) }
        await #expect(throws: PlanError.validationFailed("Portion is invalid.")) {
            try await repo.setEntry(date: "2026-10-05", slot: .lunch, mealID: "m1", portion: 0)
        }
    }

    @Test("clearSlot DELETEs; clearing an already-empty slot (404) is a success")
    func clearSlot() async throws {
        let status = Locked(204)
        let (repo, _, transport) = try make { _ in
            status.value == 204 ? (204, "") : (status.value, Fixtures.problem(status.value, code: "not_found"))
        }
        try await repo.clearSlot(date: "2026-10-05", slot: .dinner)
        status.set(404)
        try await repo.clearSlot(date: "2026-10-05", slot: .snack)
        #expect(await transport.calls("DELETE /plan/2026-10-05/dinner").count == 1)
        #expect(await transport.calls("DELETE /plan/2026-10-05/snack").count == 1)
        status.set(500)
        await #expect(throws: PlanError.server("The server had a problem clearing the slot.")) {
            try await repo.clearSlot(date: "2026-10-05", slot: .dinner)
        }
    }

    @Test("apply POSTs the start date and overwrite flag; 409 plan_conflict is a conflict, 404 is not found")
    func apply() async throws {
        let status = Locked(204)
        let (repo, _, transport) = try make { _ in
            switch status.value {
            case 204: return (204, "")
            case 409: return (409, Fixtures.problem(409, code: "plan_conflict"))
            default: return (404, Fixtures.problem(404, code: "not_found"))
            }
        }
        try await repo.apply(templateID: "t1", startDate: "2026-10-05", overwrite: true)
        let call = try #require(await transport.calls("POST /diet-templates/t1/apply").first)
        #expect(call.body.contains("\"start_date\":\"2026-10-05\""))
        #expect(call.body.contains("\"overwrite\":true"))
        status.set(409)
        await #expect(throws: PlanError.conflict) { try await repo.apply(templateID: "t1", startDate: "2026-10-05", overwrite: false) }
        status.set(404)
        await #expect(throws: PlanError.notFound) { try await repo.apply(templateID: "t1", startDate: "2026-10-05", overwrite: false) }
    }

    @Test("A 409 that is not plan_conflict is a plain server error")
    func otherConflict() async throws {
        let (repo, _, _) = try make { _ in (409, Fixtures.problem(409, code: "something_else", title: "Conflict")) }
        await #expect(throws: PlanError.server("Conflict")) { try await repo.apply(templateID: "t1", startDate: "2026-10-05", overwrite: false) }
    }

    @Test("A transport failure reaches the caller as a plain URLError")
    func transportFailureIsUnwrapped() async throws {
        let (repo, _, _) = try make { _ in throw URLError(.notConnectedToInternet) }
        await #expect(throws: URLError.self) { try await repo.refresh(from: "2026-10-05", to: "2026-10-05") }
        await #expect(throws: URLError.self) { try await repo.setEntry(date: "2026-10-05", slot: .lunch, mealID: "m1", portion: 1) }
    }

    @Test("429 and 401 map to their own errors")
    func commonStatuses() async throws {
        let status = Locked(429)
        let (repo, _, _) = try make { _ in (status.value, Fixtures.problem(status.value, code: "x")) }
        await #expect(throws: PlanError.rateLimited) { try await repo.refresh(from: "2026-10-05", to: "2026-10-05") }
        status.set(401)
        await #expect(throws: PlanError.unauthorized) { try await repo.refresh(from: "2026-10-05", to: "2026-10-05") }
    }

    @Test("clearCaches empties the plan cache")
    func clearCaches() async throws {
        let (repo, cache, _) = try make { _ in (500, "") }
        await cache.replace(days: [Fixtures.day("2026-10-05")], targets: Fixtures.targets())
        await repo.clearCaches()
        #expect(await repo.cached(from: "2000-01-01", to: "2100-01-01").days.isEmpty)
        #expect(await repo.cached(from: "2000-01-01", to: "2100-01-01").targets == nil)
    }
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd ios/MealPlannerKit && swift test --filter PlanRepositoryTests`
Expected: FAIL to compile (`PlanRepository`, `PlanError` do not exist).

- [ ] **Step 3: Implement `PlanError.swift`**

```swift
import API

public enum PlanError: Error, Equatable, Sendable {
    /// `404` on apply: the template does not exist (or is the partner's, which cannot be applied).
    case notFound
    /// `409 plan_conflict` on apply: a non-snack slot in the range already has a meal and `overwrite` was false.
    case conflict
    case validationFailed(String)
    case unauthorized
    case rateLimited
    case server(String)

    static func unexpected(_ status: Int) -> PlanError { .server("Unexpected response (\(status)).") }

    static func validation(_ problem: Components.Schemas.Problem?) -> PlanError {
        .validationFailed(problem.map(ProblemText.validation) ?? "The request was not accepted.")
    }

    /// Only `plan_conflict` is the "ask before replacing" case; any other `409` is just an error.
    static func applyConflict(_ problem: Components.Schemas.Problem?) -> PlanError {
        if problem?.code == "plan_conflict" { return .conflict }
        return .server(problem?.detail ?? problem?.title ?? "The request conflicts with the current state.")
    }
}
```

- [ ] **Step 4: Implement `PlanRepository.swift`**

```swift
import API
import Foundation
import Persistence

public struct PlanSnapshot: Equatable, Sendable {
    public var days: [Components.Schemas.DailyTotal]
    public var targets: Components.Schemas.Targets?
}

/// The plan, cache-first: `cached` then `refresh`. Writes (`setEntry`, `clearSlot`, `apply`) touch no cache, because
/// totals come from `GET /plan`; the caller refreshes the dates a write affected.
public struct PlanRepository: Sendable {
    private let client: Client
    private let cache: PlanCache

    public init(client: Client, cache: PlanCache) {
        self.client = client
        self.cache = cache
    }

    public func cached(from: String, to: String) async -> PlanSnapshot {
        PlanSnapshot(days: await cache.days(from: from, to: to), targets: await cache.targets())
    }

    /// `GET /plan` for `[from, to]`; replaces exactly those dates and the targets. A failure leaves the cache untouched.
    public func refresh(from: String, to: String) async throws {
        let response = try await unwrapping { try await client.getPlan(.init(query: .init(from: from, to: to))) }
        switch response {
        case .ok(let ok):
            let range = try ok.body.json
            await cache.replace(days: range.days, targets: range.targets)
        case .badRequest(let r): throw PlanError.validation(r.problem)
        case .unauthorized: throw PlanError.unauthorized
        case .conflict(let r): throw PlanError.server(r.problem?.detail ?? r.problem?.title ?? "The request conflicts with the current state.")
        case .tooManyRequests: throw PlanError.rateLimited
        case .internalServerError: throw PlanError.server("The server had a problem loading the plan.")
        case .undocumented(let status, _): throw PlanError.unexpected(status)
        }
    }

    /// Sets or swaps the meal for a date and slot (a snack is always added: the API cannot address one among several).
    public func setEntry(date: String, slot: Components.Schemas.Slot, mealID: String, portion: Double) async throws {
        let response = try await unwrapping {
            try await client.setPlanEntry(.init(path: .init(date: date, slot: slot), body: .json(.init(mealId: mealID, portion: portion))))
        }
        switch response {
        case .ok: return
        case .badRequest(let r): throw PlanError.validation(r.problem)
        case .unauthorized: throw PlanError.unauthorized
        case .tooManyRequests: throw PlanError.rateLimited
        case .internalServerError: throw PlanError.server("The server had a problem saving the plan.")
        case .undocumented(let status, _): throw PlanError.unexpected(status)
        }
    }

    /// Removes the entry for a date and slot (for a snack, every snack that day). Clearing a slot that is already
    /// empty (`404`) is a success: the goal is met.
    public func clearSlot(date: String, slot: Components.Schemas.Slot) async throws {
        let response = try await unwrapping { try await client.deletePlanEntry(.init(path: .init(date: date, slot: slot))) }
        switch response {
        case .noContent, .notFound: return
        case .unauthorized: throw PlanError.unauthorized
        case .tooManyRequests: throw PlanError.rateLimited
        case .internalServerError: throw PlanError.server("The server had a problem clearing the slot.")
        case .undocumented(let status, _): throw PlanError.unexpected(status)
        }
    }

    /// Copies a template of mine into the plan from `startDate`. Without `overwrite`, `409 plan_conflict` when a
    /// non-snack slot in the range already has a meal. A partner's template answers `404`.
    public func apply(templateID: String, startDate: String, overwrite: Bool) async throws {
        let response = try await unwrapping {
            try await client.applyDietTemplate(.init(path: .init(id: templateID), body: .json(.init(startDate: startDate, overwrite: overwrite))))
        }
        switch response {
        case .noContent: return
        case .badRequest(let r): throw PlanError.validation(r.problem)
        case .unauthorized: throw PlanError.unauthorized
        case .notFound: throw PlanError.notFound
        case .conflict(let r): throw PlanError.applyConflict(r.problem)
        case .tooManyRequests: throw PlanError.rateLimited
        case .internalServerError: throw PlanError.server("The server had a problem applying the template.")
        case .undocumented(let status, _): throw PlanError.unexpected(status)
        }
    }

    /// Called whenever the session ends, so a second user on this device never sees the first user's plan.
    public func clearCaches() async { await cache.clearAll() }
}
```

- [ ] **Step 5: Run to verify it passes**

Run: `cd ios/MealPlannerKit && swift test --filter PlanRepositoryTests`
Expected: PASS. If an `Output` enum has a case the switches above do not list (or one that does not exist), the compiler names it: the generated enums are the source of truth, mirror them with the same one-line mapping.

- [ ] **Step 6: Commit**

```bash
git add ios/MealPlannerKit/Sources/Repositories ios/MealPlannerKit/Tests/MealPlannerKitTests/PlanRepositoryTests.swift
git commit -m "feat(ios): add PlanRepository" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

## Task 6: `TemplatesRepository`

**Files:**
- Create: `ios/MealPlannerKit/Sources/Repositories/TemplatesError.swift`, `TemplatesRepository.swift`
- Test: `ios/MealPlannerKit/Tests/MealPlannerKitTests/TemplatesRepositoryTests.swift`

**Interfaces:**
- Consumes: `TemplateCache`, `MealScope` (typealias in `Repositories`), `unwrapping`, `ProblemText`, `problem` helpers; `Fixtures.template*` (Task 4).
- Produces:
  - `TemplatesError` (`notFound`, `partnerNotLinked`, `validationFailed(String)`, `unauthorized`, `rateLimited`, `server(String)`).
  - `TemplatesRepository(client:cache:)`: `cachedTemplates(_:) async -> [DietTemplateSummary]`, `refreshTemplates(_:) async throws`, `cachedTemplate(id:) async -> DietTemplate?`, `refreshTemplate(id:) async throws -> DietTemplate`, `create(name:dayCount:) async throws -> DietTemplate`, `update(id:_: UpdateDietTemplateRequest) async throws -> DietTemplate`, `replaceSlots(id:_: [TemplateSlotInput]) async throws -> DietTemplate`, `copy(id:) async throws -> DietTemplate`, `delete(id:) async throws`, `clearPartnerTemplates() async`, `clearCaches() async`. Each write's answer replaces the cache entry.

- [ ] **Step 1: Write the failing tests** — `TemplatesRepositoryTests.swift`

```swift
import API
import Foundation
import Persistence
import Testing
@testable import Repositories

@Suite
struct TemplatesRepositoryTests {
    private func make(_ route: @escaping RoutingTransport.Route) throws -> (TemplatesRepository, TemplateCache, RoutingTransport) {
        let transport = RoutingTransport(route)
        let cache = CacheStore.makeTemplateCache(try CacheStore.inMemoryContainer())
        return (TemplatesRepository(client: makeAuthlessClient(transport: transport), cache: cache), cache, transport)
    }

    @Test("Refresh walks every page at limit 100 and replaces the scope in one go")
    func walksPages() async throws {
        let (repo, cache, transport) = try make { call in
            switch (call.route, call.path.contains("cursor=c2")) {
            case ("GET /diet-templates", false):
                return (200, Fixtures.templateList([Fixtures.templateSummary(id: "a", name: "A"), Fixtures.templateSummary(id: "b", name: "B")], next: "c2"))
            case ("GET /diet-templates", true):
                return (200, Fixtures.templateList([Fixtures.templateSummary(id: "c", name: "C")]))
            default:
                return (500, Fixtures.problem(500, code: "internal"))
            }
        }
        try await repo.refreshTemplates(.mine)
        #expect(await cache.summaries(scope: .mine).map(\.id) == ["a", "b", "c"])
        let calls = await transport.calls("GET /diet-templates")
        #expect(calls.count == 2)
        #expect(calls.allSatisfy { $0.path.contains("limit=100") })
    }

    @Test("A failing second page leaves the cache untouched")
    func failedPageKeepsCache() async throws {
        let (repo, cache, _) = try make { call in
            call.path.contains("cursor=c2")
                ? (500, Fixtures.problem(500, code: "internal"))
                : (200, Fixtures.templateList([Fixtures.templateSummary(id: "new", name: "New")], next: "c2"))
        }
        await cache.replaceSummaries([Fixtures.templateSummary(id: "old", name: "Old")], scope: .mine)
        await #expect(throws: TemplatesError.server("The server had a problem loading your templates.")) {
            try await repo.refreshTemplates(.mine)
        }
        #expect(await cache.summaries(scope: .mine).map(\.id) == ["old"])
    }

    @Test("Partner scope reads /partner/diet-templates; 404 clears only that scope")
    func partnerScope() async throws {
        let (repo, cache, _) = try make { call in
            call.route == "GET /partner/diet-templates"
                ? (404, Fixtures.problem(404, code: "partner_not_linked"))
                : (500, Fixtures.problem(500, code: "internal"))
        }
        await cache.replaceSummaries([Fixtures.templateSummary(id: "p")], scope: .partner)
        await cache.replaceSummaries([Fixtures.templateSummary(id: "m")], scope: .mine)
        await #expect(throws: TemplatesError.partnerNotLinked) { try await repo.refreshTemplates(.partner) }
        #expect(await cache.summaries(scope: .partner).isEmpty)
        #expect(await cache.summaries(scope: .mine).map(\.id) == ["m"])
    }

    @Test("refreshTemplate caches the full template; a 404 removes the entry and throws notFound")
    func refreshTemplate() async throws {
        let live = Locked(true)
        let (repo, cache, _) = try make { _ in
            live.value
                ? (200, Fixtures.json(Fixtures.template(slots: [Fixtures.templateSlot()])))
                : (404, Fixtures.problem(404, code: "not_found"))
        }
        let template = try await repo.refreshTemplate(id: "t1")
        #expect(template.slots.count == 1)
        #expect(await repo.cachedTemplate(id: "t1")?.name == "Cut week")
        live.set(false)
        await #expect(throws: TemplatesError.notFound) { try await repo.refreshTemplate(id: "t1") }
        #expect(await cache.template(id: "t1") == nil)
    }

    @Test("create sends the name and day count and caches the answer")
    func create() async throws {
        let (repo, cache, transport) = try make { _ in (201, Fixtures.json(Fixtures.template(id: "made", name: "Cut", dayCount: 5))) }
        let made = try await repo.create(name: "Cut", dayCount: 5)
        #expect(made.id == "made")
        let body = try #require(await transport.calls("POST /diet-templates").first?.body)
        #expect(body.contains("\"name\":\"Cut\""))
        #expect(body.contains("\"day_count\":5"))
        #expect(await cache.template(id: "made") != nil)
    }

    @Test("A 400 becomes a readable validation message")
    func validation() async throws {
        let (repo, _, _) = try make { _ in (400, Fixtures.problem(400, code: "validation_failed", errors: [("name", "too_long")])) }
        await #expect(throws: TemplatesError.validationFailed("Name is too long.")) { try await repo.create(name: "x", dayCount: 3) }
    }

    @Test("update sends only the patched fields; replaceSlots PUTs the whole list, an empty one included")
    func updateAndReplace() async throws {
        let (repo, cache, transport) = try make { _ in (200, Fixtures.json(Fixtures.template(name: "New"))) }
        _ = try await repo.update(id: "t1", .init(name: "New"))
        let patch = try #require(await transport.calls("PATCH /diet-templates/t1").first)
        #expect(patch.body.contains("\"name\":\"New\""))
        #expect(!patch.body.contains("shared_with_partner"))
        _ = try await repo.replaceSlots(id: "t1", [])
        let put = try #require(await transport.calls("PUT /diet-templates/t1/slots").first)
        #expect(put.body.contains("\"items\":[]"))
        #expect(await cache.template(id: "t1")?.name == "New")
    }

    @Test("copy returns the full template and caches it")
    func copy() async throws {
        let (repo, cache, _) = try make { _ in (201, Fixtures.json(Fixtures.template(id: "copy"))) }
        #expect(try await repo.copy(id: "theirs").id == "copy")
        #expect(await cache.template(id: "copy") != nil)
    }

    @Test("delete: 204 removes the entry; 404 removes it and throws notFound")
    func delete() async throws {
        let status = Locked(204)
        let (repo, cache, _) = try make { _ in
            status.value == 204 ? (204, "") : (404, Fixtures.problem(404, code: "not_found"))
        }
        await cache.store(Fixtures.template())
        try await repo.delete(id: "t1")
        #expect(await cache.template(id: "t1") == nil)
        await cache.store(Fixtures.template())
        status.set(404)
        await #expect(throws: TemplatesError.notFound) { try await repo.delete(id: "t1") }
        #expect(await cache.template(id: "t1") == nil)
    }

    @Test("A transport failure reaches the caller as a plain URLError")
    func transportFailureIsUnwrapped() async throws {
        let (repo, _, _) = try make { _ in throw URLError(.notConnectedToInternet) }
        await #expect(throws: URLError.self) { try await repo.refreshTemplates(.mine) }
        await #expect(throws: URLError.self) { try await repo.refreshTemplate(id: "x") }
    }

    @Test("429 and 401 map to their own errors")
    func commonStatuses() async throws {
        let status = Locked(429)
        let (repo, _, _) = try make { _ in (status.value, Fixtures.problem(status.value, code: "x")) }
        await #expect(throws: TemplatesError.rateLimited) { try await repo.refreshTemplates(.mine) }
        status.set(401)
        await #expect(throws: TemplatesError.unauthorized) { try await repo.refreshTemplates(.mine) }
    }

    @Test("clearPartnerTemplates and clearCaches")
    func clearing() async throws {
        let (repo, cache, _) = try make { _ in (500, "") }
        await cache.replaceSummaries([Fixtures.templateSummary(id: "p")], scope: .partner)
        await cache.replaceSummaries([Fixtures.templateSummary(id: "m")], scope: .mine)
        await repo.clearPartnerTemplates()
        #expect(await repo.cachedTemplates(.partner).isEmpty)
        #expect(await repo.cachedTemplates(.mine).map(\.id) == ["m"])
        await repo.clearCaches()
        #expect(await repo.cachedTemplates(.mine).isEmpty)
    }
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd ios/MealPlannerKit && swift test --filter TemplatesRepositoryTests`
Expected: FAIL to compile.

- [ ] **Step 3: Implement `TemplatesError.swift`**

```swift
import API

public enum TemplatesError: Error, Equatable, Sendable {
    case notFound
    case partnerNotLinked
    case validationFailed(String)
    case unauthorized
    case rateLimited
    case server(String)

    static func unexpected(_ status: Int) -> TemplatesError { .server("Unexpected response (\(status)).") }

    static func validation(_ problem: Components.Schemas.Problem?) -> TemplatesError {
        .validationFailed(problem.map(ProblemText.validation) ?? "The request was not accepted.")
    }

    static func conflict(_ problem: Components.Schemas.Problem?) -> TemplatesError {
        .server(problem?.detail ?? problem?.title ?? "The request conflicts with the current state.")
    }
}
```

- [ ] **Step 4: Implement `TemplatesRepository.swift`**

```swift
import API
import Foundation
import Persistence

/// Diet templates, cache-first like `MealsRepository`. Each write's answer is the full template and replaces the
/// cache entry. Applying a template is `PlanRepository.apply`: it belongs to the plan, not the library.
public struct TemplatesRepository: Sendable {
    private let client: Client
    private let cache: TemplateCache
    private static let pageSize = 100

    public init(client: Client, cache: TemplateCache) {
        self.client = client
        self.cache = cache
    }

    // MARK: Reads

    public func cachedTemplates(_ scope: MealScope) async -> [Components.Schemas.DietTemplateSummary] {
        await cache.summaries(scope: scope)
    }

    public func cachedTemplate(id: String) async -> Components.Schemas.DietTemplate? {
        await cache.template(id: id)
    }

    /// Walks every page and replaces the scope's cached summaries in one transaction. A failure on any page leaves
    /// the cache untouched. `404 partner_not_linked` clears the partner scope.
    public func refreshTemplates(_ scope: MealScope) async throws {
        var all: [Components.Schemas.DietTemplateSummary] = []
        var cursor: String?
        repeat {
            let page = try await fetchPage(scope, cursor: cursor)
            all += page.items
            cursor = page.nextCursor
        } while cursor != nil
        await cache.replaceSummaries(all, scope: scope)
    }

    @discardableResult
    public func refreshTemplate(id: String) async throws -> Components.Schemas.DietTemplate {
        let response = try await unwrapping { try await client.getDietTemplate(.init(path: .init(id: id))) }
        switch response {
        case .ok(let ok):
            let template = try ok.body.json
            await cache.store(template)
            return template
        case .unauthorized: throw TemplatesError.unauthorized
        case .notFound:
            await cache.remove(id: id)
            throw TemplatesError.notFound
        case .tooManyRequests: throw TemplatesError.rateLimited
        case .internalServerError: throw TemplatesError.server("The server had a problem loading the template.")
        case .undocumented(let status, _): throw TemplatesError.unexpected(status)
        }
    }

    // MARK: Writes

    public func create(name: String, dayCount: Int) async throws -> Components.Schemas.DietTemplate {
        let response = try await unwrapping {
            try await client.createDietTemplate(.init(body: .json(.init(name: name, dayCount: dayCount))))
        }
        switch response {
        case .created(let created):
            let template = try created.body.json
            await cache.store(template)
            return template
        case .badRequest(let r): throw TemplatesError.validation(r.problem)
        case .unauthorized: throw TemplatesError.unauthorized
        case .tooManyRequests: throw TemplatesError.rateLimited
        case .internalServerError: throw TemplatesError.server("The server had a problem creating the template.")
        case .undocumented(let status, _): throw TemplatesError.unexpected(status)
        }
    }

    public func update(id: String, _ patch: Components.Schemas.UpdateDietTemplateRequest) async throws -> Components.Schemas.DietTemplate {
        let response = try await unwrapping {
            try await client.updateDietTemplate(.init(path: .init(id: id), body: .json(patch)))
        }
        switch response {
        case .ok(let ok):
            let template = try ok.body.json
            await cache.store(template)
            return template
        case .badRequest(let r): throw TemplatesError.validation(r.problem)
        case .unauthorized: throw TemplatesError.unauthorized
        case .notFound:
            await cache.remove(id: id)
            throw TemplatesError.notFound
        case .tooManyRequests: throw TemplatesError.rateLimited
        case .internalServerError: throw TemplatesError.server("The server had a problem saving the template.")
        case .undocumented(let status, _): throw TemplatesError.unexpected(status)
        }
    }

    public func replaceSlots(id: String, _ items: [Components.Schemas.TemplateSlotInput]) async throws -> Components.Schemas.DietTemplate {
        let response = try await unwrapping {
            try await client.replaceTemplateSlots(.init(path: .init(id: id), body: .json(.init(items: items))))
        }
        switch response {
        case .ok(let ok):
            let template = try ok.body.json
            await cache.store(template)
            return template
        case .badRequest(let r): throw TemplatesError.validation(r.problem)
        case .unauthorized: throw TemplatesError.unauthorized
        case .notFound:
            await cache.remove(id: id)
            throw TemplatesError.notFound
        case .conflict(let r): throw TemplatesError.conflict(r.problem)
        case .tooManyRequests: throw TemplatesError.rateLimited
        case .internalServerError: throw TemplatesError.server("The server had a problem saving the slots.")
        case .undocumented(let status, _): throw TemplatesError.unexpected(status)
        }
    }

    public func copy(id: String) async throws -> Components.Schemas.DietTemplate {
        let response = try await unwrapping { try await client.copyDietTemplate(.init(path: .init(id: id))) }
        switch response {
        case .created(let created):
            let template = try created.body.json
            await cache.store(template)
            return template
        case .unauthorized: throw TemplatesError.unauthorized
        case .notFound: throw TemplatesError.notFound
        case .tooManyRequests: throw TemplatesError.rateLimited
        case .internalServerError: throw TemplatesError.server("The server had a problem copying the template.")
        case .undocumented(let status, _): throw TemplatesError.unexpected(status)
        }
    }

    public func delete(id: String) async throws {
        let response = try await unwrapping { try await client.deleteDietTemplate(.init(path: .init(id: id))) }
        switch response {
        case .noContent:
            await cache.remove(id: id)
        case .unauthorized: throw TemplatesError.unauthorized
        case .notFound:
            await cache.remove(id: id)
            throw TemplatesError.notFound
        case .tooManyRequests: throw TemplatesError.rateLimited
        case .internalServerError: throw TemplatesError.server("The server had a problem deleting the template.")
        case .undocumented(let status, _): throw TemplatesError.unexpected(status)
        }
    }

    // MARK: Cache control

    public func clearPartnerTemplates() async { await cache.clear(scope: .partner) }

    /// Called whenever the session ends.
    public func clearCaches() async { await cache.clearAll() }

    // MARK: Pages

    private func fetchPage(_ scope: MealScope, cursor: String?) async throws -> Components.Schemas.DietTemplateList {
        switch scope {
        case .mine:
            let response = try await unwrapping {
                try await client.listDietTemplates(.init(query: .init(cursor: cursor, limit: Self.pageSize)))
            }
            switch response {
            case .ok(let ok): return try ok.body.json
            case .badRequest(let r): throw TemplatesError.validation(r.problem)
            case .unauthorized: throw TemplatesError.unauthorized
            case .tooManyRequests: throw TemplatesError.rateLimited
            case .internalServerError: throw TemplatesError.server("The server had a problem loading your templates.")
            case .undocumented(let status, _): throw TemplatesError.unexpected(status)
            }
        case .partner:
            let response = try await unwrapping {
                try await client.listPartnerDietTemplates(.init(query: .init(cursor: cursor, limit: Self.pageSize)))
            }
            switch response {
            case .ok(let ok): return try ok.body.json
            case .badRequest(let r): throw TemplatesError.validation(r.problem)
            case .unauthorized: throw TemplatesError.unauthorized
            case .notFound:
                await cache.clear(scope: .partner)
                throw TemplatesError.partnerNotLinked
            case .tooManyRequests: throw TemplatesError.rateLimited
            case .internalServerError: throw TemplatesError.server("The server had a problem loading your partner's templates.")
            case .undocumented(let status, _): throw TemplatesError.unexpected(status)
            }
        }
    }
}
```

- [ ] **Step 5: Run to verify it passes**

Run: `cd ios/MealPlannerKit && swift test --filter TemplatesRepositoryTests`
Expected: PASS (mirror any generated-enum mismatch as in Task 5).

- [ ] **Step 6: Commit**

```bash
git add ios/MealPlannerKit/Sources/Repositories ios/MealPlannerKit/Tests/MealPlannerKitTests/TemplatesRepositoryTests.swift
git commit -m "feat(ios): add TemplatesRepository" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

## Task 7: `PlanViewModel`, the in-memory plan server for tests, and error text

**Files:**
- Create: `ios/MealPlannerKit/Sources/Features/Plan/PlanViewModel.swift`
- Modify: `ios/MealPlannerKit/Sources/Features/Shared/ErrorText.swift`
- Create: `ios/MealPlannerKit/Tests/MealPlannerKitTests/PlanTestSupport.swift`
- Test: `ios/MealPlannerKit/Tests/MealPlannerKitTests/PlanViewModelTests.swift`

**Interfaces:**
- Consumes: `PlanRepository`, `PlanError`, `TemplatesError` (Tasks 5-6); `LocalDay`, `Portion` (Task 2); `ErrorText`; `Fixtures.*`; test helpers `RoutingTransport`, `Gate`, `Locked`, `waitUntil`.
- Produces:
  - `ErrorText` cases for `PlanError` and `TemplatesError`.
  - `PlanViewModel(plan:localDay:range:)` with `DateRange` (`from`, `to`, `contains(_:)`), `ApplyOutcome` (`.applied`, `.needsConfirmation`, `.failed(String)`), `localDay`, observable `range`, `days: [String: DailyTotal]`, `targets`, `isLoading`, `isStale`, `loadError`, `notice`, `busyRange`; `isWriting`, `isBusy(_:)`, `entries(on:slot:)`, `nutrition(on:)`; `load() async`, `setRange(from:to:) async`, `followToday() async`, `setMeal(date:slot:mealID:portion:) async -> String?`, `remove(date:slot:) async -> String?`, `clearSnacks(date:) async -> String?`, `apply(templateID:dayCount:startDate:overwrite:) async -> ApplyOutcome`; `PlanViewModel.refreshNotice`, `.busyMessage`.
  - Test helper `PlanServer` (in-memory `GET /plan`, `PUT`/`DELETE /plan/{date}/{slot}`, `POST /diet-templates/{id}/apply`).

- [ ] **Step 1: Create the in-memory plan server** — `PlanTestSupport.swift`

```swift
import API
import Foundation
@testable import Features

/// A tiny in-memory stand-in for the plan endpoints, so view-model tests read like the real round trip: the "server"
/// computes each day's calories (100 per portion), never the code under test.
final class PlanServer: @unchecked Sendable {
    struct Slot: Sendable {
        let dayIndex: Int
        let slot: Components.Schemas.Slot
        let mealID: String
        let portion: Double
    }

    struct Template: Sendable {
        let dayCount: Int
        let slots: [Slot]
    }

    private let lock = NSLock()
    private var byDate: [String: [Components.Schemas.PlanEntry]] = [:]
    private var templates: [String: Template]
    private let mealNames: [String: String]
    private var putGate: Gate?
    private var failPUT = false
    private var failGET = false
    private let days = LocalDay(timeZone: TimeZone(identifier: "UTC")!)

    init(
        entries: [Components.Schemas.PlanEntry] = [],
        templates: [String: Template] = [:],
        mealNames: [String: String] = ["m1": "Oats", "m2": "Rice"]
    ) {
        self.templates = templates
        self.mealNames = mealNames
        for entry in entries { byDate[entry.date, default: []].append(entry) }
    }

    func setPutGate(_ gate: Gate?) { lock.lock(); putGate = gate; lock.unlock() }
    func setFailPUT(_ value: Bool) { lock.lock(); failPUT = value; lock.unlock() }
    func setFailGET(_ value: Bool) { lock.lock(); failGET = value; lock.unlock() }

    func entries(on date: String) -> [Components.Schemas.PlanEntry] {
        lock.lock(); defer { lock.unlock() }
        return byDate[date] ?? []
    }

    func route(_ call: RoutingTransport.Call) async throws -> (status: Int, body: String) {
        let parts = call.route.split(separator: " ", maxSplits: 1).map(String.init)
        let segments = parts[1].split(separator: "/").map(String.init)
        if parts[0] == "GET", segments == ["plan"] { return try getPlan(call.path) }
        if parts[0] == "PUT", segments.count == 3, segments[0] == "plan" { return await putEntry(segments[1], segments[2], call.body) }
        if parts[0] == "DELETE", segments.count == 3, segments[0] == "plan" { return deleteEntry(segments[1], segments[2]) }
        if parts[0] == "POST", segments.count == 3, segments[0] == "diet-templates", segments[2] == "apply" { return apply(segments[1], call.body) }
        return (500, Fixtures.problem(500, code: "unrouted"))
    }

    private func getPlan(_ path: String) throws -> (status: Int, body: String) {
        lock.lock(); defer { lock.unlock() }
        if failGET { throw URLError(.notConnectedToInternet) }
        let items = URLComponents(string: "http://x" + path)?.queryItems ?? []
        guard let from = items.first(where: { $0.name == "from" })?.value, let to = items.first(where: { $0.name == "to" })?.value else {
            return (400, Fixtures.problem(400, code: "validation_failed"))
        }
        var result: [Components.Schemas.DailyTotal] = []
        var date = from
        while date <= to, result.count < 92 {
            let entries = (byDate[date] ?? []).sorted { Self.rank($0.slot) < Self.rank($1.slot) }
            let calories = entries.reduce(0.0) { $0 + $1.portion * 100 }
            result.append(Fixtures.day(date, entries: entries, calories: calories))
            date = days.addDays(date, 1)
        }
        return (200, Fixtures.planRange(from: from, to: to, days: result))
    }

    /// `NSLock` cannot be taken in an async function under Swift 6, so every locked region is its own sync method.
    private func putSettings() -> (gate: Gate?, failing: Bool) {
        lock.lock(); defer { lock.unlock() }
        return (putGate, failPUT)
    }

    private func putEntry(_ date: String, _ slotName: String, _ body: String) async -> (status: Int, body: String) {
        let settings = putSettings()
        if let gate = settings.gate { await gate.wait() }
        if settings.failing { return (500, Fixtures.problem(500, code: "internal")) }
        return storeEntry(date, slotName, body)
    }

    private func storeEntry(_ date: String, _ slotName: String, _ body: String) -> (status: Int, body: String) {
        guard let slot = Components.Schemas.Slot(rawValue: slotName),
              let object = (try? JSONSerialization.jsonObject(with: Data(body.utf8))) as? [String: Any],
              let mealID = object["meal_id"] as? String
        else { return (400, Fixtures.problem(400, code: "validation_failed")) }
        let portion = (object["portion"] as? Double) ?? 1
        let entry = Fixtures.entry(date: date, slot: slot, mealID: mealID, mealName: mealNames[mealID] ?? mealID, portion: portion)
        lock.lock(); defer { lock.unlock() }
        var list = byDate[date] ?? []
        if slot != .snack { list.removeAll { $0.slot == slot } }
        list.append(entry)
        byDate[date] = list
        return (200, Fixtures.json(entry))
    }

    private func deleteEntry(_ date: String, _ slotName: String) -> (status: Int, body: String) {
        guard let slot = Components.Schemas.Slot(rawValue: slotName) else { return (400, Fixtures.problem(400, code: "validation_failed")) }
        lock.lock(); defer { lock.unlock() }
        let before = byDate[date]?.count ?? 0
        byDate[date]?.removeAll { $0.slot == slot }
        return (byDate[date]?.count ?? 0) < before ? (204, "") : (404, Fixtures.problem(404, code: "not_found"))
    }

    private func apply(_ id: String, _ body: String) -> (status: Int, body: String) {
        lock.lock(); defer { lock.unlock() }
        guard let template = templates[id] else { return (404, Fixtures.problem(404, code: "not_found")) }
        guard let object = (try? JSONSerialization.jsonObject(with: Data(body.utf8))) as? [String: Any],
              let start = object["start_date"] as? String
        else { return (400, Fixtures.problem(400, code: "validation_failed")) }
        let overwrite = (object["overwrite"] as? Bool) ?? false
        let conflict = template.slots.contains { slot in
            slot.slot != .snack && (byDate[days.addDays(start, slot.dayIndex)] ?? []).contains { $0.slot == slot.slot }
        }
        if conflict, !overwrite { return (409, Fixtures.problem(409, code: "plan_conflict")) }
        for slot in template.slots {
            let date = days.addDays(start, slot.dayIndex)
            var list = byDate[date] ?? []
            if slot.slot != .snack { list.removeAll { $0.slot == slot.slot } }
            list.append(Fixtures.entry(date: date, slot: slot.slot, mealID: slot.mealID, mealName: mealNames[slot.mealID] ?? slot.mealID, portion: slot.portion))
            byDate[date] = list
        }
        return (204, "")
    }

    private static func rank(_ slot: Components.Schemas.Slot) -> Int {
        switch slot {
        case .breakfast: 0
        case .lunch: 1
        case .dinner: 2
        case .snack: 3
        }
    }
}
```

- [ ] **Step 2: Write the failing tests** — `PlanViewModelTests.swift`

```swift
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
```

- [ ] **Step 3: Run to verify it fails**

Run: `cd ios/MealPlannerKit && swift test --filter PlanViewModelTests --filter ErrorTextPlanTests`
Expected: FAIL to compile (`PlanViewModel` does not exist, `ErrorText` has no plan cases).

- [ ] **Step 4: Extend `ErrorText.swift`**

Add two cases to the `switch error` (before `case is URLError`):

```swift
        case let error as PlanError:
            switch error {
            case .notFound: return "That template isn't available anymore."
            case .conflict: return "Meals are already planned for some of those days."
            case .validationFailed(let message): return message
            case .unauthorized: return "Please sign in again."
            case .rateLimited: return rateLimited
            case .server(let message): return message
            }
        case let error as TemplatesError:
            switch error {
            case .notFound: return "This template isn't available anymore."
            case .partnerNotLinked: return "You're not linked with a partner."
            case .validationFailed(let message): return message
            case .unauthorized: return "Please sign in again."
            case .rateLimited: return rateLimited
            case .server(let message): return message
            }
```

- [ ] **Step 5: Implement `PlanViewModel.swift`**

```swift
import API
import Foundation
import Observation
import Repositories

/// The plan for a date range: Today is a one-day range, Plan a week, over the same cached rows. Reads render the
/// cache, await the refresh, re-read. A write (swap, portion, remove, clear snacks, apply) is the repository write
/// followed by a refresh of the affected dates, as two separate steps: totals always come from `GET /plan`, never
/// from the device, and a failed refresh after a good write is reported as exactly that.
@Observable
@MainActor
public final class PlanViewModel {
    public struct DateRange: Equatable, Sendable {
        public var from: String
        public var to: String

        public init(from: String, to: String) {
            self.from = from
            self.to = to
        }

        public func contains(_ day: String) -> Bool { day >= from && day <= to }
    }

    public enum ApplyOutcome: Equatable, Sendable {
        case applied
        case needsConfirmation
        case failed(String)
    }

    public static let refreshNotice = "Saved, but the totals could not be refreshed. Pull to refresh."
    public static let busyMessage = "Wait for the current change to finish."

    public let localDay: LocalDay
    public private(set) var range: DateRange
    /// The cached days of `range`, by date. A date that was never fetched is absent, not empty.
    public private(set) var days: [String: Components.Schemas.DailyTotal] = [:]
    public private(set) var targets: Components.Schemas.Targets?
    public private(set) var isLoading = false
    public private(set) var isStale = false
    public private(set) var loadError: String?
    public private(set) var notice: String?
    /// The dates a running write is about (and whose rings and totals dim); `nil` when idle.
    public private(set) var busyRange: DateRange?

    @ObservationIgnored private let plan: PlanRepository

    public init(plan: PlanRepository, localDay: LocalDay = LocalDay(), range: DateRange? = nil) {
        self.plan = plan
        self.localDay = localDay
        let today = localDay.today()
        self.range = range ?? DateRange(from: today, to: today)
    }

    public var isWriting: Bool { busyRange != nil }
    public func isBusy(_ day: String) -> Bool { busyRange?.contains(day) ?? false }

    public func entries(on day: String, slot: Components.Schemas.Slot) -> [Components.Schemas.PlanEntry] {
        (days[day]?.entries ?? []).filter { $0.slot == slot }
    }

    public func nutrition(on day: String) -> Components.Schemas.NutrientAmounts? {
        days[day]?.nutritionPerDay
    }

    // MARK: Reads

    /// Renders the cache at once, then awaits the refresh and re-reads. A failed refresh keeps what is shown and marks
    /// it stale; with nothing cached it shows an error state.
    public func load() async {
        let requested = range
        isLoading = true
        defer { isLoading = false }
        show(await plan.cached(from: requested.from, to: requested.to))
        do {
            try await plan.refresh(from: requested.from, to: requested.to)
            guard range == requested else { return }
            show(await plan.cached(from: requested.from, to: requested.to))
            isStale = false
            loadError = nil
            notice = nil
        } catch {
            guard range == requested else { return }
            isStale = true
            loadError = days.isEmpty ? ErrorText.message(for: error) : nil
        }
    }

    public func setRange(from: String, to: String) async {
        range = DateRange(from: from, to: to)
        await load()
    }

    /// Today's screen: moves to the local date if it changed (after midnight, or back on foreground), and reloads.
    public func followToday() async {
        let today = localDay.today()
        if range != DateRange(from: today, to: today) { range = DateRange(from: today, to: today) }
        await load()
    }

    // MARK: Writes (each returns the error text, or nil on success, so a sheet can show its own alert)

    public func setMeal(date: String, slot: Components.Schemas.Slot, mealID: String, portion: Double) async -> String? {
        let result = await write(refreshing: DateRange(from: date, to: date)) {
            try await plan.setEntry(date: date, slot: slot, mealID: mealID, portion: portion)
        }
        return failureText(result)
    }

    public func remove(date: String, slot: Components.Schemas.Slot) async -> String? {
        let result = await write(refreshing: DateRange(from: date, to: date)) {
            try await plan.clearSlot(date: date, slot: slot)
        }
        return failureText(result)
    }

    /// Removes every snack of the day (the API cannot address a single one).
    public func clearSnacks(date: String) async -> String? {
        await remove(date: date, slot: .snack)
    }

    /// Applies one of my templates. `.needsConfirmation` means `409 plan_conflict`: ask, then call again with
    /// `overwrite: true`. The applied range is refreshed afterwards.
    public func apply(templateID: String, dayCount: Int, startDate: String, overwrite: Bool) async -> ApplyOutcome {
        let end = localDay.addDays(startDate, max(dayCount, 1) - 1)
        let result = await write(refreshing: DateRange(from: startDate, to: end)) {
            try await plan.apply(templateID: templateID, startDate: startDate, overwrite: overwrite)
        }
        switch result {
        case .success:
            return .applied
        case .failure(let error):
            if let planError = error as? PlanError, planError == .conflict { return .needsConfirmation }
            return .failed(message(for: error))
        }
    }

    // MARK: Internals

    private struct WriteInProgress: Error {}

    private func write(refreshing target: DateRange, _ operation: () async throws -> Void) async -> Result<Void, Error> {
        guard busyRange == nil else { return .failure(WriteInProgress()) }
        busyRange = target
        notice = nil
        defer { busyRange = nil }
        do {
            try await operation()
        } catch {
            return .failure(error)
        }
        do {
            try await plan.refresh(from: target.from, to: target.to)
        } catch {
            isStale = true
            notice = Self.refreshNotice
        }
        show(await plan.cached(from: range.from, to: range.to))
        return .success(())
    }

    private func failureText(_ result: Result<Void, Error>) -> String? {
        if case .failure(let error) = result { return message(for: error) }
        return nil
    }

    private func message(for error: Error) -> String {
        error is WriteInProgress ? Self.busyMessage : ErrorText.message(for: error)
    }

    private func show(_ snapshot: PlanSnapshot) {
        days = Dictionary(snapshot.days.map { ($0.date, $0) }, uniquingKeysWith: { first, _ in first })
        targets = snapshot.targets
    }
}
```

- [ ] **Step 6: Run to verify it passes**

Run: `cd ios/MealPlannerKit && for i in 1 2 3; do swift test --filter PlanViewModelTests --filter ErrorTextPlanTests 2>&1 | grep -E "✘|Test run with"; done`
Expected: PASS, three times in a row (the gate-based test must be stable).

- [ ] **Step 7: Commit**

```bash
git add ios/MealPlannerKit/Sources/Features ios/MealPlannerKit/Tests/MealPlannerKitTests/PlanTestSupport.swift ios/MealPlannerKit/Tests/MealPlannerKitTests/PlanViewModelTests.swift
git commit -m "feat(ios): add PlanViewModel with write-then-refresh" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

## Task 8: Meal picker, apply-template view models, and `PlanDependencies`

**Files:**
- Create: `ios/MealPlannerKit/Sources/Features/Shared/MealPicker/MealPickerViewModel.swift`
- Create: `ios/MealPlannerKit/Sources/Features/Plan/ApplyTemplateViewModel.swift`, `PlanDependencies.swift`
- Test: `ios/MealPlannerKit/Tests/MealPlannerKitTests/MealPickerViewModelTests.swift`, `ApplyTemplateViewModelTests.swift`

**Interfaces:**
- Consumes: `MealsRepository` (`cachedMeals(.mine)`, `refreshMeals(.mine)`), `TemplatesRepository`, `PlanViewModel.apply` (Task 7), `PlanServer`, `Fixtures.*`.
- Produces:
  - `MealPickerViewModel(meals:)`: `query`, `meals`, `filtered`, `isLoading`, `errorMessage`, `isLibraryEmpty`, `load() async`.
  - `ApplyTemplateViewModel(templates:plan:startDate:)`: `templates`, `selectedID` (settable), `selected`, `startDate` (settable), `isLoading`, `isApplying`, `errorMessage`, `confirmingReplace` (settable), `didApply`; `load() async`, `apply() async`, `confirmReplace() async`.
  - `PlanDependencies(plan:templates:meals:partner:)`.

- [ ] **Step 1: Write the failing tests**

`MealPickerViewModelTests.swift`:

```swift
import API
import Foundation
import Persistence
import Repositories
import Testing
@testable import Features

@Suite
@MainActor
struct MealPickerViewModelTests {
    @MainActor
    struct Harness {
        let vm: MealPickerViewModel
        let cache: MealCache

        init(_ route: @escaping RoutingTransport.Route) throws {
            let transport = RoutingTransport(route)
            cache = CacheStore.makeMealCache(try CacheStore.inMemoryContainer())
            vm = MealPickerViewModel(meals: MealsRepository(client: makeAuthlessClient(transport: transport), cache: cache))
        }
    }

    private nonisolated static let meals = [Fixtures.summary(id: "a", name: "Oat porridge"), Fixtures.summary(id: "b", name: "Chicken rice"), Fixtures.summary(id: "c", name: "Greek salad")]

    @Test("Shows all of my meals, then filters locally, ignoring case and surrounding spaces")
    func filters() async throws {
        let h = try Harness { _ in (200, Fixtures.mealList(Self.meals)) }
        await h.vm.load()
        #expect(h.vm.filtered.map(\.name) == ["Chicken rice", "Greek salad", "Oat porridge"])
        h.vm.query = "  RICE "
        #expect(h.vm.filtered.map(\.name) == ["Chicken rice"])
        h.vm.query = "zzz"
        #expect(h.vm.filtered.isEmpty)
        h.vm.query = "   "
        #expect(h.vm.filtered.count == 3)
    }

    @Test("Offline with a cached library still lists it, with no error")
    func offlineKeepsCache() async throws {
        let h = try Harness { _ in throw URLError(.notConnectedToInternet) }
        await h.cache.replaceSummaries(Self.meals, scope: .mine)
        await h.vm.load()
        #expect(h.vm.meals.count == 3)
        #expect(h.vm.errorMessage == nil)
    }

    @Test("Offline with nothing cached shows an error, not an empty-library message")
    func offlineWithNothingCached() async throws {
        let h = try Harness { _ in throw URLError(.notConnectedToInternet) }
        await h.vm.load()
        #expect(h.vm.errorMessage == "Can't reach the server. Check your connection and try again.")
        #expect(h.vm.isLibraryEmpty == false)
    }

    @Test("An empty library is reported as such")
    func emptyLibrary() async throws {
        let h = try Harness { _ in (200, Fixtures.mealList([])) }
        await h.vm.load()
        #expect(h.vm.isLibraryEmpty)
    }

    @Test("Only my own meals are listed: the partner's endpoint is never called")
    func onlyMine() async throws {
        let transport = Locked<[String]>([])
        let h = try Harness { call in
            transport.mutate { $0.append(call.route) }
            return (200, Fixtures.mealList([]))
        }
        await h.vm.load()
        #expect(transport.value == ["GET /meals"])
    }
}
```

`ApplyTemplateViewModelTests.swift`:

```swift
import API
import Foundation
import Persistence
import Repositories
import Testing
@testable import Features

@Suite
@MainActor
struct ApplyTemplateViewModelTests {
    @MainActor
    struct Harness {
        let vm: ApplyTemplateViewModel
        let server: PlanServer
        let transport: RoutingTransport

        init(server: PlanServer, summaries: [Components.Schemas.DietTemplateSummary], startDate: String = "2026-10-05") throws {
            self.server = server
            transport = RoutingTransport { call in
                call.route == "GET /diet-templates" ? (200, Fixtures.templateList(summaries)) : try await server.route(call)
            }
            let client = makeAuthlessClient(transport: transport)
            let planRepository = PlanRepository(client: client, cache: CacheStore.makePlanCache(try CacheStore.inMemoryContainer()))
            let templates = TemplatesRepository(client: client, cache: CacheStore.makeTemplateCache(try CacheStore.inMemoryContainer()))
            let plan = PlanViewModel(plan: planRepository, localDay: LocalDay(timeZone: TimeZone(identifier: "UTC")!))
            vm = ApplyTemplateViewModel(templates: templates, plan: plan, startDate: startDate)
        }
    }

    private static let template = PlanServer.Template(dayCount: 3, slots: [.init(dayIndex: 0, slot: .breakfast, mealID: "m1", portion: 1)])

    @Test("Lists my templates only and selects the first by default")
    func loadsMineAndSelectsFirst() async throws {
        let h = try Harness(server: PlanServer(templates: ["t1": Self.template]), summaries: [Fixtures.templateSummary(id: "t1", name: "Cut", dayCount: 3)])
        await h.vm.load()
        #expect(h.vm.templates.map(\.id) == ["t1"])
        #expect(h.vm.selectedID == "t1")
        #expect(await h.transport.calls.map(\.route) == ["GET /diet-templates"])
    }

    @Test("A choice the person already made is kept when the list reloads")
    func keepsSelection() async throws {
        let h = try Harness(server: PlanServer(), summaries: [Fixtures.templateSummary(id: "a", name: "A"), Fixtures.templateSummary(id: "b", name: "B")])
        await h.vm.load()
        h.vm.selectedID = "b"
        await h.vm.load()
        #expect(h.vm.selectedID == "b")
    }

    @Test("Applying with no template asks for one")
    func noTemplate() async throws {
        let h = try Harness(server: PlanServer(), summaries: [])
        await h.vm.load()
        await h.vm.apply()
        #expect(h.vm.errorMessage == "Choose a template.")
        #expect(h.vm.didApply == false)
    }

    @Test("A clean apply uses the chosen start date and marks done")
    func applies() async throws {
        let server = PlanServer(templates: ["t1": Self.template])
        let h = try Harness(server: server, summaries: [Fixtures.templateSummary(id: "t1", dayCount: 3)], startDate: "2026-10-12")
        await h.vm.load()
        await h.vm.apply()
        #expect(h.vm.didApply)
        #expect(h.vm.errorMessage == nil)
        #expect(server.entries(on: "2026-10-12").map(\.mealName) == ["Oats"])
    }

    @Test("A conflict asks to replace; confirming replaces and finishes; the question is not an error")
    func conflictThenConfirm() async throws {
        let server = PlanServer(entries: [Fixtures.entry(date: "2026-10-05", slot: .breakfast, mealName: "Old")], templates: ["t1": Self.template])
        let h = try Harness(server: server, summaries: [Fixtures.templateSummary(id: "t1", dayCount: 3)])
        await h.vm.load()
        await h.vm.apply()
        #expect(h.vm.confirmingReplace)
        #expect(h.vm.didApply == false)
        #expect(h.vm.errorMessage == nil)
        await h.vm.confirmReplace()
        #expect(h.vm.confirmingReplace == false)
        #expect(h.vm.didApply)
        #expect(server.entries(on: "2026-10-05").map(\.mealName) == ["Oats"])
    }

    @Test("Declining the replacement leaves the plan alone")
    func declining() async throws {
        let server = PlanServer(entries: [Fixtures.entry(date: "2026-10-05", slot: .breakfast, mealName: "Old")], templates: ["t1": Self.template])
        let h = try Harness(server: server, summaries: [Fixtures.templateSummary(id: "t1", dayCount: 3)])
        await h.vm.load()
        await h.vm.apply()
        h.vm.confirmingReplace = false
        #expect(server.entries(on: "2026-10-05").map(\.mealName) == ["Old"])
        #expect(h.vm.didApply == false)
    }

    @Test("A template that is gone shows its message")
    func gone() async throws {
        let h = try Harness(server: PlanServer(), summaries: [Fixtures.templateSummary(id: "ghost")])
        await h.vm.load()
        await h.vm.apply()
        #expect(h.vm.errorMessage == "That template isn't available anymore.")
    }
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd ios/MealPlannerKit && swift test --filter MealPickerViewModelTests --filter ApplyTemplateViewModelTests`
Expected: FAIL to compile.

- [ ] **Step 3: Implement `MealPickerViewModel.swift`**

```swift
import API
import Foundation
import Observation
import Repositories

/// Choose one of my meals for a slot. The list is all of my meals (cache first, then a refresh), filtered locally
/// as web does, because the API's meal list has no search.
@Observable
@MainActor
public final class MealPickerViewModel {
    public var query = ""
    public private(set) var meals: [Components.Schemas.MealSummary] = []
    public private(set) var isLoading = false
    public private(set) var errorMessage: String?

    @ObservationIgnored private let repository: MealsRepository

    public init(meals: MealsRepository) {
        self.repository = meals
    }

    public var filtered: [Components.Schemas.MealSummary] {
        let text = query.trimmingCharacters(in: .whitespacesAndNewlines)
        return text.isEmpty ? meals : meals.filter { $0.name.localizedCaseInsensitiveContains(text) }
    }

    /// Loaded, no error, and genuinely nothing there (so "create a meal first" is true, not a symptom of being offline).
    public var isLibraryEmpty: Bool { meals.isEmpty && !isLoading && errorMessage == nil }

    public func load() async {
        isLoading = true
        defer { isLoading = false }
        meals = await repository.cachedMeals(.mine)
        do {
            try await repository.refreshMeals(.mine)
            meals = await repository.cachedMeals(.mine)
            errorMessage = nil
        } catch {
            errorMessage = meals.isEmpty ? ErrorText.message(for: error) : nil
        }
    }
}
```

- [ ] **Step 4: Implement `ApplyTemplateViewModel.swift` and `PlanDependencies.swift`**

```swift
import API
import Foundation
import Observation
import Repositories

/// The apply sheet: pick one of my templates and a start date. A `409 plan_conflict` becomes a question
/// (`confirmingReplace`), not an error; confirming repeats the call with `overwrite: true`. Partner templates cannot
/// be applied (`404`), so only mine are listed.
@Observable
@MainActor
public final class ApplyTemplateViewModel {
    public private(set) var templates: [Components.Schemas.DietTemplateSummary] = []
    public var selectedID: String?
    /// `YYYY-MM-DD`; the sheet's date picker writes it.
    public var startDate: String
    public private(set) var isLoading = false
    public private(set) var isApplying = false
    public private(set) var errorMessage: String?
    public var confirmingReplace = false
    public private(set) var didApply = false

    @ObservationIgnored private let repository: TemplatesRepository
    @ObservationIgnored private let plan: PlanViewModel

    public init(templates: TemplatesRepository, plan: PlanViewModel, startDate: String) {
        self.repository = templates
        self.plan = plan
        self.startDate = startDate
    }

    public var selected: Components.Schemas.DietTemplateSummary? { templates.first { $0.id == selectedID } }

    public func load() async {
        isLoading = true
        defer { isLoading = false }
        templates = await repository.cachedTemplates(.mine)
        select()
        do {
            try await repository.refreshTemplates(.mine)
            templates = await repository.cachedTemplates(.mine)
            select()
        } catch {
            if templates.isEmpty { errorMessage = ErrorText.message(for: error) }
        }
    }

    public func apply() async { await run(overwrite: false) }

    public func confirmReplace() async {
        confirmingReplace = false
        await run(overwrite: true)
    }

    private func select() {
        if selected == nil { selectedID = templates.first?.id }
    }

    private func run(overwrite: Bool) async {
        guard let template = selected else {
            errorMessage = "Choose a template."
            return
        }
        isApplying = true
        errorMessage = nil
        defer { isApplying = false }
        switch await plan.apply(templateID: template.id, dayCount: template.dayCount, startDate: startDate, overwrite: overwrite) {
        case .applied: didApply = true
        case .needsConfirmation: confirmingReplace = true
        case .failed(let message): errorMessage = message
        }
    }
}
```

```swift
import Repositories

/// What the Today and Plan tabs need, built once in `RootView` and handed down.
public struct PlanDependencies: Sendable {
    public let plan: PlanRepository
    public let templates: TemplatesRepository
    public let meals: MealsRepository
    public let partner: PartnerRepository

    public init(plan: PlanRepository, templates: TemplatesRepository, meals: MealsRepository, partner: PartnerRepository) {
        self.plan = plan
        self.templates = templates
        self.meals = meals
        self.partner = partner
    }
}
```

- [ ] **Step 5: Run to verify they pass**

Run: `cd ios/MealPlannerKit && swift test --filter MealPickerViewModelTests --filter ApplyTemplateViewModelTests`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add ios/MealPlannerKit/Sources/Features ios/MealPlannerKit/Tests/MealPlannerKitTests/MealPickerViewModelTests.swift ios/MealPlannerKit/Tests/MealPlannerKitTests/ApplyTemplateViewModelTests.swift
git commit -m "feat(ios): add the meal picker and apply-template view models" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

## Task 9: `TemplateDraft` (pure validation and diff)

**Files:**
- Create: `ios/MealPlannerKit/Sources/Features/Plan/Templates/TemplateDraft.swift`
- Test: `ios/MealPlannerKit/Tests/MealPlannerKitTests/TemplateDraftTests.swift`

**Interfaces:**
- Consumes: `parseDecimal`, `plainNumber` (Meals plan Task 1); `Fixtures.template/templateSlot`.
- Produces: `TemplateDraft` (`name`, `shared`, `dayCount`, `rows: [Row]`), `.Row` (`id`, `dayIndex`, `slot`, `mealID`, `mealName`, `portion: String`), `.Valid` (`name`, `shared`, `slots: [TemplateSlotInput]`), `.Errors` (`name`, `rows: [String: String]`), `.Validation` (`.valid`/`.invalid`), `.Changes` (`patch: UpdateDietTemplateRequest?`, `slots: [TemplateSlotInput]?`, `isEmpty`); `init(template:)`, `static saved(from:) -> Valid`, `validate()`, `static changes(from:to:)`, `rows(day:slot:)`, `mutating setMeal(dayIndex:slot:mealID:mealName:)`, `mutating removeRow(id:)`, `static nameError(_:)`, `static slotOrder`.

- [ ] **Step 1: Write the failing tests** — `TemplateDraftTests.swift`

```swift
import API
import Testing
@testable import Features

@Suite
struct TemplateDraftTests {
    private func template(slots: [Components.Schemas.TemplateSlot] = []) -> Components.Schemas.DietTemplate {
        Fixtures.template(name: "Cut", dayCount: 3, slots: slots)
    }

    private func valid(_ d: TemplateDraft) -> TemplateDraft.Valid? {
        if case .valid(let v) = d.validate() { v } else { nil }
    }

    private func errors(_ d: TemplateDraft) -> TemplateDraft.Errors? {
        if case .invalid(let e) = d.validate() { e } else { nil }
    }

    @Test("A draft built from a template validates to what the server holds")
    func fromTemplate() {
        let t = template(slots: [Fixtures.templateSlot(dayIndex: 1, slot: .lunch, portion: 1.5)])
        let d = TemplateDraft(template: t)
        #expect(d.name == "Cut")
        #expect(d.dayCount == 3)
        #expect(d.rows.map(\.portion) == ["1.5"])
        #expect(valid(d) == TemplateDraft.saved(from: t))
    }

    @Test("Slots are canonical: by day, then breakfast, lunch, dinner, snack, then insertion order, whatever the server sent")
    func canonicalOrder() {
        let shuffled = template(slots: [
            Fixtures.templateSlot(id: "s2", dayIndex: 1, slot: .snack, mealID: "b"),
            Fixtures.templateSlot(id: "d", dayIndex: 0, slot: .dinner),
            Fixtures.templateSlot(id: "s1", dayIndex: 1, slot: .snack, mealID: "a"),
            Fixtures.templateSlot(id: "bk", dayIndex: 0, slot: .breakfast),
            Fixtures.templateSlot(id: "l1", dayIndex: 1, slot: .lunch),
        ])
        let order = TemplateDraft.saved(from: shuffled).slots.map { "\($0.dayIndex)\($0.slot.rawValue.prefix(1))\($0.mealId)" }
        #expect(order == ["0bm1", "0dm1", "1lm1", "1sb", "1sa"])
        #expect(valid(TemplateDraft(template: shuffled)) == TemplateDraft.saved(from: shuffled))
    }

    @Test("Name is trimmed and limited to 1–200 characters")
    func nameLimits() {
        var d = TemplateDraft(template: template())
        d.name = "  "
        #expect(errors(d)?.name == "Give the template a name.")
        d.name = String(repeating: "a", count: 201)
        #expect(errors(d)?.name == "Use at most 200 characters.")
        d.name = String(repeating: "a", count: 200)
        #expect(valid(d) != nil)
        d.name = "  Bulk  "
        #expect(valid(d)?.name == "Bulk")
    }

    @Test(arguments: ["1", "0.01", "100", "1,5", "2."])
    func validPortions(text: String) {
        var d = TemplateDraft(template: template())
        d.setMeal(dayIndex: 0, slot: .breakfast, mealID: "m1", mealName: "Oats")
        d.rows[0].portion = text
        #expect(valid(d) != nil)
    }

    @Test("Out-of-range portions get the range message", arguments: ["0", "100.5", "101"])
    func portionRange(text: String) {
        var d = TemplateDraft(template: template())
        d.setMeal(dayIndex: 0, slot: .breakfast, mealID: "m1", mealName: "Oats")
        d.rows[0].portion = text
        #expect(errors(d)?.rows[d.rows[0].id] == "The portion must be more than 0 and at most 100.")
    }

    @Test("Blank and non-numeric portions ask for a portion", arguments: ["", "  ", "abc", "-1", "1e2"])
    func portionInput(text: String) {
        var d = TemplateDraft(template: template())
        d.setMeal(dayIndex: 0, slot: .breakfast, mealID: "m1", mealName: "Oats")
        d.rows[0].portion = text
        #expect(errors(d)?.rows[d.rows[0].id] == "Enter a portion.")
    }

    @Test("A slot outside the template's days is invalid")
    func dayOutsideTemplate() {
        var d = TemplateDraft(template: template())
        d.setMeal(dayIndex: 3, slot: .lunch, mealID: "m1", mealName: "Oats")
        #expect(errors(d)?.rows[d.rows[0].id] == "That day is outside the template.")
    }

    @Test("Choosing a meal for an occupied non-snack slot replaces it and keeps the portion; a snack is added")
    func setMeal() {
        var d = TemplateDraft(template: template(slots: [Fixtures.templateSlot(id: "x", dayIndex: 0, slot: .breakfast, mealID: "m1", mealName: "Oats", portion: 2)]))
        d.setMeal(dayIndex: 0, slot: .breakfast, mealID: "m2", mealName: "Rice")
        #expect(d.rows.count == 1)
        #expect(d.rows[0].id == "x")
        #expect(d.rows[0].mealName == "Rice")
        #expect(d.rows[0].portion == "2")
        d.setMeal(dayIndex: 0, slot: .snack, mealID: "m1", mealName: "Oats")
        d.setMeal(dayIndex: 0, slot: .snack, mealID: "m2", mealName: "Rice")
        #expect(d.rows(day: 0, slot: .snack).count == 2)
        #expect(d.rows(day: 0, slot: .snack).allSatisfy { $0.portion == "1" })
        #expect(Set(d.rows.map(\.id)).count == 3)
    }

    @Test("removeRow removes just that slot")
    func removeRow() {
        var d = TemplateDraft(template: template(slots: [Fixtures.templateSlot(id: "a"), Fixtures.templateSlot(id: "b", dayIndex: 1)]))
        d.removeRow(id: "a")
        #expect(d.rows.map(\.id) == ["b"])
    }

    @Test("No change gives no writes")
    func noChange() throws {
        let saved = try #require(valid(TemplateDraft(template: template(slots: [Fixtures.templateSlot()]))))
        #expect(TemplateDraft.changes(from: saved, to: saved).isEmpty)
    }

    @Test("A renamed or shared template patches only those fields and sends no slots")
    func patchOnly() throws {
        let saved = try #require(valid(TemplateDraft(template: template(slots: [Fixtures.templateSlot()]))))
        var next = saved
        next.name = "Bulk"
        let renamed = TemplateDraft.changes(from: saved, to: next)
        #expect(renamed.patch == Components.Schemas.UpdateDietTemplateRequest(name: "Bulk"))
        #expect(renamed.slots == nil)
        next = saved
        next.shared = true
        #expect(TemplateDraft.changes(from: saved, to: next).patch == .init(sharedWithPartner: true))
    }

    @Test("A changed slot sends the whole slot list")
    func slotsChange() throws {
        let saved = try #require(valid(TemplateDraft(template: template(slots: [Fixtures.templateSlot()]))))
        var d = TemplateDraft(template: template(slots: [Fixtures.templateSlot()]))
        d.rows[0].portion = "2"
        let next = try #require(valid(d))
        let changes = TemplateDraft.changes(from: saved, to: next)
        #expect(changes.patch == nil)
        #expect(changes.slots == next.slots)
    }

    @Test("Removing the last slot sends an empty list rather than skipping the write")
    func removingLastSlot() throws {
        let saved = try #require(valid(TemplateDraft(template: template(slots: [Fixtures.templateSlot(id: "a")]))))
        var d = TemplateDraft(template: template(slots: [Fixtures.templateSlot(id: "a")]))
        d.removeRow(id: "a")
        let next = try #require(valid(d))
        #expect(TemplateDraft.changes(from: saved, to: next).slots == [])
    }

    @Test("Create-form helper shares the editor's name messages")
    func nameError() {
        #expect(TemplateDraft.nameError("") == "Give the template a name.")
        #expect(TemplateDraft.nameError("Soup") == nil)
    }
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd ios/MealPlannerKit && swift test --filter TemplateDraftTests`
Expected: FAIL to compile (`TemplateDraft` does not exist).

- [ ] **Step 3: Implement `TemplateDraft.swift`**

```swift
import API
import Foundation

/// The editable text of a diet template, with validation and the diff against what the server holds (the template
/// counterpart of `MealDraft`). Slots are kept and saved in a canonical order (day, then breakfast, lunch, dinner,
/// snack, then insertion order) on both sides of the diff, so the server's ordering never reads as an edit.
public struct TemplateDraft: Equatable, Sendable {
    public typealias Slot = Components.Schemas.Slot

    public static let slotOrder: [Slot] = [.breakfast, .lunch, .dinner, .snack]

    public struct Row: Equatable, Identifiable, Sendable {
        public var id: String
        public var dayIndex: Int
        public var slot: Slot
        public var mealID: String
        public var mealName: String
        public var portion: String
    }

    public struct Valid: Equatable, Sendable {
        public var name: String
        public var shared: Bool
        public var slots: [Components.Schemas.TemplateSlotInput]
    }

    public struct Errors: Equatable, Sendable {
        public var name: String?
        public var rows: [String: String] = [:]
        var isEmpty: Bool { name == nil && rows.isEmpty }
    }

    public enum Validation: Equatable, Sendable {
        case valid(Valid)
        case invalid(Errors)
    }

    public struct Changes: Equatable, Sendable {
        public var patch: Components.Schemas.UpdateDietTemplateRequest?
        public var slots: [Components.Schemas.TemplateSlotInput]?
        public var isEmpty: Bool { patch == nil && slots == nil }
    }

    public var name = ""
    public var shared = false
    public var dayCount = 1
    public var rows: [Row] = []

    public init(name: String = "", shared: Bool = false, dayCount: Int = 1, rows: [Row] = []) {
        self.name = name
        self.shared = shared
        self.dayCount = dayCount
        self.rows = rows
    }

    public init(template: Components.Schemas.DietTemplate) {
        let rows = Self.canonical(template.slots, day: \.dayIndex, slot: \.slot).map {
            Row(id: $0.id, dayIndex: $0.dayIndex, slot: $0.slot, mealID: $0.mealId, mealName: $0.mealName, portion: plainNumber($0.portion))
        }
        self.init(name: template.name, shared: template.sharedWithPartner, dayCount: template.dayCount, rows: rows)
    }

    /// What the server holds, in the shape validation produces, so the two can be compared.
    public static func saved(from template: Components.Schemas.DietTemplate) -> Valid {
        Valid(
            name: template.name,
            shared: template.sharedWithPartner,
            slots: canonical(template.slots, day: \.dayIndex, slot: \.slot).map {
                .init(dayIndex: $0.dayIndex, slot: $0.slot, mealId: $0.mealId, portion: $0.portion)
            }
        )
    }

    public func rows(day: Int, slot: Slot) -> [Row] {
        rows.filter { $0.dayIndex == day && $0.slot == slot }
    }

    /// Choosing a meal for an occupied breakfast, lunch or dinner replaces it (keeping its portion); a snack is added.
    public mutating func setMeal(dayIndex: Int, slot: Slot, mealID: String, mealName: String) {
        if slot != .snack, let index = rows.firstIndex(where: { $0.dayIndex == dayIndex && $0.slot == slot }) {
            rows[index].mealID = mealID
            rows[index].mealName = mealName
        } else {
            rows.append(Row(id: UUID().uuidString, dayIndex: dayIndex, slot: slot, mealID: mealID, mealName: mealName, portion: "1"))
        }
    }

    public mutating func removeRow(id: String) {
        rows.removeAll { $0.id == id }
    }

    public static func nameError(_ raw: String) -> String? {
        let name = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        if name.isEmpty { return "Give the template a name." }
        if name.count > 200 { return "Use at most 200 characters." }
        return nil
    }

    public func validate() -> Validation {
        var errors = Errors()
        errors.name = Self.nameError(name)
        var portions: [String: Double] = [:]
        for row in rows {
            if row.dayIndex < 0 || row.dayIndex >= dayCount {
                errors.rows[row.id] = "That day is outside the template."
                continue
            }
            switch parseDecimal(row.portion) {
            case .value(let portion) where portion > 0 && portion <= 100:
                portions[row.id] = portion
            case .value:
                errors.rows[row.id] = "The portion must be more than 0 and at most 100."
            case .blank, .invalid:
                errors.rows[row.id] = "Enter a portion."
            }
        }
        guard errors.isEmpty else { return .invalid(errors) }
        return .valid(Valid(
            name: name.trimmingCharacters(in: .whitespacesAndNewlines),
            shared: shared,
            slots: Self.canonical(rows, day: \.dayIndex, slot: \.slot).map {
                .init(dayIndex: $0.dayIndex, slot: $0.slot, mealId: $0.mealID, portion: portions[$0.id])
            }
        ))
    }

    /// A patch of changed name and sharing, and the whole slot list when any slot changed (an empty list clears
    /// every slot, so removing the last slot still writes).
    public static func changes(from saved: Valid, to next: Valid) -> Changes {
        var patch = Components.Schemas.UpdateDietTemplateRequest()
        var changed = false
        if next.name != saved.name { patch.name = next.name; changed = true }
        if next.shared != saved.shared { patch.sharedWithPartner = next.shared; changed = true }
        return Changes(patch: changed ? patch : nil, slots: next.slots == saved.slots ? nil : next.slots)
    }

    /// By day, then breakfast, lunch, dinner, snack, then the order the items were in.
    private static func canonical<T>(_ items: [T], day: KeyPath<T, Int>, slot: KeyPath<T, Slot>) -> [T] {
        func rank(_ slot: Slot) -> Int { slotOrder.firstIndex(of: slot) ?? slotOrder.count }
        return items.enumerated().sorted { left, right in
            let (l, r) = (left.element, right.element)
            if l[keyPath: day] != r[keyPath: day] { return l[keyPath: day] < r[keyPath: day] }
            if rank(l[keyPath: slot]) != rank(r[keyPath: slot]) { return rank(l[keyPath: slot]) < rank(r[keyPath: slot]) }
            return left.offset < right.offset
        }.map(\.element)
    }
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd ios/MealPlannerKit && swift test --filter TemplateDraftTests`
Expected: PASS. Note `canonicalOrder` pins that two snacks on the same day keep their insertion order and that the canonical order puts day 0 breakfast and dinner before day 1.

- [ ] **Step 5: Commit**

```bash
git add ios/MealPlannerKit/Sources/Features/Plan/Templates/TemplateDraft.swift ios/MealPlannerKit/Tests/MealPlannerKitTests/TemplateDraftTests.swift
git commit -m "feat(ios): add TemplateDraft validation and diff" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

## Task 10: `TemplatesViewModel` (the template library)

**Files:**
- Create: `ios/MealPlannerKit/Sources/Features/Plan/Templates/TemplatesViewModel.swift`
- Test: `ios/MealPlannerKit/Tests/MealPlannerKitTests/TemplatesViewModelTests.swift`

**Interfaces:**
- Consumes: `TemplatesRepository`, `PartnerRepository`, `TemplatesError`, `MealScope` (Tasks 4, 6 and Meals); `TemplateDraft.nameError` (Task 9); `ErrorText`.
- Produces: `TemplatePresentation` (`.new`, `.edit(String)`, `.view(String)`; `Identifiable`; `templateID`); `TemplatesViewModel(templates:partner:)`: `scope`, `templates`, `isLoading`, `isStale`, `loadError`, `showsPartnerSegment`, `presentation`, `pendingDelete`, `alertMessage` (the last three settable); `appear()`, `load()`, `select(_:)`, `createTemplate(name:dayCount:) async -> String?`, `delete(id:) async -> String?`, `confirmDelete(_:) async`, `copyToLibrary(id:) async -> String?`, `static deleteMessage(name:)`.

- [ ] **Step 1: Write the failing tests** — `TemplatesViewModelTests.swift`

```swift
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
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd ios/MealPlannerKit && swift test --filter TemplatesViewModelTests`
Expected: FAIL to compile.

- [ ] **Step 3: Implement `TemplatesViewModel.swift`**

```swift
import API
import Foundation
import Observation
import Repositories

/// What the single template sheet shows. A Mine row opens `.edit`, a partner row `.view`; once the template loads,
/// its `isOwner` decides which UI appears.
public enum TemplatePresentation: Identifiable, Equatable, Sendable {
    case new
    case edit(String)
    case view(String)

    public var id: String {
        switch self {
        case .new: "new"
        case .edit(let id): "edit-\(id)"
        case .view(let id): "view-\(id)"
        }
    }

    var templateID: String? {
        switch self {
        case .new: nil
        case .edit(let id), .view(let id): id
        }
    }
}

/// The template library, with the Mine / Partner's rule of the Meals tab: the segment shows only while
/// `GET /partner` says `active`.
@Observable
@MainActor
public final class TemplatesViewModel {
    public private(set) var scope: MealScope = .mine
    public private(set) var templates: [Components.Schemas.DietTemplateSummary] = []
    public private(set) var isLoading = false
    public private(set) var isStale = false
    public private(set) var loadError: String?
    public private(set) var showsPartnerSegment = false
    public var presentation: TemplatePresentation?
    public var pendingDelete: Components.Schemas.DietTemplateSummary?
    public var alertMessage: String?

    @ObservationIgnored private let templatesRepository: TemplatesRepository
    @ObservationIgnored private let partnerRepository: PartnerRepository

    public init(templates: TemplatesRepository, partner: PartnerRepository) {
        self.templatesRepository = templates
        self.partnerRepository = partner
    }

    public static func deleteMessage(name: String) -> String {
        "\"\(name)\" will be removed from your library. This can't be undone."
    }

    /// The screen appears or the scene becomes active: re-read the partnership, then the list.
    public func appear() async {
        await refreshPartnerSegment()
        await load()
    }

    /// Renders the cache at once, then awaits the refresh and re-reads. A failed refresh keeps what is shown and marks
    /// it stale; with nothing cached it shows an error state.
    public func load() async {
        let requested = scope
        isLoading = true
        defer { isLoading = false }
        templates = await templatesRepository.cachedTemplates(requested)
        do {
            try await templatesRepository.refreshTemplates(requested)
            guard scope == requested else { return }
            templates = await templatesRepository.cachedTemplates(requested)
            isStale = false
            loadError = nil
        } catch TemplatesError.partnerNotLinked {
            showsPartnerSegment = false
            if scope == requested {
                scope = .mine
                await load()
            }
        } catch {
            guard scope == requested else { return }
            isStale = true
            loadError = templates.isEmpty ? ErrorText.message(for: error) : nil
        }
    }

    public func select(_ next: MealScope) async {
        guard next != scope else { return }
        scope = next
        await load()
    }

    /// Validates locally (the API would reject the same things), creates, and turns the sheet into the editor.
    /// Returns the error text, or `nil` on success.
    public func createTemplate(name: String, dayCount: String) async -> String? {
        if let message = TemplateDraft.nameError(name) { return message }
        guard let days = Int(dayCount.trimmingCharacters(in: .whitespacesAndNewlines)), (1...31).contains(days) else {
            return "Day count must be between 1 and 31."
        }
        do {
            let template = try await templatesRepository.create(name: name.trimmingCharacters(in: .whitespacesAndNewlines), dayCount: days)
            presentation = .edit(template.id)
            if scope == .mine { templates = await templatesRepository.cachedTemplates(.mine) }
            return nil
        } catch {
            return ErrorText.message(for: error)
        }
    }

    /// The one delete path, shared by the editor and the list swipe. Returns the error text, or `nil` on success (a
    /// template already gone counts as deleted). Closes the sheet if it was showing this template.
    public func delete(id: String) async -> String? {
        do {
            try await templatesRepository.delete(id: id)
        } catch TemplatesError.notFound {
            // Already gone elsewhere: the goal is met.
        } catch {
            return ErrorText.message(for: error)
        }
        if presentation?.templateID == id { presentation = nil }
        templates = await templatesRepository.cachedTemplates(scope)
        return nil
    }

    /// The list-swipe confirmation. The dialog's button hands over the template itself: by the time its task runs,
    /// SwiftUI has already cleared `pendingDelete`.
    public func confirmDelete(_ template: Components.Schemas.DietTemplateSummary) async {
        pendingDelete = nil
        if let message = await delete(id: template.id) { alertMessage = message }
    }

    /// Copies a template (mine or the partner's) into my library and opens the copy. Returns the error text or `nil`.
    public func copyToLibrary(id: String) async -> String? {
        do {
            let copy = try await templatesRepository.copy(id: id)
            presentation = .edit(copy.id)
            try? await templatesRepository.refreshTemplates(.mine)
            if scope == .mine { templates = await templatesRepository.cachedTemplates(.mine) }
            return nil
        } catch {
            return ErrorText.message(for: error)
        }
    }

    private func refreshPartnerSegment() async {
        do {
            let partnership = try await partnerRepository.status()
            showsPartnerSegment = partnership?.status == .active
            if !showsPartnerSegment { await templatesRepository.clearPartnerTemplates() }
        } catch {
            showsPartnerSegment = !(await templatesRepository.cachedTemplates(.partner)).isEmpty
        }
        if !showsPartnerSegment, scope == .partner { scope = .mine }
    }
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd ios/MealPlannerKit && swift test --filter TemplatesViewModelTests`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add ios/MealPlannerKit/Sources/Features/Plan/Templates/TemplatesViewModel.swift ios/MealPlannerKit/Tests/MealPlannerKitTests/TemplatesViewModelTests.swift
git commit -m "feat(ios): add TemplatesViewModel with the partner segment rule" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

## Task 11: `TemplateEditorViewModel` (loading, draft, autosave on the shared engine)

**Files:**
- Create: `ios/MealPlannerKit/Sources/Features/Plan/Templates/TemplateEditorViewModel.swift`
- Test: `ios/MealPlannerKit/Tests/MealPlannerKitTests/TemplateEditorViewModelTests.swift`

**Interfaces:**
- Consumes: `TemplatesRepository`, `TemplateDraft` and `.Valid`/`.Changes` (Task 9), `Autosaver` (Task 1), `ErrorText`; test helpers `TestSleeper`, `Gate`, `Locked`, `waitUntil`, `settle`.
- Produces: `TemplateEditorViewModel(templateID:repository:partnerLinked:sleep:)` with `phase` (`.loading`, `.ready`, `.unavailable(String)`, `.failed(String)`), `template`, `draft` (settable; every set schedules autosave), `isOwner`, `offersSharing`, `isSaving`, `saveError`, `statusText`, `bannerMessage`, `hasUnsaved`, `validationErrors: TemplateDraft.Errors?`; `load() async`, `setMeal(dayIndex:slot:meal:)`, `removeRow(id:)`, `retry() async`, `flushOnLeave()`.

- [ ] **Step 1: Write the failing tests** — `TemplateEditorViewModelTests.swift`

```swift
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
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd ios/MealPlannerKit && swift test --filter TemplateEditorViewModelTests`
Expected: FAIL to compile.

- [ ] **Step 3: Implement `TemplateEditorViewModel.swift`**

```swift
import API
import Foundation
import Observation
import Repositories

/// Loads one template and, for the owner, edits it with autosave on the shared `Autosaver` (the template counterpart
/// of `MealEditorViewModel`): name and sharing (`PATCH`) then the whole slot list (`PUT`), each step committed on
/// its own. A partner's template loads read-only.
@Observable
@MainActor
public final class TemplateEditorViewModel {
    public enum Phase: Equatable, Sendable {
        case loading
        case ready
        case unavailable(String)
        case failed(String)
    }

    public let templateID: String
    public let partnerLinked: Bool
    public private(set) var phase: Phase = .loading
    /// The server's latest answer; the draft is copied from it only once.
    public private(set) var template: Components.Schemas.DietTemplate?

    private var storedDraft = TemplateDraft()
    /// Set through here so every edit schedules autosave.
    public var draft: TemplateDraft {
        get { storedDraft }
        set {
            storedDraft = newValue
            autosaver.schedule()
        }
    }

    /// What the server holds, in the shape validation produces. `nil` until an owned template has loaded.
    private var saved: TemplateDraft.Valid?

    @ObservationIgnored private var adoptedDraft: TemplateDraft?
    @ObservationIgnored private let repository: TemplatesRepository
    @ObservationIgnored private let sleep: @Sendable (Duration) async throws -> Void
    @ObservationIgnored private lazy var autosaver = Autosaver<TemplateDraft.Valid>(
        sleep: sleep,
        pending: { [weak self] in self?.pendingValue },
        perform: { [weak self] value in try await self?.save(value) }
    )

    public init(
        templateID: String,
        repository: TemplatesRepository,
        partnerLinked: Bool,
        sleep: @escaping @Sendable (Duration) async throws -> Void = { try await Task.sleep(for: $0) }
    ) {
        self.templateID = templateID
        self.repository = repository
        self.partnerLinked = partnerLinked
        self.sleep = sleep
    }

    // MARK: Derived state

    public var isOwner: Bool { template?.isOwner ?? false }
    public var offersSharing: Bool { partnerLinked || draft.shared }
    public var isSaving: Bool { autosaver.isSaving }
    public var saveError: String? { autosaver.saveError }

    public var validationErrors: TemplateDraft.Errors? {
        if case .invalid(let errors) = draft.validate() { errors } else { nil }
    }

    private var pendingValue: TemplateDraft.Valid? {
        guard let saved, case .valid(let value) = draft.validate(), value != saved else { return nil }
        return value
    }

    public var hasUnsaved: Bool { saved != nil && (validationErrors != nil || pendingValue != nil) }

    public var statusText: String {
        if isSaving { return "Saving…" }
        if validationErrors != nil { return "Fix the highlighted fields to save." }
        if pendingValue != nil { return "Unsaved changes" }
        return "All changes saved"
    }

    /// The failure banner shows only while something is still unsaved.
    public var bannerMessage: String? { hasUnsaved ? saveError : nil }

    // MARK: Loading

    public func load() async {
        if phase == .loading, let cached = await repository.cachedTemplate(id: templateID) { adopt(cached) }
        do {
            let fresh = try await repository.refreshTemplate(id: templateID)
            // While nothing was typed, take the server's copy; once the person has typed, a refresh only updates
            // what the screen shows from the server (never the draft).
            if phase != .ready || draft == adoptedDraft { adopt(fresh) } else { template = fresh }
        } catch TemplatesError.notFound {
            phase = .unavailable(ErrorText.message(for: TemplatesError.notFound))
        } catch {
            if phase != .ready { phase = .failed(ErrorText.message(for: error)) }
        }
    }

    private func adopt(_ loaded: Components.Schemas.DietTemplate) {
        template = loaded
        phase = .ready
        guard loaded.isOwner else { return }
        let adopted = TemplateDraft(template: loaded)
        storedDraft = adopted
        adoptedDraft = adopted
        saved = TemplateDraft.saved(from: loaded)
        autosaver.forget()
    }

    // MARK: Editing

    public func setMeal(dayIndex: Int, slot: Components.Schemas.Slot, meal: Components.Schemas.MealSummary) {
        var next = draft
        next.setMeal(dayIndex: dayIndex, slot: slot, mealID: meal.id, mealName: meal.name)
        draft = next
    }

    public func removeRow(id: String) {
        var next = draft
        next.removeRow(id: id)
        draft = next
    }

    // MARK: Autosave

    /// The two writes, each committed on its own: a failure in the second leaves the first counted as saved.
    private func save(_ value: TemplateDraft.Valid) async throws {
        guard var current = saved else { return }
        let changes = TemplateDraft.changes(from: current, to: value)
        if let patch = changes.patch {
            template = try await repository.update(id: templateID, patch)
            current = TemplateDraft.Valid(name: value.name, shared: value.shared, slots: current.slots)
            saved = current
        }
        if let slots = changes.slots {
            template = try await repository.replaceSlots(id: templateID, slots)
            current.slots = value.slots
            saved = current
        }
    }

    /// "Try again": forget that the value was tried, and save now.
    public func retry() async {
        await autosaver.retry()
    }

    /// Leaving the editor saves a pending edit in an unstructured task, so dismissing the view does not cancel it.
    public func flushOnLeave() {
        autosaver.flush()
    }
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd ios/MealPlannerKit && for i in 1 2 3; do swift test --filter TemplateEditorViewModelTests 2>&1 | grep -E "✘|Test run with"; done`
Expected: PASS, three times in a row.

- [ ] **Step 5: Commit**

```bash
git add ios/MealPlannerKit/Sources/Features/Plan/Templates/TemplateEditorViewModel.swift ios/MealPlannerKit/Tests/MealPlannerKitTests/TemplateEditorViewModelTests.swift
git commit -m "feat(ios): add TemplateEditorViewModel with autosave" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

## Task 12: Meal picker and template screens (list, new, sheet, editor, read-only)

**Files:**
- Create: `ios/MealPlannerKit/Sources/Features/Shared/MealPicker/MealPickerView.swift`
- Create: `ios/MealPlannerKit/Sources/Features/Plan/Templates/TemplatesView.swift`, `NewTemplateForm.swift`, `TemplateSheetView.swift`, `TemplateEditorView.swift`, `TemplateReadOnlyView.swift`

**Interfaces:**
- Consumes: `MealPickerViewModel` (Task 8), `TemplatesViewModel`, `TemplatePresentation` (Task 10), `TemplateEditorViewModel` (Task 11), `TemplateDraft` (Task 9), `PlanDependencies` (Task 8), `decimalKeyboard()`, `inlineNavigationTitle()` (Meals plan Task 9), `plainNumber`.
- Produces: `MealPickerView(meals:title:onPick:)` (a modal sheet; calls `onPick` with the chosen meal and dismisses); `TemplatesView(dependencies:)` (public; pushed from Plan's toolbar). Accessibility identifiers used later: `mealPickerSearchField`, `mealPickerRow-<name>`, `newTemplateButton`, `templateRow-<name>`, `newTemplateNameField`, `newTemplateDaysField`, `newTemplateCreateButton`, `templateNameField`, `templateSaveStatus`, `templateSheetDoneButton`, `deleteTemplateButton`, `copyTemplateButton`.

No unit tests: the logic is in Tasks 8-11. Proof is the build here, and the XCUITest in Task 14 for the picker.

- [ ] **Step 1: Create `MealPickerView.swift`**

```swift
import API
import Repositories
import SwiftUI

/// Choose one of my meals for a slot: a modal sheet with a search field (filtered locally) and a list.
struct MealPickerView: View {
    @State private var viewModel: MealPickerViewModel
    private let title: String
    private let onPick: (Components.Schemas.MealSummary) -> Void
    @Environment(\.dismiss) private var dismiss

    init(meals: MealsRepository, title: String, onPick: @escaping (Components.Schemas.MealSummary) -> Void) {
        self.title = title
        self.onPick = onPick
        _viewModel = State(initialValue: MealPickerViewModel(meals: meals))
    }

    var body: some View {
        @Bindable var viewModel = viewModel
        NavigationStack {
            List {
                Section {
                    TextField("Search my meals", text: $viewModel.query)
                        .accessibilityIdentifier("mealPickerSearchField")
                }
                Section {
                    if viewModel.isLoading && viewModel.meals.isEmpty {
                        ProgressView()
                    }
                    if let message = viewModel.errorMessage {
                        Text(message).foregroundStyle(.red)
                    } else if viewModel.isLibraryEmpty {
                        Text("You have no meals yet. Create one in the Meals tab.").foregroundStyle(.secondary)
                    } else if viewModel.filtered.isEmpty && !viewModel.meals.isEmpty {
                        Text("No meal matches.").foregroundStyle(.secondary)
                    }
                    ForEach(viewModel.filtered, id: \.id) { meal in
                        Button {
                            onPick(meal)
                            dismiss()
                        } label: {
                            HStack {
                                Text(meal.name)
                                Spacer()
                                Text("\(plainNumber(meal.servings)) serving\(meal.servings == 1 ? "" : "s")")
                                    .font(.footnote)
                                    .foregroundStyle(.secondary)
                            }
                        }
                        .foregroundStyle(.primary)
                        .accessibilityIdentifier("mealPickerRow-\(meal.name)")
                    }
                }
            }
            .navigationTitle(title)
            .inlineNavigationTitle()
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
            }
        }
        .task { await viewModel.load() }
    }
}
```

- [ ] **Step 2: Create `TemplatesView.swift`**

```swift
import API
import Repositories
import SwiftUI

/// The diet-template library, pushed from Plan's toolbar. Mine / Partner's under the Meals rule.
public struct TemplatesView: View {
    @State private var viewModel: TemplatesViewModel
    private let dependencies: PlanDependencies
    @Environment(\.scenePhase) private var scenePhase

    public init(dependencies: PlanDependencies) {
        self.dependencies = dependencies
        _viewModel = State(initialValue: TemplatesViewModel(templates: dependencies.templates, partner: dependencies.partner))
    }

    public var body: some View {
        @Bindable var viewModel = viewModel
        List {
            if viewModel.showsPartnerSegment {
                Picker(
                    "Library",
                    selection: Binding(get: { viewModel.scope }, set: { next in Task { await viewModel.select(next) } })
                ) {
                    Text("Mine").tag(MealScope.mine)
                    Text("Partner's").tag(MealScope.partner)
                }
                .pickerStyle(.segmented)
                .listRowBackground(Color.clear)
                .accessibilityIdentifier("templatesScopePicker")
            }
            if viewModel.isStale {
                Label("Offline: showing saved templates", systemImage: "wifi.slash")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            content
        }
        .navigationTitle("Diet templates")
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Button { viewModel.presentation = .new } label: { Label("New template", systemImage: "plus") }
                    .accessibilityIdentifier("newTemplateButton")
            }
        }
        .refreshable { await viewModel.appear() }
        .task { await viewModel.appear() }
        .onChange(of: scenePhase) { _, phase in
            if phase == .active { Task { await viewModel.appear() } }
        }
        .sheet(item: $viewModel.presentation, onDismiss: { Task { await viewModel.appear() } }) { presentation in
            sheet(for: presentation).id(presentation.id)
        }
        .confirmationDialog(
            "Delete this template?",
            isPresented: Binding(get: { viewModel.pendingDelete != nil }, set: { if !$0 { viewModel.pendingDelete = nil } }),
            titleVisibility: .visible,
            presenting: viewModel.pendingDelete
        ) { template in
            Button("Delete template", role: .destructive) { Task { await viewModel.confirmDelete(template) } }
        } message: { template in
            Text(TemplatesViewModel.deleteMessage(name: template.name))
        }
        .alert(
            "Couldn't complete that",
            isPresented: Binding(get: { viewModel.alertMessage != nil }, set: { if !$0 { viewModel.alertMessage = nil } })
        ) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(viewModel.alertMessage ?? "")
        }
    }

    @ViewBuilder
    private var content: some View {
        if let error = viewModel.loadError {
            VStack(alignment: .leading, spacing: 8) {
                Text(error)
                Button("Retry") { Task { await viewModel.load() } }
            }
        } else if viewModel.templates.isEmpty && !viewModel.isLoading {
            emptyState
        } else {
            ForEach(viewModel.templates, id: \.id) { template in row(template) }
        }
    }

    @ViewBuilder
    private var emptyState: some View {
        if viewModel.scope == .mine {
            VStack(alignment: .leading, spacing: 8) {
                Text("No templates yet.").font(.headline)
                Button("Create your first template") { viewModel.presentation = .new }
            }
        } else {
            Text("Templates your partner shares with you show up here, and you can copy them into your library.")
                .foregroundStyle(.secondary)
        }
    }

    private func row(_ template: Components.Schemas.DietTemplateSummary) -> some View {
        Button {
            viewModel.presentation = viewModel.scope == .mine ? .edit(template.id) : .view(template.id)
        } label: {
            HStack {
                VStack(alignment: .leading, spacing: 2) {
                    Text(template.name)
                    Text("\(template.dayCount) day\(template.dayCount == 1 ? "" : "s")")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                Spacer()
                if viewModel.scope == .mine && template.sharedWithPartner {
                    Text("Shared")
                        .font(.caption2.weight(.semibold))
                        .padding(.horizontal, 6)
                        .padding(.vertical, 2)
                        .background(.quaternary, in: Capsule())
                }
            }
        }
        .foregroundStyle(.primary)
        .accessibilityIdentifier("templateRow-\(template.name)")
        .swipeActions(edge: .trailing) {
            if viewModel.scope == .mine {
                Button("Delete", role: .destructive) { viewModel.pendingDelete = template }
            } else {
                Button("Copy to my library") {
                    Task { viewModel.alertMessage = await viewModel.copyToLibrary(id: template.id) }
                }
                .tint(.accentColor)
            }
        }
    }

    @ViewBuilder
    private func sheet(for presentation: TemplatePresentation) -> some View {
        switch presentation {
        case .new:
            NavigationStack { NewTemplateForm(viewModel: viewModel) }
        case .edit(let id), .view(let id):
            TemplateSheetView(templateID: id, dependencies: dependencies, partnerLinked: viewModel.showsPartnerSegment, templatesViewModel: viewModel)
        }
    }
}
```

- [ ] **Step 3: Create `NewTemplateForm.swift`**

```swift
import SwiftUI

/// Name and day count, both required by `createDietTemplate`. On success the same sheet becomes the editor
/// (`TemplatesViewModel.createTemplate` sets `presentation = .edit(id)`). The day count cannot change afterwards.
struct NewTemplateForm: View {
    let viewModel: TemplatesViewModel
    @State private var name = ""
    @State private var dayCount = "7"
    @State private var isCreating = false
    @State private var error: String?
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        Form {
            Section {
                TextField("Name", text: $name).accessibilityIdentifier("newTemplateNameField")
                TextField("Number of days (1 to 31)", text: $dayCount)
                    .decimalKeyboard()
                    .accessibilityIdentifier("newTemplateDaysField")
            } footer: {
                Text("The number of days can't be changed later.")
            }
            if let error {
                Section { Text(error).foregroundStyle(.red).accessibilityIdentifier("newTemplateError") }
            }
            Section {
                Button("Create template") {
                    Task {
                        isCreating = true
                        error = await viewModel.createTemplate(name: name, dayCount: dayCount)
                        isCreating = false
                    }
                }
                .disabled(isCreating)
                .accessibilityIdentifier("newTemplateCreateButton")
            }
        }
        .navigationTitle("New template")
        .inlineNavigationTitle()
        .toolbar {
            ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
        }
    }
}
```

- [ ] **Step 4: Create `TemplateSheetView.swift`**

```swift
import SwiftUI

/// Loads the template and shows the editor or the read-only view: the loaded template's `isOwner` decides, so an
/// editor never appears for a template the caller does not own.
struct TemplateSheetView: View {
    @State private var editor: TemplateEditorViewModel
    private let dependencies: PlanDependencies
    private let templatesViewModel: TemplatesViewModel
    @Environment(\.dismiss) private var dismiss

    init(templateID: String, dependencies: PlanDependencies, partnerLinked: Bool, templatesViewModel: TemplatesViewModel) {
        self.dependencies = dependencies
        self.templatesViewModel = templatesViewModel
        _editor = State(initialValue: TemplateEditorViewModel(templateID: templateID, repository: dependencies.templates, partnerLinked: partnerLinked))
    }

    var body: some View {
        NavigationStack {
            Group {
                switch editor.phase {
                case .loading:
                    ProgressView()
                case .failed(let message):
                    VStack(spacing: 12) {
                        Text(message).multilineTextAlignment(.center)
                        Button("Try again") { Task { await editor.load() } }
                    }
                    .padding()
                case .unavailable(let message):
                    VStack(spacing: 12) {
                        Text(message).multilineTextAlignment(.center)
                        Button("Close") { dismiss() }
                    }
                    .padding()
                case .ready:
                    if editor.isOwner {
                        TemplateEditorView(editor: editor, templatesViewModel: templatesViewModel, meals: dependencies.meals)
                    } else {
                        TemplateReadOnlyView(editor: editor, templatesViewModel: templatesViewModel)
                    }
                }
            }
            .navigationTitle(editor.template?.name ?? "Template")
            .inlineNavigationTitle()
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }.accessibilityIdentifier("templateSheetDoneButton")
                }
            }
        }
        .task { await editor.load() }
    }
}
```

- [ ] **Step 5: Create `TemplateEditorView.swift`**

```swift
import API
import Repositories
import SwiftUI

/// The owner's editor: name, sharing, then one section per day with breakfast, lunch and dinner (one meal each) and
/// any number of snacks. No Save button: edits autosave, the status line says where things stand, and leaving the
/// sheet or backgrounding the app flushes a pending edit.
struct TemplateEditorView: View {
    @Bindable var editor: TemplateEditorViewModel
    let templatesViewModel: TemplatesViewModel
    let meals: MealsRepository
    @State private var picking: SlotPick?
    @State private var confirmingDelete = false
    @State private var deleteError: String?
    @Environment(\.scenePhase) private var scenePhase

    struct SlotPick: Identifiable {
        let dayIndex: Int
        let slot: Components.Schemas.Slot
        var id: String { "\(dayIndex)-\(slot.rawValue)" }
    }

    var body: some View {
        Form {
            Section {
                Text(editor.statusText).accessibilityIdentifier("templateSaveStatus")
                if let banner = editor.bannerMessage {
                    HStack {
                        Text(banner).foregroundStyle(.red)
                        Spacer()
                        Button("Try again") { Task { await editor.retry() } }
                    }
                }
            }
            Section("Template") {
                TextField("Name", text: $editor.draft.name).accessibilityIdentifier("templateNameField")
                if let message = editor.validationErrors?.name { Text(message).font(.caption).foregroundStyle(.red) }
                if editor.offersSharing {
                    Toggle("Share with my partner", isOn: $editor.draft.shared)
                }
                LabeledContent("Days", value: "\(editor.draft.dayCount)")
            }
            ForEach(0..<editor.draft.dayCount, id: \.self) { day in
                Section("Day \(day + 1)") {
                    ForEach([Components.Schemas.Slot.breakfast, .lunch, .dinner], id: \.self) { slot in
                        slotRows(day: day, slot: slot)
                    }
                    snackRows(day: day)
                }
            }
            Section {
                Button("Delete template", role: .destructive) { confirmingDelete = true }
                    .accessibilityIdentifier("deleteTemplateButton")
            }
        }
        .scrollDismissesKeyboard(.interactively)
        .onDisappear { editor.flushOnLeave() }
        .onChange(of: scenePhase) { _, phase in
            if phase != .active { editor.flushOnLeave() }
        }
        .sheet(item: $picking) { pick in
            MealPickerView(meals: meals, title: "\(Self.label(pick.slot)) · Day \(pick.dayIndex + 1)") { meal in
                editor.setMeal(dayIndex: pick.dayIndex, slot: pick.slot, meal: meal)
            }
        }
        .confirmationDialog("Delete this template?", isPresented: $confirmingDelete, titleVisibility: .visible) {
            Button("Delete template", role: .destructive) {
                let id = editor.templateID
                Task { deleteError = await templatesViewModel.delete(id: id) }
            }
        } message: {
            Text(TemplatesViewModel.deleteMessage(name: editor.template?.name ?? editor.draft.name))
        }
        .alert(
            "Couldn't delete the template",
            isPresented: Binding(get: { deleteError != nil }, set: { if !$0 { deleteError = nil } })
        ) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(deleteError ?? "")
        }
    }

    @ViewBuilder
    private func slotRows(day: Int, slot: Components.Schemas.Slot) -> some View {
        let rows = editor.draft.rows(day: day, slot: slot)
        if let row = rows.first {
            slotRow(row, title: Self.label(slot), pick: SlotPick(dayIndex: day, slot: slot))
        } else {
            Button("Add \(Self.label(slot).lowercased())") { picking = SlotPick(dayIndex: day, slot: slot) }
        }
    }

    @ViewBuilder
    private func snackRows(day: Int) -> some View {
        ForEach(editor.draft.rows(day: day, slot: .snack)) { row in
            slotRow(row, title: "Snack", pick: nil)
        }
        Button("Add snack") { picking = SlotPick(dayIndex: day, slot: .snack) }
    }

    /// One slot: the meal (tap to swap, for breakfast, lunch and dinner), its portion, swipe to remove.
    private func slotRow(_ row: TemplateDraft.Row, title: String, pick: SlotPick?) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                if let pick {
                    Button { picking = pick } label: {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(title).font(.caption).foregroundStyle(.secondary)
                            Text(row.mealName)
                        }
                    }
                    .foregroundStyle(.primary)
                } else {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(title).font(.caption).foregroundStyle(.secondary)
                        Text(row.mealName)
                    }
                }
                Spacer()
                TextField("Portion", text: portionBinding(row.id))
                    .decimalKeyboard()
                    .multilineTextAlignment(.trailing)
                    .frame(width: 64)
                    .accessibilityLabel("Portion of \(row.mealName)")
            }
            if let message = editor.validationErrors?.rows[row.id] {
                Text(message).font(.caption).foregroundStyle(.red)
            }
        }
        .swipeActions(edge: .trailing) {
            Button("Remove", role: .destructive) { editor.removeRow(id: row.id) }
        }
    }

    private func portionBinding(_ id: String) -> Binding<String> {
        Binding(
            get: { editor.draft.rows.first { $0.id == id }?.portion ?? "" },
            set: { text in
                var next = editor.draft
                if let index = next.rows.firstIndex(where: { $0.id == id }) {
                    next.rows[index].portion = text
                    editor.draft = next
                }
            }
        )
    }

    static func label(_ slot: Components.Schemas.Slot) -> String {
        switch slot {
        case .breakfast: "Breakfast"
        case .lunch: "Lunch"
        case .dinner: "Dinner"
        case .snack: "Snack"
        }
    }
}
```

- [ ] **Step 6: Create `TemplateReadOnlyView.swift`**

```swift
import SwiftUI

/// A partner's shared template: the same days and slots without controls, plus "Copy to my library", which copies the
/// template (and the meals it uses) and opens the copy in the editor. It cannot be applied: copy it first.
struct TemplateReadOnlyView: View {
    let editor: TemplateEditorViewModel
    let templatesViewModel: TemplatesViewModel
    @State private var isCopying = false
    @State private var copyError: String?

    var body: some View {
        if let template = editor.template {
            List {
                ForEach(0..<template.dayCount, id: \.self) { day in
                    Section("Day \(day + 1)") {
                        let slots = template.slots.filter { $0.dayIndex == day }
                        if slots.isEmpty {
                            Text("Nothing planned").foregroundStyle(.secondary)
                        }
                        ForEach(TemplateDraft.slotOrder, id: \.self) { slot in
                            ForEach(slots.filter { $0.slot == slot }, id: \.id) { entry in
                                HStack {
                                    VStack(alignment: .leading, spacing: 2) {
                                        Text(TemplateEditorView.label(slot)).font(.caption).foregroundStyle(.secondary)
                                        Text(entry.mealName)
                                    }
                                    Spacer()
                                    if entry.portion != 1 {
                                        Text("× \(plainNumber(entry.portion))").foregroundStyle(.secondary)
                                    }
                                }
                            }
                        }
                    }
                }
                Section {
                    Button("Copy to my library") {
                        Task {
                            isCopying = true
                            copyError = await templatesViewModel.copyToLibrary(id: template.id)
                            isCopying = false
                        }
                    }
                    .disabled(isCopying)
                    .accessibilityIdentifier("copyTemplateButton")
                }
            }
            .alert(
                "Couldn't copy the template",
                isPresented: Binding(get: { copyError != nil }, set: { if !$0 { copyError = nil } })
            ) {
                Button("OK", role: .cancel) {}
            } message: {
                Text(copyError ?? "")
            }
        }
    }
}
```

- [ ] **Step 7: Build**

Run: `cd ios/MealPlannerKit && swift build`
Expected: builds clean (macOS). Guard any UIKit-only API with `#if os(iOS)` as `ViewHelpers.swift` does.

- [ ] **Step 8: Commit**

```bash
git add ios/MealPlannerKit/Sources/Features
git commit -m "feat(ios): add the meal picker and the diet template screens" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

## Task 13: Today, Plan and the app wiring

**Files:**
- Create: `ios/MealPlannerKit/Sources/Features/Shared/RingView.swift`
- Create: `ios/MealPlannerKit/Sources/Features/Shared/Slots/SlotRowView.swift`, `PortionSheetView.swift`, `DaySlotsView.swift`
- Modify: `ios/MealPlannerKit/Sources/Features/Shared/ViewHelpers.swift`
- Replace: `ios/MealPlannerKit/Sources/Features/Today/TodayView.swift`, `ios/MealPlannerKit/Sources/Features/Plan/PlanView.swift`
- Create: `ios/MealPlannerKit/Sources/Features/Plan/WeekSummaryView.swift`, `ApplyTemplateView.swift`
- Create: `ios/MealPlannerKit/Sources/AppCore/ClearCaches.swift`
- Test: `ios/MealPlannerKit/Tests/MealPlannerKitTests/ClearCachesTests.swift`
- Modify: `ios/MealPlannerKit/Sources/AppCore/RootView.swift`, `TabShellView.swift`

**Interfaces:**
- Consumes: `PlanViewModel`, `ApplyTemplateViewModel`, `PlanDependencies` (Tasks 7-8), `MealPickerView`, `TemplatesView` (Task 12), `targetProgress`, `NutritionFormat`, `NutrientMath`/`WeekTotals`, `LocalDay`, `MacroStyle` (Meals plan Task 9), `CacheStore.makePlanCache/makeTemplateCache`, `PlanRepository`, `TemplatesRepository` (Tasks 3-6).
- Produces: `clearAllCaches(meals:plan:templates:)` (AppCore, internal), `TodayView(dependencies:)`, `PlanView(dependencies:)` (public; replace the placeholders `TodayView()`/`PlanView()`), and a launchable app whose Today and Plan tabs are the real feature, with all three caches cleared on sign-out. Accessibility identifiers used by Task 14: `todayDateLabel`, `ring-<nutrient>` (value is the spoken amount), `slotAddButton-<slot>`, `slotMeal-<slot>`, `slotMenu-<slot>`, `changePortionButton`, `portionField`, `portionSaveButton`.

No unit tests for the views: logic lives in Tasks 2-11. The one piece of wiring logic, "ending a session clears every cache", is a tested function (Step 9). The views and the wiring land in one commit because `TabShellView` cannot compile without the new view initialisers.

- [ ] **Step 1: Add the time-change helper to `ViewHelpers.swift`**

Add at the top of the file (above `import SwiftUI`):

```swift
#if os(iOS)
import UIKit
#endif
```

and inside `extension View`:

```swift
    /// Fires when the device's date or time zone changes (midnight, travelling). iOS only; elsewhere it does nothing.
    @ViewBuilder
    func onSignificantTimeChange(perform action: @escaping () -> Void) -> some View {
        #if os(iOS)
        self.onReceive(NotificationCenter.default.publisher(for: UIApplication.significantTimeChangeNotification)) { _ in action() }
        #else
        self
        #endif
    }
```

- [ ] **Step 2: Create `RingView.swift`**

```swift
import API
import SwiftUI

extension Components.Schemas.Targets {
    func target(for key: NutrientKey) -> Double? {
        switch key {
        case .calories: targetKcal
        case .protein: targetProteinG
        case .carbohydrates: targetCarbsG
        case .fat: targetFatG
        default: nil
        }
    }
}

/// One macro against its target. The ring fills to at most 100% but the number keeps the real percent (125% when
/// over). An unknown amount is "—" with an empty ring (never 0); a missing target shows the amount only.
struct RingView: View {
    let key: NutrientKey
    let amount: Double?
    let target: Double?
    var isDimmed = false
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    private var info: NutrientInfo { NutrientCatalog.info(for: key) }
    private var progress: TargetProgress? { targetProgress(value: amount, target: target) }

    var body: some View {
        VStack(spacing: 6) {
            ZStack {
                Circle().stroke(MacroStyle.color(for: key).opacity(0.2), lineWidth: 10)
                Circle()
                    .trim(from: 0, to: progress?.fraction ?? 0)
                    .stroke(MacroStyle.color(for: key), style: StrokeStyle(lineWidth: 10, lineCap: .round))
                    .rotationEffect(.degrees(-90))
                    .animation(reduceMotion ? nil : .default, value: progress?.fraction)
                VStack(spacing: 0) {
                    Text(NutritionFormat.amount(amount, unit: info.unit))
                        .font(.caption.weight(.semibold))
                        .monospacedDigit()
                        .minimumScaleFactor(0.7)
                        .lineLimit(1)
                    if let progress {
                        Text("\(progress.percent)%")
                            .font(.caption2)
                            .foregroundStyle(progress.over ? Color.red : Color.secondary)
                    }
                }
                .padding(.horizontal, 10)
            }
            .frame(width: 84, height: 84)
            Text(info.label).font(.caption).foregroundStyle(.secondary)
        }
        .opacity(isDimmed ? 0.5 : 1)
        // VoiceOver: "Calories, 520 of 2,000 kilocalories, 26 percent"; no target and unknown are spoken plainly.
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(info.label)
        .accessibilityValue(NutritionFormat.ringSpoken(amount: amount, target: target, unit: info.unit))
        .accessibilityIdentifier("ring-\(key.rawValue)")
    }
}
```

- [ ] **Step 3: Create `Slots/SlotRowView.swift`**

```swift
import API
import SwiftUI

/// One slot of one day (breakfast, lunch, dinner, or the snacks), shared by Today and Plan. Pure presentation: every
/// action is a closure, and `DaySlotsView` owns the sheets they open.
struct SlotRowView: View {
    let slot: Components.Schemas.Slot
    let entries: [Components.Schemas.PlanEntry]
    let isDisabled: Bool
    let onPick: () -> Void
    let onChangePortion: (Components.Schemas.PlanEntry) -> Void
    let onRemove: () -> Void
    let onClearSnacks: () -> Void

    private var title: String {
        switch slot {
        case .breakfast: "Breakfast"
        case .lunch: "Lunch"
        case .dinner: "Dinner"
        case .snack: "Snacks"
        }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(title).font(.subheadline.weight(.semibold)).foregroundStyle(.secondary)
            if slot == .snack { snacks } else { single }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .disabled(isDisabled)
    }

    @ViewBuilder
    private var single: some View {
        if let entry = entries.first {
            HStack {
                Button(action: onPick) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(entry.mealName)
                        if entry.portion != 1 {
                            Text("× \(plainNumber(entry.portion))").font(.caption).foregroundStyle(.secondary)
                        }
                    }
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier("slotMeal-\(slot.rawValue)")
                Spacer()
                Menu {
                    Button("Change portion") { onChangePortion(entry) }
                        .accessibilityIdentifier("changePortionButton")
                    Button("Remove", role: .destructive, action: onRemove)
                } label: {
                    Image(systemName: "ellipsis.circle")
                }
                .accessibilityLabel("\(title) options")
                .accessibilityIdentifier("slotMenu-\(slot.rawValue)")
            }
            .contextMenu {
                Button("Change portion") { onChangePortion(entry) }
                Button("Remove", role: .destructive, action: onRemove)
            }
        } else {
            Button("Add meal", action: onPick)
                .accessibilityIdentifier("slotAddButton-\(slot.rawValue)")
        }
    }

    /// A single snack cannot be edited or removed: the API addresses snacks only by date and slot, so the plan can
    /// only add one or clear them all.
    @ViewBuilder
    private var snacks: some View {
        ForEach(entries, id: \.id) { entry in
            HStack {
                Text(entry.mealName)
                if entry.portion != 1 {
                    Text("× \(plainNumber(entry.portion))").font(.caption).foregroundStyle(.secondary)
                }
            }
        }
        HStack {
            Button("Add snack", action: onPick)
                .accessibilityIdentifier("slotAddButton-snack")
            if !entries.isEmpty {
                Spacer()
                Button("Clear snacks", role: .destructive, action: onClearSnacks)
                    .accessibilityIdentifier("clearSnacksButton")
            }
        }
    }
}
```

- [ ] **Step 4: Create `Slots/PortionSheetView.swift`**

```swift
import SwiftUI

/// A small sheet with one decimal field: more than 0 and at most 100. Saving is a `PUT` with the slot's current meal
/// and the new portion (done by the caller through `onSave`, which returns the error text or `nil`).
struct PortionSheetView: View {
    let mealName: String
    let onSave: (Double) async -> String?
    @State private var text: String
    @State private var error: String?
    @State private var isSaving = false
    @Environment(\.dismiss) private var dismiss

    init(mealName: String, initial: Double, onSave: @escaping (Double) async -> String?) {
        self.mealName = mealName
        self.onSave = onSave
        _text = State(initialValue: plainNumber(initial))
    }

    var body: some View {
        NavigationStack {
            Form {
                Section(mealName) {
                    TextField("Portion", text: $text)
                        .decimalKeyboard()
                        .accessibilityIdentifier("portionField")
                    if let error { Text(error).font(.caption).foregroundStyle(.red) }
                }
                Section {
                    Button("Save") {
                        Task {
                            guard let portion = Portion.value(text) else {
                                error = Portion.message
                                return
                            }
                            isSaving = true
                            error = await onSave(portion)
                            isSaving = false
                            if error == nil { dismiss() }
                        }
                    }
                    .disabled(isSaving)
                    .accessibilityIdentifier("portionSaveButton")
                }
            }
            .navigationTitle("Change portion")
            .inlineNavigationTitle()
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
            }
        }
    }
}
```

- [ ] **Step 5: Create `Slots/DaySlotsView.swift`**

```swift
import API
import Repositories
import SwiftUI

/// The four slots of one day with their actions, as ONE grouped list row: a single set of sheets (picker, portion,
/// clear-snacks confirmation, error alert) serves the whole day. Used by Today and by each day of Plan.
struct DaySlotsView: View {
    let date: String
    let viewModel: PlanViewModel
    let meals: MealsRepository
    @State private var picking: SlotChoice?
    @State private var portionEdit: PortionEdit?
    @State private var confirmingClear = false
    @State private var errorText: String?

    struct SlotChoice: Identifiable {
        let slot: Components.Schemas.Slot
        var id: String { slot.rawValue }
    }

    struct PortionEdit: Identifiable {
        let entry: Components.Schemas.PlanEntry
        var id: String { entry.id }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            ForEach(Components.Schemas.Slot.allCases, id: \.self) { slot in
                SlotRowView(
                    slot: slot,
                    entries: viewModel.entries(on: date, slot: slot),
                    isDisabled: viewModel.isWriting,
                    onPick: { picking = SlotChoice(slot: slot) },
                    onChangePortion: { portionEdit = PortionEdit(entry: $0) },
                    onRemove: { Task { errorText = await viewModel.remove(date: date, slot: slot) } },
                    onClearSnacks: { confirmingClear = true }
                )
            }
        }
        .sheet(item: $picking) { choice in
            MealPickerView(meals: meals, title: "\(label(choice.slot)) · \(viewModel.localDay.heading(date))") { meal in
                Task { errorText = await viewModel.setMeal(date: date, slot: choice.slot, mealID: meal.id, portion: 1) }
            }
        }
        .sheet(item: $portionEdit) { edit in
            PortionSheetView(mealName: edit.entry.mealName, initial: edit.entry.portion) { portion in
                await viewModel.setMeal(date: date, slot: edit.entry.slot, mealID: edit.entry.mealId, portion: portion)
            }
        }
        .confirmationDialog("Clear all snacks?", isPresented: $confirmingClear, titleVisibility: .visible) {
            Button("Clear snacks", role: .destructive) {
                Task { errorText = await viewModel.clearSnacks(date: date) }
            }
        } message: {
            Text("This removes every snack planned for \(viewModel.localDay.longDate(date)).")
        }
        .alert(
            "Couldn't complete that",
            isPresented: Binding(get: { errorText != nil }, set: { if !$0 { errorText = nil } })
        ) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(errorText ?? "")
        }
    }

    private func label(_ slot: Components.Schemas.Slot) -> String {
        switch slot {
        case .breakfast: "Breakfast"
        case .lunch: "Lunch"
        case .dinner: "Dinner"
        case .snack: "Snack"
        }
    }
}
```

- [ ] **Step 6: Replace `TodayView.swift`**

```swift
import API
import Repositories
import SwiftUI

/// Today: the day's calories and macros as rings against the targets `GET /plan` returns, and the day's four slots
/// editable in place. The date follows the device's local day (on foreground and on a significant time change).
public struct TodayView: View {
    @State private var viewModel: PlanViewModel
    private let dependencies: PlanDependencies
    @Environment(\.scenePhase) private var scenePhase

    public init(dependencies: PlanDependencies) {
        self.dependencies = dependencies
        _viewModel = State(initialValue: PlanViewModel(plan: dependencies.plan))
    }

    private var date: String { viewModel.range.from }

    public var body: some View {
        List {
            if viewModel.isStale {
                Label("Offline: showing saved plan", systemImage: "wifi.slash")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            if let notice = viewModel.notice {
                Text(notice).font(.footnote).foregroundStyle(.secondary)
            }
            if let error = viewModel.loadError {
                VStack(alignment: .leading, spacing: 8) {
                    Text(error)
                    Button("Retry") { Task { await viewModel.load() } }
                }
            }
            Section {
                Text(viewModel.localDay.longDate(date))
                    .font(.headline)
                    .accessibilityIdentifier("todayDateLabel")
                HStack(spacing: 12) {
                    ForEach(NutrientCatalog.macroKeys) { key in
                        RingView(
                            key: key,
                            amount: viewModel.nutrition(on: date).flatMap { key.amount(in: $0) },
                            target: viewModel.targets?.target(for: key),
                            isDimmed: viewModel.isBusy(date)
                        )
                        .frame(maxWidth: .infinity)
                    }
                }
                if NutrientCatalog.macroKeys.contains(where: { viewModel.targets?.target(for: $0) == nil }) {
                    Text("Set daily targets in Profile.").font(.footnote).foregroundStyle(.secondary)
                }
            }
            Section {
                DaySlotsView(date: date, viewModel: viewModel, meals: dependencies.meals)
            }
        }
        .navigationTitle("Today")
        .refreshable { await viewModel.followToday() }
        .task { await viewModel.followToday() }
        .onChange(of: scenePhase) { _, phase in
            if phase == .active { Task { await viewModel.followToday() } }
        }
        .onSignificantTimeChange { Task { await viewModel.followToday() } }
    }
}
```

- [ ] **Step 7: Create `WeekSummaryView.swift` and `ApplyTemplateView.swift`**

```swift
import API
import SwiftUI

/// The week's total and per-day average, from the server's per-day totals. Shown only once all seven days are in
/// the cache (a date never fetched is absent, not empty).
struct WeekSummaryView: View {
    let totals: WeekTotals?

    var body: some View {
        if let totals {
            VStack(alignment: .leading, spacing: 8) {
                line("Week total", totals.total)
                line("Per day", totals.perDayAverage)
            }
        } else {
            Text("Loading the week…").foregroundStyle(.secondary)
        }
    }

    private func line(_ title: String, _ n: Components.Schemas.NutrientAmounts) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(title).font(.subheadline.weight(.semibold))
            Text(macroLine(n)).font(.footnote).foregroundStyle(.secondary).monospacedDigit()
        }
    }
}

/// "1,850 kcal · Protein 92 g · Carbs 210 g · Fat 60 g"; unknown amounts are "—".
func macroLine(_ n: Components.Schemas.NutrientAmounts) -> String {
    [
        NutritionFormat.amount(n.calories, unit: .kcal),
        "Protein \(NutritionFormat.amount(n.protein, unit: .g))",
        "Carbs \(NutritionFormat.amount(n.carbohydrates, unit: .g))",
        "Fat \(NutritionFormat.amount(n.fat, unit: .g))",
    ].joined(separator: " · ")
}
```

```swift
import SwiftUI

/// The apply sheet: pick one of my templates and a start date. A `409 plan_conflict` becomes the "Replace?" question.
struct ApplyTemplateView: View {
    @State private var viewModel: ApplyTemplateViewModel
    private let localDay: LocalDay
    @Environment(\.dismiss) private var dismiss

    init(dependencies: PlanDependencies, plan: PlanViewModel, startDate: String) {
        localDay = plan.localDay
        _viewModel = State(initialValue: ApplyTemplateViewModel(templates: dependencies.templates, plan: plan, startDate: startDate))
    }

    var body: some View {
        @Bindable var viewModel = viewModel
        NavigationStack {
            Form {
                Section("Template") {
                    if viewModel.templates.isEmpty {
                        Text(viewModel.isLoading ? "Loading…" : "You have no templates yet.").foregroundStyle(.secondary)
                    } else {
                        Picker("Template", selection: $viewModel.selectedID) {
                            ForEach(viewModel.templates, id: \.id) { template in
                                Text("\(template.name) · \(template.dayCount) day\(template.dayCount == 1 ? "" : "s")")
                                    .tag(Optional(template.id))
                            }
                        }
                        .pickerStyle(.inline)
                        .labelsHidden()
                    }
                }
                Section("Start date") {
                    DatePicker(
                        "Start date",
                        selection: Binding(
                            get: { localDay.date(viewModel.startDate) ?? Date() },
                            set: { viewModel.startDate = localDay.day(from: $0) }
                        ),
                        displayedComponents: .date
                    )
                    .accessibilityIdentifier("applyStartDate")
                }
                if let message = viewModel.errorMessage {
                    Section { Text(message).foregroundStyle(.red) }
                }
                Section {
                    Button("Apply") { Task { await viewModel.apply() } }
                        .disabled(viewModel.isApplying || viewModel.selected == nil)
                        .accessibilityIdentifier("applyButton")
                }
            }
            .navigationTitle("Apply template")
            .inlineNavigationTitle()
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
            }
            .alert("Replace the meals already planned?", isPresented: $viewModel.confirmingReplace) {
                Button("Replace", role: .destructive) { Task { await viewModel.confirmReplace() } }
                Button("Cancel", role: .cancel) {}
            } message: {
                Text("Days from \(localDay.heading(viewModel.startDate)) already have meals in these slots.")
            }
            .onChange(of: viewModel.didApply) { _, done in
                if done { dismiss() }
            }
        }
        .task { await viewModel.load() }
    }
}
```

- [ ] **Step 8: Replace `PlanView.swift`**

```swift
import API
import SwiftUI

/// Plan: a Monday-to-Sunday week with its total and per-day average, and each day's four slots editable in place
/// (the same rows as Today). The toolbar pushes the diet templates and opens "Apply template".
public struct PlanView: View {
    @State private var viewModel: PlanViewModel
    @State private var weekStart: String
    @State private var applying = false
    private let dependencies: PlanDependencies
    @Environment(\.scenePhase) private var scenePhase

    public init(dependencies: PlanDependencies) {
        self.dependencies = dependencies
        let localDay = LocalDay()
        let monday = localDay.startOfWeek(localDay.today())
        _weekStart = State(initialValue: monday)
        _viewModel = State(initialValue: PlanViewModel(
            plan: dependencies.plan, localDay: localDay,
            range: .init(from: monday, to: localDay.addDays(monday, 6))
        ))
    }

    private var weekDays: [String] { viewModel.localDay.weekDays(startingAt: weekStart) }
    private var onCurrentWeek: Bool { weekStart == viewModel.localDay.startOfWeek(viewModel.localDay.today()) }

    public var body: some View {
        List {
            if viewModel.isStale {
                Label("Offline: showing saved plan", systemImage: "wifi.slash")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            if let notice = viewModel.notice {
                Text(notice).font(.footnote).foregroundStyle(.secondary)
            }
            if let error = viewModel.loadError {
                VStack(alignment: .leading, spacing: 8) {
                    Text(error)
                    Button("Retry") { Task { await viewModel.load() } }
                }
            }
            Section {
                weekBar
                WeekSummaryView(totals: WeekTotals.make(days: viewModel.days, dates: weekDays))
                    .opacity(viewModel.isWriting ? 0.5 : 1)
            }
            ForEach(weekDays, id: \.self) { date in
                Section {
                    DaySlotsView(date: date, viewModel: viewModel, meals: dependencies.meals)
                } header: {
                    dayHeader(date)
                }
            }
        }
        .navigationTitle("Plan")
        .toolbar {
            ToolbarItemGroup(placement: .primaryAction) {
                NavigationLink {
                    TemplatesView(dependencies: dependencies)
                } label: {
                    Image(systemName: "list.bullet.rectangle")
                }
                .accessibilityLabel("Diet templates")
                .accessibilityIdentifier("dietTemplatesLink")
                Button { applying = true } label: {
                    Image(systemName: "calendar.badge.plus")
                }
                .accessibilityLabel("Apply template")
                .accessibilityIdentifier("applyTemplateButton")
            }
        }
        .refreshable { await viewModel.load() }
        .task { await moveTo(weekStart) }
        .onChange(of: scenePhase) { _, phase in
            if phase == .active { Task { await viewModel.load() } }
        }
        .sheet(isPresented: $applying) {
            ApplyTemplateView(dependencies: dependencies, plan: viewModel, startDate: weekStart)
        }
    }

    private var weekBar: some View {
        HStack {
            Button { Task { await moveTo(viewModel.localDay.addDays(weekStart, -7)) } } label: {
                Image(systemName: "chevron.left")
            }
            .accessibilityLabel("Previous week")
            Spacer()
            Text(viewModel.localDay.weekRange(startingAt: weekStart))
                .font(.headline)
                .accessibilityIdentifier("planWeekLabel")
            Spacer()
            Button { Task { await moveTo(viewModel.localDay.addDays(weekStart, 7)) } } label: {
                Image(systemName: "chevron.right")
            }
            .accessibilityLabel("Next week")
            Button("This week") {
                Task { await moveTo(viewModel.localDay.startOfWeek(viewModel.localDay.today())) }
            }
            .disabled(onCurrentWeek)
        }
        .buttonStyle(.borderless)
    }

    private func dayHeader(_ date: String) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(viewModel.localDay.heading(date) + (date == viewModel.localDay.today() ? " · Today" : ""))
                .font(.subheadline.weight(.semibold))
            Text(viewModel.nutrition(on: date).map(macroLine) ?? "—")
                .font(.caption)
                .monospacedDigit()
                .opacity(viewModel.isBusy(date) ? 0.5 : 1)
        }
        .textCase(nil)
    }

    private func moveTo(_ monday: String) async {
        weekStart = monday
        await viewModel.setRange(from: monday, to: viewModel.localDay.addDays(monday, 6))
    }
}
```

- [ ] **Step 9: `clearAllCaches`, test first**

`ClearCachesTests.swift`:

```swift
import API
import Persistence
import Repositories
import Testing
@testable import AppCore

@Suite
struct ClearCachesTests {
    @Test("Ending a session empties the meal, plan and template caches, so a second user never sees the first user's data")
    func clearsEverything() async throws {
        let container = try CacheStore.inMemoryContainer()
        let mealCache = CacheStore.makeMealCache(container)
        let planCache = CacheStore.makePlanCache(container)
        let templateCache = CacheStore.makeTemplateCache(container)
        let client = makeAuthlessClient(transport: RoutingTransport { _ in (500, "") })
        await mealCache.replaceSummaries([Fixtures.summary()], scope: .mine)
        await planCache.replace(days: [Fixtures.day("2026-10-05")], targets: Fixtures.targets())
        await templateCache.replaceSummaries([Fixtures.templateSummary()], scope: .mine)

        await clearAllCaches(
            meals: MealsRepository(client: client, cache: mealCache),
            plan: PlanRepository(client: client, cache: planCache),
            templates: TemplatesRepository(client: client, cache: templateCache)
        )

        #expect(await mealCache.summaries(scope: .mine).isEmpty)
        #expect(await planCache.days(from: "2000-01-01", to: "2100-01-01").isEmpty)
        #expect(await planCache.targets() == nil)
        #expect(await templateCache.summaries(scope: .mine).isEmpty)
    }
}
```

Run: `cd ios/MealPlannerKit && swift test --filter ClearCachesTests`
Expected: FAIL to compile (`clearAllCaches` does not exist).

Create `ios/MealPlannerKit/Sources/AppCore/ClearCaches.swift`:

```swift
import Repositories

/// Everything that must be emptied whenever the session ends, by any path (`AppState.clearCaches`), so a second user
/// on this device never sees the first user's meals, plan or templates.
func clearAllCaches(meals: MealsRepository, plan: PlanRepository, templates: TemplatesRepository) async {
    await meals.clearCaches()
    await plan.clearCaches()
    await templates.clearCaches()
}
```

Run: `cd ios/MealPlannerKit && swift test --filter ClearCachesTests`
Expected: PASS.

- [ ] **Step 10: Wire it in `RootView.swift` and `TabShellView.swift`**

In `RootView.init`, build one container and all three caches and repositories (replace the two `mealCache`/`mealsRepository` lines and extend the dependencies and the `clearCaches` closure):

```swift
        let container = CacheStore.launchContainer()
        let mealsRepository = MealsRepository(client: client, cache: CacheStore.makeMealCache(container))
        let planRepository = PlanRepository(client: client, cache: CacheStore.makePlanCache(container))
        let templatesRepository = TemplatesRepository(client: client, cache: CacheStore.makeTemplateCache(container))
        let partnerRepository = PartnerRepository(client: client)

        self.refresher = refresher
        self.mealsDependencies = MealsDependencies(
            meals: mealsRepository,
            ingredients: IngredientsRepository(client: client),
            partner: partnerRepository
        )
        self.planDependencies = PlanDependencies(
            plan: planRepository, templates: templatesRepository, meals: mealsRepository, partner: partnerRepository
        )
        _appState = State(initialValue: AppState(
            authRepository: authRepository,
            tokenStore: tokenStore,
            clearCaches: {
                await clearAllCaches(meals: mealsRepository, plan: planRepository, templates: templatesRepository)
            }
        ))
```

Add the stored property beside `mealsDependencies`:

```swift
    private let planDependencies: PlanDependencies
```

and pass it to the shell: `TabShellView(appState: appState, mealsDependencies: mealsDependencies, planDependencies: planDependencies)`.

`TabShellView.swift`:

```swift
import SwiftUI
import Features

struct TabShellView: View {
    let appState: AppState
    let mealsDependencies: MealsDependencies
    let planDependencies: PlanDependencies

    var body: some View {
        TabView {
            NavigationStack { TodayView(dependencies: planDependencies) }
                .tabItem { Label("Today", systemImage: "sun.max").accessibilityIdentifier("todayTab") }
            NavigationStack { PlanView(dependencies: planDependencies) }
                .tabItem { Label("Plan", systemImage: "calendar").accessibilityIdentifier("planTab") }
            NavigationStack { MealsView(dependencies: mealsDependencies) }
                .tabItem { Label("Meals", systemImage: "fork.knife").accessibilityIdentifier("mealsTab") }
            NavigationStack { ShoppingView() }
                .tabItem { Label("Shopping", systemImage: "cart").accessibilityIdentifier("shoppingTab") }
            NavigationStack {
                ProfileView(onSignOut: { Task { await appState.signOut() } })
            }
            .tabItem { Label("Profile", systemImage: "person").accessibilityIdentifier("profileTab") }
        }
    }
}
```

- [ ] **Step 11: Run the whole suite and build the app**

Run: `cd ios/MealPlannerKit && swift test`
Expected: PASS (every suite).

Run: `make build-ios` (from the repo root)
Expected: `** BUILD SUCCEEDED **`. This is the first compile of the iOS-only paths (`UIApplication`, `#if os(iOS)` branches); a failure here is expected to be a UIKit-only API that needs a guard.

Run: `cd ios && xcodebuild build-for-testing -project MealPlanner.xcodeproj -scheme MealPlanner -destination 'generic/platform=iOS Simulator' 2>&1 | grep -E "error:|TEST BUILD"`
Expected: `** TEST BUILD SUCCEEDED **` (`make build-ios` does not compile the UI-test target).

- [ ] **Step 12: Commit**

```bash
git add ios/MealPlannerKit/Sources ios/MealPlannerKit/Tests/MealPlannerKitTests/ClearCachesTests.swift
git commit -m "feat(ios): build Today and Plan: rings, shared slot rows, week view, apply" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

## Task 14: XCUITest flow against the real API

**Files:**
- Modify: `ios/MealPlannerUITests/AppUITestCase.swift`
- Create: `ios/MealPlannerUITests/PlanTodayFlowUITests.swift`

**Interfaces:**
- Consumes: the identifiers from Tasks 12-13 and the existing auth ones (`signInEmailField`, `signInPasswordField`, `signInSubmitButton`, `signOutButton`, `showRegisterButton`, `planTab`, `profileTab`).
- Produces: `AppUITestCase.apiRequest(_:_:token:json:expect:) throws -> [String: Any]`, `createAccountViaAPI` now returning the access token, `expectValue(of:toContain:timeout:)`, `element(withLabelContaining:in:)`, and one new flow.

CI's database has no meals, so the test creates an account, an ingredient with calories and a meal containing it through the API, signs in (the registration form's password field drops characters on CI), adds the meal to Breakfast on Today, changes the portion to 2, and checks the calories ring doubles: the server computes the total, so doubling proves the whole round trip. Then it finds the same meal on Plan. Local XCUITest is unreliable under load: compile it here, and treat GitHub CI as the signal.

- [ ] **Step 1: Replace the API helper in `AppUITestCase.swift`**

Delete the existing `StatusBox` class and `createAccountViaAPI` function and add (keep `diagnose(...)` and the registration/sign-in helpers as they are):

```swift
    private final class ResponseBox: @unchecked Sendable {
        var code = 0
        var data = Data()
    }

    /// A synchronous JSON request to the API from the test process, for flows that are not about the screens that
    /// would otherwise create the data. Fails the test unless the status is `expect`.
    @discardableResult
    func apiRequest(_ method: String, _ path: String, token: String? = nil, json: [String: Any]? = nil, expect: Int) throws -> [String: Any] {
        var request = URLRequest(url: URL(string: "http://localhost:8080/v1" + path)!)
        request.httpMethod = method
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        if let token { request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
        if let json { request.httpBody = try JSONSerialization.data(withJSONObject: json) }
        let done = expectation(description: "\(method) \(path)")
        let box = ResponseBox()
        URLSession.shared.dataTask(with: request) { data, response, _ in
            box.code = (response as? HTTPURLResponse)?.statusCode ?? 0
            box.data = data ?? Data()
            done.fulfill()
        }.resume()
        wait(for: [done], timeout: 30)
        XCTAssertEqual(box.code, expect, "\(method) \(path)")
        return ((try? JSONSerialization.jsonObject(with: box.data)) as? [String: Any]) ?? [:]
    }

    /// Creates an account straight through the API (the registration form's `.newPassword` field is the one place where
    /// Password AutoFill drops typed characters on CI, so flows that are not about registration sign in instead) and
    /// returns its access token. The Auth flows still register through the form.
    @discardableResult
    func createAccountViaAPI(email: String, password: String, displayName: String) throws -> String {
        let body = try apiRequest("POST", "/auth/register", json: ["email": email, "password": password, "display_name": displayName], expect: 201)
        return try XCTUnwrap(body["access_token"] as? String)
    }

    /// Waits until the element's accessibility value contains `text`.
    func expectValue(of element: XCUIElement, toContain text: String, timeout: TimeInterval = 45, file: StaticString = #filePath, line: UInt = #line) {
        let predicate = NSPredicate(format: "value CONTAINS %@", text)
        let expectation = XCTNSPredicateExpectation(predicate: predicate, object: element)
        XCTAssertEqual(XCTWaiter().wait(for: [expectation], timeout: timeout), .completed, "Expected the value to contain \"\(text)\"", file: file, line: line)
    }

    /// A button or text whose label contains `text` (a plain-styled button's inner text is not a separate element).
    func element(withLabelContaining text: String, in app: XCUIApplication) -> XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", text)).firstMatch
    }
```

The Meals flow's `try createAccountViaAPI(...)` call keeps compiling (the result is discardable).

- [ ] **Step 2: Create `PlanTodayFlowUITests.swift`**

```swift
import XCTest

final class PlanTodayFlowUITests: AppUITestCase {
    /// Add a meal to Breakfast on Today, change its portion to 2, and see the server's calories double, then find the
    /// meal on Plan. The meal is 100 kcal per serving (one ingredient, 100 g of a 100 kcal/100 g ingredient), so the
    /// ring reads 100 then 200 only if the PUT reached the API and its computed total came back.
    func testAddMealToTodayChangePortionAndSeeItOnPlan() throws {
        let email = uniqueEmail()
        let password = "correct-horse-battery-staple"
        let token = try createAccountViaAPI(email: email, password: password, displayName: "iOS Plan")
        let ingredient = try apiRequest(
            "POST", "/ingredients", token: token,
            json: ["name": "UI Plan Oats", "category": "other", "nutrients": ["calories": 100]], expect: 201
        )
        let meal = try apiRequest("POST", "/meals", token: token, json: ["name": "UI Plan Meal", "servings": 1], expect: 201)
        try apiRequest(
            "PUT", "/meals/\(try XCTUnwrap(meal["id"] as? String))/ingredients", token: token,
            json: ["items": [["ingredient_id": try XCTUnwrap(ingredient["id"] as? String), "quantity": 100, "unit": "g"]]], expect: 200
        )

        let app = XCUIApplication()
        app.launch()
        signIn(in: app, email: email, password: password)

        // Today is the default tab.
        let add = app.buttons["slotAddButton-breakfast"]
        waitUntilHittable(add, timeout: 45)
        add.tap()
        let mealRow = app.buttons["mealPickerRow-UI Plan Meal"]
        waitUntilHittable(mealRow, timeout: 45)
        mealRow.tap()

        let calories = app.descendants(matching: .any).matching(identifier: "ring-calories").firstMatch
        XCTAssertTrue(calories.waitForExistence(timeout: 45))
        expectValue(of: calories, toContain: "100")

        let menu = app.buttons["slotMenu-breakfast"]
        waitUntilHittable(menu, timeout: 45)
        menu.tap()
        let changePortion = app.buttons["changePortionButton"]
        waitUntilHittable(changePortion)
        changePortion.tap()
        let portionField = app.textFields["portionField"]
        waitUntilHittable(portionField)
        clearAndType("2", field: portionField)
        app.buttons["portionSaveButton"].tap()
        expectValue(of: calories, toContain: "200")

        let planTab = tabButton(in: app, identifier: "planTab", label: "Plan")
        XCTAssertTrue(planTab.waitForExistence(timeout: 45))
        planTab.tap()
        XCTAssertTrue(
            element(withLabelContaining: "UI Plan Meal", in: app).waitForExistence(timeout: 45),
            "Expected today's meal on the Plan tab's week"
        )

        // Leave no session in the Keychain for the next test.
        signOutButton(in: app).tap()
        XCTAssertTrue(app.buttons["showRegisterButton"].waitForExistence(timeout: 45), "Expected the welcome screen after signing out")
    }
}
```

- [ ] **Step 3: Compile the UI-test target**

Run: `cd ios && xcodebuild build-for-testing -project MealPlanner.xcodeproj -scheme MealPlanner -destination 'generic/platform=iOS Simulator' 2>&1 | grep -E "error:|TEST BUILD"`
Expected: `** TEST BUILD SUCCEEDED **`.

- [ ] **Step 4: Run the flow if the machine allows (optional locally; CI is the signal)**

Needs the API running (`make db-up`, `make migrate`, `make run-api`) and the simulator reset (`xcrun simctl shutdown 5F31CC42-891A-4A77-8740-FFB7012827F3; xcrun simctl erase 5F31CC42-891A-4A77-8740-FFB7012827F3`). Check `uptime` first: at a load average in the hundreds, skip it.

Run: `cd ios && xcodebuild test -project MealPlanner.xcodeproj -scheme MealPlanner -destination 'platform=iOS Simulator,id=5F31CC42-891A-4A77-8740-FFB7012827F3' -only-testing:MealPlannerUITests/PlanTodayFlowUITests`
Expected: PASS. Otherwise ledger that it was compiled but not run, and read the GitHub `ui` job after the push (see the Meals plan's notes on rerunning a hung job: `gh run rerun <run-id> --failed`).

- [ ] **Step 5: Commit**

```bash
git add ios/MealPlannerUITests
git commit -m "test(ios): add the Plan and Today XCUITest flow" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

## Task 15: Documentation and final verification

**Files:**
- Modify: `ios/CLAUDE.md`
- Modify: `docs/superpowers/specs/2026-10-03-ios-plan-today-design.md` (one sentence, §6)

**Interfaces:**
- Consumes: everything above.
- Produces: `ios/CLAUDE.md` current for the new caches, repositories, view models and gotchas; the spec's slot-action wording matches what was built.

- [ ] **Step 1: Update `ios/CLAUDE.md`**

In **Layout**, extend the `Sources/Persistence/`, `Sources/Repositories/` and `Sources/Features/` bullets (keep their existing text and append):

```markdown
    Also `PlanCache` (one row per date holding the generated `DailyTotal` JSON, plus a targets row) and `TemplateCache` (Mine / Partner's, reusing `MealScope`).
```

```markdown
    Also `PlanRepository` (`cached`/`refresh` for a date range; `setEntry`, `clearSlot`, `apply` are writes only and touch no cache) and `TemplatesRepository`.
```

```markdown
    `Shared/` also holds `Autosaver` (the debounce engine behind both editors), `LocalDay`, `TargetProgress`, `NutrientMath`/`WeekTotals`, `RingView`, the slot rows and `MealPicker`. `Today/` and `Plan/` (with `Plan/Templates/`) are the Plan and Today feature.
```

Append to **Gotchas**:

```markdown
- Plan writes are write-then-refresh, as two separate steps in `PlanViewModel`: the repository write, then `refresh` of the affected dates. Totals always come from `GET /plan`, never from the device (the only arithmetic is summing and averaging the server's per-day totals for the week, and a nutrient unknown on any day stays unknown). A failed refresh after a good write is reported as exactly that and the write is never repeated. One write at a time; only the written range dims.
- The plan cache is per date. A date never fetched is absent from `PlanCache.days`, not empty: the week summary appears only when all seven days are cached.
- Dates are the device's local calendar day as `YYYY-MM-DD` (`LocalDay`), never UTC; weeks start Monday; arithmetic goes through `Calendar` on noon of the day so a week is seven days across a daylight-saving change. Today re-reads the date on foreground and on `significantTimeChangeNotification`.
- `DELETE /plan/{date}/{slot}` on an empty slot answers `404`; the repository treats that as success. A single snack cannot be edited or removed (the API addresses snacks by date and slot): the plan can only add one or clear them all. A template's snacks are editable (its slots are replaced as a whole).
- The four slots of a day are one grouped list row (`DaySlotsView`), so one set of sheets serves the day; slot actions are a visible menu and a context menu, not swipe actions.
- A partner's template cannot be applied (`404`): copy it first. `409 plan_conflict` on apply is a question ("Replace the meals already planned?"), retried with `overwrite: true`; snack slots never conflict.
- `Autosaver` is shared by `MealEditorViewModel` and `TemplateEditorViewModel`. Each editor keeps its own partial-commit logic in its `perform` closure (fields `PATCH`, then the list `PUT`, each committed on its own). Template slots are compared in a canonical order so the server's ordering never reads as an edit.
- UI flows that are not about registration create their account (and any data) through the API with `AppUITestCase.apiRequest` and sign in through the UI. `make build-ios` does not compile the UI-test target: use `xcodebuild build-for-testing` (the Plan and Today plan's Task 14) before pushing UI-test changes.
- Test helpers: `PlanServer` (in `PlanTestSupport.swift`) is an in-memory stand-in for the plan endpoints that computes the day's calories itself (100 per portion), so view-model tests never compute totals in the code under test.
```

- [ ] **Step 2: Update the spec's slot-action wording**

In `docs/superpowers/specs/2026-10-03-ios-plan-today-design.md` §6, replace "A menu (swipe actions and a context menu) offers" with "A visible \"⋯\" menu and a long-press context menu offer (no swipe actions: a day's four slots are one grouped list row, so one set of sheets serves the day)".

- [ ] **Step 3: Final verification**

Run each from the repo root and read the output:

```bash
make test-ios
make check-generated-ios
make build-ios
(cd ios && xcodebuild build-for-testing -project MealPlanner.xcodeproj -scheme MealPlanner -destination 'generic/platform=iOS Simulator' 2>&1 | grep -E "error:|TEST BUILD")
git status --short
```

Expected: all tests pass; no generated-code drift (this plan never touches `openapi.yaml`); `BUILD SUCCEEDED` and `TEST BUILD SUCCEEDED`; `git status` shows only the two docs files modified.

- [ ] **Step 4: Commit**

```bash
git add ios/CLAUDE.md docs/superpowers/specs/2026-10-03-ios-plan-today-design.md
git commit -m "docs(ios): document the plan cache, write-then-refresh and the date rules" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

- [ ] **Step 5: Hand back**

Do not push or open a PR: the user asks for each explicitly. Report: tasks done, the exact commands run and their results, the decisions at the top of this plan that were applied, and any place where a generated enum or an API shape forced a mirror change. Suggest `superpowers:requesting-code-review` on the whole branch, then `superpowers:finishing-a-development-branch`.
