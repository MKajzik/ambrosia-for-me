# Backend: Partner and Sharing Design

Status: implemented by `docs/superpowers/plans/2026-09-24-backend-partner-sharing.md`. Extends `2026-09-21-meal-planner-design.md` (§3.1, §3.6, §4.1, §4.3, §10). Where the two disagree on the partner domain, this document wins and the parent spec is updated in the same change.

## 1. Goal

A user links with exactly one partner through an invite code. The partner can read the owner's meals and diet templates that are flagged `shared_with_partner` and copy them into their own library. Shared shopping lists are editable at item level by both partners, with live SSE updates. Unlinking ends all access immediately.

This is the last backend domain in the build order. Meals, diet templates and shopping lists already store `shared_with_partner`, but every read and write checks `owner_id` only; this domain adds the `partnerships` table and the visibility rules, and closes the handoff items listed in `backend/CLAUDE.md` ("Not built yet").

## 2. Decisions

| Question | Decision |
|---|---|
| Partner meal that uses the partner's custom ingredients | The partner can read it (ingredients resolved with the **meal owner's** visibility). On copy, each custom ingredient is duplicated into the caller's library; global (USDA) ingredients stay shared references. |
| Invite code | 8 characters from `23456789ABCDEFGHJKMNPQRSTUVWXYZ` (no `0 O 1 I L`), 48 h expiry, stored hashed, shown once, one pending invite per user (a new invite replaces the old). |
| Partner powers on a shared list | Item add, edit, check, delete. List-level actions (rename, delete, toggle sharing, regenerate) stay owner-only; regenerate keeps reading the owner's own `plan_entries`. |
| Enforcement | The package functions `activePartnerID` and `partnerOrNil` in `service/partners.go` resolve the partner, so `Meals`, `DietTemplates` and `ShoppingLists` need no new constructor argument. The result goes into the queries as one `partner_id` parameter of a shared read predicate. No other partnership knowledge in SQL. |
| Plans | `plan_entries` stay owner-only. A partner copies a template and applies their own copy. |
| Where partner resources are listed | Own `GET meals`, `GET diet-templates` and `GET shopping-lists` stay owner-only. The partner's shared resources come only from `GET partner/*`. `Get` by id, and shopping-list item edits and events, also accept a partner-shared resource (`is_owner` tells clients which). |
| Shared template and its meals | Sharing a template implicitly shares what its slots show: `meal_id` and meal name. The meals' own `shared_with_partner` flags are not consulted for that, and `GET meals/{id}` on an unshared meal still returns `404`. Copy reads the template's meals through the template, not through the meal visibility predicate. |
| Invite brute force | `partner/accept` has its own limit (per user and per IP), tighter than the general per-user limiter. |

## 3. Data model

Migration `00008_partnerships.sql`.

`partnerships`: `id`, `user_a` (inviter), `user_b` (null until accepted), `status` (`pending` | `active`), `invite_code_hash`, `invite_expires_at`, `created_by`, `created_at`, `updated_at`. All user FKs `ON DELETE CASCADE`.

Constraints:

- `user_b <> user_a` always (a null `user_b` passes the check).
- `pending` requires `user_b IS NULL` and hash and expiry set.
- `active` requires `user_b IS NOT NULL` and the hash and expiry cleared, so a spent code cannot be replayed.
- Partial unique indexes: one active row per `user_a`, one active row per `user_b`, one pending row per `user_a`, unique `invite_code_hash`.
- A user appearing once across *both* columns of active rows cannot be expressed as an index. `accept` therefore locks both `users` rows in id order inside its transaction and re-checks before activating. Concurrent accepts by different users must yield exactly one winner (test required). In the same transaction `accept` deletes the caller's own pending invite, so no stale code stays valid and `GET partner` never sees two rows for one user.

Unlinking hard-deletes the row (deletes are hard, parent spec §3). Re-linking is a fresh invite.

## 4. API

All under `/v1`, bearer-authenticated. `openapi.yaml` changes first, then `make generate`.

