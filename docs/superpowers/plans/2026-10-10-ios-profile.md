# iOS Profile Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the Profile tab's stub with the real feature: daily targets, partner connection (create invite, enter code, unlink), custom ingredient management (create, edit, delete) and account deletion, plus the existing Sign Out, on the existing API with no contract change.

**Architecture:** A small `ProfileCache` (SwiftData) holds the `User` and the partnership so Profile opens offline and on an unverified launch. `ProfileRepository`, an extended `PartnerRepository` and an extended `IngredientsRepository` own the network; writes are online-only and store the server's answer. The generated Swift request types cannot send an explicit `null`, so clearing a target (or an ingredient's weight per piece or density) sends the sentinel `-1`, which `NullSentinelMiddleware` rewrites to JSON `null` for `updateMe` and `updateIngredient` only. View models follow the Meals/Shopping conventions (`cached…()` then `refresh…()`).

**Tech Stack:** Swift 6.2 (Swift 6 mode), SwiftUI, SwiftData (`@ModelActor`), Observation, Swift Testing, XCTest/XCUITest, the existing Swift OpenAPI client. No new package dependencies; no change to `openapi.yaml`.

**Spec:** `docs/superpowers/specs/2026-10-10-ios-profile-design.md` (this plan implements all of it). Read also `docs/superpowers/specs/2026-09-30-ios-app-design.md` §6 and §9, `ios/CLAUDE.md`, and the web parity code: `web/src/features/profile/`, `web/src/features/partner/`, `web/src/features/ingredients/` and `web/CLAUDE.md` §Gotchas. GitHub issue: #28 (the Shopping half already shipped as #31).

## Global Constraints

- iOS 26+ only; the package also declares macOS 15 so `swift test` and `swift build` run natively. Any UIKit-only SwiftUI modifier or API (`UIPasteboard`, `.textInputAutocapitalization`, `.keyboardType`) sits behind `#if os(iOS)` or a helper in `Features/Shared/ViewHelpers.swift`.
- Swift 6 language mode, strict concurrency. View models are `@Observable @MainActor`. In `@MainActor` test suites, a `static` used from a `@Sendable` route closure must be `nonisolated`, and any nested helper struct must be `@MainActor`.
- The generated `Components.Schemas.*` types are the domain types. Never hand-edit `Sources/API/GeneratedSources`.
- Only `Sources/API`, `Sources/Auth` and `Sources/Repositories` may `import OpenAPIRuntime` / `HTTPTypes`. Views and view models never touch SwiftData or an HTTP status. `@Model` objects never leave `Persistence`.
- Every call to the generated client in a repository goes through `unwrapping`.
- All Profile writes are online-only and never optimistic; after a good write the cache is updated from the server's answer. No offline queue.
- Targets (spec §4): calories above 0 and at most 20000; protein at most 2000; carbs at most 5000; fat at most 2000; macros from 0; a blank field clears the target; a comma is a decimal point (`parseDecimal`); all four are sent on every save.
- Clearing: the sentinel is `NullSentinel.value` (`-1`); the middleware rewrites only top-level numbers equal to it, only for operation ids `updateMe` and `updateIngredient`. View models and views never see the sentinel: they work with `Double?` and `nil`.
- An ingredient edit sends all 18 nutrients: the four the form shows from the form (a blank one is omitted, meaning unknown), the other 14 copied from the existing ingredient. A blank weight per piece or density clears it.
- The invite code is held in memory only and never persisted. `unlink()` treats `404` as done.
- Account deletion asks for the account's email (trimmed, case-insensitive), calls `DELETE /me`, then runs the existing sign-out; it never sends a request while the email does not match.
- Tests first. Small commits, one logical change each. Commit trailer: `Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>`.
- `swift test` runs from `ios/MealPlannerKit` (filter with `--filter <Suite>`). Compiling the UI-test target needs `xcodebuild build-for-testing` (`make build-ios` does not compile it). Local XCUITest is unreliable under load: GitHub CI is the signal for the `ui` job.
- `git` works directly in this worktree (`.claude/worktrees/ios-profile`, branch `worktree-ios-profile`).

## Decisions this plan adds to the spec

These come from reading the merged code while planning. Challenge them at review.

1. **`PartnerRepository.init(client:cache:)` takes the cache as `ProfileCache? = nil`.** Six existing call sites (three view-model test files, the repository tests, the Shopping harness, `RootView`) build it with only a client. A default keeps them compiling; only `RootView` passes the cache. Without a cache `status()` simply does not store anything and `cachedStatus()` answers `.unknown`.
2. **`CachedPartnership` has cases `unknown`, `none` and `present(Partnership)`**, not `linked`: a pending invite is a partnership too, and it is not linked.
3. **`ProfileViewModel` takes a `signOut` closure**, so "deleting the account signs out exactly once, and only after `204`" is testable without a view. `ProfileView` passes its `onSignOut`.
4. **`TargetsUpdate` and `IngredientUpdate` live in `Repositories`** (they are repository inputs); the draft/form builders that produce them live in `Features`.
5. **`IngredientError` gains `.notFound`, `.inUse` (`409 ingredient_in_use`) and `.unitInUse` (`409 unit_not_convertible`, clearing a conversion factor a meal still needs).** `ErrorText`'s exhaustive switch over `IngredientError` must be extended in the same task.
6. **The edit sheet reuses `CustomIngredientView`/`CustomIngredientViewModel`** through an edit initialiser and a `validateUpdate(preserving:)` on the form, instead of a second form. The stale comments in those files that say the editor "belongs to the Profile plan" are removed.
7. **`NullSentinelMiddleware` sits outside `BearerAuthMiddleware`** in the middleware list. It rebuilds the body from `Data`, which is replayable, so the bearer middleware can still retry the request after a `401` (it refuses to retry a single-use body).
8. **`ProfileCache` is not seeded from `AppState` at sign-in.** It fills the first time Profile loads online. A first-ever launch that is offline before Profile was ever opened shows the error state with retry.
9. **The test server `ProfileServer` stubs only `/me`.** Partner and ingredient tests use inline `RoutingTransport` routes, as the existing tests do.
10. **The accept route's tighter rate limit** gets its own wording through `PartnerError.rateLimited`.

## Review Focus

Inputs and conditions the spec implies but a happy-path test would not exercise, most likely first. Each has a test in the task that owns the code.

1. **Blank target → really cleared on the server**, end to end through view model, repository, middleware and transport; and a failed save leaves the screen showing what the server has. (Task 1 middleware tests, Task 5 repository, Task 8 view model.)
2. **Editing an ingredient's name must not erase its other 14 nutrients**, and a blank weight per piece/density must clear. (Task 3 builder, Task 10 end-to-end through the view model.)
3. **Offline or unverified launch**: Profile shows the cached user with an offline label, writes fail with a retryable message and change nothing, and a first launch with no cache shows an error rather than a blank form. (Task 8.)
4. **A refresh while the person has unsaved edits must not overwrite them**, and a stale-looking cache must not enable Save with nothing changed. (Task 8.)
5. **Deleting the account**: wrong email (case, spaces), server failure and success. Nothing is sent on a mismatch, a failure leaves the account and the session untouched, success signs out once, and sign-out clears `ProfileCache` so a second user never sees the first user's email. (Task 8, Task 11.)
6. **Partner edge cases**: messy code input (spaces, dashes, lowercase) is sent trimmed as typed; `404` vs `409` vs `429` each read differently; `unlink` of an already-gone link succeeds; the invite code vanishes with the view model. (Task 6, Task 9.)

---

## File Structure

```
ios/MealPlannerKit/Sources/
├── Persistence/
│   ├── CachedProfileModels.swift       (new) @Model rows: user, partnership
│   ├── ProfileCache.swift              (new) @ModelActor + CachedPartnership
│   ├── PlanCache.swift                 (modify) setTargets
│   └── CacheStore.swift                (modify) schema + makeProfileCache
├── Repositories/
│   ├── NullSentinelMiddleware.swift    (new) NullSentinel + middleware
│   ├── TargetsUpdate.swift             (new)
│   ├── IngredientUpdate.swift          (new)
│   ├── ProfileError.swift              (new)
│   ├── ProfileRepository.swift         (new)
│   ├── PartnerError.swift              (new)
│   ├── PartnerRepository.swift         (modify) cache, invite, accept, unlink
│   ├── PlanRepository.swift            (modify) storeTargets
│   ├── IngredientsRepository.swift     (modify) customIngredients, update, delete
│   └── MealsError.swift                (modify) IngredientError cases
├── Features/
│   ├── Shared/ErrorText.swift          (modify) Profile/Partner/Ingredient text
│   ├── Shared/CustomIngredient/        (modify) form edit mode, view model edit mode, view edit init
│   └── Profile/
│       ├── TargetsDraft.swift          (new) pure
│       ├── InviteCodeFormat.swift      (new) pure
│       ├── ProfileViewModel.swift      (new)
│       ├── PartnerViewModel.swift      (new)
│       ├── MyIngredientsViewModel.swift(new)
│       ├── ProfileDependencies.swift   (new)
│       ├── ProfileView.swift           (replace stub)
│       ├── TargetsSection.swift        (new)
│       ├── PartnerSection.swift        (new)
│       ├── DeleteAccountSheet.swift    (new)
│       └── MyIngredientsView.swift     (new)
└── AppCore/ ClearCaches.swift, RootView.swift, TabShellView.swift   (modify)
ios/MealPlannerUITests/ProfileFlowUITests.swift                       (new)
ios/CLAUDE.md, the iOS spec, the Profile spec, this plan             (modify, Task 13)
Tests/MealPlannerKitTests/: ProfileFixtures, NullSentinelMiddlewareTests, TargetsDraftTests, InviteCodeFormatTests,
  CustomIngredientEditTests, ProfileCacheTests, PlanCacheTargetsTests, ProfileRepositoryTests, PartnerRepositoryProfileTests,
  IngredientsRepositoryProfileTests, ProfileServer, ProfileViewModelTests, PartnerViewModelTests,
  MyIngredientsViewModelTests, CustomIngredientEditViewModelTests
```

---

### Task 1: `NullSentinelMiddleware` and the Profile test fixtures

**Files:**
- Create: `ios/MealPlannerKit/Sources/Repositories/NullSentinelMiddleware.swift`
- Create: `ios/MealPlannerKit/Tests/MealPlannerKitTests/ProfileFixtures.swift` (test support, used by every later task)
- Test: `ios/MealPlannerKit/Tests/MealPlannerKitTests/NullSentinelMiddlewareTests.swift`

**Interfaces:**
- Consumes: the test seam `makeClient(baseURL:transport:middlewares:)` (`Sources/API/APIClient.swift`), `RoutingTransport`, `Locked` (`Tests/.../TestSupport.swift`), `Fixtures.json`, `Fixtures.date`, `Fixtures.ingredient(...)`.
- Produces: `NullSentinel.value: Double` (`-1`), `NullSentinelMiddleware()` (a `ClientMiddleware`), `NullSentinelMiddleware.operations: Set<String>`; test helpers `ProfileFixtures.user(...)`, `.active(_:)`, `.pending()`, `.invite(code:)`, `.fullNutrients()`, `.fullInput()`, `.fullIngredient()`, `.customIngredient(...)`, `.ingredientPage(_:next:)`.

- [ ] **Step 1: Write the test support file `ProfileFixtures.swift`**

```swift
import API
import Foundation

enum ProfileFixtures {
    static func user(
        id: String = "u1", email: String = "sam@example.com", name: String = "Sam",
        kcal: Double? = nil, protein: Double? = nil, carbs: Double? = nil, fat: Double? = nil
    ) -> Components.Schemas.User {
        .init(
            id: id, email: email, displayName: name, targetKcal: kcal, targetProteinG: protein, targetCarbsG: carbs,
            targetFatG: fat, createdAt: Fixtures.date, updatedAt: Fixtures.date
        )
    }

    static func active(_ name: String = "Alex") -> Components.Schemas.Partnership {
        .init(status: .active, displayName: name, linkedAt: Fixtures.date)
    }

    static func pending() -> Components.Schemas.Partnership {
        .init(status: .pending, expiresAt: Fixtures.date)
    }

    static func invite(code: String = "ABCD2345") -> Components.Schemas.PartnerInvite {
        .init(code: code, expiresAt: Fixtures.date)
    }

    /// All 18 nutrients known, so a test can see which ones an edit kept.
    static func fullNutrients() -> Components.Schemas.NutrientAmounts {
        .init(
            calories: 52, protein: 0.3, carbohydrates: 14, sugar: 10, fibre: 2.4, fat: 0.2, saturatedFat: 0.03,
            sodium: 1, potassium: 107, calcium: 6, iron: 0.1, magnesium: 5, zinc: 0.04, vitaminA: 3, vitaminC: 4.6,
            vitaminD: 0, vitaminB12: 0, folate: 3
        )
    }

    /// The same 18 values as the request type.
    static func fullInput() -> Components.Schemas.NutrientAmountsInput {
        .init(
            calories: 52, protein: 0.3, carbohydrates: 14, sugar: 10, fibre: 2.4, fat: 0.2, saturatedFat: 0.03,
            sodium: 1, potassium: 107, calcium: 6, iron: 0.1, magnesium: 5, zinc: 0.04, vitaminA: 3, vitaminC: 4.6,
            vitaminD: 0, vitaminB12: 0, folate: 3
        )
    }

    static func customIngredient(
        id: String = "ing-1", name: String = "Apple", category: Components.Schemas.IngredientCategory = .produce,
        gramsPerPiece: Double? = nil, density: Double? = nil,
        nutrients: Components.Schemas.NutrientAmounts = fullNutrients()
    ) -> Components.Schemas.Ingredient {
        .init(
            id: id, name: name, category: category, isCustom: true, gramsPerPiece: gramsPerPiece, densityGPerMl: density,
            nutrients: nutrients, createdAt: Fixtures.date, updatedAt: Fixtures.date
        )
    }

    /// A custom ingredient with all 18 nutrients and a weight per piece.
    static func fullIngredient() -> Components.Schemas.Ingredient {
        customIngredient(gramsPerPiece: 182)
    }

    static func ingredientPage(_ items: [Components.Schemas.Ingredient], next: String? = nil) -> String {
        Fixtures.json(Components.Schemas.IngredientList(items: items, nextCursor: next))
    }
}
```

- [ ] **Step 2: Write the failing tests** — `NullSentinelMiddlewareTests.swift`

```swift
import API
import Foundation
import HTTPTypes
import OpenAPIRuntime
import Testing
@testable import Repositories

@Suite
struct NullSentinelMiddlewareTests {
    private func client(_ transport: RoutingTransport) -> Client {
        makeClient(transport: transport, middlewares: [NullSentinelMiddleware()])
    }

    @Test("updateMe: the sentinel becomes null; real numbers (including 0) stay numbers")
    func updateMe() async throws {
        let transport = RoutingTransport { _ in (200, Fixtures.json(ProfileFixtures.user())) }
        _ = try await client(transport).updateMe(.init(body: .json(.init(
            targetKcal: NullSentinel.value, targetProteinG: 70, targetCarbsG: 250, targetFatG: 0
        ))))
        let body = try #require(await transport.calls("PATCH /me").first).body
        #expect(body.contains("\"target_kcal\":null"))
        #expect(body.contains("\"target_protein_g\":70"))
        #expect(body.contains("\"target_carbs_g\":250"))
        #expect(body.contains("\"target_fat_g\":0"))
    }

    @Test("updateMe with no sentinel is sent unchanged: no nulls appear")
    func noSentinel() async throws {
        let transport = RoutingTransport { _ in (200, Fixtures.json(ProfileFixtures.user())) }
        _ = try await client(transport).updateMe(.init(body: .json(.init(targetKcal: 2000, targetProteinG: 70))))
        let body = try #require(await transport.calls("PATCH /me").first).body
        #expect(body.contains("\"target_kcal\":2000"))
        #expect(!body.contains("null"))
    }

    @Test("updateIngredient: weight per piece clears, density stays; a -1 nested in nutrients is left alone")
    func updateIngredient() async throws {
        let transport = RoutingTransport { _ in (200, Fixtures.json(Fixtures.ingredient(isCustom: true))) }
        _ = try await client(transport).updateIngredient(.init(
            path: .init(id: "ing-1"),
            body: .json(.init(
                name: "Jam", gramsPerPiece: NullSentinel.value, densityGPerMl: 1.2,
                nutrients: .init(calories: NullSentinel.value)
            ))
        ))
        let body = try #require(await transport.calls("PATCH /ingredients/ing-1").first).body
        #expect(body.contains("\"grams_per_piece\":null"))
        #expect(body.contains("\"density_g_per_ml\":1.2"))
        #expect(body.contains("\"calories\":-1"))
    }

    @Test("Other operations are untouched, even with the same value")
    func otherOperations() async throws {
        let transport = RoutingTransport { _ in (201, Fixtures.json(Fixtures.ingredient(isCustom: true))) }
        _ = try await client(transport).createIngredient(.init(body: .json(.init(
            name: "Jam", category: .other, gramsPerPiece: NullSentinel.value
        ))))
        let body = try #require(await transport.calls("POST /ingredients").first).body
        #expect(body.contains("\"grams_per_piece\":-1"))
    }

    @Test("The rewritten body can be sent twice, so the bearer middleware's retry after a 401 still carries it")
    func replayable() async throws {
        let seen = Locked<HTTPBody?>(nil)
        _ = try await NullSentinelMiddleware().intercept(
            HTTPRequest(method: .patch, scheme: nil, authority: nil, path: "/me"),
            body: HTTPBody(#"{"target_kcal":-1}"#), baseURL: URL(string: "http://localhost")!, operationID: "updateMe"
        ) { _, body, _ in
            seen.set(body)
            return (HTTPResponse(status: .ok), nil)
        }
        #expect(seen.value?.iterationBehavior == .multiple)
    }

    @Test("A body that is not a JSON object passes through")
    func notAnObject() async throws {
        let seen = Locked<String>("")
        _ = try await NullSentinelMiddleware().intercept(
            HTTPRequest(method: .patch, scheme: nil, authority: nil, path: "/me"),
            body: HTTPBody("[1,2]"), baseURL: URL(string: "http://localhost")!, operationID: "updateMe"
        ) { _, body, _ in
            if let body { seen.set(try await String(collecting: body, upTo: 1024)) }
            return (HTTPResponse(status: .ok), nil)
        }
        #expect(seen.value == "[1,2]")
    }
}
```

- [ ] **Step 3: Run to verify it fails**

Run (from `ios/MealPlannerKit`): `swift test --filter NullSentinelMiddlewareTests`
Expected: FAIL to compile, "cannot find 'NullSentinel' in scope".

- [ ] **Step 4: Implement `NullSentinelMiddleware.swift`**

```swift
import Foundation
import HTTPTypes
import OpenAPIRuntime

/// The generated request types are plain optionals and cannot encode an explicit JSON `null`, but `PATCH /me` and
/// `PATCH /ingredients/{id}` use `null` to clear a field (an omitted field means "unchanged"). A repository that wants
/// to clear one sends `NullSentinel.value`, and `NullSentinelMiddleware` turns it into `null` on the way out.
///
/// The value is invalid for every field that can be cleared (they must be above 0, or 0 or more), so if the
/// middleware ever failed to run the server would answer `400` instead of changing anything.
public enum NullSentinel {
    public static let value: Double = -1
}

public struct NullSentinelMiddleware: ClientMiddleware {
    /// The only operations whose top-level numbers are rewritten.
    public static let operations: Set<String> = ["updateMe", "updateIngredient"]

    public init() {}

    public func intercept(
        _ request: HTTPRequest,
        body: HTTPBody?,
        baseURL: URL,
        operationID: String,
        next: (HTTPRequest, HTTPBody?, URL) async throws -> (HTTPResponse, HTTPBody?)
    ) async throws -> (HTTPResponse, HTTPBody?) {
        guard Self.operations.contains(operationID), let body else {
            return try await next(request, body, baseURL)
        }
        let data = try await Data(collecting: body, upTo: 1_048_576)
        guard var object = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any] else {
            // Not a JSON object: send exactly what we were given (the original body was consumed above).
            return try await next(request, HTTPBody(data), baseURL)
        }
        // Top-level fields only. Only a number can equal the sentinel; a boolean is 0 or 1 and never matches.
        for (key, value) in object {
            if let number = value as? NSNumber, number.doubleValue == NullSentinel.value {
                object[key] = NSNull()
            }
        }
        let rewritten = try JSONSerialization.data(withJSONObject: object)
        var request = request
        request.headerFields[.contentLength] = String(rewritten.count)
        return try await next(request, HTTPBody(rewritten), baseURL)
    }
}
```

- [ ] **Step 5: Run to verify it passes, then commit**

Run: `swift test --filter NullSentinelMiddlewareTests`
Expected: PASS (6 tests). If `Data(collecting:upTo:)` does not exist in the installed OpenAPIRuntime, use `try await Data(collecting: body, upTo:)`'s equivalent: `var data = Data(); for try await chunk in body { data.append(contentsOf: chunk) }`.

