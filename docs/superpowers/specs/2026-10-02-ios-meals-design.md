# iOS Meals Design

Date: 2026-10-02
Status: draft for review. Extends `2026-09-30-ios-app-design.md` (§3, §4, §6, §9, §11, §12, §15 plan 2), which extends the parent spec `2026-09-21-meal-planner-design.md`. Where this document and the iOS spec disagree on Meals, this document wins and the iOS spec is updated in the same change (§12). The shipped web Meals feature (`web/src/features/meals/`, `web/src/components/ingredient-search/`, `web/src/components/nutrition-panel.tsx`, `web/src/lib/use-autosave.ts`) is the parity reference; every "as web" below was read from that code.

## 1. Goal

Replace the Meals tab's placeholder with the real feature, on the existing API and with no contract change: search the shared ingredient catalogue; build and edit meals in an editor that autosaves and shows the server's nutrition per serving; create a custom ingredient when search finds nothing; and, once partnered, browse the partner's shared meals read-only and copy one into the user's own library. Meals are also the first screens that make authenticated calls, so this plan finishes the session handling Foundation deliberately deferred (§5.6).

Success: `swift test` passes for the pure modules (draft validation and diff, number parsing, nutrition formatting), the cache actor, the repositories and the view models, including the autosave rules in §7; one XCUITest flow passes against the real API (§11); `make check` stays green and both `ios.yml` jobs pass.

## 2. Decisions

"Brainstorm" rows were settled with the user. "Derived" rows follow from the code and the iOS spec and are the ones to challenge at review.

| Question | Decision | Source |
|---|---|---|
| Editor save model | Autosave 700 ms after the last edit, no Save button, same rules as web's `useAutosave` (§7). | Brainstorm |
| Plan scope | Mine and Partner's tabs, read-only partner view and Copy, all in this plan. | Brainstorm |
| Ingredient search | Modal sheet: search field, results, tap to add. Empty text browses alphabetically. | Brainstorm |
| Custom ingredients | The creation form reachable from search is in this plan. Listing, editing, deleting and the full 18-nutrient editor stay in the Profile plan (iOS §9, §15 plan 4). | Brainstorm (creation); derived (boundary) |
| Nutrition display | As web: four macro tiles always visible, expandable "All nutrients" tier with micronutrients against FDA Daily Values. | Brainstorm |
| Domain types | The generated `Components.Schemas.*` types are the domain types, as Foundation does for `User`. No parallel hand-written model. | Derived |
| Cache isolation | One `@ModelActor` cache per domain. `@Model` objects never leave it; it takes and returns generated value types. | Derived |
| Read path | A repository exposes `cached…()` and `refresh…()`. The view model renders the cache, awaits the refresh, re-reads. No streams or Combine (no precedent in the codebase). | Derived |
| List refresh | Walks every page (limit 100) and replaces the scope's cached summaries in one transaction. No pagination UI. | Derived |
| Presentation | New-meal form, editor, read-only view, ingredient search and custom-ingredient form are sheets (iOS §9). | Derived from iOS §9 |
| Partner segment | Shown only while `GET /partner` returns `status: active`. A pending invite also hides it. | Derived from the contract (corrects iOS §9) |
| Number formatting | Fixed en-US output and a locale-independent input rule, as web. | Derived |
| Cache migrations | None. The cache is rebuildable from the API, so an unopenable store is deleted and recreated. | Derived |

## 3. Scope

