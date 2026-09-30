# Web Shopping and Profile Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the last two areas of the web app: Shopping (lists grouped by aisle, generate from a plan date range, check-off and quick-add, edit an item, a partner badge, and live updates over SSE on shared lists) and Profile (daily targets, the partner connection by invite code, custom ingredient management, account deletion).

**Architecture:** Pages stay thin server components that render client components on TanStack Query, through the typed `api` client and the BFF proxy. A list is one query (`GET /shopping-lists/{id}`); check-off and quick-add are optimistic on that cache; everything else waits for the server. Live updates are a same-origin `EventSource` on `/api/shopping-lists/{id}/events` (the Foundation proxy already streams it and attaches the bearer): the stream carries only ids and versions, so the hook applies the API's rules (ignore an event at or below the cached item's version, refetch on a newer or unknown item, on `list_changed`, on every (re)open, and when the connection is closed for good) and never patches items itself. Profile is a stack of independent cards, each with its own mutation.

**Tech Stack:** Next.js 16 App Router, React 19, TypeScript strict (`noUncheckedIndexedAccess`), Tailwind 4, shadcn/ui, TanStack Query 5, `openapi-fetch`, Vitest 5 + Testing Library (jsdom), Playwright.

**Spec:** `docs/superpowers/specs/2026-09-25-web-app-design.md` (§2, §4, §5, §7, §10 plan 4); parent spec `docs/superpowers/specs/2026-09-21-meal-planner-design.md` §3.5 and §5.1; partner rules in `docs/superpowers/specs/2026-09-23-backend-partner-sharing-design.md`. Backend contract: `openapi.yaml` (`/shopping-lists`, `/shopping-lists/generate`, `/shopping-lists/{id}`, `/shopping-lists/{id}/items`, `/shopping-lists/{id}/items/{item_id}`, `/shopping-lists/{id}/events`, `/partner`, `/partner/invite`, `/partner/accept`, `/partner/shopping-lists`, `/me`, `/ingredients/{id}`). Builds on the merged Foundation, Meals and Plan and Today plans.

**Branch:** work on `feat/web-shopping-profile`, created from `master` in a worktree (superpowers:using-git-worktrees). `openapi.yaml` and `backend/` do not change.

## Global Constraints

- "The browser only calls its own origin." Every call goes through `api` + `unwrap` from `@/lib/api/client`, except the event stream, which is a plain same-origin `EventSource`. (Spec §2, §4)
- "Server-side data fetching: None in v1. `(app)` pages are client components on TanStack Query." Page files stay thin server components that render one client component. (Spec §2)
- "Client rules come from the API's stream description: ignore an event whose `version` is at or below the cached item's version; refetch the list on every (re)open, on a version gap, and after `list_changed`; treat `list_deleted` as "not found" and navigate away. A proxied stream that the API closes (unlink, unshare, shutdown) ends on the browser side, and the reconnect gets `404` from the refetch." (Spec §4)
- "Optimistic updates only where the parent spec asks for them: shopping check-off and quick-add, and plan swaps and portion changes. Each snapshots, applies, rolls back on error and refetches. A version conflict or an SSE version gap triggers a refetch, never a guess." (Spec §5)
- "Shopping: lists grouped by aisle category; check-off, quick-add, generate from a plan date range; a partner badge and live updates on shared lists. Profile: targets, partner connection (invite code or accept), custom ingredients, account deletion." (Parent spec §5.1)
- "Changing `name`, `quantity`, `unit` or `category` requires `version`, which must equal the item's current version, or the request fails with `409 version_conflict` carrying the current item in `current`. A request that changes only `checked` is last-write-wins and does not need `version`." (`openapi.yaml`)
- "Renaming, deleting, sharing and regenerating are owner-only (`404` for the partner); the partner may add, edit, check and delete items, and open the event stream." Regenerating "replac[es] every `generated` item, keeping every `manual` item untouched." Generation is capped at 92 days. (`openapi.yaml`)
- An invite code has 8 characters, is shown only when it is created, and expires after 48 hours; a wrong, expired, own or used code is `404 invite_invalid`; a linked caller gets `409 partner_already_linked`. Unlinking ends access at once on both sides. (`openapi.yaml`)
- "Supplying the `nutrients` field replaces the ingredient's entire nutrient set: any of the 18 keys omitted, or explicitly set to null, is cleared." (`openapi.yaml`, `NutrientAmountsInput`)
- "`DELETE /me` needs no re-authentication" and "Permanently deletes the account and everything it owns." (`backend/CLAUDE.md`, `openapi.yaml`), so the UI itself asks for a typed confirmation.
- "Field validation errors show inline; everything else is a toast. Both are driven by the problem `code`. Loading is a skeleton; empty states carry a call to action." (Spec §5)
- "`is_owner: false` renders read-only" for the list's own settings; its items stay editable for both partners. (`openapi.yaml`)
- "E2E creates custom ingredients through the API, so CI needs no `FDC_API_KEY`. Each test registers its own user." E2E runs with one worker. (Spec §7)
- Never hand-edit `web/src/lib/api/schema.gen.ts`. Colours, radius and macro colours live in `src/app/globals.css` only. shadcn components come from `npx shadcn@4.21.0 add <component>`.
- Component tests start with `// @vitest-environment jsdom`. Every task ends with `make lint-web` and `make test-web` clean (run from the repo root; ESLint 9 with the React 19 hooks rules: no ref writes during render, no synchronous `setState` in an effect body; `tsc` has `noUncheckedIndexedAccess`, so write `list[0]?.x` in tests and never spread `list[0]`).
- Fake-API tests: make the fake server reflect a write (an optimistic edit is followed by a refetch that restores the old state if the fake still serves it); scope option queries to a listbox when a native `<select>` is on screen; put `onError` on the hook, not on `mutate(vars, { onError })`.
- Reuse, do not rebuild: `Field`, `NativeSelect`, `ErrorState`, `PageHeader`, `Badge`, `Dialog*`, `Tabs*`, `Skeleton`, `IngredientSearch`, `CustomIngredientDialog` and `parseCustomIngredient`/`CI`, `useMeals`-style infinite hooks, `usePartnerLink`/`PARTNER_KEY`, `useMe`/`ME_KEY`, `logout`, `hardNavigate`, `usePlan`/`planKeys`, `useLoadAllPages`, `lib/dates`, `parseDecimal`, `CATEGORIES`/`categoryLabel`, and the test helpers in `src/test/` all exist from the earlier plans.

## Review Focus

The spec is silent on these inputs; each line names the behaviour a person would expect and the task whose tests pin it.

1. **Events about my own changes, old events and unknown items**: my own check-off echoes back over the stream at the version I already hold and must not refetch or flicker; an older event must never revert newer state; a newer or unknown item refetches; a deleted item leaves the list without a refetch. (Task 3)
2. **Losing a list while it is open** (the partner unlinks, stops sharing or deletes it): the page says it is no longer available, instead of showing stale items or retrying forever, even though data for it is still cached; a permanently failed stream triggers the refetch that learns this. (Task 3, Task 6)
3. **Two people editing one item**: a stale edit gets the item's current state back and is asked to review and save again (no silent overwrite, no loss of what the other person typed); checking an item needs no version and never conflicts; tapping a checkbox twice fast ends on the last tap. (Task 2, Task 6)
4. **Irreversible actions**: deleting the account needs the account's email typed, a failed delete leaves the session intact, success ends the session with a full page load; unlinking and deleting a list ask first and say what happens; a blank target clears it, an invalid one is never sent, `0` calories is refused while `0` protein is allowed. (Task 5, Task 7, Task 8)
5. **A bad date range for generating or regenerating**: blank dates, an end before the start, or more than 92 days are flagged inline and never sent; an empty plan range makes an empty list, not an error; regenerate says it replaces generated items and keeps the person's own. (Task 4, Task 5)
6. **Editing a custom ingredient must not erase what the form does not show**: `PATCH` with `nutrients` replaces the whole set, so the update sends all 18 keys (the 14 the form lacks are carried over unchanged). (Task 9)

Also pinned: an invite code is shown once and copying it cannot crash the page (Task 8); a wrong invite code is an inline message on the code field (Task 8); a checked item is attributed to the partner only when the partner checked it (Task 6); a half-saved optimistic item cannot be checked or edited until the server has answered (Task 6).

## File Structure

```
web/src/
  lib/api/problem.ts (modify: ApiError.body, more codes) + problem-shopping-codes.test.ts      Task 1
  test/shopping-fixtures.ts, fake-event-source.ts (+test)                                     Tasks 1, 3
  features/shopping/
    items.ts (+test)                     grouping by aisle, quantity text, counts, range label    Task 1
    list-cache.ts (+test)                pure edits of a cached list                              Task 2
    queries.ts (+test)                   lists, list, optimistic check-off and quick-add, edit     Task 2
    list-events.ts, use-list-events.ts (+tests)   the SSE rules                                   Task 3
    shopping-lists.tsx, shopping-page.tsx, new-list-dialog.tsx, generate-list-dialog.tsx (+tests) Task 4
    shopping-list-view.tsx, item-row.tsx, quick-add.tsx, edit-item-dialog.tsx (+tests)             Task 6
    list-settings-dialog.tsx (+test)                                                               Task 5
  features/profile/
    targets.ts (+test), targets-form.tsx, delete-account.tsx (+tests)                              Task 7
  features/partner/
    queries.ts, invite-code.ts (+test), partner-card.tsx (+test)                                   Task 8
  features/ingredients/
    queries.ts, edit-ingredient.ts (+test), edit-ingredient-dialog.tsx, custom-ingredients-page.tsx (+tests)   Task 9
  app/(app)/shopping/page.tsx (replace); shopping/[id]/page.tsx; profile/page.tsx (modify); profile/ingredients/page.tsx
web/e2e/shopping.spec.ts, profile.spec.ts                                                      Task 10
web/CLAUDE.md, docs/superpowers/specs/2026-09-25-web-app-design.md (modify)                    Task 11
```

---

### Task 1: Problem bodies, more problem codes, and the shopping helpers

**Files:**
- Modify: `web/src/lib/api/problem.ts`
- Create: `web/src/features/shopping/items.ts`, `web/src/test/shopping-fixtures.ts`
- Test: `web/src/lib/api/problem-shopping-codes.test.ts`, `web/src/features/shopping/items.test.ts`