```bash
git add ios/MealPlannerKit/Sources/Repositories/NullSentinelMiddleware.swift ios/MealPlannerKit/Tests/MealPlannerKitTests/ProfileFixtures.swift ios/MealPlannerKit/Tests/MealPlannerKitTests/NullSentinelMiddlewareTests.swift
git commit -m "feat(ios): add the null-sentinel middleware for clearing fields

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 2: `TargetsUpdate` and `TargetsDraft` (pure targets validation)

**Files:**
- Create: `ios/MealPlannerKit/Sources/Repositories/TargetsUpdate.swift`
- Create: `ios/MealPlannerKit/Sources/Features/Profile/TargetsDraft.swift`
- Test: `ios/MealPlannerKit/Tests/MealPlannerKitTests/TargetsDraftTests.swift`

**Interfaces:**
- Consumes: `parseDecimal`, `plainNumber` (`Features/Shared/ParseDecimal.swift`), `ProfileFixtures.user` (Task 1).
- Produces: `TargetsUpdate(kcal:protein:carbs:fat:)` (all `Double?`, `nil` = clear); `TargetsDraft` with `calories/protein/carbs/fat: String`, `init()`, `init(user:)`, `validate() -> TargetsDraft.Validation` (`.valid(TargetsUpdate)` / `.invalid([Field: String])`), `TargetsDraft.Field` (`calories, protein, carbs, fat`, `init?(apiPath:)`).

- [ ] **Step 1: Write the failing tests** — `TargetsDraftTests.swift`

```swift
import Testing
@testable import Features
@testable import Repositories

@Suite
struct TargetsDraftTests {
    private func draft(_ c: String = "", _ p: String = "", _ ca: String = "", _ f: String = "") -> TargetsDraft {
        var draft = TargetsDraft()
        draft.calories = c
        draft.protein = p
        draft.carbs = ca
        draft.fat = f
        return draft
    }

    private func update(_ draft: TargetsDraft) -> TargetsUpdate? {
        if case .valid(let update) = draft.validate() { update } else { nil }
    }

    private func errors(_ draft: TargetsDraft) -> [TargetsDraft.Field: String]? {
        if case .invalid(let errors) = draft.validate() { errors } else { nil }
    }

    @Test("Blank fields are nil (clear); numbers pass through; a comma is a decimal point")
    func parsing() {
        #expect(update(draft()) == TargetsUpdate(kcal: nil, protein: nil, carbs: nil, fat: nil))
        #expect(update(draft("2000", "70,5", "250", "0")) == TargetsUpdate(kcal: 2000, protein: 70.5, carbs: 250, fat: 0))
    }

    @Test("Calories must be above 0 and at most 20000")
    func calories() {
        #expect(errors(draft("0"))?[.calories] == "Calories must be more than 0 and at most 20000.")
        #expect(errors(draft("20000.1"))?[.calories] == "Calories must be more than 0 and at most 20000.")
        #expect(update(draft("20000")) != nil)
        #expect(update(draft("0.5")) != nil)
    }

    @Test("Macros allow 0 and have their own maximum and message")
    func macros() {
        #expect(update(draft("", "0", "0", "0")) != nil)
        #expect(errors(draft("", "2001"))?[.protein] == "Protein must be at most 2000 g.")
        #expect(errors(draft("", "", "5001"))?[.carbs] == "Carbohydrates must be at most 5000 g.")
        #expect(errors(draft("", "", "", "2001"))?[.fat] == "Fat must be at most 2000 g.")
        #expect(update(draft("", "2000", "5000", "2000")) != nil)
    }

    @Test("Text that is not a plain number gets a hint with an example, and negatives count as not a number")
    func notANumber() {
        let result = errors(draft("abc", "-5", "1e3", "12x"))
        #expect(result?[.calories] == "Enter a number, for example 2000.")
        #expect(result?[.protein] == "Enter a number, for example 70.")
        #expect(result?[.carbs] == "Enter a number, for example 250.")
        #expect(result?[.fat] == "Enter a number, for example 70.")
    }

    @Test("Every bad field is reported at once")
    func allErrors() {
        #expect(errors(draft("0", "9999", "9999", "9999"))?.count == 4)
    }

    @Test("A draft built from a user shows plain numbers and blanks for unset targets")
    func fromUser() {
        let user = ProfileFixtures.user(kcal: 2000, protein: 70.5)
        let built = TargetsDraft(user: user)
        #expect(built.calories == "2000")
        #expect(built.protein == "70.5")
        #expect(built.carbs == "")
        #expect(built.fat == "")
        #expect(built == draft("2000", "70.5"))
    }

    @Test("A server field path maps to a draft field")
    func apiPaths() {
        #expect(TargetsDraft.Field(apiPath: "target_kcal") == .calories)
        #expect(TargetsDraft.Field(apiPath: "target_protein_g") == .protein)
        #expect(TargetsDraft.Field(apiPath: "target_carbs_g") == .carbs)
        #expect(TargetsDraft.Field(apiPath: "target_fat_g") == .fat)
        #expect(TargetsDraft.Field(apiPath: "display_name") == nil)
    }
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `swift test --filter TargetsDraftTests`
Expected: FAIL to compile, "cannot find 'TargetsDraft' in scope".

- [ ] **Step 3: Implement**

`TargetsUpdate.swift`:

```swift
/// The four daily targets as the repository sends them. `nil` means "clear this target" (the repository turns it into
/// the null sentinel). All four are always sent, so a save never leaves the server and the screen disagreeing.
public struct TargetsUpdate: Equatable, Sendable {
    public var kcal: Double?
    public var protein: Double?
    public var carbs: Double?
    public var fat: Double?

    public init(kcal: Double?, protein: Double?, carbs: Double?, fat: Double?) {
        self.kcal = kcal
        self.protein = protein
        self.carbs = carbs
        self.fat = fat
    }
}
```

`TargetsDraft.swift`:

```swift
import API
import Repositories

/// The Daily targets form's text and validation, ported from web's `targets.ts`.
public struct TargetsDraft: Equatable, Sendable {
    public enum Field: Hashable, Sendable {
        case calories, protein, carbs, fat

        /// A server error path (`target_kcal`, ...) to the form field it names.
        public init?(apiPath: String) {
            switch apiPath {
            case "target_kcal": self = .calories
            case "target_protein_g": self = .protein
            case "target_carbs_g": self = .carbs
            case "target_fat_g": self = .fat
            default: return nil
            }
        }
    }

    public enum Validation: Equatable, Sendable {
        case valid(TargetsUpdate)
        case invalid([Field: String])
    }

    public var calories = ""
    public var protein = ""
    public var carbs = ""
    public var fat = ""

    public init() {}

    /// The text for a user's saved targets: plain numbers, blank where no target is set.
    public init(user: Components.Schemas.User) {
        calories = user.targetKcal.map(plainNumber) ?? ""
        protein = user.targetProteinG.map(plainNumber) ?? ""
        carbs = user.targetCarbsG.map(plainNumber) ?? ""
        fat = user.targetFatG.map(plainNumber) ?? ""
    }

    private struct Rule {
        let max: Double
        let positive: Bool
        let over: String
        let example: String
    }

    // The API's limits (`UpdateProfileRequest`): calories above 0; macros from 0.
    private static func rule(for field: Field) -> Rule {
        switch field {
        case .calories: Rule(max: 20000, positive: true, over: "Calories must be more than 0 and at most 20000.", example: "2000")
        case .protein: Rule(max: 2000, positive: false, over: "Protein must be at most 2000 g.", example: "70")
        case .carbs: Rule(max: 5000, positive: false, over: "Carbohydrates must be at most 5000 g.", example: "250")
        case .fat: Rule(max: 2000, positive: false, over: "Fat must be at most 2000 g.", example: "70")
        }
    }

    public func validate() -> Validation {
        var errors: [Field: String] = [:]

        func read(_ field: Field, _ text: String) -> Double? {
            let rule = Self.rule(for: field)
            switch parseDecimal(text) {
            case .blank:
                return nil
            case .invalid:
                errors[field] = "Enter a number, for example \(rule.example)."
                return nil
            case .value(let value):
                if (rule.positive && value <= 0) || value > rule.max {
                    errors[field] = rule.over
                    return nil
                }
                return value
            }
        }

        let update = TargetsUpdate(
            kcal: read(.calories, calories), protein: read(.protein, protein),
            carbs: read(.carbs, carbs), fat: read(.fat, fat)
        )
        return errors.isEmpty ? .valid(update) : .invalid(errors)
    }
}
```

- [ ] **Step 4: Run to verify it passes, then commit**

Run: `swift test --filter TargetsDraftTests`
Expected: PASS (7 tests).

```bash
git add ios/MealPlannerKit/Sources/Repositories/TargetsUpdate.swift ios/MealPlannerKit/Sources/Features/Profile/TargetsDraft.swift ios/MealPlannerKit/Tests/MealPlannerKitTests/TargetsDraftTests.swift
git commit -m "feat(ios): add the daily targets draft and its validation

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 3: `InviteCodeFormat`, `IngredientUpdate` and the form's edit mode

**Files:**
- Create: `ios/MealPlannerKit/Sources/Features/Profile/InviteCodeFormat.swift`
- Create: `ios/MealPlannerKit/Sources/Repositories/IngredientUpdate.swift`
- Modify: `ios/MealPlannerKit/Sources/Features/Shared/CustomIngredient/CustomIngredientForm.swift` (doc comment, `init(editing:)`, `UpdateValidation`, `validateUpdate(preserving:)`)
- Test: `ios/MealPlannerKit/Tests/MealPlannerKitTests/InviteCodeFormatTests.swift`, `CustomIngredientEditTests.swift`

**Interfaces:**
- Consumes: `CustomIngredientForm.validate()` and its `Field`/`Validation` (existing), `plainNumber`, `ProfileFixtures` (Task 1).
- Produces: `InviteCodeFormat.display(_:) -> String`, `InviteCodeFormat.spoken(_:) -> String`; `IngredientUpdate(name:category:gramsPerPiece:densityGPerMl:nutrients:)` (`gramsPerPiece`/`densityGPerMl` `nil` = clear; `nutrients: NutrientAmountsInput`); `CustomIngredientForm.init(editing: Components.Schemas.Ingredient)`; `CustomIngredientForm.UpdateValidation` (`.valid(IngredientUpdate)` / `.invalid([Field: String])`); `CustomIngredientForm.validateUpdate(preserving:) -> UpdateValidation`.

- [ ] **Step 1: Write the failing tests**

`InviteCodeFormatTests.swift`:

```swift
import Testing
@testable import Features

@Suite
struct InviteCodeFormatTests {
    @Test("An 8-character code is shown in capitals in groups of four")
    func display() {
        #expect(InviteCodeFormat.display("abcd2345") == "ABCD-2345")
        #expect(InviteCodeFormat.display("ABCD2345") == "ABCD-2345")
    }

    @Test("Short, long, spaced and already-dashed input is handled")
    func edges() {
        #expect(InviteCodeFormat.display("") == "")
        #expect(InviteCodeFormat.display("ABC") == "ABC")
        #expect(InviteCodeFormat.display("ABCDEFGHJ") == "ABCD-EFGH-J")
        #expect(InviteCodeFormat.display(" abcd-2345 ") == "ABCD-2345")
    }

    @Test("Spoken, a code is read character by character")
    func spoken() {
        #expect(InviteCodeFormat.spoken("abcd-2345") == "A B C D 2 3 4 5")
    }
}
```

`CustomIngredientEditTests.swift`:

```swift
import API
import Repositories
import Testing
@testable import Features

@Suite
struct CustomIngredientEditTests {
    private func update(_ form: CustomIngredientForm, existing: Components.Schemas.Ingredient = ProfileFixtures.fullIngredient()) -> IngredientUpdate? {
        if case .valid(let update) = form.validateUpdate(preserving: existing) { update } else { nil }
    }

    @Test("The edit form is prefilled from the ingredient")
    func prefill() {
        let form = CustomIngredientForm(editing: ProfileFixtures.fullIngredient())
        #expect(form.name == "Apple")
        #expect(form.category == .produce)
        #expect(form.calories == "52")
        #expect(form.protein == "0.3")
        #expect(form.carbohydrates == "14")
        #expect(form.fat == "0.2")
        #expect(form.gramsPerPiece == "182")
        #expect(form.density == "")
    }

    @Test("Editing the name alone sends all 18 nutrients: the 14 the form does not show are copied from the ingredient")
    func keepsHiddenNutrients() {
        var form = CustomIngredientForm(editing: ProfileFixtures.fullIngredient())
        form.name = "Apple, raw"
        let result = update(form)
        #expect(result?.name == "Apple, raw")
        #expect(result?.nutrients == ProfileFixtures.fullInput())
    }

    @Test("The four shown nutrients come from the form; a blank one is left out (unknown), the hidden ones stay")
    func shownNutrients() {
        var form = CustomIngredientForm(editing: ProfileFixtures.fullIngredient())
        form.calories = "60"
        form.protein = ""
        let nutrients = update(form)?.nutrients
        #expect(nutrients?.calories == 60)
        #expect(nutrients?.protein == nil)
        #expect(nutrients?.carbohydrates == 14)
        #expect(nutrients?.vitaminC == 4.6)
        #expect(nutrients?.folate == 3)
    }

    @Test("A blank weight per piece or density is nil, which clears it; a value is kept")
    func conversions() {
        var form = CustomIngredientForm(editing: ProfileFixtures.fullIngredient())
        form.gramsPerPiece = ""
        form.density = "1,2"
        let result = update(form)
        #expect(result?.gramsPerPiece == nil)
        #expect(result?.densityGPerMl == 1.2)
    }

    @Test("A category change is carried")
    func category() {
        var form = CustomIngredientForm(editing: ProfileFixtures.fullIngredient())
        form.category = .beverages
        #expect(update(form)?.category == .beverages)
    }

    @Test("Invalid input is reported against its field and builds no update")
    func invalid() {
        var form = CustomIngredientForm(editing: ProfileFixtures.fullIngredient())
        form.name = "  "
        form.calories = "abc"
        guard case .invalid(let errors) = form.validateUpdate(preserving: ProfileFixtures.fullIngredient()) else {
            Issue.record("expected invalid")
            return
        }
        #expect(errors[.name] == "Give the ingredient a name.")
        #expect(errors[.calories] == "Enter a number, for example 12.5.")
    }
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `swift test --filter "InviteCodeFormatTests|CustomIngredientEditTests"`
Expected: FAIL to compile, "cannot find 'InviteCodeFormat' in scope".

- [ ] **Step 3: Implement**

`InviteCodeFormat.swift`:

```swift
/// An invite code as people read it aloud: capitals, in groups of four. Spaces and dashes in what was given are ignored.
public enum InviteCodeFormat {
    private static func characters(_ code: String) -> [Character] {
        code.uppercased().filter { !$0.isWhitespace && $0 != "-" }.map { $0 }
    }

    public static func display(_ code: String) -> String {
        let letters = characters(code)
        return stride(from: 0, to: letters.count, by: 4)
            .map { String(letters[$0 ..< min($0 + 4, letters.count)]) }
            .joined(separator: "-")
    }

    /// For VoiceOver: "A B C D 2 3 4 5", so the code is not read as a word.
    public static func spoken(_ code: String) -> String {
        characters(code).map(String.init).joined(separator: " ")
    }
}
```

`IngredientUpdate.swift`:

```swift
import API

/// An edited custom ingredient as the repository sends it. `gramsPerPiece` and `densityGPerMl` are always sent: `nil`
/// clears them. `nutrients` is the whole set (the API replaces it): a nutrient left out is unknown.
public struct IngredientUpdate: Equatable, Sendable {
    public var name: String
    public var category: Components.Schemas.IngredientCategory
    public var gramsPerPiece: Double?
    public var densityGPerMl: Double?
    public var nutrients: Components.Schemas.NutrientAmountsInput

    public init(
        name: String, category: Components.Schemas.IngredientCategory, gramsPerPiece: Double?, densityGPerMl: Double?,
        nutrients: Components.Schemas.NutrientAmountsInput
    ) {
        self.name = name
        self.category = category
        self.gramsPerPiece = gramsPerPiece
        self.densityGPerMl = densityGPerMl
        self.nutrients = nutrients
    }
}
```

`CustomIngredientForm.swift` edits:

1. Replace the doc comment above the struct:

```swift
/// The custom-ingredient form's text and validation, ported from web's `custom-ingredient.ts`. It asks for the four
/// macros only; editing an existing ingredient keeps every other nutrient it has (see `validateUpdate(preserving:)`).
```

2. Add after `public enum Validation { ... }`:

```swift
    public enum UpdateValidation: Equatable, Sendable {
        case valid(IngredientUpdate)
        case invalid([Field: String])
    }
```
(and `import Repositories` at the top of the file, next to `import API`.)

3. Add after `public init(name: String = "") { self.name = name }`:

```swift
    /// The form for editing an existing ingredient: every field prefilled from it.
    public init(editing ingredient: Components.Schemas.Ingredient) {
        name = ingredient.name
        category = ingredient.category
        calories = ingredient.nutrients.calories.map(plainNumber) ?? ""
        protein = ingredient.nutrients.protein.map(plainNumber) ?? ""
        carbohydrates = ingredient.nutrients.carbohydrates.map(plainNumber) ?? ""
        fat = ingredient.nutrients.fat.map(plainNumber) ?? ""
        gramsPerPiece = ingredient.gramsPerPiece.map(plainNumber) ?? ""
        density = ingredient.densityGPerMl.map(plainNumber) ?? ""
    }
```

4. Add after `validate()`:

```swift
    /// Validates like `validate()`, then builds the update for `existing`: `nutrients` replaces the whole set on the API,
    /// so all 18 are sent. The four shown here come from the form (a blank one is left out, meaning unknown); the other
    /// 14 are copied from `existing`, so editing a name never erases the vitamins. A blank weight per piece or density
    /// is `nil`, which clears it.
    public func validateUpdate(preserving existing: Components.Schemas.Ingredient) -> UpdateValidation {
        switch validate() {
        case .invalid(let errors):
            return .invalid(errors)
        case .valid(let request):
            let shown = request.nutrients ?? Components.Schemas.NutrientAmountsInput()
            let kept = existing.nutrients
            let nutrients = Components.Schemas.NutrientAmountsInput(
                calories: shown.calories, protein: shown.protein, carbohydrates: shown.carbohydrates,
                sugar: kept.sugar, fibre: kept.fibre, fat: shown.fat, saturatedFat: kept.saturatedFat,
                sodium: kept.sodium, potassium: kept.potassium, calcium: kept.calcium, iron: kept.iron,
                magnesium: kept.magnesium, zinc: kept.zinc, vitaminA: kept.vitaminA, vitaminC: kept.vitaminC,
                vitaminD: kept.vitaminD, vitaminB12: kept.vitaminB12, folate: kept.folate
            )
            return .valid(IngredientUpdate(
                name: request.name, category: request.category, gramsPerPiece: request.gramsPerPiece,
                densityGPerMl: request.densityGPerMl, nutrients: nutrients
            ))
        }
    }
```

- [ ] **Step 4: Run to verify they pass, then the whole suite for regressions**

Run: `swift test --filter "InviteCodeFormatTests|CustomIngredientEditTests|CustomIngredientTests"`, then `swift test`.
Expected: PASS (3 + 6 + the existing CustomIngredientTests); whole suite green.

- [ ] **Step 5: Commit**

```bash
git add ios/MealPlannerKit/Sources/Features/Profile/InviteCodeFormat.swift ios/MealPlannerKit/Sources/Repositories/IngredientUpdate.swift ios/MealPlannerKit/Sources/Features/Shared/CustomIngredient/CustomIngredientForm.swift ios/MealPlannerKit/Tests/MealPlannerKitTests/InviteCodeFormatTests.swift ios/MealPlannerKit/Tests/MealPlannerKitTests/CustomIngredientEditTests.swift
git commit -m "feat(ios): add the invite code format and the ingredient form's edit mode

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 4: `ProfileCache`, the schema, and `PlanCache.setTargets`

**Files:**
- Create: `ios/MealPlannerKit/Sources/Persistence/CachedProfileModels.swift`
- Create: `ios/MealPlannerKit/Sources/Persistence/ProfileCache.swift`
- Modify: `ios/MealPlannerKit/Sources/Persistence/CacheStore.swift` (schema, `makeProfileCache`)
- Modify: `ios/MealPlannerKit/Sources/Persistence/PlanCache.swift` (`setTargets`)
- Test: `ios/MealPlannerKit/Tests/MealPlannerKitTests/ProfileCacheTests.swift`, `PlanCacheTargetsTests.swift`

**Interfaces:**
- Consumes: `CacheStore.inMemoryContainer()`, `Fixtures.day(_:calories:)`, `Fixtures.targets(...)`, `ProfileFixtures` (Task 1).
- Produces: `ProfileCache` (`user() -> User?`, `store(user:)`, `partnership() -> CachedPartnership`, `store(partnership: Partnership?)`, `clearAll()`), `CachedPartnership` (`unknown`, `none`, `present(Partnership)`), `CacheStore.makeProfileCache(_:)`, `PlanCache.setTargets(_ targets: Components.Schemas.Targets)`.

- [ ] **Step 1: Write the failing tests**

`ProfileCacheTests.swift`:

```swift
import API
import Persistence
import SwiftData
import Testing

@Suite
struct ProfileCacheTests {
    private func make() throws -> (ProfileCache, ModelContainer) {
        let container = try CacheStore.inMemoryContainer()
        return (CacheStore.makeProfileCache(container), container)
    }

