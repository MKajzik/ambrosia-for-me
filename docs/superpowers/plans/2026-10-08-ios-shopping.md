# iOS Shopping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the Shopping tab stub with the real feature: browse your lists and your partner's shared lists, create or generate a list, check off / quick-add / edit / remove items, see a partner's changes live, and keep checking off, adding and removing items while offline with the changes syncing on reconnect.

**Architecture:** The cache stores only the server's last snapshot of each list; offline changes are `ShoppingIntent` rows kept separately and collapsed at enqueue time (`IntentQueue`). What the screen shows is the pure function `PendingOverlay.apply(snapshot, intents)`, so a refetch or SSE echo can never erase a pending change. A `ShoppingSyncEngine` actor drains the queue in order; `ListEventStream` reads `GET /shopping-lists/{id}/events` with `URLSession.bytes` and a pure reducer decides refetch / ignore / remove / access-lost. View models follow the Meals/Plan conventions (`cached…()` then `refresh…()`).

**Tech Stack:** Swift 6.2 (Swift 6 mode), SwiftUI, SwiftData (`@ModelActor`), Observation, Network (`NWPathMonitor`), Swift Testing, XCTest/XCUITest, the existing Swift OpenAPI client. No new package dependencies; no change to `openapi.yaml`.

**Spec:** `docs/superpowers/specs/2026-10-08-ios-shopping-design.md` (this plan implements all of it). Read also `docs/superpowers/specs/2026-09-30-ios-app-design.md` §6–§8, `ios/CLAUDE.md`, and the web parity code: `web/src/features/shopping/` (`list-cache.ts`, `list-events.ts`, `use-list-events.ts`, `items.ts`, `range.ts`) and `web/CLAUDE.md` §Gotchas. GitHub issue: #28.

## Global Constraints

- iOS 26+ only; the package also declares macOS 15 so `swift test` and `swift build` run natively. Any UIKit-only SwiftUI modifier or API sits behind `#if os(iOS)` (use the helpers in `Features/Shared/ViewHelpers.swift`).
- Swift 6 language mode, strict concurrency. View models are `@Observable @MainActor`. In `@MainActor` test suites, a `static` used from a `@Sendable` route closure must be `nonisolated`, and any nested helper struct must be `@MainActor`.
- The generated `Components.Schemas.*` types are the domain types. Generated ids are `String`; dates (`format: date`) are `String`. Never hand-edit `Sources/API/GeneratedSources`.
- Only `Sources/API`, `Sources/Auth` and `Sources/Repositories` may `import OpenAPIRuntime` / `HTTPTypes`. Views and view models never touch SwiftData or an HTTP status. `@Model` objects never leave `Persistence`.
- Every repository/engine call to the generated client goes through `unwrapping` (the client wraps transport failures in `ClientError`).
- Only item `check`, `uncheck`, `add`, `remove` are queued offline. Create/generate/regenerate/rename/share/delete list and item field edits are online-only.
- Check-off is unversioned (last write wins). Item edits are sent against the version the sheet opened on; `409 version_conflict` carries `current` and is never overwritten silently.
- An SSE event says what changed, never to what: refetch, don't patch (except dropping a stale event, and removing a deleted item).
- No real sleeps in tests: time goes through an injected `sleep` and `TestSleeper`.
- Dates are the device's local calendar day as `YYYY-MM-DD` (`LocalDay`); text is fixed en-US. A `null` quantity renders as nothing, never 0.
- Tests first. Small commits, one logical change each. Commit trailer: `Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>`.
- `swift test` runs from `ios/MealPlannerKit` (filter with `--filter <Suite>`). Compiling the UI-test target needs `xcodebuild build-for-testing` (`make build-ios` does not compile it). Local XCUITest is unreliable under load: GitHub CI is the signal for the `ui` job.

## Decisions this plan adds to the spec

These come from reading the generated client and the existing code while planning. Challenge them at review.

1. **One `itemID` field on `ShoppingIntent`.** The spec has `itemId?` plus `clientTempId?`. An unsynced add uses a `temp:<uuid>` id in the same field (`ShoppingIntent.isTemp`). Behaviour is identical; one field removes a whole class of "which one is set" bugs.
2. **`IntentQueue` lives in `Persistence`, not `Sync/`.** The cache must collapse and write atomically inside its actor, and `Persistence` cannot import `Repositories`. `PendingOverlay`, the engine and the stream live in `Repositories/Sync/`.
3. **The engine drains strictly one request at a time overall** (spec: one in flight per list). A retryable failure blocks only that list's remaining rows; other lists keep draining.
4. **A `404` on a queued intent drops that row and removes the item from the snapshot.** If the whole list is gone, the next refresh answers `404` and `ShoppingListsRepository.refreshList` calls `ShoppingCache.removeList`, which also deletes that list's remaining intents. Same outcome as the spec, without an extra request to tell "item gone" from "list gone".
5. **Lists paginate as the spec says**: the first page replaces a scope in the cache, "Load more" merges the next page.
6. **Item edits cannot clear a quantity or unit.** The generated `UpdateShoppingItemRequest` fields are `Optional` and cannot encode an explicit `null` (the same limit as `notes` in `ios/CLAUDE.md`). A blank quantity leaves it unchanged.
7. **The XCUITest links the partner and shares the generated list through the API**, not the UI (the spec's step 1 "shares it"). Generating, the live update, the offline check-off and the sync are driven through the UI. The offline switch is a `-uiTesting`-only `NetworkSwitch` plus a toolbar button (the spec's "debug kill switch").
8. **`checkedBy` in the overlay is the signed-in user's id when known** (`AppState.session == .signedIn(user)`), else `nil`. Nothing in the UI shows who checked an item yet.
9. **Package.swift gains two edges**: `HTTPTypes` for `Repositories` (the offline middleware) and `Persistence` for `Features` (`ShoppingIntent.AddPayload`). No new external packages.

## Review Focus

Inputs and conditions the spec implies but a happy-path test would not exercise, most likely first. Each has a test in the task that owns the code.

1. **Two quick taps on one item while offline** must leave one queue row holding the last state, and the screen must show that state. (Task 1 `IntentQueueTests`, Task 11 view model.)
2. **The app is killed with intents queued**: a new cache actor on the same store must still hold them, in order. (Task 5.)
3. **A partner deletes an item while it has a pending check**: the drain gets a `404`, drops the row, the item disappears, nothing crashes or loops. (Task 7, Task 11.)
4. **The partner deletes the list while it is open**: `list_deleted`, or a `404` on refetch, must replace the screen with "no longer available" even though the list is cached, and the list's pending intents must be dropped. (Task 6, Task 11.)
5. **Hostile SSE bytes**: CRLF line endings, `: keep-alive` comments, a frame split across several `data:` lines, malformed JSON, an unknown `type`, a final line with no newline. None may crash or act. (Task 4, Task 8.)

Also covered where they arise: a 93-day generate range and a comma decimal (`1,5`) in the edit sheet; sign-out clears the queue so a second user never drains the first user's intents (Task 9).

---

## File Structure

```
ios/MealPlannerKit/Sources/
├── Persistence/
│   ├── ShoppingIntent.swift            (new) value types + IntentQueue (pure)
│   ├── CachedShoppingModels.swift      (new) @Model rows: summary, list, intent
│   ├── ShoppingCache.swift             (new) @ModelActor
│   └── CacheStore.swift                (modify) schema + makeShoppingCache
├── Repositories/
│   ├── ShoppingError.swift             (new)
│   ├── ShoppingListsRepository.swift   (new)
│   └── Sync/
│       ├── PendingOverlay.swift        (new) pure
│       ├── ListEvent.swift             (new) ListEvent, SSEFrameParser, ListEventReducer (pure)
│       ├── ListEventStream.swift       (new) URLSession SSE client
│       ├── ShoppingSyncEngine.swift    (new) queue drain actor
│       ├── NetworkSwitch.swift         (new) UI-test offline switch + middleware
│       └── NetworkMonitor.swift        (new) NWPathMonitor wrapper
├── Features/
│   ├── Shared/ErrorText.swift          (modify) ShoppingError text
│   └── Shopping/
│       ├── ItemGrouping.swift          (new) pure
│       ├── RangeValidation.swift       (new) pure
│       ├── ShoppingDependencies.swift  (new)
│       ├── ShoppingViewModel.swift     (new) lists
│       ├── ShoppingListViewModel.swift (new) one list
│       ├── ShoppingView.swift          (replace stub)
│       ├── ShoppingListView.swift      (new)
│       ├── ShoppingItemRow.swift       (new)
│       ├── QuickAddField.swift         (new)
│       ├── EditItemSheet.swift         (new)
│       ├── GenerateListSheet.swift     (new)
│       └── NewListSheet.swift          (new)
└── AppCore/ ClearCaches.swift, RootView.swift, TabShellView.swift   (modify)
ios/MealPlannerKit/Package.swift                                      (modify)
ios/MealPlannerUITests/ShoppingFlowUITests.swift                      (new)
ios/CLAUDE.md, docs/superpowers/specs/2026-10-08-ios-shopping-design.md (modify, Task 14)
Tests/MealPlannerKitTests/: IntentQueueTests, ShoppingFixtures, PendingOverlayTests, ItemGroupingTests,
  RangeValidationTests, ListEventTests, ShoppingCacheTests, ShoppingRepositoryTests, ShoppingServer,
  ShoppingSyncEngineTests, ListEventStreamTests, ShoppingViewModelTests, ShoppingListViewModelTests
```

---

### Task 1: `ShoppingIntent` and `IntentQueue` (pure collapsing)

**Files:**
- Create: `ios/MealPlannerKit/Sources/Persistence/ShoppingIntent.swift`
- Test: `ios/MealPlannerKit/Tests/MealPlannerKitTests/IntentQueueTests.swift`

**Interfaces:**
- Produces: `ShoppingIntent` (`id: UUID`, `sequence: Int`, `kind: Kind`, `listID: String`, `itemID: String`, `payload: AddPayload?`, `isTemp: Bool`, `static tempPrefix`, `static newTempID() -> String`), `ShoppingIntent.Kind` (`check, uncheck, add, remove`), `ShoppingIntent.AddPayload` (`name`, `ingredientID?`, `quantity?`, `unit: Components.Schemas.Unit?`, `category: Components.Schemas.IngredientCategory?`), `IntentQueue.enqueue(_ intent: ShoppingIntent, into queue: [ShoppingIntent]) -> [ShoppingIntent]`.

- [ ] **Step 1: Write the failing tests**

```swift
import Testing
@testable import Persistence

@Suite
struct IntentQueueTests {
    private func intent(_ kind: ShoppingIntent.Kind, _ item: String, seq: Int, list: String = "l1") -> ShoppingIntent {
        ShoppingIntent(sequence: seq, kind: kind, listID: list, itemID: item, payload: kind == .add ? .init(name: "Milk") : nil)
    }

    private func kinds(_ queue: [ShoppingIntent]) -> [String] { queue.map { "\($0.kind.rawValue):\($0.itemID)" } }

    @Test("A check replaces an earlier pending check of the same item: last write wins")
    func checkReplacesCheck() {
        var queue: [ShoppingIntent] = []
        queue = IntentQueue.enqueue(intent(.check, "i1", seq: 1), into: queue)
        queue = IntentQueue.enqueue(intent(.check, "i2", seq: 2), into: queue)
        queue = IntentQueue.enqueue(intent(.uncheck, "i1", seq: 3), into: queue)
        // Review focus 1: two quick taps on i1 leave one row holding the last state.
        #expect(kinds(queue) == ["check:i2", "uncheck:i1"])
    }

    @Test("Checking, unchecking and checking again leaves one check")
    func tripleToggle() {
        var queue: [ShoppingIntent] = []
        for (n, kind) in [ShoppingIntent.Kind.check, .uncheck, .check].enumerated() {
            queue = IntentQueue.enqueue(intent(kind, "i1", seq: n + 1), into: queue)
        }
        #expect(kinds(queue) == ["check:i1"])
    }

    @Test("An add followed by a remove of the same temp item cancels both, and any check in between")
    func addThenRemoveCancels() {
        let temp = ShoppingIntent.newTempID()
        var queue: [ShoppingIntent] = []
        queue = IntentQueue.enqueue(intent(.add, temp, seq: 1), into: queue)
        queue = IntentQueue.enqueue(intent(.check, temp, seq: 2), into: queue)
        queue = IntentQueue.enqueue(intent(.check, "i9", seq: 3), into: queue)
        queue = IntentQueue.enqueue(intent(.remove, temp, seq: 4), into: queue)
        #expect(kinds(queue) == ["check:i9"])
    }

    @Test("A check on a pending add keeps its own row, after the add")
    func checkOnPendingAdd() {
        let temp = ShoppingIntent.newTempID()
        var queue: [ShoppingIntent] = []
        queue = IntentQueue.enqueue(intent(.add, temp, seq: 1), into: queue)
        queue = IntentQueue.enqueue(intent(.check, temp, seq: 2), into: queue)
        #expect(queue.map(\.kind) == [.add, .check])
        #expect(queue.allSatisfy { $0.isTemp })
    }

    @Test("Removing a synced item drops its pending checks and queues one remove")
    func removeSynced() {
        var queue: [ShoppingIntent] = []
        queue = IntentQueue.enqueue(intent(.check, "i1", seq: 1), into: queue)
        queue = IntentQueue.enqueue(intent(.remove, "i1", seq: 2), into: queue)
        queue = IntentQueue.enqueue(intent(.remove, "i1", seq: 3), into: queue)
        #expect(kinds(queue) == ["remove:i1"])
        #expect(queue.first?.sequence == 2)
    }

    @Test("A check of an item with a pending remove is ignored")
    func checkAfterRemove() {
        var queue: [ShoppingIntent] = []
        queue = IntentQueue.enqueue(intent(.remove, "i1", seq: 1), into: queue)
        queue = IntentQueue.enqueue(intent(.check, "i1", seq: 2), into: queue)
        #expect(kinds(queue) == ["remove:i1"])
    }

    @Test("Adds keep their order and different lists never interfere")
    func addsAndLists() {
        var queue: [ShoppingIntent] = []
        queue = IntentQueue.enqueue(intent(.add, "temp:a", seq: 1), into: queue)
        queue = IntentQueue.enqueue(intent(.add, "temp:b", seq: 2, list: "l2"), into: queue)
        queue = IntentQueue.enqueue(intent(.add, "temp:c", seq: 3), into: queue)
        #expect(kinds(queue) == ["add:temp:a", "add:temp:b", "add:temp:c"])
    }

    @Test("newTempID is recognised as temp and is unique")
    func tempIDs() {
        let a = ShoppingIntent.newTempID()
        let b = ShoppingIntent.newTempID()
        #expect(a != b)
        #expect(ShoppingIntent(sequence: 1, kind: .check, listID: "l", itemID: a).isTemp)
        #expect(!ShoppingIntent(sequence: 1, kind: .check, listID: "l", itemID: "5f1c").isTemp)
    }
}
```

- [ ] **Step 2: Run to verify it fails**

Run (from `ios/MealPlannerKit`): `swift test --filter IntentQueueTests`
Expected: FAIL to compile, "cannot find 'ShoppingIntent' in scope".

- [ ] **Step 3: Implement `ShoppingIntent.swift`**

```swift
import API
import Foundation

/// One pending offline change to a shopping item. Only item-level changes are queued; everything else needs a connection.
public struct ShoppingIntent: Equatable, Sendable, Identifiable {
    public enum Kind: String, Sendable {
        case check, uncheck, add, remove
    }

    /// What an offline `add` will send once there is a connection.
    public struct AddPayload: Equatable, Sendable, Codable {
        public var name: String
        public var ingredientID: String?
        public var quantity: Double?
        public var unit: Components.Schemas.Unit?
        public var category: Components.Schemas.IngredientCategory?

        public init(
            name: String, ingredientID: String? = nil, quantity: Double? = nil,
            unit: Components.Schemas.Unit? = nil, category: Components.Schemas.IngredientCategory? = nil
        ) {
            self.name = name
            self.ingredientID = ingredientID
            self.quantity = quantity
            self.unit = unit
            self.category = category
        }
    }

    public static let tempPrefix = "temp:"
    public static func newTempID() -> String { tempPrefix + UUID().uuidString }

    public var id: UUID
    /// Monotonic across all lists; the queue drains in this order.
    public var sequence: Int
    public var kind: Kind
    public var listID: String
    /// The server's item id, or `temp:<uuid>` for an item that exists only on this device (an unsynced add).
    public var itemID: String
    public var payload: AddPayload?
    public var isTemp: Bool { itemID.hasPrefix(Self.tempPrefix) }

    public init(id: UUID = UUID(), sequence: Int, kind: Kind, listID: String, itemID: String, payload: AddPayload? = nil) {
        self.id = id
        self.sequence = sequence
        self.kind = kind
        self.listID = listID
        self.itemID = itemID
        self.payload = payload
    }
}

/// The collapsing rules, pure. The queue passed in may hold every list's intents: item ids are globally unique.
public enum IntentQueue {
    public static func enqueue(_ intent: ShoppingIntent, into queue: [ShoppingIntent]) -> [ShoppingIntent] {
        var result = queue
        let sameItem = { (other: ShoppingIntent) in other.itemID == intent.itemID }
        let isCheck = { (other: ShoppingIntent) in other.kind == .check || other.kind == .uncheck }
        switch intent.kind {
        case .check, .uncheck:
            // An item that is about to be removed cannot be checked.
            if result.contains(where: { sameItem($0) && $0.kind == .remove }) { return queue }
            // One row per item holding the latest desired state (the API's checked-only PATCH is last-write-wins).
            result.removeAll { sameItem($0) && isCheck($0) }
            result.append(intent)
        case .remove:
            if intent.isTemp {
                // The server never heard of it: forget everything about it, send nothing.
                result.removeAll(where: sameItem)
                return result
            }
            result.removeAll { sameItem($0) && isCheck($0) }
            if !result.contains(where: { sameItem($0) && $0.kind == .remove }) { result.append(intent) }
        case .add:
            result.append(intent)
        }
        return result
    }
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `swift test --filter IntentQueueTests`
Expected: PASS (7 tests).

- [ ] **Step 5: Commit**

```bash
git add ios/MealPlannerKit/Sources/Persistence/ShoppingIntent.swift ios/MealPlannerKit/Tests/MealPlannerKitTests/IntentQueueTests.swift
git commit -m "feat(ios): add ShoppingIntent and the pure offline-queue collapsing rules

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 2: `PendingOverlay` and shopping test fixtures

**Files:**
- Create: `ios/MealPlannerKit/Sources/Repositories/Sync/PendingOverlay.swift`
- Create: `ios/MealPlannerKit/Tests/MealPlannerKitTests/ShoppingFixtures.swift`
- Test: `ios/MealPlannerKit/Tests/MealPlannerKitTests/PendingOverlayTests.swift`

**Interfaces:**
- Consumes: `ShoppingIntent` (Task 1).
- Produces: `PendingOverlay.apply(_ list: Components.Schemas.ShoppingList, intents: [ShoppingIntent], userID: String?, now: Date = Date()) -> Components.Schemas.ShoppingList`. Test helpers `ShoppingFixtures.item(...)`, `.list(...)`, `.summary(_:)`, `.summary(id:name:shared:)`, `.page(_:next:)`, `.conflict(current:)`.

- [ ] **Step 1: Write `ShoppingFixtures.swift` (test support, used by every later task)**

```swift
import API
import Foundation

enum ShoppingFixtures {
    static func item(
        id: String = "i1", listID: String = "l1", name: String = "Milk",
        category: Components.Schemas.IngredientCategory = .dairyEggs, checked: Bool = false, checkedBy: String? = nil,
        version: Int = 1, position: Int = 0, quantity: Double? = nil,
        unit: Components.Schemas.ShoppingItem.UnitPayload? = nil
    ) -> Components.Schemas.ShoppingItem {
        .init(
            id: id, listId: listID, name: name, quantity: quantity, unit: unit, category: category, checked: checked,
            checkedBy: checkedBy, position: position, version: version, origin: .manual,
            createdAt: Fixtures.date, updatedAt: Fixtures.date
        )
    }

    static func list(
        id: String = "l1", name: String = "Week", items: [Components.Schemas.ShoppingItem] = [],
        isOwner: Bool = true, shared: Bool = false, from: String? = nil, to: String? = nil
    ) -> Components.Schemas.ShoppingList {
        .init(
            id: id, name: name, sharedWithPartner: shared, isOwner: isOwner, sourceFrom: from, sourceTo: to,
            items: items, createdAt: Fixtures.date, updatedAt: Fixtures.date
        )
    }

    static func summary(_ list: Components.Schemas.ShoppingList) -> Components.Schemas.ShoppingListSummary {
        .init(
            id: list.id, name: list.name, sharedWithPartner: list.sharedWithPartner, sourceFrom: list.sourceFrom,
            sourceTo: list.sourceTo, createdAt: list.createdAt, updatedAt: list.updatedAt
        )
    }

    static func summary(id: String = "l1", name: String = "Week", shared: Bool = false) -> Components.Schemas.ShoppingListSummary {
        summary(list(id: id, name: name, shared: shared))
    }

    static func page(_ items: [Components.Schemas.ShoppingListSummary], next: String? = nil) -> String {
        Fixtures.json(Components.Schemas.ShoppingListPage(items: items, nextCursor: next))
    }

    /// A `409 version_conflict` problem body carrying the item's current state.
    static func conflict(current: Components.Schemas.ShoppingItem) -> String {
        Fixtures.json(Components.Schemas.ShoppingItemConflict(
            _type: "about:blank", title: "Conflict", status: 409, code: "version_conflict", current: current
        ))
    }
}
```

- [ ] **Step 2: Write the failing tests** — `PendingOverlayTests.swift`

```swift
import API
import Foundation
import Persistence
import Testing
@testable import Repositories

@Suite
struct PendingOverlayTests {
    private func intent(_ kind: ShoppingIntent.Kind, _ item: String, seq: Int, list: String = "l1", payload: ShoppingIntent.AddPayload? = nil) -> ShoppingIntent {
        ShoppingIntent(sequence: seq, kind: kind, listID: list, itemID: item, payload: payload)
    }

    private let base = ShoppingFixtures.list(items: [
        ShoppingFixtures.item(id: "i1", name: "Milk", position: 0),
        ShoppingFixtures.item(id: "i2", name: "Eggs", checked: true, checkedBy: "u9", position: 1),
    ])

    @Test("A check flips the item and records who; version is left to the server")
    func check() {
        let shown = PendingOverlay.apply(base, intents: [intent(.check, "i1", seq: 1)], userID: "u1")
        let milk = shown.items[0]
        #expect(milk.checked)
        #expect(milk.checkedBy == "u1")
        #expect(milk.version == 1)
    }

    @Test("An uncheck clears the checker")
    func uncheck() {
        let shown = PendingOverlay.apply(base, intents: [intent(.uncheck, "i2", seq: 1)], userID: "u1")
        #expect(shown.items[1].checked == false)
        #expect(shown.items[1].checkedBy == nil)
    }

    @Test("A remove hides the item")
    func remove() {
        let shown = PendingOverlay.apply(base, intents: [intent(.remove, "i1", seq: 1)], userID: nil)
        #expect(shown.items.map(\.id) == ["i2"])
    }

    @Test("An add appears as a temp item at the end, version 1, category defaulting to other")
    func add() throws {
        let temp = ShoppingIntent.newTempID()
        let add = intent(.add, temp, seq: 1, payload: .init(name: "Bread", quantity: 2, unit: .piece))
        let shown = PendingOverlay.apply(base, intents: [add], userID: nil)
        let bread = try #require(shown.items.last)
        #expect(bread.id == temp)
        #expect(bread.name == "Bread")
        #expect(bread.category == .other)
        #expect(bread.unit == .piece)
        #expect(bread.version == 1)
        #expect(bread.position == 2)
        #expect(bread.origin == .manual)
    }

    @Test("A check on a pending add applies to the temp item")
    func checkOnTemp() {
        let temp = ShoppingIntent.newTempID()
        let shown = PendingOverlay.apply(
            base,
            intents: [intent(.add, temp, seq: 1, payload: .init(name: "Bread")), intent(.check, temp, seq: 2)],
            userID: "u1"
        )
        #expect(shown.items.last?.checked == true)
    }

    @Test("Intents apply in sequence order whatever order they are given in")
    func ordering() {
        let shown = PendingOverlay.apply(
            base, intents: [intent(.uncheck, "i1", seq: 3), intent(.check, "i1", seq: 2)], userID: "u1"
        )
        #expect(shown.items[0].checked == false)
    }

    @Test("Intents for another list are ignored, and the snapshot is never mutated")
    func otherList() {
        let shown = PendingOverlay.apply(base, intents: [intent(.remove, "i1", seq: 1, list: "other")], userID: nil)
        #expect(shown == base)
    }

    @Test("An add whose server item already exists is not shown twice")
    func addNotDuplicated() {
        let temp = ShoppingIntent.newTempID()
        let list = ShoppingFixtures.list(items: [ShoppingFixtures.item(id: temp)])
        let shown = PendingOverlay.apply(list, intents: [intent(.add, temp, seq: 1, payload: .init(name: "Milk"))], userID: nil)
        #expect(shown.items.count == 1)
    }
}
```

