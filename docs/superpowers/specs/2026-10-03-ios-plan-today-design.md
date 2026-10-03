# iOS Plan and Today Design

Date: 2026-10-03
Status: draft for review. Extends `2026-09-30-ios-app-design.md` (§3, §4, §6, §9, §11, §15 plan 3) and builds on `2026-10-02-ios-meals-design.md`, whose repository/cache/view-model conventions it reuses unchanged. Where this document and the iOS spec disagree on Plan, Today or diet templates, this document wins and the iOS spec is updated in the same change (§12). The shipped web feature (`web/src/features/plan/`, `web/src/lib/use-today.ts`, `web/src/lib/nutrition/progress.ts`, `web/src/lib/dates.ts`) is the parity reference; every "as web" below was read from that code or from `web/CLAUDE.md`.

## 1. Goal

Replace the Today and Plan tabs' placeholders with the real feature, on the existing API and with no contract change: Today shows the day's calories and macros as rings against the person's targets, with the day's meals editable in place (one-tap swap, portion change, add and clear snacks); Plan shows a Monday-to-Sunday week with the same editable day sections, daily and weekly totals, and "apply template"; and diet templates can be listed (Mine, and the partner's), created, edited with autosave, shared, copied and deleted.

Success: `swift test` passes for the pure modules (dates, ring progress, week totals, template draft), the plan and template caches, the repositories, the view models and the shared autosave engine, with every existing `MealEditorViewModel` test still passing unchanged; one XCUITest flow passes against the real API (§11); `make check` stays green and both `ios.yml` jobs pass.

## 2. Decisions

"Brainstorm" rows were settled with the user. "Derived" rows follow from the code and the iOS spec and are the ones to challenge at review.

| Question | Decision | Source |
|---|---|---|
| Scope | Full web parity: Today, week plan and apply, template list (Mine and Partner's) with copy, template editor, meal picker. | Brainstorm |
| Plan cache | Per-day: one SwiftData row per date holding that day's `DailyTotal`, plus one targets row. Today and Plan read the same rows, so a swap on Today is already on Plan. | Brainstorm |
| Writes | Straight to the API, no optimistic cache change, no queue (iOS spec §6). Totals always come from `GET /plan`, never from the device. | Derived from iOS §6 |
| View models | One `PlanViewModel`, parameterised by the visible date range, drives both screens: Today is a one-day range, Plan a week. | Derived |
| Write then read | A write (`PUT`/`DELETE` of an entry, apply) and the refresh that follows are separate steps in the view model, so a failed refresh after a good write is reported as exactly that. | Derived |
| Templates cache | Same shape as `MealCache` (Mine and Partner's scopes, summary rows, cached detail). Detail is stored as the generated `DietTemplate` JSON, because nothing queries inside it. | Derived |
| Autosave | The meal editor's engine is extracted into a shared `Autosaver` and used by the template editor too. Extracted first, in its own commit, under the existing editor tests. | Derived |
| Partner templates | Listed and viewable read-only, with Copy; never applied (`404`): the person copies first, then applies their copy. Segment shown only while `GET /partner` is `active`. | Derived from `web/CLAUDE.md`, the Meals rule |
| Targets | Read from `GET /plan`; a null target shows the amount only and a line pointing to Profile. Setting targets stays in the Shopping and Profile plan. | Derived (as web at the same stage) |
| Apply start date | Defaults to the first day of the week being shown. | Derived |
| Presentation | Meal picker, portion, apply, new template and template editor are sheets; the template list is pushed on the Plan stack (iOS §9). | Derived |
| Date and number text | Fixed en-US output; dates are the local calendar day as `YYYY-MM-DD`, never UTC; weeks start Monday. | Derived (as web, and as Meals) |

## 3. Scope

In: Today (rings, slot rows, swap, portion, remove, add and clear snacks), week plan (navigation, totals, the same slot rows), apply template with the overwrite confirmation, template list (Mine, Partner's), new template, template editor with autosave, share toggle, delete, copy, the meal picker, and extending sign-out cache clearing to the two new caches.

Out, and where it goes: setting targets (Shopping and Profile plan); a month view, copying a day, reordering slots (web has none); opening a meal from a slot (web links out; iOS leaves it to the Meals tab); editing a single snack (the API cannot address one); changing a template's day count after creation (the API forbids it); offline writes (iOS §6); localised formatting.

## 4. Module structure

No new SPM targets; the Meals graph is unchanged (`API ◄── Persistence`, `API, Persistence ◄── Repositories`, `API, Auth, Repositories ◄── Features`). New code:

- `Persistence/`: `PlanCache` and `TemplateCache` (`@ModelActor`s), their internal `@Model` classes, and `CacheStore` creating the new models in the same container.
- `Repositories/`: `PlanRepository`, `TemplatesRepository`, `PlanError`, `TemplatesError`. The meal picker reuses `MealsRepository` and the Partner segment reuses `PartnerRepository`.
- `Features/Shared/`: `LocalDay` (dates), `NutrientMath` (sum and scale), `TargetProgress`, `RingView`, `Autosaver`, `SlotRowView`, `MealPickerView`.
- `Features/Today/`, `Features/Plan/` (`PlanView`, `WeekSummaryView`, `ApplyTemplateView`) and `Features/Plan/Templates/` (`TemplatesView`, `NewTemplateForm`, `TemplateSheetView`, `TemplateEditorView`, `TemplateReadOnlyView`, `TemplateDraft`, view models).
- `RootView` builds `PlanDependencies` (plan, templates, meals, partner) beside `MealsDependencies`, and its `clearCaches` clears every cache.

## 5. Data layer

### 5.1 Plan cache and repository

`PlanCache`: `CachedPlanDay` (`date` unique, `json: Data` holding the generated `DailyTotal`, which carries the day's entries and `nutrition_per_day`) and a single `CachedTargets` row (the generated `Targets` as JSON). It takes and returns generated value types and never exposes a `@Model`.

- `days(from:to:) -> [DailyTotal]`: the cached dates in the range, in date order. A date never fetched is absent, not empty.
- `targets() -> Targets?`.
- `replace(days:targets:)`: upserts each given date and the targets in one save, touching no other date. `GET /plan` returns every date in `[from, to]`, so a refresh replaces exactly that range.
- `clearAll()`. Rows are never pruned (a year of days is a few hundred small rows).

`PlanRepository` (`Sendable` struct, `AuthRepository` shape, every call through `unwrapping`):

- `cached(from:to:) async -> PlanSnapshot` (`days`, `targets`), and `refresh(from:to:) async throws`: `GET /plan`, then `replace`. A failure leaves the cache untouched.
- `setEntry(date:slot:mealID:portion:) async throws` (`PUT /plan/{date}/{slot}`; for a snack it adds one) and `clearSlot(date:slot:) async throws` (`DELETE`; for a snack it removes every snack that day). Writes only: they touch no cache.
- `apply(templateID:startDate:overwrite:) async throws` (`POST /diet-templates/{id}/apply`, `204`). `409 plan_conflict` throws `PlanError.conflict`; `404` throws `notFound` (also what a partner's template answers).
- `PlanError`: `notFound`, `conflict`, `validationFailed(String)`, `unauthorized`, `rateLimited`, `server(String)`.

### 5.2 Templates cache and repository

`TemplateCache` mirrors `MealCache`: `CachedTemplate` (id unique, `scope` mine/partner, the `DietTemplateSummary` fields, optional detail: `isOwner` and the full `DietTemplate` JSON). `replaceSummaries(_:scope:)` upserts keeping detail and deletes only that scope's absent ids; `store(_:)`, `remove(id:)`, `clear(scope:)`, `clearAll()`.

`TemplatesRepository`: `cachedTemplates(scope)`, `refreshTemplates(scope)` (walks every page at limit 100, replaces the scope atomically; `404 partner_not_linked` clears the partner scope and throws `partnerNotLinked`), `cachedTemplate(id)`, `refreshTemplate(id)` (`404` removes the entry), `create(name:dayCount:)`, `update(id:_:)` (name and sharing only), `replaceSlots(id:_:)`, `delete(id:)`, `copy(id:)` (each returns the full `DietTemplate`, which replaces the cache entry), `clearPartnerTemplates()`, `clearCaches()`. `TemplatesError`: `notFound`, `partnerNotLinked`, `validationFailed(String)`, `unauthorized`, `rateLimited`, `server(String)`.

### 5.3 Read path and writes in the view model

`PlanViewModel.load()` assigns `cached(from:to:)` at once, then awaits `refresh`, then re-reads. A refresh failure keeps what is shown and marks it stale ("Offline: showing saved plan"); with nothing cached it shows an error state with Retry. Triggers: the screen appears, pull to refresh, scene becomes active, after any write.

A write (swap, portion, remove, clear snacks, apply) runs `repository write`, then `refresh` of the affected dates (one date, or the applied range). While the pair runs, that range's rings and totals are dimmed and the day's slot actions are disabled (one write at a time per `PlanViewModel`). A write that fails shows its error and refreshes nothing. A write that succeeds but whose refresh fails shows "Saved, but the totals could not be refreshed. Pull to refresh." and marks the view stale: the write is not repeated.

### 5.4 Dates

`LocalDay` wraps a `Calendar(identifier: .gregorian)` in the device's current time zone, with an injected clock. Days are `YYYY-MM-DD` strings (the API's `date`); arithmetic uses `Calendar.date(byAdding: .day)`, so a week stays seven days across a daylight-saving change. `startOfWeek` is Monday. Text: weekday-and-date headings and the week range ("Oct 5 – 11", "Oct 26 – Nov 1") in fixed en-US. Today's view model re-reads the day whenever the scene becomes active and on `UIApplication.significantTimeChangeNotification` (behind `#if os(iOS)`), so it is correct after midnight; the range moves only when the date actually changed.

### 5.5 Session handling

Nothing new in `AppState`. `RootView`'s `clearCaches` closure now clears the plan and template caches as well as the meal cache, so a second user on the same device never sees the first user's plan.

## 6. Today

A `NavigationStack` root `TodayView` over a `PlanViewModel` whose range is today.

- **Header and rings.** The long date, then four `RingView`s (calories, protein, carbohydrates, fat) from the day's `nutrition_per_day` against `targets`. Each ring shows the amount and, when the target exists and is positive, the real percent (125% when over) with the ring filled to at most 100%. An unknown amount shows "—" and an empty ring (never 0); a missing target shows the amount only. If any of the four targets is missing, one line reads "Set daily targets in Profile." VoiceOver: "Calories, 520 of 2,000 kilocalories, 26 percent" or "Calories, 520 kilocalories, no target"; unknown is "no data".
- **Slot rows** (`SlotRowView`, shared with Plan) in this order: Breakfast, Lunch, Dinner, Snacks. A filled single slot shows the meal name and the portion ("× 1.5" unless 1). Tapping it opens the `MealPickerView` to swap. A menu (swipe actions and a context menu) offers "Change portion" and "Remove". An empty slot shows "Add meal". Snacks list their entries read-only, with "Add snack" (opens the picker; the new entry has portion 1) and "Clear snacks" (confirmed: "Clear all snacks? This removes every snack planned for <day>."). A single snack cannot be edited or removed because the API cannot address it.
- **Portion sheet.** One decimal field using `parseDecimal`; more than 0 and at most 100 ("Portion must be more than 0 and at most 100."). Saving is a `PUT` with the slot's current meal and the new portion.
- **Dimming.** While a write and its refresh run, the rings and the day's total are dimmed (`opacity` and a progress mark; reduced motion applies), as web's `aria-busy`.
- **Offline.** The cached day is shown, marked stale; writes need connectivity and fail with the usual message.

## 7. Plan

Root `PlanView` over a `PlanViewModel` whose range is the shown week.

- **Week bar.** Previous and next week, "This week" (disabled on the current week), and the range label. Changing week sets the range and loads it.
- **Summary.** A `WeekSummaryView`: the week's total calories and macros and the per-day average (total ÷ 7, as web), from `NutrientMath.sum` of the server's per-day totals, where a nutrient unknown on any day is unknown for the week.
- **Day sections.** One per date: heading (weekday and date, "Today" marked), the day's calories and macro line (`NutritionFormat`), then the same four slot rows and actions as Today.
- **Toolbar.** "Diet templates" pushes `TemplatesView`; "Apply template" opens `ApplyTemplateView`.
- **Apply sheet.** A picker over my templates only (`TemplatesRepository.cachedTemplates(.mine)` then refresh), a start date defaulting to the first day of the shown week, and "Apply". On `PlanError.conflict` an alert asks "Replace the meals already planned? Days from <start> already have meals in these slots." with "Replace" and "Cancel"; Replace repeats the call with `overwrite: true`. Snack slots are always added by the API and are never in conflict. After success the applied range (`start` to `start + day_count − 1`) is refreshed and the sheet closes.
- **Meal picker** (`MealPickerView`): a sheet over `MealsRepository` (`cachedMeals(.mine)`, then `refreshMeals(.mine)`), filtered locally as web does, over all my meals; tap to choose; an empty library says "You have no meals yet. Create one in the Meals tab."

## 8. Templates

`TemplatesView` is pushed from Plan's toolbar. It has the Mine / Partner's control under the Meals rule (shown only while `PartnerRepository.status()` is `active`; a pending invite hides it; offline it shows only if partner templates are cached; a `404 partner_not_linked` hides it and clears the partner cache). Rows show the name, "N days" and a "Shared" badge on my shared templates; partner rows have "Copy to my library" (swipe) and open read-only. Empty states: "Create your first template" for Mine, "Templates your partner shares with you show up here, and you can copy them into your library." for Partner's. Toolbar "New template".

- **New template** (sheet): name (1–200) and day count (1–31, default 7), both required by `createDietTemplate`. On success the same sheet becomes the editor.
- **Editor** (`TemplateEditorView`, one sheet like Meals' `MealSheetView`, which shows the editor or the read-only view by the loaded template's `isOwner`): a status line, name, a "Share with my partner" toggle (when the partnership is active or the template is already shared), then one section per day `1…day_count`, each with Breakfast, Lunch and Dinner (one meal and a portion, or empty) and Snacks (any number). Tapping a slot opens the `MealPickerView`; a new slot has portion 1; a portion field accepts more than 0 and at most 100; swipe removes a slot. Choosing a meal for an occupied non-snack slot replaces it. A destructive "Delete template" (confirmed: "Delete this template? "name" will be removed from your library. This can't be undone."). The day count is shown, not editable.
- **Read-only** (a partner's template): the same days and slots without controls, plus "Copy to my library": `copyTemplate`, close the sheet, refresh Mine, present the copy in the editor (the API copies the partner's meals the template uses). It cannot be applied; the apply sheet lists my templates only.
- **Delete** shares one path (`TemplatesViewModel.delete(id:)`) between the editor button and the list swipe; both return their error text to the caller so a sheet can show its own alert (as Meals).

## 9. The template editor: draft, validation, autosave

**`TemplateDraft`** (pure): the editable text of a template (name, shared flag, slots as rows of `dayIndex`, `slot`, meal id and name, portion text) plus `validate()` and `changes(from:to:)`. Limits: name trimmed 1–200; each portion more than 0 and at most 100; each `dayIndex` within `0..<dayCount` (the UI only offers those); non-snack slots unique per day. `changes` returns a `PATCH` body of changed name/sharing and the full ordered slot list when any slot changed (an empty list clears all slots, so removing the last slot still writes). Messages: "Give the template a name.", "Use at most 200 characters.", "Enter a portion.", "The portion must be more than 0 and at most 100."

**`Autosaver<Value: Equatable & Sendable>`** (`@MainActor`, extracted from `MealEditorViewModel`): holds the debounce task, `isSaving`, `saveError` and the "tried value" bookkeeping. The client gives it `pending: () -> Value?` (the valid value that differs from what the server holds) and `perform: (Value) async throws -> Void`; it provides `schedule()`, `flush()`, `retry()` and takes the injected `sleep`. Rules unchanged from Meals §7: saves 700 ms after the last edit; one at a time; an edit during a save is saved after it; a tried value (saved or failed) is not retried by itself; leaving the editor flushes in an unstructured task. Each editor's `perform` keeps its own partial-commit logic: fields (`PATCH`) then list (`PUT`), each step committed on its own. The status line, banner and dimming match the meal editor. The refactor lands first and the existing `MealEditorViewModelTests` must pass unmodified.

## 10. Error handling

Follows iOS §11 and Meals §10; Plan specifics:

| Case | Behaviour |
|---|---|
| Read fails, cache present | Keep the cache, mark stale. |
| Read fails, no cache | Error state with Retry. |
| Write fails | Error alert or inline text; nothing is refreshed; tapping again retries. |
| Write ok, refresh fails | "Saved, but the totals could not be refreshed. Pull to refresh."; stale mark. |
| `409 plan_conflict` on apply | The Replace / Cancel confirmation (§7); never shown for snacks. |
| `404` opening my template (deleted elsewhere) | Remove from cache, close the sheet, "This template isn't available anymore." |
| `404` opening a partner template | Same; on `partner_not_linked` also hide the segment and clear the partner cache. |
| `400 validation_failed` | Mapped from the per-field `errors` array to text, as Meals; local validation prevents most. |
| `429` | "Too many requests. Please wait a moment and try again." |

Unmapped codes fall back to the problem's `title`.

## 11. Testing

- **Swift Testing, pure**: `LocalDay` (Monday weeks, month and year boundaries, a daylight-saving week is seven days, formatting); `TargetProgress` (never NaN or infinite, over-target fills to 100% but keeps the real percent, null or non-positive target is `nil`); `NutrientMath` (sum, a nutrient unknown on any day stays unknown, scale); week total and ÷ 7 average; `TemplateDraft` validate and `changes` at every limit, including the empty list.
- **Caches** against an in-memory container: `PlanCache.replace` touches only its dates, targets stored and updated, an unfetched date is absent, `clearAll`; `TemplateCache` scopes never collide, upsert keeps detail, `clearAll`.
- **Repositories** against `RoutingTransport`: range refresh replaces exactly its dates and a failure leaves the cache untouched; `PUT`/`DELETE` bodies and paths; `409 plan_conflict` and `404` on apply; the template page walk and partner `404`; each write's answer replaces the cache entry.
- **View models**: `PlanViewModel` (cache-first load, stale marking, write-then-refresh, write failure refreshes nothing, write ok plus refresh failure reports it, one write at a time, week navigation, the date rolling over); apply with the conflict then overwrite path; `MealPickerViewModel` filter; `TemplatesViewModel` (segment rule as Meals, delete returns its error); `TemplateEditorViewModel` on the shared engine (debounce coalescing, one save at a time, edit during save saved after, failed value not auto-retried, retry, flush on leaving, `PATCH`-then-`PUT` partial commit, invalid draft never saved, removing the last slot writes an empty list); the `Autosaver` itself; `AppState` clearing the new caches.
- **XCUITest, one flow, real API**: CI's database has no meals, so the test creates an account, a custom ingredient with calories and a meal containing it through the API, then signs in (the registration form's password field drops characters on CI, see `ios/CLAUDE.md`), adds the meal to Breakfast on Today, changes the portion to 2, and checks the calories ring shows double (the server computes it, so doubling proves the round trip), then opens Plan and finds the same meal on today's section. It applies Foundation's CI lessons: `typeText` only, `waitUntilHittable` before taps after a transition, 45 s waits, label fallback for tab-bar buttons, a fresh unique email per run.

## 12. Documentation changes

Made in the same change as this spec, to the iOS spec: §9 Today and Plan bullets point here and say the slot rows are shared and the week summary uses the per-day average; §15 plan 3 links this spec.

Done by the plan, not now: `ios/CLAUDE.md` gets the new layout (`PlanCache`, `TemplateCache`, `PlanRepository`, `TemplatesRepository`, `Autosaver`), the write-then-refresh rule, and the date rules (local day, Monday weeks, no UTC).

## 13. Out of scope and known gaps

Out of scope: everything under "Out" in §3.

Known, accepted for v1:

- A single snack cannot be edited or removed (API gap, §3).
- Setting targets arrives with Shopping and Profile; until then Today's rings show amounts only when a target is missing.
- Reconnecting without foregrounding does not trigger a refresh; pull to refresh does.
- Plan cache rows are never pruned.
- Date and number output is fixed en-US, as web.
