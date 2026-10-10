# iOS Profile Design

Date: 2026-10-10
Status: draft for review. Extends `2026-09-30-ios-app-design.md` (§3, §4, §6, §9, §11, §12, §15 plan 4, Profile half) and builds on `2026-10-08-ios-shopping-design.md`, whose repository/cache/view-model conventions it reuses. Where this document and the iOS spec disagree on Profile, this document wins and the iOS spec is updated in the same change (§10). The shipped web feature (`web/src/features/profile/`, `partner/`, `ingredients/`) is the parity reference; every "as web" below was read from that code or from `web/CLAUDE.md`. GitHub issue: #28 (this is its second half; Shopping shipped separately).

## 1. Goal

Replace the Profile tab's stub (a Sign Out button) with the real feature, on the existing API and with no contract change: set the four daily targets, link with a partner by invite code (create a code, enter a code, unlink), manage my custom ingredients (create, edit, delete), and delete the account. The tab keeps its Sign Out button.

Success: `swift test` passes for the pure modules (targets draft, invite code format, ingredient update builder, null-sentinel middleware), the profile cache, the repositories and the view models; one XCUITest flow passes against the real API (§7); `make check` stays green and both `ios.yml` jobs pass.

Out of scope: editing the display name (the API supports it; nothing asks for it), Sign in with Apple and its token revocation on deletion (#29), offline writes of any kind, browsing the whole ingredient catalogue (Meals already has ingredient search), notifications.

## 2. Decisions

| Topic | Decision |
|---|---|
| Data source | A small SwiftData cache, `ProfileCache`, holds the `User` and the partnership, like the other tabs. Profile opens instantly and works on an offline or `unverified` launch, where `AppState` has no `User` (it only gets one after `GET /me` succeeds). Reads: `cached…()` then `refresh…()`. |
| Writes | Online-only. A failure shows a retryable message and changes nothing. No queue. |
| Targets | Live in two places: the `User` (this cache) and `PlanCache` (what the Today rings read). `PATCH /me` answers with the updated `User`; one answer updates both, with no plan refetch. |
| Clearing a field | The server treats an explicit `null` as "clear" and an omitted field as "unchanged" (`toOptional`, `backend/internal/httpapi/account.go`). The generated Swift request types are plain optionals and cannot send `null`. So the repository sends the sentinel `-1` and a small client middleware, active only for `updateMe` and `updateIngredient`, rewrites top-level `-1` values to JSON `null`. `-1` is invalid for every field that can be cleared, so a middleware that failed to run would produce a `400`, never a silent change. |
| "Mine only" ingredients | As the iOS spec §9 locked: `GET /ingredients` has no owner filter, so the screen loads every page (limit 100) and keeps `is_custom`. The catalogue is the USDA Foundation Foods subset plus the person's own rows; the imported row count was not measured when this was written, so the plan counts it and escalates if it is large. |
| Invite code | The API returns it once. The app holds it in memory for as long as the Profile screen lives and never persists it. A pending invite fetched later shows only its expiry. |
| Deleting the account | Types the account's email to confirm (as web, even though `DELETE /me` needs no re-authentication), calls `DELETE /me`, then runs the existing `AppState.signOut()`. That clears the Keychain and every cache; its best-effort revoke of the now-dead token fails quietly (`AuthRepository.revoke` ignores errors). |
| Contract | Unchanged. |

## 3. Modules

All in `ios/MealPlannerKit/Sources/`.

- `Persistence/ProfileCache` (`@ModelActor`): `user()`, `store(user:)`, `partnership() -> CachedPartnership` where `CachedPartnership` is `unknown` (never fetched), `none` (fetched, no partner) or `linked(Partnership)`, `store(partnership:)`, `clearAll()`. Two internal `@Model` rows (the user as JSON; the partnership as optional JSON, whose absence means `unknown`). Added to `CacheStore`'s schema with `makeProfileCache`.
- `Persistence/PlanCache`: new `setTargets(_:)` writes only the targets row.
- `Repositories/ProfileRepository`: `cachedUser()`, `refreshUser()` (`GET /me`), `updateTargets(_ update: TargetsUpdate) -> User` (`PATCH /me`, stores the answer), `deleteAccount()` (`DELETE /me`), `clearCaches()`. `TargetsUpdate` holds four `Double?`; `nil` means clear. One exhaustive `switch` per response into `ProfileError` (`validationFailed(String)`, `unauthorized`, `rateLimited`, `server(String)`); every call goes through `unwrapping`.
- `Repositories/PartnerRepository`: now takes the `ProfileCache`. `status()` keeps its signature (three view models call it) and also stores its answer, so the other tabs keep Profile warm. New: `cachedStatus()`, `createInvite() -> PartnerInvite`, `accept(code:) -> Partnership`, `unlink()`. `PartnerError`: `inviteInvalid` (`404 invite_invalid`), `alreadyLinked` (`409 partner_already_linked`), `validationFailed(String)`, `unauthorized`, `rateLimited`, `server(String)`. `unlink()` treats `404` as done. The existing `MealsError` use inside `status()` stays, to avoid churn in the three callers.
- `Repositories/PlanRepository`: new `storeTargets(from user:)` forwards to `PlanCache.setTargets`.
- `Repositories/IngredientsRepository`: new `customIngredients() -> [Ingredient]` (walks the pages, filters `isCustom`), `update(id:, IngredientUpdate) -> Ingredient`, `delete(id:)`. `IngredientError` gains `.inUse` (`409 ingredient_in_use`) and `.notFound`. Still uncached.
- `Repositories/NullSentinelMiddleware` (next to `OfflineMiddleware`): for operation ids `updateMe` and `updateIngredient`, decodes the JSON body, replaces any top-level number equal to `NullSentinel.value` (`-1`) with `null`, re-encodes. Other operations and other values pass through untouched. Composed in `RootView` with the existing middlewares.
- `Features/Profile/`: `ProfileDependencies`; pure `TargetsDraft`, `InviteCodeFormat`; `ProfileViewModel` (user, targets draft, save, deletion), `PartnerViewModel`, `MyIngredientsViewModel`; `ProfileView`, `TargetsSection`, `PartnerSection`, `DeleteAccountSheet`, `MyIngredientsView`.
- `Features/Shared/CustomIngredient/`: `CustomIngredientForm` gains an edit initialiser (prefilled from an existing `Ingredient`) and a pure `ingredientUpdate(form, existing) -> IngredientUpdate` that sends all 18 nutrients: the four the form shows from the form, the other 14 copied from `existing` (the API replaces the whole set, so omitting them would erase them). A blank weight per piece or density is the clear sentinel.
- `AppCore/ClearCaches`: also clears the profile cache.

Views never call the network or SwiftData; they talk to view models, which talk to repositories.

## 4. Screens and behaviour

**Profile** (`Form`): an Account section (email and display name, read-only), Daily targets, Partner, a "My ingredients" row that pushes a screen, then Sign Out and Delete account. An "Offline: showing your saved profile" line shows when the last refresh failed.

**Daily targets.** Four text fields (Calories, Protein g, Carbs g, Fat g), prefilled from the cached user; a blank field means "no target set". A Save button is enabled only when the draft differs from the saved values. Numbers go through the shared `parseDecimal` (a comma is a decimal point). Rules, as web: calories above 0 and at most 20000; protein at most 2000; carbs at most 5000; fat at most 2000; the macros from 0. Invalid input shows "Enter a number, for example 2000." or the limit message next to the field, with no request. Save sends all four every time (a blank one as the clear sentinel), stores the answer in `ProfileCache`, then calls `PlanRepository.storeTargets`. A `400` from the server shows inline if it names a field, else as a banner.

**Partner.** Loaded cache-first from `PartnerRepository`.
- *Not linked:* "Create invite code" shows the code grouped in fours (`InviteCodeFormat`: uppercase, `ABCD-EFGH`), with a `ShareLink` and a Copy button, and its expiry. "Enter a code" takes text, trims it and refuses blank; the server ignores case, spaces and dashes, so the text is sent as typed. `inviteInvalid` and `alreadyLinked` show inline; other errors as an alert (the accept route has its own tighter rate limit, so `429` has its own wording).
- *Pending:* "Waiting for your partner. The code expires …", Create a new code, Cancel invite (`unlink()`); the code itself is shown only if it was created in this screen's lifetime.
- *Linked:* "Linked with {name} since {date}" and Unlink behind a confirmation that says what stops (shared meals, templates and shopping lists; copies stay with whoever made them).
- After any change the cached status updates at once. The Meals, Plan and Shopping tabs already hide partner content when `status()` stops saying active, and an open shared list closes through the Shopping realtime rules.

**My ingredients.** Pushed from Profile. Loads every custom ingredient (live, with a progress indicator and a retry state; offline shows an error rather than a stale list, because the screen is rarely used and uncached), with a search field that filters locally. Rows show name, category and a Custom label. New opens the existing create form; Edit opens it prefilled (name, category, weight per piece, density, calories, protein, carbs, fat); Delete asks first. `ingredient_in_use` explains that a meal still uses it. A `400` shows inline on its field, as the create form does. Meals and the plan show an edited ingredient's new nutrition on their next refresh (their tabs refresh on appear), not instantly.

**Delete account.** A sheet that explains what is deleted (your meals, templates, plan and shopping lists; a partner keeps only the copies they made) and asks for the email, taken from the cached user, so it works when unverified. The button is enabled when the trimmed text equals the email, ignoring case. On `204` it calls `appState.signOut()`. Errors show inside the sheet and the account is untouched.

Accessibility: every control labelled; the invite code is announced as separate characters; validation errors are inline and tied to their field.

## 5. State and error rules

- Reads render the cache first. A failed refresh keeps what is shown and marks it stale. With nothing cached and no connection, Profile shows an error with retry (a first launch offline).
- Writes are never optimistic. After a good write the cache is updated from the server's answer.
- A `401` is handled by the existing `TokenRefresher`; a rejected refresh signs out, which clears this cache with the rest.
- Sign-out and account deletion clear `ProfileCache` through `clearAllCaches`, so a second user never sees the first user's email, targets or partner.
- The sentinel never leaves the repository: view models and views work with `Double?` and `nil`.

## 6. Testing

Swift Testing; network code against `RoutingTransport`; no real sleeps.
- `TargetsDraft`: every limit on both sides, blank clears, comma decimals, unchanged detection.
- `InviteCodeFormat`: grouping, lowercase input, short and long strings.
- `ingredientUpdate`: all 18 nutrients present, the 14 hidden ones copied from `existing`, a blank shown nutrient left out, blank weight/density become the sentinel.
- `NullSentinelMiddleware` through the real generated client and a recording transport: `-1` becomes `null` for `updateMe` and `updateIngredient`; the same value on another operation is untouched; non-sentinel numbers and non-numeric fields are untouched; a nested `-1` is untouched.
- `ProfileCache`: persists across a new actor on the same store, `unknown` vs `none` vs `linked`, cleared by `clearAll` and by `clearAllCaches`.
- `ProfileRepository`, `PartnerRepository`, `IngredientsRepository`: each response branch, including `invite_invalid`, `partner_already_linked`, `ingredient_in_use`, `404` on unlink, and a failed refresh leaving the cache untouched.
- View models: save then rings' targets updated (`PlanCache` row), invalid input sends no request, the invite code held and then gone with the view model, accept errors inline, unlink, delete requires the matching email and calls sign-out only on `204`, my-ingredients filters to custom and survives a page boundary.

## 7. XCUITest

One flow against the real API, accounts created through the API, sign-in through the UI:
1. Set Calories to 2000 on Profile and see the Today calorie ring show a target.
2. A second account creates an invite over the API; the first enters the code in the UI and shows "Linked with …".
3. Create a custom ingredient, edit its name, delete it.
4. Delete the account, typing the email, and land on the welcome screen.

Use `typeVerified`, `scrollUntilExists` and 45 s waits per `ios/CLAUDE.md`; sign out in a teardown block as the Shopping flow does. `make build-ios` does not compile the UI-test target: use `xcodebuild build-for-testing` before pushing.

## 8. Docs

`ios/CLAUDE.md` in the same change: layout entries for `ProfileCache`, `ProfileRepository`, the extended `PartnerRepository` and `IngredientsRepository`, `NullSentinelMiddleware`; gotchas for the clear sentinel (and that the generated optionals cannot send `null`), the two places targets live, the invite code shown once, `unlink` treating `404` as done, edits sending all 18 nutrients, and account deletion reusing `signOut()`.

## 9. Build order

1. Pure modules with tests: `TargetsDraft`, `InviteCodeFormat`, `NullSentinelMiddleware`, `ingredientUpdate`.
2. `ProfileCache`, the `PlanCache.setTargets` method, and the `CacheStore` schema.
3. `ProfileRepository`, then `PartnerRepository` and `IngredientsRepository` extensions.
4. View models, then views and sheets.
5. App wiring (`ProfileDependencies`, middleware composition, `ClearCaches`), then the XCUITest flow.
6. Docs.

## 10. Updates to the parent iOS spec (same change)

- §6: the cache list says "profile" is `ProfileCache`; writes there are online-only.
- §9 `Features/Profile`: points here for details; the "mine only" workaround stays; adds the clear sentinel.
- §15: plan 4 is complete once this ships.
