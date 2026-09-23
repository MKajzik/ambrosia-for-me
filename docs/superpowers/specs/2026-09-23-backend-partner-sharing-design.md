# Backend: Partner and Sharing Design

Status: draft for review. Extends `2026-09-21-meal-planner-design.md` (§3.1, §3.6, §4.1, §4.3, §10). Where the two disagree on the partner domain, this document wins and the parent spec is updated in the same change.

## 1. Goal

A user links with exactly one partner through an invite code. The partner can read the owner's meals and diet templates that are flagged `shared_with_partner` and copy them into their own library. Shared shopping lists are editable at item level by both partners, with live SSE updates. Unlinking ends all access immediately.

This is the last backend domain in the build order. Meals, diet templates and shopping lists already store `shared_with_partner`, but every read and write checks `owner_id` only; this domain adds the `partnerships` table and the visibility rules, and closes the handoff items listed in `backend/CLAUDE.md` ("Not built yet").

## 2. Decisions

| Question | Decision |
|---|---|
| Partner meal that uses the partner's custom ingredients | The partner can read it (ingredients resolved with the **meal owner's** visibility). On copy, each custom ingredient is duplicated into the caller's library; global (USDA) ingredients stay shared references. |
| Invite code | 8 characters from `23456789ABCDEFGHJKMNPQRSTUVWXYZ` (no `0 O 1 I L`), 48 h expiry, stored hashed, shown once, one pending invite per user (a new invite replaces the old). |
| Partner powers on a shared list | Item add, edit, check, delete. List-level actions (rename, delete, toggle sharing, regenerate) stay owner-only; regenerate keeps reading the owner's own `plan_entries`. |
| Enforcement | A `Partners` resolver plus a shared visibility predicate passed into queries. No partnership knowledge in SQL. |
| Plans | `plan_entries` stay owner-only. A partner copies a template and applies their own copy. |

## 3. Data model

Migration `00008_partnerships.sql`.

`partnerships`: `id`, `user_a` (inviter), `user_b` (null until accepted), `status` (`pending` | `active`), `invite_code_hash`, `invite_expires_at`, `created_by`, `created_at`, `updated_at`. All user FKs `ON DELETE CASCADE`.

Constraints:

- `pending` requires `user_b IS NULL` and hash and expiry set.
- `active` requires `user_b IS NOT NULL`, `user_b <> user_a`, and the hash and expiry cleared, so a spent code cannot be replayed.
- Partial unique indexes: one active row per `user_a`, one active row per `user_b`, one pending row per `user_a`, unique `invite_code_hash`.
- A user appearing once across *both* columns of active rows cannot be expressed as an index. `accept` therefore locks both `users` rows in id order inside its transaction and re-checks before activating. Concurrent accepts by different users must yield exactly one winner (test required).

Unlinking hard-deletes the row (deletes are hard, parent spec §3). Re-linking is a fresh invite.

## 4. API

All under `/v1`, bearer-authenticated. `openapi.yaml` changes first, then `make generate`.

| Route | Behaviour |
|---|---|
| `POST partner/invite` | Returns `{code, expires_at}`. Replaces any pending invite. `409 partner_already_linked` if the caller is linked. |
| `POST partner/accept` `{code}` | Returns the partnership. Wrong, expired, own or used code all return the same `404 invite_invalid`. `409 partner_already_linked` if either side is linked. Rate limited per user with the existing limiter. |
| `GET partner` | Active: partner `display_name`, `linked_at`, `status`. Pending: `status` and `expires_at`, never the code. None: `404 partner_not_linked`. |
| `DELETE partner` | Ends an active link or cancels a pending invite; `204`. Either side may unlink. `404 partner_not_linked` if none. |
| `GET partner/meals` | The partner's `shared_with_partner` meals only; cursor pagination. `404 partner_not_linked` without an active partner. |
| `GET partner/diet-templates` | Same, for templates. |
| `GET partner/shopping-lists` | New (not in the parent spec). The partner's shared lists only. |

New problem codes: `partner_not_linked` (404), `partner_already_linked` (409), `invite_invalid` (404). `Meal`, `DietTemplate` and `ShoppingList` gain an `is_owner` boolean so clients can render partner resources read-only.

## 5. Visibility (service layer)

`Partners.ActivePartnerID(ctx, q, userID)` returns the active partner's id or none. Each service calls it once at the start of a transaction and passes the result to its queries. No caching, so an unlink is visible to the next transaction.

Read predicate: `owner_id = @user_id OR (owner_id = @partner_id AND shared_with_partner)`. With no partner, `@partner_id` is null and the second branch matches nothing.

- **Meals and diet templates.** `Get*` and `List*` use the read predicate. All writes (`Update`, `Delete`, `ReplaceIngredients`, `ReplaceSlots`, `Apply`) keep the strict owner-only queries, so a partner attempting them gets `404`. `GET partner/*` list queries use `owner_id = @partner_id AND shared_with_partner` only, never mixing in the caller's own items.
- **Ingredients on a shared meal.** Nutrition and ingredient names are resolved with the meal owner's id. Ingredient visibility rules do not change; partners cannot browse or search each other's ingredients.
- **Copy.** `Meals.Copy` accepts an owned or partner-shared meal. For a partner meal, each distinct custom ingredient (with its nutrient rows) is duplicated once into the caller's library, even when used on several lines. The copy always has `shared_with_partner = false`. `DietTemplates.Copy` applies the same rule to the template's meals, which it must now copy as well because slots reference the owner's meals.
- **Unseen resources** (nonexistent, unshared, or owned by a non-partner) return the same `404`.

## 6. Shopping lists and SSE

- **Reads and item edits** (`GetShoppingListForUser`, `TouchShoppingListForUser`, `GetShoppingItemForUserForUpdate`, `DeleteShoppingItemForUser`, `ListShoppingListsForUser`, and `Subscribe`) use the read predicate. A partner's shared lists appear in the caller's own `GET shopping-lists`.
- **`checked_by`** is the acting user's id when checking and cleared when unchecking.
- **Owner-only:** rename, delete, toggle `shared_with_partner`, regenerate (`404` for a partner). Partner-added items have `origin = manual`, so the owner's regenerate keeps them.
- **Unlink ordering.** Item edits resolve the partner with `SELECT ... FOR SHARE` on the partnership row. Unlink's `DELETE` waits for in-flight edits, and any edit starting afterwards finds no partner and gets `404`.
- **SSE hub.** Each subscription records the watching user and the list owner. New hub methods:
  - `CloseAccess(userA, userB)`: closes streams where one of them watches a list the other owns. Called on unlink.
  - `CloseListForNonOwners(listID)`: called when the owner turns sharing off.
  - `CloseUser(userID)`: closes all streams touching that user. Called on account deletion, which today deletes lists with a raw `DELETE` and publishes nothing.

  Closing is sufficient: clients reconnect, refetch, and get `404`. No new event type.
- **After unlink or deletion.** Lists stay with their owner, including items the partner added. If the partner's account is deleted, `checked_by` becomes null (`ON DELETE SET NULL`) and the list survives. Copies of meals and templates are independent.

## 7. Testing

Tests first, on real Postgres, following the existing flow-test layout.

- Invite lifecycle: expiry, replaced invite, own code, reused code, wrong code (all `invite_invalid`).
- Two-user accept race: exactly one winner.
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
