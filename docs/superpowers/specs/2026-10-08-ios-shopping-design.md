# iOS Shopping Design

Date: 2026-10-08
Status: draft for review. Extends `2026-09-30-ios-app-design.md` (§6, §7, §8, §9, §11, §15 plan 4, shopping half) and builds on `2026-10-02-ios-meals-design.md` and `2026-10-03-ios-plan-today-design.md`, whose repository/cache/view-model conventions it reuses unchanged. Where this document and the iOS spec disagree on Shopping, this document wins and the iOS spec is updated in the same change (§12). The shipped web feature (`web/src/features/shopping/`) is the parity reference; every "as web" below was read from that code or from `web/CLAUDE.md`. GitHub issue: #28 (Profile is the second half of that issue and gets its own spec and plan).

## 1. Goal

Replace the Shopping tab's stub with the real feature, on the existing API and with no contract change: browse your lists and your partner's shared lists; create an empty list or generate one from a plan date range (or regenerate an existing one); check off, quick-add, edit and remove items; see a partner's changes land live; and keep checking off, adding and removing items while offline, with the changes syncing when the connection returns.

Success: `swift test` passes for the pure modules (overlay, collapsing, SSE frame parsing, range validation, aisle grouping), the cache actor, the repository, the sync engine (deterministic ordered replay) and the view models; one XCUITest flow passes against the real API (§10); `make check` stays green and both `ios.yml` jobs pass.

