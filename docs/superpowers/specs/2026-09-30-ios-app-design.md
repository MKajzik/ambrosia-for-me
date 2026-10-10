# iOS App Design

Date: 2026-09-30
Status: draft for review. Extends `2026-09-21-meal-planner-design.md` (§2.5, §4.3, §5.1, §5.2, §5.4, §6, §7, §9 step 4). Where the two disagree on the iOS app, this document wins and the parent spec is updated in the same change. `2026-09-25-web-app-design.md` is the parity reference: iOS mirrors the web app's server-driven behaviour (optimistic check-off, version conflicts, SSE rules, snack addressing) wherever the platforms should feel the same, and departs only where SwiftUI/SwiftData or the App Store require it.

## 1. Goal

A native SwiftUI iOS app (iOS 26+) on the existing Go API, covering the five product areas from parent spec §5.1: Today, Plan, Meals, Shopping and Profile, plus sign-in. Shared shopping lists update live; the shopping list is also the one screen that works, and can be edited, while offline. The API contract (`openapi.yaml`) does not change for the iOS app except where §16 says so.

Success: `swift test` passes for view models and repositories (including offline-queue replay), and the XCUITest flows in §12 pass against the real API: sign in, build a meal, apply a template, generate a shopping list, check off a shared item and see it land, go offline and check off an item, come back online and see it sync.

Already locked by the parent spec: SwiftUI, iOS 26+, Liquid Glass, MVVM with `@Observable` and Swift 6 concurrency, `APIClient → Repository (network + SwiftData) → view model → view` layering, Keychain-held tokens, online-first with the API as source of truth, Swift OpenAPI Generator for the client.

## 2. Environment note (supersedes handoff assumption)

The iOS brainstorm handoff (`handoff/handoff-ios-brainstorm.md`) assumed development happened on a separate, Linux/WSL2 machine that could not compile Swift, so plans would need to be handed to a Mac session or the user. That assumption no longer holds: this session runs directly on macOS 26.1 with Xcode 26.3 and Swift 6.2.4 installed, in the same repo checkout. Plans are written for **native execution in this session** — `swift build`, `swift test` and `xcodebuild` run here directly, no cross-machine handoff step is required. XcodeGen is not yet installed; installing it (Homebrew) is a Foundation-plan setup step.

## 3. Decisions