    @Test("Nothing cached is nil, and storing a user replaces the previous one")
    func user() async throws {
        let (cache, _) = try make()
        #expect(await cache.user() == nil)
        await cache.store(user: ProfileFixtures.user(email: "a@example.com", kcal: 1800))
        await cache.store(user: ProfileFixtures.user(email: "a@example.com", kcal: 2000))
        let user = try #require(await cache.user())
        #expect(user.targetKcal == 2000)
        #expect(user.email == "a@example.com")
    }

    @Test("The partnership is unknown until fetched, then none, active or pending")
    func partnership() async throws {
        let (cache, _) = try make()
        #expect(await cache.partnership() == .unknown)
        await cache.store(partnership: nil)
        #expect(await cache.partnership() == .none)
        await cache.store(partnership: ProfileFixtures.active("Alex"))
        #expect(await cache.partnership() == .present(ProfileFixtures.active("Alex")))
        await cache.store(partnership: ProfileFixtures.pending())
        #expect(await cache.partnership() == .present(ProfileFixtures.pending()))
        await cache.store(partnership: nil)
        #expect(await cache.partnership() == .none)
    }

    @Test("A new cache actor on the same store sees what the first one saved")
    func survivesRelaunch() async throws {
        let (cache, container) = try make()
        await cache.store(user: ProfileFixtures.user(kcal: 2000))
        await cache.store(partnership: ProfileFixtures.active())
        let relaunched = CacheStore.makeProfileCache(container)
        #expect(await relaunched.user()?.targetKcal == 2000)
        #expect(await relaunched.partnership() == .present(ProfileFixtures.active()))
    }

    @Test("clearAll forgets the user and the partnership, so a second user never sees them")
    func clearAll() async throws {
        let (cache, _) = try make()
        await cache.store(user: ProfileFixtures.user())
        await cache.store(partnership: ProfileFixtures.active())
        await cache.clearAll()
        #expect(await cache.user() == nil)
        #expect(await cache.partnership() == .unknown)
    }
}
```

`PlanCacheTargetsTests.swift`:

```swift
import API
import Persistence
import Testing

@Suite
struct PlanCacheTargetsTests {
    @Test("setTargets replaces only the targets: cached days stay")
    func replacesTargetsOnly() async throws {
        let cache = CacheStore.makePlanCache(try CacheStore.inMemoryContainer())
        await cache.replace(days: [Fixtures.day("2026-10-05", calories: 7)], targets: Fixtures.targets(kcal: 1800))
        await cache.setTargets(.init(targetKcal: 2000, targetProteinG: nil, targetCarbsG: 250, targetFatG: 70))
        let targets = try #require(await cache.targets())
        #expect(targets.targetKcal == 2000)
        #expect(targets.targetProteinG == nil)
        #expect(targets.targetCarbsG == 250)
        #expect(await cache.days(from: "2026-10-05", to: "2026-10-05").count == 1)
    }

    @Test("setTargets creates the targets row when there is none yet")
    func createsRow() async throws {
        let cache = CacheStore.makePlanCache(try CacheStore.inMemoryContainer())
        #expect(await cache.targets() == nil)
        await cache.setTargets(.init(targetKcal: 1500))
        #expect(await cache.targets()?.targetKcal == 1500)
    }
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `swift test --filter "ProfileCacheTests|PlanCacheTargetsTests"`
Expected: FAIL to compile, "cannot find 'makeProfileCache'" / "no member 'setTargets'".

- [ ] **Step 3: Implement**

`CachedProfileModels.swift`:

```swift
import Foundation
import SwiftData

/// Internal to this module: `@Model` objects never leave `ProfileCache`.

/// The signed-in user, one row, as the generated `User` JSON.
@Model
final class CachedProfileUser {
    @Attribute(.unique) var key: String
    var json: Data

    init(json: Data) {
        self.key = "me"
        self.json = json
    }
}

/// The partnership, one row. `json == nil` means "fetched, and there is none"; no row at all means "never fetched".
@Model
final class CachedPartnershipRow {
    @Attribute(.unique) var key: String
    var json: Data?

    init(json: Data?) {
        self.key = "me"
        self.json = json
    }
}
```

`ProfileCache.swift`:

```swift
import API
import Foundation
import SwiftData

/// What the partnership cache knows. A pending invite is a partnership too (it is not "linked").
public enum CachedPartnership: Equatable, Sendable {
    /// Never fetched on this device.
    case unknown
    /// Fetched, and there is no partner and no pending invite.
    case none
    case present(Components.Schemas.Partnership)
}

/// The Profile tab's cache: the signed-in user and the partnership. Takes and returns generated value types;
/// best-effort like the other caches (rebuildable from the API, so a failed save is dropped, not surfaced).
@ModelActor
public actor ProfileCache {
    public func user() -> Components.Schemas.User? {
        guard let row = userRow() else { return nil }
        return try? JSONDecoder().decode(Components.Schemas.User.self, from: row.json)
    }

    public func store(user: Components.Schemas.User) {
        guard let json = try? JSONEncoder().encode(user) else { return }
        if let row = userRow() {
            row.json = json
        } else {
            modelContext.insert(CachedProfileUser(json: json))
        }
        try? modelContext.save()
    }

    public func partnership() -> CachedPartnership {
        guard let row = partnershipRow() else { return .unknown }
        guard let json = row.json else { return .none }
        guard let value = try? JSONDecoder().decode(Components.Schemas.Partnership.self, from: json) else { return .unknown }
        return .present(value)
    }

    /// `nil` records "fetched, and there is none".
    public func store(partnership: Components.Schemas.Partnership?) {
        let json = partnership.flatMap { try? JSONEncoder().encode($0) }
        if let row = partnershipRow() {
            row.json = json
        } else {
            modelContext.insert(CachedPartnershipRow(json: json))
        }
        try? modelContext.save()
    }

    public func clearAll() {
        for row in (try? modelContext.fetch(FetchDescriptor<CachedProfileUser>())) ?? [] { modelContext.delete(row) }
        for row in (try? modelContext.fetch(FetchDescriptor<CachedPartnershipRow>())) ?? [] { modelContext.delete(row) }
        try? modelContext.save()
    }

    private func userRow() -> CachedProfileUser? {
        (try? modelContext.fetch(FetchDescriptor<CachedProfileUser>()))?.first
    }

    private func partnershipRow() -> CachedPartnershipRow? {
        (try? modelContext.fetch(FetchDescriptor<CachedPartnershipRow>()))?.first
    }
}
```

`CacheStore.swift`: add the two models to the schema and the factory.

```swift
    private static let schema = Schema([
        CachedMeal.self, CachedMealIngredient.self, CachedPlanDay.self, CachedTargets.self,
        CachedTemplate.self, CachedShoppingSummary.self, CachedShoppingList.self, CachedIntent.self,
        CachedProfileUser.self, CachedPartnershipRow.self,
    ])
```

```swift
    public static func makeProfileCache(_ container: ModelContainer) -> ProfileCache {
        ProfileCache(modelContainer: container)
    }
```
(Adding two entities is a lightweight migration: the store keeps its rows, including unsynced shopping changes.)

`PlanCache.swift`: add after `targets()`:

```swift
    /// Writes only the targets row. The Today rings read targets from here; `PATCH /me` answers with the new ones, so
    /// they are stored without refetching the plan.
    public func setTargets(_ targets: Components.Schemas.Targets) {
        guard let json = try? JSONEncoder().encode(targets) else { return }
        if let row = (try? modelContext.fetch(FetchDescriptor<CachedTargets>()))?.first {
            row.json = json
        } else {
            modelContext.insert(CachedTargets(json: json))
        }
        try? modelContext.save()
    }
```

- [ ] **Step 4: Run to verify they pass, then the whole suite**

Run: `swift test --filter "ProfileCacheTests|PlanCacheTargetsTests"`, then `swift test`.
Expected: PASS (4 + 2); whole suite green (the schema change must not break the other cache suites).

- [ ] **Step 5: Commit**

```bash
git add ios/MealPlannerKit/Sources/Persistence ios/MealPlannerKit/Tests/MealPlannerKitTests/ProfileCacheTests.swift ios/MealPlannerKit/Tests/MealPlannerKitTests/PlanCacheTargetsTests.swift
git commit -m "feat(ios): add ProfileCache for the user and partnership, and PlanCache.setTargets

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 5: `ProfileRepository`, `ProfileError`, `PlanRepository.storeTargets`

**Files:**
- Create: `ios/MealPlannerKit/Sources/Repositories/ProfileError.swift`
- Create: `ios/MealPlannerKit/Sources/Repositories/ProfileRepository.swift`
- Modify: `ios/MealPlannerKit/Sources/Repositories/PlanRepository.swift` (`storeTargets`)
- Modify: `ios/MealPlannerKit/Sources/Features/Shared/ErrorText.swift` (`ProfileError` case)
- Test: `ios/MealPlannerKit/Tests/MealPlannerKitTests/ProfileRepositoryTests.swift`

**Interfaces:**
- Consumes: `ProfileCache` (Task 4), `TargetsUpdate` (Task 2), `NullSentinel`/`NullSentinelMiddleware` (Task 1), `unwrapping`, `ProblemText`, `PlanCache.setTargets` (Task 4).
- Produces:
  - `ProfileError`: `validationFailed(fields: [String: String], message: String)`, `unauthorized`, `rateLimited`, `server(String)`.
  - `ProfileRepository(client:cache:)`: `cachedUser() async -> User?`, `refreshUser() async throws -> User` (discardable), `updateTargets(_:) async throws -> User`, `deleteAccount() async throws`, `clearCaches() async`.
  - `PlanRepository.storeTargets(from user: Components.Schemas.User) async`.

- [ ] **Step 1: Write the failing tests** — `ProfileRepositoryTests.swift`

```swift
import API
import Foundation
import Persistence
import Testing
@testable import Repositories

@Suite
struct ProfileRepositoryTests {
    private func make(
        withSentinel: Bool = false, _ route: @escaping RoutingTransport.Route
    ) throws -> (ProfileRepository, ProfileCache, RoutingTransport) {
        let transport = RoutingTransport(route)
        let cache = CacheStore.makeProfileCache(try CacheStore.inMemoryContainer())
        let client = withSentinel
            ? makeClient(transport: transport, middlewares: [NullSentinelMiddleware()])
            : makeAuthlessClient(transport: transport)
        return (ProfileRepository(client: client, cache: cache), cache, transport)
    }

    @Test("refreshUser stores the user and cachedUser returns it")
    func refresh() async throws {
        let (repo, _, _) = try make { _ in (200, Fixtures.json(ProfileFixtures.user(kcal: 1800))) }
        #expect(await repo.cachedUser() == nil)
        let user = try await repo.refreshUser()
        #expect(user.targetKcal == 1800)
        #expect(await repo.cachedUser()?.targetKcal == 1800)
    }

    @Test("A failed refresh leaves the cache untouched and says what went wrong")
    func failedRefresh() async throws {
        let mode = Locked(500)
        let (repo, cache, _) = try make { _ in
            mode.value == 200 ? (200, Fixtures.json(ProfileFixtures.user(kcal: 1800))) : (mode.value, Fixtures.problem(mode.value, code: "x"))
        }
        mode.set(200)
        try await repo.refreshUser()
        mode.set(500)
        await #expect(throws: ProfileError.server("The server had a problem loading your profile.")) { try await repo.refreshUser() }
        mode.set(401)
        await #expect(throws: ProfileError.unauthorized) { try await repo.refreshUser() }
        mode.set(429)
        await #expect(throws: ProfileError.rateLimited) { try await repo.refreshUser() }
        #expect(await cache.user()?.targetKcal == 1800)
    }

    @Test("updateTargets sends all four fields; a cleared one is the sentinel when no middleware runs")
    func updateSendsAllFour() async throws {
        let (repo, _, transport) = try make { _ in (200, Fixtures.json(ProfileFixtures.user(kcal: 2000, protein: 70))) }
        _ = try await repo.updateTargets(TargetsUpdate(kcal: 2000, protein: 70, carbs: nil, fat: nil))
        let body = try #require(await transport.calls("PATCH /me").first).body
        #expect(body.contains("\"target_kcal\":2000"))
        #expect(body.contains("\"target_protein_g\":70"))
        #expect(body.contains("\"target_carbs_g\":-1"))
        #expect(body.contains("\"target_fat_g\":-1"))
    }

    @Test("Review focus 1: through the middleware a cleared target reaches the wire as null, and the answer is stored")
    func clearReachesTheWireAsNull() async throws {
        let (repo, cache, transport) = try make(withSentinel: true) { _ in (200, Fixtures.json(ProfileFixtures.user(kcal: nil, protein: 70))) }
        let saved = try await repo.updateTargets(TargetsUpdate(kcal: nil, protein: 70, carbs: nil, fat: nil))
        let body = try #require(await transport.calls("PATCH /me").first).body
        #expect(body.contains("\"target_kcal\":null"))
        #expect(body.contains("\"target_carbs_g\":null"))
        #expect(body.contains("\"target_protein_g\":70"))
        #expect(!body.contains("-1"))
        #expect(saved.targetKcal == nil)
        #expect(await cache.user()?.targetProteinG == 70)
    }

    @Test("A 400 becomes field messages keyed by the server's path, and the cache is untouched")
    func updateValidation() async throws {
        let mode = Locked(200)
        let (repo, cache, _) = try make { _ in
            mode.value == 200
                ? (200, Fixtures.json(ProfileFixtures.user(kcal: 1800)))
                : (400, Fixtures.problem(400, code: "validation_failed", errors: [("target_kcal", "invalid_value")]))
        }
        try await repo.refreshUser()
        mode.set(400)
        await #expect(throws: ProfileError.validationFailed(fields: ["target_kcal": "Target kcal is invalid."], message: "Target kcal is invalid.")) {
            try await repo.updateTargets(TargetsUpdate(kcal: 1, protein: nil, carbs: nil, fat: nil))
        }
        #expect(await cache.user()?.targetKcal == 1800)
    }

    @Test("deleteAccount succeeds on 204 and reports other answers; it never touches the cache itself")
    func delete() async throws {
        let mode = Locked(204)
        let (repo, cache, transport) = try make { _ in (mode.value, mode.value == 204 ? "" : Fixtures.problem(mode.value, code: "x")) }
        await cache.store(user: ProfileFixtures.user())
        try await repo.deleteAccount()
        #expect(await transport.calls("DELETE /me").count == 1)
        mode.set(500)
        await #expect(throws: ProfileError.server("The server had a problem deleting your account.")) { try await repo.deleteAccount() }
        mode.set(401)
        await #expect(throws: ProfileError.unauthorized) { try await repo.deleteAccount() }
        #expect(await cache.user() != nil)
    }

    @Test("clearCaches empties the profile cache")
    func clear() async throws {
        let (repo, cache, _) = try make { _ in (500, "") }
        await cache.store(user: ProfileFixtures.user())
        await repo.clearCaches()
        #expect(await cache.user() == nil)
    }

    @Test("PlanRepository.storeTargets feeds the plan cache from a saved user, leaving the cached days")
    func storeTargets() async throws {
        let planCache = CacheStore.makePlanCache(try CacheStore.inMemoryContainer())
        let plan = PlanRepository(client: makeAuthlessClient(transport: RoutingTransport { _ in (500, "") }), cache: planCache)
        await planCache.replace(days: [Fixtures.day("2026-10-05")], targets: Fixtures.targets(kcal: 1800))
        await plan.storeTargets(from: ProfileFixtures.user(kcal: 2000, protein: nil, carbs: 250, fat: 70))
        let snapshot = await plan.cached(from: "2026-10-05", to: "2026-10-05")
        #expect(snapshot.targets?.targetKcal == 2000)
        #expect(snapshot.targets?.targetProteinG == nil)
        #expect(snapshot.days.count == 1)
    }
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `swift test --filter ProfileRepositoryTests`
Expected: FAIL to compile, "cannot find 'ProfileRepository' in scope".

- [ ] **Step 3: Implement**

`ProfileError.swift`:

```swift
import API

public enum ProfileError: Error, Equatable, Sendable {
    /// `fields` maps a server field path (`target_kcal`) to its message so the form can show it inline.
    case validationFailed(fields: [String: String], message: String)
    case unauthorized
    case rateLimited
    case server(String)

    static func unexpected(_ status: Int) -> ProfileError { .server("Unexpected response (\(status)).") }

    static func validation(_ problem: Components.Schemas.Problem?) -> ProfileError {
        guard let problem else { return .validationFailed(fields: [:], message: "The request was not accepted.") }
        return .validationFailed(fields: ProblemText.fieldMessages(problem), message: ProblemText.validation(problem))
    }
}
```

`ProfileRepository.swift`:

```swift
import API
import Foundation
import Persistence

/// The signed-in user's profile, cache-first: `cachedUser()` then `refreshUser()`. Writes are online-only: each answer
/// replaces the cached user. `PATCH /me` clears a target with an explicit `null`, which the generated types cannot
/// send, so a cleared target goes out as `NullSentinel.value` and `NullSentinelMiddleware` rewrites it.
public struct ProfileRepository: Sendable {
    private let client: Client
    private let cache: ProfileCache

    public init(client: Client, cache: ProfileCache) {
        self.client = client
        self.cache = cache
    }

    public func cachedUser() async -> Components.Schemas.User? { await cache.user() }

    /// `GET /me`; the answer replaces the cached user. A failure leaves the cache untouched.
    @discardableResult
    public func refreshUser() async throws -> Components.Schemas.User {
        let response = try await unwrapping { try await client.getMe(.init()) }
        switch response {
        case .ok(let ok): return await keep(try ok.body.json)
        case .unauthorized: throw ProfileError.unauthorized
        case .tooManyRequests: throw ProfileError.rateLimited
        case .internalServerError: throw ProfileError.server("The server had a problem loading your profile.")
        case .undocumented(let status, _): throw ProfileError.unexpected(status)
        }
    }

    /// Saves all four targets (`nil` clears one). Returns, and caches, the updated user.
    public func updateTargets(_ update: TargetsUpdate) async throws -> Components.Schemas.User {
        let body = Components.Schemas.UpdateProfileRequest(
            targetKcal: update.kcal ?? NullSentinel.value,
            targetProteinG: update.protein ?? NullSentinel.value,
            targetCarbsG: update.carbs ?? NullSentinel.value,
            targetFatG: update.fat ?? NullSentinel.value
        )
        let response = try await unwrapping { try await client.updateMe(.init(body: .json(body))) }
        switch response {
        case .ok(let ok): return await keep(try ok.body.json)
        case .badRequest(let r): throw ProfileError.validation(r.problem)
        case .unauthorized: throw ProfileError.unauthorized
        case .tooManyRequests: throw ProfileError.rateLimited
        case .internalServerError: throw ProfileError.server("The server had a problem saving your targets.")
        case .undocumented(let status, _): throw ProfileError.unexpected(status)
        }
    }

    /// `DELETE /me`: permanent. Does not touch the cache; the caller signs out, which clears every cache.
    public func deleteAccount() async throws {
        let response = try await unwrapping { try await client.deleteMe(.init()) }
        switch response {
        case .noContent: return
        case .unauthorized: throw ProfileError.unauthorized
        case .tooManyRequests: throw ProfileError.rateLimited
        case .internalServerError: throw ProfileError.server("The server had a problem deleting your account.")
        case .undocumented(let status, _): throw ProfileError.unexpected(status)
        }
    }

    /// Called whenever the session ends, so a second user on this device never sees the first user's profile.
    public func clearCaches() async { await cache.clearAll() }

    private func keep(_ user: Components.Schemas.User) async -> Components.Schemas.User {
        await cache.store(user: user)
        return user
    }
}
```

`PlanRepository.swift`: add before `clearCaches()`:

```swift
    /// After `PATCH /me`: the Today rings read their targets from this cache, so the saved answer is written here too
    /// and the plan does not have to be refetched.
    public func storeTargets(from user: Components.Schemas.User) async {
        await cache.setTargets(.init(
            targetKcal: user.targetKcal, targetProteinG: user.targetProteinG,
            targetCarbsG: user.targetCarbsG, targetFatG: user.targetFatG
        ))
    }
```

`ErrorText.swift`: add above `case is URLError:`:

```swift
        case let error as ProfileError:
            switch error {
            case .validationFailed(_, let message): return message
            case .unauthorized: return "Please sign in again."
            case .rateLimited: return rateLimited
            case .server(let message): return message
            }
```

- [ ] **Step 4: Run to verify it passes, then commit**

Run: `swift test --filter ProfileRepositoryTests`
Expected: PASS (8 tests). If `ProblemText`'s wording differs from "Target kcal is invalid.", print the actual `ProfileError` from the failing assertion and match the existing helper's output rather than changing the helper.

```bash
git add ios/MealPlannerKit/Sources/Repositories ios/MealPlannerKit/Sources/Features/Shared/ErrorText.swift ios/MealPlannerKit/Tests/MealPlannerKitTests/ProfileRepositoryTests.swift
git commit -m "feat(ios): add ProfileRepository and storing saved targets in the plan cache

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 6: `PartnerRepository`: cache, invite, accept, unlink

**Files:**
- Create: `ios/MealPlannerKit/Sources/Repositories/PartnerError.swift`
- Modify: `ios/MealPlannerKit/Sources/Repositories/PartnerRepository.swift` (replace)
- Modify: `ios/MealPlannerKit/Sources/Features/Shared/ErrorText.swift` (`PartnerError` case)
- Test: `ios/MealPlannerKit/Tests/MealPlannerKitTests/PartnerRepositoryProfileTests.swift`

**Interfaces:**
- Consumes: `ProfileCache`, `CachedPartnership` (Task 4), `ProblemText`, `unwrapping`, `MealsError` (for `status()`, unchanged).
- Produces: `PartnerError` (`inviteInvalid`, `alreadyLinked`, `validationFailed(String)`, `unauthorized`, `rateLimited`, `server(String)`); `PartnerRepository(client:cache: ProfileCache? = nil)` with `status() async throws -> Partnership?` (unchanged signature; now stores), `cachedStatus() async -> CachedPartnership`, `createInvite() async throws -> PartnerInvite`, `accept(code:) async throws -> Partnership`, `unlink() async throws`.

- [ ] **Step 1: Write the failing tests** — `PartnerRepositoryProfileTests.swift`

```swift
import API
import Foundation
import Persistence
import Testing
@testable import Repositories

@Suite
struct PartnerRepositoryProfileTests {
    private func make(_ route: @escaping RoutingTransport.Route) throws -> (PartnerRepository, ProfileCache, RoutingTransport) {
        let transport = RoutingTransport(route)
        let cache = CacheStore.makeProfileCache(try CacheStore.inMemoryContainer())
        return (PartnerRepository(client: makeAuthlessClient(transport: transport), cache: cache), cache, transport)
    }

