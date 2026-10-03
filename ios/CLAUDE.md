# iOS (SwiftUI)

SwiftUI, iOS 26+, Swift 6 language mode. Design: `docs/superpowers/specs/2026-09-30-ios-app-design.md`. Read before changing auth, networking or caching behaviour.

## Layout

- `project.yml`: XcodeGen spec. Run `xcodegen generate` after editing it (or via `make build-ios`) — the generated `MealPlanner.xcodeproj` is gitignored and never hand-edited.
- `MealPlanner/`: the thin app target. `MealPlannerApp.swift` (`@main`), `Assets.xcassets`. All real logic lives in `MealPlannerKit`.
- `MealPlannerUITests/`: XCUITest target (needs the app target to launch; cannot live inside the SPM package).
- `MealPlannerKit/`: local Swift package, built and tested with `swift build`/`swift test` independent of Xcode.
  - `Sources/API/`: generated OpenAPI client (`GeneratedSources/`, never hand-edited — regenerate with `make generate-ios`) plus `APIClient.swift`, the hand-written client-construction helpers.
  - `Sources/Auth/`: `KeychainTokenStore`, `TokenRefresher` (single-flighted refresh), `BearerAuthMiddleware`, `AuthRepository`, `AuthError`.
  - `Sources/Persistence/`: SwiftData `@Model` classes (internal, never leave the module), `MealCache` (a `@ModelActor` that takes and returns generated value types), `CacheStore` (container factory: delete-and-recreate on an unopenable store, in-memory as a last resort). One cache actor per domain.
  - `Sources/Repositories/`: `MealsRepository`, `IngredientsRepository`, `PartnerRepository`. `async throws`, one exhaustive `switch` over the generated response enum into a typed error. Lists expose `cached…()` then `refresh…()`. Imports `OpenAPIRuntime` only to unwrap `ClientError`.
  - `Sources/Features/`: one folder per product area (`Auth`, `Today`, `Plan`, `Meals`, `Shopping`, `Profile`) plus `Shared/` (nutrition catalog/formatting/panel, `parseDecimal`, ingredient search, custom-ingredient form). Views never call the network or SwiftData; they talk to view models, which talk to repositories.
  - `Sources/AppCore/`: `AppState` (`@Observable`, owns `session`: `signedOut` / `unverified` / `signedIn`), `RootView` (the package's one public entry point; builds the repositories and cache), `TabShellView`.
  - `Tests/MealPlannerKitTests/`: Swift Testing (`import Testing`, `@Test`/`#expect`). Network-touching code is tested against `StubTransport`, never the real API.

## Commands (from repo root)

- `make build-ios`: `xcodegen generate` then `xcodebuild build` for the simulator. Needs Xcode and XcodeGen (`brew install xcodegen`).
- `make test-ios`: `swift test` on `MealPlannerKit`. No Xcode project, no simulator, no API needed — repositories are tested against `StubTransport`.
- `make generate-ios`: regenerate the Swift OpenAPI client from the root `openapi.yaml`.
- `make check-generated-ios`: fails if the committed generated client differs from what `openapi.yaml` produces now.

## Gotchas

- The generated client uses `namingStrategy: idiomatic` (`Sources/API/openapi-generator-config.yaml`), so schema fields are camelCase in Swift (`displayName`, `accessToken`) even though the JSON wire format stays snake_case. Don't "fix" a camelCase name back to match the YAML — that's the generator working as configured.
- `Sources/API/openapi.yaml` is a symlink to the repo-root spec, not a copy. Never edit it directly; edit the root `openapi.yaml` and run `make generate-ios`.
- `TokenRefresher` single-flights concurrent refreshes: N requests racing a 401 must trigger exactly one `/auth/refresh` call. If you touch `BearerAuthMiddleware` or `TokenRefresher`, `TokenRefresherTests.singleFlight` is the test that catches a regression here — a naive "each request refreshes independently" implementation logs every signed-in user out under load, because the backend revokes a session family when a used refresh token is replayed.
- The login API answers an unknown email and a wrong password identically (`401`, same message) — never show the user which one was wrong; `AuthRepository.login` maps both to one `AuthError.invalidCredentials`.
- `AppState.restoreSession()` calls `GET /me` on launch whenever a refresh token is on disk, rather than trusting a cached "signed in" flag — the API is the source of truth (spec §5). A revoked refresh token surfaces as `.signedOut`; a transient failure surfaces as `.unverified` (see the Session gotcha below).
- Registering a taken email is `409 email_taken`; a validation failure (e.g. a too-short password) is `400 validation_failed` carrying only a per-field `errors` array (`[{"field": "password", "code": "too_short"}]`), never a `detail` string — confirmed against the running API, not just the schema. `AuthRepository`'s `validationMessage(for:)` maps `(field, code)` pairs to friendly text and only falls back to `detail`/`title` when `errors` is absent. These are different UI messages from `email_taken` — don't collapse them into one generic "registration failed."
- Only `Sources/API` and `Sources/Auth` need `import HTTPTypes`/`import OpenAPIRuntime`. `Features` and `AppCore` should never need to import the networking runtime packages directly — if a view needs to inspect a raw HTTP status or header, that logic belongs in `Auth` or a later `Repositories` target, not the view.
- SwiftUI code that uses a UIKit-only modifier (`.keyboardType`, `.textInputAutocapitalization`, etc.) must guard it with `#if os(iOS)` — the package also declares macOS as a platform (Package.swift) purely so `swift test` can run natively on this machine without a simulator, and an unguarded UIKit-only modifier fails that macOS build.
- Autosave lives in `MealEditorViewModel` (port of web's `useAutosave`): 700 ms debounce, one save at a time, a tried value (saved or failed) is never retried by itself, `PATCH` fields then `PUT` ingredients with each step committed on its own. Its tests drive time through an injected `sleep` and a `TestSleeper`; do not add real sleeps.
- The generated `UpdateMealRequest.notes` is `String?` and cannot encode an explicit JSON `null`. Clearing notes sends `""`; `MealDraft` treats blank and `nil` as equal on both sides so this never shows as a pending edit.
- The generated client wraps transport failures in `ClientError`. Repositories rethrow `underlyingError` (`unwrapping`), so view models see a plain `URLError`. A new repository method must go through `unwrapping`.
- Session: `AppState.restoreSession()` signs out only when the refresh token is gone from the store afterwards (`TokenRefresher` clears it exactly when the API rejects it). Network, 429 and 5xx at launch give `.unverified`: the shell opens on cache, marked stale, and verification retries on foreground. Reconnecting without foregrounding is not detected (no `NWPathMonitor`).
- `TokenRefresher` calls its session-ended handler once per rejected refresh; `AppState.attach(to:)` wires it. Sign-out flips state and clears Keychain and caches first, then revokes in the background (`AppState.pendingRevoke`). Every path to signed-out clears the caches.
- `MealsViewModel`'s `delete`, `copyToLibrary` and `createMeal` return the error text rather than setting an alert: an alert on the list cannot present while a sheet covers it.
- Test helpers: `StubTransport` (one canned response) for the Foundation-era tests; `RoutingTransport` (sees method, path, query, body; may suspend on a `Gate`) for everything newer. `Fixtures` encodes the generated types to build stub JSON.
- `MealCache` is a `@ModelActor`. If a Swift toolchain bump breaks its strict-concurrency expansion, the fallback is a `@MainActor` class over `mainContext` with the same value-type API (spec §5.2); nothing above `Persistence` changes.
- XCUITest on CI: `typeKey` hardware-shortcut synthesis delivers nothing on any app launch after the first one in an `xcodebuild test` run (use `typeText`); a tab button's accessibility identifier can be absent after the first launch (match its label too, `AppUITestCase.tabButton`); wait until an element is hittable, not merely present, after a screen transition; GitHub's macOS runners need generous waits (45 s) and occasionally hang the `ui` job for the full 30 minutes (simulator boot stall), which `gh run rerun <run-id> --failed` clears. Local XCUITest is unreliable under heavy machine load: trust CI.
