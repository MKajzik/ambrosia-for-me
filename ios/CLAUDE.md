# iOS (SwiftUI)

SwiftUI, iOS 26+, Swift 6 language mode. Design: `docs/superpowers/specs/2026-09-30-ios-app-design.md`. Read before changing auth, networking or caching behaviour.

## Layout

- `project.yml`: XcodeGen spec. Run `xcodegen generate` after editing it (or via `make build-ios`) — the generated `MealPlanner.xcodeproj` is gitignored and never hand-edited.
- `MealPlanner/`: the thin app target. `MealPlannerApp.swift` (`@main`), `Assets.xcassets`. All real logic lives in `MealPlannerKit`.
- `MealPlannerUITests/`: XCUITest target (needs the app target to launch; cannot live inside the SPM package).
- `MealPlannerKit/`: local Swift package, built and tested with `swift build`/`swift test` independent of Xcode.
  - `Sources/API/`: generated OpenAPI client (`GeneratedSources/`, never hand-edited — regenerate with `make generate-ios`) plus `APIClient.swift`, the hand-written client-construction helpers.
  - `Sources/Auth/`: `KeychainTokenStore`, `TokenRefresher` (single-flighted refresh), `BearerAuthMiddleware`, `AuthRepository`, `AuthError`.
  - `Sources/Features/`: one folder per product area (`Auth`, `Today`, `Plan`, `Meals`, `Shopping`, `Profile`) — views and view models. Views never call the network or SwiftData directly (once Repositories/Persistence arrive in the Meals/Shopping plans).
  - `Sources/AppCore/`: `AppState` (`@Observable`, owns `session`), `RootView` (the package's one public entry point — switches sign-in/register vs. the tab shell), `TabShellView`.
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
- `AppState.restoreSession()` calls `GET /me` on launch whenever a refresh token is on disk, rather than trusting a cached "signed in" flag — the API is the source of truth (spec §5). A stale or revoked refresh token surfaces as `.signedOut`, not a crash or a stuck spinner.
- Registering a taken email is `409 email_taken`; a validation failure (e.g. a too-short password) is `400 validation_failed` carrying only a per-field `errors` array (`[{"field": "password", "code": "too_short"}]`), never a `detail` string — confirmed against the running API, not just the schema. `AuthRepository`'s `validationMessage(for:)` maps `(field, code)` pairs to friendly text and only falls back to `detail`/`title` when `errors` is absent. These are different UI messages from `email_taken` — don't collapse them into one generic "registration failed."
- Only `Sources/API` and `Sources/Auth` need `import HTTPTypes`/`import OpenAPIRuntime`. `Features` and `AppCore` should never need to import the networking runtime packages directly — if a view needs to inspect a raw HTTP status or header, that logic belongs in `Auth` or a later `Repositories` target, not the view.
- SwiftUI code that uses a UIKit-only modifier (`.keyboardType`, `.textInputAutocapitalization`, etc.) must guard it with `#if os(iOS)` — the package also declares macOS as a platform (Package.swift) purely so `swift test` can run natively on this machine without a simulator, and an unguarded UIKit-only modifier fails that macOS build.