    @Test("status stores what the server said: active, pending, or none on 404")
    func statusStores() async throws {
        let mode = Locked("active")
        let (repo, _, _) = try make { _ in
            switch mode.value {
            case "active": return (200, Fixtures.json(ProfileFixtures.active("Alex")))
            case "pending": return (200, Fixtures.json(ProfileFixtures.pending()))
            default: return (404, Fixtures.problem(404, code: "partner_not_linked"))
            }
        }
        #expect(await repo.cachedStatus() == .unknown)
        _ = try await repo.status()
        #expect(await repo.cachedStatus() == .present(ProfileFixtures.active("Alex")))
        mode.set("pending")
        _ = try await repo.status()
        #expect(await repo.cachedStatus() == .present(ProfileFixtures.pending()))
        mode.set("none")
        #expect(try await repo.status() == nil)
        #expect(await repo.cachedStatus() == .none)
    }

    @Test("A failing status leaves the cached answer alone; without a cache nothing is stored and status is unknown")
    func statusFailureAndNoCache() async throws {
        let (repo, cache, _) = try make { _ in (500, Fixtures.problem(500, code: "internal")) }
        await cache.store(partnership: ProfileFixtures.active())
        await #expect(throws: MealsError.server("The server had a problem loading your partnership.")) { try await repo.status() }
        #expect(await repo.cachedStatus() == .present(ProfileFixtures.active()))

        let bare = PartnerRepository(client: makeAuthlessClient(transport: RoutingTransport { _ in (404, Fixtures.problem(404, code: "partner_not_linked")) }))
        #expect(try await bare.status() == nil)
        #expect(await bare.cachedStatus() == .unknown)
    }

    @Test("createInvite returns the one-time code; an existing partner is alreadyLinked")
    func createInvite() async throws {
        let mode = Locked(201)
        let (repo, _, transport) = try make { _ in
            mode.value == 201
                ? (201, Fixtures.json(ProfileFixtures.invite(code: "ABCD2345")))
                : (mode.value, Fixtures.problem(mode.value, code: mode.value == 409 ? "partner_already_linked" : "x"))
        }
        #expect(try await repo.createInvite().code == "ABCD2345")
        #expect(await transport.calls("POST /partner/invite").count == 1)
        mode.set(409)
        await #expect(throws: PartnerError.alreadyLinked) { try await repo.createInvite() }
        mode.set(429)
        await #expect(throws: PartnerError.rateLimited) { try await repo.createInvite() }
    }

    @Test("accept sends the code as given and stores the new link")
    func accept() async throws {
        let (repo, _, transport) = try make { _ in (200, Fixtures.json(ProfileFixtures.active("Alex"))) }
        let partnership = try await repo.accept(code: "ab-cd 2345")
        #expect(partnership.status == .active)
        #expect(partnership.displayName == "Alex")
        #expect(try #require(await transport.calls("POST /partner/accept").first).body.contains("\"code\":\"ab-cd 2345\""))
        #expect(await repo.cachedStatus() == .present(ProfileFixtures.active("Alex")))
    }

    @Test("Review focus 6: a wrong code, an existing partner, a rate limit and a bad request each read differently")
    func acceptErrors() async throws {
        let mode = Locked(404)
        let (repo, cache, _) = try make { _ in
            switch mode.value {
            case 404: return (404, Fixtures.problem(404, code: "invite_invalid"))
            case 409: return (409, Fixtures.problem(409, code: "partner_already_linked"))
            case 400: return (400, Fixtures.problem(400, code: "validation_failed", errors: [("code", "required")]))
            case 429: return (429, Fixtures.problem(429, code: "too_many_requests"))
            default: return (409, Fixtures.problem(409, code: "something_else", title: "Conflict"))
            }
        }
        await cache.store(partnership: nil)
        await #expect(throws: PartnerError.inviteInvalid) { try await repo.accept(code: "x") }
        mode.set(409)
        await #expect(throws: PartnerError.alreadyLinked) { try await repo.accept(code: "x") }
        mode.set(400)
        await #expect(throws: PartnerError.validationFailed("Code is required.")) { try await repo.accept(code: "") }
        mode.set(429)
        await #expect(throws: PartnerError.rateLimited) { try await repo.accept(code: "x") }
        mode.set(0)
        await #expect(throws: PartnerError.server("Conflict")) { try await repo.accept(code: "x") }
        #expect(await repo.cachedStatus() == .none) // nothing above changed the cache
    }

    @Test("unlink stores 'none'; a link that is already gone (404) is a success; other failures throw")
    func unlink() async throws {
        let mode = Locked(204)
        let (repo, cache, _) = try make { _ in
            mode.value == 204 ? (204, "") : (mode.value, Fixtures.problem(mode.value, code: mode.value == 404 ? "partner_not_linked" : "internal"))
        }
        await cache.store(partnership: ProfileFixtures.active())
        try await repo.unlink()
        #expect(await repo.cachedStatus() == .none)

        await cache.store(partnership: ProfileFixtures.pending())
        mode.set(404)
        try await repo.unlink()
        #expect(await repo.cachedStatus() == .none)

        await cache.store(partnership: ProfileFixtures.active())
        mode.set(500)
        await #expect(throws: PartnerError.server("The server had a problem ending the link.")) { try await repo.unlink() }
        #expect(await repo.cachedStatus() == .present(ProfileFixtures.active()))
    }
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `swift test --filter PartnerRepositoryProfileTests`
Expected: FAIL to compile, "extra argument 'cache' in call".

- [ ] **Step 3: Implement**

`PartnerError.swift`:

```swift
import API

public enum PartnerError: Error, Equatable, Sendable {
    /// `404 invite_invalid`: a wrong, expired, own or already used code (the server answers all four the same).
    case inviteInvalid
    /// `409 partner_already_linked`.
    case alreadyLinked
    case validationFailed(String)
    case unauthorized
    case rateLimited
    case server(String)

    static func unexpected(_ status: Int) -> PartnerError { .server("Unexpected response (\(status)).") }

    static func validation(_ problem: Components.Schemas.Problem?) -> PartnerError {
        .validationFailed(problem.map(ProblemText.validation) ?? "The request was not accepted.")
    }

    static func conflict(_ problem: Components.Schemas.Problem?) -> PartnerError {
        if problem?.code == "partner_already_linked" { return .alreadyLinked }
        return .server(problem?.detail ?? problem?.title ?? "The request conflicts with the current state.")
    }
}
```

`PartnerRepository.swift` (replace the file):

```swift
import API
import Persistence

/// The partnership. `status()` is used by the Meals, Plan, Shopping and Profile tabs; when a `ProfileCache` is given it
/// also remembers the answer, so Profile opens with the last known state. Writes are online-only.
public struct PartnerRepository: Sendable {
    private let client: Client
    private let cache: ProfileCache?

    public init(client: Client, cache: ProfileCache? = nil) {
        self.client = client
        self.cache = cache
    }

    /// The caller's partnership, or `nil` on `404 partner_not_linked` (no partner, no pending invite,
    /// or an expired one). Callers treat only `status == .active` as linked.
    public func status() async throws -> Components.Schemas.Partnership? {
        let response = try await unwrapping { try await client.getPartner(.init()) }
        switch response {
        case .ok(let ok):
            let partnership = try ok.body.json
            await cache?.store(partnership: partnership)
            return partnership
        case .notFound:
            await cache?.store(partnership: nil)
            return nil
        case .unauthorized: throw MealsError.unauthorized
        case .tooManyRequests: throw MealsError.rateLimited
        case .internalServerError: throw MealsError.server("The server had a problem loading your partnership.")
        case .undocumented(let status, _): throw MealsError.unexpected(status)
        }
    }

    /// What was last known, without a request. `.unknown` when there is no cache or nothing was ever fetched.
    public func cachedStatus() async -> CachedPartnership { await cache?.partnership() ?? .unknown }

    /// Creates an invite. The code is returned once; the server keeps only its hash.
    public func createInvite() async throws -> Components.Schemas.PartnerInvite {
        let response = try await unwrapping { try await client.createPartnerInvite(.init()) }
        switch response {
        case .created(let created): return try created.body.json
        case .unauthorized: throw PartnerError.unauthorized
        case .conflict(let r): throw PartnerError.conflict(r.problem)
        case .tooManyRequests: throw PartnerError.rateLimited
        case .internalServerError: throw PartnerError.server("The server had a problem creating the invite.")
        case .undocumented(let status, _): throw PartnerError.unexpected(status)
        }
    }

    /// Links with whoever issued `code`. The server ignores case, spaces and dashes, so the text goes as given.
    public func accept(code: String) async throws -> Components.Schemas.Partnership {
        let response = try await unwrapping { try await client.acceptPartnerInvite(.init(body: .json(.init(code: code)))) }
        switch response {
        case .ok(let ok):
            let partnership = try ok.body.json
            await cache?.store(partnership: partnership)
            return partnership
        case .badRequest(let r): throw PartnerError.validation(r.problem)
        case .unauthorized: throw PartnerError.unauthorized
        case .notFound: throw PartnerError.inviteInvalid
        case .conflict(let r): throw PartnerError.conflict(r.problem)
        case .tooManyRequests: throw PartnerError.rateLimited
        case .internalServerError: throw PartnerError.server("The server had a problem linking your accounts.")
        case .undocumented(let status, _): throw PartnerError.unexpected(status)
        }
    }

    /// Ends the link or cancels a pending invite. Either side may do it. Nothing to end (`404`) counts as done.
    public func unlink() async throws {
        let response = try await unwrapping { try await client.unlinkPartner(.init()) }
        switch response {
        case .noContent, .notFound: await cache?.store(partnership: nil)
        case .unauthorized: throw PartnerError.unauthorized
        case .tooManyRequests: throw PartnerError.rateLimited
        case .internalServerError: throw PartnerError.server("The server had a problem ending the link.")
        case .undocumented(let status, _): throw PartnerError.unexpected(status)
        }
    }
}
```

`ErrorText.swift`: add above `case is URLError:`:

```swift
        case let error as PartnerError:
            switch error {
            case .inviteInvalid: return "That code didn't work. Check it, or ask your partner for a new one."
            case .alreadyLinked: return "You're already linked with a partner."
            case .validationFailed(let message): return message
            case .unauthorized: return "Please sign in again."
            case .rateLimited: return "Too many tries. Wait a minute and try again."
            case .server(let message): return message
            }
```

- [ ] **Step 4: Run to verify it passes, then the whole suite**

Run: `swift test --filter "PartnerRepositoryProfileTests|IngredientsPartnerRepositoryTests"`, then `swift test`.
Expected: PASS (6 new plus the existing partner tests); whole suite green (the default `cache: nil` keeps the other call sites compiling).

- [ ] **Step 5: Commit**

```bash
git add ios/MealPlannerKit/Sources/Repositories ios/MealPlannerKit/Sources/Features/Shared/ErrorText.swift ios/MealPlannerKit/Tests/MealPlannerKitTests/PartnerRepositoryProfileTests.swift
git commit -m "feat(ios): add partner invite, accept and unlink, and cache the partnership

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 7: `IngredientsRepository`: list mine, update, delete

**Files:**
- Modify: `ios/MealPlannerKit/Sources/Repositories/MealsError.swift` (`IngredientError` cases)
- Modify: `ios/MealPlannerKit/Sources/Repositories/IngredientsRepository.swift`
- Modify: `ios/MealPlannerKit/Sources/Features/Shared/ErrorText.swift` (new `IngredientError` cases)
- Test: `ios/MealPlannerKit/Tests/MealPlannerKitTests/IngredientsRepositoryProfileTests.swift`

**Interfaces:**
- Consumes: `IngredientUpdate` (Task 3), `NullSentinel`/`NullSentinelMiddleware` (Task 1), `ProfileFixtures.ingredientPage`/`.customIngredient`/`.fullInput` (Task 1), existing `IngredientsRepository.validation(_:)` (private static).
- Produces: `IngredientError.notFound`, `.inUse`, `.unitInUse`; `IngredientsRepository.customIngredients() async throws -> [Ingredient]`, `.update(id:_:) async throws -> Ingredient`, `.delete(id:) async throws`.

- [ ] **Step 1: Write the failing tests** — `IngredientsRepositoryProfileTests.swift`

```swift
import API
import Foundation
import Testing
@testable import Repositories

@Suite
struct IngredientsRepositoryProfileTests {
    private func make(withSentinel: Bool = false, _ route: @escaping RoutingTransport.Route) -> (IngredientsRepository, RoutingTransport) {
        let transport = RoutingTransport(route)
        let client = withSentinel
            ? makeClient(transport: transport, middlewares: [NullSentinelMiddleware()])
            : makeAuthlessClient(transport: transport)
        return (IngredientsRepository(client: client), transport)
    }

    private let change = IngredientUpdate(
        name: "Apple, raw", category: .produce, gramsPerPiece: nil, densityGPerMl: 1.2, nutrients: ProfileFixtures.fullInput()
    )

    @Test("customIngredients walks every page at 100 and keeps only the custom ones")
    func listsMine() async throws {
        let (repo, transport) = make { call in
            if call.path.contains("cursor=c2") {
                return (200, ProfileFixtures.ingredientPage([ProfileFixtures.customIngredient(id: "c", name: "Cherry jam")]))
            }
            return (200, ProfileFixtures.ingredientPage(
                [ProfileFixtures.customIngredient(id: "a", name: "Apple"), Fixtures.ingredient(id: "g1", name: "Rice", isCustom: false)],
                next: "c2"
            ))
        }
        let mine = try await repo.customIngredients()
        #expect(mine.map(\.id) == ["a", "c"])
        let paths = await transport.calls("GET /ingredients").map(\.path)
        #expect(paths.count == 2)
        #expect(paths.allSatisfy { $0.contains("limit=100") })
    }

    @Test("A failed page fails the whole list")
    func listFailure() async throws {
        let (repo, _) = make { _ in (500, Fixtures.problem(500, code: "internal")) }
        await #expect(throws: IngredientError.server("The server had a problem loading ingredients.")) { try await repo.customIngredients() }
    }

    @Test("update sends the whole change; a cleared weight per piece is the sentinel when no middleware runs")
    func updateBody() async throws {
        let (repo, transport) = make { _ in (200, Fixtures.json(ProfileFixtures.customIngredient(name: "Apple, raw"))) }
        let saved = try await repo.update(id: "ing-1", change)
        #expect(saved.name == "Apple, raw")
        let body = try #require(await transport.calls("PATCH /ingredients/ing-1").first).body
        #expect(body.contains("\"name\":\"Apple, raw\""))
        #expect(body.contains("\"category\":\"produce\""))
        #expect(body.contains("\"grams_per_piece\":-1"))
        #expect(body.contains("\"density_g_per_ml\":1.2"))
        #expect(body.contains("\"vitamin_c\":4.6"))
        #expect(body.contains("\"folate\":3"))
    }

    @Test("Review focus 2: through the middleware a cleared weight per piece is null on the wire and the other nutrients stay")
    func updateClearsThroughMiddleware() async throws {
        let (repo, transport) = make(withSentinel: true) { _ in (200, Fixtures.json(ProfileFixtures.customIngredient())) }
        _ = try await repo.update(id: "ing-1", change)
        let body = try #require(await transport.calls("PATCH /ingredients/ing-1").first).body
        #expect(body.contains("\"grams_per_piece\":null"))
        #expect(body.contains("\"sodium\":1"))
        #expect(body.contains("\"vitamin_b12\":0"))
    }

    @Test("update errors: unit in use, other conflict, validation by field, not found")
    func updateErrors() async throws {
        let mode = Locked("unit")
        let (repo, _) = make { _ in
            switch mode.value {
            case "unit": return (409, Fixtures.problem(409, code: "unit_not_convertible"))
            case "other": return (409, Fixtures.problem(409, code: "something", title: "Conflict"))
            case "invalid": return (400, Fixtures.problem(400, code: "validation_failed", errors: [("grams_per_piece", "invalid_value")]))
            default: return (404, Fixtures.problem(404, code: "not_found"))
            }
        }
        await #expect(throws: IngredientError.unitInUse) { try await repo.update(id: "ing-1", change) }
        mode.set("other")
        await #expect(throws: IngredientError.server("Conflict")) { try await repo.update(id: "ing-1", change) }
        mode.set("invalid")
        await #expect(throws: IngredientError.validationFailed(fields: ["grams_per_piece": "Grams per piece is invalid."], message: "Grams per piece is invalid.")) {
            try await repo.update(id: "ing-1", change)
        }
        mode.set("gone")
        await #expect(throws: IngredientError.notFound) { try await repo.update(id: "ing-1", change) }
    }