In: meal library (Mine, Partner's), new meal, editor with autosave and live nutrition, delete, ingredient search, custom-ingredient creation, read-only partner meal view, copy, and the session handling in §5.6.

Out, and where it goes: using meals in plans and templates (Plan and Today plan); custom-ingredient list, edit and delete, and the "mine only" filter (Profile plan; web keeps it there too); reordering ingredient rows (web has none); searching or filtering the meal list (web has none); offline writes (iOS §6); localised number formatting.

## 4. Module structure

Two new SPM targets, the `Persistence/` and `Repositories/` folders the iOS spec §4 already lists:

```
API ◄── Auth
API ◄── Persistence          (SwiftData @Model classes, cache actors, container factory)
API, Persistence ◄── Repositories      (MealsRepository, IngredientsRepository, PartnerRepository)
API, Auth, Repositories ◄── Features
all of the above ◄── AppCore
```

`Repositories` imports `API` only, like `AuthRepository`: it maps the generated response enums into typed errors, so nothing above it sees an HTTP status (`ios/CLAUDE.md`). New code in `Features`:

- `Features/Shared/`: `Nutrition/` (catalog, daily values, formatting, `NutritionPanelView`), `ParseDecimal`, ingredient-category labels, `IngredientSearch/`, `CustomIngredient/`. Shared because the Plan and Profile plans reuse them, as web keeps them in `components/` and `lib/`.
- `Features/Meals/`: `MealsViewModel`, `MealsView`, `MealDraft` (pure), `MealEditorViewModel`, `MealEditorView`, `MealReadOnlyView`, `NewMealForm`.

## 5. Data layer

### 5.1 Repositories

Plain `Sendable` structs, constructor-injected with the generated `Client` and, where cached, the cache actor, one `async throws` method per operation, an exhaustive `switch` over the generated response enum into a typed error (the `AuthRepository` shape). `MealsError`: `notFound`, `partnerNotLinked`, `inUse`, `validationFailed(String)`, `rateLimited`, `server(String)`. Transport failures (`URLError`) propagate unchanged and are shown as "can't reach the server".

`MealsRepository`:

- `cachedMeals(scope)` and `refreshMeals(scope)`: `scope` is `mine` (`listMeals`) or `partner` (`listPartnerMeals`). Refresh follows `next_cursor` to the end at limit 100 and replaces the scope atomically; a failure on any page leaves the cache untouched. `404 partner_not_linked` clears the partner scope and throws `partnerNotLinked`.
- `cachedMeal(id)` and `refreshMeal(id)`: `getMeal`. A `404` removes the cache entry and throws `notFound`.
- `create(name:servings:)` (`createMeal`), `update(id:_:)` (`updateMeal`), `replaceIngredients(id:_:)` (`replaceMealIngredients`), `copy(id:)` (`copyMeal`): each returns the full `Meal` the API answers with, and that answer replaces the cache entry.
- `delete(id:)`: `204` removes the entry; `409 meal_in_use` (a diet-template slot or plan entry still references it, `backend/CLAUDE.md`) throws `inUse`.

`IngredientsRepository` (not cached; search is always live): `search(text:category:)` and `create(_:)`. With text, one request returns the best matches unpaginated; without, the first alphabetical page (limit 20). `PartnerRepository`: `status()` returns the `Partnership` or `nil` on `404 partner_not_linked`.

### 5.2 Cache

`MealCache` is a `@ModelActor` built from a `ModelContainer` created once at launch. Models:

- `CachedMeal`: id, `scope` (`mine`/`partner`), the `MealSummary` fields, and optional detail: `isOwner`, `nutritionPerServing` stored as JSON `Data` encoded with the generated type's own `Codable` (not 18 columns), and a `[CachedMealIngredient]` relationship (cascade delete). No detail means "summary only, not opened yet". `GET /meals` returns `MealSummary` (no `is_owner`, ingredients or nutrition), so ownership of a list row is its scope; `isOwner` exists only on the full `Meal`.
- `CachedMealIngredient`: the denormalised `MealIngredient` fields (ingredient name and category are embedded, so no join is needed to render from cache).

`replaceSummaries(_:scope:)` upserts by id, keeping existing detail, inserts new rows, and deletes ids absent from the fetched set within that scope only, in one save. Mine and Partner's never collide. The container factory deletes the store files and retries once if the store will not open: no migrations (§2).

Risk: `@ModelActor` under Swift 6.2 strict concurrency. If it fights back, the fallback is a `@MainActor` cache over `mainContext`; the API (value types in and out) is identical, so nothing above changes.

### 5.3 Read path

`MealsViewModel.load()` assigns `await repo.cachedMeals(scope)` immediately, then `try await repo.refreshMeals(scope)`, then re-reads. A refresh failure keeps what is shown and marks it stale (the small "offline" indicator, iOS §6); with nothing cached it shows an error state with Retry. Triggers: tab appears, pull to refresh, scene becomes active, after create, copy and delete. Opening a meal does the same with `cachedMeal` and `refreshMeal`.

### 5.4 Write path

Direct to the API, no optimistic update, no queue (iOS §6). The nutrition shown is always the API's last answer, never computed on device: a loaded meal carries no per-100 g data (as web).

### 5.5 Partner segment

`MealsViewModel` reads `PartnerRepository.status()` on appear. The segmented Mine / Partner's control shows only for `status == active`. If the request fails (offline) the segment shows only when partner meals are already cached, since they exist only after a link was active. A `404` from any partner-scoped read hides the segment and clears the partner cache (iOS §11).

### 5.6 Session handling carried over from Foundation

The Foundation handoff deferred these to "the first authenticated screens", which are these. All three are needed before Meals ships:

1. **Session ending mid-session.** A refresh the API rejects already clears the Keychain (`TokenRefresher`), but only the next launch notices. The refresher must notify `AppState` once (single-flight preserved) so the UI flips to signed-out immediately.
2. **Launch with a stored session while offline.** `restoreSession()` currently treats every failure as signed-out, which would bounce an offline user to the sign-in screen and defeat the cache. Only a definitive auth failure signs out; a transient failure (network, 429, 5xx) keeps the tokens and opens the shell in an unverified state whose tabs show cache, marked stale, and retry on foreground or reconnect. The plan names the new `AppState.Session` case and keeps Foundation's call sites compiling.
3. **Sign-out.** It flips state immediately, clears the Keychain and every cache, then finishes the server revoke in the background, instead of blocking on the network.

Whenever the session becomes signed-out by any path, `AppState` awaits an injected `clearCaches` closure (built in `RootView` from the cache actors), so a second user on the same device never sees the first user's meals.

## 6. Screens and flows

- **Meals tab**: `NavigationStack` root `MealsView`. Toolbar "New meal". The Mine / Partner's control per §5.5. Rows show name and servings, a "Shared" badge on own shared meals, and a "Copy to my library" action on partner rows. Empty states as web: a "Create your first meal" action for Mine, "Meals your partner shares with you show up here, and you can copy them into your library." for Partner's.
- **One sheet**: `MealsViewModel.presentation` (`.new`, `.edit(id)`, `.view(id)`) drives a single `.sheet(item:)`. A Mine row opens `.edit` and a partner row `.view`; once the meal loads, its `isOwner` decides which UI is shown, so an editor never appears for a meal the caller does not own.
- **New meal**: name and servings (starts at 1), both required by `createMeal`. On success the same sheet becomes `.edit(id)`, as web does after `/meals/new`.
- **Editor**: status line, name, servings, notes, a "Share with my partner" toggle (shown when the partner link is active or the meal is already shared), the ingredient rows (quantity field, `g`/`ml`/`piece` picker, swipe or button to remove, "Add ingredient"), the nutrition panel, and a destructive "Delete meal".
- **Read-only view**: the same content without controls, plus "Copy to my library": `copyMeal`, close the sheet, refresh Mine, present `.edit(copyId)` (as web opens the copy). The copy starts unshared and the API copies the custom ingredients it uses into the caller's library.
- **Delete**: the editor button and a list swipe action share one path, `MealsViewModel.delete(id)`, behind one confirmation ("Delete this meal? "name" will be removed from your library. This can't be undone."). A `meal_in_use` answer shows: "This meal is used in your plan or a diet template. Remove it there first."

## 7. The editor: draft, validation, autosave

**`MealDraft`** (pure, ported from `draft.ts`): the editable text of a meal (name, notes, servings string, shared flag, rows with quantity string and unit) plus `validate()` and `diff(saved:next:)`.

- Limits: name trimmed, 1 to 200 characters; notes at most 2000 (blank saves as `null`); servings more than 0 and at most 1000; at most 200 ingredients; each quantity more than 0 and at most 100000. A newly added ingredient starts at 100 g so it is valid the moment it appears.
- `diff` returns a `PATCH` body of changed fields (or none) and the full ingredient list when any row changed (or none).
- Messages as web ("Give the meal a name.", "Use at most 200 characters.", "Servings must be more than 0 and at most 1000.", "Enter an amount.", "The amount must be more than 0 and at most 100000.").

**`parseDecimal`**: a comma is a decimal point, blank means nothing entered, and only digits with at most one point are valid; signs, exponents, hex and stray text are invalid. It uses no `NumberFormatter`, so behaviour is identical on every device locale and deterministic in tests.

**Autosave** (a port of `useAutosave` and `meal-editor.tsx`, owned by `MealEditorViewModel`):

- Saves once the valid draft has stopped changing for 700 ms and differs from what the server holds. An invalid draft is never saved; the status says "Fix the highlighted fields to save."
- One save at a time. An edit made during a save is saved after it.
- A value that was tried, saved or failed, is not tried again by itself, so a failing server is not hammered. The next edit, "Try again", or leaving the editor tries again.
- One save writes the fields first (`PATCH`), then the ingredient list (`PUT`). Each step commits on its own: a failure in the second leaves the first counted as saved and only the rest is retried.
- Leaving the editor (sheet dismissed, or scene entering the background) flushes a pending edit in an unstructured task, so dismissing does not cancel it. A flush that fails after the sheet is gone is not surfaced; web has the same limit.
- The draft is copied from the meal once. Later refreshes feed only the nutrition panel, so a refresh never overwrites what is being typed.
- Status line: "Saving…", "Fix the highlighted fields to save.", "Unsaved changes", "All changes saved". The failure banner ("message · Try again") shows only while something is still unsaved; putting the draft back to what the server holds clears it. The panel dims while unsaved.
- The delay is an injected `@Sendable (Duration) async throws -> Void` (default `Task.sleep`), so tests drive time without a clock library.

Unit choices are not filtered per ingredient (as web). A `piece` or `ml` row for an ingredient without `grams_per_piece` or `density_g_per_ml` is rejected by `PUT` and appears through the failure banner.

## 8. Ingredient search and custom ingredient

**Search sheet.** Text field (trimmed, at most 100 characters), a category filter ("All categories" and the ten categories with web's labels, e.g. `dairy_eggs` is "Dairy and eggs"), 250 ms debounce. A row shows name, category and a "Custom" badge when `isCustom`; tapping adds it and dismisses. States as web: "Searching…" (previous results stay while typing), the problem message on error, "No ingredient matches." The footer row "Create a custom ingredient “text”" is always present. Browsing with empty text shows only the first alphabetical page, as web.

**Custom-ingredient form** (a sheet over the search sheet, name prefilled from the search text). Fields: name (1 to 200), category (default Other), calories (at most 1000), protein, carbohydrates and fat (each at most 100) per 100 g, grams per piece (more than 0, at most 10000), density in g/ml (more than 0, at most 3). Only entered nutrients are sent: an omitted one stays unknown, not zero, and the nutrition panel then shows "—" with its hint. Messages as web ("Give the ingredient a name.", "Enter a number, for example 12.5.", "That is more than N per 100 g."). On `201` the new ingredient is added to the meal as if picked and both sheets close; a `400` shows inline on the field it names, else as a banner (iOS §11).

## 9. Nutrition display

`NutrientCatalog` (the 18 nutrients in API order with label, unit and group Macronutrients, Minerals or Vitamins), `macroKeys` (calories, protein, carbohydrates, fat), the 13 FDA Daily Values web uses (fibre 28 g, saturated fat 20 g, sodium 2300 mg, potassium 4700 mg, calcium 1300 mg, iron 18 mg, magnesium 420 mg, zinc 11 mg, vitamin A 900 µg, vitamin C 90 mg, vitamin D 20 µg, vitamin B12 2.4 µg, folate 400 µg), and formatting, all ported from `web/src/lib/nutrition/`.

- `nil` renders "—", never 0. Amounts show no decimals for kcal or values of 10 and above, else up to one decimal (trailing zero trimmed), grouped en-US. A percentage is the amount over its Daily Value, rounded.
- `NutritionPanelView`: title "Per serving"; four macro tiles with each macro's consistent colour (exact tokens settled with `apple-skills:design` during the plan, iOS §10); the hint "— means some ingredients lack data for that nutrient, so the total is unknown rather than zero." when any value is unknown; a disclosure "All nutrients" revealing the three groups with amount and, where a Daily Value exists, the percentage, and the note "Percentages are of the FDA Daily Value for adults."
- VoiceOver reads each tile as label and value ("Calories, 420 kilocalories"; unknown as "Calories, no data"). Dynamic Type and reduced motion apply from the first view (iOS §10).

## 10. Error handling

Follows iOS §11; Meals specifics:

| Case | Behaviour |
|---|---|
| Read fails, cache present | Keep the cache, mark stale. |
| Read fails, no cache | Error state with Retry. |
| `404` opening own meal (deleted elsewhere) | Remove from cache, close the sheet, say it is gone. |
| `404` opening a partner meal (unshared, unlinked) | Same, "not available anymore"; hide the segment on `partner_not_linked`. |
| Autosave fails | Banner with the message and "Try again" while still unsaved (§7). |
| `400 validation_failed` | Mapped from the per-field `errors` array to text, as `AuthRepository.validationMessage`; local validation prevents most. |
| `409 meal_in_use` on delete | Alert (§6). |
| Create or copy fails | Inline error in the form, or an alert on Copy; tapping again retries. |
| `429` | "Too many requests. Please wait a moment and try again." |

Unmapped codes fall back to the problem's `title`.

## 11. Testing

- **Swift Testing, pure**: `parseDecimal` table; `MealDraft` validate and diff at every limit; nutrition formatting, Daily Value percentages, and catalog invariants (18 unique keys).
- **Cache** against an in-memory `ModelContainer` per test: upsert keeps detail; deletion is scoped to one scope; Mine and Partner's do not collide; `clearAll`; the unopenable-store fallback.
- **Repositories** against `StubTransport`: a multi-page walk ends in one atomic replace and a failing second page leaves the cache untouched; `404` and `409` mappings; each write's response replaces the cache entry.
- **View models**: `MealsViewModel` (cache-first load, stale marking, the partner-segment rule including a pending invite and the offline fallback); `MealEditorViewModel` with the injected sleep (debounce coalescing, one save at a time, edit-during-save saved after, failed value not auto-retried, retry, flush on leaving, `PATCH`-then-`PUT` partial commit, invalid draft never saved); `IngredientSearchViewModel` (debounce, previous results kept, category).
- **Session handling**: every sign-out path clears the caches; a refresh rejected mid-session flips to signed-out once; an offline launch keeps the session; a definitive `401` at launch signs out.
- **XCUITest, one flow, real API**: register, open Meals, New meal, Add ingredient, create a custom ingredient with calories, see its row and "All changes saved". CI's database has no ingredients (no `FDC_API_KEY`, iOS §12), so the flow makes its own through the UI, which doubles as the custom-ingredient coverage. It applies Foundation's CI lessons: `typeText` only, `waitUntilHittable` before taps after a transition, 45 s waits, label fallback for tab-bar buttons, a fresh unique email per run.
- The `apple:accessibility` audit stays at the end of the iOS work (iOS §12).

## 12. Documentation changes

Made in the same change as this spec, to the iOS spec:

- §4: `Persistence` and `Repositories` are SPM targets with the §4 graph above.
- §6: the container is created at launch and handed to the cache actors, not injected into the SwiftUI environment (views and view models never touch SwiftData); the read-path mechanism is `cached…()` then `refresh…()`; caches are cleared whenever the session ends.
- §9, Meals bullet: autosaving editor; ingredient search as a sheet; custom-ingredient creation here, management in Profile; the partner segment shows only for `status: active`. The "mine only" line is attributed to Profile's custom-ingredient management, not the meal ingredient search.
- §15 plan 2: adds custom-ingredient creation and the session handling in §5.6.

Done by the plan, not now: `ios/CLAUDE.md` gets the new layout, the cache and `@ModelActor` gotchas, and the Foundation CI lessons it lacks today: `typeKey` hardware-shortcut synthesis delivers nothing on any app launch after the first one in an `xcodebuild test` run (use `typeText`); a tab button's accessibility identifier can be absent after the first launch (match its label too); wait until an element is hittable, not merely present, after a screen transition; GitHub's macOS runners need generous waits and occasionally hang the UI job, which a rerun clears.

## 13. Out of scope and known gaps

Out of scope: everything under "Out" in §3.

Known, accepted for v1:

- Browse mode in the search sheet shows only the first alphabetical page (limit 20), as web. Add paging if it bites.
- A refresh walks every page. Revisit if a library reaches the hundreds.
- A flush that fails after the editor is gone is not surfaced, as web.
- Number output is fixed en-US, as web.
- `GET /ingredients` has no "mine only" filter (Profile plan works around it as web does).