**Interfaces:**
- Consumes: `ApiError`, `toApiError`, `problemMessage`, `fieldMessage` from `web/src/lib/api/problem.ts`; `CATEGORIES`, `IngredientCategory` (`@/lib/ingredient-categories`); `formatMonthDay` (`@/lib/dates`).
- Produces:
  - `ApiError.body: Record<string, unknown>`: the whole problem document as the API sent it (so `version_conflict`'s `current` member is reachable); `{}` when the answer was not a problem. `ApiErrorInit` gains `body?`. `toApiError(status, body, headers)` fills it.
  - `problemMessage` gains `version_conflict`, `version_required`, `invite_invalid`, `partner_already_linked`.
  - `@/features/shopping/items`: types `ShoppingItem`, `ShoppingList`, `ShoppingListSummary`; `type ItemGroup = { category: IngredientCategory; items: ShoppingItem[] }`; `groupByCategory(items: readonly ShoppingItem[]): ItemGroup[]` (categories in `CATEGORIES` order, empty ones left out; within a group unchecked before checked, then by `position`); `checkedCount(items): { done: number; total: number }`; `formatItemQuantity(item: { quantity: number | null; unit: "g" | "ml" | "piece" | null }): string` ("" when there is no quantity; "500 g"; "1 piece", "2 pieces"; "2" when there is no unit); `rangeLabel(list: { source_from: string | null; source_to: string | null }): string | null` ("Sep 28 – Oct 4").
  - Test-only (`shopping-fixtures.ts`): `makeItem(over?)`, `makeList(over?)`, `makeListSummary(over?)`, `makeUser(over?)`.

- [ ] **Step 1: Write the failing tests**

**Create `web/src/lib/api/problem-shopping-codes.test.ts`**

```ts
import { describe, expect, it } from "vitest";
import { ApiError, problemMessage, toApiError } from "./problem";

const err = (code: string, status = 409) => new ApiError({ status, code });

describe("problemMessage for shopping and the partner link", () => {
  it.each([
    ["version_conflict", /Someone else changed this item/],
    ["version_required", /Reload/],
    ["invite_invalid", /isn't valid/],
    ["partner_already_linked", /already linked/],
  ])("explains %s in words", (code, pattern) => {
    expect(problemMessage(err(code))).toMatch(pattern);
  });
});

describe("ApiError.body", () => {
  it("keeps the whole problem document, including members the UI does not know, such as a conflict's current item", () => {
    const current = { id: "i1", version: 4, name: "Bananas" };
    const error = toApiError(409, { type: "urn:x", title: "Conflict", status: 409, code: "version_conflict", current }, new Headers());
    expect(error.code).toBe("version_conflict");
    expect(error.body.current).toEqual(current);
  });

  it("is empty when the answer was not a problem", () => {
    expect(toApiError(502, "<html>bad gateway</html>", new Headers()).body).toEqual({});
    expect(toApiError(500, null, new Headers()).body).toEqual({});
  });
});
```

**Create `web/src/features/shopping/items.test.ts`**

```ts
import { describe, expect, it } from "vitest";
import { makeItem } from "@/test/shopping-fixtures";
import { checkedCount, formatItemQuantity, groupByCategory, rangeLabel } from "./items";

describe("groupByCategory", () => {
  it("groups by aisle in the order the catalogue lists the categories, leaving out empty ones", () => {
    const groups = groupByCategory([
      makeItem({ id: "a", category: "other", name: "Bin bags" }),
      makeItem({ id: "b", category: "produce", name: "Apples" }),
      makeItem({ id: "c", category: "dairy_eggs", name: "Milk" }),
    ]);
    expect(groups.map((g) => g.category)).toEqual(["produce", "dairy_eggs", "other"]);
  });

  it("puts unchecked items before checked ones within an aisle, each in list order", () => {
    const [group] = groupByCategory([
      makeItem({ id: "1", category: "produce", position: 0, checked: true, name: "Apples" }),
      makeItem({ id: "2", category: "produce", position: 1, name: "Carrots" }),
      makeItem({ id: "3", category: "produce", position: 2, checked: true, name: "Leeks" }),
      makeItem({ id: "4", category: "produce", position: 3, name: "Onions" }),
    ]);
    expect(group?.items.map((i) => i.name)).toEqual(["Carrots", "Onions", "Apples", "Leeks"]);
  });

  it("does not change the list it was given", () => {
    const items = [makeItem({ id: "1", checked: true }), makeItem({ id: "2", position: 1 })];
    const before = JSON.stringify(items);
    groupByCategory(items);
    expect(JSON.stringify(items)).toBe(before);
  });

  it("is empty for no items", () => {
    expect(groupByCategory([])).toEqual([]);
  });
});

describe("checkedCount", () => {
  it("counts checked and all items", () => {
    expect(checkedCount([makeItem({ id: "1", checked: true }), makeItem({ id: "2" }), makeItem({ id: "3", checked: true })])).toEqual({ done: 2, total: 3 });
    expect(checkedCount([])).toEqual({ done: 0, total: 0 });
  });
});

describe("formatItemQuantity", () => {
  it.each([
    [{ quantity: null, unit: null }, ""],
    [{ quantity: null, unit: "g" as const }, ""],
    [{ quantity: 500, unit: "g" as const }, "500 g"],
    [{ quantity: 1.5, unit: "ml" as const }, "1.5 ml"],
    [{ quantity: 1000, unit: "ml" as const }, "1,000 ml"],
    [{ quantity: 1, unit: "piece" as const }, "1 piece"],
    [{ quantity: 6, unit: "piece" as const }, "6 pieces"],
    [{ quantity: 2, unit: null }, "2"],
    [{ quantity: 0.333333, unit: "g" as const }, "0.33 g"],
  ])("writes %j as %j", (item, text) => {
    expect(formatItemQuantity(item)).toBe(text);
  });
});

describe("rangeLabel", () => {
  it("names the plan range a list was generated from", () => {
    expect(rangeLabel({ source_from: "2026-09-28", source_to: "2026-10-04" })).toBe("Sep 28 – Oct 4");
  });

  it("is null for a list that was not generated", () => {
    expect(rangeLabel({ source_from: null, source_to: null })).toBeNull();
    expect(rangeLabel({ source_from: "2026-09-28", source_to: null })).toBeNull();
  });
});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/lib/api/problem-shopping-codes.test.ts src/features/shopping/items.test.ts`
Expected: FAIL: `Failed to resolve import "./items"` and `"@/test/shopping-fixtures"`; `error.body` is `undefined`; the four problem codes answer the generic line.

- [ ] **Step 3: Write the implementation**

**Create `web/src/test/shopping-fixtures.ts`** (test helper, no test of its own)

```ts
import type { components } from "@/lib/api/schema.gen";

type S = components["schemas"];
const STAMP = "2026-01-01T00:00:00Z";

export function makeItem(over: Partial<S["ShoppingItem"]> = {}): S["ShoppingItem"] {
  return {
    id: "i1",
    list_id: "l1",
    ingredient_id: null,
    name: "Milk",
    quantity: null,
    unit: null,
    category: "dairy_eggs",
    checked: false,
    checked_by: null,
    position: 0,
    version: 1,
    origin: "manual",
    created_at: STAMP,
    updated_at: STAMP,
    ...over,
  };
}

export function makeList(over: Partial<S["ShoppingList"]> = {}): S["ShoppingList"] {
  return { id: "l1", name: "Weekly shop", shared_with_partner: false, is_owner: true, source_from: null, source_to: null, items: [], created_at: STAMP, updated_at: STAMP, ...over };
}

export function makeListSummary(over: Partial<S["ShoppingListSummary"]> = {}): S["ShoppingListSummary"] {
  return { id: "l1", name: "Weekly shop", shared_with_partner: false, source_from: null, source_to: null, created_at: STAMP, updated_at: STAMP, ...over };
}

export function makeUser(over: Partial<S["User"]> = {}): S["User"] {
  return { id: "u1", email: "ann@example.test", display_name: "Ann", target_kcal: null, target_protein_g: null, target_carbs_g: null, target_fat_g: null, created_at: STAMP, updated_at: STAMP, ...over };
}
```

**Create `web/src/features/shopping/items.ts`**

```ts
import { formatMonthDay } from "@/lib/dates";
import type { components } from "@/lib/api/schema.gen";
import { CATEGORIES, type IngredientCategory } from "@/lib/ingredient-categories";

type Schemas = components["schemas"];
export type ShoppingItem = Schemas["ShoppingItem"];
export type ShoppingList = Schemas["ShoppingList"];
export type ShoppingListSummary = Schemas["ShoppingListSummary"];

export type ItemGroup = { category: IngredientCategory; items: ShoppingItem[] };

/** The items by aisle, in the order of the category catalogue. Within an aisle what is still to buy comes first. */
export function groupByCategory(items: readonly ShoppingItem[]): ItemGroup[] {
  const byCategory = new Map<IngredientCategory, ShoppingItem[]>();
  for (const item of items) {
    const group = byCategory.get(item.category) ?? [];
    group.push(item);
    byCategory.set(item.category, group);
  }
  return CATEGORIES.flatMap((category) => {
    const group = byCategory.get(category);
    if (!group) return [];
    return [{ category, items: [...group].sort((a, b) => Number(a.checked) - Number(b.checked) || a.position - b.position) }];
  });
}

export function checkedCount(items: readonly ShoppingItem[]): { done: number; total: number } {
  return { done: items.filter((item) => item.checked).length, total: items.length };
}

export function formatItemQuantity(item: { quantity: number | null; unit: "g" | "ml" | "piece" | null }): string {
  if (item.quantity === null) return "";
  const amount = item.quantity.toLocaleString("en-US", { maximumFractionDigits: 2 });
  if (item.unit === null) return amount;
  if (item.unit === "piece") return `${amount} ${item.quantity === 1 ? "piece" : "pieces"}`;
  return `${amount} ${item.unit}`;
}

/** The plan dates a list was generated from, or null for a list built by hand. */
export function rangeLabel(list: { source_from: string | null; source_to: string | null }): string | null {
  if (!list.source_from || !list.source_to) return null;
  return `${formatMonthDay(list.source_from)} – ${formatMonthDay(list.source_to)}`;
}
```

**Edit `web/src/lib/api/problem.ts`**: replace

```ts
  readonly retryAfter: number | null;

  constructor(init: { status: number; code: string; title?: string; fieldErrors?: Record<string, string>; retryAfter?: number | null }) {
```

with

```ts
  readonly retryAfter: number | null;
  /** The whole problem document as the API sent it, so members the UI does not model (a conflict's `current` item) stay reachable. */
  readonly body: Record<string, unknown>;

  constructor(init: { status: number; code: string; title?: string; fieldErrors?: Record<string, string>; retryAfter?: number | null; body?: Record<string, unknown> }) {
```

replace

```ts
    this.retryAfter = init.retryAfter ?? null;
  }
```

with

```ts
    this.retryAfter = init.retryAfter ?? null;
    this.body = init.body ?? {};
  }
```

replace

```ts
    retryAfter: Number.isFinite(retry) && retry > 0 ? retry : null,
  });
```

with

```ts
    retryAfter: Number.isFinite(retry) && retry > 0 ? retry : null,
    body: problem ? { ...problem } : {},
  });
```

and replace

```ts
    case "plan_conflict":
      return "Some of those days already have meals.";
```

with

```ts
    case "version_conflict":
      return "Someone else changed this item. Review it and save again.";
    case "version_required":
      return "Reload the list and try again.";
    case "invite_invalid":
      return "That invite code isn't valid. It may have expired or already been used.";
    case "partner_already_linked":
      return "You're already linked with a partner.";
    case "plan_conflict":
      return "Some of those days already have meals.";
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/lib src/features/shopping`
Expected: PASS.

- [ ] **Step 5: Lint and commit**

```bash
make lint-web && make test-web
git add web/src
git commit -m "feat(web): keep the problem body on ApiError, add shopping problem codes and the shopping item helpers"
```

---

### Task 2: The shopping data layer

**Files:**
- Create: `web/src/features/shopping/list-cache.ts`, `web/src/features/shopping/queries.ts`
- Test: `web/src/features/shopping/list-cache.test.ts`, `web/src/features/shopping/queries.test.tsx`

**Interfaces:**
- Consumes: `api`, `unwrap`; `ApiError`; `ShoppingItem`, `ShoppingList`, `ShoppingListSummary` (Task 1); test helpers and `shopping-fixtures`.
- Produces (`list-cache.ts`, pure, never mutate their input):
  - `withChecked(list, itemId, checked: boolean, by: string | null): ShoppingList`: sets `checked` and `checked_by` (`by` when checking, `null` when unchecking); leaves `version` alone (only the server bumps it)
  - `withAddedItem(list, item): ShoppingList` (appends), `withoutItem(list, itemId): ShoppingList`
  - `withItem(list, item): ShoppingList`: inserts an unknown item (kept in `position` order) or replaces a known one, unless the cached one has a higher `version`
  - `restoreItem(list, item): ShoppingList`: replaces the item with exactly `item` whatever the versions (for a rollback), or puts it back if it is gone
  - `optimisticItem(listId, input: { name: string; ingredientId?: string; category?: IngredientCategory }, position: number, id: string): ShoppingItem` (`origin: "manual"`, `version: 1`, unchecked)
  - `isStaleEvent(list: ShoppingList | undefined, itemId: string, version: number): boolean`: true when the cached list holds that item at `version` or higher
  - `OPTIMISTIC_PREFIX = "optimistic:"`, `isOptimistic(item): boolean`
- Produces (`queries.ts`):
  - `shoppingKeys = { mine: ["shopping","mine"], partner: ["shopping","partner"], detail(id): ["shopping","detail",id] }`
  - `useShoppingLists(scope: "mine" | "partner")`: `useInfiniteQuery` over `GET /shopping-lists` or `GET /partner/shopping-lists`
  - `useShoppingList(id)`: `useQuery` over `GET /shopping-lists/{id}`
  - `useCreateShoppingList()`: `(body: { name: string; shared_with_partner?: boolean }) => ShoppingList`; `useGenerateShoppingList()`: `({ from, to, name?, listId? }) => ShoppingList`; both seed the detail cache and mark `shoppingKeys.mine` stale
  - `useUpdateShoppingList(id)`: `(body: { name?: string; shared_with_partner?: boolean }) => ShoppingList`, seeds the detail cache, marks both list keys stale
  - `useDeleteShoppingList()`: `(id: string) => void`, drops the detail cache, marks `shoppingKeys.mine` stale
  - `useCheckItem(listId, userId: string | null, onFailure?)`: optimistic `({ itemId, checked }) => ShoppingItem`: checks at once, on success stores the server's item (new version), on error restores that item and calls `onFailure`; while another tap on the same list is still in flight an answer leaves the screen alone, so the last tap wins
  - `useAddItem(listId, onFailure?)`: optimistic `({ name, ingredientId?, category? }) => ShoppingItem`: appends a temporary item (id `optimistic:…`) at once, on success swaps it for the server's item, on error removes it and calls `onFailure`
  - `useEditItem(listId)`: `({ item, changes: { name?, quantity?: number | null, unit?: "g"|"ml"|"piece"|null, category? } }) => ShoppingItem`: sends `item.version`; on success stores the server's item; on `409 version_conflict` stores the body's `current` item, then rejects with the `ApiError`
  - `conflictingItem(error: Error): ShoppingItem | null`: the `current` item of a `409 version_conflict` (null for anything else)
  - `useDeleteItem(listId)`: `(itemId: string) => void`: removes the item from the cache when the server has deleted it

- [ ] **Step 1: Write the failing tests**

**Create `web/src/features/shopping/list-cache.test.ts`**

```ts
import { describe, expect, it } from "vitest";
import { makeItem, makeList } from "@/test/shopping-fixtures";
import { OPTIMISTIC_PREFIX, isOptimistic, isStaleEvent, optimisticItem, restoreItem, withAddedItem, withChecked, withItem, withoutItem } from "./list-cache";

const list = makeList({
  items: [makeItem({ id: "a", name: "Apples", position: 0, version: 2 }), makeItem({ id: "b", name: "Bread", position: 1, version: 1, checked: true, checked_by: "u2" })],
});
const names = (l: ReturnType<typeof makeList>) => l.items.map((i) => i.name);

describe("withChecked", () => {
  it("checks an item for the person who did, without touching its version", () => {
    const next = withChecked(list, "a", true, "u1");
    expect(next.items[0]).toMatchObject({ checked: true, checked_by: "u1", version: 2 });
    expect(next.items[1]).toBe(list.items[1]);
  });

  it("clears who checked it when it is unchecked", () => {
    expect(withChecked(list, "b", false, "u1").items[1]).toMatchObject({ checked: false, checked_by: null });
  });

  it("leaves the list alone for an unknown item, and never mutates its input", () => {
    const before = JSON.stringify(list);
    expect(withChecked(list, "zzz", true, "u1")).toEqual(list);
    withChecked(list, "a", true, "u1");
    expect(JSON.stringify(list)).toBe(before);
  });
});

describe("withAddedItem and withoutItem", () => {
  it("appends and removes", () => {
    const added = withAddedItem(list, makeItem({ id: "c", name: "Cheese", position: 2 }));
    expect(names(added)).toEqual(["Apples", "Bread", "Cheese"]);
    expect(names(withoutItem(added, "a"))).toEqual(["Bread", "Cheese"]);
    expect(names(withoutItem(list, "zzz"))).toEqual(["Apples", "Bread"]);
  });
});

describe("withItem", () => {
  it("inserts an item it does not know, in position order", () => {
    expect(names(withItem(list, makeItem({ id: "c", name: "Between", position: 0.5 })))).toEqual(["Apples", "Between", "Bread"]);
  });

  it("replaces a known item with a newer or equal version", () => {
    expect(withItem(list, makeItem({ id: "a", name: "Apples (red)", version: 3 })).items[0]?.name).toBe("Apples (red)");
    expect(withItem(list, makeItem({ id: "a", name: "Same version", version: 2 })).items[0]?.name).toBe("Same version");
  });

  it("ignores an older version, so a late answer never reverts newer state", () => {
    expect(withItem(list, makeItem({ id: "a", name: "Old", version: 1 }))).toBe(list);
  });
});

describe("restoreItem", () => {
  it("puts back exactly the item it is given, whatever the versions", () => {
    const changed = withItem(list, makeItem({ id: "a", name: "Apples (red)", version: 5 }));
    expect(restoreItem(changed, list.items[0]!).items[0]).toBe(list.items[0]);
  });

  it("puts back an item that is gone", () => {
    expect(names(restoreItem(withoutItem(list, "a"), list.items[0]!))).toEqual(["Apples", "Bread"]);
  });
});

describe("optimisticItem and isOptimistic", () => {
  it("is an unchecked manual item marked as not yet saved", () => {
    const item = optimisticItem("l1", { name: "Milk" }, 7, `${OPTIMISTIC_PREFIX}1`);
    expect(item).toMatchObject({ id: "optimistic:1", list_id: "l1", name: "Milk", category: "other", checked: false, origin: "manual", position: 7, version: 1, ingredient_id: null, quantity: null, unit: null });
    expect(isOptimistic(item)).toBe(true);
    expect(isOptimistic(makeItem())).toBe(false);
  });

  it("takes the ingredient and category it is given", () => {
    expect(optimisticItem("l1", { name: "Kale", ingredientId: "ing1", category: "produce" }, 0, "optimistic:2")).toMatchObject({ ingredient_id: "ing1", category: "produce" });
  });
});

describe("isStaleEvent", () => {
  it("is true for an event at or below the version already held, false for a newer one", () => {
    expect(isStaleEvent(list, "a", 2)).toBe(true);
    expect(isStaleEvent(list, "a", 1)).toBe(true);
    expect(isStaleEvent(list, "a", 3)).toBe(false);
  });

  it("is false for an item it does not hold, or when there is no list yet", () => {
    expect(isStaleEvent(list, "zzz", 1)).toBe(false);
    expect(isStaleEvent(undefined, "a", 1)).toBe(false);
  });
});
```

**Create `web/src/features/shopping/queries.test.tsx`**

```tsx
// @vitest-environment jsdom
import { act, renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ApiError } from "@/lib/api/problem";
import { fakeApi, json, noContent, problem } from "@/test/fake-api";
import { makeItem, makeList, makeListSummary } from "@/test/shopping-fixtures";
import { clientWrapper, testQueryClient } from "@/test/render";
import type { ShoppingList } from "./items";
import { shoppingKeys, useAddItem, useCheckItem, useCreateShoppingList, useDeleteItem, useDeleteShoppingList, useEditItem, useGenerateShoppingList, useShoppingList, useShoppingLists, useUpdateShoppingList } from "./queries";

const L = "l1";
const base = () => makeList({ items: [makeItem({ id: "i1", name: "Milk", version: 1 }), makeItem({ id: "i2", name: "Eggs", position: 1 })] });
const state = (l: ShoppingList | undefined) => l?.items.map((i) => `${i.name}:${i.checked ? "x" : "-"}`);

describe("useShoppingLists", () => {
  it("walks the pages of my lists with the API's cursor, and reads the partner's from the partner route", async () => {
    const fake = fakeApi({
      "GET /shopping-lists": (req) =>
        req.search.get("cursor") === "c1"
          ? json({ items: [makeListSummary({ id: "l2", name: "Party" })], next_cursor: null })
          : json({ items: [makeListSummary()], next_cursor: "c1" }),
      "GET /partner/shopping-lists": () => json({ items: [makeListSummary({ name: "Shared" })], next_cursor: null }),
    });
    const mine = renderHook(() => useShoppingLists("mine"), { wrapper: clientWrapper() });
    await waitFor(() => expect(mine.result.current.hasNextPage).toBe(true));
    await act(async () => {
      await mine.result.current.fetchNextPage();
    });
    await waitFor(() => expect(mine.result.current.data?.pages.flatMap((p) => p.items).map((l) => l.name)).toEqual(["Weekly shop", "Party"]));
    expect(fake.callsTo("GET", "/shopping-lists")[1]?.search.get("cursor")).toBe("c1");

    const partner = renderHook(() => useShoppingLists("partner"), { wrapper: clientWrapper() });
    await waitFor(() => expect(partner.result.current.data?.pages).toHaveLength(1));
    expect(fake.callsTo("GET", "/partner/shopping-lists")).toHaveLength(1);
  });
});

describe("list mutations", () => {
  it("create and generate seed the detail cache and mark my lists stale", async () => {
    const created = makeList({ id: "new1", name: "Party" });
    const generated = makeList({ id: "gen1", name: "Shopping", source_from: "2026-09-28", source_to: "2026-10-04" });
    const fake = fakeApi({ "POST /shopping-lists": () => json(created, 201), "POST /shopping-lists/generate": () => json(generated, 201) });
    const queryClient = testQueryClient();
    queryClient.setQueryData(shoppingKeys.mine, { pages: [], pageParams: [] });
    const create = renderHook(() => useCreateShoppingList(), { wrapper: clientWrapper(queryClient) });
    const generate = renderHook(() => useGenerateShoppingList(), { wrapper: clientWrapper(queryClient) });

    await act(async () => {
      await create.result.current.mutateAsync({ name: "Party" });
      await generate.result.current.mutateAsync({ from: "2026-09-28", to: "2026-10-04" });
    });
    expect(fake.callsTo("POST", "/shopping-lists")[0]?.body).toEqual({ name: "Party" });
    expect(fake.callsTo("POST", "/shopping-lists/generate")[0]?.body).toEqual({ from: "2026-09-28", to: "2026-10-04" });
    expect(queryClient.getQueryData(shoppingKeys.detail("new1"))).toEqual(created);
    expect(queryClient.getQueryData(shoppingKeys.detail("gen1"))).toEqual(generated);
    expect(queryClient.getQueryState(shoppingKeys.mine)?.isInvalidated).toBe(true);
  });

  it("generate sends the name and the list to regenerate only when given", async () => {
    const fake = fakeApi({ "POST /shopping-lists/generate": () => json(makeList(), 200) });
    const { result } = renderHook(() => useGenerateShoppingList(), { wrapper: clientWrapper() });
    await act(async () => {
      await result.current.mutateAsync({ from: "2026-09-28", to: "2026-10-04", name: "Week", listId: "l9" });
    });
    expect(fake.callsTo("POST", "/shopping-lists/generate")[0]?.body).toEqual({ from: "2026-09-28", to: "2026-10-04", name: "Week", list_id: "l9" });
  });

  it("update stores the answer and marks both list keys stale; delete drops the detail", async () => {
    const renamed = makeList({ name: "Renamed" });
    fakeApi({ "PATCH /shopping-lists/:id": () => json(renamed), "DELETE /shopping-lists/:id": () => noContent() });
    const queryClient = testQueryClient();
    queryClient.setQueryData(shoppingKeys.mine, { pages: [], pageParams: [] });
    queryClient.setQueryData(shoppingKeys.partner, { pages: [], pageParams: [] });
    queryClient.setQueryData(shoppingKeys.detail(L), base());
    const update = renderHook(() => useUpdateShoppingList(L), { wrapper: clientWrapper(queryClient) });
    const remove = renderHook(() => useDeleteShoppingList(), { wrapper: clientWrapper(queryClient) });

    await act(async () => {
      await update.result.current.mutateAsync({ name: "Renamed" });
    });
    expect(queryClient.getQueryData(shoppingKeys.detail(L))).toEqual(renamed);
    expect(queryClient.getQueryState(shoppingKeys.partner)?.isInvalidated).toBe(true);

    await act(async () => {
      await remove.result.current.mutateAsync(L);
    });
    expect(queryClient.getQueryData(shoppingKeys.detail(L))).toBeUndefined();
  });
});

describe("useCheckItem", () => {
  it("checks at once, then keeps the server's item with its new version", async () => {
    let release!: () => void;
    const fake = fakeApi({
      "GET /shopping-lists/:id": () => json(base()),
      "PATCH /shopping-lists/:id/items/:item": () =>
        new Promise<Response>((resolve) => {
          release = () => resolve(json(makeItem({ id: "i1", name: "Milk", checked: true, checked_by: "u1", version: 2 })));
        }),
    });
    const { result } = renderHook(() => ({ list: useShoppingList(L), check: useCheckItem(L, "u1") }), { wrapper: clientWrapper() });
    await waitFor(() => expect(state(result.current.list.data)).toEqual(["Milk:-", "Eggs:-"]));

    act(() => result.current.check.mutate({ itemId: "i1", checked: true }));
    await waitFor(() => expect(state(result.current.list.data)).toEqual(["Milk:x", "Eggs:-"]));
    expect(result.current.check.isPending).toBe(true);
    expect(fake.callsTo("PATCH", "/shopping-lists/l1/items/i1")[0]?.body).toEqual({ checked: true });

    await act(async () => release());
    await waitFor(() => expect(result.current.check.isSuccess).toBe(true));
    expect(result.current.list.data?.items[0]).toMatchObject({ checked: true, checked_by: "u1", version: 2 });
    expect(fake.callsTo("GET", "/shopping-lists/l1")).toHaveLength(1);
  });

  it("unchecks the same way, sending checked: false and no version", async () => {
    const checked = makeList({ items: [makeItem({ id: "i1", checked: true, checked_by: "u1", version: 4 })] });
    const fake = fakeApi({ "GET /shopping-lists/:id": () => json(checked), "PATCH /shopping-lists/:id/items/:item": () => json(makeItem({ id: "i1", version: 5 })) });
    const { result } = renderHook(() => ({ list: useShoppingList(L), check: useCheckItem(L, "u1") }), { wrapper: clientWrapper() });
    await waitFor(() => expect(result.current.list.data?.items[0]?.checked).toBe(true));
    act(() => result.current.check.mutate({ itemId: "i1", checked: false }));
    await waitFor(() => expect(result.current.list.data?.items[0]).toMatchObject({ checked: false, checked_by: null }));
    expect(fake.callsTo("PATCH", "/shopping-lists/l1/items/i1")[0]?.body).toEqual({ checked: false });
  });

  it("leaves the last tap in charge when an earlier answer arrives late", async () => {
    const releases: Array<(checked: boolean) => void> = [];
    fakeApi({
      "GET /shopping-lists/:id": () => json(base()),
      "PATCH /shopping-lists/:id/items/:item": () =>
        new Promise<Response>((resolve) => {
          releases.push((checked) => resolve(json(makeItem({ id: "i1", name: "Milk", checked, checked_by: checked ? "u1" : null, version: releases.length + 1 }))));
        }),
    });
    const { result } = renderHook(() => ({ list: useShoppingList(L), check: useCheckItem(L, "u1") }), { wrapper: clientWrapper() });
    await waitFor(() => expect(state(result.current.list.data)).toEqual(["Milk:-", "Eggs:-"]));

    act(() => result.current.check.mutate({ itemId: "i1", checked: true }));
    await waitFor(() => expect(state(result.current.list.data)).toEqual(["Milk:x", "Eggs:-"]));
    act(() => result.current.check.mutate({ itemId: "i1", checked: false }));
    await waitFor(() => expect(state(result.current.list.data)).toEqual(["Milk:-", "Eggs:-"]));
    await waitFor(() => expect(releases).toHaveLength(2));

    await act(async () => releases[0]?.(true));
    expect(state(result.current.list.data)).toEqual(["Milk:-", "Eggs:-"]);
    await act(async () => releases[1]?.(false));
    await waitFor(() => expect(result.current.check.isSuccess).toBe(true));
    expect(result.current.list.data?.items[0]).toMatchObject({ checked: false, checked_by: null });
  });

  it("puts the item back and reports the failure when the server refuses", async () => {
    const onFailure = vi.fn();
    fakeApi({ "GET /shopping-lists/:id": () => json(base()), "PATCH /shopping-lists/:id/items/:item": () => problem(500, "internal_error") });
    const { result } = renderHook(() => ({ list: useShoppingList(L), check: useCheckItem(L, "u1", onFailure) }), { wrapper: clientWrapper() });
    await waitFor(() => expect(state(result.current.list.data)).toEqual(["Milk:-", "Eggs:-"]));
    act(() => result.current.check.mutate({ itemId: "i1", checked: true }));
    await waitFor(() => expect(result.current.check.isError).toBe(true));
    expect(state(result.current.list.data)).toEqual(["Milk:-", "Eggs:-"]);
    expect(onFailure).toHaveBeenCalledTimes(1);
  });
});

describe("useAddItem", () => {
  it("shows the new item at once and swaps it for the server's", async () => {
    let release!: () => void;
    const fake = fakeApi({
      "GET /shopping-lists/:id": () => json(base()),
      "POST /shopping-lists/:id/items": () =>
        new Promise<Response>((resolve) => {
          release = () => resolve(json(makeItem({ id: "real1", name: "Bread", category: "other", position: 2 }), 201));
        }),
    });
    const { result } = renderHook(() => ({ list: useShoppingList(L), add: useAddItem(L) }), { wrapper: clientWrapper() });
    await waitFor(() => expect(result.current.list.data).toBeDefined());

    act(() => result.current.add.mutate({ name: "Bread" }));
    await waitFor(() => expect(result.current.list.data?.items.map((i) => i.name)).toEqual(["Milk", "Eggs", "Bread"]));
    expect(result.current.list.data?.items[2]?.id.startsWith("optimistic:")).toBe(true);
    expect(fake.callsTo("POST", "/shopping-lists/l1/items")[0]?.body).toEqual({ name: "Bread" });

    await act(async () => release());
    await waitFor(() => expect(result.current.add.isSuccess).toBe(true));
    expect(result.current.list.data?.items.map((i) => i.id)).toEqual(["i1", "i2", "real1"]);
  });

  it("adds an ingredient by id", async () => {
    const fake = fakeApi({ "GET /shopping-lists/:id": () => json(base()), "POST /shopping-lists/:id/items": () => json(makeItem({ id: "k1", name: "Kale" }), 201) });
    const { result } = renderHook(() => ({ list: useShoppingList(L), add: useAddItem(L) }), { wrapper: clientWrapper() });
    await waitFor(() => expect(result.current.list.data).toBeDefined());
    await act(async () => {
      await result.current.add.mutateAsync({ name: "Kale", ingredientId: "ing1", category: "produce" });
    });
    expect(fake.callsTo("POST", "/shopping-lists/l1/items")[0]?.body).toEqual({ name: "Kale", ingredient_id: "ing1" });
  });

  it("removes the temporary item and reports the failure when the server refuses", async () => {
    const onFailure = vi.fn();
    fakeApi({ "GET /shopping-lists/:id": () => json(base()), "POST /shopping-lists/:id/items": () => problem(404, "not_found") });
    const { result } = renderHook(() => ({ list: useShoppingList(L), add: useAddItem(L, onFailure) }), { wrapper: clientWrapper() });
    await waitFor(() => expect(result.current.list.data).toBeDefined());
    act(() => result.current.add.mutate({ name: "Bread" }));
    await waitFor(() => expect(result.current.add.isError).toBe(true));
    expect(result.current.list.data?.items.map((i) => i.name)).toEqual(["Milk", "Eggs"]);
    expect(onFailure).toHaveBeenCalledTimes(1);
  });
});

describe("useEditItem", () => {
  it("sends the item's version with the changes and keeps the server's answer", async () => {
    const fake = fakeApi({
      "GET /shopping-lists/:id": () => json(base()),
      "PATCH /shopping-lists/:id/items/:item": () => json(makeItem({ id: "i1", name: "Oat milk", quantity: 2, unit: "piece", version: 2 })),
    });
    const { result } = renderHook(() => ({ list: useShoppingList(L), edit: useEditItem(L) }), { wrapper: clientWrapper() });
    await waitFor(() => expect(result.current.list.data).toBeDefined());
    const item = result.current.list.data!.items[0]!;
    await act(async () => {
      await result.current.edit.mutateAsync({ item, changes: { name: "Oat milk", quantity: 2, unit: "piece" } });
    });
    expect(fake.callsTo("PATCH", "/shopping-lists/l1/items/i1")[0]?.body).toEqual({ version: 1, name: "Oat milk", quantity: 2, unit: "piece" });
    expect(result.current.list.data?.items[0]).toMatchObject({ name: "Oat milk", version: 2 });
  });

  it("stores the other person's version when the edit is stale, and rejects with the conflict", async () => {
    const theirs = makeItem({ id: "i1", name: "Bananas", version: 3 });
    fakeApi({ "GET /shopping-lists/:id": () => json(base()), "PATCH /shopping-lists/:id/items/:item": () => problem(409, "version_conflict", { current: theirs }) });
    const { result } = renderHook(() => ({ list: useShoppingList(L), edit: useEditItem(L) }), { wrapper: clientWrapper() });
    await waitFor(() => expect(result.current.list.data).toBeDefined());
    const item = result.current.list.data!.items[0]!;
    let caught: unknown;
    await act(async () => {
      caught = await result.current.edit.mutateAsync({ item, changes: { name: "Oat milk" } }).catch((e: unknown) => e);
    });
    expect(caught).toBeInstanceOf(ApiError);
    expect(caught).toMatchObject({ status: 409, code: "version_conflict" });
    expect(result.current.list.data?.items[0]).toMatchObject({ name: "Bananas", version: 3 });
  });

  it("does not store anything for a conflict that carries no item", async () => {
    fakeApi({ "GET /shopping-lists/:id": () => json(base()), "PATCH /shopping-lists/:id/items/:item": () => problem(409, "version_conflict") });
    const { result } = renderHook(() => ({ list: useShoppingList(L), edit: useEditItem(L) }), { wrapper: clientWrapper() });
    await waitFor(() => expect(result.current.list.data).toBeDefined());
    await act(async () => {
      await result.current.edit.mutateAsync({ item: result.current.list.data!.items[0]!, changes: { name: "X" } }).catch(() => undefined);
    });
    expect(result.current.list.data?.items[0]?.name).toBe("Milk");
  });
});

describe("useDeleteItem", () => {
  it("removes the item once the server has deleted it", async () => {
    const fake = fakeApi({ "GET /shopping-lists/:id": () => json(base()), "DELETE /shopping-lists/:id/items/:item": () => noContent() });
    const { result } = renderHook(() => ({ list: useShoppingList(L), remove: useDeleteItem(L) }), { wrapper: clientWrapper() });
    await waitFor(() => expect(result.current.list.data).toBeDefined());
    await act(async () => {
      await result.current.remove.mutateAsync("i1");
    });
    expect(fake.callsTo("DELETE", "/shopping-lists/l1/items/i1")).toHaveLength(1);
    expect(result.current.list.data?.items.map((i) => i.name)).toEqual(["Eggs"]);
  });
});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/features/shopping/list-cache.test.ts src/features/shopping/queries.test.tsx`
Expected: FAIL: `Failed to resolve import "./list-cache"` and `"./queries"`.

- [ ] **Step 3: Write the implementation**

**Create `web/src/features/shopping/list-cache.ts`**

```ts
import type { IngredientCategory } from "@/lib/ingredient-categories";
import type { ShoppingItem, ShoppingList } from "./items";

export const OPTIMISTIC_PREFIX = "optimistic:";

/** An item the server has not answered for yet: it can be seen, but not checked or edited, because the server does not know its id. */
export const isOptimistic = (item: ShoppingItem): boolean => item.id.startsWith(OPTIMISTIC_PREFIX);

const byPosition = (a: ShoppingItem, b: ShoppingItem) => a.position - b.position;

/** Checks or unchecks an item. Only the server bumps `version`, so it is left alone. */
export function withChecked(list: ShoppingList, itemId: string, checked: boolean, by: string | null): ShoppingList {
  return { ...list, items: list.items.map((item) => (item.id === itemId ? { ...item, checked, checked_by: checked ? by : null } : item)) };
}

export function withAddedItem(list: ShoppingList, item: ShoppingItem): ShoppingList {
  return { ...list, items: [...list.items, item] };
}

export function withoutItem(list: ShoppingList, itemId: string): ShoppingList {
  return list.items.some((item) => item.id === itemId) ? { ...list, items: list.items.filter((item) => item.id !== itemId) } : list;
}

/** Takes in what the server says about an item, unless the cache already holds a newer version of it. */
export function withItem(list: ShoppingList, item: ShoppingItem): ShoppingList {
  const existing = list.items.find((i) => i.id === item.id);
  if (!existing) return { ...list, items: [...list.items, item].sort(byPosition) };
  if (existing.version > item.version) return list;
  return { ...list, items: list.items.map((i) => (i.id === item.id ? item : i)) };
}

/** Puts back exactly `item`, whatever the versions, for a rollback. */
export function restoreItem(list: ShoppingList, item: ShoppingItem): ShoppingList {
  return list.items.some((i) => i.id === item.id)
    ? { ...list, items: list.items.map((i) => (i.id === item.id ? item : i)) }
    : { ...list, items: [...list.items, item].sort(byPosition) };
}

export function optimisticItem(listId: string, input: { name: string; ingredientId?: string; category?: IngredientCategory }, position: number, id: string): ShoppingItem {
  const now = new Date().toISOString();
  return {
    id,
    list_id: listId,
    ingredient_id: input.ingredientId ?? null,
    name: input.name,
    quantity: null,
    unit: null,
    category: input.category ?? "other",
    checked: false,
    checked_by: null,
    position,
    version: 1,
    origin: "manual",
    created_at: now,
    updated_at: now,
  };
}

/** An event at or below the version already held describes something the cache has already seen (often this person's own change). */
export function isStaleEvent(list: ShoppingList | undefined, itemId: string, version: number): boolean {
  const held = list?.items.find((item) => item.id === itemId);
  return held !== undefined && held.version >= version;
}
```

**Create `web/src/features/shopping/queries.ts`**

```ts
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, unwrap } from "@/lib/api/client";
import { ApiError } from "@/lib/api/problem";
import type { components } from "@/lib/api/schema.gen";
import type { IngredientCategory } from "@/lib/ingredient-categories";
import type { ShoppingItem, ShoppingList } from "./items";
import { OPTIMISTIC_PREFIX, optimisticItem, restoreItem, withAddedItem, withChecked, withItem, withoutItem } from "./list-cache";

/** Per-resource keys. The list keys prefix nothing else, so refreshing lists never touches an open list. */
export const shoppingKeys = {
  mine: ["shopping", "mine"] as const,
  partner: ["shopping", "partner"] as const,
  detail: (id: string) => ["shopping", "detail", id] as const,
};

export function useShoppingLists(scope: "mine" | "partner") {
  return useInfiniteQuery({
    queryKey: scope === "mine" ? shoppingKeys.mine : shoppingKeys.partner,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) => {
      const query = { cursor: pageParam, limit: 20 };
      return scope === "mine" ? unwrap(api.GET("/shopping-lists", { params: { query } })) : unwrap(api.GET("/partner/shopping-lists", { params: { query } }));
    },
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  });
}

export function useShoppingList(id: string) {
  return useQuery({ queryKey: shoppingKeys.detail(id), queryFn: () => unwrap(api.GET("/shopping-lists/{id}", { params: { path: { id } } })) });
}

export function useCreateShoppingList() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: { name: string; shared_with_partner?: boolean }) => unwrap(api.POST("/shopping-lists", { body })),
    onSuccess: (list) => {
      queryClient.setQueryData(shoppingKeys.detail(list.id), list);
      void queryClient.invalidateQueries({ queryKey: shoppingKeys.mine });
    },
  });
}

/** Builds a list from the plan for `from` to `to` (at most 92 days), or regenerates `listId` from that range. */
export function useGenerateShoppingList() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (vars: { from: string; to: string; name?: string; listId?: string }) =>
      unwrap(api.POST("/shopping-lists/generate", { body: { from: vars.from, to: vars.to, ...(vars.name ? { name: vars.name } : {}), ...(vars.listId ? { list_id: vars.listId } : {}) } })),
    onSuccess: (list) => {
      queryClient.setQueryData(shoppingKeys.detail(list.id), list);
      void queryClient.invalidateQueries({ queryKey: shoppingKeys.mine });
    },
  });
}

export function useUpdateShoppingList(id: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: { name?: string; shared_with_partner?: boolean }) => unwrap(api.PATCH("/shopping-lists/{id}", { params: { path: { id } }, body })),
    onSuccess: (list) => {
      queryClient.setQueryData(shoppingKeys.detail(id), list);
      void queryClient.invalidateQueries({ queryKey: shoppingKeys.mine });
      void queryClient.invalidateQueries({ queryKey: shoppingKeys.partner });
    },
  });
}

export function useDeleteShoppingList() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (id: string): Promise<void> => {
      await unwrap(api.DELETE("/shopping-lists/{id}", { params: { path: { id } } }));
    },
    onSuccess: (_data, id) => {
      queryClient.removeQueries({ queryKey: shoppingKeys.detail(id) });
      void queryClient.invalidateQueries({ queryKey: shoppingKeys.mine });
    },
  });
}

const patchItem = (listId: string, itemId: string, body: components["schemas"]["UpdateShoppingItemRequest"]) =>
  unwrap(api.PATCH("/shopping-lists/{id}/items/{item_id}", { params: { path: { id: listId, item_id: itemId } }, body }));

/** Check-off: applied at once, last write wins on the server (no version), put back if the server refuses. */
export function useCheckItem(listId: string, userId: string | null, onFailure?: (error: Error) => void) {
  const queryClient = useQueryClient();
  const key = shoppingKeys.detail(listId);
  const mutationKey = ["shopping", "check", listId] as const;
  // With another tap still in flight, this answer is about a state the screen has already moved past: leave the screen to the last tap.
  const otherTapInFlight = () => queryClient.isMutating({ mutationKey }) > 1;
  return useMutation<ShoppingItem, Error, { itemId: string; checked: boolean }, { previous: ShoppingItem | undefined }>({
    mutationKey,
    mutationFn: ({ itemId, checked }) => patchItem(listId, itemId, { checked }),
    onMutate: async ({ itemId, checked }) => {
      await queryClient.cancelQueries({ queryKey: key });
      const list = queryClient.getQueryData<ShoppingList>(key);
      if (list) queryClient.setQueryData(key, withChecked(list, itemId, checked, userId));
      return { previous: list?.items.find((item) => item.id === itemId) };
    },
    onError: (error, _vars, context) => {
      const previous = context?.previous;
      if (previous && !otherTapInFlight()) queryClient.setQueryData<ShoppingList>(key, (list) => (list ? restoreItem(list, previous) : list));
      onFailure?.(error);
    },
    onSuccess: (item) => {
      if (!otherTapInFlight()) queryClient.setQueryData<ShoppingList>(key, (list) => (list ? withItem(list, item) : list));
    },
  });
}

let temporaryIds = 0;

/** Quick-add: the item shows at once under a temporary id, then is swapped for the server's. */
export function useAddItem(listId: string, onFailure?: (error: Error) => void) {
  const queryClient = useQueryClient();
  const key = shoppingKeys.detail(listId);
  return useMutation<ShoppingItem, Error, { name: string; ingredientId?: string; category?: IngredientCategory }, { temporaryId: string }>({
    mutationFn: ({ name, ingredientId }) =>
      unwrap(api.POST("/shopping-lists/{id}/items", { params: { path: { id: listId } }, body: ingredientId ? { name, ingredient_id: ingredientId } : { name } })),
    onMutate: async (input) => {
      await queryClient.cancelQueries({ queryKey: key });
      temporaryIds += 1;
      const temporaryId = `${OPTIMISTIC_PREFIX}${temporaryIds}`;
      const list = queryClient.getQueryData<ShoppingList>(key);
      if (list) {
        const position = list.items.reduce((highest, item) => Math.max(highest, item.position), -1) + 1;
        queryClient.setQueryData(key, withAddedItem(list, optimisticItem(listId, input, position, temporaryId)));
      }
      return { temporaryId };
    },
    onError: (error, _vars, context) => {
      if (context) queryClient.setQueryData<ShoppingList>(key, (list) => (list ? withoutItem(list, context.temporaryId) : list));
      onFailure?.(error);
    },
    onSuccess: (item, _vars, context) =>
      queryClient.setQueryData<ShoppingList>(key, (list) => (list ? withItem(context ? withoutItem(list, context.temporaryId) : list, item) : list)),
  });
}

const isItem = (value: unknown): value is ShoppingItem =>
  typeof value === "object" && value !== null && typeof (value as ShoppingItem).id === "string" && typeof (value as ShoppingItem).version === "number";

/** The item a stale edit was refused for, from the `version_conflict` problem's `current` member; null for any other failure. */
export function conflictingItem(error: Error): ShoppingItem | null {
  return error instanceof ApiError && error.code === "version_conflict" && isItem(error.body.current) ? error.body.current : null;
}

type ItemChanges = { name?: string; quantity?: number | null; unit?: "g" | "ml" | "piece" | null; category?: IngredientCategory };

/**
 * Edits name, quantity, unit or category. The API needs the item's version and refuses a stale one with the current item,
 * which is stored here so the form can show the other person's version before the error reaches the caller.
 */
export function useEditItem(listId: string) {
  const queryClient = useQueryClient();
  const key = shoppingKeys.detail(listId);
  return useMutation<ShoppingItem, Error, { item: ShoppingItem; changes: ItemChanges }>({
    mutationFn: ({ item, changes }) => patchItem(listId, item.id, { version: item.version, ...changes }),
    onSuccess: (item) => queryClient.setQueryData<ShoppingList>(key, (list) => (list ? withItem(list, item) : list)),
    onError: (error) => {
      const current = conflictingItem(error);
      if (current) queryClient.setQueryData<ShoppingList>(key, (list) => (list ? withItem(list, current) : list));
    },
  });
}

export function useDeleteItem(listId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (itemId: string): Promise<void> => {
      await unwrap(api.DELETE("/shopping-lists/{id}/items/{item_id}", { params: { path: { id: listId, item_id: itemId } } }));
    },
    onSuccess: (_data, itemId) => queryClient.setQueryData<ShoppingList>(shoppingKeys.detail(listId), (list) => (list ? withoutItem(list, itemId) : list)),
  });
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/features/shopping`
Expected: PASS.

- [ ] **Step 5: Lint and commit**

```bash
make lint-web && make test-web
git add web/src/features/shopping
git commit -m "feat(web): add the shopping queries with optimistic check-off and quick-add and conflict-aware edits"
```

### Task 3: Live updates over the event stream

**Files:**
- Create: `web/src/test/fake-event-source.ts`, `web/src/features/shopping/list-events.ts`, `web/src/features/shopping/use-list-events.ts`
- Test: `web/src/features/shopping/list-events.test.ts`, `web/src/features/shopping/use-list-events.test.tsx`

**Interfaces:**
- Consumes: `shoppingKeys`, `useShoppingList` (`queries`); `isStaleEvent`, `withoutItem` (`list-cache`); `ShoppingList` (`items`).
- Produces:
  - `EVENT_TYPES = ["item_changed", "item_deleted", "list_changed", "list_deleted"]`, `type ListEvent = { type; list_id: string; item_id?: string; version?: number }`, `parseListEvent(text: string): ListEvent | null` (`null` for anything that is not one of the API's events: bad JSON, an unknown type, a missing `list_id`).
  - `useListEvents(listId: string, onDeleted: () => void): void`: opens `new EventSource("/api/shopping-lists/{listId}/events")` for as long as the caller is mounted and applies the API's rules to the cached list (`shoppingKeys.detail(listId)`): `item_changed` refetches unless the cached item is already at that version or higher; `item_deleted` removes the cached item (no refetch) unless the cache holds a higher version; `list_changed` refetches; `list_deleted` closes the stream and calls `onDeleted`; `open` refetches (every open, including reconnects); `error` refetches only when the connection is closed for good (`readyState` 2, which is what a browser does after an HTTP error such as `404` when access is gone), never while it is merely reconnecting. Events for another list, and malformed ones, are ignored. The latest `onDeleted` is always the one called.
  - Test-only: `FakeEventSource` with `install()`, `instances`, `last`, and per instance `open()`, `message(type, data)`, `raw(type, text)`, `fail(permanent: boolean)`, `close()`, `url`, `readyState`.

- [ ] **Step 1: Write the failing tests**

**Create `web/src/features/shopping/list-events.test.ts`**

```ts
import { describe, expect, it } from "vitest";
import { EVENT_TYPES, parseListEvent } from "./list-events";

describe("parseListEvent", () => {
  it("reads the four event types the API sends", () => {
    expect(EVENT_TYPES).toEqual(["item_changed", "item_deleted", "list_changed", "list_deleted"]);
    expect(parseListEvent('{"type":"item_changed","list_id":"l1","item_id":"i1","version":4}')).toEqual({ type: "item_changed", list_id: "l1", item_id: "i1", version: 4 });
    expect(parseListEvent('{"type":"item_deleted","list_id":"l1","item_id":"i1","version":2}')).toEqual({ type: "item_deleted", list_id: "l1", item_id: "i1", version: 2 });
    expect(parseListEvent('{"type":"list_changed","list_id":"l1"}')).toEqual({ type: "list_changed", list_id: "l1" });
    expect(parseListEvent('{"type":"list_deleted","list_id":"l1"}')).toEqual({ type: "list_deleted", list_id: "l1" });
  });

  it.each(["", "not json", "null", "42", '"text"', "[]", '{"type":"item_changed"}', '{"type":"nonsense","list_id":"l1"}', '{"list_id":"l1"}', '{"type":7,"list_id":"l1"}'])("refuses %j", (text) => {
    expect(parseListEvent(text)).toBeNull();
  });

  it("drops members of the wrong type instead of trusting them", () => {
    expect(parseListEvent('{"type":"item_changed","list_id":"l1","item_id":5,"version":"4"}')).toEqual({ type: "item_changed", list_id: "l1" });
  });
});
```

**Create `web/src/features/shopping/use-list-events.test.tsx`**

```tsx
// @vitest-environment jsdom
import { renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { FakeEventSource } from "@/test/fake-event-source";
import { fakeApi, json, problem } from "@/test/fake-api";
import { makeItem, makeList } from "@/test/shopping-fixtures";
import { clientWrapper } from "@/test/render";
import { useShoppingList } from "./queries";
import { useListEvents } from "./use-list-events";

const L = "l1";
const held = () => makeList({ items: [makeItem({ id: "i1", name: "Milk", version: 3 }), makeItem({ id: "i2", name: "Eggs", version: 1, position: 1 })] });
const settle = () => new Promise((resolve) => setTimeout(resolve, 60));

async function setup(onDeleted = vi.fn()) {
  FakeEventSource.install();
  let server = held();
  const fake = fakeApi({ "GET /shopping-lists/:id": () => json(server) });
  const view = renderHook(
    () => {
      useListEvents(L, onDeleted);
      return useShoppingList(L);
    },
    { wrapper: clientWrapper() },
  );
  await waitFor(() => expect(view.result.current.data).toBeDefined());
  const source = FakeEventSource.last!;
  const fetches = () => fake.callsTo("GET", "/shopping-lists/l1").length;
  return { ...view, source, fetches, serve: (next: ReturnType<typeof held>) => void (server = next), onDeleted };
}

describe("useListEvents", () => {
  it("opens the list's stream on the same origin and closes it when the screen goes away", async () => {
    const { source, unmount } = await setup();
    expect(FakeEventSource.instances).toHaveLength(1);
    expect(source.url).toBe("/api/shopping-lists/l1/events");
    expect(source.readyState).not.toBe(2);
    unmount();
    expect(source.readyState).toBe(2);
  });

  it("refetches whenever the stream opens, first time or after a reconnect", async () => {
    const { source, fetches } = await setup();
    expect(fetches()).toBe(1);
    source.open();
    await waitFor(() => expect(fetches()).toBe(2));
    source.open();
    await waitFor(() => expect(fetches()).toBe(3));
  });

  it("ignores an event about a change the cache already holds, so a person's own check-off does not refetch or flicker", async () => {
    const { source, fetches } = await setup();
    source.message("item_changed", { type: "item_changed", list_id: L, item_id: "i1", version: 3 });
    source.message("item_changed", { type: "item_changed", list_id: L, item_id: "i1", version: 2 });
    await settle();
    expect(fetches()).toBe(1);
  });

  it("refetches for a newer version of an item, and shows what the partner did", async () => {
    const { source, fetches, serve, result } = await setup();
    serve(makeList({ items: [makeItem({ id: "i1", name: "Milk", version: 4, checked: true, checked_by: "u2" }), makeItem({ id: "i2", name: "Eggs", version: 1, position: 1 })] }));
    source.message("item_changed", { type: "item_changed", list_id: L, item_id: "i1", version: 4 });
    await waitFor(() => expect(result.current.data?.items[0]).toMatchObject({ checked: true, checked_by: "u2", version: 4 }));
    expect(fetches()).toBe(2);
  });

  it("refetches for an item it has never seen, which is how the partner's additions arrive", async () => {
    const { source, fetches, serve, result } = await setup();
    serve(makeList({ items: [...held().items, makeItem({ id: "i3", name: "Bread", position: 2 })] }));
    source.message("item_changed", { type: "item_changed", list_id: L, item_id: "i3", version: 1 });
    await waitFor(() => expect(result.current.data?.items.map((i) => i.name)).toEqual(["Milk", "Eggs", "Bread"]));
    expect(fetches()).toBe(2);
  });

  it("removes a deleted item without asking the server, unless the cache holds a newer version than the event", async () => {
    const { source, fetches, result } = await setup();
    source.message("item_deleted", { type: "item_deleted", list_id: L, item_id: "i1", version: 2 });
    await settle();
    expect(result.current.data?.items.map((i) => i.name)).toEqual(["Milk", "Eggs"]);

    source.message("item_deleted", { type: "item_deleted", list_id: L, item_id: "i1", version: 3 });
    await waitFor(() => expect(result.current.data?.items.map((i) => i.name)).toEqual(["Eggs"]));
    expect(fetches()).toBe(1);
  });

  it("refetches when the list is renamed or regenerated", async () => {
    const { source, fetches } = await setup();
    source.message("list_changed", { type: "list_changed", list_id: L });
    await waitFor(() => expect(fetches()).toBe(2));
  });

  it("closes the stream and tells the screen when the list is deleted", async () => {
    const onDeleted = vi.fn();
    const { source } = await setup(onDeleted);
    source.message("list_deleted", { type: "list_deleted", list_id: L });
    expect(onDeleted).toHaveBeenCalledTimes(1);
    expect(source.readyState).toBe(2);
  });

  it("refetches when the connection is lost for good (access gone), but not while it is only reconnecting", async () => {
    const { source, fetches } = await setup();
    source.fail(false);
    await settle();
    expect(fetches()).toBe(1);

    source.fail(true);
    await waitFor(() => expect(fetches()).toBe(2));
  });

  it("ignores another list's events and anything malformed", async () => {
    const { source, fetches, onDeleted } = await setup();
    source.message("list_deleted", { type: "list_deleted", list_id: "other" });
    source.message("item_changed", { type: "item_changed", list_id: "other", item_id: "i1", version: 9 });
    source.raw("item_changed", "not json");
    source.raw("list_deleted", "{}");
    await settle();
    expect(fetches()).toBe(1);
    expect(onDeleted).not.toHaveBeenCalled();
  });

  it("calls the latest onDeleted, not the one from when the stream was opened", async () => {
    const first = vi.fn();
    const second = vi.fn();
    FakeEventSource.install();
    fakeApi({ "GET /shopping-lists/:id": () => json(held()) });
    const { rerender } = renderHook(({ cb }) => useListEvents(L, cb), { wrapper: clientWrapper(), initialProps: { cb: first } });
    rerender({ cb: second });
    FakeEventSource.last!.message("list_deleted", { type: "list_deleted", list_id: L });
    expect(first).not.toHaveBeenCalled();
    expect(second).toHaveBeenCalledTimes(1);
    expect(FakeEventSource.instances).toHaveLength(1);
  });

  it("does not open a second stream when the screen re-renders", async () => {
    const { rerender } = await setup();
    rerender();
    rerender();
    expect(FakeEventSource.instances).toHaveLength(1);
  });

  it("still works when a refetch after the stream opens fails", async () => {
    FakeEventSource.install();
    let fail = false;
    const fake = fakeApi({ "GET /shopping-lists/:id": () => (fail ? problem(500, "internal_error") : json(held())) });
    const { result } = renderHook(
      () => {
        useListEvents(L, vi.fn());
        return useShoppingList(L);
      },
      { wrapper: clientWrapper() },
    );
    await waitFor(() => expect(result.current.data).toBeDefined());
    fail = true;
    FakeEventSource.last!.open();
    await waitFor(() => expect(fake.callsTo("GET", "/shopping-lists/l1")).toHaveLength(2));
    expect(result.current.data?.items).toHaveLength(2);
  });
});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/features/shopping/list-events.test.ts src/features/shopping/use-list-events.test.tsx`
Expected: FAIL: `Failed to resolve import "./list-events"`, `"./use-list-events"` and `"@/test/fake-event-source"`.

- [ ] **Step 3: Write the implementation**

**Create `web/src/test/fake-event-source.ts`** (test helper, no test of its own)

```ts
import { vi } from "vitest";

type Listener = (event: Event) => void;

/** A stand-in for the browser's `EventSource` that tests drive by hand. Install it with `FakeEventSource.install()`. */
export class FakeEventSource {
  static instances: FakeEventSource[] = [];
  readonly url: string;
  /** 0 connecting, 1 open, 2 closed, like the real one. */
  readyState = 0;
  private listeners = new Map<string, Set<Listener>>();

  constructor(url: string) {
    this.url = url;
    FakeEventSource.instances.push(this);
  }

  addEventListener(type: string, listener: Listener) {
    const set = this.listeners.get(type) ?? new Set<Listener>();
    set.add(listener);
    this.listeners.set(type, set);
  }

  removeEventListener(type: string, listener: Listener) {
    this.listeners.get(type)?.delete(listener);
  }

  close() {
    this.readyState = 2;
  }

  /** A named event with a JSON body, the way the API sends them. */
  message(type: string, data: unknown) {
    this.raw(type, JSON.stringify(data));
  }

  raw(type: string, text: string) {
    this.dispatch(new MessageEvent(type, { data: text }));
  }

  open() {
    this.readyState = 1;
    this.dispatch(new Event("open"));
  }

  /** The connection dropped. `permanent` is what a browser does after an HTTP error answer: it gives up (closed) instead of reconnecting. */
  fail(permanent: boolean) {
    this.readyState = permanent ? 2 : 0;
    this.dispatch(new Event("error"));
  }

  private dispatch(event: Event) {
    for (const listener of this.listeners.get(event.type) ?? []) listener(event);
  }

  static install() {
    FakeEventSource.instances = [];
    vi.stubGlobal("EventSource", FakeEventSource);
    return FakeEventSource;
  }

  static get last(): FakeEventSource | undefined {
    return FakeEventSource.instances.at(-1);
  }
}
```

**Create `web/src/features/shopping/list-events.ts`**

```ts
export const EVENT_TYPES = ["item_changed", "item_deleted", "list_changed", "list_deleted"] as const;
export type ListEventType = (typeof EVENT_TYPES)[number];

/** One event of a shopping list's stream: what changed, never the new content. */
export type ListEvent = { type: ListEventType; list_id: string; item_id?: string; version?: number };

/** Reads an event's `data`. Anything that is not one of the API's events is `null`, so a malformed one can never act. */
export function parseListEvent(text: string): ListEvent | null {
  let value: unknown;
  try {
    value = JSON.parse(text);
  } catch {
    return null;
  }
  if (typeof value !== "object" || value === null) return null;
  const { type, list_id, item_id, version } = value as Record<string, unknown>;
  if (typeof type !== "string" || !(EVENT_TYPES as readonly string[]).includes(type) || typeof list_id !== "string") return null;
  return { type: type as ListEventType, list_id, ...(typeof item_id === "string" ? { item_id } : {}), ...(typeof version === "number" ? { version } : {}) };
}
```

**Create `web/src/features/shopping/use-list-events.ts`**

```ts
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef } from "react";
import type { ShoppingList } from "./items";
import { isStaleEvent, withoutItem } from "./list-cache";
import { EVENT_TYPES, parseListEvent } from "./list-events";
import { shoppingKeys } from "./queries";

const CLOSED = 2;

/**
 * Keeps an open list live. The stream says what changed, not what it changed to, so this never patches items from an event:
 * it refetches the list, except for an event about a version the cache already holds (often this person's own change) and a
 * deleted item, which simply leaves the list. The same-origin proxy attaches the bearer, and the browser reconnects by itself.
 */
export function useListEvents(listId: string, onDeleted: () => void): void {
  const queryClient = useQueryClient();
  const latestOnDeleted = useRef(onDeleted);
  useEffect(() => {
    latestOnDeleted.current = onDeleted;
  });

  useEffect(() => {
    const key = shoppingKeys.detail(listId);
    const refetch = () => void queryClient.invalidateQueries({ queryKey: key });
    const source = new EventSource(`/api/shopping-lists/${listId}/events`);

    const onMessage = (message: Event) => {
      const event = parseListEvent((message as MessageEvent<string>).data);
      if (!event || event.list_id !== listId) return;
      const list = queryClient.getQueryData<ShoppingList>(key);
      switch (event.type) {
        case "item_changed":
          if (event.item_id === undefined || event.version === undefined || !isStaleEvent(list, event.item_id, event.version)) refetch();
          break;
        case "item_deleted": {
          const held = list?.items.find((item) => item.id === event.item_id);
          if (list && held && (event.version === undefined || held.version <= event.version)) queryClient.setQueryData<ShoppingList>(key, withoutItem(list, held.id));
          break;
        }
        case "list_changed":
          refetch();
          break;
        case "list_deleted":
          source.close();
          latestOnDeleted.current();
          break;
      }
    };

    for (const type of EVENT_TYPES) source.addEventListener(type, onMessage);
    source.addEventListener("open", refetch);
    // A closed connection is a refusal (the list is gone or no longer shared): ask, and the refetch says which. A connecting one is the browser retrying.
    source.addEventListener("error", () => {
      if (source.readyState === CLOSED) refetch();
    });
    return () => source.close();
  }, [listId, queryClient]);
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/features/shopping`
Expected: PASS. If "ignores an event about a change the cache already holds" sees a second fetch, check that `setup()` waited for the first fetch before grabbing `FakeEventSource.last` (it does: `await waitFor(... data defined)`), and that the test client has no `refetchOnMount` surprise: `testQueryClient` only sets `retry: false`.

- [ ] **Step 5: Lint and commit**

```bash
make lint-web && make test-web
git add web/src/test/fake-event-source.ts web/src/features/shopping
git commit -m "feat(web): keep an open shopping list live over the event stream"
```

---

### Task 4: The shopping lists page: lists, a new list, generate from the plan

**Files:**
- Create: `web/src/features/shopping/range.ts`, `web/src/features/shopping/shopping-lists.tsx`, `web/src/features/shopping/new-list-dialog.tsx`, `web/src/features/shopping/generate-list-dialog.tsx`, `web/src/features/shopping/shopping-page.tsx`
- Modify: `web/src/app/(app)/shopping/page.tsx`
- Test: `web/src/features/shopping/range.test.ts`, `web/src/features/shopping/shopping-page.test.tsx`

**Interfaces:**
- Consumes: `useShoppingLists`, `useCreateShoppingList`, `useGenerateShoppingList` (`queries`); `rangeLabel`, `ShoppingListSummary` (`items`); `usePartnerLink`, `PARTNER_KEY` (`@/features/meals/queries`); `addDays`, `isIsoDate`, `startOfWeek`, `today` (`@/lib/dates`); `Field`, `Badge`, `Button`, `Dialog*`, `Tabs*`, `Skeleton`, `ErrorState`, `ApiError`, `problemMessage`.
- Produces:
  - `validateRange(from: string, to: string): { ok: true } | { ok: false; error: string }`: blank or not-a-date is "Choose a start and end date."; an end before the start is "The end date must be on or after the start date."; more than 92 days is "Pick at most 92 days." (92 days is allowed, 93 is not).
  - `ShoppingLists({ scope: "mine" | "partner"; emptyAction?: React.ReactNode })`: skeleton, error (with "Try again" unless the partner is unlinked, which marks `PARTNER_KEY` stale), an empty state (mine: "No shopping lists yet" with `emptyAction`; partner: "Nothing shared yet"), rows linking to `/shopping/{id}` with the name, the plan range when the list was generated, a "Shared" badge on mine, and "Load more".
  - `NewListDialog({ open, onOpenChange })`: "New shopping list": a Name field; creates and `router.push("/shopping/{id}")`.
  - `GenerateListDialog({ open, onOpenChange })`: "Generate from your plan": From and To date inputs (this week, Monday to Sunday, by default) and an optional name; validates with `validateRange`; generates and `router.push("/shopping/{id}")`; a server refusal is a toast.
  - `ShoppingPage()`: the "New list" and "Generate from plan" buttons; with an active partner, "Mine" / "Partner's" tabs (none otherwise).

- [ ] **Step 1: Write the failing tests**

**Create `web/src/features/shopping/range.test.ts`**

```ts
import { describe, expect, it } from "vitest";
import { validateRange } from "./range";

describe("validateRange", () => {
  it("accepts a week, a single day and exactly 92 days", () => {
    expect(validateRange("2026-09-28", "2026-10-04")).toEqual({ ok: true });
    expect(validateRange("2026-09-28", "2026-09-28")).toEqual({ ok: true });
    expect(validateRange("2026-01-01", "2026-04-02")).toEqual({ ok: true });
  });

  it("refuses 93 days", () => {
    expect(validateRange("2026-01-01", "2026-04-03")).toEqual({ ok: false, error: "Pick at most 92 days." });
  });

  it("refuses an end before the start", () => {
    expect(validateRange("2026-10-04", "2026-09-28")).toEqual({ ok: false, error: "The end date must be on or after the start date." });
  });

  it.each([
    ["", "2026-10-04"],
    ["2026-09-28", ""],
    ["", ""],
    ["2026-02-30", "2026-03-05"],
    ["tomorrow", "2026-03-05"],
  ])("refuses %j to %j as not a range", (from, to) => {
    expect(validateRange(from, to)).toEqual({ ok: false, error: "Choose a start and end date." });
  });

  it("counts days across a daylight-saving change and a year end", () => {
    expect(validateRange("2026-03-01", "2026-05-31")).toEqual({ ok: true });
    expect(validateRange("2026-12-01", "2027-02-28")).toEqual({ ok: true });
    expect(validateRange("2026-12-01", "2027-03-03")).toEqual({ ok: false, error: "Pick at most 92 days." });
  });
});
```

**Create `web/src/features/shopping/shopping-page.test.tsx`**

```tsx
// @vitest-environment jsdom
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { PARTNER_KEY } from "@/features/meals/queries";
import { fakeApi, json, problem } from "@/test/fake-api";
import { partnership } from "@/test/fixtures";
import { renderWithClient } from "@/test/render";
import { makeList, makeListSummary } from "@/test/shopping-fixtures";
import { ShoppingPage } from "./shopping-page";

const push = vi.fn();
vi.mock("next/navigation", () => ({ useRouter: () => ({ push: (url: string) => push(url), replace: vi.fn() }) }));

// Wednesday 30 September 2026: the week is Monday 28 September to Sunday 4 October.
beforeEach(() => vi.useFakeTimers({ toFake: ["Date"], now: new Date(2026, 8, 30, 10, 0) }));
afterEach(() => {
  vi.useRealTimers();
  push.mockReset();
});

const page = (items: ReturnType<typeof makeListSummary>[], next_cursor: string | null = null) => json({ items, next_cursor });
const noPartner = { "GET /partner": () => problem(404, "partner_not_linked") };

async function partnerSettled(queryClient: ReturnType<typeof renderWithClient>["queryClient"]) {
  await waitFor(() => expect(queryClient.getQueryState(PARTNER_KEY)?.status).toBe("success"));
}

describe("ShoppingPage lists", () => {
  it("lists my lists with the plan range they were built from and a shared badge, and no tabs without a partner", async () => {
    fakeApi({
      ...noPartner,
      "GET /shopping-lists": () =>
        page([makeListSummary({ shared_with_partner: true, source_from: "2026-09-28", source_to: "2026-10-04" }), makeListSummary({ id: "l2", name: "Party" })]),
    });
    const { queryClient } = renderWithClient(<ShoppingPage />);
    const weekly = await screen.findByRole("link", { name: /Weekly shop/ });
    expect(weekly).toHaveAttribute("href", "/shopping/l1");
    expect(weekly).toHaveTextContent("Sep 28 – Oct 4");
    expect(screen.getByRole("link", { name: /Party/ })).toHaveAttribute("href", "/shopping/l2");
    expect(screen.getByText("Shared")).toBeInTheDocument();
    await partnerSettled(queryClient);
    expect(screen.queryByRole("tab")).not.toBeInTheDocument();
  });

  it("asks for the next page with the API's cursor", async () => {
    const fake = fakeApi({
      ...noPartner,
      "GET /shopping-lists": (req) => (req.search.get("cursor") === "c1" ? page([makeListSummary({ id: "l2", name: "Party" })]) : page([makeListSummary()], "c1")),
    });
    renderWithClient(<ShoppingPage />);
    await userEvent.click(await screen.findByRole("button", { name: "Load more" }));
    expect(await screen.findByRole("link", { name: /Party/ })).toBeInTheDocument();
    expect(fake.callsTo("GET", "/shopping-lists")[1]?.search.get("cursor")).toBe("c1");
  });

  it("invites you to start a list when there are none, with the same actions as the header", async () => {
    fakeApi({ ...noPartner, "GET /shopping-lists": () => page([]) });
    renderWithClient(<ShoppingPage />);
    expect(await screen.findByText("No shopping lists yet")).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "Generate from plan" })).toHaveLength(2);
  });

  it("offers a retry when the lists cannot load", async () => {
    let fail = true;
    fakeApi({ ...noPartner, "GET /shopping-lists": () => (fail ? problem(500, "internal_error") : page([makeListSummary()])) });
    renderWithClient(<ShoppingPage />);
    expect(await screen.findByText("Something went wrong. Try again.")).toBeInTheDocument();
    fail = false;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByRole("link", { name: /Weekly shop/ })).toBeInTheDocument();
  });

  it("shows the partner's shared lists under their own tab", async () => {
    fakeApi({
      "GET /partner": () => json(partnership()),
      "GET /shopping-lists": () => page([makeListSummary()]),
      "GET /partner/shopping-lists": () => page([makeListSummary({ id: "p1", name: "Barbecue", shared_with_partner: true })]),
    });
    renderWithClient(<ShoppingPage />);
    await userEvent.click(await screen.findByRole("tab", { name: "Partner's" }));
    const link = await screen.findByRole("link", { name: /Barbecue/ });
    expect(link).toHaveAttribute("href", "/shopping/p1");
    expect(screen.queryByRole("link", { name: /Weekly shop/ })).not.toBeInTheDocument();
  });

  it("drops the partner tab, and shows my lists again, when the partner unlinks while it is open", async () => {
    let linked = true;
    fakeApi({
      "GET /partner": () => (linked ? json(partnership()) : problem(404, "partner_not_linked")),
      "GET /shopping-lists": () => page([makeListSummary()]),
      "GET /partner/shopping-lists": () => problem(404, "partner_not_linked"),
    });
    renderWithClient(<ShoppingPage />);
    const tab = await screen.findByRole("tab", { name: "Partner's" });
    linked = false;
    await userEvent.click(tab);
    await waitFor(() => expect(screen.queryByRole("tab")).not.toBeInTheDocument());
    expect(await screen.findByRole("link", { name: /Weekly shop/ })).toBeInTheDocument();
  });
});

describe("New list", () => {
  it("creates an empty list and opens it", async () => {
    const fake = fakeApi({ ...noPartner, "GET /shopping-lists": () => page([makeListSummary()]), "POST /shopping-lists": () => json(makeList({ id: "new1", name: "Party" }), 201) });
    renderWithClient(<ShoppingPage />);
    await userEvent.click(await screen.findByRole("button", { name: "New list" }));
    const dialog = await screen.findByRole("dialog", { name: "New shopping list" });
    await userEvent.type(within(dialog).getByLabelText("Name"), "Party");
    await userEvent.click(within(dialog).getByRole("button", { name: "Create list" }));

    await waitFor(() => expect(push).toHaveBeenCalledWith("/shopping/new1"));
    expect(fake.callsTo("POST", "/shopping-lists")[0]?.body).toEqual({ name: "Party" });
  });

  it("asks for a name and sends nothing without one", async () => {
    const fake = fakeApi({ ...noPartner, "GET /shopping-lists": () => page([makeListSummary()]) });
    renderWithClient(<ShoppingPage />);
    await userEvent.click(await screen.findByRole("button", { name: "New list" }));
    const dialog = await screen.findByRole("dialog", { name: "New shopping list" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Create list" }));
    expect(await within(dialog).findByText("Give the list a name.")).toBeInTheDocument();
    expect(fake.callsTo("POST", "/shopping-lists")).toHaveLength(0);
  });
});

describe("Generate from plan", () => {
  async function openGenerate() {
    await userEvent.click((await screen.findAllByRole("button", { name: "Generate from plan" }))[0]!);
    return screen.findByRole("dialog", { name: "Generate from your plan" });
  }

  it("defaults to this week, generates, and opens the new list", async () => {
    const fake = fakeApi({ ...noPartner, "GET /shopping-lists": () => page([makeListSummary()]), "POST /shopping-lists/generate": () => json(makeList({ id: "gen1" }), 201) });
    renderWithClient(<ShoppingPage />);
    const dialog = await openGenerate();
    expect(within(dialog).getByLabelText("From")).toHaveValue("2026-09-28");
    expect(within(dialog).getByLabelText("To")).toHaveValue("2026-10-04");
    await userEvent.click(within(dialog).getByRole("button", { name: "Generate list" }));

    await waitFor(() => expect(push).toHaveBeenCalledWith("/shopping/gen1"));
    expect(fake.callsTo("POST", "/shopping-lists/generate")[0]?.body).toEqual({ from: "2026-09-28", to: "2026-10-04" });
  });

  it("sends the range and the name the person chose", async () => {
    const fake = fakeApi({ ...noPartner, "GET /shopping-lists": () => page([makeListSummary()]), "POST /shopping-lists/generate": () => json(makeList({ id: "gen1" }), 201) });
    renderWithClient(<ShoppingPage />);
    const dialog = await openGenerate();
    fireEvent.change(within(dialog).getByLabelText("From"), { target: { value: "2026-10-05" } });
    fireEvent.change(within(dialog).getByLabelText("To"), { target: { value: "2026-10-07" } });
    await userEvent.type(within(dialog).getByLabelText("List name (optional)"), "Midweek");
    await userEvent.click(within(dialog).getByRole("button", { name: "Generate list" }));
    await waitFor(() => expect(fake.callsTo("POST", "/shopping-lists/generate")).toHaveLength(1));
    expect(fake.callsTo("POST", "/shopping-lists/generate")[0]?.body).toEqual({ from: "2026-10-05", to: "2026-10-07", name: "Midweek" });
  });

  it.each([
    ["an end before the start", "2026-10-07", "2026-10-05", "The end date must be on or after the start date."],
    ["more than 92 days", "2026-01-01", "2026-04-03", "Pick at most 92 days."],
    ["a blank date", "", "2026-10-05", "Choose a start and end date."],
  ])("refuses %s inline and sends nothing", async (_name, from, to, message) => {
    const fake = fakeApi({ ...noPartner, "GET /shopping-lists": () => page([makeListSummary()]) });
    renderWithClient(<ShoppingPage />);
    const dialog = await openGenerate();
    fireEvent.change(within(dialog).getByLabelText("From"), { target: { value: from } });
    fireEvent.change(within(dialog).getByLabelText("To"), { target: { value: to } });
    await userEvent.click(within(dialog).getByRole("button", { name: "Generate list" }));
    expect(await within(dialog).findByText(message)).toBeInTheDocument();
    expect(fake.callsTo("POST", "/shopping-lists/generate")).toHaveLength(0);
  });

  it("keeps the dialog open when the server refuses", async () => {
    fakeApi({ ...noPartner, "GET /shopping-lists": () => page([makeListSummary()]), "POST /shopping-lists/generate": () => problem(400, "plan_range_too_long") });
    renderWithClient(<ShoppingPage />);
    const dialog = await openGenerate();
    await userEvent.click(within(dialog).getByRole("button", { name: "Generate list" }));
    await waitFor(() => expect(within(dialog).getByRole("button", { name: "Generate list" })).toBeEnabled());
    expect(push).not.toHaveBeenCalled();
    expect(screen.getByRole("dialog", { name: "Generate from your plan" })).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/features/shopping/range.test.ts src/features/shopping/shopping-page.test.tsx`
Expected: FAIL: `Failed to resolve import "./range"` and `"./shopping-page"`.

- [ ] **Step 3: Write the implementation**

**Create `web/src/features/shopping/range.ts`**

```ts
import { addDays, isIsoDate } from "@/lib/dates";

/** The API builds a list from at most 92 days of the plan, inclusive of both ends. */
export const MAX_RANGE_DAYS = 92;

export function validateRange(from: string, to: string): { ok: true } | { ok: false; error: string } {
  if (!isIsoDate(from) || !isIsoDate(to)) return { ok: false, error: "Choose a start and end date." };
  // ISO dates sort as text, so a plain comparison is a date comparison.
  if (to < from) return { ok: false, error: "The end date must be on or after the start date." };
  if (to > addDays(from, MAX_RANGE_DAYS - 1)) return { ok: false, error: `Pick at most ${MAX_RANGE_DAYS} days.` };
  return { ok: true };
}
```

**Create `web/src/features/shopping/shopping-lists.tsx`**

```tsx
"use client";

import { useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useEffect } from "react";
import { ErrorState } from "@/components/error-state";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { PARTNER_KEY } from "@/features/meals/queries";
import { ApiError, problemMessage } from "@/lib/api/problem";
import { rangeLabel, type ShoppingListSummary } from "./items";
import { useShoppingLists } from "./queries";

type Scope = "mine" | "partner";

export function ShoppingLists({ scope, emptyAction }: { scope: Scope; emptyAction?: React.ReactNode }) {
  const query = useShoppingLists(scope);
  const queryClient = useQueryClient();
  const unlinked = query.error instanceof ApiError && query.error.code === "partner_not_linked";

  // The partner unlinked while their tab was open: refresh the link so the tab goes away.
  useEffect(() => {
    if (unlinked) void queryClient.invalidateQueries({ queryKey: PARTNER_KEY });
  }, [unlinked, queryClient]);

  if (query.isPending) {
    return (
      <ul aria-label="Loading lists" className="grid gap-3">
        {[0, 1, 2].map((n) => (
          <li key={n}>
            <Skeleton className="h-[4.5rem] rounded-xl" />
          </li>
        ))}
      </ul>
    );
  }
  if (query.data === undefined) return <ErrorState message={problemMessage(query.error)} onRetry={unlinked ? undefined : () => void query.refetch()} />;

  const lists = query.data.pages.flatMap((page) => page.items);
  if (lists.length === 0) return <EmptyState scope={scope} action={emptyAction} />;

  return (
    <div className="grid gap-3">
      <ul className="grid gap-3">
        {lists.map((list) => (
          <ListRow key={list.id} list={list} scope={scope} />
        ))}
      </ul>
      {query.isError ? <ErrorState message={problemMessage(query.error)} onRetry={() => void query.fetchNextPage()} /> : null}
      {query.hasNextPage ? (
        <Button type="button" variant="outline" className="justify-self-center" disabled={query.isFetchingNextPage} onClick={() => void query.fetchNextPage()}>
          {query.isFetchingNextPage ? "Loading…" : "Load more"}
        </Button>
      ) : null}
    </div>
  );
}

function ListRow({ list, scope }: { list: ShoppingListSummary; scope: Scope }) {
  const range = rangeLabel(list);
  return (
    <li className="bg-card flex items-center gap-3 rounded-xl border p-4">
      <Link href={`/shopping/${list.id}`} className="focus-visible:ring-ring/50 min-w-0 flex-1 rounded-md outline-none focus-visible:ring-3">
        <span className="block truncate font-medium">{list.name}</span>
        <span className="text-muted-foreground block truncate text-sm">{range ? `From your plan · ${range}` : "Your own list"}</span>
      </Link>
      {scope === "mine" && list.shared_with_partner ? <Badge variant="secondary">Shared</Badge> : null}
      {scope === "partner" ? <Badge variant="secondary">Shared by your partner</Badge> : null}
    </li>
  );
}

function EmptyState({ scope, action }: { scope: Scope; action?: React.ReactNode }) {
  return (
    <div className="bg-card flex flex-col items-center gap-3 rounded-xl border px-6 py-10 text-center">
      <p className="font-medium">{scope === "mine" ? "No shopping lists yet" : "Nothing shared yet"}</p>
      <p className="text-muted-foreground max-w-sm text-sm">
        {scope === "mine" ? "Generate a list from the meals in your plan, or start an empty one." : "Lists your partner shares with you show up here, and you can both tick things off."}
      </p>
      {scope === "mine" ? action : null}
    </div>
  );
}
```

**Create `web/src/features/shopping/new-list-dialog.tsx`**

```tsx
"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { toast } from "sonner";
import { Field } from "@/components/field";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { problemMessage } from "@/lib/api/problem";
import { useCreateShoppingList } from "./queries";

export function NewListDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New shopping list</DialogTitle>
          <DialogDescription>An empty list you fill in yourself.</DialogDescription>
        </DialogHeader>
        <NewListForm onDone={() => onOpenChange(false)} />
      </DialogContent>
    </Dialog>
  );
}

/** Mounted only while the dialog is open, so it starts fresh every time. */
function NewListForm({ onDone }: { onDone: () => void }) {
  const router = useRouter();
  const create = useCreateShoppingList();
  const [error, setError] = useState<string | undefined>();

  return (
    <form
      noValidate
      className="grid gap-4"
      onSubmit={(event) => {
        event.preventDefault();
        const name = String(new FormData(event.currentTarget).get("new-list-name") ?? "").trim();
        if (!name) return setError("Give the list a name.");
        if (name.length > 200) return setError("Use at most 200 characters.");
        setError(undefined);
        create.mutate(
          { name },
          {
            onSuccess: (list) => {
              onDone();
              router.push(`/shopping/${list.id}`);
            },
            onError: (e) => toast.error(problemMessage(e)),
          },
        );
      }}
    >
      <Field name="new-list-name" label="Name" autoComplete="off" error={error} />
      <Button type="submit" size="lg" className="h-11 text-base md:text-sm" disabled={create.isPending}>
        {create.isPending ? "Creating…" : "Create list"}
      </Button>
    </form>
  );
}
```

**Create `web/src/features/shopping/generate-list-dialog.tsx`**

```tsx
"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { toast } from "sonner";
import { Field } from "@/components/field";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { problemMessage } from "@/lib/api/problem";
import { addDays, startOfWeek, today } from "@/lib/dates";
import { useGenerateShoppingList } from "./queries";
import { validateRange } from "./range";

export function GenerateListDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Generate from your plan</DialogTitle>
          <DialogDescription>Adds up the ingredients of every meal planned in these dates and groups them by aisle.</DialogDescription>
        </DialogHeader>
        <GenerateForm onDone={() => onOpenChange(false)} />
      </DialogContent>
    </Dialog>
  );
}

/** Mounted only while the dialog is open, so it starts fresh (this week) every time. */
function GenerateForm({ onDone }: { onDone: () => void }) {
  const router = useRouter();
  const generate = useGenerateShoppingList();
  const [from, setFrom] = useState(() => startOfWeek(today()));
  const [to, setTo] = useState(() => addDays(startOfWeek(today()), 6));
  const [error, setError] = useState<string | undefined>();

  return (
    <form
      noValidate
      className="grid gap-4"
      onSubmit={(event) => {
        event.preventDefault();
        const checked = validateRange(from, to);
        if (!checked.ok) return setError(checked.error);
        setError(undefined);
        const name = String(new FormData(event.currentTarget).get("generate-name") ?? "").trim();
        generate.mutate(
          { from, to, ...(name ? { name } : {}) },
          {
            onSuccess: (list) => {
              onDone();
              router.push(`/shopping/${list.id}`);
            },
            onError: (e) => toast.error(problemMessage(e)),
          },
        );
      }}
    >
      <div className="grid grid-cols-2 gap-3">
        <Field name="generate-from" label="From" type="date" value={from} onChange={(e) => setFrom(e.target.value)} />
        <Field name="generate-to" label="To" type="date" value={to} onChange={(e) => setTo(e.target.value)} />
      </div>
      {error ? (
        <p role="alert" className="text-destructive text-sm">
          {error}
        </p>
      ) : null}
      <Field name="generate-name" label="List name (optional)" autoComplete="off" hint="Leave blank to name it after the dates." />
      <Button type="submit" size="lg" className="h-11 text-base md:text-sm" disabled={generate.isPending}>
        {generate.isPending ? "Generating…" : "Generate list"}
      </Button>
    </form>
  );
}
```

**Create `web/src/features/shopping/shopping-page.tsx`**

```tsx
"use client";

import { ListPlus, Plus } from "lucide-react";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { usePartnerLink } from "@/features/meals/queries";
import { GenerateListDialog } from "./generate-list-dialog";
import { NewListDialog } from "./new-list-dialog";
import { ShoppingLists } from "./shopping-lists";

/** My shopping lists, and, while I have a partner, the ones they share. */
export function ShoppingPage() {
  const link = usePartnerLink();
  const [tab, setTab] = useState<"mine" | "partner">("mine");
  const [creating, setCreating] = useState(false);
  const [generating, setGenerating] = useState(false);
  const partnerActive = link.data?.status === "active";

  const generateButton = (
    <Button type="button" onClick={() => setGenerating(true)}>
      <ListPlus aria-hidden />
      Generate from plan
    </Button>
  );

  return (
    <div className="grid gap-4">
      <div className="flex flex-wrap justify-end gap-2">
        <Button type="button" variant="outline" onClick={() => setCreating(true)}>
          <Plus aria-hidden />
          New list
        </Button>
        {generateButton}
      </div>
      {partnerActive ? (
        <Tabs value={tab} onValueChange={(value) => setTab(value === "partner" ? "partner" : "mine")}>
          <TabsList>
            <TabsTrigger value="mine">Mine</TabsTrigger>
            <TabsTrigger value="partner">Partner&apos;s</TabsTrigger>
          </TabsList>
          <TabsContent value="mine" className="mt-4">
            <ShoppingLists scope="mine" emptyAction={generateButton} />
          </TabsContent>
          <TabsContent value="partner" className="mt-4">
            <ShoppingLists scope="partner" />
          </TabsContent>
        </Tabs>
      ) : (
        <ShoppingLists scope="mine" emptyAction={generateButton} />
      )}
      <NewListDialog open={creating} onOpenChange={setCreating} />
      <GenerateListDialog open={generating} onOpenChange={setGenerating} />
    </div>
  );
}
```

**Replace the contents of `web/src/app/(app)/shopping/page.tsx`**

```tsx
import type { Metadata } from "next";
import { PageHeader } from "@/components/page-header";
import { ShoppingPage } from "@/features/shopping/shopping-page";

export const metadata: Metadata = { title: "Shopping" };

export default function Page() {
  return (
    <>
      <PageHeader title="Shopping" />
      <ShoppingPage />
    </>
  );
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/features/shopping/range.test.ts src/features/shopping/shopping-page.test.tsx`
Expected: PASS. Note that `Field` reads `value` and `onChange` for the two date inputs (controlled), and uses `name` as the id, so the labels "From" and "To" are unique inside the dialog; the page behind has no inputs with those labels.

- [ ] **Step 5: Lint, build and commit**

```bash
make lint-web && make test-web
cd web && npx next build && cd ..
git add web/src/features/shopping "web/src/app/(app)/shopping/page.tsx"
git commit -m "feat(web): add the shopping lists page with new list and generate from the plan"
```

### Task 5: List settings: rename, share, regenerate, delete

**Files:**
- Create: `web/src/features/shopping/list-settings-dialog.tsx`
- Test: `web/src/features/shopping/list-settings-dialog.test.tsx`

**Interfaces:**
- Consumes: `useUpdateShoppingList`, `useGenerateShoppingList`, `useDeleteShoppingList` (`queries`); `rangeLabel`, `ShoppingList` (`items`); `usePartnerLink` (`@/features/meals/queries`); `Field`, `Button`, `Dialog*`, `problemMessage`.
- Produces: `ListSettingsDialog({ list: ShoppingList; open: boolean; onOpenChange(open: boolean): void })`, for the owner only: "List settings" with a Name field, "Share with my partner" (while linked, or while already shared) and "Save" (sends only what changed; nothing to send just closes); when `list.source_from` and `list.source_to` are set, "Regenerate from plan" which first says what it does ("replaces the items generated from your plan ... items you added yourself stay ... checks on generated items are lost") and needs "Regenerate" to go ahead (`POST /shopping-lists/generate` with the list's stored range and `list_id`); "Delete list", which first asks ("Delete “{name}” and all its items? This can't be undone.") and needs "Delete list" again; it then returns to `/shopping`. A failed regenerate or delete shows its message in the dialog and changes nothing.

- [ ] **Step 1: Write the failing test**

**Create `web/src/features/shopping/list-settings-dialog.test.tsx`**

```tsx
// @vitest-environment jsdom
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { fakeApi, json, noContent, problem } from "@/test/fake-api";
import { partnership } from "@/test/fixtures";
import { renderWithClient } from "@/test/render";
import { makeList } from "@/test/shopping-fixtures";
import { ListSettingsDialog } from "./list-settings-dialog";

const replace = vi.fn();
vi.mock("next/navigation", () => ({ useRouter: () => ({ replace: (url: string) => replace(url), push: vi.fn() }) }));
afterEach(() => replace.mockReset());

const noPartner = { "GET /partner": () => problem(404, "partner_not_linked") };
const generated = () => makeList({ name: "Week shop", source_from: "2026-09-28", source_to: "2026-10-04" });

function open(list = makeList(), routes: Parameters<typeof fakeApi>[0] = noPartner) {
  const fake = fakeApi(routes);
  const onOpenChange = vi.fn();
  const view = renderWithClient(<ListSettingsDialog list={list} open onOpenChange={onOpenChange} />);
  return { fake, onOpenChange, ...view, dialog: () => screen.getByRole("dialog", { name: "List settings" }) };
}

describe("rename and share", () => {
  it("renames the list, sending only the name", async () => {
    const { fake, onOpenChange, dialog } = open(makeList(), { ...noPartner, "PATCH /shopping-lists/:id": () => json(makeList({ name: "Party" })) });
    await userEvent.clear(within(dialog()).getByLabelText("Name"));
    await userEvent.type(within(dialog()).getByLabelText("Name"), "Party");
    await userEvent.click(within(dialog()).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(fake.callsTo("PATCH", "/shopping-lists/l1")).toHaveLength(1));
    expect(fake.callsTo("PATCH", "/shopping-lists/l1")[0]?.body).toEqual({ name: "Party" });
    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
  });

  it("shares with the partner while linked, sending only the choice", async () => {
    const { fake, dialog } = open(makeList(), { "GET /partner": () => json(partnership()), "PATCH /shopping-lists/:id": () => json(makeList({ shared_with_partner: true })) });
    await userEvent.click(await within(dialog()).findByLabelText("Share with my partner"));
    await userEvent.click(within(dialog()).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(fake.callsTo("PATCH", "/shopping-lists/l1")).toHaveLength(1));
    expect(fake.callsTo("PATCH", "/shopping-lists/l1")[0]?.body).toEqual({ shared_with_partner: true });
  });

  it("hides the sharing choice when there is no partner and the list is not shared", async () => {
    const { queryClient, dialog } = open();
    await waitFor(() => expect(queryClient.getQueryState(["partner"])?.status).toBe("success"));
    expect(within(dialog()).queryByLabelText("Share with my partner")).not.toBeInTheDocument();
  });

  it("lets a shared list be unshared even after the partner is gone", async () => {
    const { dialog } = open(makeList({ shared_with_partner: true }));
    expect(await within(dialog()).findByLabelText("Share with my partner")).toBeChecked();
  });

  it("sends nothing and just closes when nothing changed", async () => {
    const { fake, onOpenChange, dialog } = open();
    await userEvent.click(within(dialog()).getByRole("button", { name: "Save" }));
    expect(fake.callsTo("PATCH", "/shopping-lists/l1")).toHaveLength(0);
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("asks for a name and sends nothing without one", async () => {
    const { fake, dialog } = open();
    await userEvent.clear(within(dialog()).getByLabelText("Name"));
    await userEvent.click(within(dialog()).getByRole("button", { name: "Save" }));
    expect(await within(dialog()).findByText("Give the list a name.")).toBeInTheDocument();
    expect(fake.callsTo("PATCH", "/shopping-lists/l1")).toHaveLength(0);
  });
});

describe("regenerate", () => {
  it("is not offered for a list built by hand", () => {
    const { dialog } = open();
    expect(within(dialog()).queryByRole("button", { name: "Regenerate from plan" })).not.toBeInTheDocument();
  });

  it("says what it replaces and what it keeps before doing anything, and does nothing if the person backs out", async () => {
    const { fake, dialog } = open(generated());
    expect(within(dialog()).getByText(/Built from your plan for Sep 28 – Oct 4/)).toBeInTheDocument();
    await userEvent.click(within(dialog()).getByRole("button", { name: "Regenerate from plan" }));
    expect(within(dialog()).getByText(/replaces the items generated from your plan/i)).toBeInTheDocument();
    expect(within(dialog()).getByText(/items you added yourself stay/i)).toBeInTheDocument();
    expect(within(dialog()).getByText(/checks on generated items are lost/i)).toBeInTheDocument();
    await userEvent.click(within(dialog()).getByRole("button", { name: "Cancel" }));
    expect(fake.callsTo("POST", "/shopping-lists/generate")).toHaveLength(0);
    expect(within(dialog()).getByRole("button", { name: "Regenerate from plan" })).toBeInTheDocument();
  });

  it("regenerates from the list's own dates, for that list", async () => {
    const { fake, onOpenChange, dialog } = open(generated(), { ...noPartner, "POST /shopping-lists/generate": () => json(generated(), 200) });
    await userEvent.click(within(dialog()).getByRole("button", { name: "Regenerate from plan" }));
    await userEvent.click(within(dialog()).getByRole("button", { name: "Regenerate" }));
    await waitFor(() => expect(fake.callsTo("POST", "/shopping-lists/generate")).toHaveLength(1));
    expect(fake.callsTo("POST", "/shopping-lists/generate")[0]?.body).toEqual({ from: "2026-09-28", to: "2026-10-04", list_id: "l1" });
    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
  });

  it("says why it failed and stays open", async () => {
    const { onOpenChange, dialog } = open(generated(), { ...noPartner, "POST /shopping-lists/generate": () => problem(404, "not_found") });
    await userEvent.click(within(dialog()).getByRole("button", { name: "Regenerate from plan" }));
    await userEvent.click(within(dialog()).getByRole("button", { name: "Regenerate" }));
    expect(await within(dialog()).findByText("That isn't available. It may have been removed, or it isn't shared with you.")).toBeInTheDocument();
    expect(onOpenChange).not.toHaveBeenCalledWith(false);
  });
});

describe("delete", () => {
  it("asks first, naming the list, and deletes only when confirmed, then returns to the lists", async () => {
    const { fake, dialog } = open(makeList({ name: "Party" }), { ...noPartner, "DELETE /shopping-lists/:id": () => noContent() });
    await userEvent.click(within(dialog()).getByRole("button", { name: "Delete list" }));
    expect(within(dialog()).getByText(/Delete “Party” and all its items\? This can't be undone\./)).toBeInTheDocument();
    expect(fake.callsTo("DELETE", "/shopping-lists/l1")).toHaveLength(0);
    await userEvent.click(within(dialog()).getByRole("button", { name: "Delete list" }));
    await waitFor(() => expect(replace).toHaveBeenCalledWith("/shopping"));
    expect(fake.callsTo("DELETE", "/shopping-lists/l1")).toHaveLength(1);
  });

  it("does nothing when the person backs out", async () => {
    const { fake, dialog } = open();
    await userEvent.click(within(dialog()).getByRole("button", { name: "Delete list" }));
    await userEvent.click(within(dialog()).getByRole("button", { name: "Cancel" }));
    expect(fake.callsTo("DELETE", "/shopping-lists/l1")).toHaveLength(0);
    expect(replace).not.toHaveBeenCalled();
  });

  it("stays put and says why when the delete fails", async () => {
    const { dialog } = open(makeList(), { ...noPartner, "DELETE /shopping-lists/:id": () => problem(500, "internal_error") });
    await userEvent.click(within(dialog()).getByRole("button", { name: "Delete list" }));
    await userEvent.click(within(dialog()).getByRole("button", { name: "Delete list" }));
    expect(await within(dialog()).findByText("Something went wrong. Try again.")).toBeInTheDocument();
    expect(replace).not.toHaveBeenCalled();
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/features/shopping/list-settings-dialog.test.tsx`
Expected: FAIL: `Failed to resolve import "./list-settings-dialog"`.

- [ ] **Step 3: Write the implementation**

**Create `web/src/features/shopping/list-settings-dialog.tsx`**

```tsx
"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { toast } from "sonner";
import { Field } from "@/components/field";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { usePartnerLink } from "@/features/meals/queries";
import { problemMessage } from "@/lib/api/problem";
import { rangeLabel, type ShoppingList } from "./items";
import { useDeleteShoppingList, useGenerateShoppingList, useUpdateShoppingList } from "./queries";

type Props = { list: ShoppingList; open: boolean; onOpenChange: (open: boolean) => void };

/** The owner's controls for a list: rename, share, regenerate from the plan, delete. The partner never sees this. */
export function ListSettingsDialog({ list, open, onOpenChange }: Props) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>List settings</DialogTitle>
          <DialogDescription>Only you can change these. Your partner can still tick items off and add to a shared list.</DialogDescription>
        </DialogHeader>
        <SettingsBody list={list} onDone={() => onOpenChange(false)} />
      </DialogContent>
    </Dialog>
  );
}

/** Mounted only while the dialog is open, so every open starts from the list as it is. */
function SettingsBody({ list, onDone }: { list: ShoppingList; onDone: () => void }) {
  const router = useRouter();
  const partner = usePartnerLink();
  const update = useUpdateShoppingList(list.id);
  const generate = useGenerateShoppingList();
  const remove = useDeleteShoppingList();
  const [shared, setShared] = useState(list.shared_with_partner);
  const [nameError, setNameError] = useState<string | undefined>();
  const [step, setStep] = useState<"idle" | "regenerate" | "delete">("idle");
  const [failure, setFailure] = useState<string | null>(null);

  const canShare = partner.data?.status === "active" || list.shared_with_partner;
  const range = rangeLabel(list);

  return (
    <div className="grid gap-6">
      <form
        noValidate
        className="grid gap-4"
        onSubmit={(event) => {
          event.preventDefault();
          const name = String(new FormData(event.currentTarget).get("list-name") ?? "").trim();
          if (!name) return setNameError("Give the list a name.");
          if (name.length > 200) return setNameError("Use at most 200 characters.");
          setNameError(undefined);
          const changes = { ...(name !== list.name ? { name } : {}), ...(shared !== list.shared_with_partner ? { shared_with_partner: shared } : {}) };
          if (Object.keys(changes).length === 0) return onDone();
          update.mutate(changes, {
            onSuccess: () => {
              toast.success("List saved.");
              onDone();
            },
            onError: (error) => toast.error(problemMessage(error)),
          });
        }}
      >
        <Field name="list-name" label="Name" autoComplete="off" defaultValue={list.name} error={nameError} />
        {canShare ? (
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" className="accent-primary size-4" checked={shared} onChange={(event) => setShared(event.target.checked)} />
            Share with my partner
          </label>
        ) : null}
        <div>
          <Button type="submit" disabled={update.isPending}>
            {update.isPending ? "Saving…" : "Save"}
          </Button>
        </div>
      </form>

      {range ? (
        <section className="grid gap-2 border-t pt-4">
          <p className="text-sm">Built from your plan for {range}.</p>
          {step === "regenerate" ? (
            <div className="grid gap-2 text-sm">
              <p>
                Regenerating replaces the items generated from your plan with the plan as it is now. Items you added yourself stay. Checks on generated items are lost.
              </p>
              <div className="flex gap-2">
                <Button
                  type="button"
                  disabled={generate.isPending}
                  onClick={() => {
                    setFailure(null);
                    generate.mutate(
                      { from: list.source_from ?? "", to: list.source_to ?? "", listId: list.id },
                      {
                        onSuccess: () => {
                          toast.success("List regenerated.");
                          onDone();
                        },
                        onError: (error) => setFailure(problemMessage(error)),
                      },
                    );
                  }}
                >
                  {generate.isPending ? "Regenerating…" : "Regenerate"}
                </Button>
                <Button type="button" variant="outline" onClick={() => setStep("idle")}>
                  Cancel
                </Button>
              </div>
            </div>
          ) : (
            <div>
              <Button type="button" variant="outline" onClick={() => setStep("regenerate")}>
                Regenerate from plan
              </Button>
            </div>
          )}
        </section>
      ) : null}

      <section className="grid gap-2 border-t pt-4">
        {step === "delete" ? (
          <div className="grid gap-2 text-sm">
            <p>Delete “{list.name}” and all its items? This can&apos;t be undone.</p>
            <div className="flex gap-2">
              <Button
                type="button"
                variant="destructive"
                disabled={remove.isPending}
                onClick={() => {
                  setFailure(null);
                  remove.mutate(list.id, {
                    onSuccess: () => {
                      toast.success("List deleted.");
                      router.replace("/shopping");
                    },
                    onError: (error) => setFailure(problemMessage(error)),
                  });
                }}
              >
                {remove.isPending ? "Deleting…" : "Delete list"}
              </Button>
              <Button type="button" variant="outline" onClick={() => setStep("idle")}>
                Cancel
              </Button>
            </div>
          </div>
        ) : (
          <div>
            <Button type="button" variant="destructive" onClick={() => setStep("delete")}>
              Delete list
            </Button>
          </div>
        )}
      </section>

      {failure ? (
        <p role="alert" className="text-destructive text-sm">
          {failure}
        </p>
      ) : null}
    </div>
  );
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd web && npx vitest run src/features/shopping/list-settings-dialog.test.tsx`
Expected: PASS. If `getByRole("button", { name: "Delete list" })` finds two buttons after the first click, that is the confirmation button replacing the first one (the `step === "delete"` branch renders only the confirm pair), so the test's second click targets the destructive confirm.

- [ ] **Step 5: Lint and commit**

```bash
make lint-web && make test-web
git add web/src/features/shopping/list-settings-dialog.tsx web/src/features/shopping/list-settings-dialog.test.tsx
git commit -m "feat(web): add list settings: rename, share, regenerate from the plan and delete, each with its warning"
```

---

### Task 6: The list screen: items by aisle, check-off, quick-add, edit, live

**Files:**
- Create: `web/src/features/shopping/item-row.tsx`, `web/src/features/shopping/quick-add.tsx`, `web/src/features/shopping/edit-item-dialog.tsx`, `web/src/features/shopping/shopping-list-view.tsx`, `web/src/app/(app)/shopping/[id]/page.tsx`
- Test: `web/src/features/shopping/item-row.test.tsx`, `web/src/features/shopping/quick-add.test.tsx`, `web/src/features/shopping/edit-item-dialog.test.tsx`, `web/src/features/shopping/shopping-list-view.test.tsx`

**Interfaces:**
- Consumes: everything from Tasks 1 to 5; `useMe` (`@/features/auth/use-me`); `usePartnerLink`; `IngredientSearch`, `Ingredient` (`@/components/ingredient-search/...`); `PageHeader`; `Field`, `NativeSelect`, `Badge`, `Button`, `Dialog*`, `ErrorState`, `Skeleton`; `categoryLabel`, `CATEGORIES`; `parseDecimal`.
- Produces:
  - `ItemRow({ item, checkedByName: string | null; onToggle(item); onEdit(item) })`: a labelled checkbox (the label holds the name, "Checked by {name}" when given, and the quantity), struck through when checked; an "Edit {name}" button; an item that has no server id yet (`isOptimistic`) has a disabled checkbox and no edit button.
  - `QuickAdd({ onAddText(name: string), onAddIngredient(ingredient: Ingredient) })`: an "Add an item" text field with an "Add" button (a blank entry says "Type what to add." and adds nothing; a good one is added and the field clears), and the shared ingredient search labelled "Or add from ingredients".
  - `EditItemDialog({ item: ShoppingItem | null; listId: string; onClose(): void })`: "Edit item" with Name, Quantity (blank is none), Unit (none, g, ml, piece) and Category; "Save" sends the version the form was opened on with the four fields (nothing changed just closes), so a change made elsewhere meanwhile is a conflict, never a silent overwrite, and a live update does not wipe what is being typed; a conflict shows "Someone else changed this item. Review it and save again." and the form restarts on the other person's version; "Remove item" deletes it. Mount it with a `key` per item so it starts fresh.
  - `ShoppingListView({ id: string })`: loads the list and keeps it live (`useListEvents`); the list's name (with its plan range), a badge ("Shared with {partner}" on my shared list, "{partner}'s list" on theirs), "{done} of {total} checked", a "List settings" button for the owner (`ListSettingsDialog`), the quick-add, items grouped by aisle (`groupByCategory`), and "No items yet" when empty. Checking an item is optimistic (`useCheckItem`, attributed to me); a failure toasts. A checked item is attributed "Checked by {partner}" only when someone other than me checked it. Losing the list (a `404` from any fetch, even with data cached, or a `list_deleted` event) replaces everything with a message and a link back; other load failures offer "Try again".
  - Route `/shopping/[id]`.

- [ ] **Step 1: Write the failing tests**

**Create `web/src/features/shopping/item-row.test.tsx`**

```tsx
// @vitest-environment jsdom
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { makeItem } from "@/test/shopping-fixtures";
import { ItemRow } from "./item-row";

function show(item = makeItem(), checkedByName: string | null = null) {
  const onToggle = vi.fn();
  const onEdit = vi.fn();
  render(
    <ul>
      <ItemRow item={item} checkedByName={checkedByName} onToggle={onToggle} onEdit={onEdit} />
    </ul>,
  );
  return { onToggle, onEdit };
}

describe("ItemRow", () => {
  it("shows the name and quantity in the checkbox's label, and toggles", async () => {
    const item = makeItem({ name: "Flour", quantity: 500, unit: "g" });
    const { onToggle } = show(item);
    const box = screen.getByRole("checkbox", { name: /Flour/ });
    expect(box).not.toBeChecked();
    expect(screen.getByText("500 g")).toBeInTheDocument();
    await userEvent.click(box);
    expect(onToggle).toHaveBeenCalledWith(item);
  });

  it("shows a checked item struck through, and who checked it when that is someone else", () => {
    show(makeItem({ name: "Milk", checked: true, checked_by: "u2" }), "Sam");
    expect(screen.getByRole("checkbox", { name: /Milk/ })).toBeChecked();
    expect(screen.getByText("Milk")).toHaveClass("line-through");
    expect(screen.getByText("Checked by Sam")).toBeInTheDocument();
  });

  it("does not attribute a check to anyone unless told to", () => {
    show(makeItem({ checked: true, checked_by: "u1" }), null);
    expect(screen.queryByText(/Checked by/)).not.toBeInTheDocument();
  });

  it("opens the editor from its button", async () => {
    const item = makeItem({ name: "Milk" });
    const { onEdit } = show(item);
    await userEvent.click(screen.getByRole("button", { name: "Edit Milk" }));
    expect(onEdit).toHaveBeenCalledWith(item);
  });

  it("cannot be checked or edited before the server has answered for it", () => {
    show(makeItem({ id: "optimistic:1", name: "Bread" }));
    expect(screen.getByRole("checkbox", { name: /Bread/ })).toBeDisabled();
    expect(screen.queryByRole("button", { name: "Edit Bread" })).not.toBeInTheDocument();
  });
});
```

**Create `web/src/features/shopping/quick-add.test.tsx`**

```tsx
// @vitest-environment jsdom
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { fakeApi, json } from "@/test/fake-api";
import { makeIngredient } from "@/test/fixtures";
import { renderWithClient } from "@/test/render";
import { QuickAdd } from "./quick-add";

function show() {
  const onAddText = vi.fn();
  const onAddIngredient = vi.fn();
  fakeApi({ "GET /ingredients": () => json({ items: [makeIngredient({ name: "Kale", category: "produce" })], next_cursor: null }) });
  renderWithClient(<QuickAdd onAddText={onAddText} onAddIngredient={onAddIngredient} />);
  return { onAddText, onAddIngredient };
}

describe("QuickAdd", () => {
  it("adds what was typed, trimmed, and clears the field so the next one can follow", async () => {
    const { onAddText } = show();
    await userEvent.type(screen.getByLabelText("Add an item"), "  Bin bags ");
    await userEvent.click(screen.getByRole("button", { name: "Add" }));
    expect(onAddText).toHaveBeenCalledWith("Bin bags");
    expect(screen.getByLabelText("Add an item")).toHaveValue("");
  });

  it("adds on Enter", async () => {
    const { onAddText } = show();
    await userEvent.type(screen.getByLabelText("Add an item"), "Milk{Enter}");
    expect(onAddText).toHaveBeenCalledWith("Milk");
  });

  it("says what to do, and adds nothing, for a blank entry", async () => {
    const { onAddText } = show();
    await userEvent.type(screen.getByLabelText("Add an item"), "   ");
    await userEvent.click(screen.getByRole("button", { name: "Add" }));
    expect(await screen.findByText("Type what to add.")).toBeInTheDocument();
    expect(onAddText).not.toHaveBeenCalled();
  });

  it("clears the reminder once something good is added", async () => {
    show();
    await userEvent.click(screen.getByRole("button", { name: "Add" }));
    expect(await screen.findByText("Type what to add.")).toBeInTheDocument();
    await userEvent.type(screen.getByLabelText("Add an item"), "Eggs{Enter}");
    await waitFor(() => expect(screen.queryByText("Type what to add.")).not.toBeInTheDocument());
  });

  it("adds an ingredient picked from the shared search", async () => {
    const { onAddIngredient } = show();
    await userEvent.click(screen.getByRole("combobox", { name: "Or add from ingredients" }));
    await userEvent.click(await screen.findByRole("option", { name: /Kale/ }));
    expect(onAddIngredient).toHaveBeenCalledWith(expect.objectContaining({ name: "Kale", category: "produce" }));
  });
});
```

**Create `web/src/features/shopping/edit-item-dialog.test.tsx`**

```tsx
// @vitest-environment jsdom
import { QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { fakeApi, json, noContent, problem } from "@/test/fake-api";
import { testQueryClient } from "@/test/render";
import { makeItem, makeList } from "@/test/shopping-fixtures";
import { EditItemDialog } from "./edit-item-dialog";
import { shoppingKeys, useShoppingList } from "./queries";

/** Renders the dialog for the item currently in the cached list, the way the screen does: the dialog follows the cache. */
function Live({ listId, itemId, onClose }: { listId: string; itemId: string; onClose: () => void }) {
  const list = useShoppingList(listId);
  const item = list.data?.items.find((i) => i.id === itemId) ?? null;
  return <EditItemDialog key={item ? `${item.id}` : "none"} item={item} listId={listId} onClose={onClose} />;
}

function show(routes: Parameters<typeof fakeApi>[0] = {}, item = makeItem({ id: "i1", name: "Milk", quantity: 2, unit: "piece", category: "dairy_eggs", version: 1 })) {
  const list = makeList({ items: [item] });
  const fake = fakeApi({ "GET /shopping-lists/:id": () => json(list), ...routes });
  const queryClient = testQueryClient();
  queryClient.setQueryData(shoppingKeys.detail("l1"), list);
  const onClose = vi.fn();
  render(
    <QueryClientProvider client={queryClient}>
      <Live listId="l1" itemId="i1" onClose={onClose} />
    </QueryClientProvider>,
  );
  return { fake, onClose, queryClient, dialog: () => screen.getByRole("dialog", { name: "Edit item" }) };
}

describe("EditItemDialog", () => {
  it("shows the item as it is", async () => {
    const { dialog } = show();
    expect(await screen.findByRole("dialog", { name: "Edit item" })).toBeInTheDocument();
    expect(within(dialog()).getByLabelText("Name")).toHaveValue("Milk");
    expect(within(dialog()).getByLabelText("Quantity")).toHaveValue("2");
    expect(within(dialog()).getByLabelText("Unit")).toHaveValue("piece");
    expect(within(dialog()).getByLabelText("Category")).toHaveValue("dairy_eggs");
  });

  it("saves the four fields together with the item's version, then closes", async () => {
    const { fake, onClose, dialog } = show({ "PATCH /shopping-lists/:id/items/:item": () => json(makeItem({ id: "i1", name: "Oat milk", quantity: 1.5, unit: "ml", version: 2 })) });
    await screen.findByRole("dialog", { name: "Edit item" });
    await userEvent.clear(within(dialog()).getByLabelText("Name"));
    await userEvent.type(within(dialog()).getByLabelText("Name"), "Oat milk");
    await userEvent.clear(within(dialog()).getByLabelText("Quantity"));
    await userEvent.type(within(dialog()).getByLabelText("Quantity"), "1,5");
    await userEvent.selectOptions(within(dialog()).getByLabelText("Unit"), "ml");
    await userEvent.click(within(dialog()).getByRole("button", { name: "Save" }));

    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(fake.callsTo("PATCH", "/shopping-lists/l1/items/i1")[0]?.body).toEqual({ version: 1, name: "Oat milk", quantity: 1.5, unit: "ml", category: "dairy_eggs" });
  });

  it("clears a quantity and a unit that were blanked, sending null rather than zero", async () => {
    const { fake, dialog } = show({ "PATCH /shopping-lists/:id/items/:item": () => json(makeItem({ id: "i1", version: 2 })) });
    await screen.findByRole("dialog", { name: "Edit item" });
    await userEvent.clear(within(dialog()).getByLabelText("Quantity"));
    await userEvent.selectOptions(within(dialog()).getByLabelText("Unit"), "");
    await userEvent.click(within(dialog()).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(fake.callsTo("PATCH", "/shopping-lists/l1/items/i1")).toHaveLength(1));
    expect(fake.callsTo("PATCH", "/shopping-lists/l1/items/i1")[0]?.body).toEqual({ version: 1, name: "Milk", quantity: null, unit: null, category: "dairy_eggs" });
  });

  it("sends nothing and just closes when nothing changed", async () => {
    const { fake, onClose, dialog } = show();
    await screen.findByRole("dialog", { name: "Edit item" });
    await userEvent.click(within(dialog()).getByRole("button", { name: "Save" }));
    expect(fake.callsTo("PATCH", "/shopping-lists/l1/items/i1")).toHaveLength(0);
    expect(onClose).toHaveBeenCalled();
  });

  it.each([
    ["a blank name", "Name", "", "Give the item a name."],
    ["text for a quantity", "Quantity", "lots", "Enter a number, for example 2 or 1.5."],
    ["a quantity of zero", "Quantity", "0", "The quantity must be more than 0 and at most 100000."],
    ["a huge quantity", "Quantity", "100001", "The quantity must be more than 0 and at most 100000."],
  ])("refuses %s inline and sends nothing", async (_what, label, value, message) => {
    const { fake, dialog } = show();
    await screen.findByRole("dialog", { name: "Edit item" });
    await userEvent.clear(within(dialog()).getByLabelText(label));
    if (value) await userEvent.type(within(dialog()).getByLabelText(label), value);
    await userEvent.click(within(dialog()).getByRole("button", { name: "Save" }));
    expect(await within(dialog()).findByText(message)).toBeInTheDocument();
    expect(fake.callsTo("PATCH", "/shopping-lists/l1/items/i1")).toHaveLength(0);
  });

  it("shows the other person's version and asks to review when the item was changed under it, then saves against the new version", async () => {
    const theirs = makeItem({ id: "i1", name: "Bananas", quantity: 6, unit: "piece", category: "produce", version: 3 });
    let attempts = 0;
    const { fake, onClose, dialog } = show({
      "PATCH /shopping-lists/:id/items/:item": () => (++attempts === 1 ? problem(409, "version_conflict", { current: theirs }) : json(makeItem({ ...theirs, name: "Bananas!", version: 4 }))),
    });
    await screen.findByRole("dialog", { name: "Edit item" });
    await userEvent.clear(within(dialog()).getByLabelText("Name"));
    await userEvent.type(within(dialog()).getByLabelText("Name"), "Oat milk");
    await userEvent.click(within(dialog()).getByRole("button", { name: "Save" }));

    expect(await within(dialog()).findByText("Someone else changed this item. Review it and save again.")).toBeInTheDocument();
    await waitFor(() => expect(within(dialog()).getByLabelText("Name")).toHaveValue("Bananas"));
    expect(within(dialog()).getByLabelText("Quantity")).toHaveValue("6");
    expect(onClose).not.toHaveBeenCalled();

    await userEvent.type(within(dialog()).getByLabelText("Name"), "!");
    await userEvent.click(within(dialog()).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(fake.callsTo("PATCH", "/shopping-lists/l1/items/i1")[1]?.body).toMatchObject({ version: 3, name: "Bananas!" });
  });

  it("keeps what is being typed when someone else's change arrives, and saves against the version it was opened on, so that change is a conflict and not an overwrite", async () => {
    const theirs = makeItem({ id: "i1", name: "Milk 1L", quantity: 2, unit: "piece", category: "dairy_eggs", version: 2 });
    const { fake, queryClient, dialog } = show({ "PATCH /shopping-lists/:id/items/:item": () => problem(409, "version_conflict", { current: theirs }) });
    await screen.findByRole("dialog", { name: "Edit item" });
    await userEvent.clear(within(dialog()).getByLabelText("Name"));
    await userEvent.type(within(dialog()).getByLabelText("Name"), "Oat milk");

    // The partner's edit arrives over the stream and lands in the cache while the form is open.
    queryClient.setQueryData(shoppingKeys.detail("l1"), makeList({ items: [theirs] }));
    await waitFor(() => expect(queryClient.getQueryData(shoppingKeys.detail("l1"))).toMatchObject({ items: [{ version: 2 }] }));
    expect(within(dialog()).getByLabelText("Name")).toHaveValue("Oat milk");

    await userEvent.click(within(dialog()).getByRole("button", { name: "Save" }));
    expect(await within(dialog()).findByText("Someone else changed this item. Review it and save again.")).toBeInTheDocument();
    await waitFor(() => expect(within(dialog()).getByLabelText("Name")).toHaveValue("Milk 1L"));
    expect(fake.callsTo("PATCH", "/shopping-lists/l1/items/i1")[0]?.body).toMatchObject({ version: 1, name: "Oat milk" });
  });

  it("explains any other failure and keeps what was typed", async () => {
    const { onClose, dialog } = show({ "PATCH /shopping-lists/:id/items/:item": () => problem(500, "internal_error") });
    await screen.findByRole("dialog", { name: "Edit item" });
    await userEvent.type(within(dialog()).getByLabelText("Name"), " 2");
    await userEvent.click(within(dialog()).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(within(dialog()).getByRole("button", { name: "Save" })).toBeEnabled());
    expect(onClose).not.toHaveBeenCalled();
    expect(within(dialog()).getByLabelText("Name")).toHaveValue("Milk 2");
  });

  it("removes the item", async () => {
    const { fake, onClose, dialog } = show({ "DELETE /shopping-lists/:id/items/:item": () => noContent() });
    await screen.findByRole("dialog", { name: "Edit item" });
    await userEvent.click(within(dialog()).getByRole("button", { name: "Remove item" }));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(fake.callsTo("DELETE", "/shopping-lists/l1/items/i1")).toHaveLength(1);
  });

  it("is closed when there is no item to edit", () => {
    render(
      <QueryClientProvider client={testQueryClient()}>
        <EditItemDialog item={null} listId="l1" onClose={() => undefined} />
      </QueryClientProvider>,
    );
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });
});
```

**Create `web/src/features/shopping/shopping-list-view.test.tsx`**

```tsx
// @vitest-environment jsdom
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { FakeEventSource } from "@/test/fake-event-source";
import { fakeApi, json, problem } from "@/test/fake-api";
import { makeIngredient, partnership } from "@/test/fixtures";
import { renderWithClient } from "@/test/render";
import { makeItem, makeList, makeUser } from "@/test/shopping-fixtures";
import type { ShoppingList } from "./items";
import { ShoppingListView } from "./shopping-list-view";

const toastError = vi.fn();
vi.mock("sonner", () => ({ toast: { error: (message: string) => toastError(message), success: vi.fn() } }));
vi.mock("next/navigation", () => ({ useRouter: () => ({ replace: vi.fn(), push: vi.fn() }) }));

beforeEach(() => FakeEventSource.install());
afterEach(() => toastError.mockReset());

const items = () => [
  makeItem({ id: "i1", name: "Milk", category: "dairy_eggs", position: 0 }),
  makeItem({ id: "i2", name: "Apples", category: "produce", position: 1, quantity: 6, unit: "piece" }),
  makeItem({ id: "i3", name: "Carrots", category: "produce", position: 2, checked: true, checked_by: "u1" }),
];
const noPartner = { "GET /partner": () => problem(404, "partner_not_linked") };
const me = { "GET /me": () => json(makeUser()) };

function show(initial: ShoppingList = makeList({ items: items() }), routes: Parameters<typeof fakeApi>[0] = {}) {
  let server = initial;
  const fake = fakeApi({ "GET /shopping-lists/:id": () => json(server), ...me, ...noPartner, ...routes });
  const view = renderWithClient(<ShoppingListView id="l1" />);
  return { fake, serve: (next: ShoppingList) => void (server = next), ...view };
}

describe("the list", () => {
  it("groups items by aisle, with what is left to buy first, and counts what is checked", async () => {
    show();
    const produce = await screen.findByRole("region", { name: "Produce" });
    expect(within(produce).getAllByRole("checkbox").map((c) => c.getAttribute("aria-label") ?? c.closest("label")?.textContent)).toEqual(["Apples6 pieces", "Carrots"]);
    expect(screen.getByRole("region", { name: "Dairy and eggs" })).toBeInTheDocument();
    expect(screen.getByText("1 of 3 checked")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Weekly shop" })).toBeInTheDocument();
  });

  it("names the plan range a generated list came from", async () => {
    show(makeList({ items: items(), source_from: "2026-09-28", source_to: "2026-10-04" }));
    expect(await screen.findByText(/Sep 28 – Oct 4/)).toBeInTheDocument();
  });

  it("says so when there is nothing on the list yet", async () => {
    show(makeList());
    expect(await screen.findByText(/No items yet/)).toBeInTheDocument();
  });

  it("offers a retry when the list cannot be loaded for another reason", async () => {
    let fail = true;
    fakeApi({ "GET /shopping-lists/:id": () => (fail ? problem(500, "internal_error") : json(makeList({ items: items() }))), ...me, ...noPartner });
    renderWithClient(<ShoppingListView id="l1" />);
    expect(await screen.findByText("Something went wrong. Try again.")).toBeInTheDocument();
    fail = false;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByRole("region", { name: "Produce" })).toBeInTheDocument();
  });
});

describe("checking items off", () => {
  it("ticks at once and tells the API only that it is checked, with no version", async () => {
    const { fake } = show(undefined, {
      "PATCH /shopping-lists/:id/items/:item": () => json(makeItem({ id: "i2", name: "Apples", category: "produce", position: 1, quantity: 6, unit: "piece", checked: true, checked_by: "u1", version: 2 })),
    });
    await userEvent.click(await screen.findByRole("checkbox", { name: /Apples/ }));
    await waitFor(() => expect(screen.getByRole("checkbox", { name: /Apples/ })).toBeChecked());
    expect(fake.callsTo("PATCH", "/shopping-lists/l1/items/i2")[0]?.body).toEqual({ checked: true });
    expect(screen.getByText("2 of 3 checked")).toBeInTheDocument();
  });

  it("puts the tick back and says why when the server refuses", async () => {
    show(undefined, { "PATCH /shopping-lists/:id/items/:item": () => problem(500, "internal_error") });
    await userEvent.click(await screen.findByRole("checkbox", { name: /Apples/ }));
    await waitFor(() => expect(toastError).toHaveBeenCalledWith("Something went wrong. Try again."));
    expect(screen.getByRole("checkbox", { name: /Apples/ })).not.toBeChecked();
    expect(screen.getByText("1 of 3 checked")).toBeInTheDocument();
  });

  it("ends on the last tap when a checkbox is tapped twice quickly", async () => {
    const { fake } = show(undefined, { "PATCH /shopping-lists/:id/items/:item": (req) => json(makeItem({ id: "i2", name: "Apples", category: "produce", position: 1, checked: (req.body as { checked: boolean }).checked, version: 2 })) });
    const box = await screen.findByRole("checkbox", { name: /Apples/ });
    await userEvent.click(box);
    await userEvent.click(box);
    await waitFor(() => expect(fake.callsTo("PATCH", "/shopping-lists/l1/items/i2")).toHaveLength(2));
    expect(fake.callsTo("PATCH", "/shopping-lists/l1/items/i2").map((c) => c.body)).toEqual([{ checked: true }, { checked: false }]);
    await waitFor(() => expect(screen.getByRole("checkbox", { name: /Apples/ })).not.toBeChecked());
  });
});

describe("quick-add", () => {
  it("shows the new item at once under Other and sends just its name", async () => {
    let release!: () => void;
    const { fake } = show(undefined, {
      "POST /shopping-lists/:id/items": () =>
        new Promise<Response>((resolve) => {
          release = () => resolve(json(makeItem({ id: "new1", name: "Bin bags", category: "other", position: 3 }), 201));
        }),
    });
    await userEvent.type(await screen.findByLabelText("Add an item"), "Bin bags{Enter}");
    const other = await screen.findByRole("region", { name: "Other" });
    const box = within(other).getByRole("checkbox", { name: /Bin bags/ });
    expect(box).toBeDisabled();
    expect(within(other).queryByRole("button", { name: "Edit Bin bags" })).not.toBeInTheDocument();
    expect(fake.callsTo("POST", "/shopping-lists/l1/items")[0]?.body).toEqual({ name: "Bin bags" });

    release();
    await waitFor(() => expect(within(screen.getByRole("region", { name: "Other" })).getByRole("checkbox", { name: /Bin bags/ })).toBeEnabled());
    expect(within(screen.getByRole("region", { name: "Other" })).getByRole("button", { name: "Edit Bin bags" })).toBeInTheDocument();
  });

  it("adds an ingredient by id, so it lands in that ingredient's aisle", async () => {
    const fake = show(undefined, {
      "GET /ingredients": () => json({ items: [makeIngredient({ id: "ing-kale", name: "Kale", category: "produce" })], next_cursor: null }),
      "POST /shopping-lists/:id/items": () => json(makeItem({ id: "k1", name: "Kale", category: "produce", position: 3, ingredient_id: "ing-kale" }), 201),
    }).fake;
    await userEvent.click(await screen.findByRole("combobox", { name: "Or add from ingredients" }));
    await userEvent.click(await screen.findByRole("option", { name: /Kale/ }));
    await waitFor(() => expect(fake.callsTo("POST", "/shopping-lists/l1/items")).toHaveLength(1));
    expect(fake.callsTo("POST", "/shopping-lists/l1/items")[0]?.body).toEqual({ name: "Kale", ingredient_id: "ing-kale" });
    expect(within(screen.getByRole("region", { name: "Produce" })).getByRole("checkbox", { name: /Kale/ })).toBeInTheDocument();
  });

  it("takes the item back out and says why when the server refuses it", async () => {
    show(undefined, { "POST /shopping-lists/:id/items": () => problem(404, "not_found") });
    await userEvent.type(await screen.findByLabelText("Add an item"), "Bin bags{Enter}");
    await waitFor(() => expect(toastError).toHaveBeenCalled());
    expect(screen.queryByRole("checkbox", { name: /Bin bags/ })).not.toBeInTheDocument();
  });
});

describe("a list shared with the partner", () => {
  const sharedByMe = () => makeList({ shared_with_partner: true, items: [makeItem({ id: "i1", name: "Milk", checked: true, checked_by: "u2" }), makeItem({ id: "i2", name: "Eggs", position: 1, checked: true, checked_by: "u1" })] });

  it("badges my shared list with the partner's name and attributes their check-offs, and mine to nobody", async () => {
    show(sharedByMe(), { "GET /partner": () => json(partnership()) });
    expect(await screen.findByText("Shared with Sam")).toBeInTheDocument();
    expect(screen.getByText("Checked by Sam")).toBeInTheDocument();
    expect(screen.getAllByText(/Checked by/)).toHaveLength(1);
    expect(screen.getByRole("button", { name: "List settings" })).toBeInTheDocument();
  });

  it("shows the partner's list as theirs, with no settings but with everything else", async () => {
    show(makeList({ is_owner: false, shared_with_partner: true, items: items() }), { "GET /partner": () => json(partnership()) });
    expect(await screen.findByText("Sam’s list")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "List settings" })).not.toBeInTheDocument();
    expect(screen.getByLabelText("Add an item")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Edit Apples" })).toBeInTheDocument();
  });

  it("opens the settings for the owner", async () => {
    show(sharedByMe(), { "GET /partner": () => json(partnership()) });
    await userEvent.click(await screen.findByRole("button", { name: "List settings" }));
    expect(await screen.findByRole("dialog", { name: "List settings" })).toBeInTheDocument();
  });
});

describe("editing an item", () => {
  it("opens the editor for the item and follows a saved change", async () => {
    show(undefined, { "PATCH /shopping-lists/:id/items/:item": () => json(makeItem({ id: "i1", name: "Oat milk", category: "dairy_eggs", version: 2 })) });
    await userEvent.click(await screen.findByRole("button", { name: "Edit Milk" }));
    const dialog = await screen.findByRole("dialog", { name: "Edit item" });
    await userEvent.clear(within(dialog).getByLabelText("Name"));
    await userEvent.type(within(dialog).getByLabelText("Name"), "Oat milk");
    await userEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    expect(await screen.findByRole("checkbox", { name: /Oat milk/ })).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });

  it("closes the editor when the item is removed by someone else while it is open", async () => {
    const { serve } = show();
    await userEvent.click(await screen.findByRole("button", { name: "Edit Milk" }));
    await screen.findByRole("dialog", { name: "Edit item" });
    serve(makeList({ items: items().slice(1) }));
    FakeEventSource.last!.message("item_deleted", { type: "item_deleted", list_id: "l1", item_id: "i1", version: 1 });
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });
});

describe("live updates", () => {
  it("opens the list's stream", async () => {
    show();
    await screen.findByRole("region", { name: "Produce" });
    expect(FakeEventSource.last?.url).toBe("/api/shopping-lists/l1/events");
  });

  it("shows what the partner ticked without a reload", async () => {
    const { serve } = show(makeList({ shared_with_partner: true, items: items() }), { "GET /partner": () => json(partnership()) });
    await screen.findByRole("region", { name: "Produce" });
    serve(makeList({ shared_with_partner: true, items: [makeItem({ id: "i1", name: "Milk", category: "dairy_eggs", checked: true, checked_by: "u2", version: 2 }), ...items().slice(1)] }));
    FakeEventSource.last!.message("item_changed", { type: "item_changed", list_id: "l1", item_id: "i1", version: 2 });
    await waitFor(() => expect(screen.getByRole("checkbox", { name: /Milk/ })).toBeChecked());
    expect(screen.getByText("Checked by Sam")).toBeInTheDocument();
  });

  it("says the list was deleted, with a way back, when the stream says so", async () => {
    show();
    await screen.findByRole("region", { name: "Produce" });
    FakeEventSource.last!.message("list_deleted", { type: "list_deleted", list_id: "l1" });
    expect(await screen.findByText("This list was deleted.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Shopping/ })).toHaveAttribute("href", "/shopping");
    expect(screen.queryByRole("region", { name: "Produce" })).not.toBeInTheDocument();
  });

  it("shows the list as unavailable, not stale items or a retry, when access is lost while it is open", async () => {
    let gone = false;
    fakeApi({ "GET /shopping-lists/:id": () => (gone ? problem(404, "not_found") : json(makeList({ shared_with_partner: true, is_owner: false, items: items() }))), ...me, "GET /partner": () => json(partnership()) });
    renderWithClient(<ShoppingListView id="l1" />);
    await screen.findByRole("region", { name: "Produce" });

    gone = true;
    FakeEventSource.last!.fail(true);
    expect(await screen.findByText("That list isn't available. It may have been deleted, or it is no longer shared with you.")).toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "Produce" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Try again" })).not.toBeInTheDocument();
  });

  it("is unavailable at once when the list was never visible", async () => {
    fakeApi({ "GET /shopping-lists/:id": () => problem(404, "not_found"), ...me, ...noPartner });
    renderWithClient(<ShoppingListView id="nope" />);
    expect(await screen.findByText("That list isn't available. It may have been deleted, or it is no longer shared with you.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Shopping/ })).toHaveAttribute("href", "/shopping");
  });
});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/features/shopping/item-row.test.tsx src/features/shopping/quick-add.test.tsx src/features/shopping/edit-item-dialog.test.tsx src/features/shopping/shopping-list-view.test.tsx`
Expected: FAIL: `Failed to resolve import "./item-row"`, `"./quick-add"`, `"./edit-item-dialog"`, `"./shopping-list-view"`.

- [ ] **Step 3: Write the implementation**

**Create `web/src/features/shopping/item-row.tsx`**

```tsx
"use client";

import { cn } from "cn";
import { Pencil } from "lucide-react";
import { Button } from "@/components/ui/button";
import { formatItemQuantity, type ShoppingItem } from "./items";
import { isOptimistic } from "./list-cache";

type Props = { item: ShoppingItem; checkedByName: string | null; onToggle: (item: ShoppingItem) => void; onEdit: (item: ShoppingItem) => void };

/** One item: a large tick target whose label carries the name, who ticked it and the quantity, and a way to edit. */
export function ItemRow({ item, checkedByName, onToggle, onEdit }: Props) {
  // The server does not know this item's id yet, so neither a tick nor an edit could reach it.
  const unsaved = isOptimistic(item);
  const quantity = formatItemQuantity(item);
  return (
    <li className="flex items-center gap-2 px-3 py-2">
      <label className="flex min-h-10 min-w-0 flex-1 items-center gap-3">
        <input type="checkbox" className="accent-primary size-5 shrink-0" checked={item.checked} disabled={unsaved} onChange={() => onToggle(item)} />
        <span className="min-w-0 flex-1">
          <span className={cn("block truncate", item.checked && "text-muted-foreground line-through")}>{item.name}</span>
          {checkedByName ? <span className="text-muted-foreground block text-xs">Checked by {checkedByName}</span> : null}
        </span>
        {quantity ? <span className="text-muted-foreground shrink-0 text-sm tabular-nums">{quantity}</span> : null}
      </label>
      {unsaved ? null : (
        <Button type="button" variant="ghost" size="icon-sm" aria-label={`Edit ${item.name}`} onClick={() => onEdit(item)}>
          <Pencil aria-hidden />
        </Button>
      )}
    </li>
  );
}
```

**Create `web/src/features/shopping/quick-add.tsx`**

```tsx
"use client";

import { useState } from "react";
import { Field } from "@/components/field";
import { IngredientSearch } from "@/components/ingredient-search/ingredient-search";
import type { Ingredient } from "@/components/ingredient-search/use-ingredient-search";
import { Button } from "@/components/ui/button";

/** Adding should take one line and one key press: type it and press Enter, or pick an ingredient. */
export function QuickAdd({ onAddText, onAddIngredient }: { onAddText: (name: string) => void; onAddIngredient: (ingredient: Ingredient) => void }) {
  const [text, setText] = useState("");
  const [error, setError] = useState<string | undefined>();

  return (
    <div className="grid gap-3">
      <form
        noValidate
        className="flex items-end gap-2"
        onSubmit={(event) => {
          event.preventDefault();
          const name = text.trim();
          if (!name) return setError("Type what to add.");
          setError(undefined);
          onAddText(name);
          setText("");
        }}
      >
        <div className="flex-1">
          <Field name="quick-add" label="Add an item" autoComplete="off" maxLength={200} value={text} onChange={(event) => setText(event.target.value)} error={error} />
        </div>
        <Button type="submit" className="h-11">
          Add
        </Button>
      </form>
      <IngredientSearch label="Or add from ingredients" onSelect={onAddIngredient} />
    </div>
  );
}
```

**Create `web/src/features/shopping/edit-item-dialog.tsx`**

```tsx
"use client";

import { useState } from "react";
import { toast } from "sonner";
import { Field } from "@/components/field";
import { NativeSelect } from "@/components/native-select";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { ApiError, problemMessage } from "@/lib/api/problem";
import { CATEGORIES, categoryLabel, type IngredientCategory } from "@/lib/ingredient-categories";
import { parseDecimal } from "@/lib/parse-number";
import type { ShoppingItem } from "./items";
import { conflictingItem, useDeleteItem, useEditItem } from "./queries";

type Props = { item: ShoppingItem | null; listId: string; onClose: () => void };

/**
 * Edit, or remove, one item. Mount it with a `key` per item so it starts fresh. An edit is saved against the version the form
 * was opened on, not the latest one: if someone changed the item meanwhile the API refuses it with their version, and the form
 * restarts on that version with a note. A live update in the background never wipes what is being typed, and never slips past
 * the version check.
 */
export function EditItemDialog({ item, listId, onClose }: Props) {
  const [opened] = useState(item);
  const [conflict, setConflict] = useState<{ current: ShoppingItem; round: number } | null>(null);
  const base = conflict?.current ?? opened;
  return (
    <Dialog open={item !== null} onOpenChange={(open) => !open && onClose()}>
      {item && base ? (
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Edit item</DialogTitle>
            <DialogDescription>Changes are saved against the version you opened, so they never silently overwrite someone else&apos;s.</DialogDescription>
          </DialogHeader>
          {conflict ? (
            <p role="alert" className="bg-destructive/10 text-destructive rounded-lg px-3 py-2 text-sm">
              {problemMessage(new ApiError({ status: 409, code: "version_conflict" }))}
            </p>
          ) : null}
          <ItemForm
            key={conflict?.round ?? 0}
            base={base}
            listId={listId}
            onDone={onClose}
            onConflict={(current) => setConflict((previous) => ({ current, round: (previous?.round ?? 0) + 1 }))}
          />
        </DialogContent>
      ) : null}
    </Dialog>
  );
}

type Errors = { name?: string; quantity?: string };

function ItemForm({ base, listId, onDone, onConflict }: { base: ShoppingItem; listId: string; onDone: () => void; onConflict: (current: ShoppingItem) => void }) {
  const edit = useEditItem(listId);
  const remove = useDeleteItem(listId);
  const [errors, setErrors] = useState<Errors>({});

  return (
    <form
      noValidate
      className="grid gap-4"
      onSubmit={(event) => {
        event.preventDefault();
        const data = new FormData(event.currentTarget);
        const name = String(data.get("edit-name") ?? "").trim();
        const parsed = parseDecimal(String(data.get("edit-quantity") ?? ""));
        const quantity = parsed.ok ? parsed.value : undefined;
        const unitText = String(data.get("edit-unit") ?? "");
        const unit = unitText === "g" || unitText === "ml" || unitText === "piece" ? unitText : null;
        const category = String(data.get("edit-category") ?? "") as IngredientCategory;

        const next: Errors = {};
        if (!name) next.name = "Give the item a name.";
        else if (name.length > 200) next.name = "Use at most 200 characters.";
        if (quantity === undefined) next.quantity = "Enter a number, for example 2 or 1.5.";
        else if (quantity !== null && (quantity <= 0 || quantity > 100000)) next.quantity = "The quantity must be more than 0 and at most 100000.";
        setErrors(next);
        if (next.name || next.quantity || quantity === undefined) return;

        if (name === base.name && quantity === base.quantity && unit === base.unit && category === base.category) return onDone();
        edit.mutate(
          { item: base, changes: { name, quantity, unit, category } },
          {
            onSuccess: onDone,
            onError: (error) => {
              const current = conflictingItem(error);
              return current ? onConflict(current) : toast.error(problemMessage(error));
            },
          },
        );
      }}
    >
      <Field name="edit-name" label="Name" autoComplete="off" defaultValue={base.name} error={errors.name} />
      <div className="grid grid-cols-2 gap-3">
        <Field name="edit-quantity" label="Quantity" inputMode="decimal" defaultValue={base.quantity === null ? "" : String(base.quantity)} error={errors.quantity} />
        <div className="grid gap-1.5">
          <Label htmlFor="edit-unit">Unit</Label>
          <NativeSelect id="edit-unit" name="edit-unit" defaultValue={base.unit ?? ""}>
            <option value="">None</option>
            <option value="g">g</option>
            <option value="ml">ml</option>
            <option value="piece">piece</option>
          </NativeSelect>
        </div>
      </div>
      <div className="grid gap-1.5">
        <Label htmlFor="edit-category">Category</Label>
        <NativeSelect id="edit-category" name="edit-category" defaultValue={base.category}>
          {CATEGORIES.map((category) => (
            <option key={category} value={category}>
              {categoryLabel(category)}
            </option>
          ))}
        </NativeSelect>
      </div>
      <div className="flex flex-wrap justify-between gap-2">
        <Button type="submit" disabled={edit.isPending}>
          {edit.isPending ? "Saving…" : "Save"}
        </Button>
        <Button type="button" variant="destructive" disabled={remove.isPending} onClick={() => remove.mutate(base.id, { onSuccess: onDone, onError: (error) => toast.error(problemMessage(error)) })}>
          Remove item
        </Button>
      </div>
    </form>
  );
}
```

**Create `web/src/features/shopping/shopping-list-view.tsx`**

```tsx
"use client";

import { ArrowLeft, Settings } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { toast } from "sonner";
import { ErrorState } from "@/components/error-state";
import { PageHeader } from "@/components/page-header";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useMe } from "@/features/auth/use-me";
import { usePartnerLink } from "@/features/meals/queries";
import { ApiError, problemMessage } from "@/lib/api/problem";
import { categoryLabel } from "@/lib/ingredient-categories";
import { EditItemDialog } from "./edit-item-dialog";
import { checkedCount, groupByCategory, rangeLabel } from "./items";
import { ItemRow } from "./item-row";
import { ListSettingsDialog } from "./list-settings-dialog";
import { QuickAdd } from "./quick-add";
import { useAddItem, useCheckItem, useShoppingList } from "./queries";
import { useListEvents } from "./use-list-events";

function BackToShopping() {
  return (
    <Link href="/shopping" className="text-muted-foreground hover:text-foreground mb-4 inline-flex items-center gap-1 text-sm">
      <ArrowLeft aria-hidden className="size-4" />
      Shopping
    </Link>
  );
}

/** One list: items by aisle, tick-off, quick-add and editing, kept live while it is open. */
export function ShoppingListView({ id }: { id: string }) {
  const query = useShoppingList(id);
  const me = useMe();
  const partner = usePartnerLink();
  const [deleted, setDeleted] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [settingsOpen, setSettingsOpen] = useState(false);
  useListEvents(id, () => setDeleted(true));

  const fail = (error: Error) => toast.error(problemMessage(error));
  const userId = me.data?.id ?? null;
  const check = useCheckItem(id, userId, fail);
  const add = useAddItem(id, fail);

  // A 404 means access is gone, even while items are still cached (a background refetch reads the same answer): never show them.
  if (deleted || (query.error instanceof ApiError && query.error.status === 404)) {
    return (
      <>
        <BackToShopping />
        <ErrorState message={deleted ? "This list was deleted." : "That list isn't available. It may have been deleted, or it is no longer shared with you."} />
      </>
    );
  }
  if (query.data === undefined) {
    return (
      <>
        <BackToShopping />
        {query.isPending ? (
          <div role="status" aria-label="Loading list" className="grid gap-3">
            <Skeleton className="h-9 w-48" />
            <Skeleton className="h-40 rounded-xl" />
          </div>
        ) : (
          <ErrorState message={problemMessage(query.error)} onRetry={() => void query.refetch()} />
        )}
      </>
    );
  }

  const list = query.data;
  const partnerName = partner.data?.status === "active" ? partner.data.display_name : null;
  const { done, total } = checkedCount(list.items);
  const range = rangeLabel(list);
  const editing = list.items.find((item) => item.id === editingId) ?? null;

  return (
    <>
      <BackToShopping />
      <PageHeader title={list.name} description={range ? `From your plan · ${range}` : undefined} />
      <div className="grid gap-6">
        <div className="flex flex-wrap items-center gap-3">
          {list.is_owner && list.shared_with_partner ? <Badge variant="secondary">Shared with {partnerName ?? "your partner"}</Badge> : null}
          {!list.is_owner ? <Badge variant="secondary">{partnerName ? `${partnerName}’s list` : "Your partner’s list"}</Badge> : null}
          <p className="text-muted-foreground text-sm">
            {done} of {total} checked
          </p>
          {list.is_owner ? (
            <Button type="button" variant="outline" size="sm" className="ml-auto" onClick={() => setSettingsOpen(true)}>
              <Settings aria-hidden />
              List settings
            </Button>
          ) : null}
        </div>

        <QuickAdd
          onAddText={(name) => add.mutate({ name })}
          onAddIngredient={(ingredient) => add.mutate({ name: ingredient.name, ingredientId: ingredient.id, category: ingredient.category })}
        />

        {total === 0 ? (
          <p className="text-muted-foreground text-sm">No items yet. Add one above.</p>
        ) : (
          <div className="grid gap-4">
            {groupByCategory(list.items).map((group) => (
              <section key={group.category} aria-label={categoryLabel(group.category)} className="bg-card overflow-hidden rounded-xl border">
                <h2 className="bg-muted/40 px-3 py-2 text-xs font-medium tracking-wide uppercase">{categoryLabel(group.category)}</h2>
                <ul className="divide-y">
                  {group.items.map((item) => (
                    <ItemRow
                      key={item.id}
                      item={item}
                      checkedByName={item.checked && item.checked_by !== null && userId !== null && item.checked_by !== userId ? (partnerName ?? "your partner") : null}
                      onToggle={(target) => check.mutate({ itemId: target.id, checked: !target.checked })}
                      onEdit={(target) => setEditingId(target.id)}
                    />
                  ))}
                </ul>
              </section>
            ))}
          </div>
        )}
      </div>

      <EditItemDialog key={editing?.id ?? "none"} item={editing} listId={id} onClose={() => setEditingId(null)} />
      {list.is_owner ? <ListSettingsDialog list={list} open={settingsOpen} onOpenChange={setSettingsOpen} /> : null}
    </>
  );
}
```

**Create `web/src/app/(app)/shopping/[id]/page.tsx`**

```tsx
import type { Metadata } from "next";
import { ShoppingListView } from "@/features/shopping/shopping-list-view";

export const metadata: Metadata = { title: "Shopping list" };

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <ShoppingListView id={id} />;
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/features/shopping`
Expected: PASS. Two things to watch: the "groups items by aisle" test reads each checkbox's label text because the checkbox has no `aria-label` (its name comes from the wrapping `<label>`), so it expects `"Apples6 pieces"` and `"Carrots"`; and the fake `serve()` in the first check-off test pre-loads the server's post-tick state because the optimistic write is followed by no refetch (the mutation stores the server's item), so the assertion holds either way.

- [ ] **Step 5: Lint, build and commit**

```bash
make lint-web && make test-web
cd web && npx next build && cd ..
git add web/src/features/shopping "web/src/app/(app)/shopping/[id]"
git commit -m "feat(web): add the shopping list screen with aisle groups, check-off, quick-add, item editing and live updates"
```

### Task 7: Profile: daily targets and account deletion

**Files:**
- Create: `web/src/features/profile/targets.ts`, `web/src/features/profile/queries.ts`, `web/src/features/profile/targets-form.tsx`, `web/src/features/profile/delete-account.tsx`
- Modify: `web/src/app/(app)/profile/page.tsx`
- Test: `web/src/features/profile/targets.test.ts`, `web/src/features/profile/targets-form.test.tsx`, `web/src/features/profile/delete-account.test.tsx`

**Interfaces:**
- Consumes: `api`, `unwrap`; `useMe`, `ME_KEY` (`@/features/auth/use-me`); `logout` (`@/features/auth/session`); `hardNavigate` (`@/lib/navigation`); `planKeys` (`@/features/plan/queries`); `parseDecimal`; `Field`, `Card*`, `Button`, `Dialog*`, `Skeleton`, `ErrorState`, `problemMessage`; `ProfileSummary` (existing).
- Produces:
  - `TARGET_FIELDS = { calories: "target-kcal", protein: "target-protein", carbs: "target-carbs", fat: "target-fat" }`, `type TargetsBody = { target_kcal: number | null; target_protein_g: number | null; target_carbs_g: number | null; target_fat_g: number | null }`, `type TargetsErrors`, `parseTargets(data: FormData): { value: TargetsBody } | { errors: TargetsErrors }`: a blank field is `null` (clears that target); a comma is a decimal point; calories must be above 0 and at most 20000; protein and fat from 0 to 2000; carbs from 0 to 5000 (`0` is a valid macro target, not a valid calorie target); anything else, or anything that is not a number, is an inline message and is never sent.
  - `useUpdateTargets()`: `PATCH /me` with all four targets; on success stores the user in `ME_KEY` and marks every plan query stale (so Today's rings and the week totals pick the new targets up); `useDeleteAccount()`: `DELETE /me`, then `logout()` (a failed logout does not undo the deletion, and is ignored).
  - `TargetsForm()`: "Daily targets" card: Calories (kcal), Protein (g), Carbohydrates (g), Fat (g), prefilled from the profile, "Save targets"; a saved form shows the toast "Targets saved." and re-reads the profile.
  - `DeleteAccount()`: a "Delete account" button opens "Delete your account?" which says what goes; "Delete my account" stays disabled until the account's email is typed (ignoring case and surrounding spaces). Success ends the session with `hardNavigate("/login")` (a full page load); a failed delete shows its message in the dialog, changes nothing and keeps the session.
  - The Profile page now shows `ProfileSummary`, `TargetsForm` and `DeleteAccount`.

- [ ] **Step 1: Write the failing tests**

**Create `web/src/features/profile/targets.test.ts`**

```ts
import { describe, expect, it } from "vitest";
import { TARGET_FIELDS, parseTargets } from "./targets";

function form(values: Partial<Record<keyof typeof TARGET_FIELDS, string>>) {
  const data = new FormData();
  for (const key of Object.keys(TARGET_FIELDS) as (keyof typeof TARGET_FIELDS)[]) data.set(TARGET_FIELDS[key], values[key] ?? "");
  return data;
}

describe("parseTargets", () => {
  it("reads all four, with a comma as a decimal point", () => {
    expect(parseTargets(form({ calories: "2000", protein: "120,5", carbs: "250", fat: "70" }))).toEqual({
      value: { target_kcal: 2000, target_protein_g: 120.5, target_carbs_g: 250, target_fat_g: 70 },
    });
  });

  it("turns a blank field into null, which clears that target", () => {
    expect(parseTargets(form({ calories: "2000" }))).toEqual({ value: { target_kcal: 2000, target_protein_g: null, target_carbs_g: null, target_fat_g: null } });
    expect(parseTargets(form({}))).toEqual({ value: { target_kcal: null, target_protein_g: null, target_carbs_g: null, target_fat_g: null } });
  });

  it("allows zero for a macro, not for calories", () => {
    expect(parseTargets(form({ protein: "0", carbs: "0", fat: "0" }))).toEqual({ value: { target_kcal: null, target_protein_g: 0, target_carbs_g: 0, target_fat_g: 0 } });
    expect(parseTargets(form({ calories: "0" }))).toEqual({ errors: { calories: "Calories must be more than 0 and at most 20000." } });
  });

  it("holds each target to the API's limits, inclusive at the top", () => {
    expect(parseTargets(form({ calories: "20000", protein: "2000", carbs: "5000", fat: "2000" }))).toHaveProperty("value");
    expect(parseTargets(form({ calories: "20001", protein: "2001", carbs: "5001", fat: "2001" }))).toEqual({
      errors: {
        calories: "Calories must be more than 0 and at most 20000.",
        protein: "Protein must be at most 2000 g.",
        carbs: "Carbohydrates must be at most 5000 g.",
        fat: "Fat must be at most 2000 g.",
      },
    });
  });

  it.each(["abc", "-5", "1e3", "12 g", "1.2.3"])("refuses %j as not a number and names the field", (text) => {
    expect(parseTargets(form({ fat: text }))).toEqual({ errors: { fat: "Enter a number, for example 70." } });
  });

  it("reports every bad field at once", () => {
    const result = parseTargets(form({ calories: "x", protein: "y" }));
    expect(result).toEqual({ errors: { calories: "Enter a number, for example 2000.", protein: "Enter a number, for example 70." } });
  });
});
```

**Create `web/src/features/profile/targets-form.test.tsx`**

```tsx
// @vitest-environment jsdom
import { fireEvent, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ME_KEY } from "@/features/auth/use-me";
import { planKeys } from "@/features/plan/queries";
import { fakeApi, json, problem } from "@/test/fake-api";
import { renderWithClient } from "@/test/render";
import { makePlan } from "@/test/plan-fixtures";
import { makeUser } from "@/test/shopping-fixtures";
import { TargetsForm } from "./targets-form";

const toastError = vi.fn();
const toastSuccess = vi.fn();
vi.mock("sonner", () => ({ toast: { error: (m: string) => toastError(m), success: (m: string) => toastSuccess(m) } }));
afterEach(() => {
  toastError.mockReset();
  toastSuccess.mockReset();
});

const user = () => makeUser({ target_kcal: 2000, target_protein_g: 120, target_carbs_g: null, target_fat_g: 70 });
const patch = (fake: ReturnType<typeof fakeApi>) => fake.callsTo("PATCH", "/me");

describe("TargetsForm", () => {
  it("shows the targets the profile holds, blank where there is none", async () => {
    fakeApi({ "GET /me": () => json(user()) });
    renderWithClient(<TargetsForm />);
    expect(await screen.findByLabelText("Calories (kcal)")).toHaveValue("2000");
    expect(screen.getByLabelText("Protein (g)")).toHaveValue("120");
    expect(screen.getByLabelText("Carbohydrates (g)")).toHaveValue("");
    expect(screen.getByLabelText("Fat (g)")).toHaveValue("70");
  });

  it("saves all four, blank as null, then refreshes the profile and the plan's totals", async () => {
    const saved = makeUser({ target_kcal: 2000, target_protein_g: 120, target_carbs_g: 250, target_fat_g: null });
    const fake = fakeApi({ "GET /me": () => json(user()), "PATCH /me": () => json(saved) });
    const { queryClient } = renderWithClient(<TargetsForm />);
    queryClient.setQueryData(planKeys.range("2026-09-28", "2026-10-04"), makePlan("2026-09-28", "2026-10-04"));

    await userEvent.type(await screen.findByLabelText("Carbohydrates (g)"), "250");
    await userEvent.clear(screen.getByLabelText("Fat (g)"));
    await userEvent.click(screen.getByRole("button", { name: "Save targets" }));

    await waitFor(() => expect(patch(fake)).toHaveLength(1));
    expect(patch(fake)[0]?.body).toEqual({ target_kcal: 2000, target_protein_g: 120, target_carbs_g: 250, target_fat_g: null });
    await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith("Targets saved."));
    expect(queryClient.getQueryData(ME_KEY)).toEqual(saved);
    expect(queryClient.getQueryState(planKeys.range("2026-09-28", "2026-10-04"))?.isInvalidated).toBe(true);
  });

  it("accepts zero for a macro and a comma in a number", async () => {
    const fake = fakeApi({ "GET /me": () => json(user()), "PATCH /me": () => json(user()) });
    renderWithClient(<TargetsForm />);
    fireEvent.change(await screen.findByLabelText("Protein (g)"), { target: { value: "0" } });
    fireEvent.change(screen.getByLabelText("Calories (kcal)"), { target: { value: "1850,5" } });
    await userEvent.click(screen.getByRole("button", { name: "Save targets" }));
    await waitFor(() => expect(patch(fake)).toHaveLength(1));
    expect(patch(fake)[0]?.body).toMatchObject({ target_kcal: 1850.5, target_protein_g: 0 });
  });

  it("refuses zero calories and text, names the fields, and sends nothing", async () => {
    const fake = fakeApi({ "GET /me": () => json(user()) });
    renderWithClient(<TargetsForm />);
    fireEvent.change(await screen.findByLabelText("Calories (kcal)"), { target: { value: "0" } });
    fireEvent.change(screen.getByLabelText("Fat (g)"), { target: { value: "lots" } });
    await userEvent.click(screen.getByRole("button", { name: "Save targets" }));
    expect(await screen.findByText("Calories must be more than 0 and at most 20000.")).toBeInTheDocument();
    expect(screen.getByText("Enter a number, for example 70.")).toBeInTheDocument();
    expect(patch(fake)).toHaveLength(0);
    expect(screen.getByLabelText("Fat (g)")).toHaveValue("lots");
  });

  it("explains a refusal in a toast and keeps what was typed", async () => {
    fakeApi({ "GET /me": () => json(user()), "PATCH /me": () => problem(500, "internal_error") });
    renderWithClient(<TargetsForm />);
    fireEvent.change(await screen.findByLabelText("Calories (kcal)"), { target: { value: "1800" } });
    await userEvent.click(screen.getByRole("button", { name: "Save targets" }));
    await waitFor(() => expect(toastError).toHaveBeenCalledWith("Something went wrong. Try again."));
    expect(screen.getByLabelText("Calories (kcal)")).toHaveValue("1800");
  });

  it("offers a retry when the profile cannot be loaded", async () => {
    let fail = true;
    fakeApi({ "GET /me": () => (fail ? problem(500, "internal_error") : json(user())) });
    renderWithClient(<TargetsForm />);
    expect(await screen.findByText("Something went wrong. Try again.")).toBeInTheDocument();
    fail = false;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByLabelText("Calories (kcal)")).toHaveValue("2000");
  });
});
```

**Create `web/src/features/profile/delete-account.test.tsx`**

```tsx
// @vitest-environment jsdom
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { fakeApi, json, noContent, problem } from "@/test/fake-api";
import { renderWithClient } from "@/test/render";
import { makeUser } from "@/test/shopping-fixtures";
import { DeleteAccount } from "./delete-account";

const hardNavigate = vi.fn();
vi.mock("@/lib/navigation", () => ({ hardNavigate: (url: string) => hardNavigate(url) }));
afterEach(() => hardNavigate.mockReset());

const me = { "GET /me": () => json(makeUser({ email: "ann@example.test" })) };

async function openDialog() {
  const opener = await screen.findByRole("button", { name: "Delete account" });
  // The button stays off until the profile (and so the email to type) has loaded.
  await waitFor(() => expect(opener).toBeEnabled());
  await userEvent.click(opener);
  return screen.findByRole("dialog", { name: "Delete your account?" });
}

describe("DeleteAccount", () => {
  it("says what is lost before asking for anything, and keeps the button off until the email is typed", async () => {
    fakeApi({ ...me });
    renderWithClient(<DeleteAccount />);
    const dialog = await openDialog();
    expect(within(dialog).getByText(/permanently deletes your account/i)).toBeInTheDocument();
    expect(within(dialog).getByText(/meals, diet templates, plan and shopping lists/i)).toBeInTheDocument();
    const confirm = within(dialog).getByRole("button", { name: "Delete my account" });
    expect(confirm).toBeDisabled();
    await userEvent.type(within(dialog).getByLabelText(/Type ann@example\.test to confirm/), "ann@example.tes");
    expect(confirm).toBeDisabled();
    await userEvent.type(within(dialog).getByLabelText(/Type ann@example\.test to confirm/), "t");
    expect(confirm).toBeEnabled();
  });

  it("accepts the email in any case and with spaces round it", async () => {
    fakeApi({ ...me });
    renderWithClient(<DeleteAccount />);
    const dialog = await openDialog();
    await userEvent.type(within(dialog).getByLabelText(/to confirm/), "  ANN@Example.test ");
    expect(within(dialog).getByRole("button", { name: "Delete my account" })).toBeEnabled();
  });

  it("deletes the account, ends the session and leaves with a full page load", async () => {
    const fake = fakeApi({ ...me, "DELETE /me": () => noContent(), "POST /auth/logout": () => noContent() });
    renderWithClient(<DeleteAccount />);
    const dialog = await openDialog();
    await userEvent.type(within(dialog).getByLabelText(/to confirm/), "ann@example.test");
    await userEvent.click(within(dialog).getByRole("button", { name: "Delete my account" }));

    await waitFor(() => expect(hardNavigate).toHaveBeenCalledWith("/login"));
    expect(fake.callsTo("DELETE", "/me")).toHaveLength(1);
    expect(fake.callsTo("POST", "/auth/logout")).toHaveLength(1);
  });

  it("still leaves when signing out fails after the account is gone", async () => {
    fakeApi({
      ...me,
      "DELETE /me": () => noContent(),
      "POST /auth/logout": () => {
        throw new TypeError("offline");
      },
    });
    renderWithClient(<DeleteAccount />);
    const dialog = await openDialog();
    await userEvent.type(within(dialog).getByLabelText(/to confirm/), "ann@example.test");
    await userEvent.click(within(dialog).getByRole("button", { name: "Delete my account" }));
    await waitFor(() => expect(hardNavigate).toHaveBeenCalledWith("/login"));
  });

  it("keeps the session and says why when the delete fails", async () => {
    const fake = fakeApi({ ...me, "DELETE /me": () => problem(500, "internal_error") });
    renderWithClient(<DeleteAccount />);
    const dialog = await openDialog();
    await userEvent.type(within(dialog).getByLabelText(/to confirm/), "ann@example.test");
    await userEvent.click(within(dialog).getByRole("button", { name: "Delete my account" }));

    expect(await within(dialog).findByText("Something went wrong. Try again.")).toBeInTheDocument();
    expect(fake.callsTo("POST", "/auth/logout")).toHaveLength(0);
    expect(hardNavigate).not.toHaveBeenCalled();
    expect(screen.getByRole("dialog", { name: "Delete your account?" })).toBeInTheDocument();
  });

  it("does nothing when the person backs out", async () => {
    const fake = fakeApi({ ...me });
    renderWithClient(<DeleteAccount />);
    const dialog = await openDialog();
    await userEvent.click(within(dialog).getByRole("button", { name: "Keep my account" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(fake.callsTo("DELETE", "/me")).toHaveLength(0);
  });

  it("starts with an empty confirmation each time it is opened", async () => {
    fakeApi({ ...me });
    renderWithClient(<DeleteAccount />);
    let dialog = await openDialog();
    await userEvent.type(within(dialog).getByLabelText(/to confirm/), "ann@example.test");
    await userEvent.click(within(dialog).getByRole("button", { name: "Keep my account" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    dialog = await openDialog();
    expect(within(dialog).getByLabelText(/to confirm/)).toHaveValue("");
    expect(within(dialog).getByRole("button", { name: "Delete my account" })).toBeDisabled();
  });
});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/features/profile`
Expected: FAIL: `Failed to resolve import "./targets"`, `"./targets-form"`, `"./delete-account"`.

- [ ] **Step 3: Write the implementation**

**Create `web/src/features/profile/targets.ts`**

```ts
import { parseDecimal } from "@/lib/parse-number";

/** Form field names, prefixed so they never collide with another form's ids on the page. */
export const TARGET_FIELDS = { calories: "target-kcal", protein: "target-protein", carbs: "target-carbs", fat: "target-fat" } as const;
type Key = keyof typeof TARGET_FIELDS;

export type TargetsBody = { target_kcal: number | null; target_protein_g: number | null; target_carbs_g: number | null; target_fat_g: number | null };
export type TargetsErrors = Partial<Record<Key, string>>;

// The API's limits (`UpdateProfileRequest`): calories above 0; macros from 0. A blank field clears the target.
const RULES: Record<Key, { max: number; positive: boolean; over: string; example: string }> = {
  calories: { max: 20000, positive: true, over: "Calories must be more than 0 and at most 20000.", example: "2000" },
  protein: { max: 2000, positive: false, over: "Protein must be at most 2000 g.", example: "70" },
  carbs: { max: 5000, positive: false, over: "Carbohydrates must be at most 5000 g.", example: "250" },
  fat: { max: 2000, positive: false, over: "Fat must be at most 2000 g.", example: "70" },
};

export function parseTargets(data: FormData): { value: TargetsBody } | { errors: TargetsErrors } {
  const errors: TargetsErrors = {};
  const numbers: Record<Key, number | null> = { calories: null, protein: null, carbs: null, fat: null };
  for (const key of Object.keys(TARGET_FIELDS) as Key[]) {
    const rule = RULES[key];
    const parsed = parseDecimal(String(data.get(TARGET_FIELDS[key]) ?? ""));
    if (!parsed.ok) errors[key] = `Enter a number, for example ${rule.example}.`;
    else if (parsed.value !== null && ((rule.positive && parsed.value <= 0) || parsed.value > rule.max)) errors[key] = rule.over;
    else numbers[key] = parsed.value;
  }
  if (Object.keys(errors).length > 0) return { errors };
  return { value: { target_kcal: numbers.calories, target_protein_g: numbers.protein, target_carbs_g: numbers.carbs, target_fat_g: numbers.fat } };
}
```

**Create `web/src/features/profile/queries.ts`**

```ts
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ME_KEY } from "@/features/auth/use-me";
import { logout } from "@/features/auth/session";
import { planKeys } from "@/features/plan/queries";
import { api, unwrap } from "@/lib/api/client";
import type { TargetsBody } from "./targets";

/** Saves the four daily targets. Today's rings and the plan's totals read them, so every plan query is refreshed. */
export function useUpdateTargets() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: TargetsBody) => unwrap(api.PATCH("/me", { body })),
    onSuccess: (user) => {
      queryClient.setQueryData(ME_KEY, user);
      void queryClient.invalidateQueries({ queryKey: planKeys.all });
    },
  });
}

/** Deletes the account for good, then ends the session. A sign-out that fails afterwards does not undo the deletion. */
export function useDeleteAccount() {
  return useMutation({
    mutationFn: async (): Promise<void> => {
      await unwrap(api.DELETE("/me"));
      await logout().catch(() => undefined);
    },
  });
}
```

**Create `web/src/features/profile/targets-form.tsx`**

```tsx
"use client";

import { useState } from "react";
import { toast } from "sonner";
import { ErrorState } from "@/components/error-state";
import { Field } from "@/components/field";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { useMe } from "@/features/auth/use-me";
import { problemMessage } from "@/lib/api/problem";
import { useUpdateTargets } from "./queries";
import { TARGET_FIELDS, parseTargets, type TargetsErrors } from "./targets";

const text = (value: number | null | undefined) => (value === null || value === undefined ? "" : String(value));

export function TargetsForm() {
  const me = useMe();
  return (
    <Card>
      <CardHeader>
        <CardTitle>Daily targets</CardTitle>
      </CardHeader>
      <CardContent>
        {me.isPending ? (
          <Skeleton role="status" aria-label="Loading targets" className="h-40 rounded-lg" />
        ) : me.data === undefined ? (
          <ErrorState message={problemMessage(me.error)} onRetry={() => void me.refetch()} />
        ) : (
          <TargetsFields
            key={me.data.updated_at}
            initial={{ calories: text(me.data.target_kcal), protein: text(me.data.target_protein_g), carbs: text(me.data.target_carbs_g), fat: text(me.data.target_fat_g) }}
          />
        )}
      </CardContent>
    </Card>
  );
}

/** Keyed by the profile's `updated_at`, so a saved (or reloaded) profile re-fills the form. */
function TargetsFields({ initial }: { initial: Record<keyof typeof TARGET_FIELDS, string> }) {
  const update = useUpdateTargets();
  const [errors, setErrors] = useState<TargetsErrors>({});

  return (
    <form
      noValidate
      className="grid gap-4"
      onSubmit={(event) => {
        event.preventDefault();
        const parsed = parseTargets(new FormData(event.currentTarget));
        if ("errors" in parsed) return setErrors(parsed.errors);
        setErrors({});
        update.mutate(parsed.value, { onSuccess: () => toast.success("Targets saved."), onError: (error) => toast.error(problemMessage(error)) });
      }}
    >
      <p className="text-muted-foreground text-sm">Today and your plan show your progress against these. Leave a field blank for no target.</p>
      <div className="grid gap-3 sm:grid-cols-2">
        <Field name={TARGET_FIELDS.calories} label="Calories (kcal)" inputMode="decimal" defaultValue={initial.calories} error={errors.calories} />
        <Field name={TARGET_FIELDS.protein} label="Protein (g)" inputMode="decimal" defaultValue={initial.protein} error={errors.protein} />
        <Field name={TARGET_FIELDS.carbs} label="Carbohydrates (g)" inputMode="decimal" defaultValue={initial.carbs} error={errors.carbs} />
        <Field name={TARGET_FIELDS.fat} label="Fat (g)" inputMode="decimal" defaultValue={initial.fat} error={errors.fat} />
      </div>
      <div>
        <Button type="submit" disabled={update.isPending}>
          {update.isPending ? "Saving…" : "Save targets"}
        </Button>
      </div>
    </form>
  );
}
```

**Create `web/src/features/profile/delete-account.tsx`**

```tsx
"use client";

import { useState } from "react";
import { Field } from "@/components/field";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { useMe } from "@/features/auth/use-me";
import { problemMessage } from "@/lib/api/problem";
import { hardNavigate } from "@/lib/navigation";
import { useDeleteAccount } from "./queries";

/** The one action that cannot be undone. The API asks for no re-authentication, so the screen asks for the email itself. */
export function DeleteAccount() {
  const me = useMe();
  const [open, setOpen] = useState(false);
  return (
    <Card>
      <CardHeader>
        <CardTitle>Delete account</CardTitle>
      </CardHeader>
      <CardContent className="grid gap-3">
        <p className="text-muted-foreground text-sm">Permanently delete your account and everything in it.</p>
        <div>
          <Button type="button" variant="destructive" disabled={me.data === undefined} onClick={() => setOpen(true)}>
            Delete account
          </Button>
        </div>
      </CardContent>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete your account?</DialogTitle>
            <DialogDescription>
              This permanently deletes your account and everything it owns: your meals, diet templates, plan and shopping lists. A partner keeps only the copies they made themselves. It cannot be undone.
            </DialogDescription>
          </DialogHeader>
          {me.data ? <Confirm email={me.data.email} onCancel={() => setOpen(false)} /> : null}
        </DialogContent>
      </Dialog>
    </Card>
  );
}

/** Mounted only while the dialog is open, so the confirmation always starts empty. */
function Confirm({ email, onCancel }: { email: string; onCancel: () => void }) {
  const remove = useDeleteAccount();
  const [typed, setTyped] = useState("");
  const matches = typed.trim().toLowerCase() === email.toLowerCase();

  return (
    <div className="grid gap-4">
      <Field name="confirm-email" label={`Type ${email} to confirm`} autoComplete="off" value={typed} onChange={(event) => setTyped(event.target.value)} />
      {remove.error ? (
        <p role="alert" className="text-destructive text-sm">
          {problemMessage(remove.error)}
        </p>
      ) : null}
      <div className="flex flex-wrap gap-2">
        <Button
          type="button"
          variant="destructive"
          // A full page load, like signing out: it drops every cache with the deleted session instead of racing a query about to refetch.
          onClick={() => remove.mutate(undefined, { onSuccess: () => hardNavigate("/login") })}
          disabled={!matches || remove.isPending}
        >
          {remove.isPending ? "Deleting…" : "Delete my account"}
        </Button>
        <Button type="button" variant="outline" onClick={onCancel}>
          Keep my account
        </Button>
      </div>
    </div>
  );
}
```

**Replace the contents of `web/src/app/(app)/profile/page.tsx`**

```tsx
import type { Metadata } from "next";
import { PageHeader } from "@/components/page-header";
import { ProfileSummary } from "@/features/auth/profile-summary";
import { DeleteAccount } from "@/features/profile/delete-account";
import { TargetsForm } from "@/features/profile/targets-form";

export const metadata: Metadata = { title: "Profile" };

export default function Page() {
  return (
    <>
      <PageHeader title="Profile" />
      <div className="grid max-w-3xl gap-6">
        <ProfileSummary />
        <TargetsForm />
        <DeleteAccount />
      </div>
    </>
  );
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/features/profile src/features/auth`
Expected: PASS (the existing auth tests still pass: the profile summary is unchanged). If `CardHeader`/`CardTitle` are not exported by the repo's `card.tsx`, use what it exports (`Card`, `CardContent` exist from the Foundation plan; `npx shadcn@4.21.0 add card --overwrite` restores the full set, but check `git diff` first because the Foundation customised it).

- [ ] **Step 5: Lint, build and commit**

```bash
make lint-web && make test-web
cd web && npx next build && cd ..
git add web/src/features/profile "web/src/app/(app)/profile/page.tsx"
git commit -m "feat(web): add the daily targets form and account deletion with a typed confirmation to Profile"
```

---

### Task 8: Profile: the partner connection

**Files:**
- Create: `web/src/features/partner/invite-code.ts`, `web/src/features/partner/queries.ts`, `web/src/features/partner/partner-card.tsx`
- Modify: `web/src/app/(app)/profile/page.tsx`
- Test: `web/src/features/partner/invite-code.test.ts`, `web/src/features/partner/partner-card.test.tsx`

**Interfaces:**
- Consumes: `usePartnerLink`, `PARTNER_KEY`, `Partnership`, `mealKeys` (`@/features/meals/queries`); `templateKeys` (`@/features/plan/template-queries`); `shoppingKeys` (`@/features/shopping/queries`); `api`, `unwrap`; `ApiError`, `problemMessage`; `Field`, `Card*`, `Button`, `Dialog*`, `Skeleton`, `ErrorState`.
- Produces:
  - `formatInviteCode(code: string): string` ("abcdefgh" is "ABCD-EFGH": upper-cased, in groups of four); `formatWhen(iso: string): string` (a medium local date and short time, en-US); `formatDate(iso: string): string` (a medium local date).
  - `useCreateInvite()`: `POST /partner/invite` → `{ code, expires_at }`, marks `PARTNER_KEY` stale; `useAcceptInvite()`: `(code: string) => Partnership` via `POST /partner/accept`, stores the partnership under `PARTNER_KEY`; `useUnlinkPartner()`: `DELETE /partner`, stores `null` under `PARTNER_KEY` and removes the cached partner lists (`mealKeys.partner`, `templateKeys.partner`, `shoppingKeys.partner`).
  - `PartnerCard()`: "Partner" card with three states. No partner: "Create invite code" (shows the code once, in groups, with its expiry, a "Copy code" button, and "Shown once. Share it with your partner.") and "Enter a code" (field "Invite code", "Link accounts"; a blank one says "Enter the code your partner sent you."; `invite_invalid` and `partner_already_linked` are shown inline on the field). A pending invite: "Waiting for your partner. The code expires {when}.", the code while this page still holds it, "Create a new code", "Cancel invite", and the same code entry. Linked: "Linked with {name} since {date}." and "Unlink", which first says what ends ("You will stop seeing each other's shared meals, diet templates and shopping lists ... Copies stay with whoever made them ... you need a new invite to link again") and needs "Unlink" again. A clipboard that refuses is a toast, never a crash.
  - The Profile page shows `PartnerCard` between the targets and the deletion.

- [ ] **Step 1: Write the failing tests**

**Create `web/src/features/partner/invite-code.test.ts`**

```ts
import { describe, expect, it } from "vitest";
import { formatDate, formatInviteCode, formatWhen } from "./invite-code";

describe("formatInviteCode", () => {
  it("groups an 8-character code in fours, in capitals", () => {
    expect(formatInviteCode("abcdefgh")).toBe("ABCD-EFGH");
    expect(formatInviteCode("K7M2QX9R")).toBe("K7M2-QX9R");
  });

  it("copes with a code of another length, and with what is not a code", () => {
    expect(formatInviteCode("ABC")).toBe("ABC");
    expect(formatInviteCode("ABCDEFGHJ")).toBe("ABCD-EFGH-J");
    expect(formatInviteCode("")).toBe("");
  });
});

describe("formatDate and formatWhen", () => {
  it("write a date the way a person would read it", () => {
    expect(formatDate("2026-01-15T12:00:00Z")).toBe("Jan 15, 2026");
    expect(formatWhen("2026-01-15T12:00:00Z")).toMatch(/^Jan 15, 2026, \d{1,2}:\d{2}\s?(AM|PM)$/);
  });
});
```

**Create `web/src/features/partner/partner-card.test.tsx`**

```tsx
// @vitest-environment jsdom
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { PARTNER_KEY, mealKeys } from "@/features/meals/queries";
import { templateKeys } from "@/features/plan/template-queries";
import { shoppingKeys } from "@/features/shopping/queries";
import { fakeApi, json, noContent, problem } from "@/test/fake-api";
import { partnership } from "@/test/fixtures";
import { renderWithClient } from "@/test/render";
import { PartnerCard } from "./partner-card";

const toastError = vi.fn();
const toastSuccess = vi.fn();
vi.mock("sonner", () => ({ toast: { error: (m: string) => toastError(m), success: (m: string) => toastSuccess(m) } }));
afterEach(() => {
  toastError.mockReset();
  toastSuccess.mockReset();
});

function setClipboard(writeText: (text: string) => Promise<void>) {
  Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
}

const noPartner = () => problem(404, "partner_not_linked");
const linked = () => ({ ...partnership(), linked_at: "2026-01-15T12:00:00Z" });
const pending = () => ({ ...partnership("pending"), expires_at: "2026-02-01T12:00:00Z" });

describe("with no partner", () => {
  it("offers to create a code or enter one", async () => {
    fakeApi({ "GET /partner": noPartner });
    renderWithClient(<PartnerCard />);
    expect(await screen.findByRole("button", { name: "Create invite code" })).toBeInTheDocument();
    expect(screen.getByLabelText("Invite code")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Link accounts" })).toBeInTheDocument();
  });

  it("shows a new code once, in groups, with its expiry, and keeps showing it while the invite is pending", async () => {
    let server: "none" | "pending" = "none";
    const fake = fakeApi({
      "GET /partner": () => (server === "none" ? noPartner() : json(pending())),
      "POST /partner/invite": () => {
        server = "pending";
        return json({ code: "ABCDEFGH", expires_at: "2026-02-01T12:00:00Z" }, 201);
      },
    });
    renderWithClient(<PartnerCard />);
    await userEvent.click(await screen.findByRole("button", { name: "Create invite code" }));

    expect(await screen.findByText("ABCD-EFGH")).toBeInTheDocument();
    expect(screen.getByText(/Shown once\. Share it with your partner\./)).toBeInTheDocument();
    expect(await screen.findByText(/Waiting for your partner\. The code expires Feb 1, 2026/)).toBeInTheDocument();
    expect(screen.getByText("ABCD-EFGH")).toBeInTheDocument();
    expect(fake.callsTo("POST", "/partner/invite")).toHaveLength(1);
  });

  it("copies the code, without the dashes", async () => {
    const writeText = vi.fn(async (_text: string) => undefined);
    setClipboard(writeText);
    fakeApi({ "GET /partner": noPartner, "POST /partner/invite": () => json({ code: "ABCDEFGH", expires_at: "2026-02-01T12:00:00Z" }, 201) });
    renderWithClient(<PartnerCard />);
    await userEvent.click(await screen.findByRole("button", { name: "Create invite code" }));
    await userEvent.click(await screen.findByRole("button", { name: "Copy code" }));
    expect(writeText).toHaveBeenCalledWith("ABCDEFGH");
    await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith("Code copied."));
  });

  it("tells the person to copy by hand, and does not crash, when the clipboard refuses", async () => {
    setClipboard(async () => Promise.reject(new Error("denied")));
    fakeApi({ "GET /partner": noPartner, "POST /partner/invite": () => json({ code: "ABCDEFGH", expires_at: "2026-02-01T12:00:00Z" }, 201) });
    renderWithClient(<PartnerCard />);
    await userEvent.click(await screen.findByRole("button", { name: "Create invite code" }));
    await userEvent.click(await screen.findByRole("button", { name: "Copy code" }));
    await waitFor(() => expect(toastError).toHaveBeenCalledWith("Couldn't copy. Select the code and copy it by hand."));
    expect(screen.getByText("ABCD-EFGH")).toBeInTheDocument();
  });

  it("links with a code, sending it trimmed, and shows the partnership", async () => {
    let server: "none" | "linked" = "none";
    const fake = fakeApi({
      "GET /partner": () => (server === "none" ? noPartner() : json(linked())),
      "POST /partner/accept": () => {
        server = "linked";
        return json(linked());
      },
    });
    renderWithClient(<PartnerCard />);
    await userEvent.type(await screen.findByLabelText("Invite code"), "  k7m2-qx9r ");
    await userEvent.click(screen.getByRole("button", { name: "Link accounts" }));

    expect(await screen.findByText(/Linked with Sam since Jan 15, 2026\./)).toBeInTheDocument();
    expect(fake.callsTo("POST", "/partner/accept")[0]?.body).toEqual({ code: "k7m2-qx9r" });
    expect(toastSuccess).toHaveBeenCalledWith("Linked with Sam.");
  });

  it.each([
    ["a wrong or used code", 404, "invite_invalid", "That invite code isn't valid. It may have expired or already been used."],
    ["already being linked", 409, "partner_already_linked", "You're already linked with a partner."],
  ])("shows %s on the code field, not as a toast", async (_what, status, code, message) => {
    fakeApi({ "GET /partner": noPartner, "POST /partner/accept": () => problem(status, code) });
    renderWithClient(<PartnerCard />);
    await userEvent.type(await screen.findByLabelText("Invite code"), "WRONGCODE");
    await userEvent.click(screen.getByRole("button", { name: "Link accounts" }));
    expect(await screen.findByText(message)).toBeInTheDocument();
    expect(toastError).not.toHaveBeenCalled();
  });

  it("asks for the code and sends nothing when the field is blank", async () => {
    const fake = fakeApi({ "GET /partner": noPartner });
    renderWithClient(<PartnerCard />);
    await userEvent.click(await screen.findByRole("button", { name: "Link accounts" }));
    expect(await screen.findByText("Enter the code your partner sent you.")).toBeInTheDocument();
    expect(fake.callsTo("POST", "/partner/accept")).toHaveLength(0);
  });

  it("toasts any other failure", async () => {
    fakeApi({ "GET /partner": noPartner, "POST /partner/accept": () => problem(429, "rate_limited") });
    renderWithClient(<PartnerCard />);
    await userEvent.type(await screen.findByLabelText("Invite code"), "ABCDEFGH");
    await userEvent.click(screen.getByRole("button", { name: "Link accounts" }));
    await waitFor(() => expect(toastError).toHaveBeenCalledWith("Too many attempts. Try again in a minute."));
  });
});

describe("with a pending invite", () => {
  it("says when it expires, offers a new code and cancelling, and cancels", async () => {
    let server: "pending" | "none" = "pending";
    const fake = fakeApi({
      "GET /partner": () => (server === "pending" ? json(pending()) : noPartner()),
      "DELETE /partner": () => {
        server = "none";
        return noContent();
      },
    });
    renderWithClient(<PartnerCard />);
    expect(await screen.findByText(/Waiting for your partner/)).toBeInTheDocument();
    expect(screen.getByText(/Feb 1, 2026/)).toBeInTheDocument();
    expect(screen.getByText(/The code was shown when you created it/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Create a new code" })).toBeInTheDocument();
    expect(screen.getByLabelText("Invite code")).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Cancel invite" }));
    expect(await screen.findByRole("button", { name: "Create invite code" })).toBeInTheDocument();
    expect(fake.callsTo("DELETE", "/partner")).toHaveLength(1);
  });
});

describe("when linked", () => {
  it("shows who and since when", async () => {
    fakeApi({ "GET /partner": () => json(linked()) });
    renderWithClient(<PartnerCard />);
    expect(await screen.findByText(/Linked with Sam since Jan 15, 2026\./)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Create invite code" })).not.toBeInTheDocument();
  });

  it("says what ends before unlinking, and unlinks only when confirmed, dropping what was the partner's from the cache", async () => {
    let server: "linked" | "none" = "linked";
    const fake = fakeApi({
      "GET /partner": () => (server === "linked" ? json(linked()) : noPartner()),
      "DELETE /partner": () => {
        server = "none";
        return noContent();
      },
    });
    const { queryClient } = renderWithClient(<PartnerCard />);
    queryClient.setQueryData(mealKeys.partner, { pages: [], pageParams: [] });
    queryClient.setQueryData(templateKeys.partner, { pages: [], pageParams: [] });
    queryClient.setQueryData(shoppingKeys.partner, { pages: [], pageParams: [] });

    await userEvent.click(await screen.findByRole("button", { name: "Unlink" }));
    const dialog = await screen.findByRole("dialog", { name: "Unlink from Sam?" });
    expect(within(dialog).getByText(/stop seeing each other's shared meals, diet templates and shopping lists/i)).toBeInTheDocument();
    expect(within(dialog).getByText(/Copies stay with whoever made them/i)).toBeInTheDocument();
    expect(within(dialog).getByText(/need a new invite to link again/i)).toBeInTheDocument();
    expect(fake.callsTo("DELETE", "/partner")).toHaveLength(0);

    await userEvent.click(within(dialog).getByRole("button", { name: "Unlink" }));
    expect(await screen.findByRole("button", { name: "Create invite code" })).toBeInTheDocument();
    expect(fake.callsTo("DELETE", "/partner")).toHaveLength(1);
    expect(queryClient.getQueryData(PARTNER_KEY)).toBeNull();
    expect(queryClient.getQueryData(mealKeys.partner)).toBeUndefined();
    expect(queryClient.getQueryData(templateKeys.partner)).toBeUndefined();
    expect(queryClient.getQueryData(shoppingKeys.partner)).toBeUndefined();
  });

  it("does nothing when the person backs out, and says why when the unlink fails", async () => {
    let fail = true;
    const fake = fakeApi({ "GET /partner": () => json(linked()), "DELETE /partner": () => (fail ? problem(500, "internal_error") : noContent()) });
    renderWithClient(<PartnerCard />);
    await userEvent.click(await screen.findByRole("button", { name: "Unlink" }));
    let dialog = await screen.findByRole("dialog", { name: "Unlink from Sam?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Keep the link" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(fake.callsTo("DELETE", "/partner")).toHaveLength(0);

    await userEvent.click(screen.getByRole("button", { name: "Unlink" }));
    dialog = await screen.findByRole("dialog", { name: "Unlink from Sam?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Unlink" }));
    expect(await within(dialog).findByText("Something went wrong. Try again.")).toBeInTheDocument();
    expect(screen.getByRole("dialog", { name: "Unlink from Sam?" })).toBeInTheDocument();
    fail = false;
  });
});

describe("loading the partner link", () => {
  it("offers a retry when it fails", async () => {
    let fail = true;
    fakeApi({ "GET /partner": () => (fail ? problem(500, "internal_error") : noPartner()) });
    renderWithClient(<PartnerCard />);
    expect(await screen.findByText("Something went wrong. Try again.")).toBeInTheDocument();
    fail = false;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByRole("button", { name: "Create invite code" })).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/features/partner`
Expected: FAIL: `Failed to resolve import "./invite-code"` and `"./partner-card"`.

- [ ] **Step 3: Write the implementation**

**Create `web/src/features/partner/invite-code.ts`**

```ts
/** An 8-character code as people read it aloud: capitals, in groups of four. */
export function formatInviteCode(code: string): string {
  return (code.toUpperCase().match(/.{1,4}/g) ?? []).join("-");
}

const DATE = new Intl.DateTimeFormat("en-US", { dateStyle: "medium" });
const WHEN = new Intl.DateTimeFormat("en-US", { dateStyle: "medium", timeStyle: "short" });

export const formatDate = (iso: string): string => DATE.format(new Date(iso));
export const formatWhen = (iso: string): string => WHEN.format(new Date(iso));
```

**Create `web/src/features/partner/queries.ts`**

```ts
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { PARTNER_KEY, mealKeys } from "@/features/meals/queries";
import { templateKeys } from "@/features/plan/template-queries";
import { shoppingKeys } from "@/features/shopping/queries";
import { api, unwrap } from "@/lib/api/client";

/** A new code replaces any pending one. The code is in this answer and nowhere else, ever. */
export function useCreateInvite() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => unwrap(api.POST("/partner/invite")),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: PARTNER_KEY }),
  });
}

export function useAcceptInvite() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (code: string) => unwrap(api.POST("/partner/accept", { body: { code } })),
    onSuccess: (partnership) => queryClient.setQueryData(PARTNER_KEY, partnership),
  });
}

/** Ends the link, or cancels a pending invite. Access ends at once, so what was the partner's is dropped from the cache too. */
export function useUnlinkPartner() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (): Promise<void> => {
      await unwrap(api.DELETE("/partner"));
    },
    onSuccess: () => {
      queryClient.setQueryData(PARTNER_KEY, null);
      for (const key of [mealKeys.partner, templateKeys.partner, shoppingKeys.partner]) queryClient.removeQueries({ queryKey: key });
    },
  });
}
```

**Create `web/src/features/partner/partner-card.tsx`**

```tsx
"use client";

import { useState } from "react";
import { toast } from "sonner";
import { ErrorState } from "@/components/error-state";
import { Field } from "@/components/field";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { usePartnerLink, type Partnership } from "@/features/meals/queries";
import { ApiError, problemMessage } from "@/lib/api/problem";
import { formatDate, formatInviteCode, formatWhen } from "./invite-code";
import { useAcceptInvite, useCreateInvite, useUnlinkPartner } from "./queries";

type Invite = { code: string; expires_at: string };

/** Link with one partner by an invite code, see the link, or end it. */
export function PartnerCard() {
  const link = usePartnerLink();
  // The code is returned once, when it is made; hold it here so the pending view can still show it.
  const [invite, setInvite] = useState<Invite | null>(null);

  return (
    <Card>
      <CardHeader>
        <CardTitle>Partner</CardTitle>
      </CardHeader>
      <CardContent className="grid gap-4">
        <p className="text-muted-foreground text-sm">Link with one partner to share meals, diet templates and live shopping lists.</p>
        {link.isPending ? (
          <Skeleton role="status" aria-label="Loading partner" className="h-24 rounded-lg" />
        ) : link.isError ? (
          <ErrorState message={problemMessage(link.error)} onRetry={() => void link.refetch()} />
        ) : link.data === null ? (
          <div className="grid gap-6 sm:grid-cols-2">
            <InviteSection invite={invite} onCreated={setInvite} />
            <AcceptForm />
          </div>
        ) : link.data.status === "pending" ? (
          <div className="grid gap-6 sm:grid-cols-2">
            <PendingSection link={link.data} invite={invite} onCreated={setInvite} onCancelled={() => setInvite(null)} />
            <AcceptForm />
          </div>
        ) : (
          <Linked link={link.data} />
        )}
      </CardContent>
    </Card>
  );
}

function CodeBox({ invite }: { invite: Invite }) {
  async function copy() {
    try {
      await navigator.clipboard.writeText(invite.code);
      toast.success("Code copied.");
    } catch {
      toast.error("Couldn't copy. Select the code and copy it by hand.");
    }
  }
  return (
    <div className="grid gap-2">
      <p className="bg-muted rounded-lg px-4 py-3 font-mono text-2xl tracking-widest select-all">{formatInviteCode(invite.code)}</p>
      <p className="text-muted-foreground text-xs">Shown once. Share it with your partner. It expires {formatWhen(invite.expires_at)}.</p>
      <div>
        <Button type="button" variant="outline" size="sm" onClick={() => void copy()}>
          Copy code
        </Button>
      </div>
    </div>
  );
}

function InviteSection({ invite, onCreated }: { invite: Invite | null; onCreated: (invite: Invite) => void }) {
  const create = useCreateInvite();
  return (
    <section className="grid content-start gap-3">
      <h3 className="text-sm font-medium">Invite your partner</h3>
      {invite ? <CodeBox invite={invite} /> : null}
      <div>
        <Button type="button" disabled={create.isPending} onClick={() => create.mutate(undefined, { onSuccess: onCreated, onError: (error) => toast.error(problemMessage(error)) })}>
          {create.isPending ? "Creating…" : "Create invite code"}
        </Button>
      </div>
    </section>
  );
}

function PendingSection({ link, invite, onCreated, onCancelled }: { link: Partnership; invite: Invite | null; onCreated: (invite: Invite) => void; onCancelled: () => void }) {
  const create = useCreateInvite();
  const cancel = useUnlinkPartner();
  return (
    <section className="grid content-start gap-3">
      <h3 className="text-sm font-medium">Invite pending</h3>
      <p className="text-sm">Waiting for your partner. The code expires {link.expires_at ? formatWhen(link.expires_at) : "soon"}.</p>
      {invite ? <CodeBox invite={invite} /> : <p className="text-muted-foreground text-xs">The code was shown when you created it. Create a new code if you need to see it again.</p>}
      <div className="flex flex-wrap gap-2">
        <Button type="button" variant="outline" disabled={create.isPending} onClick={() => create.mutate(undefined, { onSuccess: onCreated, onError: (error) => toast.error(problemMessage(error)) })}>
          Create a new code
        </Button>
        <Button type="button" variant="ghost" disabled={cancel.isPending} onClick={() => cancel.mutate(undefined, { onSuccess: onCancelled, onError: (error) => toast.error(problemMessage(error)) })}>
          Cancel invite
        </Button>
      </div>
    </section>
  );
}

function AcceptForm() {
  const accept = useAcceptInvite();
  const [error, setError] = useState<string | undefined>();
  return (
    <section className="grid content-start gap-3">
      <h3 className="text-sm font-medium">Enter a code</h3>
      <form
        noValidate
        className="grid gap-3"
        onSubmit={(event) => {
          event.preventDefault();
          const code = String(new FormData(event.currentTarget).get("invite-code") ?? "").trim();
          if (!code) return setError("Enter the code your partner sent you.");
          setError(undefined);
          accept.mutate(code, {
            onSuccess: (partnership) => toast.success(`Linked with ${partnership.display_name ?? "your partner"}.`),
            onError: (e) => (e instanceof ApiError && (e.code === "invite_invalid" || e.code === "partner_already_linked") ? setError(problemMessage(e)) : toast.error(problemMessage(e))),
          });
        }}
      >
        <Field name="invite-code" label="Invite code" autoComplete="off" autoCapitalize="characters" error={error} />
        <div>
          <Button type="submit" disabled={accept.isPending}>
            {accept.isPending ? "Linking…" : "Link accounts"}
          </Button>
        </div>
      </form>
    </section>
  );
}

function Linked({ link }: { link: Partnership }) {
  const [open, setOpen] = useState(false);
  const unlink = useUnlinkPartner();
  const name = link.display_name ?? "your partner";
  return (
    <div className="grid gap-3">
      <p className="text-sm">
        Linked with {name}
        {link.linked_at ? ` since ${formatDate(link.linked_at)}` : ""}.
      </p>
      <div>
        <Button type="button" variant="outline" onClick={() => setOpen(true)}>
          Unlink
        </Button>
      </div>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Unlink from {name}?</DialogTitle>
            <DialogDescription>
              You will stop seeing each other&apos;s shared meals, diet templates and shopping lists, and a shared list that is open will close. Copies stay with whoever made them. You need a new invite to link again.
            </DialogDescription>
          </DialogHeader>
          {unlink.error ? (
            <p role="alert" className="text-destructive text-sm">
              {problemMessage(unlink.error)}
            </p>
          ) : null}
          <div className="flex flex-wrap gap-2">
            <Button type="button" variant="destructive" disabled={unlink.isPending} onClick={() => unlink.mutate(undefined, { onSuccess: () => toast.success("Unlinked.") })}>
              {unlink.isPending ? "Unlinking…" : "Unlink"}
            </Button>
            <Button type="button" variant="outline" onClick={() => setOpen(false)}>
              Keep the link
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}
```

**Replace the contents of `web/src/app/(app)/profile/page.tsx`**

```tsx
import type { Metadata } from "next";
import { PageHeader } from "@/components/page-header";
import { ProfileSummary } from "@/features/auth/profile-summary";
import { PartnerCard } from "@/features/partner/partner-card";
import { DeleteAccount } from "@/features/profile/delete-account";
import { TargetsForm } from "@/features/profile/targets-form";

export const metadata: Metadata = { title: "Profile" };

export default function Page() {
  return (
    <>
      <PageHeader title="Profile" />
      <div className="grid max-w-3xl gap-6">
        <ProfileSummary />
        <TargetsForm />
        <PartnerCard />
        <DeleteAccount />
      </div>
    </>
  );
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/features/partner`
Expected: PASS. Two things to watch: the unlink dialog has two buttons named "Unlink" (the card's opener, hidden behind the modal, and the confirm), so the test scopes the second click with `within(dialog)`; and the clipboard is defined on the real `navigator` with `Object.defineProperty` (jsdom has none), which is why the clipboard tests set it up right before clicking "Copy code".

- [ ] **Step 5: Lint, build and commit**

```bash
make lint-web && make test-web
cd web && npx next build && cd ..
git add web/src/features/partner "web/src/app/(app)/profile/page.tsx"
git commit -m "feat(web): add the partner connection to Profile: invite code, enter a code, link status and unlink"
```

---

### Task 9: Profile: custom ingredients

**Files:**
- Create: `web/src/features/ingredients/queries.ts`, `web/src/features/ingredients/edit-ingredient.ts`, `web/src/features/ingredients/edit-ingredient-dialog.tsx`, `web/src/features/ingredients/custom-ingredients-page.tsx`, `web/src/app/(app)/profile/ingredients/page.tsx`
- Modify: `web/src/app/(app)/profile/page.tsx`
- Test: `web/src/features/ingredients/edit-ingredient.test.ts`, `web/src/features/ingredients/custom-ingredients-page.test.tsx`

**Interfaces:**
- Consumes: `api`, `unwrap`; `CI`, `parseCustomIngredient`, `CustomIngredientErrors` (`@/components/ingredient-search/custom-ingredient`); `CustomIngredientDialog`, `Ingredient`, `ingredientKeys` (`@/components/ingredient-search/...`); `NUTRIENTS`, `NutrientKey` (`@/lib/nutrition`); `CATEGORIES`, `categoryLabel`; `useDebouncedValue`; `useLoadAllPages`; `planKeys`; `Field`, `NativeSelect`, `Badge`, `Button`, `Dialog*`, `Input`, `Label`, `Skeleton`, `ErrorState`, `problemMessage`.
- Produces:
  - `ingredientUpdate(data: FormData, existing: Ingredient): { value: UpdateIngredientRequest } | { errors: CustomIngredientErrors }`: validates with `parseCustomIngredient`, then builds the update so nothing the form does not show is lost: `nutrients` carries **all 18 keys**, the 14 the form lacks copied from `existing`, the four it has (calories, protein, carbohydrates, fat) taken from the form with a blank as `null`; a blank weight per piece or density is `null` (cleared).
  - `useIngredientBrowse(text, category, enabled?, limit = 50)`: `useInfiniteQuery` over `GET /ingredients` (alphabetical with a cursor, or best matches when there is text), keyed under `ingredientKeys.all`; `useUpdateIngredient()` (`PATCH /ingredients/{id}`; refreshes the ingredient lists, the meals, and the plan, whose nutrition came from it); `useDeleteIngredient()` (`DELETE /ingredients/{id}`).
  - `EditIngredientDialog({ ingredient: Ingredient | null; onClose(): void })`: "Edit custom ingredient" with the same fields as the create dialog, prefilled; a `409 unit_not_convertible` (clearing a weight or density a meal still needs) is shown in the dialog.
  - `CustomIngredientsPage()`: a "New custom ingredient" button (the existing create dialog), a "Search ingredients" box, a "Filter by category" select, "Only my custom ingredients" (which loads every page and filters them), the results with a "Custom" badge, and "Edit {name}" / "Delete {name}" on custom rows only; "Delete" asks first and shows `ingredient_in_use` ("A meal still uses this ingredient, so it can't be deleted.") in the dialog; "Load more"; empty states.
  - Route `/profile/ingredients`, and a link to it on the Profile page.

- [ ] **Step 1: Write the failing tests**

**Create `web/src/features/ingredients/edit-ingredient.test.ts`**

```ts
import { describe, expect, it } from "vitest";
import { CI } from "@/components/ingredient-search/custom-ingredient";
import { NUTRIENTS } from "@/lib/nutrition";
import { makeIngredient, nutrients } from "@/test/fixtures";
import { ingredientUpdate } from "./edit-ingredient";

const existing = makeIngredient({
  id: "c1",
  name: "Granola",
  category: "grains_bread",
  is_custom: true,
  grams_per_piece: 40,
  density_g_per_ml: null,
  nutrients: nutrients({ calories: 450, protein: 10, carbohydrates: 60, fat: 15, fibre: 7, sodium: 120, iron: 3.5, vitamin_c: null, folate: 30 }),
});

function form(values: Partial<Record<keyof typeof CI, string>>) {
  const data = new FormData();
  const defaults = { name: "Granola", category: "grains_bread", calories: "450", protein: "10", carbohydrates: "60", fat: "15", gramsPerPiece: "40", density: "" };
  for (const [field, value] of Object.entries({ ...defaults, ...values })) data.set(CI[field as keyof typeof CI], value ?? "");
  return data;
}
function value(data: FormData) {
  const result = ingredientUpdate(data, existing);
  if ("errors" in result) throw new Error(`expected a value: ${JSON.stringify(result.errors)}`);
  return result.value;
}

describe("ingredientUpdate", () => {
  it("sends all 18 nutrients, carrying over the ones the form does not show, because the API replaces the whole set", () => {
    const body = value(form({ calories: "470" }));
    expect(Object.keys(body.nutrients ?? {}).sort()).toEqual(NUTRIENTS.map((n) => n.key).sort());
    expect(body.nutrients).toMatchObject({ calories: 470, protein: 10, carbohydrates: 60, fat: 15, fibre: 7, sodium: 120, iron: 3.5, folate: 30 });
    expect(body.nutrients?.vitamin_c).toBeNull();
    expect(body.nutrients?.zinc).toBe(0);
  });

  it("turns a blanked macro into null, which clears it, and leaves the hidden ones alone", () => {
    const body = value(form({ protein: "", fat: "" }));
    expect(body.nutrients).toMatchObject({ protein: null, fat: null, fibre: 7, sodium: 120 });
  });

  it("clears a weight per piece or a density that was blanked, and keeps one that was not", () => {
    expect(value(form({ gramsPerPiece: "", density: "0,9" }))).toMatchObject({ grams_per_piece: null, density_g_per_ml: 0.9 });
    expect(value(form({}))).toMatchObject({ grams_per_piece: 40, density_g_per_ml: null });
  });

  it("carries the name and category", () => {
    expect(value(form({ name: "  Crunchy granola ", category: "sweets_snacks" }))).toMatchObject({ name: "Crunchy granola", category: "sweets_snacks" });
  });

  it("reports the form's own mistakes and sends nothing", () => {
    expect(ingredientUpdate(form({ name: " ", calories: "abc", gramsPerPiece: "0" }), existing)).toEqual({
      errors: { name: "Give the ingredient a name.", calories: "Enter a number, for example 12.5.", gramsPerPiece: "Weight per piece must be more than 0 and at most 10000." },
    });
  });

  it("does not change the ingredient it was given", () => {
    const before = JSON.stringify(existing);
    value(form({ calories: "1" }));
    expect(JSON.stringify(existing)).toBe(before);
  });
});
```

**Create `web/src/features/ingredients/custom-ingredients-page.test.tsx`**

```tsx
// @vitest-environment jsdom
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { fakeApi, json, noContent, problem } from "@/test/fake-api";
import { makeIngredient, nutrients } from "@/test/fixtures";
import { renderWithClient } from "@/test/render";
import { CustomIngredientsPage } from "./custom-ingredients-page";

const oats = makeIngredient({ id: "g1", name: "Rolled oats" });
const granola = makeIngredient({
  id: "c1",
  name: "Granola",
  category: "grains_bread",
  is_custom: true,
  grams_per_piece: 40,
  nutrients: nutrients({ calories: 450, protein: 10, carbohydrates: 60, fat: 15, fibre: 7, iron: 3.5 }),
});
const kelp = makeIngredient({ id: "c2", name: "Kelp", category: "other", is_custom: true, nutrients: nutrients({ calories: 43 }) });
const page = (items: ReturnType<typeof makeIngredient>[], next_cursor: string | null = null) => json({ items, next_cursor });

describe("CustomIngredientsPage listing", () => {
  it("lists ingredients, marks mine as custom, and offers edit and delete on mine only", async () => {
    fakeApi({ "GET /ingredients": () => page([granola, oats, kelp]) });
    renderWithClient(<CustomIngredientsPage />);
    expect(await screen.findByText("Granola")).toBeInTheDocument();
    expect(screen.getAllByText("Custom")).toHaveLength(2);
    expect(screen.getByRole("button", { name: "Edit Granola" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Delete Kelp" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Edit Rolled oats" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Delete Rolled oats" })).not.toBeInTheDocument();
  });

  it("asks for the next page with the API's cursor", async () => {
    const fake = fakeApi({ "GET /ingredients": (req) => (req.search.get("cursor") === "c1" ? page([kelp]) : page([granola], "c1")) });
    renderWithClient(<CustomIngredientsPage />);
    await userEvent.click(await screen.findByRole("button", { name: "Load more" }));
    expect(await screen.findByText("Kelp")).toBeInTheDocument();
    expect(fake.callsTo("GET", "/ingredients")[1]?.search.get("cursor")).toBe("c1");
  });

  it("searches by name once typing settles, and filters by category", async () => {
    const fake = fakeApi({ "GET /ingredients": () => page([granola]) });
    renderWithClient(<CustomIngredientsPage />);
    await screen.findByText("Granola");
    await userEvent.type(screen.getByLabelText("Search ingredients"), "gran");
    await waitFor(() => expect(fake.calls.at(-1)?.search.get("q")).toBe("gran"));
    expect(fake.calls.map((c) => c.search.get("q"))).not.toContain("g");
    await userEvent.selectOptions(screen.getByLabelText("Filter by category"), "grains_bread");
    await waitFor(() => expect(fake.calls.at(-1)?.search.get("category")).toBe("grains_bread"));
  });

  it("shows only my custom ingredients on request, loading every page to find them", async () => {
    const fake = fakeApi({ "GET /ingredients": (req) => (req.search.get("cursor") === "c1" ? page([kelp, makeIngredient({ id: "g3", name: "Sugar" })]) : page([granola, oats], "c1")) });
    renderWithClient(<CustomIngredientsPage />);
    await screen.findByText("Rolled oats");
    await userEvent.click(screen.getByLabelText("Only my custom ingredients"));
    expect(await screen.findByText("Kelp")).toBeInTheDocument();
    expect(screen.getByText("Granola")).toBeInTheDocument();
    expect(screen.queryByText("Rolled oats")).not.toBeInTheDocument();
    expect(screen.queryByText("Sugar")).not.toBeInTheDocument();
    expect(fake.callsTo("GET", "/ingredients")).toHaveLength(2);
  });

  it("says so, with a way to make one, when there are no custom ingredients", async () => {
    fakeApi({ "GET /ingredients": () => page([oats]) });
    renderWithClient(<CustomIngredientsPage />);
    await screen.findByText("Rolled oats");
    await userEvent.click(screen.getByLabelText("Only my custom ingredients"));
    expect(await screen.findByText("You haven't created any custom ingredients yet.")).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "New custom ingredient" })).toHaveLength(2);
  });

  it("says when nothing matches a search", async () => {
    fakeApi({ "GET /ingredients": (req) => (req.search.get("q") ? page([]) : page([oats])) });
    renderWithClient(<CustomIngredientsPage />);
    await screen.findByText("Rolled oats");
    await userEvent.type(screen.getByLabelText("Search ingredients"), "zzz");
    expect(await screen.findByText("No ingredient matches.")).toBeInTheDocument();
  });

  it("offers a retry when the list cannot load", async () => {
    let fail = true;
    fakeApi({ "GET /ingredients": () => (fail ? problem(500, "internal_error") : page([granola])) });
    renderWithClient(<CustomIngredientsPage />);
    expect(await screen.findByText("Something went wrong. Try again.")).toBeInTheDocument();
    fail = false;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByText("Granola")).toBeInTheDocument();
  });
});

describe("creating", () => {
  it("makes a custom ingredient with the shared dialog", async () => {
    const fake = fakeApi({ "GET /ingredients": () => page([oats]), "POST /ingredients": () => json(makeIngredient({ id: "c9", name: "Seitan", is_custom: true }), 201) });
    renderWithClient(<CustomIngredientsPage />);
    await userEvent.click(await screen.findByRole("button", { name: "New custom ingredient" }));
    const dialog = await screen.findByRole("dialog", { name: "New custom ingredient" });
    await userEvent.type(within(dialog).getByLabelText("Ingredient name"), "Seitan");
    await userEvent.click(within(dialog).getByRole("button", { name: "Create ingredient" }));
    await waitFor(() => expect(fake.callsTo("POST", "/ingredients")).toHaveLength(1));
    expect(fake.callsTo("POST", "/ingredients")[0]?.body).toEqual({ name: "Seitan", category: "other" });
  });
});

describe("editing", () => {
  async function openEdit() {
    await userEvent.click(await screen.findByRole("button", { name: "Edit Granola" }));
    return screen.findByRole("dialog", { name: "Edit custom ingredient" });
  }

  it("shows the ingredient as it is", async () => {
    fakeApi({ "GET /ingredients": () => page([granola]) });
    renderWithClient(<CustomIngredientsPage />);
    const dialog = await openEdit();
    expect(within(dialog).getByLabelText("Ingredient name")).toHaveValue("Granola");
    expect(within(dialog).getByLabelText("Category")).toHaveValue("grains_bread");
    expect(within(dialog).getByLabelText(/Calories per 100 g/)).toHaveValue("450");
    expect(within(dialog).getByLabelText(/Weight of one piece/)).toHaveValue("40");
    expect(within(dialog).getByLabelText(/Density/)).toHaveValue("");
  });

  it("saves with all 18 nutrients, so the ones the form does not show are not erased, and refreshes what depends on them", async () => {
    const fake = fakeApi({ "GET /ingredients": () => page([granola]), "PATCH /ingredients/:id": () => json({ ...granola, name: "Crunchy granola" }) });
    const { queryClient } = renderWithClient(<CustomIngredientsPage />);
    queryClient.setQueryData(["meals", "detail", "m1"], { id: "m1" });
    const dialog = await openEdit();
    await userEvent.clear(within(dialog).getByLabelText("Ingredient name"));
    await userEvent.type(within(dialog).getByLabelText("Ingredient name"), "Crunchy granola");
    await userEvent.click(within(dialog).getByRole("button", { name: "Save ingredient" }));

    await waitFor(() => expect(fake.callsTo("PATCH", "/ingredients/c1")).toHaveLength(1));
    const body = fake.callsTo("PATCH", "/ingredients/c1")[0]?.body as { name: string; nutrients: Record<string, number | null>; grams_per_piece: number | null; density_g_per_ml: number | null };
    expect(body.name).toBe("Crunchy granola");
    expect(Object.keys(body.nutrients)).toHaveLength(18);
    expect(body.nutrients).toMatchObject({ calories: 450, protein: 10, fibre: 7, iron: 3.5 });
    expect(body).toMatchObject({ grams_per_piece: 40, density_g_per_ml: null });
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(queryClient.getQueryState(["meals", "detail", "m1"])?.isInvalidated).toBe(true);
  });

  it("clears a blanked macro and a blanked weight per piece", async () => {
    const fake = fakeApi({ "GET /ingredients": () => page([granola]), "PATCH /ingredients/:id": () => json(granola) });
    renderWithClient(<CustomIngredientsPage />);
    const dialog = await openEdit();
    await userEvent.clear(within(dialog).getByLabelText(/Protein per 100 g/));
    await userEvent.clear(within(dialog).getByLabelText(/Weight of one piece/));
    await userEvent.click(within(dialog).getByRole("button", { name: "Save ingredient" }));
    await waitFor(() => expect(fake.callsTo("PATCH", "/ingredients/c1")).toHaveLength(1));
    expect(fake.callsTo("PATCH", "/ingredients/c1")[0]?.body).toMatchObject({ grams_per_piece: null, nutrients: { protein: null, calories: 450 } });
  });

  it("flags a mistake inline and sends nothing", async () => {
    const fake = fakeApi({ "GET /ingredients": () => page([granola]) });
    renderWithClient(<CustomIngredientsPage />);
    const dialog = await openEdit();
    fireEvent.change(within(dialog).getByLabelText(/Calories per 100 g/), { target: { value: "lots" } });
    await userEvent.click(within(dialog).getByRole("button", { name: "Save ingredient" }));
    expect(await within(dialog).findByText("Enter a number, for example 12.5.")).toBeInTheDocument();
    expect(fake.callsTo("PATCH", "/ingredients/c1")).toHaveLength(0);
  });

  it("explains that a meal still needs the weight when it is cleared, and stays open", async () => {
    fakeApi({ "GET /ingredients": () => page([granola]), "PATCH /ingredients/:id": () => problem(409, "unit_not_convertible") });
    renderWithClient(<CustomIngredientsPage />);
    const dialog = await openEdit();
    await userEvent.clear(within(dialog).getByLabelText(/Weight of one piece/));
    await userEvent.click(within(dialog).getByRole("button", { name: "Save ingredient" }));
    expect(await within(dialog).findByText(/can't be measured in that unit/)).toBeInTheDocument();
    expect(screen.getByRole("dialog", { name: "Edit custom ingredient" })).toBeInTheDocument();
  });
});

describe("deleting", () => {
  it("asks first, then deletes", async () => {
    const fake = fakeApi({ "GET /ingredients": () => page([kelp]), "DELETE /ingredients/:id": () => noContent() });
    renderWithClient(<CustomIngredientsPage />);
    await userEvent.click(await screen.findByRole("button", { name: "Delete Kelp" }));
    const dialog = await screen.findByRole("dialog", { name: "Delete this ingredient?" });
    expect(within(dialog).getByText(/Delete “Kelp”\?/)).toBeInTheDocument();
    expect(fake.callsTo("DELETE", "/ingredients/c2")).toHaveLength(0);
    await userEvent.click(within(dialog).getByRole("button", { name: "Delete ingredient" }));
    await waitFor(() => expect(fake.callsTo("DELETE", "/ingredients/c2")).toHaveLength(1));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });

  it("says a meal still uses it, and stays open, when the API refuses", async () => {
    fakeApi({ "GET /ingredients": () => page([kelp]), "DELETE /ingredients/:id": () => problem(409, "ingredient_in_use") });
    renderWithClient(<CustomIngredientsPage />);
    await userEvent.click(await screen.findByRole("button", { name: "Delete Kelp" }));
    const dialog = await screen.findByRole("dialog", { name: "Delete this ingredient?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Delete ingredient" }));
    expect(await within(dialog).findByText("A meal still uses this ingredient, so it can't be deleted.")).toBeInTheDocument();
    expect(screen.getByRole("dialog", { name: "Delete this ingredient?" })).toBeInTheDocument();
  });

  it("does nothing when the person backs out", async () => {
    const fake = fakeApi({ "GET /ingredients": () => page([kelp]) });
    renderWithClient(<CustomIngredientsPage />);
    await userEvent.click(await screen.findByRole("button", { name: "Delete Kelp" }));
    const dialog = await screen.findByRole("dialog", { name: "Delete this ingredient?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Keep it" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(fake.callsTo("DELETE", "/ingredients/c2")).toHaveLength(0);
  });
});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/features/ingredients`
Expected: FAIL: `Failed to resolve import "./edit-ingredient"` and `"./custom-ingredients-page"`.

- [ ] **Step 3: Write the implementation**

**Create `web/src/features/ingredients/queries.ts`**

```ts
import { useInfiniteQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { ingredientKeys, type Ingredient } from "@/components/ingredient-search/use-ingredient-search";
import { planKeys } from "@/features/plan/queries";
import { api, unwrap } from "@/lib/api/client";
import type { components } from "@/lib/api/schema.gen";
import type { IngredientCategory } from "@/lib/ingredient-categories";

export type UpdateIngredientRequest = components["schemas"]["UpdateIngredientRequest"];

/** Browse alphabetically (with a cursor), or search by name (best matches, no cursor), optionally within one category. */
export function useIngredientBrowse(text: string, category: IngredientCategory | "", limit = 50) {
  const q = text.trim().slice(0, 100);
  return useInfiniteQuery({
    queryKey: [...ingredientKeys.all, "browse", { q, category, limit }] as const,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) => unwrap(api.GET("/ingredients", { params: { query: { q: q || undefined, category: category || undefined, cursor: pageParam, limit } } })),
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  });
}

/** An ingredient's nutrients feed every meal that uses it, and so the plan: refresh them all. */
export function useUpdateIngredient() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, body }: { id: string; body: UpdateIngredientRequest }): Promise<Ingredient> => unwrap(api.PATCH("/ingredients/{id}", { params: { path: { id } }, body })),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ingredientKeys.all });
      void queryClient.invalidateQueries({ queryKey: ["meals"] });
      void queryClient.invalidateQueries({ queryKey: planKeys.all });
    },
  });
}

export function useDeleteIngredient() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (id: string): Promise<void> => {
      await unwrap(api.DELETE("/ingredients/{id}", { params: { path: { id } } }));
    },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ingredientKeys.all }),
  });
}
```

**Create `web/src/features/ingredients/edit-ingredient.ts`**

```ts
import { parseCustomIngredient, type CustomIngredientErrors } from "@/components/ingredient-search/custom-ingredient";
import type { Ingredient } from "@/components/ingredient-search/use-ingredient-search";
import { NUTRIENTS, type NutrientKey } from "@/lib/nutrition";
import type { UpdateIngredientRequest } from "./queries";

const SHOWN = ["calories", "protein", "carbohydrates", "fat"] as const;

/**
 * The update for an edited custom ingredient. `nutrients` replaces the whole set on the API, so all 18 keys are sent: the 14 this
 * form has no field for are copied from `existing` so editing the name never erases the vitamins; the four it shows come from the
 * form (a blank one is `null`). A blank weight per piece or density is `null`, which clears it.
 */
export function ingredientUpdate(data: FormData, existing: Ingredient): { value: UpdateIngredientRequest } | { errors: CustomIngredientErrors } {
  const parsed = parseCustomIngredient(data);
  if ("errors" in parsed) return parsed;
  const form = parsed.value;

  const nutrients = Object.fromEntries(NUTRIENTS.map(({ key }) => [key, existing.nutrients[key]])) as Record<NutrientKey, number | null>;
  for (const key of SHOWN) nutrients[key] = form.nutrients?.[key] ?? null;

  return {
    value: {
      name: form.name,
      category: form.category,
      grams_per_piece: form.grams_per_piece ?? null,
      density_g_per_ml: form.density_g_per_ml ?? null,
      nutrients,
    },
  };
}
```

**Create `web/src/features/ingredients/edit-ingredient-dialog.tsx`**

```tsx
"use client";

import { useState } from "react";
import { toast } from "sonner";
import { CI, type CustomIngredientErrors } from "@/components/ingredient-search/custom-ingredient";
import type { Ingredient } from "@/components/ingredient-search/use-ingredient-search";
import { Field } from "@/components/field";
import { NativeSelect } from "@/components/native-select";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { ApiError, problemMessage } from "@/lib/api/problem";
import { CATEGORIES, categoryLabel } from "@/lib/ingredient-categories";
import { ingredientUpdate } from "./edit-ingredient";
import { useUpdateIngredient } from "./queries";

const text = (value: number | null) => (value === null ? "" : String(value));

/** Edit one of my custom ingredients. Mount it with a `key` per ingredient so it starts fresh. */
export function EditIngredientDialog({ ingredient, onClose }: { ingredient: Ingredient | null; onClose: () => void }) {
  return (
    <Dialog open={ingredient !== null} onOpenChange={(open) => !open && onClose()}>
      {ingredient ? (
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Edit custom ingredient</DialogTitle>
            <DialogDescription>Amounts are per 100 g. Nutrients not shown here are kept as they are.</DialogDescription>
          </DialogHeader>
          <EditForm ingredient={ingredient} onDone={onClose} />
        </DialogContent>
      ) : null}
    </Dialog>
  );
}

function EditForm({ ingredient, onDone }: { ingredient: Ingredient; onDone: () => void }) {
  const update = useUpdateIngredient();
  const [errors, setErrors] = useState<CustomIngredientErrors>({});
  const [failure, setFailure] = useState<string | null>(null);

  return (
    <form
      noValidate
      className="grid gap-4"
      onSubmit={(event) => {
        event.preventDefault();
        const result = ingredientUpdate(new FormData(event.currentTarget), ingredient);
        if ("errors" in result) return setErrors(result.errors);
        setErrors({});
        setFailure(null);
        update.mutate(
          { id: ingredient.id, body: result.value },
          {
            onSuccess: () => {
              toast.success("Ingredient saved.");
              onDone();
            },
            onError: (error) => (error instanceof ApiError && error.code === "unit_not_convertible" ? setFailure(problemMessage(error)) : toast.error(problemMessage(error))),
          },
        );
      }}
    >
      <Field name={CI.name} label="Ingredient name" defaultValue={ingredient.name} maxLength={200} error={errors.name} autoComplete="off" />
      <div className="grid gap-1.5">
        <Label htmlFor={CI.category}>Category</Label>
        <NativeSelect id={CI.category} name={CI.category} defaultValue={ingredient.category}>
          {CATEGORIES.map((category) => (
            <option key={category} value={category}>
              {categoryLabel(category)}
            </option>
          ))}
        </NativeSelect>
      </div>
      <div className="grid grid-cols-2 gap-3">
        <Field name={CI.calories} label="Calories per 100 g (kcal)" inputMode="decimal" defaultValue={text(ingredient.nutrients.calories)} error={errors.calories} />
        <Field name={CI.protein} label="Protein per 100 g (g)" inputMode="decimal" defaultValue={text(ingredient.nutrients.protein)} error={errors.protein} />
        <Field name={CI.carbohydrates} label="Carbohydrates per 100 g (g)" inputMode="decimal" defaultValue={text(ingredient.nutrients.carbohydrates)} error={errors.carbohydrates} />
        <Field name={CI.fat} label="Fat per 100 g (g)" inputMode="decimal" defaultValue={text(ingredient.nutrients.fat)} error={errors.fat} />
      </div>
      <div className="grid grid-cols-2 gap-3">
        <Field name={CI.gramsPerPiece} label="Weight of one piece (g)" inputMode="decimal" defaultValue={text(ingredient.grams_per_piece)} hint="Lets you use pieces." error={errors.gramsPerPiece} />
        <Field name={CI.density} label="Density (g per ml)" inputMode="decimal" defaultValue={text(ingredient.density_g_per_ml)} hint="Lets you use millilitres." error={errors.density} />
      </div>
      {failure ? (
        <p role="alert" className="text-destructive text-sm">
          {failure}
        </p>
      ) : null}
      <Button type="submit" size="lg" className="h-11 text-base md:text-sm" disabled={update.isPending}>
        {update.isPending ? "Saving…" : "Save ingredient"}
      </Button>
    </form>
  );
}
```

**Create `web/src/features/ingredients/custom-ingredients-page.tsx`**

```tsx
"use client";

import { Plus } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { CustomIngredientDialog } from "@/components/ingredient-search/custom-ingredient-dialog";
import type { Ingredient } from "@/components/ingredient-search/use-ingredient-search";
import { ErrorState } from "@/components/error-state";
import { NativeSelect } from "@/components/native-select";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { problemMessage } from "@/lib/api/problem";
import { CATEGORIES, categoryLabel, type IngredientCategory } from "@/lib/ingredient-categories";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { useLoadAllPages } from "@/lib/use-load-all-pages";
import { EditIngredientDialog } from "./edit-ingredient-dialog";
import { useDeleteIngredient, useIngredientBrowse } from "./queries";

/** Find, create, edit and delete my custom ingredients. The catalogue's own ingredients are listed but cannot be changed. */
export function CustomIngredientsPage() {
  const [text, setText] = useState("");
  const [category, setCategory] = useState<IngredientCategory | "">("");
  const [onlyMine, setOnlyMine] = useState(false);
  const [creating, setCreating] = useState(false);
  const [editing, setEditing] = useState<Ingredient | null>(null);
  const [deleting, setDeleting] = useState<Ingredient | null>(null);

  const query = useIngredientBrowse(useDebouncedValue(text, 250), category);
  // The API cannot filter to my own, so this loads every page and filters here. It is opt-in for that reason.
  useLoadAllPages({ ...query, hasNextPage: onlyMine && query.hasNextPage });

  const all = query.data?.pages.flatMap((page) => page.items) ?? [];
  const shown = onlyMine ? all.filter((ingredient) => ingredient.is_custom) : all;
  const newButton = (
    <Button type="button" onClick={() => setCreating(true)}>
      <Plus aria-hidden />
      New custom ingredient
    </Button>
  );

  return (
    <div className="grid gap-4">
      <div className="flex justify-end">{newButton}</div>
      <div className="flex flex-wrap items-end gap-3">
        <div className="grid min-w-48 flex-1 gap-1.5">
          <Label htmlFor="ingredient-search">Search ingredients</Label>
          <Input id="ingredient-search" autoComplete="off" maxLength={100} className="h-11 text-base md:text-sm" value={text} onChange={(event) => setText(event.target.value)} />
        </div>
        <NativeSelect aria-label="Filter by category" value={category} onChange={(event) => setCategory(event.target.value as IngredientCategory | "")}>
          <option value="">All categories</option>
          {CATEGORIES.map((c) => (
            <option key={c} value={c}>
              {categoryLabel(c)}
            </option>
          ))}
        </NativeSelect>
      </div>
      <label className="flex items-center gap-2 text-sm">
        <input type="checkbox" className="accent-primary size-4" checked={onlyMine} onChange={(event) => setOnlyMine(event.target.checked)} />
        Only my custom ingredients
      </label>

      {query.isPending ? (
        <ul aria-label="Loading ingredients" className="grid gap-2">
          {[0, 1, 2].map((n) => (
            <li key={n}>
              <Skeleton className="h-14 rounded-xl" />
            </li>
          ))}
        </ul>
      ) : query.data === undefined ? (
        <ErrorState message={problemMessage(query.error)} onRetry={() => void query.refetch()} />
      ) : shown.length === 0 && !(onlyMine && query.hasNextPage) ? (
        <div className="bg-card flex flex-col items-center gap-3 rounded-xl border px-6 py-10 text-center">
          <p className="font-medium">{onlyMine ? "You haven't created any custom ingredients yet." : "No ingredient matches."}</p>
          {onlyMine ? newButton : null}
        </div>
      ) : (
        <ul className="grid gap-2">
          {shown.map((ingredient) => (
            <li key={ingredient.id} className="bg-card flex items-center gap-3 rounded-xl border px-4 py-3">
              <div className="min-w-0 flex-1">
                <p className="truncate font-medium">{ingredient.name}</p>
                <p className="text-muted-foreground text-xs">{categoryLabel(ingredient.category)}</p>
              </div>
              {ingredient.is_custom ? (
                <>
                  <Badge variant="secondary">Custom</Badge>
                  <Button type="button" variant="ghost" size="sm" aria-label={`Edit ${ingredient.name}`} onClick={() => setEditing(ingredient)}>
                    Edit
                  </Button>
                  <Button type="button" variant="ghost" size="sm" aria-label={`Delete ${ingredient.name}`} onClick={() => setDeleting(ingredient)}>
                    Delete
                  </Button>
                </>
              ) : null}
            </li>
          ))}
        </ul>
      )}
      {query.data !== undefined && query.hasNextPage && !onlyMine ? (
        <Button type="button" variant="outline" className="justify-self-center" disabled={query.isFetchingNextPage} onClick={() => void query.fetchNextPage()}>
          {query.isFetchingNextPage ? "Loading…" : "Load more"}
        </Button>
      ) : null}

      <CustomIngredientDialog open={creating} onOpenChange={setCreating} initialName={text.trim()} onCreated={() => toast.success("Ingredient created.")} />
      <EditIngredientDialog key={editing?.id ?? "none"} ingredient={editing} onClose={() => setEditing(null)} />
      <DeleteIngredientDialog key={deleting?.id ?? "none"} ingredient={deleting} onClose={() => setDeleting(null)} />
    </div>
  );
}

function DeleteIngredientDialog({ ingredient, onClose }: { ingredient: Ingredient | null; onClose: () => void }) {
  const remove = useDeleteIngredient();
  return (
    <Dialog open={ingredient !== null} onOpenChange={(open) => !open && onClose()}>
      {ingredient ? (
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete this ingredient?</DialogTitle>
            <DialogDescription>
              Delete “{ingredient.name}”? An ingredient a meal still uses cannot be deleted; remove it from those meals first.
            </DialogDescription>
          </DialogHeader>
          {remove.error ? (
            <p role="alert" className="text-destructive text-sm">
              {problemMessage(remove.error)}
            </p>
          ) : null}
          <div className="flex flex-wrap gap-2">
            <Button
              type="button"
              variant="destructive"
              disabled={remove.isPending}
              onClick={() =>
                remove.mutate(ingredient.id, {
                  onSuccess: () => {
                    toast.success("Ingredient deleted.");
                    onClose();
                  },
                })
              }
            >
              {remove.isPending ? "Deleting…" : "Delete ingredient"}
            </Button>
            <Button type="button" variant="outline" onClick={onClose}>
              Keep it
            </Button>
          </div>
        </DialogContent>
      ) : null}
    </Dialog>
  );
}
```

**Create `web/src/app/(app)/profile/ingredients/page.tsx`**

```tsx
import type { Metadata } from "next";
import { ArrowLeft } from "lucide-react";
import Link from "next/link";
import { PageHeader } from "@/components/page-header";
import { CustomIngredientsPage } from "@/features/ingredients/custom-ingredients-page";

export const metadata: Metadata = { title: "Custom ingredients" };

export default function Page() {
  return (
    <>
      <Link href="/profile" className="text-muted-foreground hover:text-foreground mb-4 inline-flex items-center gap-1 text-sm">
        <ArrowLeft aria-hidden className="size-4" />
        Profile
      </Link>
      <PageHeader title="Custom ingredients" description="The ingredients you made yourself, with their nutrition per 100 g." />
      <CustomIngredientsPage />
    </>
  );
}
```

**Replace the contents of `web/src/app/(app)/profile/page.tsx`**

```tsx
import type { Metadata } from "next";
import Link from "next/link";
import { PageHeader } from "@/components/page-header";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { ProfileSummary } from "@/features/auth/profile-summary";
import { PartnerCard } from "@/features/partner/partner-card";
import { DeleteAccount } from "@/features/profile/delete-account";
import { TargetsForm } from "@/features/profile/targets-form";

export const metadata: Metadata = { title: "Profile" };

export default function Page() {
  return (
    <>
      <PageHeader title="Profile" />
      <div className="grid max-w-3xl gap-6">
        <ProfileSummary />
        <TargetsForm />
        <PartnerCard />
        <Card>
          <CardHeader>
            <CardTitle>Custom ingredients</CardTitle>
          </CardHeader>
          <CardContent className="grid gap-3">
            <p className="text-muted-foreground text-sm">Ingredients you made yourself: edit their nutrition or delete them.</p>
            <div>
              <Button asChild variant="outline">
                <Link href="/profile/ingredients">Manage custom ingredients</Link>
              </Button>
            </div>
          </CardContent>
        </Card>
        <DeleteAccount />
      </div>
    </>
  );
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/features/ingredients`
Expected: PASS. Notes: the page lists both the edit dialog's and the create dialog's `ci-*` field ids, but only one dialog is ever mounted; the "Only my custom ingredients" test expects exactly 2 requests because `useLoadAllPages` stops once `hasNextPage` is false; in the delete and edit dialogs the row's own buttons are hidden behind the modal, so `getByRole` for the dialog's buttons is unambiguous.

- [ ] **Step 5: Lint, build and commit**

```bash
make lint-web && make test-web
cd web && npx next build && cd ..
git add web/src/features/ingredients "web/src/app/(app)/profile"
git commit -m "feat(web): add custom ingredient management to Profile, editing without erasing the nutrients the form does not show"
```

### Task 10: Shopping and Profile in Playwright

**Files:**
- Create: `web/e2e/today.ts`, `web/e2e/shopping.spec.ts`, `web/e2e/profile.spec.ts`

**Interfaces:**
- Consumes: `e2e/support.ts` (`api`, `newAccount`, `register`, `PASSWORD`) from the Meals plan; the running Compose stack (`make e2e-web`); everything from Tasks 1 to 9.
- Produces: `localToday(page)` in `e2e/today.ts` (the browser's own local date, the way the app derives "today"), and these flows: generate a list from the plan, check off and quick-add, reload; a partner's check-off appearing live in a second browser context and the list disappearing when the partner unlinks (the spec's flows 4 to 6, which also prove the proxy does not buffer the event stream); targets appearing on Today; linking and unlinking a partner through the Profile screen; editing a custom ingredient without erasing its nutrients and deleting one; deleting the account.

- [ ] **Step 1: Write the flows**

**Create `web/e2e/today.ts`**

```ts
import type { Page } from "@playwright/test";

/** The browser's own local date as `YYYY-MM-DD`, the way the app derives "today". */
export async function localToday(page: Page): Promise<string> {
  return page.evaluate(() => {
    const d = new Date();
    return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
  });
}
```

**Create `web/e2e/shopping.spec.ts`**

```ts
import { expect, test } from "@playwright/test";
import { api, newAccount, register } from "./support";
import { localToday } from "./today";

type Created = { id: string };

const OATS = { name: "Rolled oats", category: "grains_bread", nutrients: { calories: 380, protein: 13, carbohydrates: 60, fat: 7 } };

test("generate a list from the plan, see it by aisle, check an item off, quick-add another, and find it all again after a reload", async ({ page }) => {
  await register(page, newAccount());
  const today = await localToday(page);
  const oats = await api<Created>(page, "POST", "/ingredients", OATS);
  const porridge = await api<Created>(page, "POST", "/meals", { name: "Porridge", servings: 1 });
  await api(page, "PUT", `/meals/${porridge.id}/ingredients`, { items: [{ ingredient_id: oats.id, quantity: 100, unit: "g" }] });
  await api(page, "PUT", `/plan/${today}/breakfast`, { meal_id: porridge.id, portion: 2 });

  await page.goto("/shopping");
  await expect(page.getByText("No shopping lists yet")).toBeVisible();
  await page.getByRole("button", { name: "Generate from plan" }).first().click();
  await page.getByRole("dialog", { name: "Generate from your plan" }).getByRole("button", { name: "Generate list" }).click();

  // 100 g of oats per serving, planned at a portion of 2: 200 g, under its aisle.
  await expect(page).toHaveURL(/\/shopping\/[0-9a-f-]{36}$/);
  await expect(page.getByText(/From your plan ·/)).toBeVisible();
  const grains = page.getByRole("region", { name: "Grains and bread" });
  await expect(grains.getByRole("checkbox", { name: /Rolled oats/ })).toBeVisible();
  await expect(grains.getByText("200 g")).toBeVisible();
  await expect(page.getByText("0 of 1 checked")).toBeVisible();

  await grains.getByRole("checkbox", { name: /Rolled oats/ }).check();
  await expect(page.getByText("1 of 1 checked")).toBeVisible();

  await page.getByLabel("Add an item").fill("Milk");
  await page.keyboard.press("Enter");
  const other = page.getByRole("region", { name: "Other" });
  await expect(other.getByRole("checkbox", { name: /Milk/ })).toBeEnabled();
  await expect(page.getByText("1 of 2 checked")).toBeVisible();

  await page.reload();
  await expect(page.getByRole("region", { name: "Grains and bread" }).getByRole("checkbox", { name: /Rolled oats/ })).toBeChecked();
  await expect(page.getByRole("region", { name: "Other" }).getByRole("checkbox", { name: /Milk/ })).not.toBeChecked();

  // The list shows up on the lists page too, with the range it was built from.
  await page.goto("/shopping");
  await expect(page.getByRole("link", { name: /From your plan/ })).toBeVisible();
});

test("a shared list updates live in the partner's browser, and is gone for them once the link ends", async ({ page, browser, baseURL }) => {
  await register(page, newAccount());
  const partnerContext = await browser.newContext({ baseURL });
  const partnerPage = await partnerContext.newPage();
  try {
    await register(partnerPage, newAccount());
    const invite = await api<{ code: string }>(page, "POST", "/partner/invite");
    await api(partnerPage, "POST", "/partner/accept", { code: invite.code });

    const list = await api<Created>(page, "POST", "/shopping-lists", { name: "Barbecue", shared_with_partner: true });
    await api(page, "POST", `/shopping-lists/${list.id}/items`, { name: "Sausages" });
    await api(page, "POST", `/shopping-lists/${list.id}/items`, { name: "Buns" });

    await page.goto(`/shopping/${list.id}`);
    await partnerPage.goto(`/shopping/${list.id}`);
    await expect(page.getByText(/^Shared with /)).toBeVisible();
    await expect(partnerPage.getByText(/’s list$/)).toBeVisible();
    await expect(partnerPage.getByRole("button", { name: "List settings" })).toHaveCount(0);

    // One person ticks, the other sees it without reloading: the event stream reaches the browser unbuffered.
    await page.getByRole("checkbox", { name: /Sausages/ }).check();
    await expect(partnerPage.getByRole("checkbox", { name: /Sausages/ })).toBeChecked({ timeout: 10_000 });
    await expect(partnerPage.getByText(/^Checked by /)).toBeVisible();

    // And the other way round: the partner may tick too.
    await partnerPage.getByRole("checkbox", { name: /Buns/ }).check();
    await expect(page.getByRole("checkbox", { name: /Buns/ })).toBeChecked({ timeout: 10_000 });

    // The owner ends the link while the partner still has the list open: the stream closes and the page says so.
    await api(page, "DELETE", "/partner");
    await expect(partnerPage.getByText("That list isn't available. It may have been deleted, or it is no longer shared with you.")).toBeVisible({ timeout: 20_000 });
    await expect(partnerPage.getByRole("checkbox", { name: /Sausages/ })).toHaveCount(0);
  } finally {
    await partnerContext.close();
  }
});

test("the owner deleting a shared list tells the partner who has it open", async ({ page, browser, baseURL }) => {
  await register(page, newAccount());
  const partnerContext = await browser.newContext({ baseURL });
  const partnerPage = await partnerContext.newPage();
  try {
    await register(partnerPage, newAccount());
    const invite = await api<{ code: string }>(page, "POST", "/partner/invite");
    await api(partnerPage, "POST", "/partner/accept", { code: invite.code });
    const list = await api<Created>(page, "POST", "/shopping-lists", { name: "Party", shared_with_partner: true });
    await api(page, "POST", `/shopping-lists/${list.id}/items`, { name: "Crisps" });

    await partnerPage.goto(`/shopping/${list.id}`);
    await expect(partnerPage.getByRole("checkbox", { name: /Crisps/ })).toBeVisible();

    await page.goto(`/shopping/${list.id}`);
    await page.getByRole("button", { name: "List settings" }).click();
    const dialog = page.getByRole("dialog", { name: "List settings" });
    await dialog.getByRole("button", { name: "Delete list" }).click();
    await dialog.getByRole("button", { name: "Delete list" }).click();
    await expect(page).toHaveURL(/\/shopping$/);

    await expect(partnerPage.getByText("This list was deleted.")).toBeVisible({ timeout: 10_000 });
  } finally {
    await partnerContext.close();
  }
});
```

**Create `web/e2e/profile.spec.ts`**

```ts
import { expect, test } from "@playwright/test";
import { PASSWORD, api, newAccount, register } from "./support";
import { localToday } from "./today";

type Created = { id: string };

test("daily targets set in Profile show up as rings on Today, and clearing one says there is no target", async ({ page }) => {
  await register(page, newAccount());
  const today = await localToday(page);
  const oats = await api<Created>(page, "POST", "/ingredients", { name: "Rolled oats", category: "grains_bread", nutrients: { calories: 380, protein: 13, carbohydrates: 60, fat: 7 } });
  const porridge = await api<Created>(page, "POST", "/meals", { name: "Porridge", servings: 1 });
  await api(page, "PUT", `/meals/${porridge.id}/ingredients`, { items: [{ ingredient_id: oats.id, quantity: 100, unit: "g" }] });
  await api(page, "PUT", `/plan/${today}/breakfast`, { meal_id: porridge.id, portion: 1 });

  await page.goto("/today");
  await expect(page.getByRole("region", { name: "Today's totals" }).getByText("No target set").first()).toBeVisible();

  await page.goto("/profile");
  await page.getByLabel("Calories (kcal)").fill("2000");
  await page.getByRole("button", { name: "Save targets" }).click();
  await expect(page.getByText("Targets saved.")).toBeVisible();

  await page.goto("/today");
  await expect(page.getByRole("region", { name: "Today's totals" }).getByText("19% of 2,000 kcal")).toBeVisible();

  await page.goto("/profile");
  await page.getByLabel("Calories (kcal)").fill("");
  await page.getByRole("button", { name: "Save targets" }).click();
  await expect(page.getByText("Targets saved.")).toBeVisible();
  await page.goto("/today");
  await expect(page.getByRole("group", { name: "Calories" }).getByText("No target set")).toBeVisible();
});

test("link a partner with an invite code through Profile, then unlink", async ({ page, browser, baseURL }) => {
  await register(page, newAccount());
  const partnerContext = await browser.newContext({ baseURL });
  const partnerPage = await partnerContext.newPage();
  try {
    await register(partnerPage, newAccount());

    await page.goto("/profile");
    await page.getByRole("button", { name: "Create invite code" }).click();
    const shown = page.getByText(/^[A-Z0-9]{4}-[A-Z0-9]{4}$/);
    await expect(shown).toBeVisible();
    const code = (await shown.textContent()) ?? "";
    await expect(page.getByText(/Waiting for your partner/)).toBeVisible();

    await partnerPage.goto("/profile");
    await partnerPage.getByLabel("Invite code").fill("not-a-code");
    await partnerPage.getByRole("button", { name: "Link accounts" }).click();
    await expect(partnerPage.getByText("That invite code isn't valid. It may have expired or already been used.")).toBeVisible();
    await partnerPage.getByLabel("Invite code").fill(code);
    await partnerPage.getByRole("button", { name: "Link accounts" }).click();
    await expect(partnerPage.getByText(/Linked with .* since /)).toBeVisible();

    await page.reload();
    await expect(page.getByText(/Linked with .* since /)).toBeVisible();
    await page.getByRole("button", { name: "Unlink" }).click();
    const dialog = page.getByRole("dialog", { name: /Unlink from/ });
    await expect(dialog.getByText(/need a new invite to link again/)).toBeVisible();
    await dialog.getByRole("button", { name: "Unlink" }).click();
    await expect(page.getByRole("button", { name: "Create invite code" })).toBeVisible();

    await partnerPage.reload();
    await expect(partnerPage.getByRole("button", { name: "Create invite code" })).toBeVisible();
  } finally {
    await partnerContext.close();
  }
});

test("editing a custom ingredient keeps the nutrients the form does not show, and one a meal uses cannot be deleted", async ({ page }) => {
  await register(page, newAccount());
  const granola = await api<Created>(page, "POST", "/ingredients", {
    name: "Granola",
    category: "grains_bread",
    grams_per_piece: 40,
    nutrients: { calories: 450, protein: 10, carbohydrates: 60, fat: 15, fibre: 7, iron: 3.5 },
  });
  const meal = await api<Created>(page, "POST", "/meals", { name: "Breakfast bowl", servings: 1 });
  await api(page, "PUT", `/meals/${meal.id}/ingredients`, { items: [{ ingredient_id: granola.id, quantity: 100, unit: "g" }] });

  await page.goto("/profile/ingredients");
  await page.getByLabel("Only my custom ingredients").check();
  await expect(page.getByText("Granola")).toBeVisible();
  await page.getByRole("button", { name: "Edit Granola" }).click();
  const edit = page.getByRole("dialog", { name: "Edit custom ingredient" });
  await edit.getByLabel(/Calories per 100 g/).fill("470");
  await edit.getByRole("button", { name: "Save ingredient" }).click();
  await expect(edit).toBeHidden();

  // The fibre and iron the form has no field for must still be there.
  const found = await api<{ items: { name: string; nutrients: Record<string, number | null> }[] }>(page, "GET", "/ingredients?q=Granola");
  const saved = found.items.find((i) => i.name === "Granola");
  expect(saved?.nutrients).toMatchObject({ calories: 470, protein: 10, fibre: 7, iron: 3.5 });

  // A meal still uses it, so it cannot be deleted.
  await page.getByRole("button", { name: "Delete Granola" }).click();
  const remove = page.getByRole("dialog", { name: "Delete this ingredient?" });
  await remove.getByRole("button", { name: "Delete ingredient" }).click();
  await expect(remove.getByText("A meal still uses this ingredient, so it can't be deleted.")).toBeVisible();
  await remove.getByRole("button", { name: "Keep it" }).click();

  // One nothing uses can be made and deleted from the page.
  await page.getByRole("button", { name: "New custom ingredient" }).first().click();
  const create = page.getByRole("dialog", { name: "New custom ingredient" });
  await create.getByLabel("Ingredient name").fill("Kelp");
  await create.getByRole("button", { name: "Create ingredient" }).click();
  await expect(page.getByText("Kelp")).toBeVisible();
  await page.getByRole("button", { name: "Delete Kelp" }).click();
  await page.getByRole("dialog", { name: "Delete this ingredient?" }).getByRole("button", { name: "Delete ingredient" }).click();
  await expect(page.getByText("Kelp")).toHaveCount(0);
});

test("deleting the account needs the email typed, ends the session, and the account can no longer sign in", async ({ page }) => {
  const account = newAccount();
  await register(page, account);
  await page.goto("/profile");
  await page.getByRole("button", { name: "Delete account" }).click();
  const dialog = page.getByRole("dialog", { name: "Delete your account?" });
  const confirm = dialog.getByRole("button", { name: "Delete my account" });
  await expect(confirm).toBeDisabled();
  await dialog.getByLabel(/to confirm/).fill(account.email);
  await expect(confirm).toBeEnabled();
  await confirm.click();

  await expect(page).toHaveURL(/\/login$/);
  expect(await page.evaluate(() => document.cookie)).toBe("");
  await page.goto("/today");
  await expect(page).toHaveURL(/\/login\?next=%2Ftoday$/);

  await page.getByLabel("Email").fill(account.email);
  await page.getByLabel("Password").fill(PASSWORD);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("alert").filter({ hasText: "don't match" })).toHaveText("That email and password don't match.");
});
```

- [ ] **Step 2: Run the flows**

Run: `make e2e-web` (needs Docker; builds the stack under its own project name and ports, runs every Playwright spec, tears the stack down).
Expected: PASS for the auth, meals, plan, shopping and profile specs. On a failure, open the trace: `cd web && npx playwright show-trace test-results/<test>/trace.zip`. If Docker is not available locally, say so in the PR: CI's `e2e` job (`web.yml`) runs these flows.

Notes for a red run: the live-update assertions wait up to 10 to 20 seconds on purpose (a browser reconnects a dropped event stream after about three seconds, and the unlink case needs that reconnect to learn it got a `404`); if the partner's tick never arrives, the proxy is buffering the stream, which is a Foundation-level bug to fix in `web/src/server/forward.ts`, not to work around here. Three flows accept one partner invite each, well inside the API's limit of 10 per hour per client address for a stack that is torn down after every run. In "link a partner through Profile" the invite code is read from the page, so it needs the grouped `ABCD-EFGH` form to be the only text of that shape.

- [ ] **Step 3: Commit**

```bash
make lint-web
git add web/e2e
git commit -m "test(web): cover generating and checking off a list, live partner updates and unlink, targets, the partner link, custom ingredients and account deletion end to end"
```

---

### Task 11: Docs

**Files:**
- Modify: `web/CLAUDE.md`, `docs/superpowers/specs/2026-09-25-web-app-design.md`

**Interfaces:**
- Consumes: the behaviour built in Tasks 1 to 10.
- Produces: `web/CLAUDE.md` describes the shopping, profile, partner and ingredients layout and the new gotchas; the web spec records the live-update, edit-conflict and profile decisions, and the two API gaps this plan worked around.

- [ ] **Step 1: Update `web/CLAUDE.md`**

After the bullet that begins ``- `src/features/plan/`:``, add:

```markdown
- `src/features/shopping/`: `queries.ts` (lists, one list, optimistic `useCheckItem` / `useAddItem`, conflict-aware `useEditItem`), `list-cache.ts` (pure edits of a cached list, `isStaleEvent`), `list-events.ts` + `use-list-events.ts` (the SSE rules), `items.ts` (aisle grouping, quantity text), `shopping-page` / `shopping-lists` / `new-list-dialog` / `generate-list-dialog` / `range.ts`, `shopping-list-view` / `item-row` / `quick-add` / `edit-item-dialog` / `list-settings-dialog`.
- `src/features/profile/`: `targets.ts` + `targets-form`, `delete-account`. `src/features/partner/`: `partner-card` (invite code, enter a code, unlink), `invite-code.ts`. `src/features/ingredients/`: custom ingredient management (`edit-ingredient.ts` builds the update).
```

After the last bullet under Gotchas, add:

```markdown
- Live lists are a plain same-origin `EventSource` on `/api/shopping-lists/{id}/events` (the proxy attaches the bearer). The stream says what changed, never to what, so `useListEvents` refetches instead of patching, except for an event at or below the cached item's version (often your own change: ignored) and a deleted item (removed). A closed connection (`readyState` 2) is a refusal: refetch, and the `404` says the list is gone. Tests drive it with `FakeEventSource` (`src/test/fake-event-source.ts`).
- A `404` from a list fetch replaces the list even while items are cached: access is gone (unlinked, unshared, deleted). Other load failures keep showing what is cached.
- Item edits go against the version the form was opened on, not the latest: someone else's change is then a `409 version_conflict`, whose `current` item is on `ApiError.body`, never a silent overwrite. Check-off needs no version. With two quick taps in flight an earlier answer never overrides the later tap (`useCheckItem` leaves the screen alone while another is pending).
- `PATCH /ingredients/{id}` with `nutrients` replaces the whole set. Anything that edits an ingredient through a form that shows only some nutrients must send all 18 (`edit-ingredient.ts`), or it silently erases the rest.
- `ApiError.body` is the whole problem document. Use it for members the UI does not model (`current`), never to parse messages.
- Irreversible actions ask in the UI even when the API does not (`DELETE /me` needs no re-authentication): the account's email is typed. Sign out and account deletion end with `hardNavigate`, a full page load.
- The API has no "only my custom ingredients" filter, so that view loads every page and filters in the browser (opt-in). If the catalogue grows, add a query parameter to `openapi.yaml` first.
```

- [ ] **Step 2: Record the decisions in the web spec**

In `docs/superpowers/specs/2026-09-25-web-app-design.md`, after the paragraph that begins `**Plan and Today.**`, add:

```markdown
**Shopping and live lists.** A list is one query; check-off and quick-add are optimistic on it, and nothing else is. Shared lists stay live through a same-origin `EventSource`: the stream carries ids and versions, so the client refetches when an event is newer than what it holds or names an unknown item, and ignores an event at or below the cached version (its own changes echo back). A `404` on a list (unlinked, unshared, deleted) replaces it with "not available" even while items are cached. An item edit is saved against the version the form was opened on, so a concurrent change is a `409 version_conflict` (the other version comes back and the form restarts on it), never an overwrite, and a live update never wipes what is being typed. A checked item is attributed to the partner only when the partner checked it.

**Profile.** Targets are four optional numbers (a blank clears one; `0` calories is refused, `0` for a macro is allowed) and refresh Today and the plan's totals. The partner card shows an invite code once, when it is made, keeps it for the session while the invite is pending, and ends the link only after saying what ends. Custom ingredients are edited with all 18 nutrients sent, because the API replaces the whole set. Deleting the account needs the email typed and ends with a full page load.
```

Then add this line as a new paragraph at the end of §11 "Out of scope", after its existing paragraph:

```markdown
API gaps worked around in v1, to fix spec-first when they bite: no "mine only" filter on `GET /ingredients` (the custom ingredients page filters in the browser), and no entry-addressed plan endpoint (a single snack cannot be edited, only added or cleared).
```

- [ ] **Step 3: Verify and commit**

Run: `make check`
Expected: PASS (API lint, backend vet/test/lint, generated-code drift, web typecheck, lint, tests).

```bash
git add web/CLAUDE.md docs/superpowers/specs/2026-09-25-web-app-design.md
git commit -m "docs: describe shopping, profile, the partner link and live lists, and record the decisions and API gaps in the web spec"
```

Finish with superpowers:finishing-a-development-branch. The PR title is "Web app Shopping and Profile: live lists, targets, partner link, custom ingredients"; its body links this plan, says it completes the four-plan web delivery of the spec's §10, and notes that the `e2e` CI job runs the new flows.

---

## After this plan

This is the last of the four web plans in the spec's §10. What the web app does not have, by design and recorded in the spec's out-of-scope list: hosting (with its single-instance and trusted-proxy requirements), server-component data fetching, offline support, i18n, personalised reference values, multi-instance deployment, and the iOS client. Worth an issue each when they bite: the two API gaps named in Task 11 (a `custom=true` filter on `GET /ingredients`, and an entry-addressed plan endpoint), demo seed data, and a shared refresh store before a second web instance. The next milestone is the iOS plan, which reuses this contract and these behaviours (optimistic check-off, version conflicts, event-stream rules) natively.