Out of scope: Profile (own spec), Sign in with Apple (#29), background sync or push, offline list create/generate/delete, offline item field edits.

## 2. Decisions

| Topic | Decision |
|---|---|
| Offline model | **Overlay.** The cache stores the server's last snapshot of each list, unmodified. Pending changes live separately as `ShoppingIntent` rows. The displayed list is the pure function `PendingOverlay.apply(snapshot, intents)`. A refetch or SSE echo replaces the snapshot and can never erase a pending change; a drained intent's row is deleted and the next snapshot already contains its effect, so nothing is reconciled. |
| What is queued | `check`, `uncheck`, `add`, `remove` of items only. Everything else (create/generate/regenerate/rename/share/delete list, edit item fields) is online-only and surfaces a plain retry-able error on failure, as every non-shopping write (iOS spec §6). |
| Realtime | `URLSession.bytes(for:)` streaming `GET /shopping-lists/{id}/events`, bearer in `Authorization`. Events say what changed, never to what: refetch, don't patch. |
| Reconnect | A small stateless `NetworkMonitor` struct (new; Foundation has none) whose `updates()` returns a fresh stream over a fresh `NWPathMonitor` per call (so it restarts across sign-out and sign-in) triggers a queue drain and a refetch. Foreground does the same. |
| Item identity offline | An offline `add` gets a client-side id (`temp:<uuid>`, in the same `itemID` field as server ids) until the server assigns the real id. |
| Reads | `cached…()` then `refresh…()`, as Meals and Plan. |
| Contract | Unchanged. |

## 3. Modules

All in `ios/MealPlannerKit/Sources/`.

- `Repositories/ShoppingListsRepository`: the only code that touches both the generated client and SwiftData for shopping. Reads: `cachedLists(scope:)` / `refreshLists(scope:cursor:)`, `cachedList(id:)` / `refreshList(id:)`. Online writes: `createList`, `generate`, `updateList`, `deleteList`, `editItem(version:)`. Offline-able item writes: `enqueue(_ intent:)`. One exhaustive `switch` over each generated response enum into `ShoppingError`; every repository method goes through `unwrapping` (CLAUDE.md gotcha). The sync engine does not (a Swift 6 actor-self capture problem; every error maps to retry-later anyway).
- `Persistence/ShoppingCache`: a `@ModelActor` taking and returning generated value types. One row per list (generated `ShoppingList` JSON) plus a list-summary page per scope (Mine / Partner's, reusing `MealScope`) and the persisted queue (`CachedIntent` rows).
- `Persistence/ShoppingIntent` (value type) and `CachedIntent` (SwiftData, internal; the spec's earlier name was `QueuedIntent`): `id`, `sequence` (monotonic), `kind` (`check`/`uncheck`/`add`/`remove`), `listID`, a single `itemID` (a server id, or a `temp:`-prefixed id for a not-yet-synced add; replaces the earlier `itemId?` + `clientTempId?` pair), `addPayload?` (name, ingredientId, quantity, unit, category). Cleared on sign-out with the other caches (`ClearCaches`).
- `Repositories/Sync/PendingOverlay`: pure function; no I/O.
- `Persistence/IntentQueue`: pure enqueue-with-collapsing (§4.2), testable without SwiftData. It lives in `Persistence/` (not `Sync/`) because the cache collapses and writes atomically inside its actor, and `Persistence` cannot import `Repositories`.
- `Repositories/Sync/ShoppingSyncEngine`: actor; drains the queue (§4.3).
- `Repositories/Sync/ListEventStream` + `ListEvent` (`SSEFrameParser`, `ListEventReducer`): SSE client, frame parser and pure event rules (§5).
- `Repositories/Sync/NetworkMonitor`: stateless struct; `updates()` returns a fresh `AsyncStream<Bool>` over its own `NWPathMonitor` each call. `NetworkSwitch` is the `-uiTesting` offline switch.
- `Package.swift` gains two edges: `HTTPTypes` for `Repositories` (the offline middleware) and `Persistence` for `Features` (`ShoppingIntent.AddPayload`). No new external packages.
- `Features/Shopping/`: `ShoppingViewModel` (lists), `ShoppingListViewModel` (one list), `ItemGrouping` (pure aisle grouping and quantity text, as `items.ts`), `RangeValidation` (pure, as `range.ts`: both dates, `to >= from`, at most 92 days inclusive), views, sheets.

Views never call the network or SwiftData; they talk to view models, which talk to the repository.

## 4. Offline queue

### 4.1 Overlay

`PendingOverlay.apply(snapshot, intents)` for one list, in `sequence` order:

- `check` / `uncheck`: set `checked` on the item; `checked_by` is the current user's id when checking, `nil` when unchecking. `version` is left alone (only the server bumps it).
- `add`: append an item built from the payload with the `temp:` `itemID`, `version = 1`, `origin = manual`, marked pending.
- `remove`: hide the item.

An item with a `temp:` id (not yet on the server) can be removed but not checked or edited from the UI beyond the folding in §4.2; its row shows the syncing mark. As web's `isOptimistic`.

### 4.2 Collapsing (at enqueue time, in `IntentQueue`)

- `check` / `uncheck` replace any earlier pending check intent for the same item: one row per item holding the latest desired state (last write wins; the API's checked-only `PATCH` is unversioned, so this never conflicts server-side). A check state equal to the snapshot's state is still queued: the snapshot may be stale.
- `add` then `remove` of the same temp id before either syncs: both rows are dropped; no network call.
- `check` / `uncheck` of an item that is itself a pending `add`: cannot be folded into the add, because `CreateShoppingItemRequest` has no `checked` field. The check intent keeps its own row, addressed by the `temp:` id, and the engine rewrites it to the server id once the add succeeds (§4.3). The user sees no difference.
- `remove` of an already-synced item: one row, sent once; it also drops any pending check intent for that item.

### 4.3 Draining

`ShoppingSyncEngine` drains in `sequence` order, strictly one request in flight overall (a retryable failure blocks only that list's remaining rows; other lists keep draining), when the queue is non-empty and the app is online: on launch, on foreground, on reconnect, and after each successful online list mutation.

| Intent | Request | On success | Failure |
|---|---|---|---|
| `check` / `uncheck` | `PATCH /shopping-lists/{id}/items/{item_id}` `{checked}` (no version) | delete row | below |
| `add` | `POST /shopping-lists/{id}/items` | delete row; rewrite later rows that reference its temp id to the returned id | below |
| `remove` | `DELETE /shopping-lists/{id}/items/{item_id}` | delete row | `404` counts as success |

Failure handling, for every intent:

- Network failure, `429` or `5xx`: stop draining that list, keep the row, retry on the next trigger.
- `404` (item or list gone, or access lost): drop the row and remove the item from the snapshot. A gone list is cleaned by the next `refreshList`, which answers `404` and removes the list with its remaining rows (no extra request to tell item-gone from list-gone).
- `400` / `409` on a queued `add`/`check`: not expected (neither is versioned, the client validates first). Drop the row and surface a one-line notice ("A change couldn't be saved"); never retry in a loop.
- `401`: the existing `TokenRefresher` handles it; a signed-out session stops the engine and the caches are cleared.

After a drain step, the engine tells the view model to re-read the cache; the next `refresh…` brings the snapshot up to date.

### 4.4 Indicator

The list screen and the tab show a syncing badge whenever any intent is pending for that list. Rows render from the overlay regardless of queue state, so a checked item looks checked at once, online or offline.

## 5. Realtime

- `ListEventStream` opens `GET /shopping-lists/{id}/events` with `URLSession.bytes(for:)`, parses `text/event-stream` frames (`event:`/`data:` lines, blank-line terminated; comment lines such as `: keep-alive` ignored), decodes each `data:` as `{type, list_id, item_id?, version?}`, and republishes valid events as an `AsyncStream<ListEvent>`. A frame that is not valid JSON or has an unknown `type` is dropped (as web's `parseListEvent`, so a malformed event can never act).
- `401` on open goes through `TokenRefresher`. `404` means access lost. Other failures: keep the cache, mark stale, retry.
- Owned by the repository for whichever list is open: started when the list screen appears, cancelled when it disappears or the app leaves the foreground.
- Rules (as web's `useListEvents`):
  - An `item_changed` / `item_deleted` event whose `version` is at or below the cached item's version is ignored (often the user's own change). Events for the same item can arrive out of order.
  - An `item_deleted` removes the item from the snapshot.
  - A version gap, `list_changed`, a closed stream, a reconnect and returning to the foreground refetch the list.
  - `list_deleted` or a `404` means access lost: the screen is replaced by "This list is no longer available" even while items are cached.
  - Other load failures keep showing the cache, marked stale.
- A closed stream triggers one refetch, then a reconnect with a capped backoff. The server closes the stream after `list_deleted`; that is not retried.
- An event never patches an item's content. The refetch replaces the snapshot and the overlay re-applies any pending intents.

## 6. Screens

- **Shopping tab** (`ShoppingView`): Mine and Partner's segments (Partner's only while partnered; `partner_not_linked` or `404` hides it rather than showing an error). Rows show name, "From your plan · {range}" or "Your own list", and a Shared / Shared by your partner badge. Load more on `next_cursor`. Toolbar: New list, Generate from plan.
- **Generate** (sheet): from/to dates (default: the next seven days from `LocalDay`), optional name, validated by `RangeValidation`. A `404` (nothing planned in the range) is shown inline. Regenerating an existing list sends `list_id` from the list menu.
- **New list** (sheet): name, share with partner toggle.
- **List screen** (`ShoppingListView`): items grouped by aisle category in the web's order, with checked items kept in place and a "n of m" progress line; a quick-add field (name only, category defaults to `other`, as web); the syncing and stale indicators; swipe-free actions (a visible menu and context menu, per the Plan gotcha).
- **List menu**: rename, share with partner, delete (confirm), regenerate (only when the list has a source range). Owner-only (`is_owner`); a partner viewing the list gets item actions only.
- **Item edit sheet**: name, quantity, unit, category. Sent against the version the sheet opened on. `409 version_conflict`: take `current` from the problem body (`ApiError`-equivalent), refetch, and reopen the sheet on the current item; never overwrite silently. Check-off needs no version. Edit requires connectivity (§2).
- Numbers go through the shared `parseDecimal`. Quantity text follows `items.ts`. Edits cannot clear a quantity or unit: the generated `UpdateShoppingItemRequest` fields are optionals that cannot encode an explicit `null` (as `notes`), so a blank quantity or unit leaves the value unchanged.
- Accessibility: every control labelled; checkbox rows announce state; the syncing badge is read as "Syncing changes".

## 7. State and error rules

- `ShoppingViewModel` counts in-flight loads for `isLoading` and resets paging on a scope change; `ShoppingListViewModel.reload()` is latest-wins (generation guard) and ignores the cache once access is lost.

- Reads: `cached…()` first, then `refresh…()`. No cache and a failed fetch: empty/error state with retry. Cache and a failed fetch: keep the cache, marked stale.
- Online writes: write then refresh, as `PlanViewModel`; a failed refresh after a good write is reported as exactly that and the write is never repeated. One list-level write at a time.
- Item check-off is never blocked by another check in flight; with two quick taps an earlier answer never overrides the later tap (as web's `useCheckItem`), which falls out of one-row-per-item collapsing.
- `409 version_conflict` only occurs on item edit (§6). `404` on a list fetch replaces the list (§5).
- Sign-out clears the queue and the shopping cache.

## 8. Testing

Swift Testing; network-touching code against `RoutingTransport` (sees method, path, query and body; `Gate` for deterministic ordering). No real sleeps: time goes through an injected `sleep` and `TestSleeper`.

- **`PendingOverlay`**: check/uncheck, add, remove, combinations, order.
- **`IntentQueue`**: every collapsing rule in §4.2 (check replaces check; add-then-remove cancels; check on pending add keeps its own row; remove drops pending checks).
- **`ShoppingSyncEngine`**: ordered replay; one in flight; temp-id rewrite after an add; `404` drops the row and the item; network failure keeps the row and stops; `400`/`409` drops with a notice; stops on sign-out.
- **SSE**: frame parsing (multi-line, comments, malformed, unknown type); stale-event drop; version-gap refetch; `item_deleted` removal; `list_deleted` and `404` mean access lost; reconnect refetches.
- **Repository and cache**: each response enum branch; `cached` then `refresh`; scope separation; queue survives re-creating the cache from disk; cleared by `ClearCaches`.
- **View models**: optimistic display through the overlay; syncing indicator; `409` reopens on `current`; generate range validation; partner-not-linked hides Partner's.
- `ItemGrouping`, `RangeValidation`: pure unit tests (`null` quantity renders as no quantity, never 0).

## 9. Docs and accessibility

- Update `ios/CLAUDE.md` in the same change as the code: layout entries for `Sync/`, the shopping repository and cache, and gotchas for the overlay, collapsing, drain outcomes, SSE rules and the temp-id rewrite.
- The `apple:accessibility` audit stays part of the release work (#30), not this plan.

## 10. XCUITest

One flow against the real API, accounts created through the API (`createAccountViaAPI`), partnership linked through the API, sign-in through the UI. The partner link and the sharing of the generated list are done through the API, not the UI; generating, the live update, the offline check-off and the sync are driven through the UI. The offline switch is the `-uiTesting`-only `NetworkSwitch` (created by `RootView`) plus the list toolbar button. The test retries the tab tap and the offline toggle itself because taps were dropped locally:

1. User A signs in, generates a list from a seeded plan, and shares it.
2. A second session (API calls as user B) checks an item; the change appears live on A's screen without a manual refresh.
3. With the network cut (debug kill switch, as the iOS spec §12), A checks another item; it looks checked and the syncing badge shows.
4. The network returns; the badge clears and B's API read shows the item checked.

Use `scrollUntilExists`, `typeVerified` and generous waits (45 s) per CLAUDE.md. `make build-ios` does not compile the UI-test target: use `xcodebuild build-for-testing` before pushing UI-test changes.

## 11. Build order

1. Pure modules with tests: `PendingOverlay`, `IntentQueue`, `ListEvent` parser, `RangeValidation`, `ItemGrouping`.
2. `ShoppingCache` and `CachedIntent`; `ClearCaches`.
3. `ShoppingListsRepository` reads and online writes.
4. `NetworkMonitor` and `ShoppingSyncEngine`.
5. `ListEventStream`.
6. View models, then views and sheets.
7. XCUITest flow.
8. Docs (`ios/CLAUDE.md`, iOS spec updates).

## 12. Updates to the parent iOS spec (same change)

- §7: reconnect detection via `NWPathMonitor`; a pending `check` for a not-yet-synced `add` keeps its own row and is rewritten to the server id after the add; drain outcomes (`404` drops, `400`/`409` drops with a notice, network/5xx stops and retries).
- §9 `Features/Shopping`: points here for details.
- §15: plan 4 is split. Shopping (this document) ships first; Profile follows with its own spec and plan.