    @Test("delete succeeds on 204; a meal still using it is inUse; gone is notFound; others throw")
    func delete() async throws {
        let mode = Locked(204)
        let (repo, transport) = make { _ in
            switch mode.value {
            case 204: return (204, "")
            case 409: return (409, Fixtures.problem(409, code: "ingredient_in_use"))
            case 404: return (404, Fixtures.problem(404, code: "not_found"))
            default: return (429, Fixtures.problem(429, code: "too_many_requests"))
            }
        }
        try await repo.delete(id: "ing-1")
        #expect(await transport.calls("DELETE /ingredients/ing-1").count == 1)
        mode.set(409)
        await #expect(throws: IngredientError.inUse) { try await repo.delete(id: "ing-1") }
        mode.set(404)
        await #expect(throws: IngredientError.notFound) { try await repo.delete(id: "ing-1") }
        mode.set(429)
        await #expect(throws: IngredientError.rateLimited) { try await repo.delete(id: "ing-1") }
    }
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `swift test --filter IngredientsRepositoryProfileTests`
Expected: FAIL to compile, "value of type 'IngredientsRepository' has no member 'customIngredients'".

- [ ] **Step 3: Implement**

`MealsError.swift`: replace the `IngredientError` enum with:

```swift
public enum IngredientError: Error, Equatable, Sendable {
    /// `fields` maps a JSON field path (`name`, `nutrients.calories`) to its message so a form can show it inline.
    case validationFailed(fields: [String: String], message: String)
    case notFound
    /// `409 ingredient_in_use`: a meal still uses it.
    case inUse
    /// `409 unit_not_convertible`: clearing the weight per piece or density would strand a meal that uses that unit.
    case unitInUse
    case unauthorized
    case rateLimited
    case server(String)
}
```

`IngredientsRepository.swift`: change the doc comment on the struct to `/// Not cached: ingredient search and the custom-ingredient screen are always live.`, and add these methods before `private static func validation`:

```swift
    /// Every custom ingredient the caller has made. `GET /ingredients` has no owner filter, so this walks every page
    /// (100 at a time) and keeps the ones with `isCustom`. A failure on any page fails the whole list.
    public func customIngredients() async throws -> [Components.Schemas.Ingredient] {
        var mine: [Components.Schemas.Ingredient] = []
        var cursor: String?
        repeat {
            let page = try await fetchPage(cursor: cursor)
            mine += page.items.filter(\.isCustom)
            cursor = page.nextCursor
        } while cursor != nil
        return mine
    }

    /// Replaces a custom ingredient's fields and its whole nutrient set. `nil` weight per piece or density clears it
    /// (sent as the null sentinel; see `NullSentinelMiddleware`).
    public func update(id: String, _ update: IngredientUpdate) async throws -> Components.Schemas.Ingredient {
        let body = Components.Schemas.UpdateIngredientRequest(
            name: update.name, category: update.category,
            gramsPerPiece: update.gramsPerPiece ?? NullSentinel.value,
            densityGPerMl: update.densityGPerMl ?? NullSentinel.value,
            nutrients: update.nutrients
        )
        let response = try await unwrapping { try await client.updateIngredient(.init(path: .init(id: id), body: .json(body))) }
        switch response {
        case .ok(let ok): return try ok.body.json
        case .badRequest(let r): throw Self.validation(r.problem)
        case .unauthorized: throw IngredientError.unauthorized
        case .notFound: throw IngredientError.notFound
        case .conflict(let r): throw Self.conflict(r.problem)
        case .tooManyRequests: throw IngredientError.rateLimited
        case .internalServerError: throw IngredientError.server("The server had a problem saving the ingredient.")
        case .undocumented(let status, _): throw IngredientError.server("Unexpected response (\(status)).")
        }
    }

    /// Deletes a custom ingredient. A meal that still uses it blocks the delete (`inUse`).
    public func delete(id: String) async throws {
        let response = try await unwrapping { try await client.deleteIngredient(.init(path: .init(id: id))) }
        switch response {
        case .noContent: return
        case .unauthorized: throw IngredientError.unauthorized
        case .notFound: throw IngredientError.notFound
        case .conflict(let r): throw Self.conflict(r.problem)
        case .tooManyRequests: throw IngredientError.rateLimited
        case .internalServerError: throw IngredientError.server("The server had a problem deleting the ingredient.")
        case .undocumented(let status, _): throw IngredientError.server("Unexpected response (\(status)).")
        }
    }

    private func fetchPage(cursor: String?) async throws -> Components.Schemas.IngredientList {
        let response = try await unwrapping { try await client.listIngredients(.init(query: .init(cursor: cursor, limit: 100))) }
        switch response {
        case .ok(let ok): return try ok.body.json
        case .badRequest(let r): throw Self.validation(r.problem)
        case .unauthorized: throw IngredientError.unauthorized
        case .tooManyRequests: throw IngredientError.rateLimited
        case .internalServerError: throw IngredientError.server("The server had a problem loading ingredients.")
        case .undocumented(let status, _): throw IngredientError.server("Unexpected response (\(status)).")
        }
    }

    private static func conflict(_ problem: Components.Schemas.Problem?) -> IngredientError {
        switch problem?.code {
        case "ingredient_in_use": .inUse
        case "unit_not_convertible": .unitInUse
        default: .server(problem?.detail ?? problem?.title ?? "The request conflicts with the current state.")
        }
    }
```

`ErrorText.swift`: in the `IngredientError` switch, add (keep the existing four cases):

```swift
            case .notFound: return "This ingredient isn't available anymore."
            case .inUse: return "A meal still uses this ingredient. Remove it from those meals first."
            case .unitInUse: return "A meal uses this ingredient by piece or by volume, so its weight per piece or density can't be cleared."
```

- [ ] **Step 4: Run to verify it passes, then the whole suite**

Run: `swift test --filter "IngredientsRepositoryProfileTests|IngredientSearchViewModelTests|CustomIngredientTests"`, then `swift test`.
Expected: PASS (6 new); whole suite green. Any other exhaustive `switch` over `IngredientError` must be extended: the compiler lists them.

- [ ] **Step 5: Commit**

```bash
git add ios/MealPlannerKit/Sources/Repositories ios/MealPlannerKit/Sources/Features/Shared/ErrorText.swift ios/MealPlannerKit/Tests/MealPlannerKitTests/IngredientsRepositoryProfileTests.swift
git commit -m "feat(ios): list, update and delete my custom ingredients

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 8: `ProfileViewModel` (account, targets, deletion) and its test server

**Files:**
- Create: `ios/MealPlannerKit/Sources/Features/Profile/ProfileViewModel.swift`
- Create: `ios/MealPlannerKit/Tests/MealPlannerKitTests/ProfileTestSupport.swift` (`ProfileServer`, `ProfileHarness`; reused by later tasks)
- Test: `ios/MealPlannerKit/Tests/MealPlannerKitTests/ProfileViewModelTests.swift`

**Interfaces:**
- Consumes: `ProfileRepository`, `PlanRepository.storeTargets(from:)`, `TargetsDraft`, `ErrorText` (Tasks 2, 5), `NullSentinelMiddleware` (Task 1), `ProfileFixtures`, `RoutingTransport`, `Locked`.
- Produces: `ProfileViewModel(profile:plan:signOut:)` (`@Observable @MainActor`) with state `user`, `draft`, `fieldErrors`, `banner`, `isSaving`, `isStale`, `loadError`, `isLoading`, `canSave`; methods `appear()`, `saveTargets() -> Bool` (discardable), `deleteAccount(typedEmail:) -> String?`; `static emailMatches(_:_:) -> Bool`. Test helpers `ProfileServer` (`current`, `isDeleted`, `setOffline(_:)`, `force(_:status:)`, `replace(_:)`, `rejectPatch(field:)`) and `ProfileHarness` (`server`, `transport`, `profileCache`, `planCache`, `profile`, `plan`, `signOuts`, `makeViewModel()`).

- [ ] **Step 1: Write the test support — `ProfileTestSupport.swift`**

```swift
import API
import Foundation
import Persistence
@testable import Features
@testable import Repositories

/// A tiny in-memory stand-in for `/me`, so view-model tests read like the real round trip. PATCH follows the real rules:
/// an explicit `null` clears a field, an omitted field is unchanged, an out-of-range number is a `400`.
final class ProfileServer: @unchecked Sendable {
    private let lock = NSLock()
    private var user: Components.Schemas.User
    private var offline = false
    private var forced: [String: Int] = [:]
    private var deleted = false
    private var rejectedField: String?

    init(_ user: Components.Schemas.User = ProfileFixtures.user()) { self.user = user }

    var current: Components.Schemas.User { lock.lock(); defer { lock.unlock() }; return user }
    var isDeleted: Bool { lock.lock(); defer { lock.unlock() }; return deleted }

    func setOffline(_ value: Bool) { lock.lock(); offline = value; lock.unlock() }
    /// Every request for this exact route (`"PATCH /me"`) answers `status`.
    func force(_ route: String, status: Int) { lock.lock(); forced[route] = status; lock.unlock() }
    /// Someone else changed the profile.
    func replace(_ next: Components.Schemas.User) { lock.lock(); user = next; lock.unlock() }
    /// The next PATCH is refused as invalid on this server field (`target_protein_g`), whatever the value.
    func rejectPatch(field: String?) { lock.lock(); rejectedField = field; lock.unlock() }

    func route(_ call: RoutingTransport.Call) async throws -> (status: Int, body: String) { try handle(call) }

    private func handle(_ call: RoutingTransport.Call) throws -> (status: Int, body: String) {
        lock.lock(); defer { lock.unlock() }
        if offline { throw URLError(.notConnectedToInternet) }
        if let status = forced[call.route] { return (status, status >= 400 ? Fixtures.problem(status, code: "forced") : "") }
        switch call.route {
        case "GET /me":
            return (200, Fixtures.json(user))
        case "PATCH /me":
            if let field = rejectedField {
                return (400, Fixtures.problem(400, code: "validation_failed", errors: [(field, "invalid_value")]))
            }
            let body = (try? JSONSerialization.jsonObject(with: Data(call.body.utf8))) as? [String: Any] ?? [:]
            var next = user
            var errors: [(field: String, code: String)] = []
            func read(_ key: String, positive: Bool, max: Double, _ set: (Double?) -> Void) {
                guard let raw = body[key] else { return } // omitted: unchanged
                if raw is NSNull { set(nil); return } // explicit null: clear
                guard let value = (raw as? NSNumber)?.doubleValue, positive ? value > 0 : value >= 0, value <= max else {
                    errors.append((key, "invalid_value"))
                    return
                }
                set(value)
            }
            read("target_kcal", positive: true, max: 20000) { next.targetKcal = $0 }
            read("target_protein_g", positive: false, max: 2000) { next.targetProteinG = $0 }
            read("target_carbs_g", positive: false, max: 5000) { next.targetCarbsG = $0 }
            read("target_fat_g", positive: false, max: 2000) { next.targetFatG = $0 }
            if !errors.isEmpty { return (400, Fixtures.problem(400, code: "validation_failed", errors: errors)) }
            user = next
            return (200, Fixtures.json(next))
        case "DELETE /me":
            deleted = true
            return (204, "")
        default:
            return (500, Fixtures.problem(500, code: "unrouted"))
        }
    }
}

/// A profile repository, plan repository and caches over one `ProfileServer`. The client has the real null-sentinel
/// middleware, so a cleared target travels the same way it does in the app.
@MainActor
struct ProfileHarness {
    let server: ProfileServer
    let transport: RoutingTransport
    let profileCache: ProfileCache
    let planCache: PlanCache
    let profile: ProfileRepository
    let plan: PlanRepository
    let signOuts = Locked(0)

    init(_ server: ProfileServer = ProfileServer()) throws {
        let transport = RoutingTransport { call in try await server.route(call) }
        let container = try CacheStore.inMemoryContainer()
        let client = makeClient(transport: transport, middlewares: [NullSentinelMiddleware()])
        self.server = server
        self.transport = transport
        self.profileCache = CacheStore.makeProfileCache(container)
        self.planCache = CacheStore.makePlanCache(container)
        self.profile = ProfileRepository(client: client, cache: profileCache)
        self.plan = PlanRepository(client: client, cache: planCache)
    }

    func makeViewModel() -> ProfileViewModel {
        let counter = signOuts
        return ProfileViewModel(profile: profile, plan: plan, signOut: { counter.mutate { $0 += 1 } })
    }
}
```

- [ ] **Step 2: Write the failing tests — `ProfileViewModelTests.swift`**

```swift
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
}
```

- [ ] **Step 3: Run to verify it fails**

Run: `swift test --filter ProfileViewModelTests`
Expected: FAIL to compile, "cannot find 'ProfileViewModel' in scope".

- [ ] **Step 4: Implement `ProfileViewModel.swift`**

```swift
import API
import Foundation
import Observation
import Repositories

/// The Profile tab's account and targets. Reads render the cache, await the refresh, re-read; a failed refresh keeps
/// what is shown and marks it stale. Saving and deleting are online-only. The draft follows the saved targets unless
/// the person has typed something, so a refresh never overwrites their typing.
@Observable
@MainActor
public final class ProfileViewModel {
    public private(set) var user: Components.Schemas.User?
    public var draft = TargetsDraft()
    public private(set) var fieldErrors: [TargetsDraft.Field: String] = [:]
    public private(set) var banner: String?
    public private(set) var isSaving = false
    public private(set) var isStale = false
    public private(set) var loadError: String?
    /// Loads can overlap (appear, refresh, foreground), so this counts them.
    public private(set) var loadsInFlight = 0
    public var isLoading: Bool { loadsInFlight > 0 }
    /// Save is offered only when the draft differs from what the server has.
    public var canSave: Bool { user != nil && !isSaving && draft != savedDraft }

    private var savedDraft = TargetsDraft()
    @ObservationIgnored private let profile: ProfileRepository
    @ObservationIgnored private let plan: PlanRepository
    @ObservationIgnored private let signOut: @MainActor () async -> Void

    /// `signOut` runs once an account has been deleted (the app's normal sign-out, which clears every cache).
    public init(profile: ProfileRepository, plan: PlanRepository, signOut: @escaping @MainActor () async -> Void) {
        self.profile = profile
        self.plan = plan
        self.signOut = signOut
    }

    /// The tab appears or the app returns to the foreground.
    public func appear() async {
        loadsInFlight += 1
        defer { loadsInFlight -= 1 }
        if let cached = await profile.cachedUser() { adopt(cached, keepEdits: true) }
        do {
            adopt(try await profile.refreshUser(), keepEdits: true)
            isStale = false
            loadError = nil
        } catch {
            isStale = true
            loadError = user == nil ? ErrorText.message(for: error) : nil
        }
    }

    /// Validates, saves all four targets, and on success stores the answer in the profile cache and gives the plan
    /// cache the new targets. Returns whether it saved.
    @discardableResult
    public func saveTargets() async -> Bool {
        guard user != nil, !isSaving else { return false }
        fieldErrors = [:]
        banner = nil
        let update: TargetsUpdate
        switch draft.validate() {
        case .invalid(let errors):
            fieldErrors = errors
            return false
        case .valid(let valid):
            update = valid
        }
        isSaving = true
        defer { isSaving = false }
        do {
            let saved = try await profile.updateTargets(update)
            await plan.storeTargets(from: saved)
            adopt(saved, keepEdits: false)
            return true
        } catch let ProfileError.validationFailed(fields, message) {
            var inline: [TargetsDraft.Field: String] = [:]
            for (path, text) in fields {
                if let field = TargetsDraft.Field(apiPath: path) { inline[field] = text }
            }
            if inline.isEmpty { banner = message } else { fieldErrors = inline }
            return false
        } catch {
            banner = ErrorText.message(for: error)
            return false
        }
    }

    public static func emailMatches(_ typed: String, _ email: String) -> Bool {
        typed.trimmingCharacters(in: .whitespacesAndNewlines).lowercased() == email.lowercased()
    }

    /// Deletes the account once the typed email matches, then signs out. Returns the text to show, or `nil` when it is
    /// gone. Nothing is sent while the email does not match; a failure leaves the account and the session alone.
    public func deleteAccount(typedEmail: String) async -> String? {
        guard let email = user?.email, Self.emailMatches(typedEmail, email) else {
            return "Type your email address exactly to confirm."
        }
        do {
            try await profile.deleteAccount()
        } catch {
            return ErrorText.message(for: error)
        }
        await signOut()
        return nil
    }