| Question | Decision |
|---|---|
| Xcode project structure | **XcodeGen** (`ios/project.yml`) generates a thin `MealPlanner` app target (just the `@main App` and asset catalog). Nearly all code lives in a local Swift package, `ios/MealPlannerKit`, built and tested with `swift test` independent of Xcode. Nothing hand-edits a `.pbxproj`; regenerating the project from `project.yml` is a normal, reviewable step. |
| API client | **Swift OpenAPI Generator** (build-plugin mode) generates `Types`/`Client` from the root `openapi.yaml` (3.0.3, already compatible) into `MealPlannerKit`. Drift fails `make check-generated-ios`, matching the web/backend pattern. |
| Cache breadth | SwiftData caches all five product areas (meals, diet templates, plan/today, shopping lists, profile) for instant launch and background refresh. **Only the shopping list supports offline writes** (queued intents); the rest require connectivity to write but keep showing the last successful read, marked stale, when offline. |
| Offline queue | A SwiftData `QueuedIntent` row per pending change (`check`, `uncheck`, `add`, `remove`), drained in order by a `ShoppingSyncEngine` actor. Checking collapses to one row per item holding the latest desired state (last-write-wins, matching the API's unversioned checked-only `PATCH`); an `add` cancelled by a later `remove` before either syncs is dropped locally without a network call. |
| Realtime | `URLSession.bytes(for:)` streaming `GET /shopping-lists/{id}/events` with the bearer in `Authorization` directly (iOS has no `EventSource`, so none of the web's proxy/EventSource workaround is needed). Same client rules as web: ignore an event at or below the cached version, refetch on a version gap, on `list_changed`, on foreground and on reconnect; treat `list_deleted`/`404` as access lost. |
| Sign in with Apple | Deferred to its own plan (parent spec §9, handoff decision 2), after backend `/auth/apple` exists. Foundation ships email/password only. |
| Visual direction | Liquid Glass materials, SF Symbols, system typography, Dynamic Type (parent spec §5.2/§5.4). Exact spacing/type/motion choices are settled per feature plan with `apple-skills:design`, mirroring the web app's "palette settled at implementation" approach. |
| Testing | Swift Testing for view models and repositories, including deterministic offline-queue replay; XCUITest for sign-in, meal editing and shopping check-off against the real API; `apple:accessibility` audit before the iOS app ships. |
| CI | New path-filtered `ios.yml` on a macOS GitHub Actions runner. Unit tests (`swift test`) run every time; a separate job builds the Go API and a Postgres service container to run XCUITest against the real backend, mirroring the web Playwright job. Client regeneration is checked for drift like the Go and TS clients. |
| Build order | Unchanged from the handoff: (1) Foundation, (2) Meals, (3) Plan and Today, (4) Shopping and Profile, (5) Sign in with Apple. |

## 4. Project structure and build

```
ios/
├── project.yml              # XcodeGen spec: app target + UI test target + package dependency
├── MealPlanner/              # thin app target: @main App, Assets.xcassets, Info.plist
├── MealPlannerUITests/       # XCUITest target (needs an app bundle to launch; can't live in the SPM package)
└── MealPlannerKit/           # local SPM package: almost all app code
    ├── Package.swift         # depends on swift-openapi-generator, swift-openapi-urlsession
    ├── Sources/
    │   ├── API/              # generated Types + Client (OpenAPI plugin output) + APIClient wrapper
    │   ├── Auth/             # Keychain token storage, refresh single-flight actor
    │   ├── Persistence/      # SwiftData schema, per-domain cache actors, ModelContainer factory
    │   ├── Repositories/     # one per domain: Meals, DietTemplates, Plan, ShoppingLists, Partner, Profile
    │   ├── Sync/             # ShoppingSyncEngine (offline queue), ListEventStream (SSE)
    │   ├── Features/         # {Today,Plan,Meals,Shopping,Profile}/{ViewModel,View}
    │   └── App/              # RootView (TabView), AppState, DI wiring
    └── Tests/MealPlannerKitTests/     # Swift Testing: VMs, repositories, queue replay
```

`xcodegen generate` produces `MealPlanner.xcodeproj`, which is gitignored; only `project.yml`, `MealPlannerUITests/` sources and the package are committed. XCUITest needs a real app to launch, so its target is defined in `project.yml` against the `MealPlanner` app target, not inside the SPM package — `swift test` alone never runs it, only `xcodebuild test` does. CI runs `xcodegen generate` before `xcodebuild`. `swift build` / `swift test` work on `MealPlannerKit` without Xcode at all, which is what CI's unit-test job and local iteration use day to day. `Persistence` and `Repositories` are SPM targets alongside `API`, `Auth`, `Features` and `AppCore`; their dependency graph is in `2026-10-02-ios-meals-design.md` §4.

## 5. Networking and auth

- The OpenAPI plugin generates `Types` and `Client` structs against `openapi.yaml`, same source of truth as web and backend.
- `APIClient` wraps the generated `Client` with a `ClientMiddleware` that attaches the bearer access token and, on a `401`, triggers a refresh and retries the request once. A `TokenRefresher` actor single-flights concurrent refreshes and keeps only the newest token pair, mirroring the backend rule that replaying a used refresh token revokes the whole session family (`backend/CLAUDE.md` §Behaviour) and the web BFF's single-flight (`web/CLAUDE.md` §BFF rules) — same problem, solved on-device here since iOS talks to the API directly rather than through a BFF.
- Access and refresh tokens live in the Keychain (`kSecClassGenericPassword`, `kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly`), never in `UserDefaults` or SwiftData.
- Sign-out and a session the API reports as over (refresh itself fails) both clear the Keychain and reset `AppState` to signed-out, which SwiftUI's view hierarchy reacts to directly (no full relaunch needed, unlike the web app's forced page load, since there is no shared in-memory server cache to worry about racing).

## 6. Persistence and caching

- One `ModelContainer` for the whole app, created at launch and handed to the per-domain cache actors (`2026-10-02-ios-meals-design.md` §5.2). Views and view models never see it, so it is not injected into the SwiftUI environment.
- Each repository (`MealsRepository`, `PlanRepository`, `ShoppingListsRepository`, etc.) is the only code that touches both the generated `Client` and SwiftData for its domain; view models never see either directly.
- Read path: return the cached SwiftData snapshot immediately (instant launch), then fetch from the API in the background and update the cache and the view model when the fetch lands. A fetch failure with no cached data shows an empty/error state; a fetch failure with cached data keeps showing the cache, marked stale (small "offline" indicator, matching the web app's "404 replaces, other failures keep cache" split from `web/CLAUDE.md`, generalized to iOS's own read failures). The mechanism is two repository calls, `cached…()` then `refresh…()`, with the view model re-reading in between (Meals spec §5.3).
- Write path (everything except shopping items): call the API directly; a failure surfaces an error and does not mutate the cache optimistically, since there is no offline queue for these domains. This is deliberately simpler than shopping: the parent spec only asks for offline support on the shopping list (§5.4), and non-shopping writes are rare enough on a phone (editing a meal, applying a template) that requiring connectivity is an acceptable, YAGNI-respecting limit for v1.
- Shopping items are the one write path with an offline queue — see §7.
- Every cache is cleared whenever the session ends, by any path, so a second user on the device never sees the first user's data (Meals spec §5.6).

## 7. Shopping offline queue and sync

- `QueuedIntent` (SwiftData model): `id`, `sequence` (monotonic), `kind` (`check`/`uncheck`/`add`/`remove`), `listId`, `itemId` (server id, or nil for a not-yet-synced `add`), `clientTempId` (for an `add` before the server assigns a real id), and a payload for `add` (name, quantity, unit, category).
- `ShoppingSyncEngine` actor drains the queue in `sequence` order whenever the app has connectivity and the queue is non-empty (on launch, on reconnect via `NWPathMonitor`, and after each successful list mutation while online). One request in flight at a time per list, so ordering matches the parent spec's requirement (§4.3).
- Collapsing rules match the parent spec exactly: a `check`/`uncheck` intent replaces any earlier pending check intent for the same item (last-write-wins, and the API's checked-only `PATCH` is itself last-write-wins and unversioned, so this never conflicts server-side); an `add` followed by a `remove` for the same still-unsynced `clientTempId` cancels both out of the queue with no network call; a `remove` for an already-synced item is sent once.
- Non-check edits (name/quantity/unit/category) go through the versioned `PATCH` and are **not** queued offline — the parent spec's offline story is check/uncheck/add/remove (§4.3); editing a field is rare enough on a phone that it can simply require connectivity, same YAGNI reasoning as §6. A `409 version_conflict` while online refetches the item and restarts the edit sheet on the current version, exactly like the web app (`web/CLAUDE.md` §Gotchas).
- The Shopping tab shows a syncing indicator whenever the queue is non-empty, and item rows render optimistically from SwiftData regardless of queue state, so a checked-off item looks checked immediately whether online or not.
- Reconnect is detected with an `NWPathMonitor` wrapper. A `check`/`uncheck` for a not-yet-synced `add` keeps its own row (the create request has no `checked`) and is rewritten to the server id after the add. Drain outcomes: `404` drops the row, `400`/`409` drops it with a notice, network/`429`/`5xx` stops and retries on the next trigger. Details: `2026-10-08-ios-shopping-design.md` §4.

## 8. Realtime (SSE)

- `ListEventStream` opens `URLSession.bytes(for:)` against `GET /shopping-lists/{id}/events` with the bearer token in `Authorization`, parses `text/event-stream` frames, and republishes them as an `AsyncStream` the repository consumes.
- On each event: ignore it if its version is at or below the cached item's version (usually the device's own change echoing back); otherwise refetch the list. Also refetch on stream (re)open, on a detected version gap, on `list_changed`, on scenePhase becoming `.active`, and on `NWPathMonitor` reporting reconnect — identical triggers to the web app's `useListEvents` (`web/CLAUDE.md` §Gotchas), so the two clients converge the same way.
- `list_deleted` (or a `404` from the follow-up refetch) is treated as access lost: the list view shows "not available," matching the web app's handling of an unlinked/unshared/deleted list.
- The stream is owned by `ShoppingListsRepository` for whichever list is currently open, started when its detail view appears and cancelled when it disappears.

## 9. App structure

- `RootView`: a `TabView` with five tabs — Today, Plan, Meals, Shopping, Profile — each wrapping its own `NavigationStack` (parent spec §5.4). Meal and ingredient editors, the template editor, and item edit/add dialogs are sheets.
- `Features/Today`: rings for calories and the three macros against `GET /plan`'s targets, one-tap meal swap and portion change, "Add snack"/"Clear snacks" for the multi-row slot (mirrors the web app's snack handling, since the API addresses a snack only by date+slot, not individually — `web/CLAUDE.md` §Gotchas). Details: `2026-10-03-ios-plan-today-design.md`.
- `Features/Plan`: week calendar (Monday to Sunday), apply-template flow with the `409 plan_conflict` → confirm-overwrite dialog, daily totals and a weekly total with per-day average, sharing the Today slot rows; diet templates (Mine and Partner's lists, autosaving editor, copy) are pushed from its toolbar. Details: `2026-10-03-ios-plan-today-design.md`.
- `Features/Meals`: shared ingredient search sheet (type-ahead, category filter, "create custom ingredient" fallback — same shape as the web app's, parent spec §5.1), meal library with "Mine"/"Partner's" segments, an autosaving editor (no Save button, as web) with a live nutrition panel. A partner's meal is read-only with a "Copy to my library" action; the Partner segment shows only while `GET /partner` returns `status: active` (a pending invite, like `404 partner_not_linked`, hides it). Details: `2026-10-02-ios-meals-design.md`.
- `Features/Shopping`: lists grouped by aisle category, check-off, quick-add, generate-from-date-range, the sync indicator from §7, a partner badge on shared lists. Details: `2026-10-08-ios-shopping-design.md`.
- `Features/Profile`: targets (four optional numbers, same blank-clears / zero-calories-refused rule as web), partner connection (invite code display/entry, unlink), custom ingredient management (sends all 18 nutrients on update, since the API replaces the whole set — `backend/CLAUDE.md` §Behaviour), account deletion. Deletion asks for the account's email to be typed in the UI even though the API needs no re-authentication for `DELETE /me`, mirroring the web app's belt-and-suspenders pattern (`web/CLAUDE.md` §Gotchas).
- The Profile screen's custom-ingredient management filters "mine only" client-side after fetching all pages, same documented gap and workaround as the web app (`GET /ingredients` has no owner filter — parent spec known gap, web spec §11). The meal ingredient search has no such filter (it shows a "Custom" badge instead), as web.

## 10. Visual direction

Liquid Glass materials, SF Symbols, system typography and native Dynamic Type, matching parent spec §5.2/§5.4: the two clients feel like one product without imitating each other pixel-for-pixel. Each macro keeps its one consistent colour, matched against the web app's token values where SF Symbols/system colors allow a direct equivalent. VoiceOver labels, Dynamic Type and reduced motion (`UIAccessibility.isReduceMotionEnabled`) are required from the first feature plan, not retrofitted. Exact spacing, type scale and motion timing are settled per feature plan with `apple-skills:design`, consistent with how the web app deferred its own token values to implementation.

## 11. Error handling

- The generated client's error responses decode into a typed `ProblemDetail` (RFC 9457 shape) exposed as `APIError` with a stable `code`, mirroring the web app's `ApiError`/`problemMessage` (`web/CLAUDE.md` §Gotchas). View models map known codes to user-facing text; an unmapped code falls back to the problem's `title`.
- A `409 version_conflict` on a shopping item refetches the current item and restarts the edit sheet on it (§7). A `409 plan_conflict` on applying a template surfaces the overwrite confirmation (§9). A `404` on a partner-scoped resource (`partner_not_linked`, or an unlinked partner's resource) hides that UI rather than showing an error, matching the "unseen resources return 404" sharing rule (root `CLAUDE.md`).
- A network failure on a non-shopping write (§6) shows a plain retry-able error; a network failure on a shopping intent enqueues it instead (§7) — the only place iOS silently defers a write.
- Validation errors from the OpenAPI schema (`400`) show inline on the offending field where the form has one (meal/ingredient/template editors), else as a banner.

## 12. Testing

- **Swift Testing** (`import Testing`) for view models and repositories: unit conversion display, nutrition summary rendering (`null` renders as "—", never 0, mirroring `web/CLAUDE.md`), the offline-queue's collapsing rules from §7 (check-collapses, add-then-remove-cancels, ordered replay), SSE event handling from §8 (stale-event drop, version-gap refetch), and auth refresh single-flight.
- **XCUITest** against a running API (no mocked network for these): sign in / register / sign out, build a meal end to end, apply a template to a date, generate a shopping list, check off a shared item and confirm it appears from a second signed-in session, and an offline-then-online round trip (airplane mode toggled via the simulator's network link conditioner or a debug network-kill switch) that checks an item offline and confirms it lands after reconnect.
- `apple:accessibility` audit runs before the iOS app is considered release-ready (parent spec §6), not gated per feature plan, since VoiceOver/Dynamic Type checks are cheapest done once the full tab set exists.
- Each test run registers its own user through the API (same pattern as the web app's Playwright suite, `2026-09-25-web-app-design.md` §7), so tests never share state and need no `FDC_API_KEY`.

## 13. Delivery and CI

- New path-filtered `.github/workflows/ios.yml`, triggered on `ios/**`, `openapi.yaml`, and itself. Runs on a macOS GitHub Actions runner (`macos-15` or newer, matching an available Xcode 26 image).
- **Unit job** (every run): install XcodeGen and Mint (or use the SPM-plugin path directly) → `swift build` → `swift test` on `MealPlannerKit`. No API, no Postgres, no simulator needed — repositories under test use a stubbed `URLProtocol`, not the real network.
- **UI job** (every run, since XCUITest is the only place iOS talks to a real API): GitHub's macOS runners cannot use `services:` containers (Docker only works there on Linux runners), so Postgres runs natively via Homebrew (`brew install postgresql@16 && brew services start postgresql@16`) instead of the backend's `testcontainers`/Compose approach. Then: build the Go API binary, run `cmd/migrate` against the local Postgres, start the API, `xcodegen generate`, `xcodebuild test` against a simulator destination, targeting the running API's base URL. Mirrors the web app's Playwright job (`2026-09-25-web-app-design.md` §8) in spirit — build the real backend, run the real flows — with a native Postgres in place of a container.
- **Drift check**: regenerate the Swift OpenAPI client and fail if committed output differs, alongside the existing Go/TS drift checks (root `CLAUDE.md` `make check-generated`); a new `make check-generated-ios` target wraps it.
- New Makefile targets: `make generate-ios` (regenerate the Swift client — mainly for local diffing, since the build plugin also regenerates it during `swift build`), `check-generated-ios`, `build-ios` (`xcodegen generate && xcodebuild build`), `test-ios` (`swift test`).
- No new Docker Compose service: the iOS app talks to `make run-api` directly during local development, same base URL pattern as the web app.

## 14. Documentation updates

- Root `CLAUDE.md`: add the `ios/` repo-map entry (already present as a stub — "added by own plan") and the new `ios.yml`-related Make targets from §13.
- New `ios/CLAUDE.md`, mirroring `web/CLAUDE.md`'s shape: stack summary and pointer to this spec; layout (§4); commands (§13); gotchas — version conflicts, SSE refetch rules, snack single-vs-multi addressing, nutrient replace-whole-set on update, targets arriving on `GET /plan`, the ingredients "mine only" client-side filter — everything from parent-spec parity that a future change could otherwise silently break.
- `AGENTS.md`'s existing iOS skill row (`apple-skills:*` + `apple:*`) stays as is; no change needed there.

## 15. Plans

One spec, five plans, five PRs, in order (parent spec §9, handoff decision 2):

1. **Foundation.** `ios/` scaffold (XcodeGen `project.yml`, `MealPlannerKit` package) and `ios/CLAUDE.md`; Swift OpenAPI Generator wiring and drift check; Keychain auth with transparent refresh and single-flight; email/password sign-in and registration; the five-tab shell with empty placeholder screens; `ios.yml` CI (unit + UI jobs) and Makefile targets.
2. **Meals.** The shared ingredient search with custom-ingredient creation, meal library and autosaving editor with live nutrition, partner's shared meals and copy; plus the session handling Foundation deferred to the first authenticated screens (`2026-10-02-ios-meals-design.md`).
3. **Plan and Today.** Diet template editor, week calendar and apply, Today with macro rings and one-tap swaps. (`2026-10-03-ios-plan-today-design.md`)
4. **Shopping and Profile.** Lists, generate, check-off and quick-add; the offline queue (§7) and SSE (§8); the partner connection UI; targets, custom ingredients and account deletion; the accessibility audit. Split in two: Shopping first (`2026-10-08-ios-shopping-design.md`), then Profile with its own spec and plan.
5. **Sign in with Apple.** Its own backend plan first (`POST /auth/apple`, `openapi.yaml` change, Apple Developer Service ID and keys), then the iOS `AuthenticationServices` integration.

Plans 2 through 5 are written after the previous plan merges, so each reflects what was actually built, same discipline as the web app's plan sequencing (`2026-09-25-web-app-design.md` §10).

## 16. Out of scope and known API gaps

Out of scope for this spec: Sign in with Apple's implementation details (its own plan), offline support for anything other than the shopping list, Android, push notifications, a watch/widget companion, hosting.

Known API gaps, inherited from the parent spec and worked around exactly as the web app does, to fix spec-first when they bite: no "mine only" filter on `GET /ingredients` (client fetches all pages and filters locally, §9); no entry-addressed plan endpoint, so a single snack cannot be edited, only added or cleared (§9).