| Route | Behaviour |
|---|---|
| `POST partner/invite` | Returns `{code, expires_at}`. Replaces any pending invite. `409 partner_already_linked` if the caller is linked. |
| `POST partner/accept` `{code}` | Returns the partnership. Wrong, expired, own or used code all return the same `404 invite_invalid`. `409 partner_already_linked` if the caller is linked. Because a linked inviter's code is always cleared, a code that resolves to a linked inviter cannot occur and never leaks as `409`. Rate limited by a dedicated limiter, per user and per client IP (8 characters from 31 symbols is about 39 bits, so the general per-user limit is not enough against many accounts). |
| `GET partner` | One schema with four fields, the unused ones `null`: `status`, `display_name` (the partner's, when active), `linked_at` (when active), `expires_at` (when pending). Never the code. `404 partner_not_linked` when there is neither an active partner nor a live invite, so an expired pending invite is `404` too (`DELETE partner` still cancels it). |
| `DELETE partner` | Ends an active link or cancels a pending invite; `204`. Either side may unlink. `404 partner_not_linked` if none. |
| `GET partner/meals` | The partner's `shared_with_partner` meals only; cursor pagination. `404 partner_not_linked` without an active partner. |
| `GET partner/diet-templates` | Same, for templates. |
| `GET partner/shopping-lists` | New (not in the parent spec). The partner's shared lists only. |

New problem codes: `partner_not_linked` (404), `partner_already_linked` (409), `invite_invalid` (404). `Meal`, `DietTemplate` and `ShoppingList` gain an `is_owner` boolean so clients can render partner resources read-only.

## 5. Visibility (service layer)

`activePartnerID(ctx, q, userID, forShare)` returns the active partner's id, or `ErrPartnerNotLinked`. Each service calls it (through `partnerOrNil`, which turns "none" into a nil `partner_id`) once at the start of a transaction and passes the result to its queries. No caching, so an unlink is visible to the next transaction. Plain reads take no lock. Only shopping-item writes and the final access check of an SSE subscribe take `FOR SHARE` on the partnership row (see §6).

Read predicate: `owner_id = @user_id OR (owner_id = @partner_id AND shared_with_partner)`. With no partner, `@partner_id` is null and the second branch matches nothing.

- **Meals and diet templates.** Only `Get` (by id) uses the read predicate. `List` stays owner-only. All writes (`Update`, `Delete`, `ReplaceIngredients`, `ReplaceSlots`, `Apply`) keep the strict owner-only queries, so a partner attempting them gets `404`. `GET partner/*` list queries use `owner_id = @partner_id AND shared_with_partner` only.
- **Ingredients on a shared meal.** Nutrition and ingredient names are resolved with the meal owner's id. Ingredient visibility rules do not change; partners cannot browse or search each other's ingredients.
- **Slots of a shared template.** A partner reading a shared template sees each slot's `meal_id` and meal name even if that meal is not shared itself (§2). The meal's detail stays `404`.
- **Copy.** `Meals.Copy` accepts an owned or partner-shared meal. For a partner meal, each distinct custom ingredient (with its nutrient rows) is duplicated once into the caller's library, even when used on several lines. Repeated copies duplicate again; there is no cross-copy dedupe in v1. The copy always has `shared_with_partner = false`.
- **Template copy.** Copying an own template keeps today's behavior: the copy's slots reference the same meals. Copying a partner template copies each distinct meal once, even when it fills several slots, using the meal copy rule above, and points the new slots at those copies. Meals are read through the template, so unshared meals of a shared template copy fine.
- **Unseen resources** (nonexistent, unshared, or owned by a non-partner) return the same `404`.

## 6. Shopping lists and SSE

- **Reads and item edits** (`GetShoppingListForUser`, `TouchShoppingListForUser`, `GetShoppingItemForUserForUpdate`, `DeleteShoppingItemForUser`, and `Subscribe`) use the read predicate. `ListShoppingListsForUser` stays owner-only; a partner's shared lists come from `GET partner/shopping-lists`. `SetShoppingListSourceForUser` stays owner-only (regenerate).
- **`checked_by`** is the acting user's id when checking and cleared when unchecking.
- **Owner-only:** rename, delete, toggle `shared_with_partner`, regenerate (`404` for a partner). Partner-added items have `origin = manual`, so the owner's regenerate keeps them.
- **Unlink ordering.** Item edits resolve the partner with `SELECT ... FOR SHARE` on the partnership row. Unlink's `DELETE` waits for in-flight edits, and any edit starting afterwards finds no partner and gets `404`.
- **Subscribe ordering.** `Subscribe` looks the list up (which tells the hub who owns it), registers in the hub, then checks access again with the partnership row taken `FOR SHARE`, so the check waits for an unlink in flight. If the check fails, it unregisters and returns `404`. Without this order, a subscribe that passed its check just before an unlink commits could register after `CloseAccess` ran and stay open. Test required.
- **SSE hub.** Each subscription records the watching user and the list owner. New hub methods:
  - `CloseAccess(userA, userB)`: closes streams where one of them watches a list the other owns. Called on unlink.
  - `CloseListForNonOwners(listID)`: called when the owner turns sharing off.
  - `CloseUser(userID)`: closes all streams touching that user. Called on account deletion, which today deletes lists with a raw `DELETE` and publishes nothing.

  Closing is sufficient: clients reconnect, refetch, and get `404`. No new event type. The hub is in-process, so on a second API replica these closes would not reach streams held there; that stays with the existing cross-replica limitation (§9).
- **After unlink or deletion.** Lists stay with their owner, including items the partner added. If the partner's account is deleted, `checked_by` becomes null (`ON DELETE SET NULL`) and the list survives. Copies of meals and templates are independent.

## 7. Testing

Tests first, on real Postgres, following the existing flow-test layout.

- Invite lifecycle: expiry, replaced invite, own code, reused code, wrong code (all `invite_invalid`); accepting another user's code deletes the caller's own pending invite, so that old code then returns `invite_invalid`; the accept limiter trips.
- Two-user accept race: exactly one winner.
- Subscribe versus unlink race: a stream never survives an unlink.
- A shared template with an unshared meal: partner sees the slot, meal detail is `404`, copy succeeds; a meal in several slots is copied once.
- Visibility matrix (owner, active partner, unshared, unlinked, stranger) for meals, templates and lists.
- Copy: custom-ingredient deep copy, one ingredient used on several lines duplicated once, independence after the original changes or is deleted, template copy copies meals.
- Shopping lists: partner edits and `checked_by`, owner-only list actions return `404` for a partner, unlink-versus-edit ordering.
- SSE: streams close on unlink, on unshare, and on account deletion.
- Handoff items: `checked_by` set-null on partner deletion; a list survives its non-owner partner's account deletion.
- End-to-end HTTP flows with two users; the existing contract test keeps handlers and spec aligned.

## 8. Docs

Replace the "Not built yet" partner note in `backend/CLAUDE.md` with a description of the partner domain. Update parent spec §3.1 (invite format and expiry), §4.1 (`GET partner/shopping-lists`), §10 (close the invite-code item).

## 9. Out of scope

Sharing or applying plans, multiple partners or groups, notifications and email invites, cross-replica SSE (the in-process hub is unchanged), the web and iOS clients.

## 10. Delivery

Branch `feat/backend-partner-sharing`, small commits in this order: migration, queries, resolver, meals and templates, shopping lists, SSE hub, handlers and contract, docs. One PR.