    /// Takes in a user from the cache, the server or a save. The draft follows it unless the person has typed
    /// something (`keepEdits`).
    private func adopt(_ next: Components.Schemas.User, keepEdits: Bool) {
        let edited = keepEdits && draft != savedDraft
        user = next
        savedDraft = TargetsDraft(user: next)
        if !edited { draft = savedDraft }
    }
}
```

- [ ] **Step 5: Run to verify it passes, then commit**

Run: `swift test --filter ProfileViewModelTests` three times.
Expected: PASS (11 tests) each time. If `serverRefusals` shows a different field label, print `vm.fieldErrors` and match `ProblemText`'s output.

```bash
git add ios/MealPlannerKit/Sources/Features/Profile/ProfileViewModel.swift ios/MealPlannerKit/Tests/MealPlannerKitTests/ProfileTestSupport.swift ios/MealPlannerKit/Tests/MealPlannerKitTests/ProfileViewModelTests.swift
git commit -m "feat(ios): add the Profile view model for the account, targets and deletion

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 9: `PartnerViewModel`

**Files:**
- Create: `ios/MealPlannerKit/Sources/Features/Profile/PartnerViewModel.swift`
- Test: `ios/MealPlannerKit/Tests/MealPlannerKitTests/PartnerViewModelTests.swift`

**Interfaces:**
- Consumes: `PartnerRepository` (`status()`, `cachedStatus()`, `createInvite()`, `accept(code:)`, `unlink()`), `CachedPartnership`, `PartnerError`, `ErrorText` (Task 6), `ProfileFixtures`.
- Produces: `PartnerViewModel(partner:)` (`@Observable @MainActor`) with `phase: Phase` (`loading`, `none`, `pending(expiresAt: Date?)`, `linked(name: String, since: Date?)`), `invite: PartnerInvite?`, `isStale`, `loadError`, `isBusy`, `codeText` (settable), `acceptError`, `alertMessage` (settable); methods `appear()`, `createInvite()`, `accept()`, `unlink()`.

- [ ] **Step 1: Write the failing tests** — `PartnerViewModelTests.swift`

```swift
import API
import Foundation
import Persistence
import Testing
@testable import Features
@testable import Repositories

@MainActor
@Suite
struct PartnerViewModelTests {
    private func make(_ route: @escaping RoutingTransport.Route) throws -> (PartnerViewModel, PartnerRepository, ProfileCache, RoutingTransport) {
        let transport = RoutingTransport(route)
        let cache = CacheStore.makeProfileCache(try CacheStore.inMemoryContainer())
        let repo = PartnerRepository(client: makeAuthlessClient(transport: transport), cache: cache)
        return (PartnerViewModel(partner: repo), repo, cache, transport)
    }

    @Test("appear shows none, pending or linked from the server")
    func appear() async throws {
        let mode = Locked("none")
        let (vm, _, _, _) = try make { _ in
            switch mode.value {
            case "active": (200, Fixtures.json(ProfileFixtures.active("Alex")))
            case "pending": (200, Fixtures.json(ProfileFixtures.pending()))
            default: (404, Fixtures.problem(404, code: "partner_not_linked"))
            }
        }
        #expect(vm.phase == .loading)
        await vm.appear()
        #expect(vm.phase == .none)
        mode.set("active")
        await vm.appear()
        #expect(vm.phase == .linked(name: "Alex", since: Fixtures.date))
        mode.set("pending")
        await vm.appear()
        #expect(vm.phase == .pending(expiresAt: Fixtures.date))
        #expect(!vm.isStale)
    }

    @Test("Offline, the cached state is shown marked stale; with nothing cached it is an error")
    func offline() async throws {
        let (vm, _, cache, _) = try make { _ in throw URLError(.notConnectedToInternet) }
        await vm.appear()
        #expect(vm.phase == .loading)
        #expect(vm.loadError == "Can't reach the server. Check your connection and try again.")

        await cache.store(partnership: ProfileFixtures.active("Alex"))
        await vm.appear()
        #expect(vm.phase == .linked(name: "Alex", since: Fixtures.date))
        #expect(vm.isStale)
        #expect(vm.loadError == nil)
    }

    @Test("Creating an invite shows the code once and moves to pending")
    func createInvite() async throws {
        let (vm, _, _, transport) = try make { call in
            call.route == "POST /partner/invite" ? (201, Fixtures.json(ProfileFixtures.invite(code: "ABCD2345"))) : (200, Fixtures.json(ProfileFixtures.pending()))
        }
        await vm.createInvite()
        #expect(vm.invite?.code == "ABCD2345")
        #expect(vm.phase == .pending(expiresAt: Fixtures.date))
        #expect(await transport.calls("POST /partner/invite").count == 1)
    }

    @Test("Review focus 6: the code lives only in this view model: a new one shows pending but no code")
    func codeIsNotKept() async throws {
        let (vm, repo, _, _) = try make { call in
            call.route == "POST /partner/invite" ? (201, Fixtures.json(ProfileFixtures.invite())) : (200, Fixtures.json(ProfileFixtures.pending()))
        }
        await vm.createInvite()
        #expect(vm.invite != nil)
        let fresh = PartnerViewModel(partner: repo)
        await fresh.appear()
        #expect(fresh.phase == .pending(expiresAt: Fixtures.date))
        #expect(fresh.invite == nil)
    }

    @Test("A refused invite is an alert and changes nothing")
    func createInviteRefused() async throws {
        let (vm, _, _, _) = try make { _ in (409, Fixtures.problem(409, code: "partner_already_linked")) }
        await vm.createInvite()
        #expect(vm.alertMessage == "You're already linked with a partner.")
        #expect(vm.invite == nil)
        #expect(vm.phase == .loading)
    }

    @Test("Accepting a blank code is refused without a request")
    func acceptBlank() async throws {
        let (vm, _, _, transport) = try make { _ in (500, "") }
        vm.codeText = "   "
        await vm.accept()
        #expect(vm.acceptError == "Enter the code your partner sent you.")
        #expect(await transport.calls.isEmpty)
    }

    @Test("Review focus 6: messy input is sent trimmed as typed; success links and clears the field")
    func acceptMessy() async throws {
        let (vm, _, _, transport) = try make { _ in (200, Fixtures.json(ProfileFixtures.active("Alex"))) }
        vm.codeText = "  ab-cd 2345 "
        await vm.accept()
        #expect(try #require(await transport.calls("POST /partner/accept").first).body.contains("\"code\":\"ab-cd 2345\""))
        #expect(vm.phase == .linked(name: "Alex", since: Fixtures.date))
        #expect(vm.codeText == "")
        #expect(vm.acceptError == nil)
    }

    @Test("Review focus 6: a wrong code and an existing partner show inline; a rate limit is an alert; nothing links")
    func acceptErrors() async throws {
        let mode = Locked(404)
        let (vm, _, _, _) = try make { _ in
            switch mode.value {
            case 404: (404, Fixtures.problem(404, code: "invite_invalid"))
            case 409: (409, Fixtures.problem(409, code: "partner_already_linked"))
            default: (429, Fixtures.problem(429, code: "too_many_requests"))
            }
        }
        vm.codeText = "ZZZZ9999"
        await vm.accept()
        #expect(vm.acceptError == "That code didn't work. Check it, or ask your partner for a new one.")
        mode.set(409)
        await vm.accept()
        #expect(vm.acceptError == "You're already linked with a partner.")
        mode.set(429)
        await vm.accept()
        #expect(vm.acceptError == nil)
        #expect(vm.alertMessage == "Too many tries. Wait a minute and try again.")
        #expect(vm.phase == .loading)
        #expect(vm.codeText == "ZZZZ9999") // kept, so a retry is one tap
    }

    @Test("Unlinking moves to none, forgets the code, and treats an already-gone link as done; a failure changes nothing")
    func unlink() async throws {
        let mode = Locked(204)
        let (vm, repo, cache, _) = try make { call in
            if call.route == "GET /partner" { return (200, Fixtures.json(ProfileFixtures.active("Alex"))) }
            return mode.value == 204 ? (204, "") : (mode.value, Fixtures.problem(mode.value, code: mode.value == 404 ? "partner_not_linked" : "internal"))
        }
        await vm.appear()
        await vm.unlink()
        #expect(vm.phase == .none)
        #expect(await repo.cachedStatus() == .none)

        await vm.appear()
        mode.set(404)
        await vm.unlink()
        #expect(vm.phase == .none)

        await vm.appear()
        mode.set(500)
        await vm.unlink()
        #expect(vm.alertMessage == "The server had a problem ending the link.")
        #expect(vm.phase == .linked(name: "Alex", since: Fixtures.date))
        #expect(await cache.partnership() == .present(ProfileFixtures.active("Alex")))
    }
}
```
- [ ] **Step 2: Run to verify it fails**

Run: `swift test --filter PartnerViewModelTests`
Expected: FAIL to compile, "cannot find 'PartnerViewModel' in scope".

- [ ] **Step 3: Implement `PartnerViewModel.swift`**

```swift
import API
import Foundation
import Observation
import Persistence
import Repositories

/// The partner connection on Profile. Reads render the cached state, await the refresh, re-read. Every change needs a
/// connection. The invite code is held here, in memory, only: the server returns it once and the app never stores it.
@Observable
@MainActor
public final class PartnerViewModel {
    public enum Phase: Equatable, Sendable {
        /// Nothing cached and no answer yet.
        case loading
        case none
        case pending(expiresAt: Date?)
        case linked(name: String, since: Date?)
    }

    public private(set) var phase: Phase = .loading
    public private(set) var invite: Components.Schemas.PartnerInvite?
    public private(set) var isStale = false
    public private(set) var loadError: String?
    public private(set) var isBusy = false
    /// The accept field's text. Kept after a failed attempt so a retry is one tap.
    public var codeText = ""
    public private(set) var acceptError: String?
    public var alertMessage: String?

    @ObservationIgnored private let partner: PartnerRepository

    public init(partner: PartnerRepository) {
        self.partner = partner
    }

    public func appear() async {
        switch await partner.cachedStatus() {
        case .unknown: break
        case .none: phase = .none
        case .present(let partnership): phase = Self.phase(for: partnership)
        }
        do {
            let status = try await partner.status()
            phase = status.map(Self.phase(for:)) ?? .none
            isStale = false
            loadError = nil
        } catch {
            isStale = true
            loadError = phase == .loading ? ErrorText.message(for: error) : nil
        }
    }

    public func createInvite() async {
        guard !isBusy else { return }
        isBusy = true
        defer { isBusy = false }
        do {
            let created = try await partner.createInvite()
            invite = created
            phase = .pending(expiresAt: created.expiresAt)
            // Keeps the cached status in step; the screen already shows what this answer means.
            _ = try? await partner.status()
        } catch {
            alertMessage = ErrorText.message(for: error)
        }
    }

    public func accept() async {
        guard !isBusy else { return }
        let code = codeText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !code.isEmpty else {
            acceptError = "Enter the code your partner sent you."
            return
        }
        acceptError = nil
        isBusy = true
        defer { isBusy = false }
        do {
            let linked = try await partner.accept(code: code)
            phase = Self.phase(for: linked)
            codeText = ""
            invite = nil
        } catch PartnerError.inviteInvalid {
            acceptError = ErrorText.message(for: PartnerError.inviteInvalid)
        } catch PartnerError.alreadyLinked {
            acceptError = ErrorText.message(for: PartnerError.alreadyLinked)
        } catch {
            alertMessage = ErrorText.message(for: error)
        }
    }

    /// Ends the link, or cancels a pending invite.
    public func unlink() async {
        guard !isBusy else { return }
        isBusy = true
        defer { isBusy = false }
        do {
            try await partner.unlink()
            phase = .none
            invite = nil
        } catch {
            alertMessage = ErrorText.message(for: error)
        }
    }

    private static func phase(for partnership: Components.Schemas.Partnership) -> Phase {
        switch partnership.status {
        case .active: .linked(name: partnership.displayName ?? "your partner", since: partnership.linkedAt)
        case .pending: .pending(expiresAt: partnership.expiresAt)
        }
    }
}
```

- [ ] **Step 4: Run to verify it passes, then commit**

Run: `swift test --filter PartnerViewModelTests`
Expected: PASS (9 tests).

```bash
git add ios/MealPlannerKit/Sources/Features/Profile/PartnerViewModel.swift ios/MealPlannerKit/Tests/MealPlannerKitTests/PartnerViewModelTests.swift
git commit -m "feat(ios): add the partner connection view model

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 10: Ingredient edit mode in `CustomIngredientViewModel`, and `MyIngredientsViewModel`

**Files:**
- Modify: `ios/MealPlannerKit/Sources/Features/Shared/CustomIngredient/CustomIngredientViewModel.swift` (replace)
- Create: `ios/MealPlannerKit/Sources/Features/Profile/MyIngredientsViewModel.swift`
- Test: `ios/MealPlannerKit/Tests/MealPlannerKitTests/CustomIngredientEditViewModelTests.swift`, `MyIngredientsViewModelTests.swift`

**Interfaces:**
- Consumes: `CustomIngredientForm.init(editing:)`, `.validateUpdate(preserving:)` (Task 3), `IngredientsRepository.update/delete/customIngredients` (Task 7), `IngredientError`, `ErrorText`.
- Produces: `CustomIngredientViewModel.init(editing: Ingredient, repository:)` (the existing `init(name:repository:)` is unchanged); `save()` returns the created or updated ingredient. `MyIngredientsViewModel(repository:)` with `all`, `shown`, `searchText` (settable), `isLoading`, `loadError`; `load()`, `didSave(_:)`, `delete(_:) -> String?`.

- [ ] **Step 1: Write the failing tests**

`CustomIngredientEditViewModelTests.swift`:

```swift
import API
import Foundation
import Testing
@testable import Features
@testable import Repositories

@MainActor
@Suite
struct CustomIngredientEditViewModelTests {
    private func make(_ route: @escaping RoutingTransport.Route) -> (IngredientsRepository, RoutingTransport) {
        let transport = RoutingTransport(route)
        return (IngredientsRepository(client: makeClient(transport: transport, middlewares: [NullSentinelMiddleware()])), transport)
    }

    @Test("Review focus 2: saving a renamed ingredient sends all 18 nutrients and clears a blank weight per piece")
    func rename() async throws {
        let (repo, transport) = make { _ in (200, Fixtures.json(ProfileFixtures.customIngredient(name: "Apple, raw"))) }
        let vm = CustomIngredientViewModel(editing: ProfileFixtures.fullIngredient(), repository: repo)
        vm.form.name = "Apple, raw"
        vm.form.gramsPerPiece = ""
        let saved = await vm.save()
        #expect(saved?.name == "Apple, raw")
        let body = try #require(await transport.calls("PATCH /ingredients/ing-1").first).body
        #expect(body.contains("\"name\":\"Apple, raw\""))
        #expect(body.contains("\"grams_per_piece\":null"))
        #expect(body.contains("\"calories\":52"))
        #expect(body.contains("\"sodium\":1"))
        #expect(body.contains("\"vitamin_a\":3"))
        #expect(body.contains("\"iron\":0.1"))
        #expect(await transport.calls("POST /ingredients").isEmpty)
    }

    @Test("Invalid input shows on the field and sends nothing")
    func invalid() async throws {
        let (repo, transport) = make { _ in (500, "") }
        let vm = CustomIngredientViewModel(editing: ProfileFixtures.fullIngredient(), repository: repo)
        vm.form.name = " "
        #expect(await vm.save() == nil)
        #expect(vm.fieldErrors[.name] == "Give the ingredient a name.")
        #expect(await transport.calls.isEmpty)
    }

    @Test("A server refusal that names a field shows inline; a meal still needing the conversion is a banner")
    func serverRefusals() async throws {
        let mode = Locked("field")
        let (repo, _) = make { _ in
            mode.value == "field"
                ? (400, Fixtures.problem(400, code: "validation_failed", errors: [("grams_per_piece", "invalid_value")]))
                : (409, Fixtures.problem(409, code: "unit_not_convertible"))
        }
        let vm = CustomIngredientViewModel(editing: ProfileFixtures.fullIngredient(), repository: repo)
        #expect(await vm.save() == nil)
        #expect(vm.fieldErrors[.gramsPerPiece] == "Grams per piece is invalid.")
        #expect(vm.bannerError == nil)

        mode.set("unit")
        #expect(await vm.save() == nil)
        #expect(vm.fieldErrors.isEmpty)
        #expect(vm.bannerError == "A meal uses this ingredient by piece or by volume, so its weight per piece or density can't be cleared.")
    }

    @Test("Creating still posts a new ingredient")
    func createStillWorks() async throws {
        let (repo, transport) = make { _ in (201, Fixtures.json(ProfileFixtures.customIngredient(name: "Jam"))) }
        let vm = CustomIngredientViewModel(name: "Jam", repository: repo)
        let created = await vm.save()
        #expect(created?.name == "Jam")
        #expect(await transport.calls("POST /ingredients").count == 1)
        #expect(await transport.calls("PATCH /ingredients/ing-1").isEmpty)
    }
}
```

`MyIngredientsViewModelTests.swift`:

```swift
import API
import Foundation
import Testing
@testable import Features
@testable import Repositories

@MainActor
@Suite
struct MyIngredientsViewModelTests {
    private func make(_ route: @escaping RoutingTransport.Route) -> (MyIngredientsViewModel, RoutingTransport) {
        let transport = RoutingTransport(route)
        return (MyIngredientsViewModel(repository: IngredientsRepository(client: makeAuthlessClient(transport: transport))), transport)
    }

    private func page() -> String {
        ProfileFixtures.ingredientPage([
            ProfileFixtures.customIngredient(id: "b", name: "banana bread mix"),
            Fixtures.ingredient(id: "g", name: "Rice", isCustom: false),
            ProfileFixtures.customIngredient(id: "a", name: "Apple jam"),
        ])
    }

    @Test("load keeps only my custom ingredients, sorted by name ignoring case")
    func load() async throws {
        let (vm, _) = make { _ in (200, page()) }
        await vm.load()
        #expect(vm.all.map(\.id) == ["a", "b"])
        #expect(vm.loadError == nil)
        #expect(!vm.isLoading)
    }

    @Test("Search filters locally and ignores case; blank shows everything")
    func search() async throws {
        let (vm, transport) = make { _ in (200, page()) }
        await vm.load()
        vm.searchText = "JAM"
        #expect(vm.shown.map(\.id) == ["a"])
        vm.searchText = "  "
        #expect(vm.shown.count == 2)
        vm.searchText = "zzz"
        #expect(vm.shown.isEmpty)
        #expect(await transport.calls("GET /ingredients").count == 1) // filtering made no request
    }

    @Test("A failed load is an error with nothing shown, and keeps the list it had when a later load fails")
    func loadFailure() async throws {
        let fail = Locked(true)
        let (vm, _) = make { _ in fail.value ? (500, Fixtures.problem(500, code: "internal")) : (200, page()) }
        await vm.load()
        #expect(vm.all.isEmpty)
        #expect(vm.loadError == "The server had a problem loading ingredients.")
        fail.set(false)
        await vm.load()
        #expect(vm.all.count == 2)
        #expect(vm.loadError == nil)
        fail.set(true)
        await vm.load()
        #expect(vm.all.count == 2)
    }

    @Test("didSave replaces an edited row and inserts a new one, keeping the order")
    func didSave() async throws {
        let (vm, _) = make { _ in (200, page()) }
        await vm.load()
        vm.didSave(ProfileFixtures.customIngredient(id: "a", name: "Zucchini jam"))
        vm.didSave(ProfileFixtures.customIngredient(id: "c", name: "Cherry jam"))
        #expect(vm.all.map(\.name) == ["banana bread mix", "Cherry jam", "Zucchini jam"])
    }