- [ ] **Step 3: Run to verify it fails**

Run: `swift test --filter PendingOverlayTests`
Expected: FAIL to compile, "cannot find 'PendingOverlay' in scope".

- [ ] **Step 4: Implement `PendingOverlay.swift`**

```swift
import API
import Foundation
import Persistence

/// What the screen shows: the server's last snapshot of a list with this device's pending changes laid over it.
/// Pure. A refetch replaces the snapshot and can never erase a pending change, because pending changes are not in it.
public enum PendingOverlay {
    public static func apply(
        _ list: Components.Schemas.ShoppingList, intents: [ShoppingIntent], userID: String?, now: Date = Date()
    ) -> Components.Schemas.ShoppingList {
        var result = list
        for intent in intents.filter({ $0.listID == list.id }).sorted(by: { $0.sequence < $1.sequence }) {
            switch intent.kind {
            case .check, .uncheck:
                let checked = intent.kind == .check
                result.items = result.items.map { item in
                    guard item.id == intent.itemID else { return item }
                    var item = item
                    item.checked = checked
                    item.checkedBy = checked ? userID : nil
                    return item
                }
            case .remove:
                result.items.removeAll { $0.id == intent.itemID }
            case .add:
                guard let payload = intent.payload, !result.items.contains(where: { $0.id == intent.itemID }) else { continue }
                result.items.append(.init(
                    id: intent.itemID, listId: list.id, ingredientId: payload.ingredientID, name: payload.name,
                    quantity: payload.quantity,
                    unit: payload.unit.flatMap { Components.Schemas.ShoppingItem.UnitPayload(rawValue: $0.rawValue) },
                    category: payload.category ?? .other, checked: false, checkedBy: nil,
                    position: (result.items.map(\.position).max() ?? -1) + 1, version: 1, origin: .manual,
                    createdAt: now, updatedAt: now
                ))
            }
        }
        return result
    }
}
```

- [ ] **Step 5: Run to verify it passes, then commit**

Run: `swift test --filter PendingOverlayTests`
Expected: PASS (8 tests).

```bash
git add ios/MealPlannerKit/Sources/Repositories/Sync/PendingOverlay.swift ios/MealPlannerKit/Tests/MealPlannerKitTests/ShoppingFixtures.swift ios/MealPlannerKit/Tests/MealPlannerKitTests/PendingOverlayTests.swift
git commit -m "feat(ios): add PendingOverlay and shopping test fixtures

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 3: `ItemGrouping` and `RangeValidation` (pure feature helpers)

**Files:**
- Create: `ios/MealPlannerKit/Sources/Features/Shopping/ItemGrouping.swift`
- Create: `ios/MealPlannerKit/Sources/Features/Shopping/RangeValidation.swift`
- Test: `ios/MealPlannerKit/Tests/MealPlannerKitTests/ItemGroupingTests.swift`, `RangeValidationTests.swift`

**Interfaces:**
- Consumes: `LocalDay` (`date(_:)`, `addDays(_:_:)`), `Components.Schemas.IngredientCategory.allCases` (API order) and `.label`.
- Produces: `ItemGroup` (`category`, `items`, `id`), `ItemGrouping.groups(_:) -> [ItemGroup]`, `ItemGrouping.progress(_:) -> (done: Int, total: Int)`, `ItemGrouping.quantityText(_:) -> String`, `RangeValidation.maxDays`, `RangeValidation.error(from:to:day:) -> String?`.

- [ ] **Step 1: Write the failing tests**

`ItemGroupingTests.swift`:

```swift
import API
import Testing
@testable import Features

@Suite
struct ItemGroupingTests {
    @Test("Groups follow the API's category order and skip empty aisles; unchecked come first, then by position")
    func groups() {
        let items = [
            ShoppingFixtures.item(id: "a", name: "Soda", category: .beverages, position: 0),
            ShoppingFixtures.item(id: "b", name: "Apple", category: .produce, checked: true, position: 1),
            ShoppingFixtures.item(id: "c", name: "Leek", category: .produce, position: 3),
            ShoppingFixtures.item(id: "d", name: "Kale", category: .produce, position: 2),
        ]
        let groups = ItemGrouping.groups(items)
        #expect(groups.map(\.category) == [.produce, .beverages])
        #expect(groups[0].items.map(\.id) == ["d", "c", "b"])
    }

    @Test("Progress counts checked of total")
    func progress() {
        let items = [ShoppingFixtures.item(id: "a", checked: true), ShoppingFixtures.item(id: "b")]
        #expect(ItemGrouping.progress(items) == (done: 1, total: 2))
    }

    @Test("Quantity text: nothing for null, never 0; pieces pluralise; up to two decimals")
    func quantity() {
        #expect(ItemGrouping.quantityText(ShoppingFixtures.item()) == "")
        #expect(ItemGrouping.quantityText(ShoppingFixtures.item(quantity: 2, unit: .g)) == "2 g")
        #expect(ItemGrouping.quantityText(ShoppingFixtures.item(quantity: 1.5, unit: .ml)) == "1.5 ml")
        #expect(ItemGrouping.quantityText(ShoppingFixtures.item(quantity: 0.333, unit: .g)) == "0.33 g")
        #expect(ItemGrouping.quantityText(ShoppingFixtures.item(quantity: 1, unit: .piece)) == "1 piece")
        #expect(ItemGrouping.quantityText(ShoppingFixtures.item(quantity: 3, unit: .piece)) == "3 pieces")
        #expect(ItemGrouping.quantityText(ShoppingFixtures.item(quantity: 4)) == "4")
        #expect(ItemGrouping.quantityText(ShoppingFixtures.item(quantity: 1000, unit: .g)) == "1,000 g")
    }
}
```

`RangeValidationTests.swift`:

```swift
import Foundation
import Testing
@testable import Features

@Suite
struct RangeValidationTests {
    private let day = LocalDay(timeZone: TimeZone(identifier: "UTC")!)

    @Test("A one-day range and a 92-day range are valid")
    func valid() {
        #expect(RangeValidation.error(from: "2026-10-05", to: "2026-10-05", day: day) == nil)
        #expect(RangeValidation.error(from: "2026-01-01", to: "2026-04-02", day: day) == nil) // 92 days inclusive
    }

    @Test("93 days is refused, as is an end before the start or a malformed date")
    func invalid() {
        #expect(RangeValidation.error(from: "2026-01-01", to: "2026-04-03", day: day) == "Pick at most 92 days.")
        #expect(RangeValidation.error(from: "2026-10-06", to: "2026-10-05", day: day) == "The end date must be on or after the start date.")
        #expect(RangeValidation.error(from: "", to: "2026-10-05", day: day) == "Choose a start and end date.")
        #expect(RangeValidation.error(from: "2026-13-40", to: "2026-10-05", day: day) == "Choose a start and end date.")
    }
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `swift test --filter "ItemGroupingTests|RangeValidationTests"`
Expected: FAIL to compile, "cannot find 'ItemGrouping' in scope".

- [ ] **Step 3: Implement**

`ItemGrouping.swift`:

```swift
import API
import Foundation

public struct ItemGroup: Equatable, Identifiable, Sendable {
    public let category: Components.Schemas.IngredientCategory
    public let items: [Components.Schemas.ShoppingItem]
    public var id: String { category.rawValue }
}

/// Aisle grouping and item text, as web's `items.ts`.
public enum ItemGrouping {
    /// The items by aisle in the API's category order. Within an aisle what is still to buy comes first.
    public static func groups(_ items: [Components.Schemas.ShoppingItem]) -> [ItemGroup] {
        Components.Schemas.IngredientCategory.allCases.compactMap { category in
            let inAisle = items.filter { $0.category == category }
            guard !inAisle.isEmpty else { return nil }
            let sorted = inAisle.sorted {
                if $0.checked != $1.checked { return !$0.checked }
                return $0.position < $1.position
            }
            return ItemGroup(category: category, items: sorted)
        }
    }

    public static func progress(_ items: [Components.Schemas.ShoppingItem]) -> (done: Int, total: Int) {
        (items.filter(\.checked).count, items.count)
    }

    /// "" for no quantity (never 0), "2 g", "1.5 ml", "1 piece", "3 pieces", "4". Fixed en-US.
    public static func quantityText(_ item: Components.Schemas.ShoppingItem) -> String {
        guard let quantity = item.quantity else { return "" }
        let amount = quantity.formatted(.number.precision(.fractionLength(0...2)).locale(Locale(identifier: "en_US")))
        guard let unit = item.unit else { return amount }
        if unit == .piece { return "\(amount) \(quantity == 1 ? "piece" : "pieces")" }
        return "\(amount) \(unit.rawValue)"
    }
}
```

`RangeValidation.swift`:

```swift
import Foundation

/// The date range a list is generated from, as web's `range.ts`: at most 92 days, both ends inclusive.
public enum RangeValidation {
    public static let maxDays = 92

    /// The message to show, or `nil` when the range is acceptable.
    public static func error(from: String, to: String, day: LocalDay) -> String? {
        guard day.date(from) != nil, day.date(to) != nil else { return "Choose a start and end date." }
        // ISO dates sort as text, so a plain comparison is a date comparison.
        if to < from { return "The end date must be on or after the start date." }
        if to > day.addDays(from, maxDays - 1) { return "Pick at most \(maxDays) days." }
        return nil
    }
}
```

- [ ] **Step 4: Run to verify they pass, then commit**

Run: `swift test --filter "ItemGroupingTests|RangeValidationTests"`
Expected: PASS (5 tests). If `"1,000 g"` fails, check the locale argument on `formatted`: grouping must be on.

```bash
git add ios/MealPlannerKit/Sources/Features/Shopping/ItemGrouping.swift ios/MealPlannerKit/Sources/Features/Shopping/RangeValidation.swift ios/MealPlannerKit/Tests/MealPlannerKitTests/ItemGroupingTests.swift ios/MealPlannerKit/Tests/MealPlannerKitTests/RangeValidationTests.swift
git commit -m "feat(ios): add shopping aisle grouping, quantity text and range validation

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 4: `ListEvent`, `SSEFrameParser`, `ListEventReducer` (pure SSE rules)

**Files:**
- Create: `ios/MealPlannerKit/Sources/Repositories/Sync/ListEvent.swift`
- Test: `ios/MealPlannerKit/Tests/MealPlannerKitTests/ListEventTests.swift`

**Interfaces:**
- Consumes: `Components.Schemas.ShoppingList`.
- Produces: `ListEvent` (`kind: Kind`, `listID`, `itemID?`, `version?`; `static parse(data:) -> ListEvent?`), `ListEvent.Kind` (`itemChanged, itemDeleted, listChanged, listDeleted`), `SSEFrameParser` (`mutating feed(_ line: String) -> ListEvent?`), `ListEventAction` (`ignore, refetch, removeItem(String), accessLost`), `ListEventReducer.action(for:cached:) -> ListEventAction`.

- [ ] **Step 1: Write the failing tests**

```swift
import Testing
@testable import Repositories

@Suite
struct ListEventTests {
    private func feed(_ lines: [String]) -> [ListEvent] {
        var parser = SSEFrameParser()
        return lines.compactMap { parser.feed($0) }
    }

    private let changed = #"{"type":"item_changed","list_id":"l1","item_id":"i1","version":3}"#

    @Test("A data line ended by a blank line is one event")
    func basic() {
        let events = feed(["event: item_changed", "data: \(changed)", ""])
        #expect(events == [ListEvent(kind: .itemChanged, listID: "l1", itemID: "i1", version: 3)])
    }

    @Test("CRLF endings, keep-alive comments and ignored fields do not matter")
    func crlfAndComments() {
        let events = feed([": keep-alive\r", "", "id: 7\r", "data: \(changed)\r", "\r"])
        #expect(events.count == 1)
        #expect(events.first?.version == 3)
    }

    @Test("A frame split over several data lines is joined with newlines")
    func multiLine() {
        let events = feed([#"data: {"type":"list_changed","#, #"data: "list_id":"l1"}"#, ""])
        #expect(events == [ListEvent(kind: .listChanged, listID: "l1", itemID: nil, version: nil)])
    }

    @Test("Malformed JSON, an unknown type or a missing list id never produce an event")
    func hostile() {
        #expect(feed(["data: {not json", ""]).isEmpty)
        #expect(feed([#"data: {"type":"explode","list_id":"l1"}"#, ""]).isEmpty)
        #expect(feed([#"data: {"type":"list_deleted"}"#, ""]).isEmpty)
        #expect(feed([#"data: ["item_changed"]"#, ""]).isEmpty)
        #expect(feed(["data:", ""]).isEmpty)
    }

    @Test("A blank line with no data, and a frame cut off before its blank line, produce nothing")
    func incomplete() {
        #expect(feed(["", "", ""]).isEmpty)
        #expect(feed(["data: \(changed)"]).isEmpty)
    }

    @Test("The parser resets between frames")
    func resets() {
        let events = feed(["data: \(changed)", "", "data: \(changed)", ""])
        #expect(events.count == 2)
    }

    // MARK: Reducer

    private let list = ShoppingFixtures.list(items: [ShoppingFixtures.item(id: "i1", version: 3)])

    @Test("An event at or below the cached item version is ignored; a newer one refetches")
    func staleRule() {
        func action(_ version: Int) -> ListEventAction {
            ListEventReducer.action(for: ListEvent(kind: .itemChanged, listID: "l1", itemID: "i1", version: version), cached: list)
        }
        #expect(action(2) == .ignore)
        #expect(action(3) == .ignore)
        #expect(action(4) == .refetch)
    }

    @Test("An item_changed for an item the cache has never seen refetches")
    func unknownItem() {
        let event = ListEvent(kind: .itemChanged, listID: "l1", itemID: "new", version: 1)
        #expect(ListEventReducer.action(for: event, cached: list) == .refetch)
        #expect(ListEventReducer.action(for: event, cached: nil) == .refetch)
    }

    @Test("item_deleted removes a cached item whatever its version, and ignores an unknown one")
    func deleted() {
        let known = ListEvent(kind: .itemDeleted, listID: "l1", itemID: "i1", version: 3)
        let unknown = ListEvent(kind: .itemDeleted, listID: "l1", itemID: "zzz", version: 1)
        #expect(ListEventReducer.action(for: known, cached: list) == .removeItem("i1"))
        #expect(ListEventReducer.action(for: unknown, cached: list) == .ignore)
    }

    @Test("list_changed refetches and list_deleted means access is lost")
    func listLevel() {
        #expect(ListEventReducer.action(for: ListEvent(kind: .listChanged, listID: "l1", itemID: nil, version: nil), cached: list) == .refetch)
        #expect(ListEventReducer.action(for: ListEvent(kind: .listDeleted, listID: "l1", itemID: nil, version: nil), cached: list) == .accessLost)
    }
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `swift test --filter ListEventTests`
Expected: FAIL to compile, "cannot find 'SSEFrameParser' in scope".

- [ ] **Step 3: Implement `ListEvent.swift`**

```swift
import API
import Foundation

/// One event of a shopping list's stream: what changed, never the new content.
public struct ListEvent: Equatable, Sendable {
    public enum Kind: String, Sendable {
        case itemChanged = "item_changed"
        case itemDeleted = "item_deleted"
        case listChanged = "list_changed"
        case listDeleted = "list_deleted"
    }

    public var kind: Kind
    public var listID: String
    public var itemID: String?
    public var version: Int?

    public init(kind: Kind, listID: String, itemID: String? = nil, version: Int? = nil) {
        self.kind = kind
        self.listID = listID
        self.itemID = itemID
        self.version = version
    }

    /// Reads an event's `data`. Anything that is not one of the API's events is `nil`, so a malformed one can never act.
    public static func parse(data: String) -> ListEvent? {
        guard let object = (try? JSONSerialization.jsonObject(with: Data(data.utf8))) as? [String: Any],
              let type = object["type"] as? String, let kind = Kind(rawValue: type),
              let listID = object["list_id"] as? String
        else { return nil }
        return ListEvent(kind: kind, listID: listID, itemID: object["item_id"] as? String, version: object["version"] as? Int)
    }
}

/// Turns the lines of a `text/event-stream` into events. Feed one line at a time, without its newline. Only `data:` lines
/// matter (the JSON carries its own `type`); comments (`: keep-alive`), `event:`/`id:`/`retry:` and CRLF are ignored.
/// A frame ends at a blank line; a frame cut off before one is never delivered.
public struct SSEFrameParser: Sendable {
    private var dataLines: [String] = []

    public init() {}

    public mutating func feed(_ line: String) -> ListEvent? {
        var line = line
        if line.hasSuffix("\r") { line.removeLast() }
        if line.isEmpty {
            defer { dataLines = [] }
            guard !dataLines.isEmpty else { return nil }
            return ListEvent.parse(data: dataLines.joined(separator: "\n"))
        }
        guard line.hasPrefix("data:") else { return nil }
        var value = String(line.dropFirst(5))
        if value.hasPrefix(" ") { value.removeFirst() }
        dataLines.append(value)
        return nil
    }
}

public enum ListEventAction: Equatable, Sendable {
    case ignore
    case refetch
    case removeItem(String)
    case accessLost
}

/// The client rules for an event, pure (web's `useListEvents`).
public enum ListEventReducer {
    public static func action(for event: ListEvent, cached: Components.Schemas.ShoppingList?) -> ListEventAction {
        switch event.kind {
        case .listDeleted:
            return .accessLost
        case .listChanged:
            return .refetch
        case .itemDeleted:
            guard let id = event.itemID, cached?.items.contains(where: { $0.id == id }) == true else { return .ignore }
            return .removeItem(id)
        case .itemChanged:
            guard let id = event.itemID, let version = event.version else { return .refetch }
            // At or below what is held: often this person's own change, already seen.
            if let held = cached?.items.first(where: { $0.id == id }), held.version >= version { return .ignore }
            // Newer, or an item never seen (a version gap is a refetch too): the event carries no content.
            return .refetch
        }
    }
}
```

- [ ] **Step 4: Run to verify it passes, then commit**

Run: `swift test --filter ListEventTests`
Expected: PASS (10 tests).

```bash
git add ios/MealPlannerKit/Sources/Repositories/Sync/ListEvent.swift ios/MealPlannerKit/Tests/MealPlannerKitTests/ListEventTests.swift
git commit -m "feat(ios): add the shopping SSE frame parser and client event rules

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 5: `ShoppingCache` and the schema

**Files:**
- Create: `ios/MealPlannerKit/Sources/Persistence/CachedShoppingModels.swift`
- Create: `ios/MealPlannerKit/Sources/Persistence/ShoppingCache.swift`
- Modify: `ios/MealPlannerKit/Sources/Persistence/CacheStore.swift` (schema list, new factory)
- Test: `ios/MealPlannerKit/Tests/MealPlannerKitTests/ShoppingCacheTests.swift`

**Interfaces:**
- Consumes: `ShoppingIntent`, `IntentQueue` (Task 1), `MealScope`.
- Produces: `ShoppingCache` with
  `summaries(scope:) -> [ShoppingListSummary]`, `replaceSummaries(_:scope:)`, `mergeSummaries(_:scope:)`, `clear(scope:)`,
  `list(id:) -> ShoppingList?`, `store(_ list:)`, `applyItem(_ item:, listID:)`, `removeItem(id:listID:)`, `removeList(id:)`,
  `intents() -> [ShoppingIntent]`, `intents(listID:) -> [ShoppingIntent]`,
  `enqueue(kind:listID:itemID:payload:) -> [ShoppingIntent]` (the list's intents after collapsing),
  `removeIntent(id: UUID)`, `dropIntents(itemID:)`, `rewriteTempID(_:to:)`, `clearAll()`; `CacheStore.makeShoppingCache(_:)`.

- [ ] **Step 1: Write the failing tests** — `ShoppingCacheTests.swift`

```swift
import API
import Persistence
import SwiftData
import Testing

@Suite
struct ShoppingCacheTests {
    private func make() throws -> (ShoppingCache, ModelContainer) {
        let container = try CacheStore.inMemoryContainer()
        return (CacheStore.makeShoppingCache(container), container)
    }

    @Test("Summaries keep the server's order within a scope, and the scopes are separate")
    func summaryOrderAndScopes() async throws {
        let (cache, _) = try make()
        await cache.replaceSummaries([ShoppingFixtures.summary(id: "b", name: "B"), ShoppingFixtures.summary(id: "a", name: "A")], scope: .mine)
        await cache.replaceSummaries([ShoppingFixtures.summary(id: "p", name: "P")], scope: .partner)
        #expect(await cache.summaries(scope: .mine).map(\.id) == ["b", "a"])
        #expect(await cache.summaries(scope: .partner).map(\.id) == ["p"])
    }

    @Test("replace deletes ids absent from the first page; merge appends without deleting")
    func replaceAndMerge() async throws {
        let (cache, _) = try make()
        await cache.replaceSummaries([ShoppingFixtures.summary(id: "a"), ShoppingFixtures.summary(id: "b")], scope: .mine)
        await cache.mergeSummaries([ShoppingFixtures.summary(id: "c")], scope: .mine)
        #expect(await cache.summaries(scope: .mine).map(\.id) == ["a", "b", "c"])
        await cache.replaceSummaries([ShoppingFixtures.summary(id: "b")], scope: .mine)
        #expect(await cache.summaries(scope: .mine).map(\.id) == ["b"])
    }

    @Test("storing a list updates its summary row's name and sharing")
    func storeUpdatesSummary() async throws {
        let (cache, _) = try make()
        await cache.replaceSummaries([ShoppingFixtures.summary(id: "l1", name: "Old")], scope: .mine)
        await cache.store(ShoppingFixtures.list(id: "l1", name: "New", shared: true))
        let summary = try #require(await cache.summaries(scope: .mine).first)
        #expect(summary.name == "New")
        #expect(summary.sharedWithPartner)
    }

    @Test("applyItem inserts by position and never replaces an item with an older version")
    func applyItem() async throws {
        let (cache, _) = try make()
        await cache.store(ShoppingFixtures.list(items: [ShoppingFixtures.item(id: "i1", name: "Milk", version: 3, position: 1)]))
        await cache.applyItem(ShoppingFixtures.item(id: "i1", name: "Stale", version: 2, position: 1), listID: "l1")
        await cache.applyItem(ShoppingFixtures.item(id: "i0", name: "First", version: 1, position: 0), listID: "l1")
        await cache.applyItem(ShoppingFixtures.item(id: "i1", name: "Fresh", version: 4, position: 1), listID: "l1")
        let items = try #require(await cache.list(id: "l1")).items
        #expect(items.map(\.name) == ["First", "Fresh"])
    }

    @Test("applyItem and removeItem do nothing for a list that is not cached")
    func unknownList() async throws {
        let (cache, _) = try make()
        await cache.applyItem(ShoppingFixtures.item(), listID: "nope")
        await cache.removeItem(id: "i1", listID: "nope")
        #expect(await cache.list(id: "nope") == nil)
    }

    @Test("enqueue numbers intents monotonically across lists and collapses a repeated check")
    func enqueueCollapses() async throws {
        let (cache, _) = try make()
        _ = await cache.enqueue(kind: .check, listID: "l1", itemID: "i1", payload: nil)
        _ = await cache.enqueue(kind: .check, listID: "l2", itemID: "j1", payload: nil)
        let after = await cache.enqueue(kind: .uncheck, listID: "l1", itemID: "i1", payload: nil)
        #expect(after.map(\.kind) == [.uncheck])
        let all = await cache.intents()
        #expect(all.map(\.itemID) == ["j1", "i1"])
        #expect(all.map(\.sequence) == [2, 3])
    }

    @Test("Review focus 2: queued intents survive the app being killed (a new cache on the same store)")
    func survivesRelaunch() async throws {
        let (cache, container) = try make()
        let temp = ShoppingIntent.newTempID()
        _ = await cache.enqueue(kind: .add, listID: "l1", itemID: temp, payload: .init(name: "Bread", quantity: 2, unit: .piece))
        _ = await cache.enqueue(kind: .check, listID: "l1", itemID: "i1", payload: nil)
        let relaunched = CacheStore.makeShoppingCache(container)
        let intents = await relaunched.intents()
        #expect(intents.map(\.kind) == [.add, .check])
        #expect(intents[0].payload == .init(name: "Bread", quantity: 2, unit: .piece))
        #expect(intents[0].itemID == temp)
    }

    @Test("rewriteTempID points later intents at the server id; dropIntents forgets an item's rows")
    func rewriteAndDrop() async throws {
        let (cache, _) = try make()
        let temp = ShoppingIntent.newTempID()
        _ = await cache.enqueue(kind: .add, listID: "l1", itemID: temp, payload: .init(name: "Bread"))
        _ = await cache.enqueue(kind: .check, listID: "l1", itemID: temp, payload: nil)
        await cache.rewriteTempID(temp, to: "srv-1")
        #expect(await cache.intents().map(\.itemID) == ["srv-1", "srv-1"])
        await cache.dropIntents(itemID: "srv-1")
        #expect(await cache.intents().isEmpty)
    }

    @Test("removeList deletes the list, its summary row and its intents (review focus 4)")
    func removeList() async throws {
        let (cache, _) = try make()
        await cache.replaceSummaries([ShoppingFixtures.summary(id: "l1")], scope: .mine)
        await cache.store(ShoppingFixtures.list(id: "l1", items: [ShoppingFixtures.item()]))
        _ = await cache.enqueue(kind: .check, listID: "l1", itemID: "i1", payload: nil)
        _ = await cache.enqueue(kind: .check, listID: "l2", itemID: "j1", payload: nil)
        await cache.removeList(id: "l1")
        #expect(await cache.list(id: "l1") == nil)
        #expect(await cache.summaries(scope: .mine).isEmpty)
        #expect(await cache.intents().map(\.listID) == ["l2"])
    }

    @Test("clearAll empties everything")
    func clearAll() async throws {
        let (cache, _) = try make()
        await cache.store(ShoppingFixtures.list())
        await cache.replaceSummaries([ShoppingFixtures.summary()], scope: .partner)
        _ = await cache.enqueue(kind: .check, listID: "l1", itemID: "i1", payload: nil)
        await cache.clearAll()
        #expect(await cache.list(id: "l1") == nil)
        #expect(await cache.summaries(scope: .partner).isEmpty)
        #expect(await cache.intents().isEmpty)
    }
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `swift test --filter ShoppingCacheTests`
Expected: FAIL to compile, "cannot find 'ShoppingCache'" / "makeShoppingCache".

- [ ] **Step 3: Implement**

`CachedShoppingModels.swift`:

```swift
import Foundation
import SwiftData

/// Internal to this module: `@Model` objects never leave `ShoppingCache`. Lists are stored as the generated JSON
/// because nothing queries inside them.

/// A row of the Mine / Partner's list of lists. `order` keeps the server's order.
@Model
final class CachedShoppingSummary {
    @Attribute(.unique) var id: String
    var scopeRaw: String
    var order: Int
    var json: Data

    init(id: String, scopeRaw: String, order: Int, json: Data) {
        self.id = id
        self.scopeRaw = scopeRaw
        self.order = order
        self.json = json
    }
}

/// The server's last snapshot of one list, items included. Pending changes are never written here.
@Model
final class CachedShoppingList {
    @Attribute(.unique) var id: String
    var json: Data

    init(id: String, json: Data) {
        self.id = id
        self.json = json
    }
}

/// One pending offline change (`ShoppingIntent`).
@Model
final class CachedIntent {
    @Attribute(.unique) var id: UUID
    var sequence: Int
    var kindRaw: String
    var listID: String
    var itemID: String
    var payloadJSON: Data?

    init(id: UUID, sequence: Int, kindRaw: String, listID: String, itemID: String, payloadJSON: Data?) {
        self.id = id
        self.sequence = sequence
        self.kindRaw = kindRaw
        self.listID = listID
        self.itemID = itemID
        self.payloadJSON = payloadJSON
    }
}
```

`ShoppingCache.swift`:

```swift
import API
import Foundation
import SwiftData

/// The shopping cache: list summaries (Mine / Partner's, reusing `MealScope`), the server's snapshot of each list, and
/// the offline queue. Takes and returns value types. Snapshots are best-effort (rebuildable from the API); the queue is
/// not, which is why intents are written in the same save as the change that produced them.
@ModelActor
public actor ShoppingCache {
    // MARK: Summaries

    public func summaries(scope: MealScope) -> [Components.Schemas.ShoppingListSummary] {
        summaryRows().filter { $0.scopeRaw == scope.rawValue }.sorted { $0.order < $1.order }
            .compactMap { try? JSONDecoder().decode(Components.Schemas.ShoppingListSummary.self, from: $0.json) }
    }

    /// The first page of a scope: upserts in the given order and deletes ids absent from `summaries` within `scope`.
    public func replaceSummaries(_ summaries: [Components.Schemas.ShoppingListSummary], scope: MealScope) {
        upsert(summaries, scope: scope, firstOrder: 0)
        let incoming = Set(summaries.map(\.id))
        for row in summaryRows() where row.scopeRaw == scope.rawValue && !incoming.contains(row.id) { modelContext.delete(row) }
        try? modelContext.save()
    }

    /// A later page ("Load more"): upserts after the rows already there, deletes nothing.
    public func mergeSummaries(_ summaries: [Components.Schemas.ShoppingListSummary], scope: MealScope) {
        let next = (summaryRows().filter { $0.scopeRaw == scope.rawValue }.map(\.order).max() ?? -1) + 1
        upsert(summaries, scope: scope, firstOrder: next)
        try? modelContext.save()
    }

    public func clear(scope: MealScope) {
        for row in summaryRows() where row.scopeRaw == scope.rawValue { modelContext.delete(row) }
        try? modelContext.save()
    }

    // MARK: Lists

    public func list(id: String) -> Components.Schemas.ShoppingList? {
        listRow(id).flatMap { try? JSONDecoder().decode(Components.Schemas.ShoppingList.self, from: $0.json) }
    }

    /// Stores the server's answer for a list, and brings an existing summary row up to date (rename, sharing).
    public func store(_ list: Components.Schemas.ShoppingList) {
        guard let json = try? JSONEncoder().encode(list) else { return }
        if let row = listRow(list.id) { row.json = json } else { modelContext.insert(CachedShoppingList(id: list.id, json: json)) }
        if let row = summaryRows().first(where: { $0.id == list.id }) {
            let summary = Components.Schemas.ShoppingListSummary(
                id: list.id, name: list.name, sharedWithPartner: list.sharedWithPartner, sourceFrom: list.sourceFrom,
                sourceTo: list.sourceTo, createdAt: list.createdAt, updatedAt: list.updatedAt
            )
            if let encoded = try? JSONEncoder().encode(summary) { row.json = encoded }
        }
        try? modelContext.save()
    }

    /// Takes in what the server said about one item, unless the snapshot already holds a newer version of it.
    public func applyItem(_ item: Components.Schemas.ShoppingItem, listID: String) {
        guard var list = list(id: listID) else { return }
        if let index = list.items.firstIndex(where: { $0.id == item.id }) {
            guard list.items[index].version <= item.version else { return }
            list.items[index] = item
        } else {
            list.items.append(item)
            list.items.sort { $0.position < $1.position }
        }
        store(list)
    }

    public func removeItem(id: String, listID: String) {
        guard var list = list(id: listID), list.items.contains(where: { $0.id == id }) else { return }
        list.items.removeAll { $0.id == id }
        store(list)
    }

    /// The list is gone (deleted, unshared, unlinked): its snapshot, its summary row and its pending changes.
    public func removeList(id: String) {
        if let row = listRow(id) { modelContext.delete(row) }
        for row in summaryRows() where row.id == id { modelContext.delete(row) }
        for row in intentRows() where row.listID == id { modelContext.delete(row) }
        try? modelContext.save()
    }

    // MARK: Queue

    /// Every pending change, in `sequence` order.
    public func intents() -> [ShoppingIntent] {
        intentRows().sorted { $0.sequence < $1.sequence }.compactMap(Self.intent)
    }

    public func intents(listID: String) -> [ShoppingIntent] {
        intents().filter { $0.listID == listID }
    }

    /// Adds a change, collapsing it against what is already pending (`IntentQueue`), in one save. Returns the
    /// list's pending changes afterwards.
    @discardableResult
    public func enqueue(kind: ShoppingIntent.Kind, listID: String, itemID: String, payload: ShoppingIntent.AddPayload?) -> [ShoppingIntent] {
        let all = intents()
        let next = ShoppingIntent(
            sequence: (all.map(\.sequence).max() ?? 0) + 1, kind: kind, listID: listID, itemID: itemID, payload: payload
        )
        let result = IntentQueue.enqueue(next, into: all)
        let keep = Set(result.map(\.id))
        let existing = Set(all.map(\.id))
        for row in intentRows() where !keep.contains(row.id) { modelContext.delete(row) }
        for intent in result where !existing.contains(intent.id) {
            modelContext.insert(CachedIntent(
                id: intent.id, sequence: intent.sequence, kindRaw: intent.kind.rawValue, listID: intent.listID,
                itemID: intent.itemID, payloadJSON: intent.payload.flatMap { try? JSONEncoder().encode($0) }
            ))
        }
        try? modelContext.save()
        return result.filter { $0.listID == listID }
    }

    public func removeIntent(id: UUID) {
        for row in intentRows() where row.id == id { modelContext.delete(row) }
        try? modelContext.save()
    }

    /// Forgets every pending change for one item (its add was refused, so nothing addressed to it can succeed).
    public func dropIntents(itemID: String) {
        for row in intentRows() where row.itemID == itemID { modelContext.delete(row) }
        try? modelContext.save()
    }

    /// An add succeeded: later changes addressed to `temp` now go to the server's id.
    public func rewriteTempID(_ temp: String, to real: String) {
        for row in intentRows() where row.itemID == temp { row.itemID = real }
        try? modelContext.save()
    }

    public func clearAll() {
        for row in summaryRows() { modelContext.delete(row) }
        for row in (try? modelContext.fetch(FetchDescriptor<CachedShoppingList>())) ?? [] { modelContext.delete(row) }
        for row in intentRows() { modelContext.delete(row) }
        try? modelContext.save()
    }

    // MARK: Private

    private func upsert(_ summaries: [Components.Schemas.ShoppingListSummary], scope: MealScope, firstOrder: Int) {
        let existing = Dictionary(summaryRows().map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        for (offset, summary) in summaries.enumerated() {
            guard let json = try? JSONEncoder().encode(summary) else { continue }
            if let row = existing[summary.id] {
                row.json = json
                row.scopeRaw = scope.rawValue
                row.order = firstOrder + offset
            } else {
                modelContext.insert(CachedShoppingSummary(id: summary.id, scopeRaw: scope.rawValue, order: firstOrder + offset, json: json))
            }
        }
    }

    private func summaryRows() -> [CachedShoppingSummary] {
        (try? modelContext.fetch(FetchDescriptor<CachedShoppingSummary>())) ?? []
    }

    private func listRow(_ id: String) -> CachedShoppingList? {
        (try? modelContext.fetch(FetchDescriptor<CachedShoppingList>()))?.first { $0.id == id }
    }

    private func intentRows() -> [CachedIntent] {
        (try? modelContext.fetch(FetchDescriptor<CachedIntent>())) ?? []
    }

    private static func intent(_ row: CachedIntent) -> ShoppingIntent? {
        guard let kind = ShoppingIntent.Kind(rawValue: row.kindRaw) else { return nil }
        return ShoppingIntent(
            id: row.id, sequence: row.sequence, kind: kind, listID: row.listID, itemID: row.itemID,
            payload: row.payloadJSON.flatMap { try? JSONDecoder().decode(ShoppingIntent.AddPayload.self, from: $0) }
        )
    }
}
```

In `CacheStore.swift`, add the three classes to the schema and the factory:

```swift
    private static let schema = Schema([
        CachedMeal.self, CachedMealIngredient.self, CachedPlanDay.self, CachedTargets.self,
        CachedTemplate.self, CachedShoppingSummary.self, CachedShoppingList.self, CachedIntent.self,
    ])
```

```swift
    public static func makeShoppingCache(_ container: ModelContainer) -> ShoppingCache {
        ShoppingCache(modelContainer: container)
    }
```

- [ ] **Step 4: Run to verify it passes**

Run: `swift test --filter ShoppingCacheTests`
Expected: PASS (10 tests). Then `swift test` (whole suite) to confirm the schema change broke nothing: all existing suites still PASS.

- [ ] **Step 5: Commit**

```bash
git add ios/MealPlannerKit/Sources/Persistence ios/MealPlannerKit/Tests/MealPlannerKitTests/ShoppingCacheTests.swift
git commit -m "feat(ios): add ShoppingCache with list snapshots and the persisted offline queue

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 6: `ShoppingError`, `ShoppingListsRepository`, error text

**Files:**
- Create: `ios/MealPlannerKit/Sources/Repositories/ShoppingError.swift`
- Create: `ios/MealPlannerKit/Sources/Repositories/ShoppingListsRepository.swift`
- Modify: `ios/MealPlannerKit/Sources/Features/Shared/ErrorText.swift` (new `case let error as ShoppingError`)
- Test: `ios/MealPlannerKit/Tests/MealPlannerKitTests/ShoppingRepositoryTests.swift`

**Interfaces:**
- Consumes: `ShoppingCache` (Task 5), `ShoppingIntent` (Task 1), `unwrapping`, `ProblemText`, `MealScope`.
- Produces:
  - `ShoppingError`: `notFound`, `partnerNotLinked`, `versionConflict(current: Components.Schemas.ShoppingItem)`, `validationFailed(String)`, `unauthorized`, `rateLimited`, `server(String)`.
  - `ItemEdit` (`name`, `quantity: Double?`, `unit: Components.Schemas.Unit?`, `category`).
  - `ShoppingListsRepository(client:cache:)` with:
    reads `cachedLists(_:)`, `cachedList(id:)`, `pendingIntents(listID:)`, `refreshLists(_:cursor:) -> String?` (next cursor), `refreshList(id:) -> ShoppingList` (discardable);
    online writes `createList(name:shared:)`, `generate(from:to:name:listID:)`, `updateList(id:name:shared:)`, `deleteList(id:)`, `editItem(listID:itemID:version:edit:)`;
    offline-able `setChecked(_:itemID:listID:)`, `addItem(_:listID:) -> String` (the temp id), `removeItem(itemID:listID:)`;
    cache upkeep `removeCachedItem(itemID:listID:)`, `dropList(id:)`, `clearPartnerLists()`, `clearCaches()`.

- [ ] **Step 1: Write the failing tests** — `ShoppingRepositoryTests.swift`

```swift
import API
import Foundation
import Persistence
import Testing
@testable import Repositories

@Suite
struct ShoppingRepositoryTests {
    private func make(_ route: @escaping RoutingTransport.Route) throws -> (ShoppingListsRepository, ShoppingCache, RoutingTransport) {
        let transport = RoutingTransport(route)
        let cache = CacheStore.makeShoppingCache(try CacheStore.inMemoryContainer())
        return (ShoppingListsRepository(client: makeAuthlessClient(transport: transport), cache: cache), cache, transport)
    }

    @Test("The first page replaces the scope and returns the next cursor; the next page merges")
    func paging() async throws {
        let (repo, _, transport) = try make { call in
            if call.path.contains("cursor=c2") { return (200, ShoppingFixtures.page([ShoppingFixtures.summary(id: "c")])) }
            return (200, ShoppingFixtures.page([ShoppingFixtures.summary(id: "a"), ShoppingFixtures.summary(id: "b")], next: "c2"))
        }
        let next = try await repo.refreshLists(.mine, cursor: nil)
        #expect(next == "c2")
        #expect(await repo.cachedLists(.mine).map(\.id) == ["a", "b"])
        let last = try await repo.refreshLists(.mine, cursor: "c2")
        #expect(last == nil)
        #expect(await repo.cachedLists(.mine).map(\.id) == ["a", "b", "c"])
        #expect(await transport.calls("GET /shopping-lists").count == 2)
    }

    @Test("The partner scope reads /partner/shopping-lists; a 404 clears it and says the partner is not linked")
    func partnerScope() async throws {
        let linked = Locked(true)
        let (repo, _, transport) = try make { _ in
            linked.value ? (200, ShoppingFixtures.page([ShoppingFixtures.summary(id: "p")])) : (404, Fixtures.problem(404, code: "partner_not_linked"))
        }
        _ = try await repo.refreshLists(.partner, cursor: nil)
        #expect(await repo.cachedLists(.partner).map(\.id) == ["p"])
        #expect(await transport.calls("GET /partner/shopping-lists").count == 1)
        linked.set(false)
        await #expect(throws: ShoppingError.partnerNotLinked) { try await repo.refreshLists(.partner, cursor: nil) }
        #expect(await repo.cachedLists(.partner).isEmpty)
    }

    @Test("refreshList stores the snapshot; a failed refresh leaves the cache alone")
    func refreshList() async throws {
        let fail = Locked(false)
        let list = ShoppingFixtures.list(items: [ShoppingFixtures.item()])
        let (repo, _, _) = try make { _ in fail.value ? (500, Fixtures.problem(500, code: "internal")) : (200, Fixtures.json(list)) }
        try await repo.refreshList(id: "l1")
        #expect(await repo.cachedList(id: "l1")?.items.count == 1)
        fail.set(true)
        await #expect(throws: ShoppingError.server("The server had a problem loading the list.")) { try await repo.refreshList(id: "l1") }
        #expect(await repo.cachedList(id: "l1")?.items.count == 1)
    }

    @Test("Review focus 4: a 404 on refresh removes the list and its pending changes")
    func refreshListGone() async throws {
        let (repo, cache, _) = try make { _ in (404, Fixtures.problem(404, code: "not_found")) }
        await cache.store(ShoppingFixtures.list(items: [ShoppingFixtures.item()]))
        await repo.setChecked(true, itemID: "i1", listID: "l1")
        await #expect(throws: ShoppingError.notFound) { try await repo.refreshList(id: "l1") }
        #expect(await repo.cachedList(id: "l1") == nil)
        #expect(await repo.pendingIntents(listID: "l1").isEmpty)
    }

    @Test("generate accepts 200 (regenerated) and 201 (new), and sends list_id when regenerating")
    func generate() async throws {
        let status = Locked(201)
        let (repo, _, transport) = try make { _ in (status.value, Fixtures.json(ShoppingFixtures.list(id: "g1", from: "2026-10-05", to: "2026-10-11"))) }
        let created = try await repo.generate(from: "2026-10-05", to: "2026-10-11", name: "Week", listID: nil)
        #expect(created.id == "g1")
        status.set(200)
        _ = try await repo.generate(from: "2026-10-05", to: "2026-10-11", name: nil, listID: "g1")
        let bodies = await transport.calls("POST /shopping-lists/generate").map(\.body)
        #expect(bodies[0].contains("\"name\":\"Week\""))
        #expect(!bodies[0].contains("list_id"))
        #expect(bodies[1].contains("\"list_id\":\"g1\""))
        #expect(await repo.cachedList(id: "g1") != nil)
    }

    @Test("generate over an empty plan is a 404")
    func generateNothingPlanned() async throws {
        let (repo, _, _) = try make { _ in (404, Fixtures.problem(404, code: "not_found")) }
        await #expect(throws: ShoppingError.notFound) { try await repo.generate(from: "2026-10-05", to: "2026-10-06", name: nil, listID: nil) }
    }

    @Test("editItem sends the version and fields; the answer replaces the cached item")
    func editItem() async throws {
        let edited = ShoppingFixtures.item(name: "Oat milk", version: 2, quantity: 1.5, unit: .ml)
        let (repo, cache, transport) = try make { _ in (200, Fixtures.json(edited)) }
        await cache.store(ShoppingFixtures.list(items: [ShoppingFixtures.item()]))
        let item = try await repo.editItem(
            listID: "l1", itemID: "i1", version: 1, edit: .init(name: "Oat milk", quantity: 1.5, unit: .ml, category: .dairyEggs)
        )
        #expect(item == edited)
        let body = try #require(await transport.calls("PATCH /shopping-lists/l1/items/i1").first?.body)
        #expect(body.contains("\"version\":1"))
        #expect(body.contains("\"name\":\"Oat milk\""))
        #expect(body.contains("\"unit\":\"ml\""))
        #expect(await cache.list(id: "l1")?.items.first?.name == "Oat milk")
    }

    @Test("A 409 carries the item's current state, which also replaces the cached item")
    func editConflict() async throws {
        let current = ShoppingFixtures.item(name: "Someone else's", version: 5)
        let (repo, cache, _) = try make { _ in (409, ShoppingFixtures.conflict(current: current)) }
        await cache.store(ShoppingFixtures.list(items: [ShoppingFixtures.item(version: 3)]))
        await #expect(throws: ShoppingError.versionConflict(current: current)) {
            try await repo.editItem(listID: "l1", itemID: "i1", version: 3, edit: .init(name: "Mine", quantity: nil, unit: nil, category: .other))
        }
        #expect(await cache.list(id: "l1")?.items.first?.version == 5)
    }

    @Test("Rename and share patch the list; delete removes it, and deleting a list already gone is a success")
    func listWrites() async throws {
        let (repo, cache, transport) = try make { call in
            if call.method == "DELETE" { return (404, Fixtures.problem(404, code: "not_found")) }
            return (200, Fixtures.json(ShoppingFixtures.list(name: "Renamed", shared: true)))
        }
        let updated = try await repo.updateList(id: "l1", name: "Renamed", shared: true)
        #expect(updated.sharedWithPartner)
        #expect(try #require(await transport.calls("PATCH /shopping-lists/l1").first).body.contains("\"shared_with_partner\":true"))
        try await repo.deleteList(id: "l1")
        #expect(await cache.list(id: "l1") == nil)
    }

    @Test("The offline-able writes only enqueue, collapse, and send nothing")
    func enqueueOnly() async throws {
        let (repo, _, transport) = try make { _ in (500, "") }
        await repo.setChecked(true, itemID: "i1", listID: "l1")
        await repo.setChecked(false, itemID: "i1", listID: "l1")
        #expect(await repo.pendingIntents(listID: "l1").map(\.kind) == [.uncheck])
        let temp = await repo.addItem(.init(name: "Bread"), listID: "l1")
        #expect(temp.hasPrefix(ShoppingIntent.tempPrefix))
        await repo.removeItem(itemID: temp, listID: "l1")
        #expect(await repo.pendingIntents(listID: "l1").map(\.kind) == [.uncheck])
        #expect(await transport.calls.isEmpty)
    }

    @Test("A 400 becomes a readable validation message")
    func validation() async throws {
        let (repo, _, _) = try make { _ in (400, Fixtures.problem(400, code: "validation_failed", errors: [("name", "required")])) }
        await #expect(throws: ShoppingError.validationFailed("Name is required.")) { try await repo.createList(name: "", shared: false) }
    }
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `swift test --filter ShoppingRepositoryTests`
Expected: FAIL to compile, "cannot find 'ShoppingListsRepository' in scope".

- [ ] **Step 3: Implement**

`ShoppingError.swift`:

```swift
import API

public enum ShoppingError: Error, Equatable, Sendable {
    /// The list or item is gone, or access to it was (unshared, unlinked, deleted). Never `403`.
    case notFound
    case partnerNotLinked
    /// `409 version_conflict` on an item edit: `current` is the item as the server has it now.
    case versionConflict(current: Components.Schemas.ShoppingItem)
    case validationFailed(String)
    case unauthorized
    case rateLimited
    case server(String)

    static func unexpected(_ status: Int) -> ShoppingError { .server("Unexpected response (\(status)).") }

    static func validation(_ problem: Components.Schemas.Problem?) -> ShoppingError {
        .validationFailed(problem.map(ProblemText.validation) ?? "The request was not accepted.")
    }
}
```

`ShoppingListsRepository.swift`:

```swift
import API
import Foundation
import Persistence

/// What the item edit sheet sends. A `nil` quantity or unit means "leave as it is": the generated optional cannot
/// encode an explicit `null` (the same limit as a meal's notes).
public struct ItemEdit: Equatable, Sendable {
    public var name: String
    public var quantity: Double?
    public var unit: Components.Schemas.Unit?
    public var category: Components.Schemas.IngredientCategory

    public init(name: String, quantity: Double?, unit: Components.Schemas.Unit?, category: Components.Schemas.IngredientCategory) {
        self.name = name
        self.quantity = quantity
        self.unit = unit
        self.category = category
    }
}

/// Shopping lists, cache-first (`cached…()` then `refresh…()`). Item `check`/`add`/`remove` only enqueue (the sync
/// engine sends them); every other write goes straight to the API and stores its answer.
public struct ShoppingListsRepository: Sendable {
    private let client: Client
    private let cache: ShoppingCache
    private static let pageSize = 50

    public init(client: Client, cache: ShoppingCache) {
        self.client = client
        self.cache = cache
    }

    // MARK: Reads

    public func cachedLists(_ scope: MealScope) async -> [Components.Schemas.ShoppingListSummary] {
        await cache.summaries(scope: scope)
    }

    public func cachedList(id: String) async -> Components.Schemas.ShoppingList? { await cache.list(id: id) }

    public func pendingIntents(listID: String) async -> [ShoppingIntent] { await cache.intents(listID: listID) }

    /// One page of lists. `cursor == nil` is the first page and replaces the scope in the cache; a later page is merged.
    /// Returns the next cursor, or `nil` when there is no more. A failure leaves the cache untouched, except that
    /// `404 partner_not_linked` clears the partner scope.
    public func refreshLists(_ scope: MealScope, cursor: String?) async throws -> String? {
        let page: Components.Schemas.ShoppingListPage
        switch scope {
        case .mine:
            let response = try await unwrapping {
                try await client.listShoppingLists(.init(query: .init(cursor: cursor, limit: Self.pageSize)))
            }
            switch response {
            case .ok(let ok): page = try ok.body.json
            case .badRequest(let r): throw ShoppingError.validation(r.problem)
            case .unauthorized: throw ShoppingError.unauthorized
            case .tooManyRequests: throw ShoppingError.rateLimited
            case .internalServerError: throw ShoppingError.server("The server had a problem loading your lists.")
            case .undocumented(let status, _): throw ShoppingError.unexpected(status)
            }
        case .partner:
            let response = try await unwrapping {
                try await client.listPartnerShoppingLists(.init(query: .init(cursor: cursor, limit: Self.pageSize)))
            }
            switch response {
            case .ok(let ok): page = try ok.body.json
            case .badRequest(let r): throw ShoppingError.validation(r.problem)
            case .unauthorized: throw ShoppingError.unauthorized
            case .notFound:
                await cache.clear(scope: .partner)
                throw ShoppingError.partnerNotLinked
            case .tooManyRequests: throw ShoppingError.rateLimited
            case .internalServerError: throw ShoppingError.server("The server had a problem loading your partner's lists.")
            case .undocumented(let status, _): throw ShoppingError.unexpected(status)
            }
        }
        if cursor == nil {
            await cache.replaceSummaries(page.items, scope: scope)
        } else {
            await cache.mergeSummaries(page.items, scope: scope)
        }
        return page.nextCursor
    }

    /// `GET /shopping-lists/{id}`; the answer replaces the snapshot. `404` means access is gone: the list and its
    /// pending changes are removed from the cache.
    @discardableResult
    public func refreshList(id: String) async throws -> Components.Schemas.ShoppingList {
        let response = try await unwrapping { try await client.getShoppingList(.init(path: .init(id: id))) }
        switch response {
        case .ok(let ok): return try await keep(try ok.body.json)
        case .badRequest(let r): throw ShoppingError.validation(r.problem)
        case .unauthorized: throw ShoppingError.unauthorized
        case .notFound:
            await cache.removeList(id: id)
            throw ShoppingError.notFound
        case .tooManyRequests: throw ShoppingError.rateLimited
        case .internalServerError: throw ShoppingError.server("The server had a problem loading the list.")
        case .undocumented(let status, _): throw ShoppingError.unexpected(status)
        }
    }

    // MARK: Online writes

    public func createList(name: String, shared: Bool) async throws -> Components.Schemas.ShoppingList {
        let response = try await unwrapping {
            try await client.createShoppingList(.init(body: .json(.init(name: name, sharedWithPartner: shared))))
        }
        switch response {
        case .created(let created): return try await keep(try created.body.json)
        case .badRequest(let r): throw ShoppingError.validation(r.problem)
        case .unauthorized: throw ShoppingError.unauthorized
        case .tooManyRequests: throw ShoppingError.rateLimited
        case .internalServerError: throw ShoppingError.server("The server had a problem creating the list.")
        case .undocumented(let status, _): throw ShoppingError.unexpected(status)
        }
    }

    /// Builds a list from the plan between two dates, or regenerates `listID`. `404`: nothing is planned in the range.
    public func generate(from: String, to: String, name: String?, listID: String?) async throws -> Components.Schemas.ShoppingList {
        let response = try await unwrapping {
            try await client.generateShoppingList(.init(body: .json(.init(from: from, to: to, name: name, listId: listID))))
        }
        switch response {
        case .ok(let ok): return try await keep(try ok.body.json)
        case .created(let created): return try await keep(try created.body.json)
        case .badRequest(let r): throw ShoppingError.validation(r.problem)
        case .unauthorized: throw ShoppingError.unauthorized
        case .notFound: throw ShoppingError.notFound
        case .tooManyRequests: throw ShoppingError.rateLimited
        case .internalServerError: throw ShoppingError.server("The server had a problem generating the list.")
        case .undocumented(let status, _): throw ShoppingError.unexpected(status)
        }
    }

    public func updateList(id: String, name: String?, shared: Bool?) async throws -> Components.Schemas.ShoppingList {
        let response = try await unwrapping {
            try await client.updateShoppingList(.init(path: .init(id: id), body: .json(.init(name: name, sharedWithPartner: shared))))
        }
        switch response {
        case .ok(let ok): return try await keep(try ok.body.json)
        case .badRequest(let r): throw ShoppingError.validation(r.problem)
        case .unauthorized: throw ShoppingError.unauthorized
        case .notFound:
            await cache.removeList(id: id)
            throw ShoppingError.notFound
        case .tooManyRequests: throw ShoppingError.rateLimited
        case .internalServerError: throw ShoppingError.server("The server had a problem updating the list.")
        case .undocumented(let status, _): throw ShoppingError.unexpected(status)
        }
    }

    /// Deleting a list that is already gone (`404`) is a success: the goal is met.
    public func deleteList(id: String) async throws {
        let response = try await unwrapping { try await client.deleteShoppingList(.init(path: .init(id: id))) }
        switch response {
        case .noContent, .notFound: await cache.removeList(id: id)
        case .badRequest(let r): throw ShoppingError.validation(r.problem)
        case .unauthorized: throw ShoppingError.unauthorized
        case .tooManyRequests: throw ShoppingError.rateLimited
        case .internalServerError: throw ShoppingError.server("The server had a problem deleting the list.")
        case .undocumented(let status, _): throw ShoppingError.unexpected(status)
        }
    }

    /// Edits an item against the version the form was opened on. `409` throws `versionConflict(current:)` and also puts
    /// that current item in the cache, so the screen already shows what the server has.
    @discardableResult
    public func editItem(listID: String, itemID: String, version: Int, edit: ItemEdit) async throws -> Components.Schemas.ShoppingItem {
        let response = try await unwrapping {
            try await client.updateShoppingItem(.init(
                path: .init(id: listID, itemId: itemID),
                body: .json(.init(
                    version: version, name: edit.name, quantity: edit.quantity,
                    unit: edit.unit.flatMap { Components.Schemas.UpdateShoppingItemRequest.UnitPayload(rawValue: $0.rawValue) },
                    category: edit.category
                ))
            ))
        }
        switch response {
        case .ok(let ok):
            let item = try ok.body.json
            await cache.applyItem(item, listID: listID)
            return item
        case .conflict(let conflict):
            let current = try conflict.body.applicationProblemJson.current
            await cache.applyItem(current, listID: listID)
            throw ShoppingError.versionConflict(current: current)
        case .badRequest(let r): throw ShoppingError.validation(r.problem)
        case .unauthorized: throw ShoppingError.unauthorized
        case .notFound:
            await cache.removeItem(id: itemID, listID: listID)
            throw ShoppingError.notFound
        case .tooManyRequests: throw ShoppingError.rateLimited
        case .internalServerError: throw ShoppingError.server("The server had a problem saving the item.")
        case .undocumented(let status, _): throw ShoppingError.unexpected(status)
        }
    }

    // MARK: Offline-able writes (enqueue only; the sync engine sends them)

    public func setChecked(_ checked: Bool, itemID: String, listID: String) async {
        await cache.enqueue(kind: checked ? .check : .uncheck, listID: listID, itemID: itemID, payload: nil)
    }

    /// Queues an item for the list and returns its temporary id (shown at once, replaced by the server's id after sync).
    @discardableResult
    public func addItem(_ payload: ShoppingIntent.AddPayload, listID: String) async -> String {
        let temp = ShoppingIntent.newTempID()
        await cache.enqueue(kind: .add, listID: listID, itemID: temp, payload: payload)
        return temp
    }

    public func removeItem(itemID: String, listID: String) async {
        await cache.enqueue(kind: .remove, listID: listID, itemID: itemID, payload: nil)
    }

    // MARK: Cache upkeep

    public func removeCachedItem(itemID: String, listID: String) async { await cache.removeItem(id: itemID, listID: listID) }
    public func dropList(id: String) async { await cache.removeList(id: id) }
    public func clearPartnerLists() async { await cache.clear(scope: .partner) }

    /// Called whenever the session ends, so a second user on this device never sees (or sends) the first user's lists.
    public func clearCaches() async { await cache.clearAll() }

    private func keep(_ list: Components.Schemas.ShoppingList) async -> Components.Schemas.ShoppingList {
        await cache.store(list)
        return list
    }
}
```

In `ErrorText.swift`, add this case just above `case is URLError:`:

```swift
        case let error as ShoppingError:
            switch error {
            case .notFound: return "This list isn't available anymore."
            case .partnerNotLinked: return "You're not linked with a partner."
            case .versionConflict: return "Someone else changed this item."
            case .validationFailed(let message): return message
            case .unauthorized: return "Please sign in again."
            case .rateLimited: return rateLimited
            case .server(let message): return message
            }
```

- [ ] **Step 4: Run to verify it passes**

Run: `swift test --filter ShoppingRepositoryTests`
Expected: PASS (11 tests). If a generated-client label differs (for example `.init(path: .init(id:itemId:))`), read the generated `Operations.UpdateShoppingItem.Input.Path` in `Sources/API/GeneratedSources/Types+Operations.swift` and match it; do not edit that file.

- [ ] **Step 5: Commit**

```bash
git add ios/MealPlannerKit/Sources/Repositories ios/MealPlannerKit/Sources/Features/Shared/ErrorText.swift ios/MealPlannerKit/Tests/MealPlannerKitTests/ShoppingRepositoryTests.swift
git commit -m "feat(ios): add ShoppingListsRepository and ShoppingError

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 7: `ShoppingSyncEngine` (drain the queue) and the in-memory `ShoppingServer`

**Files:**
- Create: `ios/MealPlannerKit/Sources/Repositories/Sync/ShoppingSyncEngine.swift`
- Create: `ios/MealPlannerKit/Tests/MealPlannerKitTests/ShoppingServer.swift` (test support, reused by the view-model tests)
- Test: `ios/MealPlannerKit/Tests/MealPlannerKitTests/ShoppingSyncEngineTests.swift`

**Interfaces:**
- Consumes: `ShoppingCache` (Task 5), `ShoppingFixtures` (Task 2), `RoutingTransport`/`Gate`/`Locked` (existing test support).
- Produces:
  - `ShoppingSyncEngine(client:cache:)` (an `actor`) with `drain() async` (coalesces concurrent calls), `changes() -> AsyncStream<Void>` (one element after every queue step; one stream per caller), `takeNotice() -> String?`.
  - `ShoppingServer(_ lists:)` with `route(_:)`, `setOffline(_:)`, `setPartnerLinked(_:)`, `force(_ route:status:)`, `list(_:)`, `mutate(listID:itemID:_:)`, `deleteItem(listID:itemID:)`, `deleteList(_:)`. New server ids are `srv-101`, `srv-102`, …

- [ ] **Step 1: Write `ShoppingServer.swift` (test support)**

```swift
import API
import Foundation

/// A tiny in-memory stand-in for the shopping endpoints, so tests read like the real round trip. Item `PATCH` honours
/// `version` (a mismatch is the real `409` with `current`), `checked` is unversioned, every change bumps `version`.
final class ShoppingServer: @unchecked Sendable {
    private let lock = NSLock()
    private var lists: [String: Components.Schemas.ShoppingList]
    private var offline = false
    private var partnerLinked = true
    private var forced: [String: Int] = [:]
    private var counter = 100

    init(_ lists: [Components.Schemas.ShoppingList] = []) {
        self.lists = Dictionary(uniqueKeysWithValues: lists.map { ($0.id, $0) })
    }

    func setOffline(_ value: Bool) { lock.lock(); offline = value; lock.unlock() }
    func setPartnerLinked(_ value: Bool) { lock.lock(); partnerLinked = value; lock.unlock() }

    /// Every request for this exact route (`"PATCH /shopping-lists/l1/items/i1"`) answers `status`.
    func force(_ route: String, status: Int) { lock.lock(); forced[route] = status; lock.unlock() }

    func list(_ id: String) -> Components.Schemas.ShoppingList? { lock.lock(); defer { lock.unlock() }; return lists[id] }

    /// Changes an item as another person would: its version goes up by one.
    func mutate(listID: String, itemID: String, _ change: (inout Components.Schemas.ShoppingItem) -> Void) {
        lock.lock(); defer { lock.unlock() }
        guard var list = lists[listID], let index = list.items.firstIndex(where: { $0.id == itemID }) else { return }
        change(&list.items[index])
        list.items[index].version += 1
        lists[listID] = list
    }

    func deleteItem(listID: String, itemID: String) {
        lock.lock(); defer { lock.unlock() }
        lists[listID]?.items.removeAll { $0.id == itemID }
    }

    func deleteList(_ id: String) { lock.lock(); lists[id] = nil; lock.unlock() }

    func route(_ call: RoutingTransport.Call) async throws -> (status: Int, body: String) { try handle(call) }

    private func notFound() -> (status: Int, body: String) { (404, Fixtures.problem(404, code: "not_found")) }

    private func handle(_ call: RoutingTransport.Call) throws -> (status: Int, body: String) {
        lock.lock(); defer { lock.unlock() }
        if offline { throw URLError(.notConnectedToInternet) }
        if let status = forced[call.route] { return (status, status >= 400 ? Fixtures.problem(status, code: "forced") : "") }
        let parts = call.route.split(separator: " ", maxSplits: 1).map(String.init)
        let method = parts[0]
        let segments = parts[1].split(separator: "/").map(String.init)
        let body = (try? JSONSerialization.jsonObject(with: Data(call.body.utf8))) as? [String: Any] ?? [:]

        if method == "GET", segments == ["shopping-lists"] {
            return (200, ShoppingFixtures.page(lists.values.sorted { $0.id < $1.id }.map(ShoppingFixtures.summary)))
        }
        if method == "GET", segments == ["partner", "shopping-lists"] {
            return partnerLinked ? (200, ShoppingFixtures.page([])) : (404, Fixtures.problem(404, code: "partner_not_linked"))
        }
        if method == "POST", segments == ["shopping-lists"] {
            counter += 1
            let list = ShoppingFixtures.list(
                id: "srv-\(counter)", name: body["name"] as? String ?? "List", shared: body["shared_with_partner"] as? Bool ?? false
            )
            lists[list.id] = list
            return (201, Fixtures.json(list))
        }
        if method == "POST", segments == ["shopping-lists", "generate"] {
            counter += 1
            let id = (body["list_id"] as? String) ?? "srv-\(counter)"
            let rice = ShoppingFixtures.item(id: "gen-\(counter)", listID: id, name: "Rice", category: .grainsBread, quantity: 200, unit: .g)
            let list = ShoppingFixtures.list(
                id: id, name: body["name"] as? String ?? "Shopping", items: [rice],
                from: body["from"] as? String, to: body["to"] as? String
            )
            let existed = lists[id] != nil
            lists[id] = list
            return (existed ? 200 : 201, Fixtures.json(list))
        }

        guard segments.first == "shopping-lists", segments.count >= 2, var list = lists[segments[1]] else { return notFound() }
        let listID = segments[1]

        if segments.count == 2 {
            switch method {
            case "GET": return (200, Fixtures.json(list))
            case "PATCH":
                if let name = body["name"] as? String { list.name = name }
                if let shared = body["shared_with_partner"] as? Bool { list.sharedWithPartner = shared }
                lists[listID] = list
                return (200, Fixtures.json(list))
            case "DELETE":
                lists[listID] = nil
                return (204, "")
            default: break
            }
        }
        if segments.count == 3, segments[2] == "items", method == "POST" {
            counter += 1
            var item = ShoppingFixtures.item(
                id: "srv-\(counter)", listID: listID, name: body["name"] as? String ?? "Item",
                position: (list.items.map(\.position).max() ?? -1) + 1
            )
            if let raw = body["category"] as? String, let category = Components.Schemas.IngredientCategory(rawValue: raw) { item.category = category }
            if let raw = body["unit"] as? String { item.unit = .init(rawValue: raw) }
            item.quantity = body["quantity"] as? Double
            list.items.append(item)
            lists[listID] = list
            return (201, Fixtures.json(item))
        }
        if segments.count == 4, segments[2] == "items" {
            guard let index = list.items.firstIndex(where: { $0.id == segments[3] }) else { return notFound() }
            switch method {
            case "DELETE":
                list.items.remove(at: index)
                lists[listID] = list
                return (204, "")
            case "PATCH":
                if let version = body["version"] as? Int, version != list.items[index].version {
                    return (409, ShoppingFixtures.conflict(current: list.items[index]))
                }
                if let checked = body["checked"] as? Bool { list.items[index].checked = checked }
                if let name = body["name"] as? String { list.items[index].name = name }
                if let quantity = body["quantity"] as? Double { list.items[index].quantity = quantity }
                list.items[index].version += 1
                lists[listID] = list
                return (200, Fixtures.json(list.items[index]))
            default: break
            }
        }
        return (500, Fixtures.problem(500, code: "unrouted"))
    }
}
```

- [ ] **Step 2: Write the failing tests** — `ShoppingSyncEngineTests.swift`

```swift
import API
import Foundation
import Persistence
import Testing
@testable import Repositories

@Suite
struct ShoppingSyncEngineTests {
    private func make(_ server: ShoppingServer, gate: Gate? = nil) throws -> (ShoppingSyncEngine, ShoppingCache, RoutingTransport) {
        let transport = RoutingTransport { call in
            if let gate { await gate.wait() }
            return try await server.route(call)
        }
        let cache = CacheStore.makeShoppingCache(try CacheStore.inMemoryContainer())
        return (ShoppingSyncEngine(client: makeAuthlessClient(transport: transport), cache: cache), cache, transport)
    }

    private func waitForCalls(_ transport: RoutingTransport, _ count: Int) async {
        let deadline = ContinuousClock.now + .seconds(3)
        while await transport.calls.count < count, ContinuousClock.now < deadline { try? await Task.sleep(for: .milliseconds(5)) }
    }

    private func seeded() -> ShoppingServer {
        ShoppingServer([ShoppingFixtures.list(items: [ShoppingFixtures.item(id: "i1"), ShoppingFixtures.item(id: "i2", name: "Eggs", position: 1)])])
    }

    @Test("Replays in sequence order, rewrites a temp id after its add, and leaves the queue empty")
    func orderedReplay() async throws {
        let server = seeded()
        let (engine, cache, transport) = try make(server)
        await cache.store(try #require(server.list("l1")))
        let temp = ShoppingIntent.newTempID()
        await cache.enqueue(kind: .add, listID: "l1", itemID: temp, payload: .init(name: "Bread"))
        await cache.enqueue(kind: .check, listID: "l1", itemID: temp, payload: nil)
        await cache.enqueue(kind: .check, listID: "l1", itemID: "i1", payload: nil)

        await engine.drain()

        #expect(await transport.calls.map(\.route) == [
            "POST /shopping-lists/l1/items", "PATCH /shopping-lists/l1/items/srv-101", "PATCH /shopping-lists/l1/items/i1",
        ])
        #expect(await cache.intents().isEmpty)
        let items = try #require(await cache.list(id: "l1")).items
        let bread = try #require(items.first { $0.name == "Bread" })
        #expect(bread.id == "srv-101")
        #expect(bread.checked)
        #expect(items.first { $0.id == "i1" }?.checked == true)
        #expect(server.list("l1")?.items.first { $0.id == "i1" }?.checked == true)
    }

    @Test("A network failure keeps the rows and stops; the next drain finishes the job")
    func networkFailureKeepsRows() async throws {
        let server = seeded()
        let (engine, cache, _) = try make(server)
        await cache.store(try #require(server.list("l1")))
        await cache.enqueue(kind: .check, listID: "l1", itemID: "i1", payload: nil)
        await cache.enqueue(kind: .check, listID: "l1", itemID: "i2", payload: nil)
        server.setOffline(true)
        await engine.drain()
        #expect(await cache.intents().count == 2)
        server.setOffline(false)
        await engine.drain()
        #expect(await cache.intents().isEmpty)
        #expect(server.list("l1")?.items.allSatisfy(\.checked) == true)
    }

    @Test("A 5xx on one list blocks only that list; another list still drains")
    func perListBlocking() async throws {
        let server = ShoppingServer([
            ShoppingFixtures.list(id: "l1", items: [ShoppingFixtures.item(id: "i1", listID: "l1")]),
            ShoppingFixtures.list(id: "l2", items: [ShoppingFixtures.item(id: "j1", listID: "l2")]),
        ])
        server.force("PATCH /shopping-lists/l1/items/i1", status: 503)
        let (engine, cache, _) = try make(server)
        await cache.enqueue(kind: .check, listID: "l1", itemID: "i1", payload: nil)
        await cache.enqueue(kind: .check, listID: "l2", itemID: "j1", payload: nil)
        await engine.drain()
        #expect(await cache.intents().map(\.listID) == ["l1"])
        #expect(server.list("l2")?.items.first?.checked == true)
    }

    @Test("Review focus 3: a 404 (the partner deleted the item) drops the row and the item, silently")
    func deletedUnderneath() async throws {
        let server = seeded()
        let (engine, cache, _) = try make(server)
        await cache.store(try #require(server.list("l1")))
        await cache.enqueue(kind: .check, listID: "l1", itemID: "i1", payload: nil)
        server.deleteItem(listID: "l1", itemID: "i1")
        await engine.drain()
        #expect(await cache.intents().isEmpty)
        #expect(await cache.list(id: "l1")?.items.map(\.id) == ["i2"])
        #expect(await engine.takeNotice() == nil)
    }

    @Test("A remove of an item that is already gone counts as done")
    func removeAlreadyGone() async throws {
        let server = seeded()
        let (engine, cache, _) = try make(server)
        await cache.store(try #require(server.list("l1")))
        await cache.enqueue(kind: .remove, listID: "l1", itemID: "i1", payload: nil)
        server.deleteItem(listID: "l1", itemID: "i1")
        await engine.drain()
        #expect(await cache.intents().isEmpty)
        #expect(await cache.list(id: "l1")?.items.map(\.id) == ["i2"])
    }

    @Test("A refused add (400) drops the row and everything addressed to its temp id, and leaves a notice")
    func refusedAdd() async throws {
        let server = seeded()
        server.force("POST /shopping-lists/l1/items", status: 400)
        let (engine, cache, transport) = try make(server)
        let temp = ShoppingIntent.newTempID()
        await cache.enqueue(kind: .add, listID: "l1", itemID: temp, payload: .init(name: "Bread"))
        await cache.enqueue(kind: .check, listID: "l1", itemID: temp, payload: nil)
        await engine.drain()
        #expect(await cache.intents().isEmpty)
        #expect(await transport.calls.count == 1)
        #expect(await engine.takeNotice() == "A change couldn't be saved.")
        #expect(await engine.takeNotice() == nil)
    }

    @Test("Concurrent drains coalesce: the request is sent once")
    func coalesces() async throws {
        let server = seeded()
        let gate = Gate()
        let (engine, cache, transport) = try make(server, gate: gate)
        await cache.store(try #require(server.list("l1")))
        await cache.enqueue(kind: .check, listID: "l1", itemID: "i1", payload: nil)
        let first = Task { await engine.drain() }
        await waitForCalls(transport, 1)
        await engine.drain() // arrives while the first is mid-request: it must not start a second request
        await gate.release()
        await first.value
        #expect(await transport.calls("PATCH /shopping-lists/l1/items/i1").count == 1)
        #expect(await cache.intents().isEmpty)
    }

    @Test("changes() yields after queue steps, to every subscriber")
    func changesStream() async throws {
        let server = seeded()
        let (engine, cache, _) = try make(server)
        await cache.store(try #require(server.list("l1")))
        let a = await engine.changes()
        let b = await engine.changes()
        await cache.enqueue(kind: .check, listID: "l1", itemID: "i1", payload: nil)
        await engine.drain()
        var iteratorA = a.makeAsyncIterator()
        var iteratorB = b.makeAsyncIterator()
        #expect(await iteratorA.next() != nil)
        #expect(await iteratorB.next() != nil)
    }
}
```

- [ ] **Step 3: Run to verify it fails**

Run: `swift test --filter ShoppingSyncEngineTests`
Expected: FAIL to compile, "cannot find 'ShoppingSyncEngine' in scope".

- [ ] **Step 4: Implement `ShoppingSyncEngine.swift`**

```swift
import API
import Foundation
import Persistence

/// Drains the offline queue, oldest first, one request at a time. Triggers (launch, foreground, reconnect, after an
/// online change) all just call `drain()`; calls that arrive mid-drain coalesce into one more pass.
public actor ShoppingSyncEngine {
    public static let notice = "A change couldn't be saved."

    private enum Outcome {
        /// Sent (or already true on the server): delete the row.
        case done
        /// The server will never accept it (404, 400, 409): delete the row; `notify` says whether to tell the person.
        case drop(notify: Bool)
        /// Network, 429, 5xx, expired session: keep the row and stop draining this list for now.
        case retryLater
    }

    private let client: Client
    private let cache: ShoppingCache
    private var isDraining = false
    private var wantsAnotherPass = false
    private var pendingNotice: String?
    private var subscribers: [UUID: AsyncStream<Void>.Continuation] = [:]

    public init(client: Client, cache: ShoppingCache) {
        self.client = client
        self.cache = cache
    }

    /// One element after every queue step, so a view model knows to re-read the cache. One stream per caller.
    public func changes() -> AsyncStream<Void> {
        let id = UUID()
        let (stream, continuation) = AsyncStream<Void>.makeStream(bufferingPolicy: .bufferingNewest(1))
        subscribers[id] = continuation
        continuation.onTermination = { [weak self] _ in Task { await self?.unsubscribe(id) } }
        return stream
    }

    /// The text for a change that was refused, once.
    public func takeNotice() -> String? {
        defer { pendingNotice = nil }
        return pendingNotice
    }

    public func drain() async {
        if isDraining {
            wantsAnotherPass = true
            return
        }
        isDraining = true
        repeat {
            wantsAnotherPass = false
            await drainOnce()
        } while wantsAnotherPass && !Task.isCancelled
        isDraining = false
    }

    private func drainOnce() async {
        var blocked: Set<String> = []
        while !Task.isCancelled, let next = await cache.intents().first(where: { !blocked.contains($0.listID) }) {
            switch await send(next) {
            case .done:
                await cache.removeIntent(id: next.id)
            case .drop(let notify):
                await cache.removeIntent(id: next.id)
                // Its later changes are addressed to an item that will never exist.
                if next.kind == .add { await cache.dropIntents(itemID: next.itemID) }
                if notify { pendingNotice = Self.notice }
            case .retryLater:
                blocked.insert(next.listID)
            }
            publish()
        }
    }

    private func send(_ intent: ShoppingIntent) async -> Outcome {
        do {
            switch intent.kind {
            case .check, .uncheck:
                // A check addressed to an item whose add never synced cannot be sent.
                guard !intent.isTemp else { return .drop(notify: false) }
                let response = try await unwrapping {
                    try await client.updateShoppingItem(.init(
                        path: .init(id: intent.listID, itemId: intent.itemID),
                        body: .json(.init(checked: intent.kind == .check))
                    ))
                }
                switch response {
                case .ok(let ok):
                    await cache.applyItem(try ok.body.json, listID: intent.listID)
                    return .done
                case .notFound:
                    await cache.removeItem(id: intent.itemID, listID: intent.listID)
                    return .drop(notify: false)
                case .badRequest, .conflict: return .drop(notify: true)
                case .unauthorized, .tooManyRequests, .internalServerError: return .retryLater
                case .undocumented(let status, _): return status >= 500 ? .retryLater : .drop(notify: true)
                }
            case .add:
                guard let payload = intent.payload else { return .drop(notify: true) }
                let response = try await unwrapping {
                    try await client.createShoppingItem(.init(
                        path: .init(id: intent.listID),
                        body: .json(.init(
                            ingredientId: payload.ingredientID, name: payload.name, quantity: payload.quantity,
                            unit: payload.unit, category: payload.category
                        ))
                    ))
                }
                switch response {
                case .created(let created):
                    let item = try created.body.json
                    await cache.rewriteTempID(intent.itemID, to: item.id)
                    await cache.applyItem(item, listID: intent.listID)
                    return .done
                case .notFound: return .drop(notify: false)
                case .badRequest: return .drop(notify: true)
                case .unauthorized, .tooManyRequests, .internalServerError: return .retryLater
                case .undocumented(let status, _): return status >= 500 ? .retryLater : .drop(notify: true)
                }
            case .remove:
                guard !intent.isTemp else { return .drop(notify: false) }
                let response = try await unwrapping {
                    try await client.deleteShoppingItem(.init(path: .init(id: intent.listID, itemId: intent.itemID)))
                }
                switch response {
                case .noContent, .notFound:
                    await cache.removeItem(id: intent.itemID, listID: intent.listID)
                    return .done
                case .badRequest: return .drop(notify: true)
                case .unauthorized, .tooManyRequests, .internalServerError: return .retryLater
                case .undocumented(let status, _): return status >= 500 ? .retryLater : .drop(notify: true)
                }
            }
        } catch {
            // A transport failure (URLError) or an unreadable answer: try again on the next trigger.
            return .retryLater
        }
    }

    private func publish() {
        for continuation in subscribers.values { continuation.yield() }
    }

    private func unsubscribe(_ id: UUID) { subscribers[id] = nil }
}
```

- [ ] **Step 5: Run to verify it passes, then commit**

Run: `swift test --filter ShoppingSyncEngineTests`
Expected: PASS (8 tests). If `coalesces` is flaky because the second `drain()` arrives before the first request starts, the `waitForCalls` line is what orders them: keep it.

```bash
git add ios/MealPlannerKit/Sources/Repositories/Sync/ShoppingSyncEngine.swift ios/MealPlannerKit/Tests/MealPlannerKitTests/ShoppingServer.swift ios/MealPlannerKit/Tests/MealPlannerKitTests/ShoppingSyncEngineTests.swift
git commit -m "feat(ios): add the shopping sync engine that drains the offline queue

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 8: `ListEventStream`, `NetworkSwitch`, `NetworkMonitor`, Package.swift edges

**Files:**
- Modify: `ios/MealPlannerKit/Package.swift` (Repositories gains `HTTPTypes`; Features gains `Persistence`)
- Create: `ios/MealPlannerKit/Sources/Repositories/Sync/NetworkSwitch.swift`
- Create: `ios/MealPlannerKit/Sources/Repositories/Sync/NetworkMonitor.swift`
- Create: `ios/MealPlannerKit/Sources/Repositories/Sync/ListEventStream.swift`
- Test: `ios/MealPlannerKit/Tests/MealPlannerKitTests/ListEventStreamTests.swift`

**Interfaces:**
- Consumes: `SSEFrameParser`, `ListEvent` (Task 4).
- Produces:
  - `NetworkSwitch` (`isOffline`, `setOffline(_:)`) and `OfflineMiddleware(_ networkSwitch:)` (a `ClientMiddleware` that throws `URLError(.notConnectedToInternet)` while offline). UI-test builds only (Task 9 wires it).
  - `NetworkMonitor` (`updates: AsyncStream<Bool>`; `true` = a network path exists).
  - `ListEventStream(baseURL:accessToken:refreshToken:networkSwitch:open:)` with `events(listID:) -> AsyncThrowingStream<ListEvent, Error>`, `ListEventStream.StreamError` (`accessLost`, `unauthorized`, `unavailable(Int)`), `ListEventStream.Opener`.

- [ ] **Step 1: Edit `Package.swift`**

In the `Repositories` target, add `HTTPTypes`; in `Features`, add `"Persistence"`:

```swift
        .target(
            name: "Repositories",
            dependencies: [
                "API", "Persistence",
                .product(name: "OpenAPIRuntime", package: "swift-openapi-runtime"),
                .product(name: "HTTPTypes", package: "swift-http-types"),
            ]
        ),
        .target(name: "Features", dependencies: ["API", "Auth", "Persistence", "Repositories"]),
```

Run: `swift build`
Expected: builds (nothing uses the new edges yet).

- [ ] **Step 2: Write the failing tests** — `ListEventStreamTests.swift`

```swift
import Foundation
import Testing
@testable import Repositories

@Suite
struct ListEventStreamTests {
    private let changed = #"data: {"type":"item_changed","list_id":"l1","item_id":"i1","version":2}"#

    private func lines(_ values: [String], failing error: Error? = nil) -> ListEventStream.Lines {
        ListEventStream.Lines { continuation in
            for value in values { continuation.yield(value) }
            continuation.finish(throwing: error)
        }
    }

    private func make(
        token: Locked<String?> = Locked("t1"), refreshed: String? = nil, networkSwitch: NetworkSwitch? = nil,
        open: @escaping ListEventStream.Opener
    ) -> ListEventStream {
        ListEventStream(
            baseURL: URL(string: "http://localhost:8080/v1")!, accessToken: { token.value }, refreshToken: { refreshed },
            networkSwitch: networkSwitch, open: open
        )
    }

    private func collect(_ stream: AsyncThrowingStream<ListEvent, Error>) async throws -> [ListEvent] {
        var events: [ListEvent] = []
        for try await event in stream { events.append(event) }
        return events
    }

    @Test("Opens the list's events URL with the bearer and Accept, and yields parsed events until the server closes")
    func yieldsEvents() async throws {
        let requests = Locked<[URLRequest]>([])
        let stream = make { request in
            requests.mutate { $0.append(request) }
            return (200, lines([": keep-alive", changed, "", "event: list_changed", #"data: {"type":"list_changed","list_id":"l1"}"#, ""]))
        }
        let events = try await collect(stream.events(listID: "l1"))
        #expect(events.map(\.kind) == [.itemChanged, .listChanged])
        let request = try #require(requests.value.first)
        #expect(request.url?.absoluteString == "http://localhost:8080/v1/shopping-lists/l1/events")
        #expect(request.value(forHTTPHeaderField: "Authorization") == "Bearer t1")
        #expect(request.value(forHTTPHeaderField: "Accept") == "text/event-stream")
    }

    @Test("Review focus 5: a frame cut off before its blank line is not delivered, and junk lines do nothing")
    func truncated() async throws {
        let stream = make { _ in (200, lines(["garbage", changed, "", changed])) }
        #expect(try await collect(stream.events(listID: "l1")).count == 1)
    }

    @Test("A 404 means access is lost")
    func notFound() async throws {
        let stream = make { _ in (404, lines([])) }
        await #expect(throws: ListEventStream.StreamError.accessLost) { try await collect(stream.events(listID: "l1")) }
    }

    @Test("A 401 refreshes the token once and retries; a second 401 is an error")
    func unauthorized() async throws {
        let seen = Locked<[String]>([])
        let retried = make(refreshed: "t2") { request in
            seen.mutate { $0.append(request.value(forHTTPHeaderField: "Authorization") ?? "") }
            return seen.value.count == 1 ? (401, lines([])) : (200, lines([changed, ""]))
        }
        #expect(try await collect(retried.events(listID: "l1")).count == 1)
        #expect(seen.value == ["Bearer t1", "Bearer t2"])

        let stuck = make(refreshed: "t2") { _ in (401, lines([])) }
        await #expect(throws: ListEventStream.StreamError.unauthorized) { try await collect(stuck.events(listID: "l1")) }
    }

    @Test("Other statuses are 'unavailable', so the caller retries later")
    func unavailable() async throws {
        let stream = make { _ in (503, lines([])) }
        await #expect(throws: ListEventStream.StreamError.unavailable(503)) { try await collect(stream.events(listID: "l1")) }
    }

    @Test("A dropped connection surfaces as the error that dropped it")
    func dropped() async throws {
        let stream = make { _ in (200, lines([changed, ""], failing: URLError(.networkConnectionLost))) }
        await #expect(throws: URLError.self) { try await collect(stream.events(listID: "l1")) }
    }

    @Test("While the UI-test offline switch is on, nothing is opened")
    func offlineSwitch() async throws {
        let opened = Locked(0)
        let networkSwitch = NetworkSwitch()
        networkSwitch.setOffline(true)
        let stream = make(networkSwitch: networkSwitch) { _ in
            opened.mutate { $0 += 1 }
            return (200, lines([]))
        }
        await #expect(throws: URLError.self) { try await collect(stream.events(listID: "l1")) }
        #expect(opened.value == 0)
    }
}
```

- [ ] **Step 3: Run to verify it fails**

Run: `swift test --filter ListEventStreamTests`
Expected: FAIL to compile, "cannot find 'ListEventStream' in scope".

- [ ] **Step 4: Implement**

`NetworkSwitch.swift`:

```swift
import Foundation
import HTTPTypes
import OpenAPIRuntime

/// A switch the UI tests flip to cut the app off from the API (the simulator cannot be told to go offline mid-test).
/// Built only when the app is launched with `-uiTesting`; in a normal launch nothing creates one.
public final class NetworkSwitch: @unchecked Sendable {
    private let lock = NSLock()
    private var offline = false

    public init() {}

    public var isOffline: Bool {
        lock.lock(); defer { lock.unlock() }
        return offline
    }

    public func setOffline(_ value: Bool) {
        lock.lock(); offline = value; lock.unlock()
    }
}

/// Fails every request with `URLError(.notConnectedToInternet)` while the switch is offline, before it reaches the network.
public struct OfflineMiddleware: ClientMiddleware {
    private let networkSwitch: NetworkSwitch

    public init(_ networkSwitch: NetworkSwitch) { self.networkSwitch = networkSwitch }

    public func intercept(
        _ request: HTTPRequest,
        body: HTTPBody?,
        baseURL: URL,
        operationID: String,
        next: (HTTPRequest, HTTPBody?, URL) async throws -> (HTTPResponse, HTTPBody?)
    ) async throws -> (HTTPResponse, HTTPBody?) {
        if networkSwitch.isOffline { throw URLError(.notConnectedToInternet) }
        return try await next(request, body, baseURL)
    }
}
```

`NetworkMonitor.swift`:

```swift
import Foundation
import Network

/// Whether the device has a network path, as a stream: `true` when one appears. The sync engine drains the queue on `true`.
/// A path is not proof the API is reachable; a drain that fails just leaves the rows for the next trigger.
public final class NetworkMonitor: @unchecked Sendable {
    public let updates: AsyncStream<Bool>
    private let monitor = NWPathMonitor()

    public init() {
        let (stream, continuation) = AsyncStream<Bool>.makeStream(bufferingPolicy: .bufferingNewest(1))
        updates = stream
        monitor.pathUpdateHandler = { continuation.yield($0.status == .satisfied) }
        continuation.onTermination = { [monitor] _ in monitor.cancel() }
        monitor.start(queue: DispatchQueue(label: "NetworkMonitor"))
    }
}
```

`ListEventStream.swift`:

```swift
import Foundation

/// The client for `GET /shopping-lists/{id}/events`. iOS has no `EventSource`, so this reads the response bytes itself,
/// with the bearer in `Authorization` like every other route. It yields events until the server closes the stream; the
/// caller decides what to do next (refetch, reconnect with backoff).
public struct ListEventStream: Sendable {
    public typealias Lines = AsyncThrowingStream<String, Error>
    /// Opens the request and returns its status and its lines (empty lines included: they end a frame).
    public typealias Opener = @Sendable (URLRequest) async throws -> (status: Int, lines: Lines)

    public enum StreamError: Error, Equatable, Sendable {
        /// `404`: the list is gone, or the person can no longer see it.
        case accessLost
        case unauthorized
        case unavailable(Int)
    }

    private let baseURL: URL
    private let accessToken: @Sendable () async -> String?
    private let refreshToken: @Sendable () async -> String?
    private let networkSwitch: NetworkSwitch?
    private let open: Opener

    public init(
        baseURL: URL,
        accessToken: @escaping @Sendable () async -> String?,
        refreshToken: @escaping @Sendable () async -> String?,
        networkSwitch: NetworkSwitch? = nil,
        open: @escaping Opener = ListEventStream.urlSessionOpener
    ) {
        self.baseURL = baseURL
        self.accessToken = accessToken
        self.refreshToken = refreshToken
        self.networkSwitch = networkSwitch
        self.open = open
    }

    public func events(listID: String) -> AsyncThrowingStream<ListEvent, Error> {
        AsyncThrowingStream { continuation in
            let task = Task {
                do {
                    if networkSwitch?.isOffline == true { throw URLError(.notConnectedToInternet) }
                    var result = try await open(request(listID: listID, token: await accessToken()))
                    if result.status == 401, let fresh = await refreshToken() {
                        result = try await open(request(listID: listID, token: fresh))
                    }
                    switch result.status {
                    case 200: break
                    case 401: throw StreamError.unauthorized
                    case 404: throw StreamError.accessLost
                    default: throw StreamError.unavailable(result.status)
                    }
                    var parser = SSEFrameParser()
                    for try await line in result.lines {
                        if let event = parser.feed(line) { continuation.yield(event) }
                    }
                    continuation.finish()
                } catch {
                    continuation.finish(throwing: error)
                }
            }
            continuation.onTermination = { _ in task.cancel() }
        }
    }

    private func request(listID: String, token: String?) -> URLRequest {
        var request = URLRequest(url: baseURL.appending(path: "shopping-lists/\(listID)/events"))
        request.setValue("text/event-stream", forHTTPHeaderField: "Accept")
        if let token { request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
        return request
    }

    /// The real transport. `URLSession.AsyncBytes.lines` drops empty lines, and an empty line is what ends an SSE frame,
    /// so the bytes are split on `\n` by hand.
    public static let urlSessionOpener: Opener = { request in
        let (bytes, response) = try await URLSession.shared.bytes(for: request)
        let status = (response as? HTTPURLResponse)?.statusCode ?? 0
        let lines = Lines { continuation in
            let task = Task {
                do {
                    var line: [UInt8] = []
                    for try await byte in bytes {
                        if byte == 0x0A {
                            continuation.yield(String(decoding: line, as: UTF8.self))
                            line.removeAll(keepingCapacity: true)
                        } else {
                            line.append(byte)
                        }
                    }
                    if !line.isEmpty { continuation.yield(String(decoding: line, as: UTF8.self)) }
                    continuation.finish()
                } catch {
                    continuation.finish(throwing: error)
                }
            }
            continuation.onTermination = { _ in task.cancel() }
        }
        return (status, lines)
    }
}
```

- [ ] **Step 5: Run to verify it passes, then commit**

Run: `swift test --filter ListEventStreamTests` then `swift build`
Expected: PASS (7 tests); build succeeds on macOS (`Network` is available there).

```bash
git add ios/MealPlannerKit/Package.swift ios/MealPlannerKit/Sources/Repositories/Sync ios/MealPlannerKit/Tests/MealPlannerKitTests/ListEventStreamTests.swift
git commit -m "feat(ios): add the shopping SSE client, the network monitor and the UI-test offline switch

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 9: `ShoppingDependencies`, sign-out clearing, app wiring

**Files:**
- Create: `ios/MealPlannerKit/Sources/Features/Shopping/ShoppingDependencies.swift`
- Modify: `ios/MealPlannerKit/Sources/AppCore/ClearCaches.swift`
- Modify: `ios/MealPlannerKit/Sources/AppCore/RootView.swift`
- Modify: `ios/MealPlannerKit/Sources/AppCore/TabShellView.swift`
- Modify: `ios/MealPlannerKit/Sources/Features/Shopping/ShoppingView.swift` (init takes `dependencies`; real body arrives in Task 12)
- Test: modify `ios/MealPlannerKit/Tests/MealPlannerKitTests/ClearCachesTests.swift`

**Interfaces:**
- Consumes: `ShoppingListsRepository`, `ShoppingSyncEngine`, `ListEventStream`, `NetworkMonitor`, `NetworkSwitch`, `PartnerRepository`, `TokenRefresher` (`currentAccessToken()`, `refreshAccessToken()`), `AppState.session`.
- Produces: `ShoppingDependencies(shopping:sync:partner:events:monitor:networkSwitch:currentUserID:)` with `runSyncLoop() async`; `clearAllCaches(meals:plan:templates:shopping:)`.

- [ ] **Step 1: Update the failing test** — `ClearCachesTests.swift`

Extend the existing test (keep its current assertions) so it also seeds and checks shopping, and passes the new argument:

```swift
        let shoppingCache = CacheStore.makeShoppingCache(container)
        await shoppingCache.store(ShoppingFixtures.list())
        await shoppingCache.enqueue(kind: .check, listID: "l1", itemID: "i1", payload: nil)
```
(before the `clearAllCaches` call), add `shopping: ShoppingListsRepository(client: client, cache: shoppingCache)` to the call, and after it:

```swift
        #expect(await shoppingCache.list(id: "l1") == nil)
        #expect(await shoppingCache.intents().isEmpty) // the next user never drains this user's queue
```

Run: `swift test --filter ClearCachesTests`
Expected: FAIL to compile, "extra argument 'shopping' in call".

- [ ] **Step 2: Implement**

`ClearCaches.swift`:

```swift
import Repositories

/// Everything that must be emptied whenever the session ends, by any path (`AppState.clearCaches`), so a second user
/// on this device never sees the first user's meals, plan, templates or shopping lists, and never sends their queued changes.
func clearAllCaches(meals: MealsRepository, plan: PlanRepository, templates: TemplatesRepository, shopping: ShoppingListsRepository) async {
    await meals.clearCaches()
    await plan.clearCaches()
    await templates.clearCaches()
    await shopping.clearCaches()
}
```

`ShoppingDependencies.swift`:

```swift
import Repositories

/// What the Shopping tab needs, built once in `RootView` and handed down.
public struct ShoppingDependencies: Sendable {
    public let shopping: ShoppingListsRepository
    public let sync: ShoppingSyncEngine
    public let partner: PartnerRepository
    public let events: ListEventStream
    public let monitor: NetworkMonitor
    /// Non-nil only when the app was launched with `-uiTesting`: the UI tests flip it to go offline.
    public let networkSwitch: NetworkSwitch?
    public let currentUserID: @Sendable @MainActor () -> String?

    public init(
        shopping: ShoppingListsRepository, sync: ShoppingSyncEngine, partner: PartnerRepository, events: ListEventStream,
        monitor: NetworkMonitor, networkSwitch: NetworkSwitch?, currentUserID: @escaping @Sendable @MainActor () -> String?
    ) {
        self.shopping = shopping
        self.sync = sync
        self.partner = partner
        self.events = events
        self.monitor = monitor
        self.networkSwitch = networkSwitch
        self.currentUserID = currentUserID
    }

    /// Drains the queue now (launch), then again each time a network path appears. Run for as long as someone is signed in.
    public func runSyncLoop() async {
        await sync.drain()
        for await online in monitor.updates where online {
            await sync.drain()
        }
    }
}
```

`RootView.swift` changes:

1. New stored property `private let shoppingDependencies: ShoppingDependencies`.
2. In `init`, build the client with the offline middleware first when launched with `-uiTesting`:

```swift
        let networkSwitch: NetworkSwitch? = CommandLine.arguments.contains("-uiTesting") ? NetworkSwitch() : nil
        let client = makeClient(
            baseURL: baseURL,
            middlewares: (networkSwitch.map { [OfflineMiddleware($0) as any ClientMiddleware] } ?? []) + [BearerAuthMiddleware(refresher: refresher)]
        )
```
(this replaces the existing `makeClient(baseURL: baseURL, middlewares: [BearerAuthMiddleware(refresher: refresher)])` line; add `import OpenAPIRuntime` for `ClientMiddleware`.)

3. After `partnerRepository`:

```swift
        let shoppingCache = CacheStore.makeShoppingCache(container)
        let shoppingRepository = ShoppingListsRepository(client: client, cache: shoppingCache)
        let syncEngine = ShoppingSyncEngine(client: client, cache: shoppingCache)
```
4. The signed-in user's id reaches the overlay through a small box, because `appState` does not exist yet when the dependencies are built. Add at file scope in `RootView.swift`:

```swift
@MainActor
final class UserBox {
    var id: String?
}
```
and a stored property `private let userBox = UserBox()`. After `planDependencies`:

```swift
        let userBox = UserBox()
        self.userBox = userBox
        self.shoppingDependencies = ShoppingDependencies(
            shopping: shoppingRepository, sync: syncEngine, partner: partnerRepository,
            events: ListEventStream(
                baseURL: baseURL,
                accessToken: { await refresher.currentAccessToken() },
                refreshToken: { try? await refresher.refreshAccessToken() },
                networkSwitch: networkSwitch
            ),
            monitor: NetworkMonitor(), networkSwitch: networkSwitch,
            currentUserID: { userBox.id }
        )
```
(the `body` keeps the box current: step 7 below).
5. `clearCaches` closure becomes:

```swift
                await clearAllCaches(meals: mealsRepository, plan: planRepository, templates: templatesRepository, shopping: shoppingRepository)
```
6. `TabShellView(appState:mealsDependencies:planDependencies:shoppingDependencies:)`.
7. In `body`, on the `Group`: 

```swift
        .task(id: isSignedOut) {
            guard !isSignedOut else { return }
            await shoppingDependencies.runSyncLoop()
        }
        .onChange(of: appState.session) { _, session in
            if case .signedIn(let user) = session { userBox.id = user.id } else { userBox.id = nil }
        }
```
(`AppState.Session` is `Equatable`, so `onChange` works. `.unverified` has no user, so the overlay's `checkedBy` is `nil` until verification succeeds.)
and in the existing `scenePhase` handler, also `Task { await shoppingDependencies.sync.drain() }` when `phase == .active`.

`TabShellView.swift`: add `let shoppingDependencies: ShoppingDependencies` and replace `ShoppingView()` with `ShoppingView(dependencies: shoppingDependencies)`.

`ShoppingView.swift` (temporary, replaced in Task 12 — keep the build green):

```swift
import SwiftUI

public struct ShoppingView: View {
    private let dependencies: ShoppingDependencies
    public init(dependencies: ShoppingDependencies) { self.dependencies = dependencies }
    public var body: some View {
        Text("Shopping").navigationTitle("Shopping")
    }
}
```

- [ ] **Step 3: Run to verify it passes**

Run: `swift test --filter ClearCachesTests` then `swift build` then `swift test`
Expected: PASS; whole suite green (`AppStateTests` and the others do not construct `clearAllCaches`).

- [ ] **Step 4: Commit**

```bash
git add ios/MealPlannerKit
git commit -m "feat(ios): wire shopping dependencies, the sync loop and sign-out clearing

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 10: `ShoppingViewModel` (the lists screen)

**Files:**
- Create: `ios/MealPlannerKit/Sources/Features/Shopping/ShoppingViewModel.swift`
- Create: `ios/MealPlannerKit/Tests/MealPlannerKitTests/ShoppingTestSupport.swift` (the harness both view-model suites use)
- Test: `ios/MealPlannerKit/Tests/MealPlannerKitTests/ShoppingViewModelTests.swift`

**Interfaces:**
- Consumes: `ShoppingListsRepository`, `PartnerRepository`, `RangeValidation`, `LocalDay`, `ErrorText`, `MealScope`.
- Produces: `ShoppingViewModel(shopping:partner:day:)` (`@Observable @MainActor`) with state `scope`, `lists`, `isLoading`, `isStale`, `loadError`, `showsPartnerSegment`, `hasMore`, `isLoadingMore`, `alertMessage`; methods `appear()`, `load()`, `loadMore()`, `select(_:)`, `createList(name:shared:) -> Outcome`, `generate(from:to:name:) -> Outcome`, `defaultRange() -> (from: String, to: String)`; `ShoppingViewModel.Outcome` (`opened(String)`, `failed(String)`). Test helper `ShoppingHarness`.

- [ ] **Step 1: Write `ShoppingTestSupport.swift`**

```swift
import API
import Foundation
import Persistence
@testable import Repositories

/// A repository, a sync engine and a cache over one in-memory `ShoppingServer`, for the view-model suites.
@MainActor
struct ShoppingHarness {
    let server: ShoppingServer
    let transport: RoutingTransport
    let cache: ShoppingCache
    let repository: ShoppingListsRepository
    let engine: ShoppingSyncEngine
    let partner: PartnerRepository

    init(_ server: ShoppingServer = ShoppingServer(), partnerActive: Bool = true) throws {
        let transport = RoutingTransport { call in
            if call.route == "GET /partner" {
                return partnerActive
                    ? (200, Fixtures.json(Components.Schemas.Partnership(status: .active, displayName: "Sam", linkedAt: Fixtures.date)))
                    : (404, Fixtures.problem(404, code: "partner_not_linked"))
            }
            return try await server.route(call)
        }
        let client = makeAuthlessClient(transport: transport)
        let cache = CacheStore.makeShoppingCache(try CacheStore.inMemoryContainer())
        self.server = server
        self.transport = transport
        self.cache = cache
        self.repository = ShoppingListsRepository(client: client, cache: cache)
        self.engine = ShoppingSyncEngine(client: client, cache: cache)
        self.partner = PartnerRepository(client: client)
    }
}
```

- [ ] **Step 2: Write the failing tests** — `ShoppingViewModelTests.swift`

```swift
import API
import Foundation
import Persistence
import Testing
@testable import Features
@testable import Repositories

@MainActor
@Suite
struct ShoppingViewModelTests {
    private let utc = LocalDay(timeZone: TimeZone(identifier: "UTC")!, now: { Date(timeIntervalSince1970: 1_791_000_000) })

    private func make(_ harness: ShoppingHarness) -> ShoppingViewModel {
        ShoppingViewModel(shopping: harness.repository, partner: harness.partner, day: utc)
    }

    @Test("appear shows the cache at once, then the server's lists, and the Partner's segment when linked")
    func appear() async throws {
        let h = try ShoppingHarness(ShoppingServer([ShoppingFixtures.list(id: "a", name: "Week")]))
        await h.cache.replaceSummaries([ShoppingFixtures.summary(id: "old", name: "Old")], scope: .mine)
        let vm = make(h)
        await vm.appear()
        #expect(vm.lists.map(\.id) == ["a"])
        #expect(vm.showsPartnerSegment)
        #expect(!vm.isStale)
    }

    @Test("Without a partner the segment is hidden and a partner scope falls back to Mine")
    func noPartner() async throws {
        let h = try ShoppingHarness(ShoppingServer([ShoppingFixtures.list(id: "a")]), partnerActive: false)
        let vm = make(h)
        await vm.appear()
        #expect(!vm.showsPartnerSegment)
        #expect(vm.scope == .mine)
    }

    @Test("Offline with a cache keeps the lists and marks them stale; with no cache it shows an error")
    func offline() async throws {
        let server = ShoppingServer([ShoppingFixtures.list(id: "a")])
        let h = try ShoppingHarness(server)
        let vm = make(h)
        await vm.appear()
        server.setOffline(true)
        await vm.load()
        #expect(vm.lists.map(\.id) == ["a"])
        #expect(vm.isStale)
        #expect(vm.loadError == nil)

        let empty = try ShoppingHarness(ShoppingServer())
        empty.server.setOffline(true)
        let bare = make(empty)
        await bare.load()
        #expect(bare.lists.isEmpty)
        #expect(bare.loadError == "Can't reach the server. Check your connection and try again.")
    }

    @Test("Load more fetches the next page with its cursor and appends")
    func loadMore() async throws {
        let transport = RoutingTransport { call in
            if call.path.contains("cursor=c2") { return (200, ShoppingFixtures.page([ShoppingFixtures.summary(id: "c")])) }
            return (200, ShoppingFixtures.page([ShoppingFixtures.summary(id: "a"), ShoppingFixtures.summary(id: "b")], next: "c2"))
        }
        let client = makeAuthlessClient(transport: transport)
        let repo = ShoppingListsRepository(client: client, cache: CacheStore.makeShoppingCache(try CacheStore.inMemoryContainer()))
        let vm = ShoppingViewModel(shopping: repo, partner: PartnerRepository(client: client), day: utc)
        await vm.load()
        #expect(vm.hasMore)
        await vm.loadMore()
        #expect(vm.lists.map(\.id) == ["a", "b", "c"])
        #expect(!vm.hasMore)
    }

    @Test("createList trims, refuses a blank name without a request, and reports the new list")
    func createList() async throws {
        let h = try ShoppingHarness()
        let vm = make(h)
        #expect(await vm.createList(name: "   ", shared: false) == .failed("Give the list a name."))
        #expect(await h.transport.calls.isEmpty)
        let outcome = await vm.createList(name: "  Party  ", shared: true)
        guard case .opened(let id) = outcome else { Issue.record("expected opened, got \(outcome)"); return }
        #expect(h.server.list(id)?.name == "Party")
        #expect(h.server.list(id)?.sharedWithPartner == true)
        #expect(vm.lists.map(\.id) == [id])
    }

    @Test("generate refuses 93 days without a request, defaults to the next seven days, and explains an empty plan")
    func generate() async throws {
        let h = try ShoppingHarness()
        let vm = make(h)
        let range = vm.defaultRange()
        #expect(range.from == utc.today())
        #expect(range.to == utc.addDays(utc.today(), 6))
        #expect(await vm.generate(from: "2026-01-01", to: "2026-04-03", name: nil) == .failed("Pick at most 92 days."))
        #expect(await h.transport.calls.isEmpty)
        let ok = await vm.generate(from: range.from, to: range.to, name: " ")
        guard case .opened = ok else { Issue.record("expected opened, got \(ok)"); return }
        // A blank name is sent as no name, so the server picks its default.
        let generateBody = try #require(await h.transport.calls("POST /shopping-lists/generate").first).body
        #expect(!generateBody.contains("\"name\""))

        h.server.force("POST /shopping-lists/generate", status: 404)
        #expect(await vm.generate(from: range.from, to: range.to, name: nil) == .failed("Nothing is planned on those days."))
    }
}
```

- [ ] **Step 3: Run to verify it fails**

Run: `swift test --filter ShoppingViewModelTests`
Expected: FAIL to compile, "cannot find 'ShoppingViewModel' in scope".

- [ ] **Step 4: Implement `ShoppingViewModel.swift`**

```swift
import API
import Foundation
import Observation
import Repositories

/// The Shopping tab's list of lists (Mine / Partner's). Reads render the cache, await the refresh, re-read; a failed
/// refresh keeps what is shown and marks it stale. Creating and generating are online-only.
@Observable
@MainActor
public final class ShoppingViewModel {
    public enum Outcome: Equatable, Sendable {
        case opened(String)
        case failed(String)
    }

    public private(set) var scope: MealScope = .mine
    public private(set) var lists: [Components.Schemas.ShoppingListSummary] = []
    public private(set) var isLoading = false
    public private(set) var isStale = false
    public private(set) var loadError: String?
    public private(set) var showsPartnerSegment = false
    public private(set) var hasMore = false
    public private(set) var isLoadingMore = false
    public var alertMessage: String?

    @ObservationIgnored private let shopping: ShoppingListsRepository
    @ObservationIgnored private let partner: PartnerRepository
    @ObservationIgnored private let day: LocalDay
    @ObservationIgnored private var nextCursor: String?

    public init(shopping: ShoppingListsRepository, partner: PartnerRepository, day: LocalDay = LocalDay()) {
        self.shopping = shopping
        self.partner = partner
        self.day = day
    }

    /// The tab appears or the scene becomes active: re-read the partnership, then the lists.
    public func appear() async {
        await refreshPartnerSegment()
        await load()
    }

    public func load() async {
        let requested = scope
        isLoading = true
        defer { isLoading = false }
        lists = await shopping.cachedLists(requested)
        do {
            let next = try await shopping.refreshLists(requested, cursor: nil)
            guard scope == requested else { return }
            nextCursor = next
            hasMore = next != nil
            lists = await shopping.cachedLists(requested)
            isStale = false
            loadError = nil
        } catch ShoppingError.partnerNotLinked {
            showsPartnerSegment = false
            if scope == requested {
                scope = .mine
                await load()
            }
        } catch {
            guard scope == requested else { return }
            isStale = true
            loadError = lists.isEmpty ? ErrorText.message(for: error) : nil
        }
    }

    public func loadMore() async {
        guard let cursor = nextCursor, !isLoadingMore else { return }
        let requested = scope
        isLoadingMore = true
        defer { isLoadingMore = false }
        do {
            let next = try await shopping.refreshLists(requested, cursor: cursor)
            guard scope == requested else { return }
            nextCursor = next
            hasMore = next != nil
            lists = await shopping.cachedLists(requested)
        } catch {
            if scope == requested { alertMessage = ErrorText.message(for: error) }
        }
    }

    public func select(_ next: MealScope) async {
        guard next != scope else { return }
        scope = next
        await load()
    }

    /// The generate sheet's starting range: today and the six days after it.
    public func defaultRange() -> (from: String, to: String) {
        let today = day.today()
        return (today, day.addDays(today, 6))
    }

    public func createList(name: String, shared: Bool) async -> Outcome {
        let trimmed = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return .failed("Give the list a name.") }
        guard trimmed.count <= 200 else { return .failed("The name is too long.") }
        do {
            let list = try await shopping.createList(name: trimmed, shared: shared)
            await refreshMine()
            return .opened(list.id)
        } catch {
            return .failed(ErrorText.message(for: error))
        }
    }

    public func generate(from: String, to: String, name: String?) async -> Outcome {
        if let message = RangeValidation.error(from: from, to: to, day: day) { return .failed(message) }
        let trimmed = name?.trimmingCharacters(in: .whitespacesAndNewlines)
        do {
            let list = try await shopping.generate(from: from, to: to, name: (trimmed?.isEmpty ?? true) ? nil : trimmed, listID: nil)
            await refreshMine()
            return .opened(list.id)
        } catch ShoppingError.notFound {
            return .failed("Nothing is planned on those days.")
        } catch {
            return .failed(ErrorText.message(for: error))
        }
    }

    private func refreshMine() async {
        _ = try? await shopping.refreshLists(.mine, cursor: nil)
        if scope == .mine { lists = await shopping.cachedLists(.mine) }
    }

    /// Shown only while `GET /partner` says `active`. If the request fails (offline) it shows only when partner lists
    /// are already cached. Anything but active clears the partner scope.
    private func refreshPartnerSegment() async {
        do {
            let partnership = try await partner.status()
            showsPartnerSegment = partnership?.status == .active
            if !showsPartnerSegment { await shopping.clearPartnerLists() }
        } catch {
            showsPartnerSegment = !(await shopping.cachedLists(.partner)).isEmpty
        }
        if !showsPartnerSegment, scope == .partner { scope = .mine }
    }
}
```

- [ ] **Step 5: Run to verify it passes, then commit**

Run: `swift test --filter ShoppingViewModelTests`
Expected: PASS (6 tests).

```bash
git add ios/MealPlannerKit/Sources/Features/Shopping/ShoppingViewModel.swift ios/MealPlannerKit/Tests/MealPlannerKitTests/ShoppingTestSupport.swift ios/MealPlannerKit/Tests/MealPlannerKitTests/ShoppingViewModelTests.swift
git commit -m "feat(ios): add the Shopping lists view model

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 11: `ShoppingListViewModel` (one list: overlay, queue, edits, live updates)

**Files:**
- Create: `ios/MealPlannerKit/Sources/Features/Shopping/ShoppingListViewModel.swift`
- Test: `ios/MealPlannerKit/Tests/MealPlannerKitTests/ShoppingListViewModelTests.swift`

**Interfaces:**
- Consumes: `ShoppingListsRepository`, `ShoppingSyncEngine` (`drain`, `changes`, `takeNotice`), `PendingOverlay`, `ListEventReducer`, `ListEventStream.StreamError`, `ItemGrouping`, `ItemEdit`, `parseDecimal`, `ErrorText`, `ShoppingIntent`.
- Produces: `ShoppingListViewModel(listID:shopping:sync:events:userID:sleep:)` (`@Observable @MainActor`) with state `displayed: ShoppingList?`, `groups: [ItemGroup]`, `progress`, `isOwner`, `isLoading`, `isStale`, `accessLost`, `loadError`, `pendingCount`, `isSyncing`, `alertMessage`; methods `run()`, `load()`, `reload()`, `toggle(_:)`, `quickAdd(_:) -> Bool`, `remove(_:)`, `edit(_:name:quantity:unit:category:) -> EditOutcome`, `rename(_:)`, `setShared(_:)`, `delete() -> Bool`, `regenerate()`, `handle(_ event:)`, `runLiveUpdates()`; `EditOutcome` (`saved`, `conflict(ShoppingItem)`, `failed(String)`); `EventSource` = `@Sendable (String) -> AsyncThrowingStream<ListEvent, Error>`.

- [ ] **Step 1: Write the failing tests** — `ShoppingListViewModelTests.swift`

```swift
import API
import Foundation
import Persistence
import Testing
@testable import Features
@testable import Repositories

typealias ListEventSource = ShoppingListViewModel.EventSource

@MainActor
@Suite
struct ShoppingListViewModelTests {
    private static func quiet(_: String) -> AsyncThrowingStream<ListEvent, Error> { AsyncThrowingStream { $0.finish() } }

    private func seeded() -> ShoppingServer {
        ShoppingServer([ShoppingFixtures.list(items: [
            ShoppingFixtures.item(id: "i1", name: "Milk", category: .dairyEggs, position: 0),
            ShoppingFixtures.item(id: "i2", name: "Eggs", category: .dairyEggs, position: 1),
        ])])
    }

    private func make(
        _ h: ShoppingHarness, events: @escaping ListEventSource = { ShoppingListViewModelTests.quiet($0) },
        sleep: @escaping @Sendable (Duration) async throws -> Void = { _ in }
    ) -> ShoppingListViewModel {
        ShoppingListViewModel(listID: "l1", shopping: h.repository, sync: h.engine, events: events, userID: { "u1" }, sleep: sleep)
    }

    private func item(_ vm: ShoppingListViewModel, _ id: String) -> Components.Schemas.ShoppingItem? {
        vm.displayed?.items.first { $0.id == id }
    }

    @Test("load shows the server's list grouped by aisle with its progress")
    func load() async throws {
        let h = try ShoppingHarness(seeded())
        let vm = make(h)
        await vm.load()
        #expect(vm.groups.map(\.category) == [.dairyEggs])
        #expect(vm.progress == (done: 0, total: 2))
        #expect(vm.isOwner)
    }

    @Test("Online, a toggle is sent at once and the queue ends empty")
    func toggleOnline() async throws {
        let h = try ShoppingHarness(seeded())
        let vm = make(h)
        await vm.load()
        await vm.toggle(try #require(item(vm, "i1")))
        #expect(item(vm, "i1")?.checked == true)
        #expect(vm.pendingCount == 0)
        #expect(h.server.list("l1")?.items.first?.checked == true)
    }

    @Test("Offline, a toggle looks done at once, queues one row, and syncs when the connection returns")
    func toggleOffline() async throws {
        let h = try ShoppingHarness(seeded())
        let vm = make(h)
        await vm.load()
        h.server.setOffline(true)
        await vm.toggle(try #require(item(vm, "i1")))
        #expect(item(vm, "i1")?.checked == true)
        #expect(vm.isSyncing)
        #expect(h.server.list("l1")?.items.first?.checked == false)

        h.server.setOffline(false)
        await h.engine.drain()
        await vm.reload()
        #expect(!vm.isSyncing)
        #expect(h.server.list("l1")?.items.first?.checked == true)
    }

    @Test("Review focus 1: two quick taps offline leave one row and the last state on screen")
    func doubleToggleOffline() async throws {
        let h = try ShoppingHarness(seeded())
        let vm = make(h)
        await vm.load()
        h.server.setOffline(true)
        await vm.toggle(try #require(item(vm, "i1"))) // check
        await vm.toggle(try #require(item(vm, "i1"))) // uncheck
        await vm.toggle(try #require(item(vm, "i1"))) // check
        #expect(vm.pendingCount == 1)
        #expect(item(vm, "i1")?.checked == true)
    }

    @Test("An offline quick-add appears at once as pending, then becomes the server's item")
    func quickAddOffline() async throws {
        let h = try ShoppingHarness(seeded())
        let vm = make(h)
        await vm.load()
        h.server.setOffline(true)
        #expect(await vm.quickAdd("  Bread  "))
        let temp = try #require(vm.displayed?.items.last)
        #expect(temp.name == "Bread")
        #expect(temp.id.hasPrefix(ShoppingIntent.tempPrefix))

        h.server.setOffline(false)
        await h.engine.drain()
        await vm.reload()
        #expect(!vm.isSyncing)
        #expect(vm.displayed?.items.last?.id == "srv-101")
    }

    @Test("A blank quick-add is refused without queueing anything")
    func quickAddBlank() async throws {
        let h = try ShoppingHarness(seeded())
        let vm = make(h)
        await vm.load()
        #expect(await vm.quickAdd("   ") == false)
        #expect(vm.pendingCount == 0)
    }

    @Test("Removing an item that was never synced cancels its add and sends nothing")
    func removeUnsynced() async throws {
        let h = try ShoppingHarness(seeded())
        let vm = make(h)
        await vm.load()
        h.server.setOffline(true)
        _ = await vm.quickAdd("Bread")
        await vm.remove(try #require(vm.displayed?.items.last))
        #expect(vm.pendingCount == 0)
        #expect(vm.displayed?.items.count == 2)
    }

    @Test("An edit on a stale version is a conflict that carries the server's item, and the screen shows it")
    func editConflict() async throws {
        let h = try ShoppingHarness(seeded())
        let vm = make(h)
        await vm.load()
        let opened = try #require(item(vm, "i1"))
        h.server.mutate(listID: "l1", itemID: "i1") { $0.name = "Oat milk" }
        let outcome = await vm.edit(opened, name: "Mine", quantity: "", unit: nil, category: .dairyEggs)
        guard case .conflict(let current) = outcome else { Issue.record("expected conflict, got \(outcome)"); return }
        #expect(current.name == "Oat milk")
        #expect(item(vm, "i1")?.name == "Oat milk")
    }

    @Test("An edit saves a comma decimal; junk and blank names are refused before any request")
    func editValidation() async throws {
        let h = try ShoppingHarness(seeded())
        let vm = make(h)
        await vm.load()
        let milk = try #require(item(vm, "i1"))
        let before = await h.transport.calls.count
        #expect(await vm.edit(milk, name: " ", quantity: "", unit: nil, category: .dairyEggs) == .failed("Give the item a name."))
        #expect(await vm.edit(milk, name: "Milk", quantity: "abc", unit: nil, category: .dairyEggs) == .failed("Enter a valid quantity."))
        #expect(await h.transport.calls.count == before)
        #expect(await vm.edit(milk, name: "Milk", quantity: "1,5", unit: .ml, category: .dairyEggs) == .saved)
        #expect(item(vm, "i1")?.quantity == 1.5)
    }

    // MARK: Live updates

    @Test("An event at or below the cached version is ignored; a newer one refetches")
    func staleAndNewer() async throws {
        let h = try ShoppingHarness(seeded())
        let vm = make(h)
        await vm.load()
        let gets = { await h.transport.calls("GET /shopping-lists/l1").count }
        let base = await gets()
        await vm.handle(ListEvent(kind: .itemChanged, listID: "l1", itemID: "i1", version: 1))
        #expect(await gets() == base)
        h.server.mutate(listID: "l1", itemID: "i1") { $0.checked = true }
        await vm.handle(ListEvent(kind: .itemChanged, listID: "l1", itemID: "i1", version: 2))
        #expect(await gets() == base + 1)
        #expect(item(vm, "i1")?.checked == true)
    }

    @Test("A refetch keeps a pending change on screen (the overlay survives the echo)")
    func overlaySurvivesRefetch() async throws {
        let h = try ShoppingHarness(seeded())
        let vm = make(h)
        await vm.load()
        h.server.setOffline(true)
        await vm.toggle(try #require(item(vm, "i1")))
        h.server.setOffline(false)
        h.server.mutate(listID: "l1", itemID: "i2") { $0.checked = true } // the partner ticked Eggs meanwhile
        await vm.handle(ListEvent(kind: .listChanged, listID: "l1"))
        #expect(item(vm, "i1")?.checked == true) // still pending, still shown
        #expect(item(vm, "i2")?.checked == true) // and the partner's change arrived
    }

    @Test("item_deleted removes the item")
    func itemDeleted() async throws {
        let h = try ShoppingHarness(seeded())
        let vm = make(h)
        await vm.load()
        await vm.handle(ListEvent(kind: .itemDeleted, listID: "l1", itemID: "i1", version: 1))
        #expect(vm.displayed?.items.map(\.id) == ["i2"])
    }

    @Test("Review focus 4: list_deleted means access lost even though the list is cached, and pending changes are dropped")
    func listDeleted() async throws {
        let h = try ShoppingHarness(seeded())
        let vm = make(h)
        await vm.load()
        h.server.setOffline(true)
        await vm.toggle(try #require(item(vm, "i1")))
        await vm.handle(ListEvent(kind: .listDeleted, listID: "l1"))
        #expect(vm.accessLost)
        #expect(vm.displayed == nil)
        #expect(await h.repository.pendingIntents(listID: "l1").isEmpty)
    }

    @Test("A 404 on refetch is access lost, even with items cached")
    func refetch404() async throws {
        let h = try ShoppingHarness(seeded())
        let vm = make(h)
        await vm.load()
        h.server.deleteList("l1")
        await vm.handle(ListEvent(kind: .listChanged, listID: "l1"))
        #expect(vm.accessLost)
    }

    @Test("When the stream closes it waits, refetches and reconnects; a 404 on the stream ends it with access lost")
    func reconnect() async throws {
        let h = try ShoppingHarness(seeded())
        let opened = Locked(0)
        let sleeper = TestSleeper()
        let vm = make(
            h,
            events: { _ in
                opened.mutate { $0 += 1 }
                if opened.value == 1 { return AsyncThrowingStream { $0.finish() } }
                return AsyncThrowingStream { $0.finish(throwing: ListEventStream.StreamError.accessLost) }
            },
            sleep: { _ in try await sleeper.sleep() }
        )
        await vm.load()
        let task = Task { await vm.runLiveUpdates() }
        await sleeper.fire() // the first stream ended; release the backoff
        await task.value
        #expect(opened.value == 2)
        #expect(vm.accessLost)
    }

    @Test("A refused queued change surfaces the engine's notice once")
    func notice() async throws {
        let h = try ShoppingHarness(seeded())
        h.server.force("POST /shopping-lists/l1/items", status: 400)
        let vm = make(h)
        await vm.load()
        _ = await vm.quickAdd("Bread")
        #expect(vm.alertMessage == "A change couldn't be saved.")
    }
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `swift test --filter ShoppingListViewModelTests`
Expected: FAIL to compile, "cannot find 'ShoppingListViewModel' in scope".

- [ ] **Step 3: Implement `ShoppingListViewModel.swift`**

```swift
import API
import Foundation
import Observation
import Persistence
import Repositories

/// One shopping list. The screen shows `PendingOverlay(snapshot, pending changes)`. Item check/add/remove only enqueue
/// and then ask the sync engine to drain, so they work offline; every other write goes straight to the API.
@Observable
@MainActor
public final class ShoppingListViewModel {
    public enum EditOutcome: Equatable, Sendable {
        case saved
        /// Someone else changed the item first: this is the server's current version, already on screen.
        case conflict(Components.Schemas.ShoppingItem)
        case failed(String)
    }

    public typealias EventSource = @Sendable (String) -> AsyncThrowingStream<ListEvent, Error>

    public let listID: String
    public private(set) var displayed: Components.Schemas.ShoppingList?
    public private(set) var isLoading = false
    public private(set) var isStale = false
    /// The list is gone or no longer visible: the screen says so, even while items are cached.
    public private(set) var accessLost = false
    public private(set) var loadError: String?
    public private(set) var pendingCount = 0
    public var alertMessage: String?

    public var isSyncing: Bool { pendingCount > 0 }
    public var isOwner: Bool { displayed?.isOwner ?? false }
    public var groups: [ItemGroup] { displayed.map { ItemGrouping.groups($0.items) } ?? [] }
    public var progress: (done: Int, total: Int) { displayed.map { ItemGrouping.progress($0.items) } ?? (0, 0) }

    @ObservationIgnored private let shopping: ShoppingListsRepository
    @ObservationIgnored private let sync: ShoppingSyncEngine
    @ObservationIgnored private let events: EventSource
    @ObservationIgnored private let userID: @MainActor () -> String?
    @ObservationIgnored private let sleep: @Sendable (Duration) async throws -> Void
    @ObservationIgnored private var snapshot: Components.Schemas.ShoppingList?

    public init(
        listID: String, shopping: ShoppingListsRepository, sync: ShoppingSyncEngine, events: @escaping EventSource,
        userID: @escaping @MainActor () -> String?,
        sleep: @escaping @Sendable (Duration) async throws -> Void = { try await Task.sleep(for: $0) }
    ) {
        self.listID = listID
        self.shopping = shopping
        self.sync = sync
        self.events = events
        self.userID = userID
        self.sleep = sleep
    }

    /// The screen's lifetime: load, then keep the screen in step with the queue and with the partner's changes.
    public func run() async {
        await load()
        guard !accessLost else { return }
        await withTaskGroup(of: Void.self) { group in
            group.addTask { await self.observeSync() }
            group.addTask { await self.runLiveUpdates() }
        }
    }

    public func load() async {
        isLoading = true
        defer { isLoading = false }
        await reload()
        await refresh()
    }

    /// Re-reads the cache (the snapshot and the pending changes) and lays one over the other.
    public func reload() async {
        snapshot = await shopping.cachedList(id: listID)
        let intents = await shopping.pendingIntents(listID: listID)
        pendingCount = intents.count
        displayed = snapshot.map { PendingOverlay.apply($0, intents: intents, userID: userID()) }
    }

    private func refresh() async {
        do {
            try await shopping.refreshList(id: listID)
            isStale = false
            loadError = nil
        } catch ShoppingError.notFound {
            accessLost = true
        } catch {
            isStale = true
            loadError = snapshot == nil ? ErrorText.message(for: error) : nil
        }
        await reload()
    }

    // MARK: Offline-able changes

    public func toggle(_ item: Components.Schemas.ShoppingItem) async {
        await shopping.setChecked(!item.checked, itemID: item.id, listID: listID)
        await settle()
    }

    /// Returns false (and queues nothing) for a blank or over-long name.
    @discardableResult
    public func quickAdd(_ name: String) async -> Bool {
        let trimmed = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty, trimmed.count <= 200 else { return false }
        await shopping.addItem(.init(name: trimmed), listID: listID)
        await settle()
        return true
    }

    public func remove(_ item: Components.Schemas.ShoppingItem) async {
        await shopping.removeItem(itemID: item.id, listID: listID)
        await settle()
    }

    /// Show the change, ask the engine to send it, show the result.
    private func settle() async {
        await reload()
        await sync.drain()
        await reload()
        if let notice = await sync.takeNotice() { alertMessage = notice }
    }

    private func observeSync() async {
        let changes = await sync.changes()
        for await _ in changes {
            await reload()
            if let notice = await sync.takeNotice() { alertMessage = notice }
        }
    }

    // MARK: Online-only changes

    /// Edits against the version the sheet was opened on. Needs a connection.
    public func edit(
        _ item: Components.Schemas.ShoppingItem, name: String, quantity: String, unit: Components.Schemas.Unit?,
        category: Components.Schemas.IngredientCategory
    ) async -> EditOutcome {
        let trimmed = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return .failed("Give the item a name.") }
        guard trimmed.count <= 200 else { return .failed("The name is too long.") }
        let amount: Double?
        switch parseDecimal(quantity) {
        case .blank: amount = nil
        case .value(let value):
            guard value <= 100_000 else { return .failed("Quantity is too large.") }
            amount = value
        case .invalid: return .failed("Enter a valid quantity.")
        }
        do {
            try await shopping.editItem(
                listID: listID, itemID: item.id, version: item.version,
                edit: ItemEdit(name: trimmed, quantity: amount, unit: unit, category: category)
            )
            await reload()
            return .saved
        } catch ShoppingError.versionConflict(let current) {
            await reload()
            return .conflict(current)
        } catch ShoppingError.notFound {
            await refresh()
            return .failed("This item was removed.")
        } catch {
            return .failed(ErrorText.message(for: error))
        }
    }

    public func rename(_ name: String) async {
        let trimmed = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { alertMessage = "Give the list a name."; return }
        await write { _ = try await self.shopping.updateList(id: self.listID, name: trimmed, shared: nil) }
    }

    public func setShared(_ shared: Bool) async {
        await write { _ = try await self.shopping.updateList(id: self.listID, name: nil, shared: shared) }
    }

    /// Rebuilds a generated list from the plan dates it came from.
    public func regenerate() async {
        guard let from = snapshot?.sourceFrom, let to = snapshot?.sourceTo else { return }
        await write { _ = try await self.shopping.generate(from: from, to: to, name: nil, listID: self.listID) }
    }

    /// True once the list is gone, so the screen can close.
    public func delete() async -> Bool {
        do {
            try await shopping.deleteList(id: listID)
            accessLost = true
            await reload()
            return true
        } catch {
            alertMessage = ErrorText.message(for: error)
            return false
        }
    }

    private func write(_ body: () async throws -> Void) async {
        do {
            try await body()
            await reload()
        } catch ShoppingError.notFound {
            await markAccessLost()
        } catch {
            alertMessage = ErrorText.message(for: error)
        }
    }

    // MARK: Live updates

    public func handle(_ event: ListEvent) async {
        switch ListEventReducer.action(for: event, cached: snapshot) {
        case .ignore: break
        case .refetch: await refresh()
        case .removeItem(let id):
            await shopping.removeCachedItem(itemID: id, listID: listID)
            await reload()
        case .accessLost: await markAccessLost()
        }
    }

    private func markAccessLost() async {
        await shopping.dropList(id: listID)
        accessLost = true
        await reload()
    }

    /// Reads the list's event stream; when it closes or fails, waits (1 s, doubling to 30 s), refetches and reconnects.
    /// Ends when the list is gone or the task is cancelled.
    public func runLiveUpdates() async {
        var delay = Duration.seconds(1)
        var reconnecting = false // the first connection follows `load()`, which has just refreshed
        while !Task.isCancelled, !accessLost {
            if reconnecting {
                await refresh()
                if accessLost { return }
            }
            reconnecting = true
            do {
                for try await event in events(listID) {
                    delay = .seconds(1)
                    await handle(event)
                    if accessLost { return }
                }
            } catch ListEventStream.StreamError.accessLost {
                await markAccessLost()
                return
            } catch is CancellationError {
                return
            } catch {
                // Offline, dropped, unavailable: fall through to the backoff and try again.
            }
            do { try await sleep(delay) } catch { return }
            delay = min(delay * 2, .seconds(30))
        }
    }
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `swift test --filter ShoppingListViewModelTests` then the whole `swift test`.
Expected: PASS (16 tests); whole suite green. If `reconnect` hangs, `TestSleeper.fire()` waits up to 3 s for a waiter: the first stream must finish immediately and the loop must reach `sleep`.

- [ ] **Step 5: Commit**

```bash
git add ios/MealPlannerKit/Sources/Features/Shopping/ShoppingListViewModel.swift ios/MealPlannerKit/Tests/MealPlannerKitTests/ShoppingListViewModelTests.swift
git commit -m "feat(ios): add the shopping list view model with overlay, queue, edits and live updates

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 12: The Shopping screens

**Files:**
- Replace: `ios/MealPlannerKit/Sources/Features/Shopping/ShoppingView.swift`
- Create: `ShoppingListView.swift`, `ShoppingItemRow.swift`, `QuickAddField.swift`, `EditItemSheet.swift`, `GenerateListSheet.swift`, `NewListSheet.swift` (all in `ios/MealPlannerKit/Sources/Features/Shopping/`)

**Interfaces:**
- Consumes: `ShoppingDependencies`, `ShoppingViewModel`, `ShoppingListViewModel`, `ItemGrouping`, `ItemGroup`, `IngredientCategory.label`, `LocalDay`, `decimalKeyboard()` / `inlineNavigationTitle()` (`ViewHelpers.swift`).
- Produces (accessibility identifiers the UI test in Task 13 relies on): `addListMenu`, `newListButton`, `generateListButton`, `generateNameField`, `generateSubmitButton`, `newListNameField`, `newListSubmitButton`, `shoppingListRow-<name>`, `shoppingItem-<name>` (value `bought` / `to buy`), `quickAddField`, `syncingBadge`, `debugOfflineToggle`, `listUnavailable`, `editItemSaveButton`.

There is no unit test for SwiftUI views in this codebase (the logic is in the tested view models); the gate for this task is the build, plus the UI test in Task 13.

- [ ] **Step 1: `ShoppingView.swift`**

```swift
import API
import Repositories
import SwiftUI

public struct ShoppingView: View {
    @State private var viewModel: ShoppingViewModel
    @State private var opened: String?
    @State private var showingNew = false
    @State private var showingGenerate = false
    private let dependencies: ShoppingDependencies
    private let day = LocalDay()
    @Environment(\.scenePhase) private var scenePhase

    public init(dependencies: ShoppingDependencies) {
        self.dependencies = dependencies
        _viewModel = State(initialValue: ShoppingViewModel(shopping: dependencies.shopping, partner: dependencies.partner))
    }

    public var body: some View {
        @Bindable var viewModel = viewModel
        List {
            if viewModel.showsPartnerSegment {
                Picker(
                    "Lists",
                    selection: Binding(get: { viewModel.scope }, set: { next in Task { await viewModel.select(next) } })
                ) {
                    Text("Mine").tag(MealScope.mine)
                    Text("Partner's").tag(MealScope.partner)
                }
                .pickerStyle(.segmented)
                .listRowBackground(Color.clear)
                .accessibilityIdentifier("shoppingScopePicker")
            }
            if viewModel.isStale {
                Label("Offline: showing saved lists", systemImage: "wifi.slash")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            content
        }
        .navigationTitle("Shopping")
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Menu {
                    Button("New list") { showingNew = true }.accessibilityIdentifier("newListButton")
                    Button("Generate from plan") { showingGenerate = true }.accessibilityIdentifier("generateListButton")
                } label: {
                    Label("Add list", systemImage: "plus")
                }
                .accessibilityIdentifier("addListMenu")
            }
        }
        .refreshable { await viewModel.appear() }
        .task { await viewModel.appear() }
        .onChange(of: scenePhase) { _, phase in
            if phase == .active { Task { await viewModel.appear() } }
        }
        .navigationDestination(item: $opened) { id in
            ShoppingListView(dependencies: dependencies, listID: id).id(id)
        }
        .sheet(isPresented: $showingNew) {
            NewListSheet(onSubmit: { name, shared in await viewModel.createList(name: name, shared: shared) }) { id in
                showingNew = false
                opened = id
            }
        }
        .sheet(isPresented: $showingGenerate) {
            GenerateListSheet(
                day: day, range: viewModel.defaultRange(),
                onSubmit: { from, to, name in await viewModel.generate(from: from, to: to, name: name) }
            ) { id in
                showingGenerate = false
                opened = id
            }
        }
        .alert(
            "Couldn't load more",
            isPresented: Binding(get: { viewModel.alertMessage != nil }, set: { if !$0 { viewModel.alertMessage = nil } })
        ) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(viewModel.alertMessage ?? "")
        }
    }

    @ViewBuilder
    private var content: some View {
        if viewModel.lists.isEmpty {
            if viewModel.isLoading {
                ProgressView()
            } else if let error = viewModel.loadError {
                VStack(alignment: .leading, spacing: 8) {
                    Text(error)
                    Button("Try again") { Task { await viewModel.load() } }
                }
            } else {
                Text(viewModel.scope == .mine
                    ? "No shopping lists yet. Generate one from your plan, or start an empty one."
                    : "Lists your partner shares with you show up here.")
                    .foregroundStyle(.secondary)
            }
        } else {
            ForEach(viewModel.lists, id: \.id) { list in
                Button { opened = list.id } label: { row(list) }
                    .accessibilityIdentifier("shoppingListRow-\(list.name)")
            }
            if viewModel.hasMore {
                Button(viewModel.isLoadingMore ? "Loading…" : "Load more") { Task { await viewModel.loadMore() } }
                    .disabled(viewModel.isLoadingMore)
                    .accessibilityIdentifier("loadMoreListsButton")
            }
        }
    }

    private func row(_ list: Components.Schemas.ShoppingListSummary) -> some View {
        HStack {
            VStack(alignment: .leading) {
                Text(list.name).foregroundStyle(.primary)
                Text(subtitle(list)).font(.footnote).foregroundStyle(.secondary)
            }
            Spacer()
            if viewModel.scope == .partner {
                Text("Shared by your partner").font(.caption).foregroundStyle(.secondary)
            } else if list.sharedWithPartner {
                Text("Shared").font(.caption).foregroundStyle(.secondary)
            }
        }
    }

    private func subtitle(_ list: Components.Schemas.ShoppingListSummary) -> String {
        guard let from = list.sourceFrom, let to = list.sourceTo else { return "Your own list" }
        return "From your plan · \(day.heading(from)) – \(day.heading(to))"
    }
}
```

- [ ] **Step 2: `ShoppingItemRow.swift` and `QuickAddField.swift`**

```swift
import API
import Persistence
import SwiftUI

struct ShoppingItemRow: View {
    let item: Components.Schemas.ShoppingItem
    let onToggle: () -> Void
    let onEdit: () -> Void
    let onRemove: () -> Void

    /// An item that exists only on this device: it syncs soon; until then it can be removed but not edited.
    private var isPending: Bool { item.id.hasPrefix(ShoppingIntent.tempPrefix) }

    var body: some View {
        HStack(spacing: 12) {
            Button(action: onToggle) {
                HStack(spacing: 12) {
                    Image(systemName: item.checked ? "checkmark.circle.fill" : "circle")
                        .foregroundStyle(item.checked ? Color.accentColor : Color.secondary)
                    VStack(alignment: .leading) {
                        Text(item.name).strikethrough(item.checked).foregroundStyle(item.checked ? .secondary : .primary)
                        let quantity = ItemGrouping.quantityText(item)
                        if !quantity.isEmpty { Text(quantity).font(.footnote).foregroundStyle(.secondary) }
                    }
                    Spacer()
                    if isPending { ProgressView().controlSize(.small) }
                }
            }
            .buttonStyle(.plain)
            .accessibilityIdentifier("shoppingItem-\(item.name)")
            // "checked" would match "unchecked" in a UI-test predicate, so the words differ.
            .accessibilityValue(item.checked ? "bought" : "to buy")

            Menu {
                Button("Edit", action: onEdit).disabled(isPending)
                Button("Remove", role: .destructive, action: onRemove)
            } label: {
                Image(systemName: "ellipsis.circle")
            }
            .accessibilityLabel("More actions for \(item.name)")
        }
        .contextMenu {
            Button("Edit", action: onEdit).disabled(isPending)
            Button("Remove", role: .destructive, action: onRemove)
        }
    }
}
```

```swift
import SwiftUI

struct QuickAddField: View {
    let onAdd: (String) async -> Bool
    @State private var text = ""

    var body: some View {
        HStack {
            TextField("Add an item", text: $text)
                .submitLabel(.done)
                .onSubmit(submit)
                .accessibilityIdentifier("quickAddField")
            Button("Add", action: submit)
                .disabled(text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                .accessibilityIdentifier("quickAddButton")
        }
    }

    private func submit() {
        let entered = text
        Task { if await onAdd(entered) { text = "" } }
    }
}
```

- [ ] **Step 3: `EditItemSheet.swift`, `NewListSheet.swift`, `GenerateListSheet.swift`**

```swift
import API
import SwiftUI

struct EditItemSheet: View {
    let item: Components.Schemas.ShoppingItem
    let onSave: (String, String, Components.Schemas.Unit?, Components.Schemas.IngredientCategory) async -> ShoppingListViewModel.EditOutcome
    let onConflict: (Components.Schemas.ShoppingItem) -> Void
    let onClose: () -> Void

    @State private var name: String
    @State private var quantity: String
    @State private var unit: Components.Schemas.Unit?
    @State private var category: Components.Schemas.IngredientCategory
    @State private var message: String?
    @State private var isSaving = false

    init(
        item: Components.Schemas.ShoppingItem,
        onSave: @escaping (String, String, Components.Schemas.Unit?, Components.Schemas.IngredientCategory) async -> ShoppingListViewModel.EditOutcome,
        onConflict: @escaping (Components.Schemas.ShoppingItem) -> Void,
        onClose: @escaping () -> Void
    ) {
        self.item = item
        self.onSave = onSave
        self.onConflict = onConflict
        self.onClose = onClose
        _name = State(initialValue: item.name)
        _quantity = State(initialValue: item.quantity.map { $0.formatted(.number.precision(.fractionLength(0...2)).grouping(.never)) } ?? "")
        _unit = State(initialValue: item.unit.flatMap { Components.Schemas.Unit(rawValue: $0.rawValue) })
        _category = State(initialValue: item.category)
    }

    var body: some View {
        NavigationStack {
            Form {
                TextField("Name", text: $name).accessibilityIdentifier("editItemNameField")
                TextField("Quantity", text: $quantity).decimalKeyboard().accessibilityIdentifier("editItemQuantityField")
                Picker("Unit", selection: $unit) {
                    Text("None").tag(Components.Schemas.Unit?.none)
                    ForEach(Components.Schemas.Unit.allCases, id: \.self) { Text($0.rawValue).tag(Components.Schemas.Unit?.some($0)) }
                }
                Picker("Aisle", selection: $category) {
                    ForEach(Components.Schemas.IngredientCategory.allCases, id: \.self) { Text($0.label).tag($0) }
                }
                if let message { Text(message).foregroundStyle(.red).accessibilityIdentifier("editItemMessage") }
            }
            .navigationTitle("Edit item")
            .inlineNavigationTitle()
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel", action: onClose) }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") { Task { await save() } }
                        .disabled(isSaving)
                        .accessibilityIdentifier("editItemSaveButton")
                }
            }
        }
    }

    private func save() async {
        isSaving = true
        defer { isSaving = false }
        switch await onSave(name, quantity, unit, category) {
        case .saved: onClose()
        case .conflict(let current):
            // Reopen on the server's version, so the next save is against what is really there.
            onConflict(current)
        case .failed(let text): message = text
        }
    }
}
```

```swift
import SwiftUI

struct NewListSheet: View {
    let onSubmit: (String, Bool) async -> ShoppingViewModel.Outcome
    let onOpened: (String) -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var name = ""
    @State private var shared = false
    @State private var message: String?
    @State private var isSaving = false

    var body: some View {
        NavigationStack {
            Form {
                TextField("List name", text: $name).accessibilityIdentifier("newListNameField")
                Toggle("Share with partner", isOn: $shared)
                if let message { Text(message).foregroundStyle(.red) }
            }
            .navigationTitle("New list")
            .inlineNavigationTitle()
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Create") { Task { await create() } }
                        .disabled(isSaving)
                        .accessibilityIdentifier("newListSubmitButton")
                }
            }
        }
    }

    private func create() async {
        isSaving = true
        defer { isSaving = false }
        switch await onSubmit(name, shared) {
        case .opened(let id): onOpened(id)
        case .failed(let text): message = text
        }
    }
}
```

```swift
import SwiftUI

struct GenerateListSheet: View {
    let day: LocalDay
    let onSubmit: (String, String, String?) async -> ShoppingViewModel.Outcome
    let onOpened: (String) -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var from: Date
    @State private var to: Date
    @State private var name = ""
    @State private var message: String?
    @State private var isSaving = false

    init(
        day: LocalDay, range: (from: String, to: String),
        onSubmit: @escaping (String, String, String?) async -> ShoppingViewModel.Outcome,
        onOpened: @escaping (String) -> Void
    ) {
        self.day = day
        self.onSubmit = onSubmit
        self.onOpened = onOpened
        _from = State(initialValue: day.date(range.from) ?? Date())
        _to = State(initialValue: day.date(range.to) ?? Date())
    }

    var body: some View {
        NavigationStack {
            Form {
                DatePicker("From", selection: $from, displayedComponents: .date)
                DatePicker("To", selection: $to, displayedComponents: .date)
                TextField("List name (optional)", text: $name).accessibilityIdentifier("generateNameField")
                Text("Builds the list from the meals planned on these days.").font(.footnote).foregroundStyle(.secondary)
                if let message { Text(message).foregroundStyle(.red).accessibilityIdentifier("generateMessage") }
            }
            .navigationTitle("Generate list")
            .inlineNavigationTitle()
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Generate") { Task { await generate() } }
                        .disabled(isSaving)
                        .accessibilityIdentifier("generateSubmitButton")
                }
            }
        }
    }

    private func generate() async {
        isSaving = true
        defer { isSaving = false }
        switch await onSubmit(day.day(from: from), day.day(from: to), name) {
        case .opened(let id): onOpened(id)
        case .failed(let text): message = text
        }
    }
}
```

- [ ] **Step 4: `ShoppingListView.swift`**

```swift
import API
import Repositories
import SwiftUI

public struct ShoppingListView: View {
    @State private var viewModel: ShoppingListViewModel
    @State private var editing: Components.Schemas.ShoppingItem?
    @State private var showingRename = false
    @State private var renameText = ""
    @State private var confirmingDelete = false
    @State private var debugOffline = false
    private let dependencies: ShoppingDependencies
    @Environment(\.dismiss) private var dismiss
    @Environment(\.scenePhase) private var scenePhase

    public init(dependencies: ShoppingDependencies, listID: String) {
        self.dependencies = dependencies
        _viewModel = State(initialValue: ShoppingListViewModel(
            listID: listID, shopping: dependencies.shopping, sync: dependencies.sync,
            events: { dependencies.events.events(listID: $0) }, userID: { dependencies.currentUserID() }
        ))
    }

    public var body: some View {
        Group {
            if viewModel.accessLost {
                ContentUnavailableView("This list is no longer available", systemImage: "cart.badge.minus")
                    .accessibilityIdentifier("listUnavailable")
            } else if let list = viewModel.displayed {
                listBody(list)
            } else if viewModel.isLoading {
                ProgressView()
            } else {
                VStack(spacing: 12) {
                    Text(viewModel.loadError ?? "This list isn't loaded.")
                    Button("Try again") { Task { await viewModel.load() } }
                }
            }
        }
        .navigationTitle(viewModel.displayed?.name ?? "List")
        .inlineNavigationTitle()
        .toolbar { toolbarContent }
        .task { await viewModel.run() }
        .onChange(of: scenePhase) { _, phase in
            if phase == .active { Task { await viewModel.load() } }
        }
        .sheet(item: $editing) { item in
            EditItemSheet(
                item: item,
                onSave: { name, quantity, unit, category in
                    await viewModel.edit(item, name: name, quantity: quantity, unit: unit, category: category)
                },
                onConflict: { editing = $0 },
                onClose: { editing = nil }
            )
            .id("\(item.id)-\(item.version)")
        }
        .alert(
            "Shopping",
            isPresented: Binding(get: { viewModel.alertMessage != nil }, set: { if !$0 { viewModel.alertMessage = nil } })
        ) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(viewModel.alertMessage ?? "")
        }
        .alert("Rename list", isPresented: $showingRename) {
            TextField("Name", text: $renameText)
            Button("Save") { Task { await viewModel.rename(renameText) } }
            Button("Cancel", role: .cancel) {}
        }
        .confirmationDialog("Delete this list?", isPresented: $confirmingDelete, titleVisibility: .visible) {
            Button("Delete list", role: .destructive) {
                Task { if await viewModel.delete() { dismiss() } }
            }
        } message: {
            Text("The list is removed for you and your partner. This can't be undone.")
        }
    }

    private func listBody(_ list: Components.Schemas.ShoppingList) -> some View {
        List {
            Section {
                let progress = viewModel.progress
                Text("\(progress.done) of \(progress.total) bought").foregroundStyle(.secondary)
                if viewModel.isStale {
                    Label("Offline: showing the saved list", systemImage: "wifi.slash").font(.footnote).foregroundStyle(.secondary)
                }
            }
            ForEach(viewModel.groups) { group in
                Section(group.category.label) {
                    ForEach(group.items, id: \.id) { item in
                        ShoppingItemRow(
                            item: item,
                            onToggle: { Task { await viewModel.toggle(item) } },
                            onEdit: { editing = item },
                            onRemove: { Task { await viewModel.remove(item) } }
                        )
                    }
                }
            }
            Section { QuickAddField { await viewModel.quickAdd($0) } }
        }
    }

    @ToolbarContentBuilder
    private var toolbarContent: some ToolbarContent {
        ToolbarItemGroup(placement: .primaryAction) {
            if viewModel.isSyncing {
                Label("Syncing changes", systemImage: "arrow.triangle.2.circlepath")
                    .labelStyle(.iconOnly)
                    .accessibilityIdentifier("syncingBadge")
            }
            if let networkSwitch = dependencies.networkSwitch {
                // UI-test builds only (`-uiTesting`): cut the app off from the API, and bring it back.
                Button(debugOffline ? "Go online" : "Go offline") {
                    debugOffline.toggle()
                    networkSwitch.setOffline(debugOffline)
                    if !debugOffline { Task { await dependencies.sync.drain() } }
                }
                .accessibilityIdentifier("debugOfflineToggle")
            }
            if viewModel.isOwner {
                Menu {
                    Button("Rename") {
                        renameText = viewModel.displayed?.name ?? ""
                        showingRename = true
                    }
                    Button(viewModel.displayed?.sharedWithPartner == true ? "Stop sharing" : "Share with partner") {
                        Task { await viewModel.setShared(!(viewModel.displayed?.sharedWithPartner ?? false)) }
                    }
                    if viewModel.displayed?.sourceFrom != nil {
                        Button("Regenerate from plan") { Task { await viewModel.regenerate() } }
                    }
                    Button("Delete", role: .destructive) { confirmingDelete = true }
                } label: {
                    Label("List actions", systemImage: "ellipsis.circle")
                }
                .accessibilityIdentifier("listMenu")
            }
        }
    }
}
```

`ShoppingItem` must be `Identifiable` for `.sheet(item:)`: the generated type is not. Add this one line to `ShoppingListView.swift` (file scope): `extension Components.Schemas.ShoppingItem: @retroactive Identifiable {}`.

- [ ] **Step 5: Build, then the whole suite**

Run: `swift build` (from `ios/MealPlannerKit`), then `swift test`.
Expected: both succeed on macOS. Then from the repo root: `make build-ios`.
Expected: `** BUILD SUCCEEDED **` (the first compile of the iOS-only paths: a failure is most likely a UIKit-only modifier that needs `#if os(iOS)`).

- [ ] **Step 6: Commit**

```bash
git add ios/MealPlannerKit/Sources/Features/Shopping
git commit -m "feat(ios): add the Shopping screens: lists, list detail, quick-add, edit, generate

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 13: The XCUITest flow

**Files:**
- Create: `ios/MealPlannerUITests/ShoppingFlowUITests.swift`

**Interfaces:**
- Consumes: `AppUITestCase` helpers (`uniqueEmail`, `createAccountViaAPI`, `apiRequest`, `signIn`, `tabButton`, `waitUntilHittable`, `expectValue`, `typeVerified`, `signOutButton`); the identifiers from Task 12; the `-uiTesting` launch argument (Task 9).
- Produces: the flow in spec §10: generate in the UI, a partner's check-off lands live, an offline check-off shows the syncing badge and syncs on reconnect.

- [ ] **Step 1: Write the test**

```swift
import XCTest

final class ShoppingFlowUITests: AppUITestCase {
    private func localToday() -> String {
        let formatter = DateFormatter()
        formatter.dateFormat = "yyyy-MM-dd"
        formatter.locale = Locale(identifier: "en_US_POSIX")
        return formatter.string(from: Date())
    }

    private func itemID(named name: String, inList listID: String, token: String) throws -> String {
        let list = try apiRequest("GET", "/shopping-lists/\(listID)", token: token, expect: 200)
        let items = try XCTUnwrap(list["items"] as? [[String: Any]])
        return try XCTUnwrap(items.first { $0["name"] as? String == name }?["id"] as? String, "no item named \(name)")
    }

    /// A signs in, generates a list from a seeded plan, and B (the partner, over the API) checks an item: it lands on A's
    /// screen without a refresh. A then goes offline, checks another item (it looks bought and a syncing badge shows),
    /// comes back online, and the change reaches the server.
    func testGenerateSeeLiveCheckOffAndSyncAfterOffline() throws {
        let password = "correct-horse-battery-staple"
        let emailA = uniqueEmail()
        let tokenA = try createAccountViaAPI(email: emailA, password: password, displayName: "Shop A")
        let tokenB = try createAccountViaAPI(email: uniqueEmail(), password: password, displayName: "Shop B")

        // Link the two accounts.
        let invite = try apiRequest("POST", "/partner/invite", token: tokenA, expect: 201)
        try apiRequest("POST", "/partner/accept", token: tokenB, json: ["code": try XCTUnwrap(invite["code"] as? String)], expect: 200)

        // Seed A's plan for today with a meal of two ingredients, so the generated list has two items.
        let rice = try apiRequest("POST", "/ingredients", token: tokenA, json: ["name": "UI Shop Rice", "category": "other", "nutrients": ["calories": 130]], expect: 201)
        let beans = try apiRequest("POST", "/ingredients", token: tokenA, json: ["name": "UI Shop Beans", "category": "other", "nutrients": ["calories": 90]], expect: 201)
        let meal = try apiRequest("POST", "/meals", token: tokenA, json: ["name": "UI Shop Meal", "servings": 1], expect: 201)
        let mealID = try XCTUnwrap(meal["id"] as? String)
        try apiRequest(
            "PUT", "/meals/\(mealID)/ingredients", token: tokenA,
            json: ["items": [
                ["ingredient_id": try XCTUnwrap(rice["id"] as? String), "quantity": 100, "unit": "g"],
                ["ingredient_id": try XCTUnwrap(beans["id"] as? String), "quantity": 50, "unit": "g"],
            ]], expect: 200
        )
        try apiRequest("PUT", "/plan/\(localToday())/breakfast", token: tokenA, json: ["meal_id": mealID, "portion": 1], expect: 200)

        let app = XCUIApplication()
        app.launchArguments += ["-uiTesting"]
        app.launch()
        signIn(in: app, email: emailA, password: password)

        // 1. Generate a list from the plan through the UI (the default range starts today).
        let shoppingTab = tabButton(in: app, identifier: "shoppingTab", label: "Shopping")
        XCTAssertTrue(shoppingTab.waitForExistence(timeout: 45))
        shoppingTab.tap()
        let addMenu = app.buttons["addListMenu"]
        waitUntilHittable(addMenu, timeout: 45)
        addMenu.tap()
        let generate = app.buttons["generateListButton"]
        waitUntilHittable(generate)
        generate.tap()
        let nameField = app.textFields["generateNameField"]
        waitUntilHittable(nameField)
        nameField.tap()
        typeVerified("UI Shop List", field: nameField)
        app.buttons["generateSubmitButton"].tap()

        let riceRow = app.buttons["shoppingItem-UI Shop Rice"]
        let beansRow = app.buttons["shoppingItem-UI Shop Beans"]
        waitUntilHittable(riceRow, timeout: 45)
        waitUntilHittable(beansRow, timeout: 45)

        // 2. Share the list and let B check Rice over the API: it must appear on A's screen live.
        let lists = try apiRequest("GET", "/shopping-lists", token: tokenA, expect: 200)
        let listID = try XCTUnwrap((lists["items"] as? [[String: Any]])?.first?["id"] as? String)
        try apiRequest("PATCH", "/shopping-lists/\(listID)", token: tokenA, json: ["shared_with_partner": true], expect: 200)
        let riceID = try itemID(named: "UI Shop Rice", inList: listID, token: tokenB)
        try apiRequest("PATCH", "/shopping-lists/\(listID)/items/\(riceID)", token: tokenB, json: ["checked": true], expect: 200)
        expectValue(of: riceRow, toContain: "bought")

        // 3. Offline: Beans looks bought at once, and the syncing badge shows.
        let offlineToggle = app.buttons["debugOfflineToggle"]
        waitUntilHittable(offlineToggle)
        offlineToggle.tap() // "Go offline"
        waitUntilHittable(beansRow)
        beansRow.tap()
        expectValue(of: beansRow, toContain: "bought")
        XCTAssertTrue(app.images["syncingBadge"].waitForExistence(timeout: 45) || app.otherElements["syncingBadge"].waitForExistence(timeout: 5), "Expected the syncing badge while offline")

        // 4. Back online: the badge clears and the server has Beans checked.
        offlineToggle.tap() // "Go online"
        let badgeGone = NSPredicate(format: "exists == false")
        let gone = XCTNSPredicateExpectation(predicate: badgeGone, object: app.images["syncingBadge"])
        XCTAssertEqual(XCTWaiter().wait(for: [gone], timeout: 45), .completed, "Expected the syncing badge to clear after reconnecting")
        let beansID = try itemID(named: "UI Shop Beans", inList: listID, token: tokenB)
        var beansChecked = false
        for _ in 0..<15 where !beansChecked {
            let list = try apiRequest("GET", "/shopping-lists/\(listID)", token: tokenB, expect: 200)
            let items = try XCTUnwrap(list["items"] as? [[String: Any]])
            beansChecked = items.first { $0["id"] as? String == beansID }?["checked"] as? Bool == true
            if !beansChecked { Thread.sleep(forTimeInterval: 1) }
        }
        XCTAssertTrue(beansChecked, "Expected the offline check-off to reach the server")

        // Leave no session in the Keychain for the next test.
        signOutButton(in: app).tap()
        XCTAssertTrue(app.buttons["showRegisterButton"].waitForExistence(timeout: 45), "Expected the welcome screen after signing out")
    }
}
```

- [ ] **Step 2: Compile the UI-test target**

Run: `cd ios && xcodegen generate && xcodebuild build-for-testing -project MealPlanner.xcodeproj -scheme MealPlanner -destination 'generic/platform=iOS Simulator' 2>&1 | grep -E "error:|TEST BUILD"`
Expected: `** TEST BUILD SUCCEEDED **`. If `app.images["syncingBadge"]` never matches, print `app.debugDescription` once and match the element type SwiftUI actually gave the `Label` (an image or other element); keep the OR in the assertion.

- [ ] **Step 3: Run the flow if the machine allows (optional locally; CI is the signal)**

Start the API (`make db-up && make migrate && make run-api` in another terminal), then:
`cd ios && xcodebuild test -project MealPlanner.xcodeproj -scheme MealPlanner -destination 'platform=iOS Simulator,name=iPhone 16' -only-testing:MealPlannerUITests/ShoppingFlowUITests`
Expected: the test passes. Local XCUITest is unreliable under load; if it fails for waits, trust CI.

- [ ] **Step 4: Commit**

```bash
git add ios/MealPlannerUITests/ShoppingFlowUITests.swift
git commit -m "test(ios): add the Shopping XCUITest flow: generate, live check-off, offline sync

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 14: Docs, spec reconciliation and the final gate

**Files:**
- Modify: `ios/CLAUDE.md`
- Modify: `docs/superpowers/specs/2026-10-08-ios-shopping-design.md`
- Modify: `docs/superpowers/plans/2026-10-08-ios-shopping.md` (tick the boxes)

- [ ] **Step 1: Update `ios/CLAUDE.md`**

In the layout list add, in the matching bullets:
- `Sources/Persistence/`: "Also `ShoppingCache` (list summaries per scope, the server's snapshot of each list, and the persisted offline queue of `ShoppingIntent` rows) and `IntentQueue`, the pure collapsing rules."
- `Sources/Repositories/`: "Also `ShoppingListsRepository` (reads cache-first; item check/add/remove only enqueue; everything else is an online write that stores the server's answer) and `Sync/`: `PendingOverlay`, `ShoppingSyncEngine`, `ListEventStream` + `SSEFrameParser` + `ListEventReducer`, `NetworkMonitor`, `NetworkSwitch`."
- `Sources/Features/`: "`Shopping/`: `ShoppingViewModel` (lists), `ShoppingListViewModel` (one list), `ItemGrouping`, `RangeValidation`, the views."

In **Gotchas** add:

```
- Shopping offline model: the cache holds only the server's snapshot of a list; pending changes are `ShoppingIntent` rows. The screen shows `PendingOverlay.apply(snapshot, intents)`, so a refetch or SSE echo can never erase a pending change. Never write an optimistic edit into the snapshot.
- Collapsing happens in `ShoppingCache.enqueue` via `IntentQueue`: one row per item for check/uncheck (last wins), add-then-remove of a temp item cancels both, a check on an unsynced add keeps its own row addressed by the `temp:` id, which the engine rewrites to the server id after the add succeeds.
- `ShoppingSyncEngine` drains one request at a time. Network, 429, 5xx: keep the row, block only that list. 404: drop the row and the item (a vanished list is cleaned by the next `refreshList`, which also drops its intents). 400/409: drop with the notice "A change couldn't be saved.". A refused add also drops everything addressed to its temp id.
- `URLSession.AsyncBytes.lines` drops empty lines, and an empty line is what ends an SSE frame: `ListEventStream.urlSessionOpener` splits the bytes on `\n` itself. Do not "simplify" it to `.lines`.
- SSE rules live in `ListEventReducer` (pure): at or below the cached version is ignored, newer or unknown item and `list_changed` refetch, `item_deleted` removes, `list_deleted` and a `404` mean access lost even with items cached. An event never patches an item's content.
- A `409 version_conflict` on an item edit puts the server's `current` in the cache before throwing; the edit sheet reopens on it. Edits cannot clear a quantity or unit (the generated optionals cannot encode `null`).
- Reconnect is detected by `NetworkMonitor` (`NWPathMonitor`); `ShoppingDependencies.runSyncLoop` drains on launch and on every path appearing, and `RootView` drains again on foreground. Sign-out clears the queue (`clearAllCaches`), so a second user never sends the first user's changes.
- UI tests launched with `-uiTesting` get a `NetworkSwitch` and an `OfflineMiddleware`; the list screen shows a "Go offline" / "Go online" toolbar button (`debugOfflineToggle`). A normal launch creates neither.
- Test helpers: `ShoppingServer` (in-memory shopping endpoints; item `PATCH` honours `version` and answers the real `409`, ids are `srv-101`, `srv-102`, …) and `ShoppingHarness` (repository + engine + cache over one server).
```

- [ ] **Step 2: Reconcile the spec with what was built**

In `docs/superpowers/specs/2026-10-08-ios-shopping-design.md`:
- §3: `IntentQueue` lives in `Persistence/` (the cache collapses atomically); `QueuedIntent` is `CachedIntent`; `ShoppingIntent` carries a single `itemID` with a `temp:` prefix for an unsynced add instead of `itemId?` + `clientTempId?`.
- §4.3: the engine drains one request at a time overall; a `404` drops the row and the item, and a gone list is cleaned by the next refresh.
- §6: edits cannot clear a quantity or unit.
- §10: the partner link and the sharing of the generated list are done through the API, not the UI; the offline switch is the `-uiTesting` `NetworkSwitch` and the toolbar button.

- [ ] **Step 3: Run the full gate**

Run, from the repo root:
- `make test-ios` — expected: all suites PASS.
- `make build-ios` — expected: `** BUILD SUCCEEDED **`.
- `cd ios && xcodebuild build-for-testing -project MealPlanner.xcodeproj -scheme MealPlanner -destination 'generic/platform=iOS Simulator' 2>&1 | grep -E "error:|TEST BUILD"` — expected: `** TEST BUILD SUCCEEDED **`.
- `make check` — expected: green (it does not need Docker for iOS; the other gates are unchanged because `openapi.yaml` did not change). If `check-generated-ios` fails, something edited generated code: revert it.

- [ ] **Step 4: Tick this plan's boxes, commit, and report**

Mark every `- [ ]` in this file `- [x]`.

```bash
git add ios/CLAUDE.md docs/superpowers
git commit -m "docs(ios): document the Shopping design in ios/CLAUDE.md and reconcile the spec

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

Then open the pull request (the CI `ios.yml` `ui` job is the signal for Task 13) and comment on issue #28 with the PR link, saying the Shopping half is done and Profile remains.

---

## Self-review

**Spec coverage** (spec section → task):

| Spec | Task |
|---|---|
| §1 goal, success | all; gate in 14 |
| §2 overlay, queued kinds, SSE, reconnect, temp id, reads | 1, 2, 5, 7, 8 |
| §3 modules | 1–12 (file map above) |
| §4.1 overlay | 2 |
| §4.2 collapsing | 1, 5 |
| §4.3 draining, failure table | 7 |
| §4.4 indicator | 11 (`isSyncing`), 12 (`syncingBadge`) |
| §5 realtime, parser, rules, backoff | 4, 8, 11 |
| §6 screens (tab, generate, new list, list, menu, edit, a11y) | 10, 12 |
| §7 state and error rules | 10, 11 |
| §8 testing | every task's tests |
| §9 docs | 14 |
| §10 XCUITest | 13 |
| §11 build order | task order |
| §12 parent-spec updates | already committed with the spec; reconciled again in 14 |

**Placeholder scan:** none left. Task 13 deliberately shows a wrong stub and then replaces it: remove the stub when pasting.

**Type consistency:** `ShoppingIntent`/`AddPayload`/`IntentQueue` (Task 1) are used unchanged by the cache (5), repository (6), engine (7), overlay (2) and view model (11). `ListEvent(kind:listID:itemID:version:)` (4) is used by the stream (8) and view model (11). `ShoppingError.versionConflict(current:)` (6) is caught in Task 11. `ShoppingListViewModel.EventSource` (11) is satisfied by `ListEventStream.events(listID:)` in Task 12. `ShoppingHarness` (10) is reused in 11. `ItemEdit` (6) is built in 11.

**Not compiled:** every code block in this plan was written against the generated client's signatures as read from `Types+Components+Schemas.swift` and `Types+Operations.swift`, but none has been compiled. The first run of each task's test step is the real check; where a generated label differs, match the generated file and never edit it.