    @Test("delete removes the row; one already gone counts as deleted; one a meal uses stays with a message")
    func delete() async throws {
        let mode = Locked(204)
        let (vm, _) = make { call in
            if call.method == "GET" { return (200, page()) }
            switch mode.value {
            case 204: return (204, "")
            case 404: return (404, Fixtures.problem(404, code: "not_found"))
            default: return (409, Fixtures.problem(409, code: "ingredient_in_use"))
            }
        }
        await vm.load()
        let apple = try #require(vm.all.first { $0.id == "a" })
        let banana = try #require(vm.all.first { $0.id == "b" })
        #expect(await vm.delete(apple) == nil)
        #expect(vm.all.map(\.id) == ["b"])

        mode.set(409)
        #expect(await vm.delete(banana) == "A meal still uses this ingredient. Remove it from those meals first.")
        #expect(vm.all.map(\.id) == ["b"])

        mode.set(404)
        #expect(await vm.delete(banana) == nil)
        #expect(vm.all.isEmpty)
    }
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `swift test --filter "CustomIngredientEditViewModelTests|MyIngredientsViewModelTests"`
Expected: FAIL to compile, "no exact matches in call to initializer" / "cannot find 'MyIngredientsViewModel'".

- [ ] **Step 3: Implement**

`CustomIngredientViewModel.swift` (replace the file):

```swift
import API
import Observation
import Repositories

/// The custom-ingredient form's state, for creating an ingredient or editing one of mine.
@Observable
@MainActor
public final class CustomIngredientViewModel {
    public var form: CustomIngredientForm
    public private(set) var fieldErrors: [CustomIngredientForm.Field: String] = [:]
    public private(set) var bannerError: String?
    public private(set) var isSaving = false

    @ObservationIgnored private let repository: IngredientsRepository
    @ObservationIgnored private let editing: Components.Schemas.Ingredient?

    /// `name` is the search text the person was looking for, prefilled.
    public init(name: String, repository: IngredientsRepository) {
        self.form = CustomIngredientForm(name: name)
        self.repository = repository
        self.editing = nil
    }

    /// Editing an existing ingredient: the form is prefilled, and a save keeps every nutrient the form does not show.
    public init(editing ingredient: Components.Schemas.Ingredient, repository: IngredientsRepository) {
        self.form = CustomIngredientForm(editing: ingredient)
        self.repository = repository
        self.editing = ingredient
    }

    /// Returns the created or updated ingredient, or `nil` with `fieldErrors` / `bannerError` set. A `400` shows
    /// inline on the field it names, else as a banner.
    public func save() async -> Components.Schemas.Ingredient? {
        fieldErrors = [:]
        bannerError = nil
        if let editing {
            switch form.validateUpdate(preserving: editing) {
            case .invalid(let errors):
                fieldErrors = errors
                return nil
            case .valid(let update):
                return await send { try await self.repository.update(id: editing.id, update) }
            }
        }
        switch form.validate() {
        case .invalid(let errors):
            fieldErrors = errors
            return nil
        case .valid(let request):
            return await send { try await self.repository.create(request) }
        }
    }

    private func send(_ call: () async throws -> Components.Schemas.Ingredient) async -> Components.Schemas.Ingredient? {
        isSaving = true
        defer { isSaving = false }
        do {
            return try await call()
        } catch let IngredientError.validationFailed(fields, message) {
            var inline: [CustomIngredientForm.Field: String] = [:]
            for (path, text) in fields {
                if let field = CustomIngredientForm.Field(apiPath: path) { inline[field] = text }
            }
            if inline.isEmpty { bannerError = message } else { fieldErrors = inline }
            return nil
        } catch {
            bannerError = ErrorText.message(for: error)
            return nil
        }
    }
}
```

`MyIngredientsViewModel.swift`:

```swift
import API
import Foundation
import Observation
import Repositories

/// "My ingredients" on Profile: the custom ingredients, loaded live (every page, filtered on the device because the API
/// has no owner filter) and searched locally. Needs a connection, so offline it is an error with retry, never a stale list.
@Observable
@MainActor
public final class MyIngredientsViewModel {
    public private(set) var all: [Components.Schemas.Ingredient] = []
    public private(set) var isLoading = false
    public private(set) var loadError: String?
    public var searchText = ""

    /// `all`, narrowed by the search text (whole-word or not, ignoring case).
    public var shown: [Components.Schemas.Ingredient] {
        let query = searchText.trimmingCharacters(in: .whitespacesAndNewlines)
        return query.isEmpty ? all : all.filter { $0.name.localizedCaseInsensitiveContains(query) }
    }

    @ObservationIgnored private let repository: IngredientsRepository

    public init(repository: IngredientsRepository) {
        self.repository = repository
    }

    public func load() async {
        isLoading = true
        defer { isLoading = false }
        do {
            all = Self.sorted(try await repository.customIngredients())
            loadError = nil
        } catch {
            // A list already on screen stays; with nothing there the error is shown.
            loadError = all.isEmpty ? ErrorText.message(for: error) : nil
        }
    }

    /// A created or edited ingredient: replaces its row (or adds it) and keeps the order.
    public func didSave(_ ingredient: Components.Schemas.Ingredient) {
        var next = all.filter { $0.id != ingredient.id }
        next.append(ingredient)
        all = Self.sorted(next)
    }

    /// Returns the text to show, or `nil` when the ingredient is gone. One already gone elsewhere counts as deleted.
    public func delete(_ ingredient: Components.Schemas.Ingredient) async -> String? {
        do {
            try await repository.delete(id: ingredient.id)
        } catch IngredientError.notFound {
            // Already gone: the goal is met.
        } catch {
            return ErrorText.message(for: error)
        }
        all.removeAll { $0.id == ingredient.id }
        return nil
    }

    private static func sorted(_ items: [Components.Schemas.Ingredient]) -> [Components.Schemas.Ingredient] {
        items.sorted { $0.name.localizedCaseInsensitiveCompare($1.name) == .orderedAscending }
    }
}
```

- [ ] **Step 4: Run to verify they pass, then the existing ingredient suites and the whole suite**

Run: `swift test --filter "CustomIngredientEditViewModelTests|MyIngredientsViewModelTests|CustomIngredientTests|IngredientSearchViewModelTests"`, then `swift test`.
Expected: PASS (4 + 5 new, existing unchanged); whole suite green.

- [ ] **Step 5: Commit**

```bash
git add ios/MealPlannerKit/Sources/Features ios/MealPlannerKit/Tests/MealPlannerKitTests/CustomIngredientEditViewModelTests.swift ios/MealPlannerKit/Tests/MealPlannerKitTests/MyIngredientsViewModelTests.swift
git commit -m "feat(ios): add ingredient edit mode and the My ingredients view model

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 11: The Profile screens and the app wiring

**Files:**
- Create: `ios/MealPlannerKit/Sources/Features/Profile/ProfileDependencies.swift`
- Replace: `ios/MealPlannerKit/Sources/Features/Profile/ProfileView.swift`
- Create: `TargetsSection.swift`, `PartnerSection.swift`, `DeleteAccountSheet.swift`, `MyIngredientsView.swift` (all in `Sources/Features/Profile/`)
- Modify: `ios/MealPlannerKit/Sources/Features/Shared/CustomIngredient/CustomIngredientView.swift` (edit initialiser, title)
- Modify: `ios/MealPlannerKit/Sources/Features/Shared/ViewHelpers.swift` (`noAutocapitalization()`)
- Modify: `ios/MealPlannerKit/Sources/AppCore/ClearCaches.swift`, `RootView.swift`, `TabShellView.swift`
- Test: modify `ios/MealPlannerKit/Tests/MealPlannerKitTests/ClearCachesTests.swift`

**Interfaces:**
- Consumes: `ProfileViewModel`, `PartnerViewModel`, `MyIngredientsViewModel`, `InviteCodeFormat`, `CustomIngredientView`, `IngredientCategory.label`, `ProfileRepository`/`PartnerRepository`/`IngredientsRepository`/`PlanRepository`, `NullSentinelMiddleware`, `CacheStore.makeProfileCache`.
- Produces: `ProfileDependencies(profile:plan:partner:ingredients:)`; `ProfileView(dependencies:onSignOut:)`; `clearAllCaches(meals:plan:templates:shopping:profile:sync:)`. Accessibility identifiers the UI test in Task 12 relies on: `profileEmail`, `profileOfflineLabel`, `targetCaloriesField`, `targetProteinField`, `targetCarbsField`, `targetFatField`, `targetsSaveButton`, `targetsBanner`, `partnerCreateInviteButton`, `partnerInviteCode`, `partnerCodeField`, `partnerLinkButton`, `partnerAcceptError`, `partnerLinkedLabel`, `partnerPendingLabel`, `partnerUnlinkButton`, `partnerCancelInviteButton`, `myIngredientsRow`, `myIngredientsNewButton`, `myIngredientRow-<name>`, `ingredientMenu-<name>`, `ingredientEditButton`, `ingredientDeleteButton`, `deleteAccountButton`, `deleteAccountEmailField`, `deleteAccountConfirmButton`, `deleteAccountMessage`, and the existing `signOutButton`.

There is no unit test for SwiftUI views in this codebase (the logic is in the tested view models); the gate for the views is the build, plus the UI test in Task 12. The one unit-tested part is the sign-out clearing (Review focus 5).

- [ ] **Step 1: Extend the failing test — `ClearCachesTests.swift`**

Make these four edits to the existing test (keep everything else):

1. After `let templateCache = CacheStore.makeTemplateCache(container)` add:
```swift
        let profileCache = CacheStore.makeProfileCache(container)
```
2. After `await templateCache.replaceSummaries([Fixtures.templateSummary()], scope: .mine)` add:
```swift
        await profileCache.store(user: ProfileFixtures.user())
        await profileCache.store(partnership: ProfileFixtures.active())
```
3. In the `clearAllCaches(` call, before `sync: engine`, add:
```swift
            profile: ProfileRepository(client: client, cache: profileCache),
```
4. After `#expect(await templateCache.summaries(scope: .mine).isEmpty)` add:
```swift
        #expect(await profileCache.user() == nil) // a second user never sees this user's email or targets
        #expect(await profileCache.partnership() == .unknown)
```
Also change the test's display name to mention the profile: `"Ending a session empties the meal, plan, template, shopping and profile caches, so a second user never sees the first user's data"`.

Run: `swift test --filter ClearCachesTests`
Expected: FAIL to compile, "extra argument 'profile' in call".

- [ ] **Step 2: Write the files**

`ProfileDependencies.swift`:

```swift
import Repositories

/// What the Profile tab needs, built once in `RootView` and handed down.
public struct ProfileDependencies: Sendable {
    public let profile: ProfileRepository
    public let plan: PlanRepository
    public let partner: PartnerRepository
    public let ingredients: IngredientsRepository

    public init(profile: ProfileRepository, plan: PlanRepository, partner: PartnerRepository, ingredients: IngredientsRepository) {
        self.profile = profile
        self.plan = plan
        self.partner = partner
        self.ingredients = ingredients
    }
}
```

`ViewHelpers.swift`: inside the existing `extension View`, before the line `/// Fires when the device's date or time zone changes (midnight, travelling). iOS only; elsewhere it does nothing.`, add:

```swift
    /// `.textInputAutocapitalization` is UIKit-only; the package also builds for macOS so `swift test` runs without a simulator.
    @ViewBuilder
    func noAutocapitalization() -> some View {
        #if os(iOS)
        self.textInputAutocapitalization(.never).autocorrectionDisabled()
        #else
        self
        #endif
    }

```

`ProfileView.swift` (replace the file):

```swift
import API
import Repositories
import SwiftUI

public struct ProfileView: View {
    @State private var viewModel: ProfileViewModel
    @State private var partnerViewModel: PartnerViewModel
    @State private var showingDelete = false
    private let dependencies: ProfileDependencies
    private let onSignOut: () -> Void
    @Environment(\.scenePhase) private var scenePhase

    public init(dependencies: ProfileDependencies, onSignOut: @escaping () -> Void) {
        self.dependencies = dependencies
        self.onSignOut = onSignOut
        _viewModel = State(initialValue: ProfileViewModel(profile: dependencies.profile, plan: dependencies.plan, signOut: { onSignOut() }))
        _partnerViewModel = State(initialValue: PartnerViewModel(partner: dependencies.partner))
    }

    public var body: some View {
        Form {
            if viewModel.isStale {
                Label("Offline: showing your saved profile", systemImage: "wifi.slash")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .accessibilityIdentifier("profileOfflineLabel")
            }
            accountSection
            TargetsSection(viewModel: viewModel)
            PartnerSection(viewModel: partnerViewModel)
            Section {
                NavigationLink("My ingredients") { MyIngredientsView(repository: dependencies.ingredients) }
                    .accessibilityIdentifier("myIngredientsRow")
            }
            Section {
                Button("Sign Out", role: .destructive, action: onSignOut)
                    .accessibilityIdentifier("signOutButton")
                Button("Delete account", role: .destructive) { showingDelete = true }
                    .disabled(viewModel.user == nil)
                    .accessibilityIdentifier("deleteAccountButton")
            }
        }
        .scrollDismissesKeyboard(.interactively)
        .navigationTitle("Profile")
        .task { await load() }
        .refreshable { await load() }
        .onChange(of: scenePhase) { _, phase in
            if phase == .active { Task { await load() } }
        }
        .sheet(isPresented: $showingDelete) {
            DeleteAccountSheet(email: viewModel.user?.email ?? "") { typed in
                await viewModel.deleteAccount(typedEmail: typed)
            }
        }
    }

    @ViewBuilder
    private var accountSection: some View {
        Section("Account") {
            if let user = viewModel.user {
                LabeledContent("Email", value: user.email).accessibilityIdentifier("profileEmail")
                LabeledContent("Name", value: user.displayName)
            } else if let error = viewModel.loadError {
                Text(error)
                Button("Try again") { Task { await load() } }
            } else {
                ProgressView()
            }
        }
    }

    private func load() async {
        await viewModel.appear()
        await partnerViewModel.appear()
    }
}
```

`TargetsSection.swift`:

```swift
import SwiftUI

struct TargetsSection: View {
    @Bindable var viewModel: ProfileViewModel

    var body: some View {
        Section {
            field("Calories (kcal)", text: $viewModel.draft.calories, field: .calories, id: "targetCaloriesField")
            field("Protein (g)", text: $viewModel.draft.protein, field: .protein, id: "targetProteinField")
            field("Carbohydrates (g)", text: $viewModel.draft.carbs, field: .carbs, id: "targetCarbsField")
            field("Fat (g)", text: $viewModel.draft.fat, field: .fat, id: "targetFatField")
            if let banner = viewModel.banner {
                Text(banner).font(.footnote).foregroundStyle(.red).accessibilityIdentifier("targetsBanner")
            }
            Button("Save targets") { Task { await viewModel.saveTargets() } }
                .disabled(!viewModel.canSave)
                .accessibilityIdentifier("targetsSaveButton")
        } header: {
            Text("Daily targets")
        } footer: {
            Text("Leave a field blank for no target.")
        }
    }

    private func field(_ title: String, text: Binding<String>, field: TargetsDraft.Field, id: String) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            TextField(title, text: text).decimalKeyboard().accessibilityIdentifier(id)
            if let message = viewModel.fieldErrors[field] {
                Text(message).font(.caption).foregroundStyle(.red)
            }
        }
    }
}
```

`PartnerSection.swift`:

```swift
import API
import Repositories
import SwiftUI
#if os(iOS)
import UIKit
#endif

struct PartnerSection: View {
    @Bindable var viewModel: PartnerViewModel
    @State private var confirmingUnlink = false

    var body: some View {
        Section {
            switch viewModel.phase {
            case .loading:
                loading
            case .none:
                inviteBlock
                acceptBlock
            case .pending(let expiresAt):
                Text(expiresAt.map { "Waiting for your partner. The code expires \($0.formatted(date: .abbreviated, time: .shortened))." } ?? "Waiting for your partner.")
                    .accessibilityIdentifier("partnerPendingLabel")
                inviteBlock
                Button("Cancel invite", role: .destructive) { Task { await viewModel.unlink() } }
                    .disabled(viewModel.isBusy)
                    .accessibilityIdentifier("partnerCancelInviteButton")
                acceptBlock
            case .linked(let name, let since):
                Text("Linked with \(name)" + (since.map { " since \($0.formatted(date: .abbreviated, time: .omitted))" } ?? ""))
                    .accessibilityIdentifier("partnerLinkedLabel")
                Button("Unlink", role: .destructive) { confirmingUnlink = true }
                    .disabled(viewModel.isBusy)
                    .accessibilityIdentifier("partnerUnlinkButton")
            }
        } header: {
            Text("Partner")
        } footer: {
            Text("Link with one partner to share meals, diet templates and live shopping lists.")
        }
        .confirmationDialog("Unlink from your partner?", isPresented: $confirmingUnlink, titleVisibility: .visible) {
            Button("Unlink", role: .destructive) { Task { await viewModel.unlink() } }
        } message: {
            Text("You will stop seeing each other's shared meals, diet templates and shopping lists. Copies stay with whoever made them.")
        }
        .alert(
            "Partner",
            isPresented: Binding(get: { viewModel.alertMessage != nil }, set: { if !$0 { viewModel.alertMessage = nil } })
        ) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(viewModel.alertMessage ?? "")
        }
    }

    @ViewBuilder
    private var loading: some View {
        if let error = viewModel.loadError {
            Text(error)
            Button("Try again") { Task { await viewModel.appear() } }
        } else {
            ProgressView()
        }
    }

    @ViewBuilder
    private var inviteBlock: some View {
        if let invite = viewModel.invite {
            VStack(alignment: .leading, spacing: 6) {
                Text(InviteCodeFormat.display(invite.code))
                    .font(.system(.title2, design: .monospaced))
                    .textSelection(.enabled)
                    .accessibilityLabel(InviteCodeFormat.spoken(invite.code))
                    .accessibilityIdentifier("partnerInviteCode")
                Text("Shown once. Share it with your partner. It expires \(invite.expiresAt.formatted(date: .abbreviated, time: .shortened)).")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                HStack {
                    ShareLink(item: invite.code) { Label("Share", systemImage: "square.and.arrow.up") }
                        .buttonStyle(.borderless)
                    copyButton(invite.code)
                }
            }
        }
        Button(viewModel.invite == nil ? "Create invite code" : "Create a new code") { Task { await viewModel.createInvite() } }
            .disabled(viewModel.isBusy)
            .accessibilityIdentifier("partnerCreateInviteButton")
    }

    @ViewBuilder
    private func copyButton(_ code: String) -> some View {
        #if os(iOS)
        Button { UIPasteboard.general.string = code } label: { Label("Copy", systemImage: "doc.on.doc") }
            .buttonStyle(.borderless)
        #endif
    }

    @ViewBuilder
    private var acceptBlock: some View {
        VStack(alignment: .leading, spacing: 4) {
            TextField("Enter your partner's code", text: $viewModel.codeText)
                .noAutocapitalization()
                .accessibilityIdentifier("partnerCodeField")
            if let message = viewModel.acceptError {
                Text(message).font(.caption).foregroundStyle(.red).accessibilityIdentifier("partnerAcceptError")
            }
        }
        Button("Link accounts") { Task { await viewModel.accept() } }
            .disabled(viewModel.isBusy)
            .accessibilityIdentifier("partnerLinkButton")
    }
}
```

`DeleteAccountSheet.swift`:

```swift
import SwiftUI

/// The one action that cannot be undone. The API asks for no re-authentication, so the sheet asks for the email itself.
struct DeleteAccountSheet: View {
    let email: String
    /// Returns the text to show, or `nil` once the account is gone (the app has signed out and this sheet goes with it).
    let onDelete: (String) async -> String?
    @Environment(\.dismiss) private var dismiss
    @State private var typed = ""
    @State private var message: String?
    @State private var isDeleting = false

    private var matches: Bool { ProfileViewModel.emailMatches(typed, email) }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Text("This permanently deletes your account and everything it owns: your meals, diet templates, plan and shopping lists. A partner keeps only the copies they made.")
                }
                Section {
                    TextField("Type \(email) to confirm", text: $typed)
                        .noAutocapitalization()
                        .accessibilityIdentifier("deleteAccountEmailField")
                    if let message {
                        Text(message).foregroundStyle(.red).accessibilityIdentifier("deleteAccountMessage")
                    }
                }
                Section {
                    Button("Delete my account", role: .destructive) { Task { await delete() } }
                        .disabled(!matches || isDeleting)
                        .accessibilityIdentifier("deleteAccountConfirmButton")
                }
            }
            .navigationTitle("Delete account")
            .inlineNavigationTitle()
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Keep my account") { dismiss() } }
            }
        }
    }

    private func delete() async {
        isDeleting = true
        defer { isDeleting = false }
        message = await onDelete(typed)
    }
}
```

`MyIngredientsView.swift`:

```swift
import API
import Repositories
import SwiftUI

struct MyIngredientsView: View {
    @State private var viewModel: MyIngredientsViewModel
    @State private var creating = false
    @State private var editing: IngredientRef?
    @State private var deleting: IngredientRef?
    @State private var alertMessage: String?
    private let repository: IngredientsRepository

    /// `Components.Schemas.Ingredient` is not `Identifiable`; this is what the sheet and dialog bind to.
    private struct IngredientRef: Identifiable {
        let ingredient: Components.Schemas.Ingredient
        var id: String { ingredient.id }
    }

    init(repository: IngredientsRepository) {
        self.repository = repository
        _viewModel = State(initialValue: MyIngredientsViewModel(repository: repository))
    }

    var body: some View {
        @Bindable var viewModel = viewModel
        List { content }
            .navigationTitle("My ingredients")
            .searchable(text: $viewModel.searchText, prompt: "Search my ingredients")
            .toolbar {
                ToolbarItem(placement: .primaryAction) {
                    Button { creating = true } label: { Label("New ingredient", systemImage: "plus") }
                        .accessibilityIdentifier("myIngredientsNewButton")
                }
            }
            .task { await viewModel.load() }
            .refreshable { await viewModel.load() }
            .sheet(isPresented: $creating) {
                CustomIngredientView(name: viewModel.searchText.trimmingCharacters(in: .whitespacesAndNewlines), repository: repository) { created in
                    creating = false
                    viewModel.didSave(created)
                }
            }
            .sheet(item: $editing) { ref in
                CustomIngredientView(editing: ref.ingredient, repository: repository) { saved in
                    editing = nil
                    viewModel.didSave(saved)
                }
            }
            .confirmationDialog(
                "Delete this ingredient?",
                isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }),
                titleVisibility: .visible,
                presenting: deleting
            ) { ref in
                Button("Delete ingredient", role: .destructive) { Task { await delete(ref.ingredient) } }
            } message: { ref in
                Text("Delete “\(ref.ingredient.name)”? An ingredient a meal still uses cannot be deleted; remove it from those meals first.")
            }
            .alert(
                "Ingredient",
                isPresented: Binding(get: { alertMessage != nil }, set: { if !$0 { alertMessage = nil } })
            ) {
                Button("OK", role: .cancel) {}
            } message: {
                Text(alertMessage ?? "")
            }
    }

    @ViewBuilder
    private var content: some View {
        if viewModel.isLoading && viewModel.all.isEmpty {
            ProgressView()
        } else if let error = viewModel.loadError, viewModel.all.isEmpty {
            Text(error)
            Button("Try again") { Task { await viewModel.load() } }
        } else if viewModel.shown.isEmpty {
            Text(viewModel.all.isEmpty ? "You haven't created any custom ingredients yet." : "No ingredient matches.")
                .foregroundStyle(.secondary)
        } else {
            ForEach(viewModel.shown, id: \.id) { ingredient in row(ingredient) }
        }
    }

    private func row(_ ingredient: Components.Schemas.Ingredient) -> some View {
        HStack {
            VStack(alignment: .leading) {
                Text(ingredient.name).accessibilityIdentifier("myIngredientRow-\(ingredient.name)")
                Text(ingredient.category.label).font(.footnote).foregroundStyle(.secondary)
            }
            Spacer()
            Menu {
                Button("Edit") { editing = IngredientRef(ingredient: ingredient) }
                    .accessibilityIdentifier("ingredientEditButton")
                Button("Delete", role: .destructive) { deleting = IngredientRef(ingredient: ingredient) }
                    .accessibilityIdentifier("ingredientDeleteButton")
            } label: {
                Image(systemName: "ellipsis.circle")
            }
            .accessibilityLabel("Actions for \(ingredient.name)")
            .accessibilityIdentifier("ingredientMenu-\(ingredient.name)")
        }
    }

    private func delete(_ ingredient: Components.Schemas.Ingredient) async {
        if let message = await viewModel.delete(ingredient) { alertMessage = message }
    }
}
```

`CustomIngredientView.swift` edits:

1. Replace the doc comment and the stored properties and initialiser (everything from `/// The creation form reachable from search` through the closing brace of `init(name:repository:onCreated:)`) with:

```swift
/// The custom-ingredient form, as a sheet: creating one (reachable from search, over the search sheet) or editing one
/// of mine (from Profile's "My ingredients"). Editing shows the four main nutrients and keeps every other one.
struct CustomIngredientView: View {
    @State private var viewModel: CustomIngredientViewModel
    @Environment(\.dismiss) private var dismiss
    private let title: String
    private let onSaved: (Components.Schemas.Ingredient) -> Void

    init(name: String, repository: IngredientsRepository, onCreated: @escaping (Components.Schemas.Ingredient) -> Void) {
        self.title = "Custom ingredient"
        self.onSaved = onCreated
        _viewModel = State(initialValue: CustomIngredientViewModel(name: name, repository: repository))
    }

    init(editing ingredient: Components.Schemas.Ingredient, repository: IngredientsRepository, onSaved: @escaping (Components.Schemas.Ingredient) -> Void) {
        self.title = "Edit ingredient"
        self.onSaved = onSaved
        _viewModel = State(initialValue: CustomIngredientViewModel(editing: ingredient, repository: repository))
    }
```
2. Replace `Task { if let ingredient = await viewModel.save() { onCreated(ingredient) } }` with `Task { if let ingredient = await viewModel.save() { onSaved(ingredient) } }`.
3. Replace `.navigationTitle("Custom ingredient")` with `.navigationTitle(title)`.

`ClearCaches.swift` (replace the file):

```swift
import Repositories

/// Everything that must be emptied whenever the session ends, by any path (`AppState.clearCaches`), so a second user
/// on this device never sees the first user's meals, plan, templates, shopping lists or profile, never sends their
/// queued changes, and never sees a sync notice meant for them.
func clearAllCaches(
    meals: MealsRepository, plan: PlanRepository, templates: TemplatesRepository, shopping: ShoppingListsRepository,
    profile: ProfileRepository, sync: ShoppingSyncEngine
) async {
    await meals.clearCaches()
    await plan.clearCaches()
    await templates.clearCaches()
    await shopping.clearCaches()
    await profile.clearCaches()
    await sync.reset()
}
```

`TabShellView.swift`: add `let profileDependencies: ProfileDependencies` after `let shoppingDependencies: ShoppingDependencies`, and replace

```swift
            NavigationStack {
                ProfileView(onSignOut: { Task { await appState.signOut() } })
            }
```
with
```swift
            NavigationStack {
                ProfileView(dependencies: profileDependencies, onSignOut: { Task { await appState.signOut() } })
            }
```

`RootView.swift` (six edits):

1. After `private let shoppingDependencies: ShoppingDependencies` add `private let profileDependencies: ProfileDependencies`.
2. Replace the `middlewares:` line of `makeClient` with:
```swift
            // The null sentinel is rewritten before the bearer middleware sees the request, so a retry after a 401
            // still has a replayable body.
            middlewares: (networkSwitch.map { [OfflineMiddleware($0) as any ClientMiddleware] } ?? [])
                + ([NullSentinelMiddleware(), BearerAuthMiddleware(refresher: refresher)] as [any ClientMiddleware])
```
3. Replace `let partnerRepository = PartnerRepository(client: client)` with:
```swift
        let profileCache = CacheStore.makeProfileCache(container)
        let profileRepository = ProfileRepository(client: client, cache: profileCache)
        let partnerRepository = PartnerRepository(client: client, cache: profileCache)
        let ingredientsRepository = IngredientsRepository(client: client)
```
and in the `MealsDependencies(` call replace `ingredients: IngredientsRepository(client: client),` with `ingredients: ingredientsRepository,`.
4. Before `let userBox = UserBox()` add:
```swift
        self.profileDependencies = ProfileDependencies(
            profile: profileRepository, plan: planRepository, partner: partnerRepository, ingredients: ingredientsRepository
        )
```
5. In the `clearAllCaches(` call replace `meals: mealsRepository, plan: planRepository, templates: templatesRepository, shopping: shoppingRepository,` and the next line `sync: syncEngine` with:
```swift
                    meals: mealsRepository, plan: planRepository, templates: templatesRepository, shopping: shoppingRepository,
                    profile: profileRepository, sync: syncEngine
```
6. In the `TabShellView(` call replace `shoppingDependencies: shoppingDependencies` with `shoppingDependencies: shoppingDependencies, profileDependencies: profileDependencies`.

- [ ] **Step 3: Build and run the tests**

Run, from `ios/MealPlannerKit`: `swift build`, then `swift test --filter ClearCachesTests`, then the whole `swift test`.
Expected: build succeeds; `ClearCachesTests` PASS; whole suite green. Then, from the worktree root: `make build-ios`.
Expected: `** BUILD SUCCEEDED **` (the first compile of the iOS-only paths: a failure is most likely a UIKit-only modifier that needs `#if os(iOS)`).

The plan's SwiftUI was written without compiling. Fix compile errors minimally: a `@ViewBuilder` function whose body is entirely inside `#if` (`copyButton`) may need an explicit `EmptyView()` in an `#else` branch; `confirmationDialog(presenting:)` and `searchable` signatures may differ slightly. Keep every accessibility identifier exactly as listed.

- [ ] **Step 4: Commit**

```bash
git add ios/MealPlannerKit/Sources ios/MealPlannerKit/Tests/MealPlannerKitTests/ClearCachesTests.swift
git commit -m "feat(ios): add the Profile screens and wire them into the app

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 12: The XCUITest flow

**Files:**
- Create: `ios/MealPlannerUITests/ProfileFlowUITests.swift`

**Interfaces:**
- Consumes: `AppUITestCase` helpers (`uniqueEmail`, `createAccountViaAPI`, `apiRequest`, `signIn`, `tabButton`, `profileTabButton`, `waitUntilHittable`, `expectValue`, `typeVerified`, `clearAndType`, `scrollUntilExists`); the accessibility identifiers from Task 11; the existing `customIngredientNameField`, `customIngredientSaveButton`, `showRegisterButton`, `ring-calories`.
- Produces: the flow in spec §7: set a calorie target and see Today's ring use it, link a partner by invite code, create/edit/delete a custom ingredient, delete the account and land on the welcome screen.

- [ ] **Step 1: Write the test**

```swift
import XCTest

final class ProfileFlowUITests: AppUITestCase {
    private func localToday() -> String {
        let formatter = DateFormatter()
        formatter.dateFormat = "yyyy-MM-dd"
        formatter.locale = Locale(identifier: "en_US_POSIX")
        return formatter.string(from: Date())
    }

    /// Taps `element` once it can be hit, scrolling the form first if the keyboard or the fold is in the way.
    private func tapWhenHittable(_ element: XCUIElement, in app: XCUIApplication) {
        for _ in 0..<3 where !element.isHittable { app.swipeUp() }
        waitUntilHittable(element, timeout: 45)
        element.tap()
    }

    private func openProfile(in app: XCUIApplication) {
        let profileTab = profileTabButton(in: app)
        XCTAssertTrue(profileTab.waitForExistence(timeout: 45))
        // A tab tap right after the shell appears is sometimes lost: tap again until the screen is up.
        let email = app.staticTexts["profileEmail"]
        for _ in 0..<4 where !email.exists {
            profileTab.tap()
            _ = email.waitForExistence(timeout: 10)
        }
    }

    /// Profile end to end. A signs in with a plan for today that has a 100 kcal meal; sets a calorie target of 2000 and
    /// sees it on Today's ring; enters the invite code of a second account (made over the API) and sees the link; creates,
    /// renames and deletes a custom ingredient; deletes the account by typing the email, and lands on the welcome screen.
    func testTargetsPartnerIngredientsAndDeletingTheAccount() throws {
        let password = "correct-horse-battery-staple"
        let emailA = uniqueEmail()
        let tokenA = try createAccountViaAPI(email: emailA, password: password, displayName: "Profile A")
        let tokenB = try createAccountViaAPI(email: uniqueEmail(), password: password, displayName: "Profile B")
        let invite = try apiRequest("POST", "/partner/invite", token: tokenB, expect: 201)
        let code = try XCTUnwrap(invite["code"] as? String)

        // A plan for today, so the Today ring has an amount to put against the target.
        let ingredient = try apiRequest(
            "POST", "/ingredients", token: tokenA,
            json: ["name": "UI Profile Oats", "category": "other", "nutrients": ["calories": 100]], expect: 201
        )
        let meal = try apiRequest("POST", "/meals", token: tokenA, json: ["name": "UI Profile Meal", "servings": 1], expect: 201)
        let mealID = try XCTUnwrap(meal["id"] as? String)
        try apiRequest(
            "PUT", "/meals/\(mealID)/ingredients", token: tokenA,
            json: ["items": [["ingredient_id": try XCTUnwrap(ingredient["id"] as? String), "quantity": 100, "unit": "g"]]], expect: 200
        )
        try apiRequest("PUT", "/plan/\(localToday())/breakfast", token: tokenA, json: ["meal_id": mealID, "portion": 1], expect: 200)

        let app = XCUIApplication()
        app.launch()
        // Whatever fails below, never leave A signed in for the next test. Tolerant: with no tab shell (already signed
        // out, deleted, or sign-in never finished) there is nothing to do.
        addTeardownBlock {
            let profileTab = app.buttons.matching(NSPredicate(format: "identifier == %@ OR label == %@", "profileTab", "Profile")).firstMatch
            guard app.state == .runningForeground, profileTab.waitForExistence(timeout: 5) else { return }
            profileTab.tap()
            let signOut = app.buttons["signOutButton"]
            if signOut.waitForExistence(timeout: 10) { signOut.tap() }
        }
        signIn(in: app, email: emailA, password: password)

        // 1. A calorie target of 2000, then Today's ring shows it.
        openProfile(in: app)
        let calories = app.textFields["targetCaloriesField"]
        XCTAssertTrue(scrollUntilExists(calories, in: app))
        waitUntilHittable(calories, timeout: 45)
        calories.tap()
        typeVerified("2000", field: calories)
        tapWhenHittable(app.buttons["targetsSaveButton"], in: app)

        let todayTab = tabButton(in: app, identifier: "todayTab", label: "Today")
        waitUntilHittable(todayTab)
        todayTab.tap()
        let ring = app.descendants(matching: .any).matching(identifier: "ring-calories").firstMatch
        XCTAssertTrue(ring.waitForExistence(timeout: 45))
        expectValue(of: ring, toContain: "2,000")

        // 2. Link with the second account by its invite code.
        openProfile(in: app)
        let codeField = app.textFields["partnerCodeField"]
        XCTAssertTrue(scrollUntilExists(codeField, in: app))
        waitUntilHittable(codeField, timeout: 45)
        codeField.tap()
        typeVerified(code, field: codeField)
        tapWhenHittable(app.buttons["partnerLinkButton"], in: app)
        let linked = app.staticTexts["partnerLinkedLabel"]
        XCTAssertTrue(linked.waitForExistence(timeout: 45), "Expected the linked state after entering the code")
        XCTAssertTrue(linked.label.contains("Profile B"), "Expected the partner's name, got \(linked.label)")

        // 3. A custom ingredient: create, rename, delete.
        let ingredientsRow = app.descendants(matching: .any)["myIngredientsRow"]
        XCTAssertTrue(scrollUntilExists(ingredientsRow, in: app))
        waitUntilHittable(ingredientsRow, timeout: 45)
        ingredientsRow.tap()

        let newButton = app.buttons["myIngredientsNewButton"]
        waitUntilHittable(newButton, timeout: 45)
        newButton.tap()
        let nameField = app.textFields["customIngredientNameField"]
        waitUntilHittable(nameField, timeout: 45)
        nameField.tap()
        typeVerified("UI Profile Jam", field: nameField)
        tapWhenHittable(app.buttons["customIngredientSaveButton"], in: app)
        XCTAssertTrue(app.staticTexts["myIngredientRow-UI Profile Jam"].waitForExistence(timeout: 45), "Expected the new ingredient in the list")

        let menu = app.buttons["ingredientMenu-UI Profile Jam"]
        waitUntilHittable(menu)
        menu.tap()
        let edit = app.buttons["ingredientEditButton"]
        waitUntilHittable(edit)
        edit.tap()
        let renameField = app.textFields["customIngredientNameField"]
        waitUntilHittable(renameField, timeout: 45)
        clearAndType("UI Profile Jam 2", field: renameField)
        tapWhenHittable(app.buttons["customIngredientSaveButton"], in: app)
        XCTAssertTrue(app.staticTexts["myIngredientRow-UI Profile Jam 2"].waitForExistence(timeout: 45), "Expected the renamed ingredient")

        let renamedMenu = app.buttons["ingredientMenu-UI Profile Jam 2"]
        waitUntilHittable(renamedMenu)
        renamedMenu.tap()
        let delete = app.buttons["ingredientDeleteButton"]
        waitUntilHittable(delete)
        delete.tap()
        let confirmDelete = app.buttons["Delete ingredient"]
        waitUntilHittable(confirmDelete)
        confirmDelete.tap()
        let gone = XCTNSPredicateExpectation(predicate: NSPredicate(format: "exists == false"), object: app.staticTexts["myIngredientRow-UI Profile Jam 2"])
        XCTAssertEqual(XCTWaiter().wait(for: [gone], timeout: 45), .completed, "Expected the ingredient to disappear after deleting it")

        // 4. Delete the account: the email is typed, and the welcome screen follows.
        app.navigationBars.buttons.firstMatch.tap() // back to Profile
        let deleteAccount = app.buttons["deleteAccountButton"]
        XCTAssertTrue(scrollUntilExists(deleteAccount, in: app))
        waitUntilHittable(deleteAccount, timeout: 45)
        deleteAccount.tap()
        let emailField = app.textFields["deleteAccountEmailField"]
        waitUntilHittable(emailField, timeout: 45)
        emailField.tap()
        typeVerified(emailA, field: emailField)
        tapWhenHittable(app.buttons["deleteAccountConfirmButton"], in: app)
        XCTAssertTrue(app.buttons["showRegisterButton"].waitForExistence(timeout: 45), "Expected the welcome screen after deleting the account")

        // The account is really gone on the server: its credentials no longer work.
        try apiRequest("POST", "/auth/login", json: ["email": emailA, "password": password], expect: 401)
    }
}
```

- [ ] **Step 2: Compile the UI-test target**

Run: `cd ios && xcodegen generate && xcodebuild build-for-testing -project MealPlanner.xcodeproj -scheme MealPlanner -destination 'generic/platform=iOS Simulator' 2>&1 | grep -E "error:|TEST BUILD"`
Expected: `** TEST BUILD SUCCEEDED **`.

- [ ] **Step 3: Run the flow if the machine allows (optional locally; CI is the signal)**

Start the API (`make db-up`, `make migrate`, `make run-api` from the worktree root, the API in a second terminal), then:
`cd ios && xcodebuild test -project MealPlanner.xcodeproj -scheme MealPlanner -destination 'platform=iOS Simulator,name=iPhone 17 Pro' -only-testing:MealPlannerUITests/ProfileFlowUITests`
Expected: the test passes. Local XCUITest is unreliable under load (the Shopping flow needed retries for dropped taps); say exactly what you could and could not run, and do not claim a pass you did not see. If a failed run leaves a Keychain session behind, erase the simulator before the next run. Things most likely to need a test-side adjustment: the ring text if the day's calories come back as "no data" (then assert `"2,000"` only after the seeded meal shows), a menu item that is not hittable the first time, a tab tap that is lost. Fix the test; report an app problem with evidence rather than changing the app silently.

- [ ] **Step 4: Commit**

```bash
git add ios/MealPlannerUITests/ProfileFlowUITests.swift
git commit -m "test(ios): add the Profile XCUITest flow: targets, partner, ingredients, account deletion

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 13: Docs, spec reconciliation and the final gate

**Files:**
- Modify: `ios/CLAUDE.md`
- Modify: `docs/superpowers/specs/2026-10-10-ios-profile-design.md`
- Modify: `docs/superpowers/specs/2026-09-30-ios-app-design.md` (§6, §9, §15)
- Modify: `docs/superpowers/plans/2026-10-10-ios-profile.md` (tick the boxes)

- [ ] **Step 1: Update `ios/CLAUDE.md`**

Read the file first and match its style and place. In the **Layout** list add to the matching bullets:
- `Sources/Persistence/`: "Also `ProfileCache` (the signed-in `User` and the partnership; `CachedPartnership` is `unknown` / `none` / `present`) and `PlanCache.setTargets`."
- `Sources/Repositories/`: "Also `ProfileRepository` (`cachedUser()` / `refreshUser()`, `updateTargets`, `deleteAccount`), the extended `PartnerRepository` (`createInvite`, `accept(code:)`, `unlink`; `status()` also fills `ProfileCache`), `IngredientsRepository.customIngredients/update/delete`, `NullSentinelMiddleware`, and `PlanRepository.storeTargets(from:)`."
- `Sources/Features/`: "`Profile/`: `ProfileViewModel`, `PartnerViewModel`, `MyIngredientsViewModel`, pure `TargetsDraft` and `InviteCodeFormat`, and the views."

In **Gotchas** add (merge with an existing line only where it says the same thing):

```
- The generated request types cannot send an explicit JSON `null` (optionals are omitted when `nil`), but `PATCH /me` and `PATCH /ingredients/{id}` clear a field with `null`. A repository clears by sending `NullSentinel.value` (`-1`), and `NullSentinelMiddleware` rewrites top-level `-1` to `null` for `updateMe` and `updateIngredient` only. `-1` is invalid for every clearable field, so a missing middleware gives a `400`, never silent data change. The middleware sits outside `BearerAuthMiddleware` so a retried request still has a replayable body. View models and views never see the sentinel.
- Targets live in two places: the `User` (`ProfileCache`) and `PlanCache` (what the Today rings read). `PATCH /me` answers with the updated user, and `ProfileViewModel` writes it to both (`PlanRepository.storeTargets`) so Today follows without a plan refetch. Save sends all four targets every time (a blank one is the clear sentinel).
- `ProfileCache` exists so Profile opens offline and on an `unverified` launch, where `AppState` has no `User` (it only gets one after `GET /me`). It fills the first time Profile loads online; it is cleared on sign-out with the other caches.
- The invite code is returned once and held only in `PartnerViewModel`; a pending invite fetched later shows its expiry, never the code. The server ignores case, spaces and dashes in a code, so the text goes as typed, trimmed. `unlink()` treats `404` as done. The accept route has its own tighter rate limit (`PartnerError.rateLimited` has its own wording).
- Editing an ingredient sends all 18 nutrients (the API replaces the whole set): `CustomIngredientForm.validateUpdate(preserving:)` takes the four shown from the form and copies the other 14 from the existing ingredient. A blank weight per piece or density clears it. `409 unit_not_convertible` means a meal still uses that unit.
- "My ingredients" loads every page of `GET /ingredients` (limit 100) and keeps `isCustom`, because the API has no owner filter. It is not cached.
- Deleting the account types the email (as web), calls `DELETE /me`, then runs the normal `AppState.signOut()`; its revoke of the already-dead token fails quietly. Nothing is sent while the typed email does not match.
```

- [ ] **Step 2: Reconcile the specs with what was built**

In `docs/superpowers/specs/2026-10-10-ios-profile-design.md`:
- §3: `CachedPartnership` cases are `unknown`, `none` and `present(Partnership)` (a pending invite is a partnership, not "linked"); `PartnerRepository.init(client:cache:)` takes `ProfileCache? = nil` so existing callers compile; `ProfileViewModel` takes a `signOut` closure; `IngredientError` gains `.notFound`, `.inUse`, `.unitInUse`; `TargetsUpdate` and `IngredientUpdate` live in `Repositories`.
- §3/§5: `NullSentinelMiddleware` is composed outside `BearerAuthMiddleware`.
- §4: ProfileCache is not seeded at sign-in (a first-ever offline launch before Profile was opened shows the error state with retry).
- Anything else the implementation changed while the plan was executed.

In `docs/superpowers/specs/2026-09-30-ios-app-design.md`: §6 cache list says "profile" is `ProfileCache` and its writes are online-only; §9 `Features/Profile` points to `2026-10-10-ios-profile-design.md` and mentions the clear sentinel; §15 plan 4 is complete.

- [ ] **Step 3: Tick this plan's boxes**

Run (from the worktree root): `sed -i '' 's/^- \[ \]/- [x]/' docs/superpowers/plans/2026-10-10-ios-profile.md`

- [ ] **Step 4: Run the full gate**

Run, from the repo root:
- `make test-ios` — expected: all suites PASS.
- `make build-ios` — expected: `** BUILD SUCCEEDED **`.
- `cd ios && xcodebuild build-for-testing -project MealPlanner.xcodeproj -scheme MealPlanner -destination 'generic/platform=iOS Simulator' 2>&1 | grep -E "error:|TEST BUILD"` — expected: `** TEST BUILD SUCCEEDED **`.
- `make check` (needs Docker running) — expected: green. `openapi.yaml` did not change, so `check-generated-ios` must still pass: if it fails, something edited generated code; revert it.

Run `swift test` from `ios/MealPlannerKit` three times and report any intermittent failure with its test name rather than rerunning until green (the Shopping branch saw about one in twelve runs crash inside CoreData when tests create in-memory stores in parallel; say so if it recurs, with the crash report).

- [ ] **Step 5: Commit**

```bash
git add ios/CLAUDE.md docs/superpowers
git commit -m "docs(ios): document the Profile design in ios/CLAUDE.md and reconcile the specs

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

Then open the pull request (the CI `ios.yml` `ui` job is the signal for Task 12) and comment on issue #28 with the PR link, saying the Profile half is done.

---

## Self-review

**Spec coverage** (spec section → task):

| Spec | Task |
|---|---|
| §1 goal, success | all; the gate in 13 |
| §2 data source: `ProfileCache`, offline and unverified launch | 4, 8, 11 |
| §2 writes online-only, no queue | 5, 6, 7, 8, 9, 10 |
| §2 targets in two places, one `PATCH` answer feeds both | 4 (`setTargets`), 5 (`storeTargets`), 8 |
| §2 clearing a field, sentinel and middleware | 1, 5, 7, 8, 10 |
| §2 "mine only" ingredients | 7, 10 |
| §2 invite code in memory only | 9 |
| §2 deleting the account, typed email, `signOut()` | 8, 11 |
| §3 modules | 1–11 (file map above) |
| §4 Profile `Form`, offline label | 11 |
| §4 targets rules and messages | 2, 8 |
| §4 partner not linked / pending / linked, errors | 6, 9, 11 |
| §4 my ingredients: list, search, create, edit, delete | 7, 10, 11 |
| §4 delete account | 8, 11 |
| §4 accessibility | 11 (labels, spoken code, identifiers) |
| §5 state and error rules, sign-out clearing | 5–11 (clearing: 11) |
| §6 testing | every task's tests |
| §7 XCUITest | 12 |
| §8 docs | 13 |
| §9 build order | task order (the screens and the wiring share Task 11 so the build stays green) |
| §10 parent-spec updates | 13 |

**Placeholder scan:** none ("TBD", "TODO" and "fill in" do not appear; every step has the code or the exact edit).

**Type consistency:** `NullSentinel.value` (Task 1) is used by 5 and 7; `TargetsUpdate` (2) by 5 and 8; `TargetsDraft` (2) by 8 and 11; `IngredientUpdate` (3) by 7 and 10; `CustomIngredientForm.validateUpdate(preserving:)` (3) by 10; `CachedPartnership` (4) by 6 and 9; `ProfileRepository` (5) by 8 and 11; `PartnerRepository.cachedStatus()` (6) by 9; `PartnerError` (6) by 9; `IngredientError.notFound/.inUse/.unitInUse` (7) by 10; `ProfileViewModel.emailMatches` (8) by 11's `DeleteAccountSheet`; `ProfileDependencies` and `clearAllCaches(...profile:...)` (11) by the RootView wiring in the same task; test helpers `ProfileFixtures` (1), `ProfileServer`/`ProfileHarness` (8).

**Not compiled:** every code block was written against the generated client's signatures as read from `Types+Components+Schemas.swift` and `Types+Operations.swift` and against the merged code on `master` (`d40f0ac`), but none has been compiled or run. The first run of each task's test step is the real check; where a generated label differs, match the generated file and never edit it.
