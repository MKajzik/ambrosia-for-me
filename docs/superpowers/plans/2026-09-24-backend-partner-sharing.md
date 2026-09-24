# Backend Partner and Sharing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A user links with exactly one partner through an invite code. The partner reads the owner's `shared_with_partner` meals and diet templates and copies them into their own library. Shared shopping lists are editable at item level by both partners, with live SSE updates. Unlinking ends all access at once. This is the last backend domain in the build order, and it closes the handoff items `backend/CLAUDE.md` lists under "Not built yet".

**Architecture:** A new `partnerships` table and a `Partners` service (invite, accept, get, unlink). The visibility rules stay in the service layer: `Meals`, `DietTemplates` and `ShoppingLists` resolve the caller's active partner once per transaction with the package functions `activePartnerID` and `partnerOrNil`, and pass it into their read queries as a single `partner_id` that feeds one shared read predicate. Every write stays owner-only. Copying a partner's meal or template duplicates what the copy needs into the caller's library (`ingredientCopier`). `ListEventHub` subscriptions learn who watches and who owns, so an unlink, an unshare or an account deletion can close exactly the streams that lost access. Two HTTP tasks add the routes, `is_owner`, and a tighter rate limit on `accept`.

**Tech Stack:** Go 1.26, chi, pgx/pgxpool, goose, sqlc, oapi-codegen, kin-openapi. No new dependencies and no new environment variables.

**Spec:** `docs/superpowers/specs/2026-09-23-backend-partner-sharing-design.md` (extends `docs/superpowers/specs/2026-09-21-meal-planner-design.md` sections 3.1, 3.6, 4.1, 4.3, 10). Read both before starting: where they disagree on the partner domain, the partner spec wins.

## Global Constraints

- OpenAPI 3.0.3 is the source of truth: edit `openapi.yaml` first, then `make generate`, then implement. Never hand-edit `backend/internal/api/api.gen.go` or `backend/internal/store/sqlc/`.
- No SQL outside `backend/internal/store`. Dependencies point one way: `httpapi` → `service` → `store`.
- Global `security: bearerAuth` protects every operation by default. No new route opts out.
- Unseen resources return `404`, never `403`: a nonexistent resource, an unshared one, and one owned by a non-partner are indistinguishable. A partner's attempt at a write to a partner's resource is `404` too.
- Errors are RFC 9457 `application/problem+json` with a stable `code`. New codes: `partner_not_linked` (404), `partner_already_linked` (409), `invite_invalid` (404).
- Migrations are goose SQL, `00008_partnerships.sql`, forward-only. The new table has `created_at`, `updated_at` and the `set_updated_at()` trigger, and every user foreign key is `ON DELETE CASCADE`.
- Invite code: 8 characters from `23456789ABCDEFGHJKMNPQRSTUVWXYZ` (no `0 O 1 I L`), 48 h expiry, stored hashed, shown once, one pending invite per user (a new invite replaces the old one).
- Partner access to meals and diet templates is read-only plus copy. Shopping lists are editable by both partners at item level (add, edit, check, delete). Rename, delete, toggling `shared_with_partner` and regenerate stay owner-only, and regenerate keeps reading the owner's own `plan_entries`. `plan_entries` stay owner-only: a partner copies a template and applies their own copy.
- The partner is resolved once at the start of a transaction and never cached, so an unlink is visible to the next transaction. Only `shared_with_partner` resources of the active partner are visible; the caller's own listings (`GET meals`, `GET diet-templates`, `GET shopping-lists`) never include the partner's.
- Tests first, on a real Postgres (no database mocks), following the existing layout: service tests in `backend/internal/service`, flow tests through `contract(...)` in `backend/internal/httpapi`.
- Keep commits small, one logical change each: one commit per task.
- Any new environment variable goes into `.env.example` in the same commit. This plan adds none (`RateLimits.AcceptPerHour` is a code default, like the other limits).

## Where this plan settles what the spec left open

- **The resolver is a pair of package functions, not a `Partners` method.** `activePartnerID(ctx, q, userID, forShare)` and `partnerOrNil` live in `service/partners.go`, so `Meals`, `DietTemplates` and `ShoppingLists` get no new constructor argument and no existing test changes for it. The spec's "Enforcement" row and section 5 say so.
- **`GET partner` is one flat schema**: `status`, `display_name`, `linked_at`, `expires_at`, the unused ones `null`. `linked_at` is the row's `updated_at`: the activation `UPDATE` sets it, and nothing else updates an active row. An expired pending invite reads as `404 partner_not_linked`; `DELETE partner` still cancels it (`204`).
- **`accept` looks at the caller before the code.** A caller who is already linked gets `409 partner_already_linked` whatever the code, so a linked user cannot use `accept` to test guesses. Because a linked user never has a pending invite (`accept` deletes it), a code that resolves to a linked inviter cannot occur.
- **`Meals.GetOwn`** is the owner-only read `Plan.GetRange` uses, so a plan range does not resolve a partner for every scheduled meal. `Plan.SetEntry` calls `GetMealForUser` without a partner, so a partner's shared meal cannot be scheduled directly (`400 invalid_meal`): copy it first.
- **Streams.** `ListEventHub.Subscribe` gains the watcher and the list owner. `ShoppingLists.Subscribe` looks the list up first (that tells it the owner), registers, then re-checks access with the partnership row `FOR SHARE`, so the check waits for an unlink in flight. `Auth.OnUserDeleted(fn)` is a setter, not a constructor argument, so `NewAuth`'s callers do not change; `cmd/api` registers `listEvents.CloseUser`.
- **The accept limiter** is `RateLimits.AcceptPerHour` (default 10), applied once per user and once per client IP, to `POST /v1/partner/accept` only.
- **Copy semantics.** An own copy is unchanged (same meals, same ingredients). A partner copy duplicates each distinct custom ingredient, with its nutrient rows, once per copy operation (a template copy shares one duplicate across all its meals), keeps global (USDA) ingredients as references, and copies each distinct meal of a template once. Copying the same partner meal twice duplicates its ingredients twice.

- **Task order.** The spec's delivery list runs migration, queries, resolver, meals and templates, shopping lists, hub, handlers, docs. The hub comes second here instead, because `Partners.Unlink` closes streams through it and each task must build and pass on its own.

## Review Focus

Inputs the spec implies but its main paths do not exercise, most likely to bite first. Each has a test in the task that owns the code:

1. **A partner tries to schedule the owner's shared meal into their own plan.** Refused as an unknown meal (`ErrPlanMealNotFound`, `400 invalid_meal`); their own copy works. Task 4, `TestMealsAPartnersSharedMealCannotBeScheduledInThePlanDirectly`.
2. **A double tap on "invite" sends two requests at once.** Both succeed, exactly one pending invite is left, and nobody gets a 500 from the unique index. Task 3, `TestPartnersConcurrentInvitesByOneUserLeaveExactlyOnePendingInvite`.
3. **A partner adds a shopping item on the owner's custom ingredient.** Refused as an unknown ingredient, revealing nothing about it. Task 6, `TestShoppingListsPartnerCannotAddAnItemOnTheOwnersCustomIngredient`.
4. **The owner deletes a shared list while the partner has it open.** The partner's stream ends with `list_deleted`. Task 6, `TestShoppingListsDeletingASharedListEndsThePartnersStream`.
5. **Paging through `GET partner/meals` with a small limit.** Every shared meal exactly once, in order, none unshared, none of the caller's own. Task 4, `TestMealsListPartnerPaginatesTheSharedMealsOnly`.

## How this plan was verified

Every code block in Tasks 1 to 9 is the output of `git show` or `git diff` on nine commits assembled in a throwaway copy of the repo, in exactly the state each task leaves. In that copy:

- For each task, the new tests were run against the previous commit first and failed as shown in Step 2 (Task 1 at run time, the others as compile errors), then passed after the task.
- `make generate` (sqlc v1.31.1, oapi-codegen v2.8.0) ran on every task that needs it and produced no diff afterwards; `make lint-api` (Redocly 2.53.3) reports the spec valid; `golangci-lint` v2.13.2 (the repo's CI command) reports 0 issues; `go vet ./...` is clean; `go test ./...` passes every package, all pre-existing tests included.
- The ordering and locking tests were shown to be load-bearing by removing what they guard: the users lock in `Accept` and in `Invite`, the re-check in `Accept`, `FOR SHARE` in `UpdateItem`, the final check in `Subscribe`, and the register-first order in `Subscribe` each make a named test fail. `TestPartnersCrossedAcceptsLinkTwoUsersOnlyOnce`, `TestPartnersConcurrentAcceptsOfOneCodeHaveExactlyOneWinner` and the `-race` runs are smoke tests that usually pass even without the locks.
- Docker was not available in the planning environment, so the tests ran against a local Postgres 17.11 through a scratch-only change to `internal/testutil` that is not part of this plan. Run them the usual way (`make test-backend`, which needs Docker) before you trust the result, and run `make check` at the end.

## File Structure

- `backend/migrations/00008_partnerships.sql`: the `partnerships` table (Task 1).
- `backend/internal/db/schema_test.go`: `TestPartnershipsSchemaEnforcesItsConstraints`.
- `backend/internal/service/shopping_events.go`: `ListSubscription` gains `watcher` and `owner`; `Subscribe` takes them; `CloseAccess`, `CloseListForNonOwners`, `CloseUser` (Task 2).
- `backend/internal/store/queries/partnerships.sql`: the partnership queries (Task 3).
- `backend/internal/service/partners.go` / `partners_test.go` / `partners_edges_test.go`: `Partners`, the resolver functions, and their tests (Tasks 3, 4, 6).
- `backend/internal/service/copy.go`: `ingredientCopier` and `copyMeal`, shared by meal and template copy (Task 4).
- `backend/internal/store/queries/meals.sql`, `diet_templates.sql`, `shopping_lists.sql`: the read predicate and `shared_only` (Tasks 4 to 6).
- `backend/internal/service/meals.go`, `diet_templates.go`, `shopping_lists.go`, `plan.go`, `auth.go`: partner visibility, copy, listings, `GetOwn`, `OnUserDeleted`.
- `openapi.yaml`: tag `Partner`, six paths, three schemas, `is_owner` on three schemas (Tasks 8, 9).
- `backend/internal/httpapi/partner.go`: the four link handlers. `meals.go`, `diet_templates.go`, `shopping_lists.go`: list helpers and `is_owner`. `problem.go`, `account.go`, `server.go`, `router.go`, `ratelimit.go`: codes, wiring, limiter.
- `backend/cmd/api/main.go`: builds `Partners` and registers `accounts.OnUserDeleted(listEvents.CloseUser)`.
- `backend/CLAUDE.md`, `docs/superpowers/specs/2026-09-21-meal-planner-design.md`, `docs/superpowers/specs/2026-09-23-backend-partner-sharing-design.md`: docs (Task 10).

---

### Task 0: Bring the branch up to date

The branch was cut before the markdown files were compressed on `master`, and Task 10 edits `backend/CLAUDE.md`. Merge `master` first so those edits apply to the current wording. The branch only adds files under `docs/superpowers/`, so the merge is clean.

- [ ] **Step 1: Merge master**

Run: `git merge master`

Expected: a clean merge (`AGENTS.md`, `CLAUDE.md` and `backend/CLAUDE.md` change; nothing conflicts).

- [ ] **Step 2: Check the baseline**

Run: `make check-generated && cd backend && go build ./... && go vet ./...`

Expected: no output and exit 0.


### Task 1: Migration: the `partnerships` table

**Files:**
- Create: `backend/migrations/00008_partnerships.sql`
- Modify: `backend/internal/db/schema_test.go`

**Interfaces:**
- Produces: the `partnerships` table (`id, user_a, user_b, status, invite_code_hash, invite_expires_at, created_by, created_at, updated_at`). Constraint and index names later tasks translate: `partnerships_active_user_a_idx`, `partnerships_active_user_b_idx`, `partnerships_pending_user_a_idx`, `partnerships_invite_code_hash_idx` (unique indexes) and the default foreign-key names `partnerships_user_a_fkey`, `partnerships_user_b_fkey`, `partnerships_created_by_fkey`, all `ON DELETE CASCADE`.

The schema enforces what it can: a pending row has no `user_b` and carries a code hash and an expiry; an active row has a `user_b` and neither hash nor expiry (so a spent code cannot be replayed); nobody is their own partner; each user has at most one active row per column and one pending invite; a code hash is unique. What an index cannot say, a user being `user_a` of one active row and `user_b` of another, is `Partners.Accept`'s job (Task 3).

- [ ] **Step 1: Write the failing tests**

Modify `backend/internal/db/schema_test.go` (unified diff against the current file):

```diff
--- a/backend/internal/db/schema_test.go
+++ b/backend/internal/db/schema_test.go
@@ -443,3 +443,81 @@ func TestShoppingListsSchemaEnforcesItsConstraints(t *testing.T) {
 		t.Errorf("shopping_items rows after deleting the list = %d (err %v), want 0", n, err)
 	}
 }
+
+func TestPartnershipsSchemaEnforcesItsConstraints(t *testing.T) {
+	ctx := context.Background()
+	conn := migratedConn(t)
+
+	newUser := func(email string) string {
+		t.Helper()
+		var id string
+		if err := conn.QueryRow(ctx,
+			`INSERT INTO users (email, password_hash, display_name) VALUES ($1, 'h', 'U') RETURNING id`, email,
+		).Scan(&id); err != nil {
+			t.Fatalf("insert user %s: %v", email, err)
+		}
+		return id
+	}
+	alice, bob, carol, dave := newUser("alice@example.com"), newUser("bob@example.com"), newUser("carol@example.com"), newUser("dave@example.com")
+
+	insertPending := func(user, hash string) error {
+		_, err := conn.Exec(ctx,
+			`INSERT INTO partnerships (user_a, status, invite_code_hash, invite_expires_at, created_by)
+			 VALUES ($1, 'pending', $2::bytea, now() + interval '48 hours', $1)`, user, hash)
+		return err
+	}
+	insertActive := func(a, b string) error {
+		_, err := conn.Exec(ctx,
+			`INSERT INTO partnerships (user_a, user_b, status, created_by) VALUES ($1, $2, 'active', $1)`, a, b)
+		return err
+	}
+
+	if err := insertPending(alice, "hash-alice"); err != nil {
+		t.Fatalf("valid pending invite: %v", err)
+	}
+	if err := insertPending(alice, "hash-alice-2"); err == nil {
+		t.Error("a second pending invite for one user was accepted, want a unique violation")
+	}
+	if err := insertPending(bob, "hash-alice"); err == nil {
+		t.Error("a reused invite_code_hash was accepted, want a unique violation")
+	}
+
+	for name, stmt := range map[string]string{
+		"pending with a user_b": `INSERT INTO partnerships (user_a, user_b, status, invite_code_hash, invite_expires_at, created_by)
+			VALUES ('` + carol + `', '` + dave + `', 'pending', 'x'::bytea, now(), '` + carol + `')`,
+		"pending without a code hash": `INSERT INTO partnerships (user_a, status, invite_expires_at, created_by)
+			VALUES ('` + carol + `', 'pending', now(), '` + carol + `')`,
+		"pending without an expiry": `INSERT INTO partnerships (user_a, status, invite_code_hash, created_by)
+			VALUES ('` + carol + `', 'pending', 'y'::bytea, '` + carol + `')`,
+		"active without a user_b": `INSERT INTO partnerships (user_a, status, created_by)
+			VALUES ('` + carol + `', 'active', '` + carol + `')`,
+		"active that keeps its code hash": `INSERT INTO partnerships (user_a, user_b, status, invite_code_hash, invite_expires_at, created_by)
+			VALUES ('` + carol + `', '` + dave + `', 'active', 'z'::bytea, now(), '` + carol + `')`,
+		"a user linked to themselves": `INSERT INTO partnerships (user_a, user_b, status, created_by)
+			VALUES ('` + carol + `', '` + carol + `', 'active', '` + carol + `')`,
+		"an unknown status": `INSERT INTO partnerships (user_a, user_b, status, created_by)
+			VALUES ('` + carol + `', '` + dave + `', 'blocked', '` + carol + `')`,
+	} {
+		if _, err := conn.Exec(ctx, stmt); err == nil {
+			t.Errorf("%s was accepted, want a constraint violation", name)
+		}
+	}
+
+	if err := insertActive(carol, dave); err != nil {
+		t.Fatalf("valid active partnership: %v", err)
+	}
+	if err := insertActive(carol, alice); err == nil {
+		t.Error("a second active partnership for user_a was accepted, want a unique violation")
+	}
+	if err := insertActive(bob, dave); err == nil {
+		t.Error("a second active partnership for user_b was accepted, want a unique violation")
+	}
+	// Deleting a user deletes every partnership row they are in.
+	if _, err := conn.Exec(ctx, `DELETE FROM users WHERE id = $1`, carol); err != nil {
+		t.Fatalf("delete user: %v", err)
+	}
+	var n int
+	if err := conn.QueryRow(ctx, `SELECT count(*) FROM partnerships WHERE user_a = $1 OR user_b = $1 OR created_by = $1`, carol).Scan(&n); err != nil || n != 0 {
+		t.Errorf("partnerships left after deleting one of the users = %d (err %v), want 0", n, err)
+	}
+}
```

- [ ] **Step 2: Run them to see them fail**

Run: `cd backend && go test ./internal/db -count=1`

Expected: FAIL, for the right reason:

```
--- FAIL: TestPartnershipsSchemaEnforcesItsConstraints
    schema_test.go:476: valid pending invite: ERROR: relation "partnerships" does not exist (SQLSTATE 42P01)
```

- [ ] **Step 3: Implement**

Create `backend/migrations/00008_partnerships.sql`:

```sql
-- +goose Up
CREATE TABLE partnerships (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_a            uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    user_b            uuid REFERENCES users (id) ON DELETE CASCADE,
    status            text NOT NULL CHECK (status IN ('pending', 'active')),
    invite_code_hash  bytea,
    invite_expires_at timestamptz,
    created_by        uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    CHECK (user_b IS NULL OR user_b <> user_a),
    CHECK (status <> 'pending' OR (user_b IS NULL AND invite_code_hash IS NOT NULL AND invite_expires_at IS NOT NULL)),
    CHECK (status <> 'active' OR (user_b IS NOT NULL AND invite_code_hash IS NULL AND invite_expires_at IS NULL))
);

-- One active partnership per user, per column. A user who is user_a in one
-- active row and user_b in another cannot be expressed as an index:
-- Partners.Accept locks both users rows and re-checks before it activates.
CREATE UNIQUE INDEX partnerships_active_user_a_idx ON partnerships (user_a) WHERE status = 'active';
CREATE UNIQUE INDEX partnerships_active_user_b_idx ON partnerships (user_b) WHERE status = 'active';
CREATE UNIQUE INDEX partnerships_pending_user_a_idx ON partnerships (user_a) WHERE status = 'pending';
CREATE UNIQUE INDEX partnerships_invite_code_hash_idx ON partnerships (invite_code_hash) WHERE invite_code_hash IS NOT NULL;

CREATE TRIGGER partnerships_set_updated_at
    BEFORE UPDATE ON partnerships
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TABLE partnerships;
```

- [ ] **Step 4: Run the tests and see them pass**

Run: `cd backend && go test ./internal/db -count=1`

Expected: PASS (`ok  	github.com/InzKazik/mealplanner/backend/internal/db`).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/db/schema_test.go backend/migrations/00008_partnerships.sql
git commit -m "feat(backend): add the partnerships table"
```

### Task 2: Event hub: who watches, and closing streams

**Files:**
- Modify: `backend/internal/service/shopping_events.go`
- Modify: `backend/internal/service/shopping_events_test.go`
- Modify: `backend/internal/service/shopping_lists.go` (one line: the existing `Subscribe` call keeps compiling)

**Interfaces:**
- Consumes: `ListEventHub`, `ListSubscription`, `ErrEventStreamsClosed` (already in `shopping_events.go`).
- Produces: `(*ListEventHub).Subscribe(listID, watcherID, ownerID uuid.UUID) (*ListSubscription, error)` (was `Subscribe(listID)`), `(*ListEventHub).CloseAccess(userA, userB uuid.UUID)`, `(*ListEventHub).CloseListForNonOwners(listID uuid.UUID)`, `(*ListEventHub).CloseUser(userID uuid.UUID)`. A subscription remembers its watcher and the list's owner; the close methods match on those.

This task is pure in-memory code with no database. Task 6 changes `ShoppingLists.Subscribe` to pass the real owner; until then it passes the caller for both ids (the caller is the owner while lists are owner-only). The seven existing `hub.Subscribe(x)` calls in `shopping_events_test.go` become `hub.Subscribe(x, uuid.Nil, uuid.Nil)`: an anonymous subscription that no close method ever matches.

- [ ] **Step 1: Write the failing tests**

Modify `backend/internal/service/shopping_events_test.go` (unified diff against the current file):

```diff
--- a/backend/internal/service/shopping_events_test.go
+++ b/backend/internal/service/shopping_events_test.go
@@ -20,12 +20,12 @@ func itemEvent(listID uuid.UUID, version int) service.ListEvent {
 func TestListEventHubDeliversOnlyToSubscribersOfThatList(t *testing.T) {
 	hub := service.NewListEventHub()
 	listA, listB := uuid.New(), uuid.New()
-	subA, err := hub.Subscribe(listA)
+	subA, err := hub.Subscribe(listA, uuid.Nil, uuid.Nil)
 	if err != nil {
 		t.Fatalf("Subscribe A: %v", err)
 	}
 	defer subA.Close()
-	subB, err := hub.Subscribe(listB)
+	subB, err := hub.Subscribe(listB, uuid.Nil, uuid.Nil)
 	if err != nil {
 		t.Fatalf("Subscribe B: %v", err)
 	}
@@ -51,7 +51,7 @@ func TestListEventHubDeliversOnlyToSubscribersOfThatList(t *testing.T) {
 func TestListEventHubDisconnectsASubscriberThatFallsBehind(t *testing.T) {
 	hub := service.NewListEventHub()
 	list := uuid.New()
-	slow, err := hub.Subscribe(list)
+	slow, err := hub.Subscribe(list, uuid.Nil, uuid.Nil)
 	if err != nil {
 		t.Fatalf("Subscribe: %v", err)
 	}
@@ -77,7 +77,7 @@ func TestListEventHubDisconnectsASubscriberThatFallsBehind(t *testing.T) {
 func TestListEventHubEndsStreamsAfterListDeleted(t *testing.T) {
 	hub := service.NewListEventHub()
 	list := uuid.New()
-	sub, err := hub.Subscribe(list)
+	sub, err := hub.Subscribe(list, uuid.Nil, uuid.Nil)
 	if err != nil {
 		t.Fatalf("Subscribe: %v", err)
 	}
@@ -96,7 +96,7 @@ func TestListEventHubEndsStreamsAfterListDeleted(t *testing.T) {
 
 func TestListEventHubCloseEndsEveryStreamAndRefusesNewOnes(t *testing.T) {
 	hub := service.NewListEventHub()
-	sub, err := hub.Subscribe(uuid.New())
+	sub, err := hub.Subscribe(uuid.New(), uuid.Nil, uuid.Nil)
 	if err != nil {
 		t.Fatalf("Subscribe: %v", err)
 	}
@@ -107,7 +107,7 @@ func TestListEventHubCloseEndsEveryStreamAndRefusesNewOnes(t *testing.T) {
 		t.Error("channel still open after Close, want closed")
 	}
 	sub.Close() // must not panic on an already-closed subscription
-	if _, err := hub.Subscribe(uuid.New()); !errors.Is(err, service.ErrEventStreamsClosed) {
+	if _, err := hub.Subscribe(uuid.New(), uuid.Nil, uuid.Nil); !errors.Is(err, service.ErrEventStreamsClosed) {
 		t.Errorf("Subscribe after Close: err = %v, want ErrEventStreamsClosed", err)
 	}
 }
@@ -166,7 +166,7 @@ func TestListEventHubConcurrentPublishSubscribeClose(t *testing.T) {
 		go func(s int) {
 			defer wg.Done()
 			list := lists[s%numLists]
-			sub, err := hub.Subscribe(list)
+			sub, err := hub.Subscribe(list, uuid.Nil, uuid.Nil)
 			if err != nil {
 				// The hub is never closed in this test, so this must never happen.
 				atomic.AddInt64(&subFailures, 1)
@@ -210,3 +210,117 @@ func TestListEventHubConcurrentPublishSubscribeClose(t *testing.T) {
 		}
 	}
 }
+
+// closedWithin reports whether sub's channel is closed (after draining any
+// buffered events) within a moment.
+func closedWithin(sub *service.ListSubscription) bool {
+	deadline := time.After(time.Second)
+	for {
+		select {
+		case _, open := <-sub.Events():
+			if !open {
+				return true
+			}
+		case <-deadline:
+			return false
+		}
+	}
+}
+
+// stillOpen reports whether sub's channel is open with nothing pending.
+func stillOpen(sub *service.ListSubscription) bool {
+	select {
+	case _, open := <-sub.Events():
+		return open
+	default:
+		return true
+	}
+}
+
+func TestListEventHubCloseAccessEndsOnlyTheStreamsBetweenTwoUsers(t *testing.T) {
+	hub := service.NewListEventHub()
+	alice, bob, carol := uuid.New(), uuid.New(), uuid.New()
+	aliceList, bobList, carolList := uuid.New(), uuid.New(), uuid.New()
+	subscribe := func(list, watcher, owner uuid.UUID) *service.ListSubscription {
+		t.Helper()
+		sub, err := hub.Subscribe(list, watcher, owner)
+		if err != nil {
+			t.Fatalf("Subscribe: %v", err)
+		}
+		t.Cleanup(sub.Close)
+		return sub
+	}
+	bobWatchesAlice := subscribe(aliceList, bob, alice)
+	aliceWatchesBob := subscribe(bobList, alice, bob)
+	aliceWatchesOwn := subscribe(aliceList, alice, alice)
+	carolWatchesAlice := subscribe(aliceList, carol, alice)
+	aliceWatchesCarol := subscribe(carolList, alice, carol)
+
+	hub.CloseAccess(alice, bob)
+
+	if !closedWithin(bobWatchesAlice) || !closedWithin(aliceWatchesBob) {
+		t.Error("a stream between the two users stayed open, want it closed both ways")
+	}
+	for name, sub := range map[string]*service.ListSubscription{
+		"the owner's own stream":              aliceWatchesOwn,
+		"a third user watching one of them":   carolWatchesAlice,
+		"one of them watching a third user's": aliceWatchesCarol,
+	} {
+		if !stillOpen(sub) {
+			t.Errorf("%s was closed, want it left open", name)
+		}
+	}
+}
+
+func TestListEventHubCloseListForNonOwnersKeepsTheOwnersStreams(t *testing.T) {
+	hub := service.NewListEventHub()
+	owner, partner := uuid.New(), uuid.New()
+	list, otherList := uuid.New(), uuid.New()
+	ownerSub, _ := hub.Subscribe(list, owner, owner)
+	partnerSub, _ := hub.Subscribe(list, partner, owner)
+	elsewhere, _ := hub.Subscribe(otherList, partner, owner)
+	defer ownerSub.Close()
+	defer partnerSub.Close()
+	defer elsewhere.Close()
+
+	hub.CloseListForNonOwners(list)
+
+	if !closedWithin(partnerSub) {
+		t.Error("the partner's stream stayed open, want it closed")
+	}
+	if !stillOpen(ownerSub) {
+		t.Error("the owner's stream was closed, want it left open")
+	}
+	if !stillOpen(elsewhere) {
+		t.Error("a stream on another list was closed, want it left open")
+	}
+}
+
+func TestListEventHubCloseUserEndsEveryStreamTouchingThem(t *testing.T) {
+	hub := service.NewListEventHub()
+	gone, other, third := uuid.New(), uuid.New(), uuid.New()
+	goneList, otherList := uuid.New(), uuid.New()
+	watching, _ := hub.Subscribe(otherList, gone, other)
+	ownersOwn, _ := hub.Subscribe(goneList, gone, gone)
+	watchedByOther, _ := hub.Subscribe(goneList, other, gone)
+	unrelated, _ := hub.Subscribe(otherList, third, other)
+	defer watching.Close()
+	defer ownersOwn.Close()
+	defer watchedByOther.Close()
+	defer unrelated.Close()
+
+	hub.CloseUser(gone)
+
+	for name, sub := range map[string]*service.ListSubscription{
+		"a stream the user watched":       watching,
+		"the user's own stream":           ownersOwn,
+		"a stream on the user's own list": watchedByOther,
+	} {
+		if !closedWithin(sub) {
+			t.Errorf("%s stayed open, want it closed", name)
+		}
+	}
+	if !stillOpen(unrelated) {
+		t.Error("an unrelated stream was closed, want it left open")
+	}
+}
```

- [ ] **Step 2: Run them to see them fail**

Run: `cd backend && go test ./internal/service/ -run 'ListEventHub' -count=1`

Expected: FAIL, for the right reason:

```
internal/service/shopping_events_test.go:23:36: too many arguments in call to hub.Subscribe
	have (uuid.UUID, uuid.UUID, uuid.UUID)
	want (uuid.UUID)
```

- [ ] **Step 3: Implement**

Modify `backend/internal/service/shopping_events.go` (unified diff against the current file):

```diff
--- a/backend/internal/service/shopping_events.go
+++ b/backend/internal/service/shopping_events.go
@@ -61,11 +61,15 @@ func NewListEventHub() *ListEventHub {
 	return &ListEventHub{subs: make(map[uuid.UUID]map[*ListSubscription]struct{})}
 }
 
-// ListSubscription is one open event stream for one list.
+// ListSubscription is one open event stream for one list. It remembers who
+// watches it and who owns the list, so the hub can end the streams of a
+// partner whose access ended (see CloseAccess).
 type ListSubscription struct {
-	hub    *ListEventHub
-	listID uuid.UUID
-	events chan ListEvent
+	hub     *ListEventHub
+	listID  uuid.UUID
+	watcher uuid.UUID
+	owner   uuid.UUID
+	events  chan ListEvent
 }
 
 // Events delivers the list's events in publish order. It is closed when the
@@ -81,15 +85,19 @@ func (s *ListSubscription) Close() {
 	s.hub.removeLocked(s)
 }
 
-// Subscribe opens a subscription to listID's events. It does not check who
-// may see the list: callers go through ShoppingLists.Subscribe, which does.
-func (h *ListEventHub) Subscribe(listID uuid.UUID) (*ListSubscription, error) {
+// Subscribe opens a subscription to listID's events for watcherID, the user
+// reading the stream, on a list owned by ownerID. It does not check who may
+// see the list: callers go through ShoppingLists.Subscribe, which does.
+func (h *ListEventHub) Subscribe(listID, watcherID, ownerID uuid.UUID) (*ListSubscription, error) {
 	h.mu.Lock()
 	defer h.mu.Unlock()
 	if h.closed {
 		return nil, ErrEventStreamsClosed
 	}
-	sub := &ListSubscription{hub: h, listID: listID, events: make(chan ListEvent, listSubscriptionBuffer)}
+	sub := &ListSubscription{
+		hub: h, listID: listID, watcher: watcherID, owner: ownerID,
+		events: make(chan ListEvent, listSubscriptionBuffer),
+	}
 	if h.subs[listID] == nil {
 		h.subs[listID] = make(map[*ListSubscription]struct{})
 	}
@@ -141,6 +149,49 @@ func (h *ListEventHub) Close() {
 	}
 }
 
+// CloseAccess disconnects every stream where userA watches a list userB owns,
+// or userB watches a list userA owns. Partners.Unlink calls it after the
+// partnership row is deleted, so a partner's open streams end instead of
+// outliving their access. Clients reconnect, refetch and get 404.
+func (h *ListEventHub) CloseAccess(userA, userB uuid.UUID) {
+	h.closeMatching(func(s *ListSubscription) bool {
+		return (s.watcher == userA && s.owner == userB) || (s.watcher == userB && s.owner == userA)
+	})
+}
+
+// CloseListForNonOwners disconnects every stream of listID whose watcher is
+// not the list's owner. Called when the owner turns sharing off.
+func (h *ListEventHub) CloseListForNonOwners(listID uuid.UUID) {
+	h.mu.Lock()
+	defer h.mu.Unlock()
+	for sub := range h.subs[listID] {
+		if sub.watcher != sub.owner {
+			h.removeLocked(sub)
+		}
+	}
+}
+
+// CloseUser disconnects every stream that touches userID: one they watch, or
+// one on a list they own. Called on account deletion, which removes the
+// user's lists with a raw DELETE and so publishes nothing.
+func (h *ListEventHub) CloseUser(userID uuid.UUID) {
+	h.closeMatching(func(s *ListSubscription) bool {
+		return s.watcher == userID || s.owner == userID
+	})
+}
+
+func (h *ListEventHub) closeMatching(match func(*ListSubscription) bool) {
+	h.mu.Lock()
+	defer h.mu.Unlock()
+	for _, set := range h.subs {
+		for sub := range set {
+			if match(sub) {
+				h.removeLocked(sub)
+			}
+		}
+	}
+}
+
 // removeLocked unsubscribes sub and closes its channel, at most once. The
 // caller holds h.mu; every send and close happens under it, so a send can
 // never hit a closed channel.
```

Modify `backend/internal/service/shopping_lists.go` (unified diff against the current file):

```diff
--- a/backend/internal/service/shopping_lists.go
+++ b/backend/internal/service/shopping_lists.go
@@ -494,7 +494,7 @@ func (s *ShoppingLists) DeleteItem(ctx context.Context, ownerID, listID, itemID
 // subscription is already registered when the list_deleted event is
 // published.
 func (s *ShoppingLists) Subscribe(ctx context.Context, ownerID, listID uuid.UUID) (*ListSubscription, error) {
-	sub, err := s.events.Subscribe(listID)
+	sub, err := s.events.Subscribe(listID, ownerID, ownerID)
 	if err != nil {
 		return nil, err
 	}
```

- [ ] **Step 4: Run the tests and see them pass**

Run: `cd backend && go test ./internal/service/ -run 'ListEventHub' -count=1`

Expected: PASS (`ok  	github.com/InzKazik/mealplanner/backend/internal/service`).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/service/shopping_events.go backend/internal/service/shopping_events_test.go backend/internal/service/shopping_lists.go
git commit -m "feat(backend): let the event hub close streams by user"
```

### Task 3: Partners: invite, accept, get, unlink

**Files:**
- Create: `backend/internal/store/queries/partnerships.sql`
- Create: `backend/internal/service/partners.go`
- Create: `backend/internal/service/partners_test.go`
- Create: `backend/internal/service/partners_edges_test.go`
- Generated by `make generate`: `backend/internal/store/sqlc/partnerships.sql.go`, `backend/internal/store/sqlc/models.go`, `backend/internal/store/sqlc/querier.go` if present

**Interfaces:**
- Consumes: the `partnerships` table (Task 1); `ListEventHub.CloseAccess` (Task 2); `store.IsNotFound`, `IsUniqueViolation`, `IsForeignKeyViolation`; `service.ErrNotFound`.
- Produces: `service.NewPartners(st *store.Store, events *ListEventHub, now func() time.Time) *Partners` with `Invite(ctx, callerID) (Invite, error)`, `Accept(ctx, callerID, code string) (Partnership, error)`, `Get(ctx, callerID) (Partnership, error)`, `Unlink(ctx, callerID) error`; types `Invite{Code string; ExpiresAt time.Time}` and `Partnership{Status string; DisplayName *string; LinkedAt, ExpiresAt *time.Time}`; errors `ErrPartnerNotLinked`, `ErrPartnerAlreadyLinked`, `ErrInviteInvalid`; unexported `activePartnerID(ctx, q *sqlc.Queries, userID uuid.UUID, forShare bool) (uuid.UUID, error)` (returns `ErrPartnerNotLinked` for none), `requireUnlinked`, `lockUsers`; sqlc `GetActivePartnerID`, `GetActivePartnerIDForShare`, `LockUsersForUpdate`, `GetPartnershipForUser`, `GetPendingPartnershipByCodeHash`, `DeletePendingPartnershipForUser`, `CreatePartnerInvite`, `ActivatePartnership`, `DeletePartnershipsForUser`. Test helpers (package `service_test`): `newPartnersFixture`, `newNamedUser`, `fakeClock`.

`Invite` and `Accept` are where the races live, so both lock the involved `users` rows (`FOR NO KEY UPDATE`, in id order, so two transactions cannot deadlock and inserts of dependent rows are not blocked) and check again under the lock. Four tests are the load-bearing ones and were each shown to fail when the code they guard is removed: `TestPartnersAcceptWaitsForTheUserLocksAndChecksAgainAfterThem` (no lock, or no re-check) and `TestPartnersConcurrentInvitesByOneUserLeaveExactlyOnePendingInvite` (no lock in `Invite`). `TestPartnersCrossedAcceptsLinkTwoUsersOnlyOnce` and `TestPartnersConcurrentAcceptsOfOneCodeHaveExactlyOneWinner` are smoke tests: they usually pass even without the locks, because the window is small.

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/service/partners_test.go`:

```go
package service_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/InzKazik/mealplanner/backend/internal/service"
	"github.com/InzKazik/mealplanner/backend/internal/store"
	"github.com/InzKazik/mealplanner/backend/internal/store/sqlc"
)

// partnersFixture is a Partners service over a real database with a clock the
// test can move.
type partnersFixture struct {
	partners *service.Partners
	events   *service.ListEventHub
	st       *store.Store
	clock    *fakeClock
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newPartnersFixture(t *testing.T) partnersFixture {
	t.Helper()
	_, st := newIngredientsFixture(t)
	clock := &fakeClock{t: time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)}
	events := service.NewListEventHub()
	return partnersFixture{partners: service.NewPartners(st, events, clock.Now), events: events, st: st, clock: clock}
}

// newNamedUser creates a user with its own display name, for tests that read
// the partner's name back.
func newNamedUser(t *testing.T, st *store.Store, email, displayName string) uuid.UUID {
	t.Helper()
	hash := "hash"
	u, err := st.CreateUser(context.Background(), sqlc.CreateUserParams{Email: email, PasswordHash: &hash, DisplayName: displayName})
	if err != nil {
		t.Fatalf("create user %s: %v", email, err)
	}
	return u.ID
}

// link makes a and b partners through the real invite flow.
func (f partnersFixture) link(t *testing.T, a, b uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	inv, err := f.partners.Invite(ctx, a)
	if err != nil {
		t.Fatalf("Invite: %v", err)
	}
	if _, err := f.partners.Accept(ctx, b, inv.Code); err != nil {
		t.Fatalf("Accept: %v", err)
	}
}

func TestPartnersInviteAndAcceptLinkTwoUsers(t *testing.T) {
	f := newPartnersFixture(t)
	ctx := context.Background()
	alice := newNamedUser(t, f.st, "alice1@example.com", "Alice")
	bob := newNamedUser(t, f.st, "bob1@example.com", "Bob")

	inv, err := f.partners.Invite(ctx, alice)
	if err != nil {
		t.Fatalf("Invite: %v", err)
	}
	if len(inv.Code) != 8 || strings.Trim(inv.Code, "23456789ABCDEFGHJKMNPQRSTUVWXYZ") != "" {
		t.Errorf("code %q, want 8 characters from the unambiguous alphabet", inv.Code)
	}
	if want := f.clock.Now().Add(48 * time.Hour); !inv.ExpiresAt.Equal(want) {
		t.Errorf("ExpiresAt = %v, want %v", inv.ExpiresAt, want)
	}

	pending, err := f.partners.Get(ctx, alice)
	if err != nil {
		t.Fatalf("Get while pending: %v", err)
	}
	if pending.Status != "pending" || pending.ExpiresAt == nil || pending.DisplayName != nil || pending.LinkedAt != nil {
		t.Errorf("pending partnership = %+v, want status pending with only ExpiresAt set", pending)
	}

	// People type codes in lower case, with a space or a dash in the middle.
	typed := strings.ToLower(inv.Code[:4]) + " - " + inv.Code[4:]
	accepted, err := f.partners.Accept(ctx, bob, typed)
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if accepted.Status != "active" || accepted.DisplayName == nil || *accepted.DisplayName != "Alice" || accepted.LinkedAt == nil {
		t.Errorf("accepted = %+v, want an active partnership with Alice", accepted)
	}

	forAlice, err := f.partners.Get(ctx, alice)
	if err != nil {
		t.Fatalf("Get for the inviter: %v", err)
	}
	if forAlice.Status != "active" || forAlice.DisplayName == nil || *forAlice.DisplayName != "Bob" {
		t.Errorf("the inviter sees %+v, want an active partnership with Bob", forAlice)
	}
}

func TestPartnersInviteRules(t *testing.T) {
	f := newPartnersFixture(t)
	ctx := context.Background()
	alice := newNamedUser(t, f.st, "alice2@example.com", "Alice")
	bob := newNamedUser(t, f.st, "bob2@example.com", "Bob")
	carol := newNamedUser(t, f.st, "carol2@example.com", "Carol")

	first, err := f.partners.Invite(ctx, alice)
	if err != nil {
		t.Fatalf("Invite: %v", err)
	}
	second, err := f.partners.Invite(ctx, alice)
	if err != nil {
		t.Fatalf("second Invite: %v", err)
	}
	if first.Code == second.Code {
		t.Fatal("a new invite returned the old code")
	}
	if _, err := f.partners.Accept(ctx, bob, first.Code); !errors.Is(err, service.ErrInviteInvalid) {
		t.Errorf("Accept of a replaced code: err = %v, want ErrInviteInvalid", err)
	}

	tests := map[string]struct {
		caller uuid.UUID
		code   string
	}{
		"a wrong code":     {bob, "ZZZZZZZZ"},
		"an empty code":    {bob, ""},
		"the caller's own": {alice, second.Code},
	}
	for name, tt := range tests {
		if _, err := f.partners.Accept(ctx, tt.caller, tt.code); !errors.Is(err, service.ErrInviteInvalid) {
			t.Errorf("Accept of %s: err = %v, want ErrInviteInvalid", name, err)
		}
	}

	f.clock.Advance(48*time.Hour - time.Second)
	if _, err := f.partners.Get(ctx, alice); err != nil {
		t.Errorf("Get just before the invite expires: %v", err)
	}
	f.clock.Advance(time.Second)
	if _, err := f.partners.Get(ctx, alice); !errors.Is(err, service.ErrPartnerNotLinked) {
		t.Errorf("Get of an expired invite: err = %v, want ErrPartnerNotLinked", err)
	}
	if _, err := f.partners.Accept(ctx, bob, second.Code); !errors.Is(err, service.ErrInviteInvalid) {
		t.Errorf("Accept of an expired code: err = %v, want ErrInviteInvalid", err)
	}

	fresh, err := f.partners.Invite(ctx, alice)
	if err != nil {
		t.Fatalf("Invite after expiry: %v", err)
	}
	if _, err := f.partners.Accept(ctx, bob, fresh.Code); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if _, err := f.partners.Accept(ctx, carol, fresh.Code); !errors.Is(err, service.ErrInviteInvalid) {
		t.Errorf("Accept of a used code: err = %v, want ErrInviteInvalid", err)
	}
	if _, err := f.partners.Invite(ctx, alice); !errors.Is(err, service.ErrPartnerAlreadyLinked) {
		t.Errorf("Invite while linked: err = %v, want ErrPartnerAlreadyLinked", err)
	}
	if _, err := f.partners.Invite(ctx, bob); !errors.Is(err, service.ErrPartnerAlreadyLinked) {
		t.Errorf("Invite by the accepting side while linked: err = %v, want ErrPartnerAlreadyLinked", err)
	}
}

func TestPartnersAcceptWhileLinkedIsAConflictWhateverTheCode(t *testing.T) {
	f := newPartnersFixture(t)
	ctx := context.Background()
	alice := newNamedUser(t, f.st, "alice3@example.com", "Alice")
	bob := newNamedUser(t, f.st, "bob3@example.com", "Bob")
	carol := newNamedUser(t, f.st, "carol3@example.com", "Carol")
	f.link(t, alice, bob)
	carolInvite, err := f.partners.Invite(ctx, carol)
	if err != nil {
		t.Fatalf("Invite: %v", err)
	}

	for name, code := range map[string]string{"a valid code": carolInvite.Code, "a wrong code": "ZZZZZZZZ"} {
		if _, err := f.partners.Accept(ctx, alice, code); !errors.Is(err, service.ErrPartnerAlreadyLinked) {
			t.Errorf("a linked caller accepting %s: err = %v, want ErrPartnerAlreadyLinked (no oracle for guessing codes)", name, err)
		}
	}
}

func TestPartnersAcceptDropsTheCallersOwnPendingInvite(t *testing.T) {
	f := newPartnersFixture(t)
	ctx := context.Background()
	xena := newNamedUser(t, f.st, "xena4@example.com", "Xena")
	yuri := newNamedUser(t, f.st, "yuri4@example.com", "Yuri")
	zed := newNamedUser(t, f.st, "zed4@example.com", "Zed")

	xenaInvite, err := f.partners.Invite(ctx, xena)
	if err != nil {
		t.Fatalf("Invite by Xena: %v", err)
	}
	yuriInvite, err := f.partners.Invite(ctx, yuri)
	if err != nil {
		t.Fatalf("Invite by Yuri: %v", err)
	}
	if _, err := f.partners.Accept(ctx, xena, yuriInvite.Code); err != nil {
		t.Fatalf("Xena accepting Yuri's code: %v", err)
	}

	if _, err := f.partners.Accept(ctx, zed, xenaInvite.Code); !errors.Is(err, service.ErrInviteInvalid) {
		t.Errorf("Accept of Xena's old code after she linked: err = %v, want ErrInviteInvalid (not a conflict that would reveal the code was once valid)", err)
	}
	got, err := f.partners.Get(ctx, xena)
	if err != nil || got.Status != "active" || got.DisplayName == nil || *got.DisplayName != "Yuri" {
		t.Errorf("Xena's partnership = %+v (err %v), want the single active one with Yuri", got, err)
	}
}

func TestPartnersConcurrentAcceptsOfOneCodeHaveExactlyOneWinner(t *testing.T) {
	f := newPartnersFixture(t)
	ctx := context.Background()
	inviter := newNamedUser(t, f.st, "inviter5@example.com", "Inviter")
	inv, err := f.partners.Invite(ctx, inviter)
	if err != nil {
		t.Fatalf("Invite: %v", err)
	}

	const racers = 6
	users := make([]uuid.UUID, racers)
	for i := range users {
		users[i] = newNamedUser(t, f.st, "racer5-"+string(rune('a'+i))+"@example.com", "Racer")
	}
	errs := make([]error, racers)
	var wg sync.WaitGroup
	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = f.partners.Accept(ctx, users[i], inv.Code)
		}()
	}
	wg.Wait()

	winners := 0
	for i, err := range errs {
		switch {
		case err == nil:
			winners++
		case !errors.Is(err, service.ErrInviteInvalid) && !errors.Is(err, service.ErrPartnerAlreadyLinked):
			t.Errorf("racer %d: unexpected error %v", i, err)
		}
	}
	if winners != 1 {
		t.Fatalf("%d accepts of one code succeeded, want exactly 1", winners)
	}
}

func TestPartnersCrossedAcceptsLinkTwoUsersOnlyOnce(t *testing.T) {
	// Alice accepts Bob's code while Bob accepts Alice's. Each activation
	// would satisfy the unique indexes on its own (they sit in different
	// columns), so only Accept's locks and re-checks keep a user out of two
	// active rows.
	for round := range 5 {
		f := newPartnersFixture(t)
		ctx := context.Background()
		alice := newNamedUser(t, f.st, "alice6@example.com", "Alice")
		bob := newNamedUser(t, f.st, "bob6@example.com", "Bob")
		aliceInvite, err := f.partners.Invite(ctx, alice)
		if err != nil {
			t.Fatalf("round %d: Invite by Alice: %v", round, err)
		}
		bobInvite, err := f.partners.Invite(ctx, bob)
		if err != nil {
			t.Fatalf("round %d: Invite by Bob: %v", round, err)
		}

		var wg sync.WaitGroup
		errs := make([]error, 2)
		wg.Add(2)
		go func() { defer wg.Done(); _, errs[0] = f.partners.Accept(ctx, alice, bobInvite.Code) }()
		go func() { defer wg.Done(); _, errs[1] = f.partners.Accept(ctx, bob, aliceInvite.Code) }()
		wg.Wait()

		winners := 0
		for _, err := range errs {
			if err == nil {
				winners++
			}
		}
		if winners != 1 {
			t.Fatalf("round %d: %d of the two crossed accepts succeeded (errors %v), want exactly 1", round, winners, errs)
		}
		// Deleting Alice's partnership rows shows how many she is in.
		rows, err := f.st.DeletePartnershipsForUser(ctx, alice)
		if err != nil || len(rows) != 1 {
			t.Fatalf("round %d: Alice is in %d partnership rows (err %v), want 1", round, len(rows), err)
		}
	}
}

func TestPartnersUnlinkEndsTheLinkForBothSidesAndClosesStreams(t *testing.T) {
	f := newPartnersFixture(t)
	ctx := context.Background()
	alice := newNamedUser(t, f.st, "alice7@example.com", "Alice")
	bob := newNamedUser(t, f.st, "bob7@example.com", "Bob")
	f.link(t, alice, bob)

	list := uuid.New()
	bobWatching, err := f.events.Subscribe(list, bob, alice)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer bobWatching.Close()
	aliceWatching, err := f.events.Subscribe(list, alice, alice)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer aliceWatching.Close()

	if err := f.partners.Unlink(ctx, bob); err != nil {
		t.Fatalf("Unlink by the accepting side: %v", err)
	}
	for name, id := range map[string]uuid.UUID{"the accepting side": bob, "the inviter": alice} {
		if _, err := f.partners.Get(ctx, id); !errors.Is(err, service.ErrPartnerNotLinked) {
			t.Errorf("Get by %s after unlink: err = %v, want ErrPartnerNotLinked", name, err)
		}
	}
	if err := f.partners.Unlink(ctx, alice); !errors.Is(err, service.ErrPartnerNotLinked) {
		t.Errorf("second Unlink: err = %v, want ErrPartnerNotLinked", err)
	}
	if !closedWithin(bobWatching) {
		t.Error("the partner's event stream stayed open after the unlink")
	}
	if !stillOpen(aliceWatching) {
		t.Error("the owner's own event stream was closed by the unlink")
	}

	// Re-linking is a fresh invite.
	f.link(t, bob, alice)
}

func TestPartnersUnlinkCancelsAPendingInvite(t *testing.T) {
	f := newPartnersFixture(t)
	ctx := context.Background()
	alice := newNamedUser(t, f.st, "alice8@example.com", "Alice")
	bob := newNamedUser(t, f.st, "bob8@example.com", "Bob")
	inv, err := f.partners.Invite(ctx, alice)
	if err != nil {
		t.Fatalf("Invite: %v", err)
	}
	if err := f.partners.Unlink(ctx, alice); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if _, err := f.partners.Accept(ctx, bob, inv.Code); !errors.Is(err, service.ErrInviteInvalid) {
		t.Errorf("Accept of a cancelled invite: err = %v, want ErrInviteInvalid", err)
	}
}

func TestPartnersDeletingAnAccountEndsItsPartnership(t *testing.T) {
	f := newPartnersFixture(t)
	ctx := context.Background()
	alice := newNamedUser(t, f.st, "alice9@example.com", "Alice")
	bob := newNamedUser(t, f.st, "bob9@example.com", "Bob")
	f.link(t, alice, bob)

	if n, err := f.st.DeleteUser(ctx, alice); err != nil || n != 1 {
		t.Fatalf("delete user: n = %d, err = %v", n, err)
	}
	if _, err := f.partners.Get(ctx, bob); !errors.Is(err, service.ErrPartnerNotLinked) {
		t.Errorf("Get by the surviving partner: err = %v, want ErrPartnerNotLinked", err)
	}
	if _, err := f.partners.Invite(ctx, bob); err != nil {
		t.Errorf("Invite by the surviving partner: %v", err)
	}
}

// A user who is user_a of one active row and user_b of another cannot be
// caught by the unique indexes, so Accept must lock both users and look again.
// This test holds Bob's users row in another transaction, links Bob to Carol
// there, and only then lets Accept (Bob taking Alice's code) continue.
func TestPartnersAcceptWaitsForTheUserLocksAndChecksAgainAfterThem(t *testing.T) {
	f := newPartnersFixture(t)
	ctx := context.Background()
	alice := newNamedUser(t, f.st, "alice10@example.com", "Alice")
	bob := newNamedUser(t, f.st, "bob10@example.com", "Bob")
	carol := newNamedUser(t, f.st, "carol10@example.com", "Carol")
	aliceInvite, err := f.partners.Invite(ctx, alice)
	if err != nil {
		t.Fatalf("Invite by Alice: %v", err)
	}
	if _, err := f.partners.Invite(ctx, bob); err != nil {
		t.Fatalf("Invite by Bob: %v", err)
	}
	bobsInvite, err := f.st.GetPartnershipForUser(ctx, bob)
	if err != nil {
		t.Fatalf("read Bob's invite: %v", err)
	}

	locked, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseTx := func() { releaseOnce.Do(func() { close(release) }) }
	// A failed assertion below must not leave the transaction open: closing
	// the pool would wait for it forever.
	t.Cleanup(releaseTx)
	txDone := make(chan error, 1)
	go func() {
		txDone <- f.st.InTx(ctx, func(q *sqlc.Queries) error {
			if _, err := q.LockUsersForUpdate(ctx, []uuid.UUID{bob, carol}); err != nil {
				return err
			}
			close(locked)
			<-release
			// Carol takes Bob's code: Bob is now user_a of an active row.
			_, err := q.ActivatePartnership(ctx, sqlc.ActivatePartnershipParams{ID: bobsInvite.ID, UserB: &carol})
			return err
		})
	}()
	<-locked

	accepted := make(chan error, 1)
	go func() {
		_, err := f.partners.Accept(ctx, bob, aliceInvite.Code)
		accepted <- err
	}()
	select {
	case err := <-accepted:
		t.Fatalf("Accept finished (err %v) while another transaction held Bob's users row, want it to wait for the lock", err)
	case <-time.After(300 * time.Millisecond):
	}

	releaseTx()
	if err := <-txDone; err != nil {
		t.Fatalf("linking Bob and Carol: %v", err)
	}
	if err := <-accepted; !errors.Is(err, service.ErrPartnerAlreadyLinked) {
		t.Errorf("Accept after Bob got linked meanwhile: err = %v, want ErrPartnerAlreadyLinked", err)
	}
	rows, err := f.st.DeletePartnershipsForUser(ctx, bob)
	if err != nil || len(rows) != 1 {
		t.Errorf("Bob is in %d partnership rows (err %v), want 1", len(rows), err)
	}
}
```

Create `backend/internal/service/partners_edges_test.go`:

```go
package service_test

import (
	"context"
	"sync"
	"testing"
)

// A double tap on "invite" sends two requests at once. Both must succeed, and
// only one invite may be left alive: the newest replaces the others.
func TestPartnersConcurrentInvitesByOneUserLeaveExactlyOnePendingInvite(t *testing.T) {
	f := newPartnersFixture(t)
	ctx := context.Background()
	alice := newNamedUser(t, f.st, "alice-edge1@example.com", "Alice")

	const taps = 6
	errs := make([]error, taps)
	var wg sync.WaitGroup
	for i := range taps {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = f.partners.Invite(ctx, alice)
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("concurrent Invite %d: %v, want every request to succeed", i, err)
		}
	}
	rows, err := f.st.DeletePartnershipsForUser(ctx, alice)
	if err != nil || len(rows) != 1 || rows[0].Status != "pending" {
		t.Errorf("Alice's partnership rows after %d concurrent invites = %+v (err %v), want exactly one pending invite", taps, rows, err)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `cd backend && go test ./internal/service/ -run 'Partners' -count=1`

Expected: FAIL, for the right reason:

```
internal/service/partners_test.go:21:20: undefined: service.Partners
internal/service/partners_test.go:49:43: undefined: service.NewPartners
internal/service/partners_test.go:139:80: undefined: service.ErrInviteInvalid
```

- [ ] **Step 3: Implement**

Create `backend/internal/store/queries/partnerships.sql`:

```sql
-- name: GetActivePartnerID :one
-- The other side of the user's active partnership. store.IsNotFound on the
-- error means the user has no active partner.
SELECT (CASE WHEN user_a = sqlc.arg('user_id') THEN user_b ELSE user_a END)::uuid AS partner_id
FROM partnerships
WHERE status = 'active' AND (user_a = sqlc.arg('user_id') OR user_b = sqlc.arg('user_id'));

-- name: GetActivePartnerIDForShare :one
-- GetActivePartnerID plus a shared lock on the partnership row. Shopping-item
-- writes use it: Partners.Unlink's DELETE waits for the edits in flight, and an
-- edit that starts afterwards finds no partner (spec §6, unlink ordering).
SELECT (CASE WHEN user_a = sqlc.arg('user_id') THEN user_b ELSE user_a END)::uuid AS partner_id
FROM partnerships
WHERE status = 'active' AND (user_a = sqlc.arg('user_id') OR user_b = sqlc.arg('user_id'))
FOR SHARE;

-- name: LockUsersForUpdate :many
-- Locks the given users rows in id order, so two transactions that lock the
-- same pair cannot deadlock. FOR NO KEY UPDATE conflicts with itself but not
-- with the FOR KEY SHARE that inserts of dependent rows take, so it does not
-- block a user's other writes. Partners.Invite and Partners.Accept use it to
-- serialize their check-then-write over one user.
SELECT id FROM users
WHERE id = ANY(sqlc.arg('ids')::uuid[])
ORDER BY id
FOR NO KEY UPDATE;

-- name: GetPartnershipForUser :one
-- The user's partnership row (pending or active) with the other side's display
-- name, which is NULL while the invite is still pending.
SELECT partnerships.id, partnerships.status, partnerships.invite_expires_at, partnerships.updated_at,
       other.display_name AS partner_display_name
FROM partnerships
LEFT JOIN users AS other
       ON other.id = CASE WHEN partnerships.user_a = sqlc.arg('user_id') THEN partnerships.user_b ELSE partnerships.user_a END
WHERE partnerships.user_a = sqlc.arg('user_id') OR partnerships.user_b = sqlc.arg('user_id');

-- name: GetPendingPartnershipByCodeHash :one
SELECT * FROM partnerships
WHERE status = 'pending' AND invite_code_hash = sqlc.arg('invite_code_hash');

-- name: DeletePendingPartnershipForUser :exec
DELETE FROM partnerships WHERE user_a = sqlc.arg('user_id') AND status = 'pending';

-- name: CreatePartnerInvite :one
INSERT INTO partnerships (user_a, status, invite_code_hash, invite_expires_at, created_by)
VALUES (sqlc.arg('user_id'), 'pending', sqlc.arg('invite_code_hash'), sqlc.arg('invite_expires_at'), sqlc.arg('user_id'))
RETURNING *;

-- name: ActivatePartnership :one
-- Turns a pending invite into an active partnership and clears the code, so a
-- spent code cannot be replayed. No row means the invite is gone (cancelled,
-- replaced or accepted a moment ago).
UPDATE partnerships SET
    user_b            = sqlc.arg('user_b'),
    status            = 'active',
    invite_code_hash  = NULL,
    invite_expires_at = NULL
WHERE id = sqlc.arg('id') AND status = 'pending'
RETURNING *;

-- name: DeletePartnershipsForUser :many
-- Ends the user's active partnership or cancels their pending invite. Returns
-- what was deleted so Partners.Unlink can close the partner's event streams.
DELETE FROM partnerships
WHERE user_a = sqlc.arg('user_id') OR user_b = sqlc.arg('user_id')
RETURNING id, user_a, user_b, status;
```

Create `backend/internal/service/partners.go`:

```go
// Package service holds business rules. It calls the store and never speaks HTTP.
package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/InzKazik/mealplanner/backend/internal/store"
	"github.com/InzKazik/mealplanner/backend/internal/store/sqlc"
)

// Errors returned by Partners and by the partner listings of the other
// services. Handlers map them to problem responses.
var (
	// ErrPartnerNotLinked means the caller has no active partner (or, for
	// Get, no live invite).
	ErrPartnerNotLinked = errors.New("no linked partner")
	// ErrPartnerAlreadyLinked means the caller already has an active partner.
	ErrPartnerAlreadyLinked = errors.New("already linked to a partner")
	// ErrInviteInvalid means the invite code is wrong, expired, the caller's
	// own, or already used. Those cases are deliberately indistinguishable.
	ErrInviteInvalid = errors.New("invite code is invalid")
)

const (
	// inviteAlphabet has no 0, O, 1, I or L, so a code read aloud or from a
	// screenshot is hard to mistype.
	inviteAlphabet   = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"
	inviteCodeLength = 8
	inviteTTL        = 48 * time.Hour
	// inviteCodeAttempts is how often Invite retries when a freshly generated
	// code collides with another live invite's hash (about 1 in 10^12).
	inviteCodeAttempts = 3
)

// Invite is a freshly created invite. Code is shown once: only its hash is
// stored.
type Invite struct {
	Code      string
	ExpiresAt time.Time
}

// Partnership is a user's partnership as they see it. DisplayName and
// LinkedAt are set while Status is "active", ExpiresAt while it is "pending".
type Partnership struct {
	Status      string // "pending" | "active"
	DisplayName *string
	LinkedAt    *time.Time
	ExpiresAt   *time.Time
}

// Partners links a user with one partner through an invite code, and ends the
// link. The visibility rules that follow from a link live in Meals,
// DietTemplates and ShoppingLists, which resolve the partner with
// activePartnerID.
type Partners struct {
	st     *store.Store
	events *ListEventHub
	now    func() time.Time
}

// NewPartners returns a Partners service. events is closed on unlink so a
// partner's open shopping-list streams end with the link.
func NewPartners(st *store.Store, events *ListEventHub, now func() time.Time) *Partners {
	return &Partners{st: st, events: events, now: now}
}

// Invite creates an invite for callerID, replacing any pending one, and
// returns its code. It fails with ErrPartnerAlreadyLinked if the caller is
// linked.
func (p *Partners) Invite(ctx context.Context, callerID uuid.UUID) (Invite, error) {
	var inv Invite
	err := p.st.InTx(ctx, func(q *sqlc.Queries) error {
		// Serialize with Accept and with a concurrent Invite by the same user.
		if err := lockUsers(ctx, q, callerID); err != nil {
			return err
		}
		if err := requireUnlinked(ctx, q, callerID, ErrPartnerAlreadyLinked); err != nil {
			return err
		}
		if err := q.DeletePendingPartnershipForUser(ctx, callerID); err != nil {
			return fmt.Errorf("replace pending invite: %w", err)
		}
		expires := p.now().Add(inviteTTL)
		for range inviteCodeAttempts {
			code, err := newInviteCode()
			if err != nil {
				return err
			}
			_, err = q.CreatePartnerInvite(ctx, sqlc.CreatePartnerInviteParams{
				UserID: callerID, InviteCodeHash: hashInviteCode(code), InviteExpiresAt: &expires,
			})
			if store.IsUniqueViolation(err, "partnerships_invite_code_hash_idx") {
				continue
			}
			if store.IsForeignKeyViolation(err, "partnerships_user_a_fkey") {
				return ErrNotFound
			}
			if err != nil {
				return fmt.Errorf("create invite: %w", err)
			}
			inv = Invite{Code: code, ExpiresAt: expires}
			return nil
		}
		return errors.New("could not generate a unique invite code")
	})
	if err != nil {
		return Invite{}, err
	}
	return inv, nil
}

// Accept links callerID with the user who issued code. A wrong, expired, own
// or used code is ErrInviteInvalid, all alike; a caller who already has a
// partner gets ErrPartnerAlreadyLinked whatever the code, so a linked user
// cannot use Accept to test codes.
func (p *Partners) Accept(ctx context.Context, callerID uuid.UUID, code string) (Partnership, error) {
	hash := hashInviteCode(code)
	var out Partnership
	err := p.st.InTx(ctx, func(q *sqlc.Queries) error {
		if err := requireUnlinked(ctx, q, callerID, ErrPartnerAlreadyLinked); err != nil {
			return err
		}
		invite, err := q.GetPendingPartnershipByCodeHash(ctx, hash)
		if store.IsNotFound(err) {
			return ErrInviteInvalid
		}
		if err != nil {
			return fmt.Errorf("find invite: %w", err)
		}
		if invite.UserA == callerID {
			return ErrInviteInvalid
		}

		// Lock both users in id order, then re-check everything: two accepts
		// by different users, or an accept racing an invite replace or an
		// unlink, must not both win. A user appearing once across both
		// columns of active rows is not expressible as an index.
		if err := lockUsers(ctx, q, callerID, invite.UserA); err != nil {
			return err
		}
		if err := requireUnlinked(ctx, q, callerID, ErrPartnerAlreadyLinked); err != nil {
			return err
		}
		if err := requireUnlinked(ctx, q, invite.UserA, ErrInviteInvalid); err != nil {
			return err // the inviter got linked meanwhile
		}
		invite, err = q.GetPendingPartnershipByCodeHash(ctx, hash)
		if store.IsNotFound(err) {
			return ErrInviteInvalid
		}
		if err != nil {
			return fmt.Errorf("find invite: %w", err)
		}
		if invite.InviteExpiresAt == nil || !p.now().Before(*invite.InviteExpiresAt) {
			return ErrInviteInvalid
		}

		// The caller's own pending invite dies with their becoming linked, so
		// no stale code stays valid.
		if err := q.DeletePendingPartnershipForUser(ctx, callerID); err != nil {
			return fmt.Errorf("drop own pending invite: %w", err)
		}
		row, err := q.ActivatePartnership(ctx, sqlc.ActivatePartnershipParams{ID: invite.ID, UserB: &callerID})
		if store.IsNotFound(err) {
			return ErrInviteInvalid
		}
		if store.IsUniqueViolation(err, "partnerships_active_user_a_idx") || store.IsUniqueViolation(err, "partnerships_active_user_b_idx") {
			return ErrPartnerAlreadyLinked
		}
		if store.IsForeignKeyViolation(err, "partnerships_user_b_fkey") {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("activate partnership: %w", err)
		}
		inviter, err := q.GetUserByID(ctx, invite.UserA)
		if err != nil {
			return fmt.Errorf("get inviter: %w", err)
		}
		out = Partnership{Status: row.Status, DisplayName: &inviter.DisplayName, LinkedAt: &row.UpdatedAt}
		return nil
	})
	if err != nil {
		return Partnership{}, err
	}
	return out, nil
}

// Get returns callerID's partnership: the active partner, or a pending invite
// that has not expired. Anything else is ErrPartnerNotLinked.
func (p *Partners) Get(ctx context.Context, callerID uuid.UUID) (Partnership, error) {
	row, err := p.st.GetPartnershipForUser(ctx, callerID)
	if store.IsNotFound(err) {
		return Partnership{}, ErrPartnerNotLinked
	}
	if err != nil {
		return Partnership{}, fmt.Errorf("get partnership: %w", err)
	}
	if row.Status == "active" {
		return Partnership{Status: row.Status, DisplayName: row.PartnerDisplayName, LinkedAt: &row.UpdatedAt}, nil
	}
	if row.InviteExpiresAt == nil || !p.now().Before(*row.InviteExpiresAt) {
		return Partnership{}, ErrPartnerNotLinked
	}
	return Partnership{Status: row.Status, ExpiresAt: row.InviteExpiresAt}, nil
}

// Unlink ends callerID's active partnership, or cancels their pending invite
// (an expired one too). Either side may unlink. Access ends at once: the
// partner's next read finds no partnership, and their open shopping-list
// streams are closed.
func (p *Partners) Unlink(ctx context.Context, callerID uuid.UUID) error {
	rows, err := p.st.DeletePartnershipsForUser(ctx, callerID)
	if err != nil {
		return fmt.Errorf("delete partnership: %w", err)
	}
	if len(rows) == 0 {
		return ErrPartnerNotLinked
	}
	for _, r := range rows {
		if r.Status == "active" && r.UserB != nil {
			p.events.CloseAccess(r.UserA, *r.UserB)
		}
	}
	return nil
}

// activePartnerID returns the id of userID's active partner, or
// ErrPartnerNotLinked. With forShare it also takes a shared lock on the
// partnership row, so an Unlink's DELETE waits for the caller's transaction.
// The Meals, DietTemplates and ShoppingLists services call it once at the
// start of a transaction and pass the result into their visibility queries;
// nothing is cached, so an unlink is visible to the next transaction.
func activePartnerID(ctx context.Context, q *sqlc.Queries, userID uuid.UUID, forShare bool) (uuid.UUID, error) {
	var (
		id  uuid.UUID
		err error
	)
	if forShare {
		id, err = q.GetActivePartnerIDForShare(ctx, userID)
	} else {
		id, err = q.GetActivePartnerID(ctx, userID)
	}
	if store.IsNotFound(err) {
		return uuid.Nil, ErrPartnerNotLinked
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("get active partner: %w", err)
	}
	return id, nil
}

// requireUnlinked returns nil if userID has no active partner and linkedErr
// if they do.
func requireUnlinked(ctx context.Context, q *sqlc.Queries, userID uuid.UUID, linkedErr error) error {
	_, err := activePartnerID(ctx, q, userID, false)
	switch {
	case err == nil:
		return linkedErr
	case errors.Is(err, ErrPartnerNotLinked):
		return nil
	default:
		return err
	}
}

func lockUsers(ctx context.Context, q *sqlc.Queries, ids ...uuid.UUID) error {
	locked, err := q.LockUsersForUpdate(ctx, ids)
	if err != nil {
		return fmt.Errorf("lock users: %w", err)
	}
	if len(locked) != len(ids) {
		return ErrNotFound // a user vanished (account deleted)
	}
	return nil
}

// newInviteCode returns inviteCodeLength characters drawn uniformly from
// inviteAlphabet.
func newInviteCode() (string, error) {
	var b strings.Builder
	limit := big.NewInt(int64(len(inviteAlphabet)))
	for range inviteCodeLength {
		n, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", fmt.Errorf("generate invite code: %w", err)
		}
		b.WriteByte(inviteAlphabet[n.Int64()])
	}
	return b.String(), nil
}

// hashInviteCode hashes a code after normalizing what a person types: case,
// spaces and dashes do not matter. SHA-256 is enough because the code is
// random, short-lived (inviteTTL) and rate limited; it is never a password.
func hashInviteCode(code string) []byte {
	normalized := strings.ToUpper(strings.NewReplacer(" ", "", "-", "").Replace(code))
	sum := sha256.Sum256([]byte(normalized))
	return sum[:]
}
```

- [ ] **Step 4: Regenerate**

Run `make generate` from the repo root. It rewrites `backend/internal/store/sqlc/`. Commit the output and never edit it by hand. `make check-generated` fails if it is stale.

- [ ] **Step 5: Run the tests and see them pass**

Run: `cd backend && go test ./internal/service/ -run 'Partners' -count=1`

Expected: PASS (`ok  	github.com/InzKazik/mealplanner/backend/internal/service`).

- [ ] **Step 6: Commit**

```bash
git add backend/internal/service/partners.go backend/internal/service/partners_edges_test.go backend/internal/service/partners_test.go backend/internal/store/queries/partnerships.sql backend/internal/store/sqlc/models.go backend/internal/store/sqlc/partnerships.sql.go
git commit -m "feat(backend): link two users with an invite code"
```

### Task 4: Meals: partner read, copy and the partner listing

**Files:**
- Modify: `backend/internal/store/queries/meals.sql`
- Create: `backend/internal/service/copy.go`
- Modify: `backend/internal/service/meals.go`
- Modify: `backend/internal/service/partners.go` (add `partnerOrNil`)
- Modify: `backend/internal/service/plan.go` (use `Meals.GetOwn`)
- Modify: `backend/internal/service/meals_test.go`
- Modify: `backend/internal/service/partners_test.go` (two helpers)
- Create: `backend/internal/service/meals_partner_edges_test.go`
- Generated: `backend/internal/store/sqlc/meals.sql.go`

**Interfaces:**
- Consumes: `activePartnerID`, `ErrPartnerNotLinked`, `Partners` (Task 3).
- Produces: `partnerOrNil(ctx, q *sqlc.Queries, userID uuid.UUID, forShare bool) (*uuid.UUID, error)` (nil = no partner); sqlc `GetMealForUserParams{ID, UserID uuid.UUID; PartnerID *uuid.UUID}` and `ListMealsForUserParams` gaining `SharedOnly bool`; `Meal.OwnerID uuid.UUID`; `(*Meals).Get(ctx, callerID, id)` (read predicate), `GetOwn(ctx, ownerID, id)` (owner only), `List` (own only), `ListPartner(ctx, callerID, in ListMealsInput) (MealPage, error)`, `Copy`; `ingredientCopier` with `newIngredientCopier(q, from, to uuid.UUID)` and `remap(ctx, ingredientID) (uuid.UUID, error)`; `copyMeal(ctx, q, callerID uuid.UUID, original sqlc.Meal, ic *ingredientCopier) (sqlc.Meal, []sqlc.MealIngredient, error)`; test helpers `partnersFor(st) *Partners` and `linkPartners(t, st, a, b) *Partners`.

`GetMealForUser` now takes a `partner_id`; passing none keeps it owner-only, which is what `Plan.SetEntry` and every write want. `Plan.GetRange` reads each meal through the new `GetOwn` so a plan range does not pay one partner lookup per meal. Deleting the original after a partner copied it changes nothing for the copy, because a copy of a partner's meal owns duplicates of its custom ingredients.

- [ ] **Step 1: Write the failing tests**

Modify `backend/internal/service/meals_test.go` (unified diff against the current file):

```diff
--- a/backend/internal/service/meals_test.go
+++ b/backend/internal/service/meals_test.go
@@ -279,7 +279,7 @@ func TestMealsEmptyMealHasZeroNutritionForEveryKey(t *testing.T) {
 	}
 }
 
-func TestMealsAreOwnerOnlyForNow(t *testing.T) {
+func TestMealsOfOneUserAreInvisibleAndUntouchableToAStranger(t *testing.T) {
 	meals, _, st := newMealsFixture(t)
 	owner := newTestUser(t, st, "owner7@example.com")
 	other := newTestUser(t, st, "other7@example.com")
@@ -666,3 +666,226 @@ func TestMealsDeleteIsBlockedWhileInUseByAPlanEntry(t *testing.T) {
 		t.Errorf("Delete once no longer referenced: %v", err)
 	}
 }
+
+func TestMealsPartnerReadsOnlySharedMealsAndNeverWritesThem(t *testing.T) {
+	meals, ing, st := newMealsFixture(t)
+	ctx := context.Background()
+	alice := newTestUser(t, st, "alice20@example.com")
+	bob := newTestUser(t, st, "bob20@example.com")
+	carol := newTestUser(t, st, "carol20@example.com")
+	partners := linkPartners(t, st, alice, bob)
+
+	oats := mustCreateIngredient(t, ing, alice, service.CreateIngredientInput{
+		Name: "Alice's Oats", Category: "grains_bread",
+		Nutrients: map[string]float64{service.NutrientCalories: 400},
+	})
+	shared, err := meals.Create(ctx, alice, service.CreateMealInput{Name: "Shared Porridge", Servings: 2, SharedWithPartner: true})
+	if err != nil {
+		t.Fatalf("Create shared: %v", err)
+	}
+	shared, err = meals.ReplaceIngredients(ctx, alice, shared.ID, []service.MealIngredientInput{{IngredientID: oats.ID, Quantity: 100, Unit: "g"}})
+	if err != nil {
+		t.Fatalf("ReplaceIngredients: %v", err)
+	}
+	private, err := meals.Create(ctx, alice, service.CreateMealInput{Name: "Private Snack", Servings: 1})
+	if err != nil {
+		t.Fatalf("Create private: %v", err)
+	}
+
+	got, err := meals.Get(ctx, bob, shared.ID)
+	if err != nil {
+		t.Fatalf("partner Get of a shared meal: %v", err)
+	}
+	if got.OwnerID != alice || len(got.Ingredients) != 1 || got.Ingredients[0].IngredientName != "Alice's Oats" ||
+		got.NutritionPerServing[service.NutrientCalories] != 200 {
+		t.Errorf("partner sees %+v, want Alice's meal with her custom ingredient resolved and 200 kcal per serving", got)
+	}
+	if own, err := meals.Get(ctx, alice, shared.ID); err != nil || own.OwnerID != alice {
+		t.Errorf("owner Get: %+v, %v", own, err)
+	}
+
+	name := "Hijacked"
+	for label, err := range map[string]error{
+		"Get of an unshared meal":        errOf(meals.Get(ctx, bob, private.ID)),
+		"Get by a stranger":              errOf(meals.Get(ctx, carol, shared.ID)),
+		"Update of a shared meal":        errOf(meals.Update(ctx, bob, shared.ID, service.UpdateMealInput{Name: &name})),
+		"Delete of a shared meal":        meals.Delete(ctx, bob, shared.ID),
+		"ReplaceIngredients of a shared": errOf(meals.ReplaceIngredients(ctx, bob, shared.ID, nil)),
+	} {
+		if !errors.Is(err, service.ErrMealNotFound) {
+			t.Errorf("%s: err = %v, want ErrMealNotFound", label, err)
+		}
+	}
+
+	ownList, err := meals.List(ctx, bob, service.ListMealsInput{Limit: 10})
+	if err != nil || len(ownList.Items) != 0 {
+		t.Errorf("the partner's own List = %+v (err %v), want empty: shared meals are not mixed in", ownList.Items, err)
+	}
+	partnerList, err := meals.ListPartner(ctx, bob, service.ListMealsInput{Limit: 10})
+	if err != nil || len(partnerList.Items) != 1 || partnerList.Items[0].ID != shared.ID {
+		t.Errorf("ListPartner = %+v (err %v), want only the shared meal", partnerList.Items, err)
+	}
+	if _, err := meals.ListPartner(ctx, carol, service.ListMealsInput{Limit: 10}); !errors.Is(err, service.ErrPartnerNotLinked) {
+		t.Errorf("ListPartner without a partner: err = %v, want ErrPartnerNotLinked", err)
+	}
+
+	// Unsharing hides the meal at once; so does unlinking.
+	if _, err := meals.Update(ctx, alice, shared.ID, service.UpdateMealInput{SharedWithPartner: ptr(false)}); err != nil {
+		t.Fatalf("unshare: %v", err)
+	}
+	if _, err := meals.Get(ctx, bob, shared.ID); !errors.Is(err, service.ErrMealNotFound) {
+		t.Errorf("partner Get after unsharing: err = %v, want ErrMealNotFound", err)
+	}
+	if _, err := meals.Update(ctx, alice, shared.ID, service.UpdateMealInput{SharedWithPartner: ptr(true)}); err != nil {
+		t.Fatalf("share again: %v", err)
+	}
+	if err := partners.Unlink(ctx, alice); err != nil {
+		t.Fatalf("Unlink: %v", err)
+	}
+	if _, err := meals.Get(ctx, bob, shared.ID); !errors.Is(err, service.ErrMealNotFound) {
+		t.Errorf("partner Get after unlinking: err = %v, want ErrMealNotFound", err)
+	}
+	if _, err := meals.ListPartner(ctx, bob, service.ListMealsInput{Limit: 10}); !errors.Is(err, service.ErrPartnerNotLinked) {
+		t.Errorf("ListPartner after unlinking: err = %v, want ErrPartnerNotLinked", err)
+	}
+}
+
+// errOf drops a call's value, keeping its error, so a table of calls with
+// different return types can be checked the same way.
+func errOf[T any](_ T, err error) error { return err }
+
+func TestMealsCopyOfAPartnersMealDuplicatesCustomIngredientsOnceAndSharesGlobalOnes(t *testing.T) {
+	meals, ing, st := newMealsFixture(t)
+	ctx := context.Background()
+	alice := newTestUser(t, st, "alice21@example.com")
+	bob := newTestUser(t, st, "bob21@example.com")
+	carol := newTestUser(t, st, "carol21@example.com")
+	partners := linkPartners(t, st, alice, bob)
+
+	fdc := int32(170000)
+	salt, err := st.UpsertUSDAIngredient(ctx, sqlc.UpsertUSDAIngredientParams{Name: "Salt", Category: "spices_herbs", UsdaFdcID: &fdc})
+	if err != nil {
+		t.Fatalf("UpsertUSDAIngredient: %v", err)
+	}
+	flour := mustCreateIngredient(t, ing, alice, service.CreateIngredientInput{
+		Name: "Alice's Flour", Category: "grains_bread", GramsPerPiece: ptr(30.0),
+		Nutrients: map[string]float64{service.NutrientCalories: 360, service.NutrientProtein: 10},
+	})
+	original, err := meals.Create(ctx, alice, service.CreateMealInput{Name: "Flatbread", Notes: strPtr("thin"), Servings: 2, SharedWithPartner: true})
+	if err != nil {
+		t.Fatalf("Create: %v", err)
+	}
+	// The custom ingredient is on two lines, in two units; the global one on a third.
+	original, err = meals.ReplaceIngredients(ctx, alice, original.ID, []service.MealIngredientInput{
+		{IngredientID: flour.ID, Quantity: 200, Unit: "g"},
+		{IngredientID: salt.ID, Quantity: 5, Unit: "g"},
+		{IngredientID: flour.ID, Quantity: 2, Unit: "piece"},
+	})
+	if err != nil {
+		t.Fatalf("ReplaceIngredients: %v", err)
+	}
+
+	cp, err := meals.Copy(ctx, bob, original.ID)
+	if err != nil {
+		t.Fatalf("partner Copy: %v", err)
+	}
+	if cp.OwnerID != bob || cp.SharedWithPartner || cp.Name != "Flatbread" || cp.Notes == nil || *cp.Notes != "thin" || cp.Servings != 2 {
+		t.Errorf("copy = %+v, want Bob's private copy of the meal", cp)
+	}
+	if len(cp.Ingredients) != 3 {
+		t.Fatalf("copy has %d lines, want 3", len(cp.Ingredients))
+	}
+	copiedFlour := cp.Ingredients[0].IngredientID
+	if copiedFlour == flour.ID || cp.Ingredients[2].IngredientID != copiedFlour {
+		t.Errorf("flour ids on the copy = %v and %v, want one new id (not %v) on both lines", copiedFlour, cp.Ingredients[2].IngredientID, flour.ID)
+	}
+	if cp.Ingredients[1].IngredientID != salt.ID {
+		t.Errorf("the global ingredient on the copy = %v, want the shared %v", cp.Ingredients[1].IngredientID, salt.ID)
+	}
+	for k, want := range original.NutritionPerServing {
+		if got := cp.NutritionPerServing[k]; !almostEqual(got, want) {
+			t.Errorf("copy nutrition[%s] = %v, want the original's %v", k, got, want)
+		}
+	}
+
+	// The duplicate is a real custom ingredient in Bob's library, invisible to Alice.
+	dup, err := st.GetIngredientForUser(ctx, sqlc.GetIngredientForUserParams{ID: copiedFlour, UserID: &bob})
+	if err != nil || dup.OwnerID == nil || *dup.OwnerID != bob || dup.GramsPerPiece == nil || *dup.GramsPerPiece != 30 {
+		t.Errorf("duplicate ingredient = %+v (err %v), want Bob's custom flour with grams_per_piece 30", dup, err)
+	}
+	if _, err := st.GetIngredientForUser(ctx, sqlc.GetIngredientForUserParams{ID: copiedFlour, UserID: &alice}); !store.IsNotFound(err) {
+		t.Errorf("Alice sees Bob's duplicate ingredient: err = %v, want not found", err)
+	}
+
+	// The copy is independent of the original: rename the ingredient, delete
+	// the meal, delete the ingredient's owner's ingredient.
+	if _, err := ing.Update(ctx, alice, flour.ID, service.UpdateIngredientInput{Name: strPtr("Renamed")}); err != nil {
+		t.Fatalf("rename original ingredient: %v", err)
+	}
+	if err := meals.Delete(ctx, alice, original.ID); err != nil {
+		t.Fatalf("delete original meal: %v", err)
+	}
+	if err := ing.Delete(ctx, alice, flour.ID); err != nil {
+		t.Fatalf("delete original ingredient: %v", err)
+	}
+	after, err := meals.Get(ctx, bob, cp.ID)
+	if err != nil || after.Ingredients[0].IngredientName != "Alice's Flour" {
+		t.Errorf("copy after the original changed = %+v (err %v), want it untouched", after.Ingredients, err)
+	}
+
+	// Bob now owns the ingredient, so copying his own copy keeps referencing it.
+	again, err := meals.Copy(ctx, bob, cp.ID)
+	if err != nil || again.Ingredients[0].IngredientID != copiedFlour {
+		t.Errorf("copy of Bob's own copy: %+v (err %v), want it to keep Bob's own ingredient", again.Ingredients, err)
+	}
+
+	// Not shared, or not a partner: not copyable.
+	hidden, err := meals.Create(ctx, alice, service.CreateMealInput{Name: "Hidden", Servings: 1})
+	if err != nil {
+		t.Fatalf("Create hidden: %v", err)
+	}
+	if _, err := meals.Copy(ctx, bob, hidden.ID); !errors.Is(err, service.ErrMealNotFound) {
+		t.Errorf("Copy of an unshared partner meal: err = %v, want ErrMealNotFound", err)
+	}
+	visible, err := meals.Create(ctx, alice, service.CreateMealInput{Name: "Visible", Servings: 1, SharedWithPartner: true})
+	if err != nil {
+		t.Fatalf("Create visible: %v", err)
+	}
+	if _, err := meals.Copy(ctx, carol, visible.ID); !errors.Is(err, service.ErrMealNotFound) {
+		t.Errorf("Copy by a stranger: err = %v, want ErrMealNotFound", err)
+	}
+	if err := partners.Unlink(ctx, bob); err != nil {
+		t.Fatalf("Unlink: %v", err)
+	}
+	if _, err := meals.Copy(ctx, bob, visible.ID); !errors.Is(err, service.ErrMealNotFound) {
+		t.Errorf("Copy after unlinking: err = %v, want ErrMealNotFound", err)
+	}
+}
+
+func TestMealsEachCopyOfAPartnersMealDuplicatesItsCustomIngredientsAgain(t *testing.T) {
+	meals, ing, st := newMealsFixture(t)
+	ctx := context.Background()
+	alice := newTestUser(t, st, "alice22@example.com")
+	bob := newTestUser(t, st, "bob22@example.com")
+	linkPartners(t, st, alice, bob)
+
+	milk := mustCreateIngredient(t, ing, alice, service.CreateIngredientInput{Name: "Alice's Milk", Category: "dairy_eggs"})
+	m, err := meals.Create(ctx, alice, service.CreateMealInput{Name: "Latte", Servings: 1, SharedWithPartner: true})
+	if err != nil {
+		t.Fatalf("Create: %v", err)
+	}
+	if _, err := meals.ReplaceIngredients(ctx, alice, m.ID, []service.MealIngredientInput{{IngredientID: milk.ID, Quantity: 100, Unit: "g"}}); err != nil {
+		t.Fatalf("ReplaceIngredients: %v", err)
+	}
+	first, err := meals.Copy(ctx, bob, m.ID)
+	if err != nil {
+		t.Fatalf("first Copy: %v", err)
+	}
+	again, err := meals.Copy(ctx, bob, m.ID)
+	if err != nil {
+		t.Fatalf("second Copy: %v", err)
+	}
+	if first.Ingredients[0].IngredientID == again.Ingredients[0].IngredientID {
+		t.Error("two copies share one duplicated ingredient, want one duplicate per copy operation")
+	}
+}
```

Modify `backend/internal/service/partners_test.go` (unified diff against the current file):

```diff
--- a/backend/internal/service/partners_test.go
+++ b/backend/internal/service/partners_test.go
@@ -455,3 +455,23 @@ func TestPartnersAcceptWaitsForTheUserLocksAndChecksAgainAfterThem(t *testing.T)
 		t.Errorf("Bob is in %d partnership rows (err %v), want 1", len(rows), err)
 	}
 }
+
+// partnersFor returns a Partners service over st for tests of the other
+// services that only need to link or unlink two users.
+func partnersFor(st *store.Store) *service.Partners {
+	return service.NewPartners(st, service.NewListEventHub(), time.Now)
+}
+
+// linkPartners makes a and b partners through the real invite flow.
+func linkPartners(t *testing.T, st *store.Store, a, b uuid.UUID) *service.Partners {
+	t.Helper()
+	p := partnersFor(st)
+	inv, err := p.Invite(context.Background(), a)
+	if err != nil {
+		t.Fatalf("Invite: %v", err)
+	}
+	if _, err := p.Accept(context.Background(), b, inv.Code); err != nil {
+		t.Fatalf("Accept: %v", err)
+	}
+	return p
+}
```

Create `backend/internal/service/meals_partner_edges_test.go`:

```go
package service_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/InzKazik/mealplanner/backend/internal/service"
)

// A partner's meal is read-only + copy. Scheduling it directly would put a
// row in the plan that points at someone else's meal, so it is refused like
// any meal the caller cannot see: copy it first.
func TestMealsAPartnersSharedMealCannotBeScheduledInThePlanDirectly(t *testing.T) {
	meals, _, st := newMealsFixture(t)
	plan := service.NewPlan(st, meals)
	ctx := context.Background()
	alice := newTestUser(t, st, "alice-edge2@example.com")
	bob := newTestUser(t, st, "bob-edge2@example.com")
	linkPartners(t, st, alice, bob)
	shared, err := meals.Create(ctx, alice, service.CreateMealInput{Name: "Shared Dinner", Servings: 1, SharedWithPartner: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	date := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	if _, err := plan.SetEntry(ctx, bob, date, "dinner", service.SetPlanEntryInput{MealID: shared.ID, Portion: 1}); !errors.Is(err, service.ErrPlanMealNotFound) {
		t.Errorf("SetEntry with the partner's shared meal: err = %v, want ErrPlanMealNotFound", err)
	}
	cp, err := meals.Copy(ctx, bob, shared.ID)
	if err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if _, err := plan.SetEntry(ctx, bob, date, "dinner", service.SetPlanEntryInput{MealID: cp.ID, Portion: 1}); err != nil {
		t.Errorf("SetEntry with Bob's own copy: %v", err)
	}
}

// Paging through the partner's meals with a small limit returns every shared
// meal exactly once, in order, and never one of the caller's own.
func TestMealsListPartnerPaginatesTheSharedMealsOnly(t *testing.T) {
	meals, _, st := newMealsFixture(t)
	ctx := context.Background()
	alice := newTestUser(t, st, "alice-edge3@example.com")
	bob := newTestUser(t, st, "bob-edge3@example.com")
	linkPartners(t, st, alice, bob)
	for name, shared := range map[string]bool{"Apple Pie": true, "Bagel": true, "Curry": true, "Dumplings": false} {
		if _, err := meals.Create(ctx, alice, service.CreateMealInput{Name: name, Servings: 1, SharedWithPartner: shared}); err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
	}
	if _, err := meals.Create(ctx, bob, service.CreateMealInput{Name: "Bob's Own", Servings: 1, SharedWithPartner: true}); err != nil {
		t.Fatalf("Create Bob's meal: %v", err)
	}

	var names []string
	var cursor *service.MealCursor
	for range 10 {
		page, err := meals.ListPartner(ctx, bob, service.ListMealsInput{Cursor: cursor, Limit: 1})
		if err != nil {
			t.Fatalf("ListPartner: %v", err)
		}
		for _, m := range page.Items {
			names = append(names, m.Name)
		}
		if page.NextCursor == nil {
			break
		}
		cursor = page.NextCursor
	}
	if want := []string{"Apple Pie", "Bagel", "Curry"}; !slices.Equal(names, want) {
		t.Errorf("paged partner meals = %v, want %v (each shared meal once, none unshared, none of Bob's own)", names, want)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `cd backend && go test ./internal/service/ -count=1`

Expected: FAIL, for the right reason:

```
internal/service/meals_test.go:699:9: got.OwnerID undefined (type service.Meal has no field or method OwnerID)
internal/service/meals_test.go:724:28: meals.ListPartner undefined (type *service.Meals has no field or method ListPartner)
```

- [ ] **Step 3: Implement**

Modify `backend/internal/store/queries/meals.sql` (unified diff against the current file):

```diff
--- a/backend/internal/store/queries/meals.sql
+++ b/backend/internal/store/queries/meals.sql
@@ -4,11 +4,16 @@ VALUES ($1, $2, $3, $4, $5)
 RETURNING *;
 
 -- name: GetMealForUser :one
--- Owner-only visibility for now: shared_with_partner has no effect on GET
--- until the partner plan adds the partnerships table and an active-partner
--- lookup. See "Not built yet" in backend/CLAUDE.md.
+-- The read predicate (spec §5): the caller's own meal, or one the caller's
+-- active partner has shared. partner_id is NULL when the caller has no
+-- partner (or when the caller wants owner-only access), which makes the second
+-- branch match nothing. Every write keeps the strict owner-only queries.
 SELECT * FROM meals
-WHERE id = sqlc.arg('id') AND owner_id = sqlc.arg('user_id');
+WHERE id = sqlc.arg('id')
+  AND (
+    owner_id = sqlc.arg('user_id')
+    OR (owner_id = sqlc.narg('partner_id')::uuid AND shared_with_partner)
+  );
 
 -- name: TouchMealForUser :one
 -- Bumps updated_at (via the meals_set_updated_at trigger) and, just as
@@ -21,8 +26,11 @@ WHERE id = sqlc.arg('id') AND owner_id = sqlc.arg('user_id')
 RETURNING *;
 
 -- name: ListMealsForUser :many
+-- shared_only is for GET partner/meals: user_id is then the partner's id and
+-- only what the partner has shared comes back.
 SELECT * FROM meals
 WHERE owner_id = sqlc.arg('user_id')
+  AND (NOT sqlc.arg('shared_only')::boolean OR shared_with_partner)
   AND (
     NOT sqlc.arg('has_cursor')::boolean
     OR name > sqlc.arg('cursor_name')::text
```

Modify `backend/internal/service/partners.go` (unified diff against the current file):

```diff
--- a/backend/internal/service/partners.go
+++ b/backend/internal/service/partners.go
@@ -259,6 +259,19 @@ func activePartnerID(ctx context.Context, q *sqlc.Queries, userID uuid.UUID, for
 	return id, nil
 }
 
+// partnerOrNil is activePartnerID for the read predicate: nil means "no
+// partner", so the predicate's partner branch matches nothing.
+func partnerOrNil(ctx context.Context, q *sqlc.Queries, userID uuid.UUID, forShare bool) (*uuid.UUID, error) {
+	id, err := activePartnerID(ctx, q, userID, forShare)
+	if errors.Is(err, ErrPartnerNotLinked) {
+		return nil, nil //nolint:nilnil // nil is the "no partner" value the visibility queries take
+	}
+	if err != nil {
+		return nil, err
+	}
+	return &id, nil
+}
+
 // requireUnlinked returns nil if userID has no active partner and linkedErr
 // if they do.
 func requireUnlinked(ctx context.Context, q *sqlc.Queries, userID uuid.UUID, linkedErr error) error {
```

Create `backend/internal/service/copy.go`:

```go
package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/InzKazik/mealplanner/backend/internal/store"
	"github.com/InzKazik/mealplanner/backend/internal/store/sqlc"
)

// ingredientCopier gives a copy of a partner's meal the ingredients it needs
// in the caller's library. A global (USDA) ingredient stays a shared
// reference. A custom ingredient is duplicated, with its nutrient rows, into
// the caller's library, once per copier: one copier serves one copy
// operation, so an ingredient used on several lines, or in several meals of
// one template copy, is duplicated a single time. Copying twice duplicates
// twice; there is no dedupe across operations.
//
// When the caller copies their own meal, from == to and remap is the
// identity, so an own copy keeps referencing the same ingredients.
type ingredientCopier struct {
	q      *sqlc.Queries
	from   uuid.UUID // owner of the meals being copied
	to     uuid.UUID // the caller, who will own the copies
	copied map[uuid.UUID]uuid.UUID
}

func newIngredientCopier(q *sqlc.Queries, from, to uuid.UUID) *ingredientCopier {
	return &ingredientCopier{q: q, from: from, to: to, copied: make(map[uuid.UUID]uuid.UUID)}
}

// remap returns the id a copied meal line should reference for the source
// ingredient id.
func (c *ingredientCopier) remap(ctx context.Context, id uuid.UUID) (uuid.UUID, error) {
	if c.from == c.to {
		return id, nil
	}
	if mapped, ok := c.copied[id]; ok {
		return mapped, nil
	}
	// Resolved with the source owner's visibility, exactly like the meal
	// itself is read: a partner never gets to browse the owner's ingredients,
	// only to receive the ones a shared meal uses.
	rows, err := c.q.GetIngredientsForUser(ctx, sqlc.GetIngredientsForUserParams{Ids: []uuid.UUID{id}, UserID: &c.from})
	if err != nil {
		return uuid.Nil, fmt.Errorf("get ingredient to copy: %w", err)
	}
	if len(rows) == 0 {
		return uuid.Nil, ErrMealIngredientNotFound
	}
	src := rows[0]
	if src.OwnerID == nil {
		c.copied[id] = id
		return id, nil
	}
	dup, err := c.q.CreateIngredient(ctx, sqlc.CreateIngredientParams{
		Name: src.Name, Category: src.Category, OwnerID: &c.to,
		GramsPerPiece: src.GramsPerPiece, DensityGPerMl: src.DensityGPerMl,
	})
	if store.IsForeignKeyViolation(err, "ingredients_owner_id_fkey") {
		return uuid.Nil, ErrNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("copy ingredient: %w", err)
	}
	nutrients, err := c.q.GetIngredientNutrients(ctx, []uuid.UUID{id})
	if err != nil {
		return uuid.Nil, fmt.Errorf("get nutrients to copy: %w", err)
	}
	for _, n := range nutrients {
		if err := c.q.UpsertIngredientNutrient(ctx, sqlc.UpsertIngredientNutrientParams{
			IngredientID: dup.ID, NutrientKey: n.NutrientKey, AmountPer100g: n.AmountPer100g,
		}); err != nil {
			return uuid.Nil, fmt.Errorf("copy nutrient: %w", err)
		}
	}
	c.copied[id] = dup.ID
	return dup.ID, nil
}

// copyMeal writes a copy of original owned by callerID, always private
// (shared_with_partner = false), with its ingredient lines remapped through
// ic. It returns the new meals row and its lines.
func copyMeal(ctx context.Context, q *sqlc.Queries, callerID uuid.UUID, original sqlc.Meal, ic *ingredientCopier) (sqlc.Meal, []sqlc.MealIngredient, error) {
	lines, err := q.GetMealIngredients(ctx, original.ID)
	if err != nil {
		return sqlc.Meal{}, nil, fmt.Errorf("get meal ingredients: %w", err)
	}
	row, err := q.CreateMeal(ctx, sqlc.CreateMealParams{
		OwnerID: callerID, Name: original.Name, Notes: original.Notes,
		Servings: original.Servings, SharedWithPartner: false,
	})
	if store.IsForeignKeyViolation(err, "meals_owner_id_fkey") {
		return sqlc.Meal{}, nil, ErrNotFound
	}
	if err != nil {
		return sqlc.Meal{}, nil, fmt.Errorf("create meal copy: %w", err)
	}
	inserted := make([]sqlc.MealIngredient, len(lines))
	for i, line := range lines {
		ingredientID, err := ic.remap(ctx, line.IngredientID)
		if err != nil {
			return sqlc.Meal{}, nil, err
		}
		ins, err := q.InsertMealIngredient(ctx, sqlc.InsertMealIngredientParams{
			MealID: row.ID, IngredientID: ingredientID, Quantity: line.Quantity, Unit: line.Unit, Position: line.Position,
		})
		if err != nil {
			return sqlc.Meal{}, nil, fmt.Errorf("copy meal ingredient: %w", err)
		}
		inserted[i] = ins
	}
	return row, inserted, nil
}
```

Modify `backend/internal/service/meals.go` (unified diff against the current file):

```diff
--- a/backend/internal/service/meals.go
+++ b/backend/internal/service/meals.go
@@ -15,9 +15,10 @@ import (
 
 // Errors returned by Meals. Handlers map them to problem responses.
 var (
-	// ErrMealNotFound means the meal does not exist, or is not owned by the
-	// caller. Partner visibility is not implemented yet (see the package doc
-	// comment on Meals below), so a meal is visible only to its owner.
+	// ErrMealNotFound means the meal does not exist, or the caller may not
+	// see it: it belongs to someone other than the caller and the caller's
+	// active partner, or to the partner but is not shared. Writes to a
+	// partner's meal, shared or not, are also ErrMealNotFound.
 	ErrMealNotFound = errors.New("meal not found")
 	// ErrMealIngredientNotFound means a meal_ingredients row references an
 	// ingredient that no longer exists or is no longer visible to the meal's
@@ -67,6 +68,7 @@ type MealIngredient struct {
 // ingredients has every key present at 0.
 type Meal struct {
 	ID                  uuid.UUID
+	OwnerID             uuid.UUID
 	Name                string
 	Notes               *string
 	Servings            float64
@@ -143,10 +145,9 @@ type ingredientReader interface {
 	GetIngredientNutrients(ctx context.Context, ingredientIds []uuid.UUID) ([]sqlc.IngredientNutrient, error)
 }
 
-// Meals implements meals built from ingredients, owned by a single user.
-// Partner sharing is not implemented: shared_with_partner is stored (it is
-// part of the data model), but every read here checks owner_id only. See the
-// "Not built yet" note in backend/CLAUDE.md.
+// Meals implements meals built from ingredients. A meal is owned by one user;
+// the owner's active partner may read it, and copy it, when shared_with_partner
+// is true (spec §3.6). Every write is owner-only.
 type Meals struct {
 	st *store.Store
 }
@@ -180,10 +181,27 @@ func (s *Meals) Create(ctx context.Context, ownerID uuid.UUID, in CreateMealInpu
 	return meal, nil
 }
 
-// Get returns a meal owned by ownerID, with its ingredients and computed
-// nutrition.
-func (s *Meals) Get(ctx context.Context, ownerID, id uuid.UUID) (Meal, error) {
-	row, err := s.st.GetMealForUser(ctx, sqlc.GetMealForUserParams{ID: id, UserID: ownerID})
+// Get returns a meal with its ingredients and computed nutrition: one owned
+// by callerID, or one their active partner has shared. Nutrition and
+// ingredient names are resolved with the meal owner's visibility, so a shared
+// meal that uses the partner's custom ingredients reads fine.
+func (s *Meals) Get(ctx context.Context, callerID, id uuid.UUID) (Meal, error) {
+	partnerID, err := partnerOrNil(ctx, s.st.Queries, callerID, false)
+	if err != nil {
+		return Meal{}, err
+	}
+	return s.get(ctx, callerID, partnerID, id)
+}
+
+// GetOwn is Get restricted to meals owned by ownerID. Plan uses it: an entry's
+// meal is always its owner's, so resolving a partner for every meal in a
+// plan range would only add queries.
+func (s *Meals) GetOwn(ctx context.Context, ownerID, id uuid.UUID) (Meal, error) {
+	return s.get(ctx, ownerID, nil, id)
+}
+
+func (s *Meals) get(ctx context.Context, callerID uuid.UUID, partnerID *uuid.UUID, id uuid.UUID) (Meal, error) {
+	row, err := s.st.GetMealForUser(ctx, sqlc.GetMealForUserParams{ID: id, UserID: callerID, PartnerID: partnerID})
 	if store.IsNotFound(err) {
 		return Meal{}, ErrMealNotFound
 	}
@@ -242,14 +260,27 @@ func (s *Meals) Delete(ctx context.Context, ownerID, id uuid.UUID) error {
 	return nil
 }
 
-// List returns a page of the caller's alphabetical meal list. It never
-// includes another user's meals, even ones shared_with_partner: true (no
-// partner visibility yet, see the Meals doc comment).
+// List returns a page of the caller's own meals, alphabetically. The
+// partner's shared meals are listed by ListPartner, never mixed in here.
 func (s *Meals) List(ctx context.Context, ownerID uuid.UUID, in ListMealsInput) (MealPage, error) {
+	return s.list(ctx, ownerID, false, in)
+}
+
+// ListPartner returns a page of the meals callerID's active partner has
+// shared, alphabetically. Without an active partner it is ErrPartnerNotLinked.
+func (s *Meals) ListPartner(ctx context.Context, callerID uuid.UUID, in ListMealsInput) (MealPage, error) {
+	partnerID, err := activePartnerID(ctx, s.st.Queries, callerID, false)
+	if err != nil {
+		return MealPage{}, err
+	}
+	return s.list(ctx, partnerID, true, in)
+}
+
+func (s *Meals) list(ctx context.Context, ownerID uuid.UUID, sharedOnly bool, in ListMealsInput) (MealPage, error) {
 	if in.Limit < 1 {
 		in.Limit = 1
 	}
-	params := sqlc.ListMealsForUserParams{UserID: ownerID, RowLimit: toRowLimit(in.Limit + 1)}
+	params := sqlc.ListMealsForUserParams{UserID: ownerID, SharedOnly: sharedOnly, RowLimit: toRowLimit(in.Limit + 1)}
 	if in.Cursor != nil {
 		params.HasCursor = true
 		params.CursorName = in.Cursor.Name
@@ -335,41 +366,27 @@ func (s *Meals) ReplaceIngredients(ctx context.Context, ownerID, id uuid.UUID, i
 
 // Copy creates a new meal owned by callerID, with the same name, notes,
 // servings and ingredients as the meal at id, and shared_with_partner always
-// false regardless of the original. callerID must own the original (partner
-// copy access is not implemented yet, see the Meals doc comment).
+// false regardless of the original. The original is one callerID owns or one
+// their active partner has shared. Copying a partner's meal duplicates each
+// distinct custom ingredient it uses into callerID's library (with its
+// nutrient rows) and leaves global ingredients as shared references.
 func (s *Meals) Copy(ctx context.Context, callerID, id uuid.UUID) (Meal, error) {
 	var meal Meal
 	err := s.st.InTx(ctx, func(q *sqlc.Queries) error {
-		original, err := q.GetMealForUser(ctx, sqlc.GetMealForUserParams{ID: id, UserID: callerID})
+		partnerID, err := partnerOrNil(ctx, q, callerID, false)
+		if err != nil {
+			return err
+		}
+		original, err := q.GetMealForUser(ctx, sqlc.GetMealForUserParams{ID: id, UserID: callerID, PartnerID: partnerID})
 		if store.IsNotFound(err) {
 			return ErrMealNotFound
 		}
 		if err != nil {
 			return fmt.Errorf("get meal: %w", err)
 		}
-		originalLines, err := q.GetMealIngredients(ctx, id)
+		copyRow, inserted, err := copyMeal(ctx, q, callerID, original, newIngredientCopier(q, original.OwnerID, callerID))
 		if err != nil {
-			return fmt.Errorf("get meal ingredients: %w", err)
-		}
-
-		copyRow, err := q.CreateMeal(ctx, sqlc.CreateMealParams{
-			OwnerID: callerID, Name: original.Name, Notes: original.Notes,
-			Servings: original.Servings, SharedWithPartner: false,
-		})
-		if err != nil {
-			return fmt.Errorf("create meal copy: %w", err)
-		}
-
-		inserted := make([]sqlc.MealIngredient, len(originalLines))
-		for i, line := range originalLines {
-			ins, err := q.InsertMealIngredient(ctx, sqlc.InsertMealIngredientParams{
-				MealID: copyRow.ID, IngredientID: line.IngredientID, Quantity: line.Quantity,
-				Unit: line.Unit, Position: line.Position,
-			})
-			if err != nil {
-				return fmt.Errorf("copy meal ingredient: %w", err)
-			}
-			inserted[i] = ins
+			return err
 		}
 		meal, err = s.toMeal(ctx, q, copyRow, inserted)
 		return err
@@ -454,7 +471,7 @@ func (s *Meals) toMeal(ctx context.Context, r ingredientReader, row sqlc.Meal, m
 	}
 
 	return Meal{
-		ID: row.ID, Name: row.Name, Notes: row.Notes, Servings: row.Servings, SharedWithPartner: row.SharedWithPartner,
+		ID: row.ID, OwnerID: row.OwnerID, Name: row.Name, Notes: row.Notes, Servings: row.Servings, SharedWithPartner: row.SharedWithPartner,
 		Ingredients: items, NutritionPerServing: perServing, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
 	}, nil
 }
```

Modify `backend/internal/service/plan.go` (unified diff against the current file):

```diff
--- a/backend/internal/service/plan.go
+++ b/backend/internal/service/plan.go
@@ -84,7 +84,7 @@ type SetPlanEntryInput struct {
 // Plan implements the calendar: which meal is scheduled for which date and
 // slot, and the nutrition totals that follow from it. Unlike every other
 // service in this package, it depends on Meals rather than reading meal rows
-// via store directly — specifically to reuse Meals.Get's nutrition
+// via store directly — specifically to reuse Meals.GetOwn's nutrition
 // computation instead of reimplementing unit conversion and
 // null-propagation a third time. See the diets-and-plan plan's Global
 // Constraints for the reasoning.
@@ -127,7 +127,7 @@ func (s *Plan) GetRange(ctx context.Context, ownerID uuid.UUID, from, to time.Ti
 	mealIDs := uniqueUUIDs(rows, func(r sqlc.PlanEntry) uuid.UUID { return r.MealID })
 	mealsByID := make(map[uuid.UUID]Meal, len(mealIDs))
 	for _, id := range mealIDs {
-		// s.meals.Get can return ErrMealNotFound here only from a race: a
+		// s.meals.GetOwn can return ErrMealNotFound here only from a race: a
 		// meal referenced by a plan_entries row in this range was deleted
 		// between the read above and this lookup (deleting an in-use meal
 		// is normally blocked by plan_entries_meal_id_fkey, but this read
@@ -136,7 +136,7 @@ func (s *Plan) GetRange(ctx context.Context, ownerID uuid.UUID, from, to time.Ti
 		// mistake (400), a GET returning 400 because of someone else's
 		// concurrent write would be confusing, so this is left as a plain
 		// wrapped error (500) rather than translated to ErrPlanMealNotFound.
-		m, err := s.meals.Get(ctx, ownerID, id)
+		m, err := s.meals.GetOwn(ctx, ownerID, id)
 		if err != nil {
 			return PlanRange{}, fmt.Errorf("get meal %s: %w", id, err)
 		}
```

- [ ] **Step 4: Regenerate**

Run `make generate` from the repo root. It rewrites `backend/internal/store/sqlc/`. Commit the output and never edit it by hand. `make check-generated` fails if it is stale.

- [ ] **Step 5: Run the tests and see them pass**

Run: `cd backend && go test ./internal/service/ -count=1`

Expected: PASS (`ok  	github.com/InzKazik/mealplanner/backend/internal/service`).

- [ ] **Step 6: Commit**

```bash
git add backend/internal/service/copy.go backend/internal/service/meals.go backend/internal/service/meals_partner_edges_test.go backend/internal/service/meals_test.go backend/internal/service/partners.go backend/internal/service/partners_test.go backend/internal/service/plan.go backend/internal/store/queries/meals.sql backend/internal/store/sqlc/meals.sql.go backend/internal/store/sqlc/partnerships.sql.go
git commit -m "feat(backend): let a partner read and copy shared meals"
```

### Task 5: Diet templates: partner read, copy and the partner listing

**Files:**
- Modify: `backend/internal/store/queries/diet_templates.sql`
- Modify: `backend/internal/service/diet_templates.go`
- Modify: `backend/internal/service/diet_templates_test.go`
- Generated: `backend/internal/store/sqlc/diet_templates.sql.go`

**Interfaces:**
- Consumes: `partnerOrNil`, `activePartnerID` (Tasks 3, 4); `ingredientCopier`, `newIngredientCopier`, `copyMeal` (Task 4).
- Produces: `DietTemplate.OwnerID`; `(*DietTemplates).Get` (read predicate), `List` (own only), `ListPartner(ctx, callerID, in ListDietTemplatesInput) (DietTemplatePage, error)`, `Copy`; unexported `copiedMeals(ctx, q, callerID, original sqlc.DietTemplate, slots []sqlc.TemplateSlot) (func(uuid.UUID) uuid.UUID, error)`; sqlc `GetDietTemplateForUserParams.PartnerID`, `ListDietTemplatesForUserParams.SharedOnly`.

Sharing a template shares what its slots show (spec §2): a partner sees each slot's `meal_id` and meal name even when the meal itself is not shared, and `Meals.Get` on that meal stays `404`. `toTemplate` already resolves slot meals with the template owner's id, so reading needs no change. `Apply` keeps calling `GetDietTemplateForUser` without a partner, so applying a partner's template is `404`: copy it first. An own copy still points at the same meals; only a partner copy copies meals, each distinct one once.

- [ ] **Step 1: Write the failing tests**

Modify `backend/internal/service/diet_templates_test.go` (unified diff against the current file):

```diff
--- a/backend/internal/service/diet_templates_test.go
+++ b/backend/internal/service/diet_templates_test.go
@@ -554,3 +554,187 @@ func TestDietTemplatesApplySnackSlotsNeverConflictAndAlwaysAccumulate(t *testing
 		t.Errorf("snack entries after overwrite apply = %d, want 3 (overwrite must not remove existing snacks)", got)
 	}
 }
+
+// sharedTemplate creates a template owned by owner with the given slots and
+// shared_with_partner set to shared.
+func sharedTemplate(t *testing.T, tpls *service.DietTemplates, owner uuid.UUID, name string, shared bool, slots []service.TemplateSlotInput) service.DietTemplate {
+	t.Helper()
+	ctx := context.Background()
+	tpl, err := tpls.Create(ctx, owner, service.CreateDietTemplateInput{Name: name, DayCount: 2, SharedWithPartner: shared})
+	if err != nil {
+		t.Fatalf("Create template %q: %v", name, err)
+	}
+	tpl, err = tpls.ReplaceSlots(ctx, owner, tpl.ID, slots)
+	if err != nil {
+		t.Fatalf("ReplaceSlots %q: %v", name, err)
+	}
+	return tpl
+}
+
+func TestDietTemplatesPartnerReadsOnlySharedTemplatesAndNeverWritesThem(t *testing.T) {
+	tpls, meals, _, st := newDietTemplatesFixture(t)
+	ctx := context.Background()
+	alice := newTestUser(t, st, "alice30@example.com")
+	bob := newTestUser(t, st, "bob30@example.com")
+	carol := newTestUser(t, st, "carol30@example.com")
+	partners := linkPartners(t, st, alice, bob)
+
+	toast := mustCreateMeal(t, meals, alice, "Toast") // not shared on its own
+	shared := sharedTemplate(t, tpls, alice, "Shared Week", true, []service.TemplateSlotInput{
+		{DayIndex: 0, Slot: "breakfast", MealID: toast.ID, Portion: 1},
+	})
+	private := sharedTemplate(t, tpls, alice, "Private Week", false, []service.TemplateSlotInput{
+		{DayIndex: 0, Slot: "breakfast", MealID: toast.ID, Portion: 1},
+	})
+
+	got, err := tpls.Get(ctx, bob, shared.ID)
+	if err != nil {
+		t.Fatalf("partner Get of a shared template: %v", err)
+	}
+	if got.OwnerID != alice || len(got.Slots) != 1 || got.Slots[0].MealID != toast.ID || got.Slots[0].MealName != "Toast" {
+		t.Errorf("partner sees %+v, want Alice's template whose slot shows the meal id and name", got)
+	}
+	// Sharing the template shares what its slots show, not the meal itself.
+	if _, err := meals.Get(ctx, bob, toast.ID); !errors.Is(err, service.ErrMealNotFound) {
+		t.Errorf("partner Get of the unshared meal in a shared template: err = %v, want ErrMealNotFound", err)
+	}
+
+	newName := "Hijacked"
+	for label, err := range map[string]error{
+		"Get of an unshared template": errOf(tpls.Get(ctx, bob, private.ID)),
+		"Get by a stranger":           errOf(tpls.Get(ctx, carol, shared.ID)),
+		"Update":                      errOf(tpls.Update(ctx, bob, shared.ID, service.UpdateDietTemplateInput{Name: &newName})),
+		"Delete":                      tpls.Delete(ctx, bob, shared.ID),
+		"ReplaceSlots":                errOf(tpls.ReplaceSlots(ctx, bob, shared.ID, nil)),
+		"Apply":                       errOf(tpls.Apply(ctx, bob, shared.ID, service.ApplyTemplateInput{StartDate: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)})),
+	} {
+		if !errors.Is(err, service.ErrDietTemplateNotFound) {
+			t.Errorf("%s: err = %v, want ErrDietTemplateNotFound", label, err)
+		}
+	}
+
+	if own, err := tpls.List(ctx, bob, service.ListDietTemplatesInput{Limit: 10}); err != nil || len(own.Items) != 0 {
+		t.Errorf("the partner's own List = %+v (err %v), want empty: shared templates are not mixed in", own.Items, err)
+	}
+	page, err := tpls.ListPartner(ctx, bob, service.ListDietTemplatesInput{Limit: 10})
+	if err != nil || len(page.Items) != 1 || page.Items[0].ID != shared.ID {
+		t.Errorf("ListPartner = %+v (err %v), want only the shared template", page.Items, err)
+	}
+	if _, err := tpls.ListPartner(ctx, carol, service.ListDietTemplatesInput{Limit: 10}); !errors.Is(err, service.ErrPartnerNotLinked) {
+		t.Errorf("ListPartner without a partner: err = %v, want ErrPartnerNotLinked", err)
+	}
+
+	if err := partners.Unlink(ctx, bob); err != nil {
+		t.Fatalf("Unlink: %v", err)
+	}
+	if _, err := tpls.Get(ctx, bob, shared.ID); !errors.Is(err, service.ErrDietTemplateNotFound) {
+		t.Errorf("partner Get after unlinking: err = %v, want ErrDietTemplateNotFound", err)
+	}
+}
+
+func TestDietTemplatesCopyOfAPartnersTemplateCopiesItsMealsOnce(t *testing.T) {
+	tpls, meals, ing, st := newDietTemplatesFixture(t)
+	ctx := context.Background()
+	alice := newTestUser(t, st, "alice31@example.com")
+	bob := newTestUser(t, st, "bob31@example.com")
+	partners := linkPartners(t, st, alice, bob)
+
+	flour := mustCreateIngredient(t, ing, alice, service.CreateIngredientInput{
+		Name: "Alice's Flour", Category: "grains_bread", Nutrients: map[string]float64{service.NutrientCalories: 360},
+	})
+	makeMeal := func(name string) service.Meal {
+		t.Helper()
+		m := mustCreateMeal(t, meals, alice, name)
+		m, err := meals.ReplaceIngredients(ctx, alice, m.ID, []service.MealIngredientInput{{IngredientID: flour.ID, Quantity: 100, Unit: "g"}})
+		if err != nil {
+			t.Fatalf("ReplaceIngredients %q: %v", name, err)
+		}
+		return m
+	}
+	pancakes, bread := makeMeal("Pancakes"), makeMeal("Bread")
+	original := sharedTemplate(t, tpls, alice, "Baking Week", true, []service.TemplateSlotInput{
+		{DayIndex: 0, Slot: "breakfast", MealID: pancakes.ID, Portion: 1},
+		{DayIndex: 0, Slot: "dinner", MealID: bread.ID, Portion: 1.5},
+		{DayIndex: 1, Slot: "breakfast", MealID: pancakes.ID, Portion: 2},
+	})
+
+	cp, err := tpls.Copy(ctx, bob, original.ID)
+	if err != nil {
+		t.Fatalf("partner Copy: %v", err)
+	}
+	if cp.OwnerID != bob || cp.SharedWithPartner || cp.Name != "Baking Week" || cp.DayCount != 2 || len(cp.Slots) != 3 {
+		t.Fatalf("copy = %+v, want Bob's private 2-day copy with 3 slots", cp)
+	}
+	// Slots come back ordered by day, then meal time: breakfast d0, dinner d0, breakfast d1.
+	first, second, third := cp.Slots[0], cp.Slots[1], cp.Slots[2]
+	if first.MealID == pancakes.ID || second.MealID == bread.ID {
+		t.Errorf("copy slots still point at Alice's meals: %+v", cp.Slots)
+	}
+	if first.MealID != third.MealID {
+		t.Errorf("the pancakes fill two slots but were copied to %v and %v, want one copy", first.MealID, third.MealID)
+	}
+	if first.MealName != "Pancakes" || second.MealName != "Bread" || second.Portion != 1.5 || third.Portion != 2 {
+		t.Errorf("copy slots = %+v, want names and portions carried over", cp.Slots)
+	}
+
+	// Bob owns exactly two new private meals, sharing one duplicated ingredient.
+	page, err := meals.List(ctx, bob, service.ListMealsInput{Limit: 10})
+	if err != nil || len(page.Items) != 2 {
+		t.Fatalf("Bob's meals = %+v (err %v), want exactly the 2 copies", page.Items, err)
+	}
+	var flourIDs []uuid.UUID
+	for _, item := range page.Items {
+		if item.SharedWithPartner {
+			t.Errorf("copied meal %q is shared, want private", item.Name)
+		}
+		m, err := meals.Get(ctx, bob, item.ID)
+		if err != nil || len(m.Ingredients) != 1 || m.Ingredients[0].IngredientID == flour.ID {
+			t.Fatalf("copied meal %q = %+v (err %v), want one line on a duplicated flour", item.Name, m.Ingredients, err)
+		}
+		flourIDs = append(flourIDs, m.Ingredients[0].IngredientID)
+	}
+	if flourIDs[0] != flourIDs[1] {
+		t.Errorf("the two copied meals use flour %v and %v, want one duplicate shared across the template copy", flourIDs[0], flourIDs[1])
+	}
+
+	// Bob can plan with it: Apply needs meals Bob owns.
+	written, err := tpls.Apply(ctx, bob, cp.ID, service.ApplyTemplateInput{StartDate: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)})
+	if err != nil || written != 3 {
+		t.Errorf("Apply of the copy = %d entries (err %v), want 3", written, err)
+	}
+
+	// Independent of the original: Alice can delete her template and meals.
+	if err := partners.Unlink(ctx, alice); err != nil {
+		t.Fatalf("Unlink: %v", err)
+	}
+	if err := tpls.Delete(ctx, alice, original.ID); err != nil {
+		t.Fatalf("delete original template: %v", err)
+	}
+	if err := meals.Delete(ctx, alice, pancakes.ID); err != nil {
+		t.Fatalf("delete original meal: %v", err)
+	}
+	if again, err := tpls.Get(ctx, bob, cp.ID); err != nil || len(again.Slots) != 3 {
+		t.Errorf("copy after the original was deleted = %+v (err %v), want it untouched", again.Slots, err)
+	}
+}
+
+func TestDietTemplatesCopyOfAnOwnTemplateKeepsItsMeals(t *testing.T) {
+	tpls, meals, _, st := newDietTemplatesFixture(t)
+	ctx := context.Background()
+	owner := newTestUser(t, st, "owner32@example.com")
+	meal := mustCreateMeal(t, meals, owner, "Toast")
+	original := sharedTemplate(t, tpls, owner, "Mine", true, []service.TemplateSlotInput{
+		{DayIndex: 0, Slot: "breakfast", MealID: meal.ID, Portion: 1},
+	})
+
+	cp, err := tpls.Copy(ctx, owner, original.ID)
+	if err != nil {
+		t.Fatalf("Copy: %v", err)
+	}
+	if len(cp.Slots) != 1 || cp.Slots[0].MealID != meal.ID {
+		t.Errorf("own copy slots = %+v, want the same meal %v", cp.Slots, meal.ID)
+	}
+	if page, err := meals.List(ctx, owner, service.ListMealsInput{Limit: 10}); err != nil || len(page.Items) != 1 {
+		t.Errorf("meals after an own copy = %+v (err %v), want still 1", page.Items, err)
+	}
+}
```

- [ ] **Step 2: Run them to see them fail**

Run: `cd backend && go test ./internal/service/ -count=1`

Expected: FAIL, for the right reason:

```
internal/service/diet_templates_test.go:594:9: got.OwnerID undefined (type service.DietTemplate has no field or method OwnerID)
internal/service/diet_templates_test.go:619:20: tpls.ListPartner undefined (type *service.DietTemplates has no field or method ListPartner)
```

- [ ] **Step 3: Implement**

Modify `backend/internal/store/queries/diet_templates.sql` (unified diff against the current file):

```diff
--- a/backend/internal/store/queries/diet_templates.sql
+++ b/backend/internal/store/queries/diet_templates.sql
@@ -4,11 +4,17 @@ VALUES ($1, $2, $3, $4)
 RETURNING *;
 
 -- name: GetDietTemplateForUser :one
--- Owner-only visibility for now: shared_with_partner has no effect on GET
--- until the partner plan adds the partnerships table and an active-partner
--- lookup. See "Not built yet" in backend/CLAUDE.md.
+-- The read predicate (spec §5): the caller's own template, or one the
+-- caller's active partner has shared. partner_id is NULL when the caller has
+-- no partner (or wants owner-only access, as Apply does), which makes the
+-- second branch match nothing. Every write keeps the strict owner-only
+-- queries.
 SELECT * FROM diet_templates
-WHERE id = sqlc.arg('id') AND owner_id = sqlc.arg('user_id');
+WHERE id = sqlc.arg('id')
+  AND (
+    owner_id = sqlc.arg('user_id')
+    OR (owner_id = sqlc.narg('partner_id')::uuid AND shared_with_partner)
+  );
 
 -- name: TouchDietTemplateForUser :one
 -- Bumps updated_at (via the diet_templates_set_updated_at trigger) and, just
@@ -22,8 +28,11 @@ WHERE id = sqlc.arg('id') AND owner_id = sqlc.arg('user_id')
 RETURNING *;
 
 -- name: ListDietTemplatesForUser :many
+-- shared_only is for GET partner/diet-templates: user_id is then the partner's
+-- id and only what the partner has shared comes back.
 SELECT * FROM diet_templates
 WHERE owner_id = sqlc.arg('user_id')
+  AND (NOT sqlc.arg('shared_only')::boolean OR shared_with_partner)
   AND (
     NOT sqlc.arg('has_cursor')::boolean
     OR name > sqlc.arg('cursor_name')::text
```

Modify `backend/internal/service/diet_templates.go` (unified diff against the current file):

```diff
--- a/backend/internal/service/diet_templates.go
+++ b/backend/internal/service/diet_templates.go
@@ -16,6 +16,11 @@ import (
 
 // Errors returned by DietTemplates. Handlers map them to problem responses.
 var (
+	// ErrDietTemplateNotFound means the template does not exist, or the
+	// caller may not see it: it belongs to someone other than the caller and
+	// the caller's active partner, or to the partner but is not shared.
+	// Writes to a partner's template, shared or not, are also
+	// ErrDietTemplateNotFound.
 	ErrDietTemplateNotFound = errors.New("diet template not found")
 	// ErrDayIndexOutOfRange means a slot's day_index is >= the template's
 	// day_count. There is no database constraint for this (a CHECK cannot
@@ -46,6 +51,7 @@ type TemplateSlot struct {
 // DietTemplate is a reusable meal schedule.
 type DietTemplate struct {
 	ID                uuid.UUID
+	OwnerID           uuid.UUID
 	Name              string
 	DayCount          int
 	SharedWithPartner bool
@@ -114,10 +120,15 @@ type ApplyTemplateInput struct {
 	Overwrite bool
 }
 
-// DietTemplates implements reusable meal-schedule templates, owned by a
-// single user. Partner sharing is not implemented: shared_with_partner is
-// stored, but every read here checks owner_id only. See the "Not built yet"
-// note in backend/CLAUDE.md.
+// DietTemplates implements reusable meal-schedule templates. A template is
+// owned by one user; the owner's active partner may read it, and copy it,
+// when shared_with_partner is true (spec §3.6). Every write, and Apply, is
+// owner-only.
+//
+// Sharing a template shares what its slots show: each slot's meal_id and meal
+// name, even when the meal itself is not shared. The meal's own detail stays
+// hidden (Meals.Get is 404 for it), and a copy of the template carries the
+// meals along.
 type DietTemplates struct {
 	st *store.Store
 }
@@ -148,9 +159,14 @@ func (s *DietTemplates) Create(ctx context.Context, ownerID uuid.UUID, in Create
 	return tpl, nil
 }
 
-// Get returns a template owned by ownerID, with its slots.
-func (s *DietTemplates) Get(ctx context.Context, ownerID, id uuid.UUID) (DietTemplate, error) {
-	row, err := s.st.GetDietTemplateForUser(ctx, sqlc.GetDietTemplateForUserParams{ID: id, UserID: ownerID})
+// Get returns a template with its slots: one owned by callerID, or one their
+// active partner has shared.
+func (s *DietTemplates) Get(ctx context.Context, callerID, id uuid.UUID) (DietTemplate, error) {
+	partnerID, err := partnerOrNil(ctx, s.st.Queries, callerID, false)
+	if err != nil {
+		return DietTemplate{}, err
+	}
+	row, err := s.st.GetDietTemplateForUser(ctx, sqlc.GetDietTemplateForUserParams{ID: id, UserID: callerID, PartnerID: partnerID})
 	if store.IsNotFound(err) {
 		return DietTemplate{}, ErrDietTemplateNotFound
 	}
@@ -202,12 +218,27 @@ func (s *DietTemplates) Delete(ctx context.Context, ownerID, id uuid.UUID) error
 	return nil
 }
 
-// List returns a page of the caller's alphabetical template list.
+// List returns a page of the caller's own templates, alphabetically. The
+// partner's shared templates are listed by ListPartner, never mixed in here.
 func (s *DietTemplates) List(ctx context.Context, ownerID uuid.UUID, in ListDietTemplatesInput) (DietTemplatePage, error) {
+	return s.list(ctx, ownerID, false, in)
+}
+
+// ListPartner returns a page of the templates callerID's active partner has
+// shared, alphabetically. Without an active partner it is ErrPartnerNotLinked.
+func (s *DietTemplates) ListPartner(ctx context.Context, callerID uuid.UUID, in ListDietTemplatesInput) (DietTemplatePage, error) {
+	partnerID, err := activePartnerID(ctx, s.st.Queries, callerID, false)
+	if err != nil {
+		return DietTemplatePage{}, err
+	}
+	return s.list(ctx, partnerID, true, in)
+}
+
+func (s *DietTemplates) list(ctx context.Context, ownerID uuid.UUID, sharedOnly bool, in ListDietTemplatesInput) (DietTemplatePage, error) {
 	if in.Limit < 1 {
 		in.Limit = 1
 	}
-	params := sqlc.ListDietTemplatesForUserParams{UserID: ownerID, RowLimit: toRowLimit(in.Limit + 1)}
+	params := sqlc.ListDietTemplatesForUserParams{UserID: ownerID, SharedOnly: sharedOnly, RowLimit: toRowLimit(in.Limit + 1)}
 	if in.Cursor != nil {
 		params.HasCursor = true
 		params.CursorName = in.Cursor.Name
@@ -314,11 +345,24 @@ func (s *DietTemplates) ReplaceSlots(ctx context.Context, ownerID, id uuid.UUID,
 
 // Copy creates a new template owned by callerID, with the same name,
 // day_count and slots as the template at id, and shared_with_partner always
-// false regardless of the original. callerID must own the original.
+// false regardless of the original. The original is one callerID owns or one
+// their active partner has shared.
+//
+// Copying an own template keeps its slots pointing at the same meals.
+// Copying a partner's template copies the meals too, since its slots
+// reference the partner's meals: each distinct meal is copied once, even when
+// it fills several slots, with the same ingredient rules as Meals.Copy (one
+// duplicate per distinct custom ingredient across the whole template, global
+// ingredients shared). The meals are read through the template, so a meal the
+// partner did not share on its own copies fine.
 func (s *DietTemplates) Copy(ctx context.Context, callerID, id uuid.UUID) (DietTemplate, error) {
 	var tpl DietTemplate
 	err := s.st.InTx(ctx, func(q *sqlc.Queries) error {
-		original, err := q.GetDietTemplateForUser(ctx, sqlc.GetDietTemplateForUserParams{ID: id, UserID: callerID})
+		partnerID, err := partnerOrNil(ctx, q, callerID, false)
+		if err != nil {
+			return err
+		}
+		original, err := q.GetDietTemplateForUser(ctx, sqlc.GetDietTemplateForUserParams{ID: id, UserID: callerID, PartnerID: partnerID})
 		if store.IsNotFound(err) {
 			return ErrDietTemplateNotFound
 		}
@@ -329,17 +373,24 @@ func (s *DietTemplates) Copy(ctx context.Context, callerID, id uuid.UUID) (DietT
 		if err != nil {
 			return fmt.Errorf("get template slots: %w", err)
 		}
+		mealFor, err := copiedMeals(ctx, q, callerID, original, originalSlots)
+		if err != nil {
+			return err
+		}
 
 		copyRow, err := q.CreateDietTemplate(ctx, sqlc.CreateDietTemplateParams{
 			OwnerID: callerID, Name: original.Name, DayCount: original.DayCount, SharedWithPartner: false,
 		})
+		if store.IsForeignKeyViolation(err, "diet_templates_owner_id_fkey") {
+			return ErrNotFound
+		}
 		if err != nil {
 			return fmt.Errorf("create diet template copy: %w", err)
 		}
 		inserted := make([]sqlc.TemplateSlot, len(originalSlots))
 		for i, sl := range originalSlots {
 			ins, err := q.InsertTemplateSlot(ctx, sqlc.InsertTemplateSlotParams{
-				TemplateID: copyRow.ID, DayIndex: sl.DayIndex, Slot: sl.Slot, MealID: sl.MealID, Portion: sl.Portion,
+				TemplateID: copyRow.ID, DayIndex: sl.DayIndex, Slot: sl.Slot, MealID: mealFor(sl.MealID), Portion: sl.Portion,
 			})
 			if err != nil {
 				return fmt.Errorf("copy template slot: %w", err)
@@ -355,6 +406,38 @@ func (s *DietTemplates) Copy(ctx context.Context, callerID, id uuid.UUID) (DietT
 	return tpl, nil
 }
 
+// copiedMeals returns the meal id each of a template copy's slots should
+// reference, given the original slot's meal id: the same id for an own
+// template, a fresh private copy (made here, once per distinct meal) for a
+// partner's.
+func copiedMeals(ctx context.Context, q *sqlc.Queries, callerID uuid.UUID, original sqlc.DietTemplate, slots []sqlc.TemplateSlot) (func(uuid.UUID) uuid.UUID, error) {
+	if original.OwnerID == callerID {
+		return func(id uuid.UUID) uuid.UUID { return id }, nil
+	}
+	mealIDs := uniqueUUIDs(slots, func(r sqlc.TemplateSlot) uuid.UUID { return r.MealID })
+	rows, err := q.GetMealsForUser(ctx, sqlc.GetMealsForUserParams{Ids: mealIDs, UserID: original.OwnerID})
+	if err != nil {
+		return nil, fmt.Errorf("get meals to copy: %w", err)
+	}
+	if len(rows) != len(mealIDs) {
+		return nil, ErrTemplateMealNotFound
+	}
+	byID := make(map[uuid.UUID]sqlc.Meal, len(rows))
+	for _, m := range rows {
+		byID[m.ID] = m
+	}
+	ic := newIngredientCopier(q, original.OwnerID, callerID)
+	copies := make(map[uuid.UUID]uuid.UUID, len(mealIDs))
+	for _, id := range mealIDs {
+		row, _, err := copyMeal(ctx, q, callerID, byID[id], ic)
+		if err != nil {
+			return nil, err
+		}
+		copies[id] = row.ID
+	}
+	return func(id uuid.UUID) uuid.UUID { return copies[id] }, nil
+}
+
 // Apply copies the template's slots into plan_entries starting at
 // startDate: day_index 0 lands on startDate, day_index 1 on startDate+1,
 // and so on. Without overwrite, it fails with ErrPlanConflict (writing
@@ -472,7 +555,7 @@ func (s *DietTemplates) toTemplate(ctx context.Context, q *sqlc.Queries, row sql
 		}
 	}
 	return DietTemplate{
-		ID: row.ID, Name: row.Name, DayCount: int(row.DayCount), SharedWithPartner: row.SharedWithPartner,
+		ID: row.ID, OwnerID: row.OwnerID, Name: row.Name, DayCount: int(row.DayCount), SharedWithPartner: row.SharedWithPartner,
 		Slots: slots, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
 	}, nil
 }
```

- [ ] **Step 4: Regenerate**

Run `make generate` from the repo root. It rewrites `backend/internal/store/sqlc/`. Commit the output and never edit it by hand. `make check-generated` fails if it is stale.

- [ ] **Step 5: Run the tests and see them pass**

Run: `cd backend && go test ./internal/service/ -count=1`

Expected: PASS (`ok  	github.com/InzKazik/mealplanner/backend/internal/service`).

- [ ] **Step 6: Commit**

```bash
git add backend/internal/service/diet_templates.go backend/internal/service/diet_templates_test.go backend/internal/store/queries/diet_templates.sql backend/internal/store/sqlc/diet_templates.sql.go
git commit -m "feat(backend): let a partner read and copy shared diet templates"
```

### Task 6: Shopping lists: partner edits, partner listing, closing streams

**Files:**
- Modify: `backend/internal/store/queries/shopping_lists.sql`
- Modify: `backend/internal/service/shopping_lists.go`
- Modify: `backend/internal/service/shopping_lists_test.go`
- Modify: `backend/internal/service/partners_test.go` (split a helper)
- Create: `backend/internal/service/shopping_lists_partner_edges_test.go`
- Generated: `backend/internal/store/sqlc/shopping_lists.sql.go`

**Interfaces:**
- Consumes: `partnerOrNil`, `activePartnerID` (Tasks 3, 4); `ListEventHub.Subscribe(listID, watcherID, ownerID)` and the close methods (Task 2); `Partners.Unlink` (Task 3).
- Produces: `ShoppingList.OwnerID`; `(*ShoppingLists).Get` (read predicate), `List` (own only), `ListPartner(ctx, callerID, in ListShoppingListsInput) (ShoppingListPage, error)`; `AddItem`, `UpdateItem`, `DeleteItem` and `Subscribe` take the caller (owner or partner); `Update` closes non-owner streams when sharing is turned off; sqlc params gain `PartnerID` on `GetShoppingListForUser`, `TouchShoppingListForUser`, `GetShoppingItemForUserForUpdate`, `DeleteShoppingItemForUser` and `SharedOnly` on `ListShoppingListsForUser`; test helpers `linkWith`, `sharedListFixture`, `newSharedListFixture`, `errOf2`.

Item writes (`AddItem`, `UpdateItem`, `DeleteItem`) resolve the partner with the partnership row taken `FOR SHARE`, so an unlink's `DELETE` waits for them and an edit that starts afterwards finds no partner. `Subscribe` looks the list up (to learn its owner), registers with the hub, then re-checks access under `FOR SHARE`. `SetShoppingListSourceForUser`, `UpdateShoppingList` and `DeleteShoppingList` stay owner-only, so rename, delete, sharing and regenerate are `404` for a partner, and regenerate keeps reading the owner's `plan_entries`. `TestShoppingListsItemWritesWaitForAnUnlinkInFlightAndThenFindNoPartner` and `TestShoppingListsSubscribeRegistersBeforeItsFinalAccessCheck` are deterministic (they hold the partnership row in an open transaction, as an unlink does) and were each shown to fail when the `FOR SHARE`, the re-check, or the register-first order is removed.

- [ ] **Step 1: Write the failing tests**

Modify `backend/internal/service/shopping_lists_test.go` (unified diff against the current file):

```diff
--- a/backend/internal/service/shopping_lists_test.go
+++ b/backend/internal/service/shopping_lists_test.go
@@ -519,3 +519,361 @@ func TestShoppingListsGenerateValidatesTheRangeAndTheListsOwner(t *testing.T) {
 		t.Errorf("92-day range with no plan entries = %+v, %v, %v; want a new empty list with the default name", empty, created, err)
 	}
 }
+
+// sharedListFixture is a shopping lists service with two linked users, alice
+// (the owner) and bob (the partner), one stranger, and a Partners service that
+// shares the lists' event hub, so an unlink closes streams.
+type sharedListFixture struct {
+	shoppingFixture
+	partners             *service.Partners
+	alice, bob, stranger uuid.UUID
+}
+
+func newSharedListFixture(t *testing.T, tag string) sharedListFixture {
+	t.Helper()
+	f := newShoppingListsFixture(t)
+	alice := newTestUser(t, f.st, "alice-"+tag+"@example.com")
+	bob := newTestUser(t, f.st, "bob-"+tag+"@example.com")
+	stranger := newTestUser(t, f.st, "stranger-"+tag+"@example.com")
+	partners := service.NewPartners(f.st, f.events, time.Now)
+	linkWith(t, partners, alice, bob)
+	return sharedListFixture{shoppingFixture: f, partners: partners, alice: alice, bob: bob, stranger: stranger}
+}
+
+func (f sharedListFixture) createList(t *testing.T, owner uuid.UUID, name string, shared bool) service.ShoppingList {
+	t.Helper()
+	list, err := f.lists.Create(context.Background(), owner, service.CreateShoppingListInput{Name: name, SharedWithPartner: shared})
+	if err != nil {
+		t.Fatalf("Create %q: %v", name, err)
+	}
+	return list
+}
+
+func TestShoppingListsPartnerEditsItemsOfASharedListButNotTheListItself(t *testing.T) {
+	f := newSharedListFixture(t, "s1")
+	ctx := context.Background()
+	shared := f.createList(t, f.alice, "Shared", true)
+	private := f.createList(t, f.alice, "Private", false)
+	milk, err := f.lists.AddItem(ctx, f.alice, shared.ID, service.CreateShoppingItemInput{Name: "Milk"})
+	if err != nil {
+		t.Fatalf("AddItem: %v", err)
+	}
+
+	got, err := f.lists.Get(ctx, f.bob, shared.ID)
+	if err != nil || got.OwnerID != f.alice || len(got.Items) != 1 {
+		t.Fatalf("partner Get = %+v (err %v), want Alice's shared list with its item", got, err)
+	}
+
+	// Add, check, edit, uncheck and delete, as the partner.
+	eggs, err := f.lists.AddItem(ctx, f.bob, shared.ID, service.CreateShoppingItemInput{Name: "Eggs"})
+	if err != nil || eggs.Origin != "manual" {
+		t.Fatalf("partner AddItem = %+v (err %v), want a manual item", eggs, err)
+	}
+	checked, err := f.lists.UpdateItem(ctx, f.bob, shared.ID, milk.ID, service.UpdateShoppingItemInput{Checked: ptr(true)})
+	if err != nil || !checked.Checked || checked.CheckedBy == nil || *checked.CheckedBy != f.bob || checked.Version != 2 {
+		t.Fatalf("partner check = %+v (err %v), want checked by Bob at version 2", checked, err)
+	}
+	renamed, err := f.lists.UpdateItem(ctx, f.bob, shared.ID, milk.ID, service.UpdateShoppingItemInput{Version: ptr(2), Name: ptr("Oat milk")})
+	if err != nil || renamed.Name != "Oat milk" || renamed.CheckedBy == nil || *renamed.CheckedBy != f.bob {
+		t.Fatalf("partner edit = %+v (err %v), want the rename with Bob still recorded as checker", renamed, err)
+	}
+	// The owner unchecks: checked_by is cleared, not left on the partner.
+	unchecked, err := f.lists.UpdateItem(ctx, f.alice, shared.ID, milk.ID, service.UpdateShoppingItemInput{Checked: ptr(false)})
+	if err != nil || unchecked.Checked || unchecked.CheckedBy != nil {
+		t.Fatalf("owner uncheck = %+v (err %v), want unchecked with checked_by cleared", unchecked, err)
+	}
+	if err := f.lists.DeleteItem(ctx, f.bob, shared.ID, eggs.ID); err != nil {
+		t.Fatalf("partner DeleteItem: %v", err)
+	}
+	if list, _ := f.lists.Get(ctx, f.alice, shared.ID); len(list.Items) != 1 {
+		t.Errorf("the owner sees %d items after the partner's changes, want 1", len(list.Items))
+	}
+
+	// List-level actions are owner-only, and answer as if the list did not exist.
+	newName := "Mine now"
+	for label, err := range map[string]error{
+		"Update":     errOf(f.lists.Update(ctx, f.bob, shared.ID, service.UpdateShoppingListInput{Name: &newName})),
+		"unshare":    errOf(f.lists.Update(ctx, f.bob, shared.ID, service.UpdateShoppingListInput{SharedWithPartner: ptr(false)})),
+		"Delete":     f.lists.Delete(ctx, f.bob, shared.ID),
+		"regenerate": errOf2(f.lists.Generate(ctx, f.bob, service.GenerateShoppingListInput{From: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC), ListID: &shared.ID})),
+	} {
+		if !errors.Is(err, service.ErrShoppingListNotFound) {
+			t.Errorf("partner %s: err = %v, want ErrShoppingListNotFound", label, err)
+		}
+	}
+
+	// An unshared list, and any list to a stranger, is not there at all.
+	for label, id := range map[string]uuid.UUID{"an unshared list": private.ID, "a list to a stranger": shared.ID} {
+		caller := f.bob
+		if label == "a list to a stranger" {
+			caller = f.stranger
+		}
+		if _, err := f.lists.Get(ctx, caller, id); !errors.Is(err, service.ErrShoppingListNotFound) {
+			t.Errorf("Get of %s: err = %v, want ErrShoppingListNotFound", label, err)
+		}
+		if _, err := f.lists.AddItem(ctx, caller, id, service.CreateShoppingItemInput{Name: "X"}); !errors.Is(err, service.ErrShoppingListNotFound) {
+			t.Errorf("AddItem on %s: err = %v, want ErrShoppingListNotFound", label, err)
+		}
+		if _, err := f.lists.UpdateItem(ctx, caller, id, milk.ID, service.UpdateShoppingItemInput{Checked: ptr(true)}); !errors.Is(err, service.ErrShoppingItemNotFound) {
+			t.Errorf("UpdateItem on %s: err = %v, want ErrShoppingItemNotFound", label, err)
+		}
+		if err := f.lists.DeleteItem(ctx, caller, id, milk.ID); !errors.Is(err, service.ErrShoppingItemNotFound) {
+			t.Errorf("DeleteItem on %s: err = %v, want ErrShoppingItemNotFound", label, err)
+		}
+		if _, err := f.lists.Subscribe(ctx, caller, id); !errors.Is(err, service.ErrShoppingListNotFound) {
+			t.Errorf("Subscribe to %s: err = %v, want ErrShoppingListNotFound", label, err)
+		}
+	}
+
+	// The owner's regenerate keeps the partner's manual item.
+	if _, err := f.lists.AddItem(ctx, f.bob, shared.ID, service.CreateShoppingItemInput{Name: "Bread"}); err != nil {
+		t.Fatalf("partner AddItem: %v", err)
+	}
+	regenerated, _, err := f.lists.Generate(ctx, f.alice, service.GenerateShoppingListInput{
+		From: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC), ListID: &shared.ID,
+	})
+	if err != nil || len(regenerated.Items) != 2 {
+		t.Errorf("regenerated = %+v (err %v), want both manual items kept (Oat milk, Bread)", regenerated.Items, err)
+	}
+}
+
+// errOf2 is errOf for the three-value Generate.
+func errOf2[A, B any](_ A, _ B, err error) error { return err }
+
+func TestShoppingListsPartnerListingsAreSeparateFromTheOwnLists(t *testing.T) {
+	f := newSharedListFixture(t, "s2")
+	ctx := context.Background()
+	shared := f.createList(t, f.alice, "Shared", true)
+	f.createList(t, f.alice, "Private", false)
+	mine := f.createList(t, f.bob, "Bob's own", true)
+
+	page, err := f.lists.List(ctx, f.bob, service.ListShoppingListsInput{Limit: 10})
+	if err != nil || len(page.Items) != 1 || page.Items[0].ID != mine.ID {
+		t.Errorf("Bob's own List = %+v (err %v), want only his own list", page.Items, err)
+	}
+	partnerPage, err := f.lists.ListPartner(ctx, f.bob, service.ListShoppingListsInput{Limit: 10})
+	if err != nil || len(partnerPage.Items) != 1 || partnerPage.Items[0].ID != shared.ID {
+		t.Errorf("ListPartner = %+v (err %v), want only Alice's shared list", partnerPage.Items, err)
+	}
+	if _, err := f.lists.ListPartner(ctx, f.stranger, service.ListShoppingListsInput{Limit: 10}); !errors.Is(err, service.ErrPartnerNotLinked) {
+		t.Errorf("ListPartner without a partner: err = %v, want ErrPartnerNotLinked", err)
+	}
+}
+
+func TestShoppingListsPartnersSeeEachOthersChangesLive(t *testing.T) {
+	f := newSharedListFixture(t, "s3")
+	ctx := context.Background()
+	list := f.createList(t, f.alice, "Shared", true)
+	item, err := f.lists.AddItem(ctx, f.alice, list.ID, service.CreateShoppingItemInput{Name: "Milk"})
+	if err != nil {
+		t.Fatalf("AddItem: %v", err)
+	}
+	aliceSub, err := f.lists.Subscribe(ctx, f.alice, list.ID)
+	if err != nil {
+		t.Fatalf("owner Subscribe: %v", err)
+	}
+	defer aliceSub.Close()
+	bobSub, err := f.lists.Subscribe(ctx, f.bob, list.ID)
+	if err != nil {
+		t.Fatalf("partner Subscribe: %v", err)
+	}
+	defer bobSub.Close()
+
+	if _, err := f.lists.UpdateItem(ctx, f.bob, list.ID, item.ID, service.UpdateShoppingItemInput{Checked: ptr(true)}); err != nil {
+		t.Fatalf("partner check: %v", err)
+	}
+	if ev := nextEvent(t, aliceSub); ev.Type != service.ListEventItemChanged || ev.ItemID == nil || *ev.ItemID != item.ID || *ev.Version != 2 {
+		t.Errorf("owner's stream got %+v, want the partner's check as item_changed at version 2", ev)
+	}
+	if ev := nextEvent(t, bobSub); ev.Type != service.ListEventItemChanged {
+		t.Errorf("partner's own stream got %+v, want item_changed", ev)
+	}
+	if err := f.lists.DeleteItem(ctx, f.alice, list.ID, item.ID); err != nil {
+		t.Fatalf("owner DeleteItem: %v", err)
+	}
+	if ev := nextEvent(t, bobSub); ev.Type != service.ListEventItemDeleted {
+		t.Errorf("partner's stream got %+v, want the owner's delete as item_deleted", ev)
+	}
+}
+
+func TestShoppingListsAPartnersAccessEndsWithUnlinkingAndUnsharing(t *testing.T) {
+	f := newSharedListFixture(t, "s4")
+	ctx := context.Background()
+	list := f.createList(t, f.alice, "Shared", true)
+	item, err := f.lists.AddItem(ctx, f.alice, list.ID, service.CreateShoppingItemInput{Name: "Milk"})
+	if err != nil {
+		t.Fatalf("AddItem: %v", err)
+	}
+	subscribe := func(user uuid.UUID) *service.ListSubscription {
+		t.Helper()
+		sub, err := f.lists.Subscribe(ctx, user, list.ID)
+		if err != nil {
+			t.Fatalf("Subscribe: %v", err)
+		}
+		t.Cleanup(sub.Close)
+		return sub
+	}
+
+	// Unsharing closes the partner's stream, not the owner's.
+	ownerSub, partnerSub := subscribe(f.alice), subscribe(f.bob)
+	if _, err := f.lists.Update(ctx, f.alice, list.ID, service.UpdateShoppingListInput{SharedWithPartner: ptr(false)}); err != nil {
+		t.Fatalf("unshare: %v", err)
+	}
+	if !closedWithin(partnerSub) {
+		t.Error("the partner's stream stayed open after the owner stopped sharing")
+	}
+	if !stillOpen(ownerSub) {
+		t.Error("the owner's stream was closed by unsharing")
+	}
+	if _, err := f.lists.Get(ctx, f.bob, list.ID); !errors.Is(err, service.ErrShoppingListNotFound) {
+		t.Errorf("partner Get after unsharing: err = %v, want ErrShoppingListNotFound", err)
+	}
+
+	// Unlinking closes it too, and edits stop at once.
+	if _, err := f.lists.Update(ctx, f.alice, list.ID, service.UpdateShoppingListInput{SharedWithPartner: ptr(true)}); err != nil {
+		t.Fatalf("share again: %v", err)
+	}
+	partnerSub = subscribe(f.bob)
+	if err := f.partners.Unlink(ctx, f.alice); err != nil {
+		t.Fatalf("Unlink: %v", err)
+	}
+	if !closedWithin(partnerSub) {
+		t.Error("the partner's stream stayed open after the unlink")
+	}
+	if _, err := f.lists.UpdateItem(ctx, f.bob, list.ID, item.ID, service.UpdateShoppingItemInput{Checked: ptr(true)}); !errors.Is(err, service.ErrShoppingItemNotFound) {
+		t.Errorf("partner check after unlinking: err = %v, want ErrShoppingItemNotFound", err)
+	}
+	// The list and everything on it, including what the partner added, stays with the owner.
+	if got, err := f.lists.Get(ctx, f.alice, list.ID); err != nil || len(got.Items) != 1 {
+		t.Errorf("owner's list after the unlink = %+v (err %v), want it intact", got.Items, err)
+	}
+}
+
+// An item edit that starts while an unlink is deleting the partnership must
+// wait for it and then find no partner. Each case holds the partnership row
+// in an open transaction (as the unlink's DELETE does), starts the operation,
+// and checks that it waits.
+func TestShoppingListsItemWritesWaitForAnUnlinkInFlightAndThenFindNoPartner(t *testing.T) {
+	ops := map[string]func(f sharedListFixture, list service.ShoppingList, item service.ShoppingItem) error{
+		"AddItem": func(f sharedListFixture, list service.ShoppingList, _ service.ShoppingItem) error {
+			_, err := f.lists.AddItem(context.Background(), f.bob, list.ID, service.CreateShoppingItemInput{Name: "Eggs"})
+			return err
+		},
+		"UpdateItem": func(f sharedListFixture, list service.ShoppingList, item service.ShoppingItem) error {
+			_, err := f.lists.UpdateItem(context.Background(), f.bob, list.ID, item.ID, service.UpdateShoppingItemInput{Checked: ptr(true)})
+			return err
+		},
+		"DeleteItem": func(f sharedListFixture, list service.ShoppingList, item service.ShoppingItem) error {
+			return f.lists.DeleteItem(context.Background(), f.bob, list.ID, item.ID)
+		},
+	}
+	for name, op := range ops {
+		t.Run(name, func(t *testing.T) {
+			f := newSharedListFixture(t, "s5-"+name)
+			ctx := context.Background()
+			list := f.createList(t, f.alice, "Shared", true)
+			item, err := f.lists.AddItem(ctx, f.alice, list.ID, service.CreateShoppingItemInput{Name: "Milk"})
+			if err != nil {
+				t.Fatalf("AddItem: %v", err)
+			}
+
+			deleted, release := make(chan struct{}), make(chan struct{})
+			var releaseOnce sync.Once
+			releaseTx := func() { releaseOnce.Do(func() { close(release) }) }
+			t.Cleanup(releaseTx) // an open transaction would make closing the pool wait forever
+			txDone := make(chan error, 1)
+			go func() {
+				txDone <- f.st.InTx(ctx, func(q *sqlc.Queries) error {
+					if _, err := q.DeletePartnershipsForUser(ctx, f.alice); err != nil {
+						return err
+					}
+					close(deleted)
+					<-release
+					return nil
+				})
+			}()
+			<-deleted
+
+			result := make(chan error, 1)
+			go func() { result <- op(f, list, item) }()
+			select {
+			case err := <-result:
+				t.Fatalf("%s finished (err %v) while an unlink was in flight, want it to wait for the partnership row", name, err)
+			case <-time.After(300 * time.Millisecond):
+			}
+
+			releaseTx()
+			if err := <-txDone; err != nil {
+				t.Fatalf("unlink transaction: %v", err)
+			}
+			err = <-result
+			if !errors.Is(err, service.ErrShoppingListNotFound) && !errors.Is(err, service.ErrShoppingItemNotFound) {
+				t.Errorf("%s after the unlink committed: err = %v, want the list or item to be not found", name, err)
+			}
+		})
+	}
+}
+
+// Subscribe registers with the hub first and only then makes its final access
+// check, which waits for an unlink in flight. Registered-before-checked is
+// what lets CloseAccess (which runs after an unlink commits) reach a stream
+// whose first lookup was made just before the unlink.
+func TestShoppingListsSubscribeRegistersBeforeItsFinalAccessCheck(t *testing.T) {
+	f := newSharedListFixture(t, "s6")
+	ctx := context.Background()
+	list := f.createList(t, f.alice, "Shared", true)
+
+	deleted, release := make(chan struct{}), make(chan struct{})
+	var releaseOnce sync.Once
+	releaseTx := func() { releaseOnce.Do(func() { close(release) }) }
+	t.Cleanup(releaseTx)
+	txDone := make(chan error, 1)
+	go func() {
+		txDone <- f.st.InTx(ctx, func(q *sqlc.Queries) error {
+			if _, err := q.DeletePartnershipsForUser(ctx, f.alice); err != nil {
+				return err
+			}
+			close(deleted)
+			<-release
+			return nil
+		})
+	}()
+	<-deleted
+
+	type subscribed struct {
+		sub *service.ListSubscription
+		err error
+	}
+	result := make(chan subscribed, 1)
+	go func() {
+		sub, err := f.lists.Subscribe(ctx, f.bob, list.ID)
+		result <- subscribed{sub, err}
+	}()
+
+	deadline := time.Now().Add(5 * time.Second)
+	for f.events.Subscribers(list.ID) != 1 {
+		if time.Now().After(deadline) {
+			t.Fatal("Subscribe did not register with the hub while the unlink was in flight, want it registered before its final check")
+		}
+		time.Sleep(5 * time.Millisecond)
+	}
+	select {
+	case r := <-result:
+		if r.sub != nil {
+			r.sub.Close()
+		}
+		t.Fatalf("Subscribe returned (err %v) while an unlink was in flight, want its final check to wait for the partnership row", r.err)
+	case <-time.After(200 * time.Millisecond):
+	}
+
+	releaseTx()
+	if err := <-txDone; err != nil {
+		t.Fatalf("unlink transaction: %v", err)
+	}
+	if r := <-result; !errors.Is(r.err, service.ErrShoppingListNotFound) {
+		t.Errorf("Subscribe after the unlink committed: err = %v, want ErrShoppingListNotFound", r.err)
+	}
+	if n := f.events.Subscribers(list.ID); n != 0 {
+		t.Errorf("Subscribers after the refused Subscribe = %d, want 0", n)
+	}
+}
```

Modify `backend/internal/service/partners_test.go` (unified diff against the current file):

```diff
--- a/backend/internal/service/partners_test.go
+++ b/backend/internal/service/partners_test.go
@@ -462,10 +462,9 @@ func partnersFor(st *store.Store) *service.Partners {
 	return service.NewPartners(st, service.NewListEventHub(), time.Now)
 }
 
-// linkPartners makes a and b partners through the real invite flow.
-func linkPartners(t *testing.T, st *store.Store, a, b uuid.UUID) *service.Partners {
+// linkWith makes a and b partners through p's real invite flow.
+func linkWith(t *testing.T, p *service.Partners, a, b uuid.UUID) {
 	t.Helper()
-	p := partnersFor(st)
 	inv, err := p.Invite(context.Background(), a)
 	if err != nil {
 		t.Fatalf("Invite: %v", err)
@@ -473,5 +472,13 @@ func linkPartners(t *testing.T, st *store.Store, a, b uuid.UUID) *service.Partne
 	if _, err := p.Accept(context.Background(), b, inv.Code); err != nil {
 		t.Fatalf("Accept: %v", err)
 	}
+}
+
+// linkPartners makes a and b partners with a fresh Partners service, which it
+// returns so the test can unlink them again.
+func linkPartners(t *testing.T, st *store.Store, a, b uuid.UUID) *service.Partners {
+	t.Helper()
+	p := partnersFor(st)
+	linkWith(t, p, a, b)
 	return p
 }
```

Create `backend/internal/service/shopping_lists_partner_edges_test.go`:

```go
package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/InzKazik/mealplanner/backend/internal/service"
)

// A partner resolves ingredients with their own visibility: the owner's custom
// ingredient is not theirs to reference, and the answer must not reveal it.
func TestShoppingListsPartnerCannotAddAnItemOnTheOwnersCustomIngredient(t *testing.T) {
	f := newSharedListFixture(t, "e1")
	ctx := context.Background()
	list := f.createList(t, f.alice, "Shared", true)
	secret := mustCreateIngredient(t, f.ing, f.alice, service.CreateIngredientInput{Name: "Alice's Secret Sauce", Category: "condiments_oils"})

	_, err := f.lists.AddItem(ctx, f.bob, list.ID, service.CreateShoppingItemInput{IngredientID: &secret.ID, Name: "Sauce"})
	if !errors.Is(err, service.ErrShoppingItemIngredientNotFound) {
		t.Errorf("partner AddItem on the owner's custom ingredient: err = %v, want ErrShoppingItemIngredientNotFound", err)
	}
	if _, err := f.lists.AddItem(ctx, f.alice, list.ID, service.CreateShoppingItemInput{IngredientID: &secret.ID, Name: "Sauce"}); err != nil {
		t.Errorf("the owner's AddItem on their own ingredient: %v", err)
	}
}

// When the owner deletes a shared list while the partner watches it, the
// partner's stream ends with list_deleted like the owner's.
func TestShoppingListsDeletingASharedListEndsThePartnersStream(t *testing.T) {
	f := newSharedListFixture(t, "e2")
	ctx := context.Background()
	list := f.createList(t, f.alice, "Shared", true)
	partnerSub, err := f.lists.Subscribe(ctx, f.bob, list.ID)
	if err != nil {
		t.Fatalf("partner Subscribe: %v", err)
	}
	defer partnerSub.Close()

	if err := f.lists.Delete(ctx, f.alice, list.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if ev := nextEvent(t, partnerSub); ev.Type != service.ListEventListDeleted || ev.ListID != list.ID {
		t.Errorf("partner's stream got %+v, want list_deleted for the list", ev)
	}
	if !closedWithin(partnerSub) {
		t.Error("the partner's stream stayed open after list_deleted")
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `cd backend && go test ./internal/service/ -count=1`

Expected: FAIL, for the right reason:

```
internal/service/shopping_lists_test.go:563:23: got.OwnerID undefined (type service.ShoppingList has no field or method OwnerID)
internal/service/shopping_lists_test.go:654:30: f.lists.ListPartner undefined (type *service.ShoppingLists has no field or method ListPartner)
```

- [ ] **Step 3: Implement**

Modify `backend/internal/store/queries/shopping_lists.sql` (unified diff against the current file):

```diff
--- a/backend/internal/store/queries/shopping_lists.sql
+++ b/backend/internal/store/queries/shopping_lists.sql
@@ -4,27 +4,39 @@ VALUES ($1, $2, $3, $4, $5)
 RETURNING *;
 
 -- name: GetShoppingListForUser :one
--- Owner-only visibility for now: shared_with_partner has no effect until the
--- partner plan adds the partnerships table and an active-partner lookup. See
--- "Not built yet" in backend/CLAUDE.md.
+-- The read predicate (spec §5): the caller's own list, or one the caller's
+-- active partner has shared. partner_id is NULL when the caller has no
+-- partner, which makes the second branch match nothing.
 SELECT * FROM shopping_lists
-WHERE id = sqlc.arg('id') AND owner_id = sqlc.arg('user_id');
+WHERE id = sqlc.arg('id')
+  AND (
+    owner_id = sqlc.arg('user_id')
+    OR (owner_id = sqlc.narg('partner_id')::uuid AND shared_with_partner)
+  );
 
 -- name: TouchShoppingListForUser :one
 -- Bumps updated_at (via the shopping_lists_set_updated_at trigger) and, just
 -- as importantly, takes the list row's write lock: AddItem uses this instead
 -- of a plain SELECT so two concurrent adds cannot read the same
 -- NextShoppingItemPosition. See TouchMealForUser in meals.sql for the same
--- pattern in the meals domain.
+-- pattern in the meals domain. Uses the read predicate, because a partner may
+-- add items to a shared list.
 UPDATE shopping_lists SET updated_at = now()
-WHERE id = sqlc.arg('id') AND owner_id = sqlc.arg('user_id')
+WHERE id = sqlc.arg('id')
+  AND (
+    owner_id = sqlc.arg('user_id')
+    OR (owner_id = sqlc.narg('partner_id')::uuid AND shared_with_partner)
+  )
 RETURNING *;
 
 -- name: ListShoppingListsForUser :many
 -- Newest first. The row comparison is the keyset cursor over
--- (created_at DESC, id DESC).
+-- (created_at DESC, id DESC). shared_only is for GET partner/shopping-lists:
+-- user_id is then the partner's id and only what the partner has shared comes
+-- back.
 SELECT * FROM shopping_lists
 WHERE owner_id = sqlc.arg('user_id')
+  AND (NOT sqlc.arg('shared_only')::boolean OR shared_with_partner)
   AND (
     NOT sqlc.arg('has_cursor')::boolean
     OR (created_at, id) < (sqlc.arg('cursor_created_at')::timestamptz, sqlc.arg('cursor_id')::uuid)
@@ -62,12 +74,17 @@ SELECT * FROM shopping_items WHERE list_id = sqlc.arg('list_id') ORDER BY positi
 
 -- name: GetShoppingItemForUserForUpdate :one
 -- Locks the one item row for UpdateItem's version check and the UPDATE after
--- it. The join is the ownership check; only the item row is locked.
+-- it. The join is the visibility check (the read predicate: the caller's own
+-- list, or a list the caller's active partner has shared); only the item row
+-- is locked.
 SELECT shopping_items.* FROM shopping_items
 JOIN shopping_lists ON shopping_lists.id = shopping_items.list_id
 WHERE shopping_items.id = sqlc.arg('id')
   AND shopping_items.list_id = sqlc.arg('list_id')
-  AND shopping_lists.owner_id = sqlc.arg('user_id')
+  AND (
+    shopping_lists.owner_id = sqlc.arg('user_id')
+    OR (shopping_lists.owner_id = sqlc.narg('partner_id')::uuid AND shopping_lists.shared_with_partner)
+  )
 FOR UPDATE OF shopping_items;
 
 -- name: NextShoppingItemPosition :one
@@ -101,13 +118,17 @@ RETURNING *;
 
 -- name: DeleteShoppingItemForUser :one
 -- :one, not :execrows: the deleted item's last version goes into the
--- item_deleted event.
+-- item_deleted event. Uses the read predicate: a partner may delete items
+-- from a list the owner shared.
 DELETE FROM shopping_items
 USING shopping_lists
 WHERE shopping_items.id = sqlc.arg('id')
   AND shopping_items.list_id = sqlc.arg('list_id')
   AND shopping_lists.id = shopping_items.list_id
-  AND shopping_lists.owner_id = sqlc.arg('user_id')
+  AND (
+    shopping_lists.owner_id = sqlc.arg('user_id')
+    OR (shopping_lists.owner_id = sqlc.narg('partner_id')::uuid AND shopping_lists.shared_with_partner)
+  )
 RETURNING shopping_items.id, shopping_items.version;
 
 -- name: DeleteGeneratedShoppingItems :exec
```

Modify `backend/internal/service/shopping_lists.go` (unified diff against the current file):

```diff
--- a/backend/internal/service/shopping_lists.go
+++ b/backend/internal/service/shopping_lists.go
@@ -17,12 +17,15 @@ import (
 
 // Errors returned by ShoppingLists. Handlers map them to problem responses.
 var (
-	// ErrShoppingListNotFound means the list does not exist or is not owned
-	// by the caller. Partner visibility is not implemented yet (see the
-	// ShoppingLists doc comment).
+	// ErrShoppingListNotFound means the list does not exist, or the caller may
+	// not see it: it belongs to someone other than the caller and the
+	// caller's active partner, or to the partner but is not shared. A
+	// partner's attempt at an owner-only action (rename, delete, share,
+	// regenerate) is also ErrShoppingListNotFound.
 	ErrShoppingListNotFound = errors.New("shopping list not found")
 	// ErrShoppingItemNotFound means the item does not exist, is not on the
-	// given list, or the list is not visible to the caller.
+	// given list, or the list is not visible to the caller (including a
+	// partner's list after an unlink).
 	ErrShoppingItemNotFound = errors.New("shopping item not found")
 	// ErrShoppingItemIngredientNotFound means a new item's ingredient_id does
 	// not exist or is not visible to the caller.
@@ -67,6 +70,7 @@ type ShoppingItem struct {
 // ShoppingList is a list with its items, ordered by position.
 type ShoppingList struct {
 	ID                uuid.UUID
+	OwnerID           uuid.UUID
 	Name              string
 	SharedWithPartner bool
 	SourceFrom        *time.Time
@@ -150,12 +154,13 @@ type UpdateShoppingItemInput struct {
 	Checked  *bool
 }
 
-// ShoppingLists implements shopping lists owned by a single user, their
-// generation from the plan, item edits with optimistic concurrency, and the
-// live event streams. Partner sharing is not implemented: shared_with_partner
-// is stored, but every read and write here checks owner_id only, and only
-// the owner's own edits reach a list's event streams. See the "Not built
-// yet" note in backend/CLAUDE.md.
+// ShoppingLists implements shopping lists, their generation from the plan,
+// item edits with optimistic concurrency, and the live event streams. A list
+// is owned by one user. When shared_with_partner is true the owner's active
+// partner may read it, add, edit, check and delete items, and watch its event
+// stream (spec §3.6: editable by both). List-level actions (rename, delete,
+// toggling sharing, regenerating) stay owner-only, and regenerating reads the
+// owner's own plan entries.
 type ShoppingLists struct {
 	st     *store.Store
 	events *ListEventHub
@@ -182,9 +187,14 @@ func (s *ShoppingLists) Create(ctx context.Context, ownerID uuid.UUID, in Create
 	return toShoppingList(row, nil), nil
 }
 
-// Get returns a list owned by ownerID, with its items.
-func (s *ShoppingLists) Get(ctx context.Context, ownerID, id uuid.UUID) (ShoppingList, error) {
-	row, err := s.st.GetShoppingListForUser(ctx, sqlc.GetShoppingListForUserParams{ID: id, UserID: ownerID})
+// Get returns a list with its items: one owned by callerID, or one their
+// active partner has shared.
+func (s *ShoppingLists) Get(ctx context.Context, callerID, id uuid.UUID) (ShoppingList, error) {
+	partnerID, err := partnerOrNil(ctx, s.st.Queries, callerID, false)
+	if err != nil {
+		return ShoppingList{}, err
+	}
+	row, err := s.st.GetShoppingListForUser(ctx, sqlc.GetShoppingListForUserParams{ID: id, UserID: callerID, PartnerID: partnerID})
 	if store.IsNotFound(err) {
 		return ShoppingList{}, ErrShoppingListNotFound
 	}
@@ -198,12 +208,27 @@ func (s *ShoppingLists) Get(ctx context.Context, ownerID, id uuid.UUID) (Shoppin
 	return toShoppingList(row, items), nil
 }
 
-// List returns a page of the caller's lists, newest first.
+// List returns a page of the caller's own lists, newest first. The partner's
+// shared lists are listed by ListPartner, never mixed in here.
 func (s *ShoppingLists) List(ctx context.Context, ownerID uuid.UUID, in ListShoppingListsInput) (ShoppingListPage, error) {
+	return s.list(ctx, ownerID, false, in)
+}
+
+// ListPartner returns a page of the lists callerID's active partner has
+// shared, newest first. Without an active partner it is ErrPartnerNotLinked.
+func (s *ShoppingLists) ListPartner(ctx context.Context, callerID uuid.UUID, in ListShoppingListsInput) (ShoppingListPage, error) {
+	partnerID, err := activePartnerID(ctx, s.st.Queries, callerID, false)
+	if err != nil {
+		return ShoppingListPage{}, err
+	}
+	return s.list(ctx, partnerID, true, in)
+}
+
+func (s *ShoppingLists) list(ctx context.Context, ownerID uuid.UUID, sharedOnly bool, in ListShoppingListsInput) (ShoppingListPage, error) {
 	if in.Limit < 1 {
 		in.Limit = 1
 	}
-	params := sqlc.ListShoppingListsForUserParams{UserID: ownerID, RowLimit: toRowLimit(in.Limit + 1)}
+	params := sqlc.ListShoppingListsForUserParams{UserID: ownerID, SharedOnly: sharedOnly, RowLimit: toRowLimit(in.Limit + 1)}
 	if in.Cursor != nil {
 		params.HasCursor = true
 		params.CursorCreatedAt = in.Cursor.CreatedAt
@@ -231,7 +256,8 @@ func (s *ShoppingLists) List(ctx context.Context, ownerID uuid.UUID, in ListShop
 }
 
 // Update applies a partial update to a list owned by ownerID and tells its
-// event streams the list changed.
+// event streams the list changed. Turning sharing off ends the partner's open
+// streams: they reconnect, refetch and get 404.
 func (s *ShoppingLists) Update(ctx context.Context, ownerID, id uuid.UUID, in UpdateShoppingListInput) (ShoppingList, error) {
 	var list ShoppingList
 	err := s.st.InTx(ctx, func(q *sqlc.Queries) error {
@@ -255,6 +281,9 @@ func (s *ShoppingLists) Update(ctx context.Context, ownerID, id uuid.UUID, in Up
 		return ShoppingList{}, err
 	}
 	s.events.Publish(ListEvent{Type: ListEventListChanged, ListID: id})
+	if in.SharedWithPartner != nil && !*in.SharedWithPartner {
+		s.events.CloseListForNonOwners(id)
+	}
 	return list, nil
 }
 
@@ -359,14 +388,23 @@ func (s *ShoppingLists) Generate(ctx context.Context, ownerID uuid.UUID, in Gene
 	return list, created, nil
 }
 
-// AddItem appends a manual item to a list owned by ownerID.
-func (s *ShoppingLists) AddItem(ctx context.Context, ownerID, listID uuid.UUID, in CreateShoppingItemInput) (ShoppingItem, error) {
+// AddItem appends a manual item to a list callerID owns or their active
+// partner has shared. A partner's items are manual too, so the owner's
+// regenerate keeps them.
+func (s *ShoppingLists) AddItem(ctx context.Context, callerID, listID uuid.UUID, in CreateShoppingItemInput) (ShoppingItem, error) {
 	var item ShoppingItem
 	err := s.st.InTx(ctx, func(q *sqlc.Queries) error {
-		// TouchShoppingListForUser checks ownership and takes the list row's
+		// FOR SHARE on the partnership row: an unlink waits for this
+		// transaction, and an add that starts after the unlink finds no
+		// partner and gets a 404 (spec §6).
+		partnerID, err := partnerOrNil(ctx, q, callerID, true)
+		if err != nil {
+			return err
+		}
+		// TouchShoppingListForUser checks visibility and takes the list row's
 		// write lock, so two concurrent adds (or an add racing a
 		// regeneration) cannot both read the same next position.
-		if _, err := q.TouchShoppingListForUser(ctx, sqlc.TouchShoppingListForUserParams{ID: listID, UserID: ownerID}); err != nil {
+		if _, err := q.TouchShoppingListForUser(ctx, sqlc.TouchShoppingListForUserParams{ID: listID, UserID: callerID, PartnerID: partnerID}); err != nil {
 			if store.IsNotFound(err) {
 				return ErrShoppingListNotFound
 			}
@@ -374,7 +412,7 @@ func (s *ShoppingLists) AddItem(ctx context.Context, ownerID, listID uuid.UUID,
 		}
 		category := "other"
 		if in.IngredientID != nil {
-			rows, err := q.GetIngredientsForUser(ctx, sqlc.GetIngredientsForUserParams{Ids: []uuid.UUID{*in.IngredientID}, UserID: &ownerID})
+			rows, err := q.GetIngredientsForUser(ctx, sqlc.GetIngredientsForUserParams{Ids: []uuid.UUID{*in.IngredientID}, UserID: &callerID})
 			if err != nil {
 				return fmt.Errorf("get ingredient: %w", err)
 			}
@@ -413,7 +451,8 @@ func (s *ShoppingLists) AddItem(ctx context.Context, ownerID, listID uuid.UUID,
 	return item, nil
 }
 
-// UpdateItem edits or checks off an item on a list owned by ownerID.
+// UpdateItem edits or checks off an item on a list callerID owns or their
+// active partner has shared. CheckedBy records callerID, the acting user.
 //
 // An edit (Name, Quantity, Unit or Category present) needs in.Version and
 // fails with *ShoppingItemVersionConflictError, carrying the current item,
@@ -422,7 +461,7 @@ func (s *ShoppingLists) AddItem(ctx context.Context, ownerID, listID uuid.UUID,
 // it already has changes nothing (no version bump, no event), so retries
 // and offline replays are safe. Every real change bumps the version by one
 // and returns the item with its new version for the client to chain on.
-func (s *ShoppingLists) UpdateItem(ctx context.Context, ownerID, listID, itemID uuid.UUID, in UpdateShoppingItemInput) (ShoppingItem, error) {
+func (s *ShoppingLists) UpdateItem(ctx context.Context, callerID, listID, itemID uuid.UUID, in UpdateShoppingItemInput) (ShoppingItem, error) {
 	isEdit := in.Name != nil || in.Quantity.Specified || in.Unit.Specified || in.Category != nil
 	if isEdit && in.Version == nil {
 		return ShoppingItem{}, ErrShoppingItemVersionRequired
@@ -432,11 +471,16 @@ func (s *ShoppingLists) UpdateItem(ctx context.Context, ownerID, listID, itemID
 		changed bool
 	)
 	err := s.st.InTx(ctx, func(q *sqlc.Queries) error {
+		// FOR SHARE on the partnership row: see AddItem.
+		partnerID, err := partnerOrNil(ctx, q, callerID, true)
+		if err != nil {
+			return err
+		}
 		// FOR UPDATE: the version check below and the UPDATE after it must
 		// see the same row, so a concurrent edit waits here instead of
 		// slipping in between them.
 		cur, err := q.GetShoppingItemForUserForUpdate(ctx, sqlc.GetShoppingItemForUserForUpdateParams{
-			ID: itemID, ListID: listID, UserID: ownerID,
+			ID: itemID, ListID: listID, UserID: callerID, PartnerID: partnerID,
 		})
 		if store.IsNotFound(err) {
 			return ErrShoppingItemNotFound
@@ -455,7 +499,7 @@ func (s *ShoppingLists) UpdateItem(ctx context.Context, ownerID, listID, itemID
 			ID: itemID, Name: in.Name,
 			SetQuantity: in.Quantity.Specified, Quantity: in.Quantity.Value,
 			SetUnit: in.Unit.Specified, Unit: in.Unit.Value,
-			Category: in.Category, Checked: in.Checked, CheckedBy: &ownerID,
+			Category: in.Category, Checked: in.Checked, CheckedBy: &callerID,
 		})
 		if err != nil {
 			return fmt.Errorf("update shopping item: %w", err)
@@ -472,42 +516,85 @@ func (s *ShoppingLists) UpdateItem(ctx context.Context, ownerID, listID, itemID
 	return item, nil
 }
 
-// DeleteItem removes an item from a list owned by ownerID, whatever its
-// version: removes merge (spec §4.3), so a remove is never a conflict.
-func (s *ShoppingLists) DeleteItem(ctx context.Context, ownerID, listID, itemID uuid.UUID) error {
-	row, err := s.st.DeleteShoppingItemForUser(ctx, sqlc.DeleteShoppingItemForUserParams{
-		ID: itemID, ListID: listID, UserID: ownerID,
-	})
-	if store.IsNotFound(err) {
-		return ErrShoppingItemNotFound
+// DeleteItem removes an item from a list callerID owns or their active partner
+// has shared, whatever its version: removes merge (spec §4.3), so a remove is
+// never a conflict.
+func (s *ShoppingLists) DeleteItem(ctx context.Context, callerID, listID, itemID uuid.UUID) error {
+	var deleted struct {
+		id      uuid.UUID
+		version int
 	}
+	err := s.st.InTx(ctx, func(q *sqlc.Queries) error {
+		// FOR SHARE on the partnership row: see AddItem.
+		partnerID, err := partnerOrNil(ctx, q, callerID, true)
+		if err != nil {
+			return err
+		}
+		row, err := q.DeleteShoppingItemForUser(ctx, sqlc.DeleteShoppingItemForUserParams{
+			ID: itemID, ListID: listID, UserID: callerID, PartnerID: partnerID,
+		})
+		if store.IsNotFound(err) {
+			return ErrShoppingItemNotFound
+		}
+		if err != nil {
+			return fmt.Errorf("delete shopping item: %w", err)
+		}
+		deleted.id, deleted.version = row.ID, int(row.Version)
+		return nil
+	})
 	if err != nil {
-		return fmt.Errorf("delete shopping item: %w", err)
+		return err
 	}
-	s.publishItem(ListEventItemDeleted, listID, row.ID, int(row.Version))
+	s.publishItem(ListEventItemDeleted, listID, deleted.id, deleted.version)
 	return nil
 }
 
-// Subscribe opens an event stream for a list owned by ownerID. It subscribes
-// before checking ownership, so a delete that commits between the two can
-// never be missed: either the check sees the list gone (404), or the
-// subscription is already registered when the list_deleted event is
-// published.
-func (s *ShoppingLists) Subscribe(ctx context.Context, ownerID, listID uuid.UUID) (*ListSubscription, error) {
-	sub, err := s.events.Subscribe(listID, ownerID, ownerID)
+// Subscribe opens an event stream for a list callerID owns or their active
+// partner has shared. It registers with the hub before its final access
+// check, and that check takes the partnership row FOR SHARE, so neither a
+// delete nor an unlink can slip in unnoticed: either the check sees the list
+// or the partnership gone (404), or the subscription is already registered
+// when list_deleted is published or CloseAccess runs after the unlink commits.
+func (s *ShoppingLists) Subscribe(ctx context.Context, callerID, listID uuid.UUID) (*ListSubscription, error) {
+	// The first lookup tells the hub who owns the list, which CloseAccess
+	// needs; it is not the check that counts.
+	list, err := s.visibleList(ctx, callerID, listID, false)
 	if err != nil {
 		return nil, err
 	}
-	if _, err := s.st.GetShoppingListForUser(ctx, sqlc.GetShoppingListForUserParams{ID: listID, UserID: ownerID}); err != nil {
+	sub, err := s.events.Subscribe(listID, callerID, list.OwnerID)
+	if err != nil {
+		return nil, err
+	}
+	if _, err := s.visibleList(ctx, callerID, listID, true); err != nil {
 		sub.Close()
-		if store.IsNotFound(err) {
-			return nil, ErrShoppingListNotFound
-		}
-		return nil, fmt.Errorf("get shopping list: %w", err)
+		return nil, err
 	}
 	return sub, nil
 }
 
+// visibleList reads a list under the read predicate, in one transaction with
+// the partner lookup. With forShare the partnership row is locked FOR SHARE
+// until the read is done.
+func (s *ShoppingLists) visibleList(ctx context.Context, callerID, listID uuid.UUID, forShare bool) (sqlc.ShoppingList, error) {
+	var row sqlc.ShoppingList
+	err := s.st.InTx(ctx, func(q *sqlc.Queries) error {
+		partnerID, err := partnerOrNil(ctx, q, callerID, forShare)
+		if err != nil {
+			return err
+		}
+		row, err = q.GetShoppingListForUser(ctx, sqlc.GetShoppingListForUserParams{ID: listID, UserID: callerID, PartnerID: partnerID})
+		if store.IsNotFound(err) {
+			return ErrShoppingListNotFound
+		}
+		if err != nil {
+			return fmt.Errorf("get shopping list: %w", err)
+		}
+		return nil
+	})
+	return row, err
+}
+
 func (s *ShoppingLists) publishItem(typ string, listID, itemID uuid.UUID, version int) {
 	s.events.Publish(ListEvent{Type: typ, ListID: listID, ItemID: &itemID, Version: &version})
 }
@@ -662,7 +749,7 @@ func toShoppingList(row sqlc.ShoppingList, itemRows []sqlc.ShoppingItem) Shoppin
 		items[i] = toShoppingItem(r)
 	}
 	return ShoppingList{
-		ID: row.ID, Name: row.Name, SharedWithPartner: row.SharedWithPartner,
+		ID: row.ID, OwnerID: row.OwnerID, Name: row.Name, SharedWithPartner: row.SharedWithPartner,
 		SourceFrom: fromPgDatePtr(row.SourceFrom), SourceTo: fromPgDatePtr(row.SourceTo),
 		Items: items, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
 	}
```

- [ ] **Step 4: Regenerate**

Run `make generate` from the repo root. It rewrites `backend/internal/store/sqlc/`. Commit the output and never edit it by hand. `make check-generated` fails if it is stale.

- [ ] **Step 5: Run the tests and see them pass**

Run: `cd backend && go test ./internal/service/ -count=1`

Expected: PASS (`ok  	github.com/InzKazik/mealplanner/backend/internal/service`).

- [ ] **Step 6: Commit**

```bash
git add backend/internal/service/partners_test.go backend/internal/service/shopping_lists.go backend/internal/service/shopping_lists_partner_edges_test.go backend/internal/service/shopping_lists_test.go backend/internal/store/queries/shopping_lists.sql backend/internal/store/sqlc/shopping_lists.sql.go
git commit -m "feat(backend): let a partner edit the items of a shared shopping list"
```

### Task 7: Account deletion ends partner access and streams

**Files:**
- Modify: `backend/internal/service/auth.go`
- Modify: `backend/internal/service/auth_test.go`

**Interfaces:**
- Consumes: `ListEventHub.CloseUser` (Task 2), `Partners`, `ShoppingLists` (Tasks 3, 6).
- Produces: `(*Auth).OnUserDeleted(fn func(uuid.UUID))`: `fn` runs after a `DeleteUser` commits, with the deleted id. `cmd/api` registers `listEvents.CloseUser` in Task 8.

`partnerships` needs no step in `DeleteUser`'s explicit delete chain: nothing references it `NO ACTION`, and its three user columns cascade. What the chain does not do is tell the event hub: it removes the user's lists with a raw `DELETE`, which publishes no `list_deleted`. The hook closes those streams. The tests also cover the two handoff items `backend/CLAUDE.md` carried for this plan: `checked_by` is set to NULL when a partner's account goes (the owner's list survives), and an owner's deletion ends the partner's access.

- [ ] **Step 1: Write the failing tests**

Modify `backend/internal/service/auth_test.go` (unified diff against the current file):

```diff
--- a/backend/internal/service/auth_test.go
+++ b/backend/internal/service/auth_test.go
@@ -569,3 +569,124 @@ func TestRefreshWaitsForTheTokenRowLock(t *testing.T) {
 }
 
 func ptr[T any](v T) *T { return &v }
+
+// linkedUsers registers two users through Auth and links them as partners.
+func linkedUsers(t *testing.T, f *fixture, ownerEmail, partnerEmail string) (owner, partner uuid.UUID, partners *service.Partners, hub *service.ListEventHub) {
+	t.Helper()
+	owner, partner = register(t, f, ownerEmail).User.ID, register(t, f, partnerEmail).User.ID
+	hub = service.NewListEventHub()
+	partners = service.NewPartners(f.store, hub, time.Now)
+	linkWith(t, partners, owner, partner)
+	return owner, partner, partners, hub
+}
+
+func TestDeleteUserAPartnerWhoCheckedItemsLeavesTheOwnersListIntact(t *testing.T) {
+	f := newFixture(t)
+	ctx := context.Background()
+	owner, partner, partners, hub := linkedUsers(t, f, "owner-del1@example.com", "partner-del1@example.com")
+	lists := service.NewShoppingLists(f.store, hub)
+
+	list, err := lists.Create(ctx, owner, service.CreateShoppingListInput{Name: "Shared", SharedWithPartner: true})
+	if err != nil {
+		t.Fatalf("Create: %v", err)
+	}
+	item, err := lists.AddItem(ctx, owner, list.ID, service.CreateShoppingItemInput{Name: "Milk"})
+	if err != nil {
+		t.Fatalf("AddItem: %v", err)
+	}
+	if _, err := lists.UpdateItem(ctx, partner, list.ID, item.ID, service.UpdateShoppingItemInput{Checked: ptr(true)}); err != nil {
+		t.Fatalf("partner check: %v", err)
+	}
+	added, err := lists.AddItem(ctx, partner, list.ID, service.CreateShoppingItemInput{Name: "Eggs"})
+	if err != nil {
+		t.Fatalf("partner AddItem: %v", err)
+	}
+
+	if err := f.svc.DeleteUser(ctx, partner); err != nil {
+		t.Fatalf("DeleteUser of the partner: %v", err)
+	}
+
+	got, err := lists.Get(ctx, owner, list.ID)
+	if err != nil {
+		t.Fatalf("the owner's list did not survive its partner's account deletion: %v", err)
+	}
+	if len(got.Items) != 2 {
+		t.Fatalf("items after the partner's deletion = %+v, want both (the partner's added item stays too)", got.Items)
+	}
+	for _, it := range got.Items {
+		if it.ID == item.ID && (!it.Checked || it.CheckedBy != nil) {
+			t.Errorf("item the partner checked = %+v, want it still checked with checked_by NULL (ON DELETE SET NULL)", it)
+		}
+		if it.ID == added.ID && it.Origin != "manual" {
+			t.Errorf("item the partner added = %+v, want origin manual", it)
+		}
+	}
+	if _, err := partners.Get(ctx, owner); !errors.Is(err, service.ErrPartnerNotLinked) {
+		t.Errorf("owner's partnership after the partner's deletion: err = %v, want ErrPartnerNotLinked", err)
+	}
+}
+
+func TestDeleteUserAnOwnerEndsThePartnersAccessAndClosesTheirStreams(t *testing.T) {
+	f := newFixture(t)
+	ctx := context.Background()
+	owner, partner, partners, hub := linkedUsers(t, f, "owner-del2@example.com", "partner-del2@example.com")
+	f.svc.OnUserDeleted(hub.CloseUser)
+	lists := service.NewShoppingLists(f.store, hub)
+	list, err := lists.Create(ctx, owner, service.CreateShoppingListInput{Name: "Shared", SharedWithPartner: true})
+	if err != nil {
+		t.Fatalf("Create: %v", err)
+	}
+	partnerSub, err := lists.Subscribe(ctx, partner, list.ID)
+	if err != nil {
+		t.Fatalf("partner Subscribe: %v", err)
+	}
+	defer partnerSub.Close()
+	other := register(t, f, "other-del2@example.com").User.ID
+	otherList, err := lists.Create(ctx, other, service.CreateShoppingListInput{Name: "Unrelated"})
+	if err != nil {
+		t.Fatalf("Create: %v", err)
+	}
+	otherSub, err := lists.Subscribe(ctx, other, otherList.ID)
+	if err != nil {
+		t.Fatalf("Subscribe: %v", err)
+	}
+	defer otherSub.Close()
+
+	if err := f.svc.DeleteUser(ctx, owner); err != nil {
+		t.Fatalf("DeleteUser of the owner: %v", err)
+	}
+
+	if !closedWithin(partnerSub) {
+		t.Error("the partner's stream on the deleted owner's list stayed open, want it closed (no list_deleted event is published for an account deletion)")
+	}
+	if !stillOpen(otherSub) {
+		t.Error("an unrelated user's stream was closed")
+	}
+	if _, err := lists.Get(ctx, partner, list.ID); !errors.Is(err, service.ErrShoppingListNotFound) {
+		t.Errorf("partner Get of the deleted owner's list: err = %v, want ErrShoppingListNotFound", err)
+	}
+	if _, err := partners.Get(ctx, partner); !errors.Is(err, service.ErrPartnerNotLinked) {
+		t.Errorf("partner's partnership after the owner's deletion: err = %v, want ErrPartnerNotLinked", err)
+	}
+}
+
+func TestDeleteUserRunsTheOnUserDeletedHookOnlyAfterACommittedDelete(t *testing.T) {
+	f := newFixture(t)
+	ctx := context.Background()
+	var got []uuid.UUID
+	f.svc.OnUserDeleted(func(id uuid.UUID) { got = append(got, id) })
+	s := register(t, f, "hook@example.com")
+
+	if err := f.svc.DeleteUser(ctx, uuid.New()); !errors.Is(err, service.ErrNotFound) {
+		t.Fatalf("DeleteUser of a missing user: err = %v, want ErrNotFound", err)
+	}
+	if len(got) != 0 {
+		t.Fatalf("hook ran for a delete that failed: %v", got)
+	}
+	if err := f.svc.DeleteUser(ctx, s.User.ID); err != nil {
+		t.Fatalf("DeleteUser: %v", err)
+	}
+	if len(got) != 1 || got[0] != s.User.ID {
+		t.Errorf("hook calls = %v, want exactly [%v]", got, s.User.ID)
+	}
+}
```

- [ ] **Step 2: Run them to see them fail**

Run: `cd backend && go test ./internal/service/ -run 'TestDeleteUser' -count=1`

Expected: FAIL, for the right reason:

```
internal/service/auth_test.go:633:8: f.svc.OnUserDeleted undefined (type *service.Auth has no field or method OnUserDeleted)
```

- [ ] **Step 3: Implement**

Modify `backend/internal/service/auth.go` (unified diff against the current file):

```diff
--- a/backend/internal/service/auth.go
+++ b/backend/internal/service/auth.go
@@ -77,6 +77,9 @@ type Auth struct {
 	tokens     *auth.TokenIssuer
 	refreshTTL time.Duration
 	now        func() time.Time
+	// onUserDeleted, when set, runs after an account is deleted. See
+	// OnUserDeleted.
+	onUserDeleted func(uuid.UUID)
 }
 
 // NewAuth returns an Auth service. now is injected so tests control time.
@@ -84,6 +87,12 @@ func NewAuth(st *store.Store, hasher *auth.Hasher, tokens *auth.TokenIssuer, ref
 	return &Auth{st: st, hasher: hasher, tokens: tokens, refreshTTL: refreshTTL, now: now}
 }
 
+// OnUserDeleted registers fn to run, after the transaction commits, with the id
+// of every account DeleteUser removes. cmd/api uses it to close the account's
+// shopping-list event streams (ListEventHub.CloseUser): DeleteUser removes
+// the user's lists with a raw DELETE, which publishes no list_deleted event.
+func (a *Auth) OnUserDeleted(fn func(uuid.UUID)) { a.onUserDeleted = fn }
+
 // Register creates an account and signs it in.
 func (a *Auth) Register(ctx context.Context, in RegisterInput) (Session, error) {
 	hash, err := a.hasher.Hash(in.Password)
@@ -265,8 +274,13 @@ func (a *Auth) UpdateUser(ctx context.Context, id uuid.UUID, in UpdateInput) (Us
 // rows (ingredient_id, checked_by) are ON DELETE SET NULL, not NO ACTION, so
 // no cascade order can make them fail. A future NO ACTION reference into
 // shopping_lists or shopping_items would change that.
+//
+// The user's partnership row (pending or active) needs no step of its own:
+// all three user references in partnerships cascade, which ends the
+// partner's access at once. Items the deleted user checked on a list they
+// did not own keep existing, with checked_by set to NULL.
 func (a *Auth) DeleteUser(ctx context.Context, id uuid.UUID) error {
-	return a.st.InTx(ctx, func(q *sqlc.Queries) error {
+	err := a.st.InTx(ctx, func(q *sqlc.Queries) error {
 		if err := q.DeleteShoppingListsForUser(ctx, id); err != nil {
 			return fmt.Errorf("delete shopping lists: %w", err)
 		}
@@ -288,6 +302,13 @@ func (a *Auth) DeleteUser(ctx context.Context, id uuid.UUID) error {
 		}
 		return nil
 	})
+	if err != nil {
+		return err
+	}
+	if a.onUserDeleted != nil {
+		a.onUserDeleted(id)
+	}
+	return nil
 }
 
 func (a *Auth) newSession(ctx context.Context, q *sqlc.Queries, user sqlc.User, family uuid.UUID) (Session, error) {
```

- [ ] **Step 4: Run the tests and see them pass**

Run: `cd backend && go test ./internal/service/ -run 'TestDeleteUser' -count=1`

Expected: PASS (`ok  	github.com/InzKazik/mealplanner/backend/internal/service`).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/service/auth.go backend/internal/service/auth_test.go
git commit -m "feat(backend): close a deleted account's event streams"
```

### Task 8: HTTP: the partner link routes and the accept limiter

**Files:**
- Modify: `openapi.yaml`
- Create: `backend/internal/httpapi/partner.go`
- Modify: `backend/internal/httpapi/problem.go`, `backend/internal/httpapi/account.go`, `backend/internal/httpapi/server.go`, `backend/internal/httpapi/router.go`, `backend/internal/httpapi/ratelimit.go`
- Modify: `backend/cmd/api/main.go`
- Modify: `backend/internal/httpapi/contract_test.go`
- Create: `backend/internal/httpapi/partner_flow_test.go`
- Generated: `backend/internal/api/api.gen.go`

**Interfaces:**
- Consumes: `Partners` and its errors (Task 3), `Auth.OnUserDeleted` (Task 7).
- Produces: operations `getPartner`, `unlinkPartner`, `createPartnerInvite`, `acceptPartnerInvite`; schemas `Partnership`, `PartnerInvite`, `AcceptPartnerInviteRequest`; `httpapi.PartnerService` and `Deps.Partners` (required); `httpapi.RateLimits.AcceptPerHour` (default 10); codes `partner_not_linked` (404), `partner_already_linked` (409), `invite_invalid` (404); test helpers `partnerEnv`, `newPartnerEnv`, `stubThreeUserTokens`, `stubPartners`.

Edit `openapi.yaml` first, then run `make generate`: the build stays red until the handlers exist, so the contract and its handlers land in one task. `POST /partner/accept` has its own limiter, keyed per user and, separately, per client IP, because an invite code has about 39 bits and the general per-user limit does not stop many accounts behind one IP. The IP limiter runs before routing (like `authIPLimiter`), the user limiter after the validator; both apply only to `/v1/partner/accept`. A description in `openapi.yaml` must not contain `: ` in a plain scalar (YAML reads it as a mapping); the wording below already avoids it.

- [ ] **Step 1: Write the failing tests**

Modify `backend/internal/httpapi/contract_test.go` (unified diff against the current file):

```diff
--- a/backend/internal/httpapi/contract_test.go
+++ b/backend/internal/httpapi/contract_test.go
@@ -95,6 +95,10 @@ type stubPlan struct{ httpapi.PlanService }
 // shopping lists service fail loudly if they do.
 type stubShoppingLists struct{ httpapi.ShoppingListsService }
 
+// stubPartners panics on any call, so tests that must not reach the partners
+// service fail loudly if they do.
+type stubPartners struct{ httpapi.PartnerService }
+
 func init() {
 	// kin-openapi ships no body decoder for text/event-stream, so validating
 	// the events stream's response would fail as an unsupported content type.
@@ -114,6 +118,7 @@ func newTestRouter(t *testing.T, mods ...func(*httpapi.Deps)) http.Handler {
 		DietTemplates: stubDietTemplates{},
 		Plan:          stubPlan{},
 		ShoppingLists: stubShoppingLists{},
+		Partners:      stubPartners{},
 		Tokens:        stubTokens{},
 	}
 	for _, m := range mods {
@@ -265,7 +270,7 @@ func TestNewRouterPanicsWithoutRequiredDependencies(t *testing.T) {
 	full := httpapi.Deps{
 		Logger: slog.New(slog.DiscardHandler), Ready: alwaysReady,
 		WebOrigin: "http://localhost:3000", Auth: stubAuth{}, Ingredients: stubIngredients{}, Meals: stubMeals{}, Tokens: stubTokens{}, DietTemplates: stubDietTemplates{}, Plan: stubPlan{},
-		ShoppingLists: stubShoppingLists{},
+		ShoppingLists: stubShoppingLists{}, Partners: stubPartners{},
 	}
 	tests := map[string]func(*httpapi.Deps){
 		"no logger":           func(d *httpapi.Deps) { d.Logger = nil },
@@ -276,6 +281,7 @@ func TestNewRouterPanicsWithoutRequiredDependencies(t *testing.T) {
 		"no diet templates":   func(d *httpapi.Deps) { d.DietTemplates = nil },
 		"no plan":             func(d *httpapi.Deps) { d.Plan = nil },
 		"no shopping lists":   func(d *httpapi.Deps) { d.ShoppingLists = nil },
+		"no partners":         func(d *httpapi.Deps) { d.Partners = nil },
 		"no tokens":           func(d *httpapi.Deps) { d.Tokens = nil },
 		"empty web origin":    func(d *httpapi.Deps) { d.WebOrigin = "" },
 		"wildcard web origin": func(d *httpapi.Deps) { d.WebOrigin = "*" },
```

Create `backend/internal/httpapi/partner_flow_test.go`:

```go
package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/InzKazik/mealplanner/backend/internal/api"
	"github.com/InzKazik/mealplanner/backend/internal/auth"
	"github.com/InzKazik/mealplanner/backend/internal/db"
	"github.com/InzKazik/mealplanner/backend/internal/httpapi"
	"github.com/InzKazik/mealplanner/backend/internal/service"
	"github.com/InzKazik/mealplanner/backend/internal/store"
	"github.com/InzKazik/mealplanner/backend/internal/store/sqlc"
	"github.com/InzKazik/mealplanner/backend/internal/testutil"
)

// partnerEnv is a router over the real services and Postgres with three
// users: Alice (token1), Bob (token2) and Carol (token3), not yet linked.
type partnerEnv struct {
	router                 http.Handler
	events                 *service.ListEventHub
	alice, bob, carol      uuid.UUID
	token1, token2, token3 string
}

type stubThreeUserTokens struct{ u1, u2, u3 uuid.UUID }

func (s stubThreeUserTokens) ParseAccess(token string) (uuid.UUID, error) {
	switch token {
	case "user1-token":
		return s.u1, nil
	case "user2-token":
		return s.u2, nil
	case "user3-token":
		return s.u3, nil
	}
	return uuid.Nil, auth.ErrInvalidAccessToken
}

func newPartnerEnv(t *testing.T) partnerEnv {
	t.Helper()
	pool, err := db.Connect(context.Background(), testutil.NewMigratedDatabase(t))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	st := store.New(pool)

	hash := "hash"
	newUser := func(email, name string) uuid.UUID {
		u, err := st.CreateUser(context.Background(), sqlc.CreateUserParams{Email: email, PasswordHash: &hash, DisplayName: name})
		if err != nil {
			t.Fatalf("create user %s: %v", email, err)
		}
		return u.ID
	}
	alice, bob, carol := newUser("alice@example.com", "Alice"), newUser("bob@example.com", "Bob"), newUser("carol@example.com", "Carol")

	meals := service.NewMeals(st)
	events := service.NewListEventHub()
	router := newTestRouter(t, func(d *httpapi.Deps) {
		d.Ingredients = service.NewIngredients(st)
		d.Meals = meals
		d.DietTemplates = service.NewDietTemplates(st)
		d.Plan = service.NewPlan(st, meals)
		d.ShoppingLists = service.NewShoppingLists(st, events)
		d.Partners = service.NewPartners(st, events, time.Now)
		d.Tokens = stubThreeUserTokens{u1: alice, u2: bob, u3: carol}
		d.Limits = httpapi.RateLimits{AuthPerMinute: 1000, UserPerMinute: 1000, AcceptPerHour: 1000}
	})
	return partnerEnv{
		router: router, events: events, alice: alice, bob: bob, carol: carol,
		token1: "user1-token", token2: "user2-token", token3: "user3-token",
	}
}

// link makes Alice and Bob partners through the real routes.
func (e partnerEnv) link(t *testing.T) {
	t.Helper()
	rec := contract(t, e.router, http.MethodPost, "/partner/invite", withBearer(e.token1))
	if rec.Code != http.StatusCreated {
		t.Fatalf("invite: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	invite := decodeAs[api.PartnerInvite](t, rec)
	rec = contract(t, e.router, http.MethodPost, "/partner/accept", withBearer(e.token2), withBody(`{"code":"`+invite.Code+`"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("accept: status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestPartnerLifecycle(t *testing.T) {
	e := newPartnerEnv(t)
	router := e.router

	rec := contract(t, router, http.MethodGet, "/partner", withBearer(e.token1))
	if rec.Code != http.StatusNotFound || problemCode(t, rec) != "partner_not_linked" {
		t.Fatalf("GET /partner without a partner: status = %d, body = %s, want 404 partner_not_linked", rec.Code, rec.Body.String())
	}

	rec = contract(t, router, http.MethodPost, "/partner/invite", withBearer(e.token1))
	if rec.Code != http.StatusCreated {
		t.Fatalf("invite: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	invite := decodeAs[api.PartnerInvite](t, rec)
	if len(invite.Code) != 8 {
		t.Errorf("invite code = %q, want 8 characters", invite.Code)
	}

	rec = contract(t, router, http.MethodGet, "/partner", withBearer(e.token1))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /partner with a pending invite: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	pending := decodeAs[api.Partnership](t, rec)
	if pending.Status != api.PartnershipStatusPending || pending.ExpiresAt.IsNull() || !pending.DisplayName.IsNull() || !pending.LinkedAt.IsNull() {
		t.Errorf("pending partnership = %+v, want status pending with only expires_at set", pending)
	}
	if strings.Contains(rec.Body.String(), invite.Code) {
		t.Error("GET /partner returned the invite code, want it shown only when the invite is created")
	}

	rec = contract(t, router, http.MethodPost, "/partner/accept", withBearer(e.token2), withBody(`{"code":"ZZZZZZZZ"}`))
	if rec.Code != http.StatusNotFound || problemCode(t, rec) != "invite_invalid" {
		t.Errorf("accept of a wrong code: status = %d, body = %s, want 404 invite_invalid", rec.Code, rec.Body.String())
	}
	rec = contract(t, router, http.MethodPost, "/partner/accept", withBearer(e.token1), withBody(`{"code":"`+invite.Code+`"}`))
	if rec.Code != http.StatusNotFound || problemCode(t, rec) != "invite_invalid" {
		t.Errorf("accept of the caller's own code: status = %d, body = %s, want 404 invite_invalid", rec.Code, rec.Body.String())
	}
	rec = contract(t, router, http.MethodPost, "/partner/accept", withBearer(e.token2), withBody(`{"code":""}`), withInvalidRequest())
	if rec.Code != http.StatusBadRequest {
		t.Errorf("accept of an empty code: status = %d, want 400", rec.Code)
	}

	rec = contract(t, router, http.MethodPost, "/partner/accept", withBearer(e.token2), withBody(`{"code":"`+strings.ToLower(invite.Code)+`"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("accept: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	accepted := decodeAs[api.Partnership](t, rec)
	if accepted.Status != api.PartnershipStatusActive || accepted.DisplayName.MustGet() != "Alice" || accepted.LinkedAt.IsNull() || !accepted.ExpiresAt.IsNull() {
		t.Errorf("accepted = %+v, want an active partnership with Alice", accepted)
	}
	rec = contract(t, router, http.MethodGet, "/partner", withBearer(e.token1))
	if got := decodeAs[api.Partnership](t, rec); rec.Code != http.StatusOK || got.DisplayName.MustGet() != "Bob" {
		t.Errorf("the inviter's view = %+v (status %d), want an active partnership with Bob", got, rec.Code)
	}

	// Linked users cannot invite again, or accept anyone's code.
	rec = contract(t, router, http.MethodPost, "/partner/invite", withBearer(e.token1))
	if rec.Code != http.StatusConflict || problemCode(t, rec) != "partner_already_linked" {
		t.Errorf("invite while linked: status = %d, body = %s, want 409 partner_already_linked", rec.Code, rec.Body.String())
	}
	rec = contract(t, router, http.MethodPost, "/partner/invite", withBearer(e.token3))
	carolInvite := decodeAs[api.PartnerInvite](t, rec)
	rec = contract(t, router, http.MethodPost, "/partner/accept", withBearer(e.token1), withBody(`{"code":"`+carolInvite.Code+`"}`))
	if rec.Code != http.StatusConflict || problemCode(t, rec) != "partner_already_linked" {
		t.Errorf("accept while linked: status = %d, body = %s, want 409 partner_already_linked", rec.Code, rec.Body.String())
	}
	rec = contract(t, router, http.MethodPost, "/partner/accept", withBearer(e.token3), withBody(`{"code":"`+carolInvite.Code+`"}`))
	if rec.Code != http.StatusNotFound || problemCode(t, rec) != "invite_invalid" {
		t.Errorf("accept of Carol's own code: status = %d, want 404 invite_invalid", rec.Code)
	}

	// Either side may unlink; both then have no partner.
	rec = contract(t, router, http.MethodDelete, "/partner", withBearer(e.token2))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("unlink: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rec = contract(t, router, http.MethodDelete, "/partner", withBearer(e.token1))
	if rec.Code != http.StatusNotFound || problemCode(t, rec) != "partner_not_linked" {
		t.Errorf("second unlink: status = %d, body = %s, want 404 partner_not_linked", rec.Code, rec.Body.String())
	}
	for _, token := range []string{e.token1, e.token2} {
		if rec := contract(t, router, http.MethodGet, "/partner", withBearer(token)); rec.Code != http.StatusNotFound {
			t.Errorf("GET /partner after the unlink: status = %d, want 404", rec.Code)
		}
	}

	// A pending invite is cancelled the same way.
	rec = contract(t, router, http.MethodDelete, "/partner", withBearer(e.token3))
	if rec.Code != http.StatusNoContent {
		t.Errorf("cancel a pending invite: status = %d, want 204", rec.Code)
	}
	if rec := contract(t, router, http.MethodPost, "/partner/accept", withBearer(e.token1), withBody(`{"code":"`+carolInvite.Code+`"}`)); rec.Code != http.StatusNotFound {
		t.Errorf("accept of a cancelled invite: status = %d, want 404", rec.Code)
	}
}

func TestPartnerRoutesRequireAuthentication(t *testing.T) {
	router := newTestRouter(t)
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/partner", ""},
		{http.MethodDelete, "/partner", ""},
		{http.MethodPost, "/partner/invite", ""},
		{http.MethodPost, "/partner/accept", `{"code":"ABCDEFGH"}`},
	} {
		opts := []requestOption{withInvalidRequest()}
		if tc.body != "" {
			opts = append(opts, withBody(tc.body))
		}
		rec := contract(t, router, tc.method, tc.path, opts...)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without a token: status = %d, want 401", tc.method, tc.path, rec.Code)
		}
	}
}

// rejectingPartners answers every Accept as a wrong code and every Get as no
// partner, so the limiter tests need no database.
type rejectingPartners struct{ httpapi.PartnerService }

func (rejectingPartners) Accept(context.Context, uuid.UUID, string) (service.Partnership, error) {
	return service.Partnership{}, service.ErrInviteInvalid
}

func (rejectingPartners) Get(context.Context, uuid.UUID) (service.Partnership, error) {
	return service.Partnership{}, service.ErrPartnerNotLinked
}

func TestPartnerAcceptIsRateLimitedPerUserAndPerIP(t *testing.T) {
	newRouterWithLimit := func(t *testing.T) http.Handler {
		return newTestRouter(t, func(d *httpapi.Deps) {
			d.Partners = rejectingPartners{}
			d.Limits = httpapi.RateLimits{AcceptPerHour: 2}
		})
	}
	accept := func(t *testing.T, h http.Handler, token, addr string) *struct{ code int } {
		t.Helper()
		rec := contract(t, h, http.MethodPost, "/partner/accept", withBearer(token), withRemoteAddr(addr), withBody(`{"code":"ABCDEFGH"}`))
		return &struct{ code int }{rec.Code}
	}

	t.Run("per user, whatever the IP", func(t *testing.T) {
		h := newRouterWithLimit(t)
		for i, ip := range []string{"198.51.100.1:1000", "198.51.100.2:1000"} {
			if got := accept(t, h, validToken, ip).code; got != http.StatusNotFound {
				t.Fatalf("attempt %d: status = %d, want 404", i+1, got)
			}
		}
		if got := accept(t, h, validToken, "198.51.100.3:1000").code; got != http.StatusTooManyRequests {
			t.Errorf("third attempt by one user from a fresh IP: status = %d, want 429", got)
		}
		if got := accept(t, h, validToken2, "198.51.100.4:1000").code; got != http.StatusNotFound {
			t.Errorf("another user's first attempt: status = %d, want 404 (the limit is per user)", got)
		}
	})

	t.Run("per IP, whatever the user", func(t *testing.T) {
		h := newRouterWithLimit(t)
		const ip = "203.0.113.7:1000"
		for i, token := range []string{validToken, validToken2} {
			if got := accept(t, h, token, ip).code; got != http.StatusNotFound {
				t.Fatalf("attempt %d: status = %d, want 404", i+1, got)
			}
		}
		if got := accept(t, h, validToken, ip).code; got != http.StatusTooManyRequests {
			t.Errorf("third attempt from one IP: status = %d, want 429", got)
		}
	})

	t.Run("only the accept route", func(t *testing.T) {
		h := newRouterWithLimit(t)
		for range 5 {
			rec := contract(t, h, http.MethodGet, "/partner", withBearer(validToken), withRemoteAddr("203.0.113.9:1000"))
			if rec.Code != http.StatusNotFound {
				t.Fatalf("GET /partner: status = %d, want 404 (never rate limited by the accept limits)", rec.Code)
			}
		}
	})
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `make lint-api && cd backend && go test ./internal/httpapi/ -run 'TestPartner|TestNewRouter' -count=1`

Expected: FAIL, for the right reason:

```
internal/httpapi/contract_test.go:100:35: undefined: httpapi.PartnerService
internal/httpapi/contract_test.go:121:3: unknown field Partners in struct literal of type httpapi.Deps
internal/httpapi/partner_flow_test.go:89:25: undefined: api.PartnerInvite
```

- [ ] **Step 3: Implement**

Modify `openapi.yaml` (unified diff against the current file):

```diff
--- a/openapi.yaml
+++ b/openapi.yaml
@@ -25,6 +25,8 @@ tags:
     description: The calendar of scheduled meals and their computed nutrition totals.
   - name: ShoppingLists
     description: Shopping lists generated from the plan or built by hand, with live updates over Server-Sent Events.
+  - name: Partner
+    description: Linking with one partner through an invite code, and reading what the partner has shared.
 paths:
   /ingredients:
     get:
@@ -984,6 +986,95 @@ paths:
           $ref: '#/components/responses/Problem'
         '503':
           $ref: '#/components/responses/Problem'
+  /partner:
+    get:
+      tags: [Partner]
+      operationId: getPartner
+      summary: Get the caller's partnership
+      description: Returns the active partnership (the partner's `display_name` and `linked_at`), or the caller's own pending invite (`expires_at`; the code is never returned again). `404 partner_not_linked` when there is neither, and also once a pending invite has expired.
+      responses:
+        '200':
+          description: The partnership.
+          content:
+            application/json:
+              schema:
+                $ref: '#/components/schemas/Partnership'
+        '401':
+          $ref: '#/components/responses/Unauthorized'
+        '404':
+          $ref: '#/components/responses/NotFound'
+        '429':
+          $ref: '#/components/responses/TooManyRequests'
+        '500':
+          $ref: '#/components/responses/Problem'
+    delete:
+      tags: [Partner]
+      operationId: unlinkPartner
+      summary: End the partnership or cancel a pending invite
+      description: Either side may unlink. Access ends at once, so the former partner's next read of shared meals, diet templates and shopping lists is `404`, and their open shopping-list event streams are closed. Every list, meal and template stays with its owner; copies are independent. Re-linking needs a fresh invite.
+      responses:
+        '204':
+          description: The link was ended, or the pending invite cancelled.
+        '401':
+          $ref: '#/components/responses/Unauthorized'
+        '404':
+          $ref: '#/components/responses/NotFound'
+        '429':
+          $ref: '#/components/responses/TooManyRequests'
+        '500':
+          $ref: '#/components/responses/Problem'
+  /partner/invite:
+    post:
+      tags: [Partner]
+      operationId: createPartnerInvite
+      summary: Create an invite code
+      description: Creates an invite for the caller and returns its code, which is shown only here (only its hash is stored). The code has 8 characters from `23456789ABCDEFGHJKMNPQRSTUVWXYZ` and expires after 48 hours. A new invite replaces the caller's pending one. `409 partner_already_linked` if the caller already has a partner.
+      responses:
+        '201':
+          description: The invite.
+          content:
+            application/json:
+              schema:
+                $ref: '#/components/schemas/PartnerInvite'
+        '401':
+          $ref: '#/components/responses/Unauthorized'
+        '409':
+          $ref: '#/components/responses/Conflict'
+        '429':
+          $ref: '#/components/responses/TooManyRequests'
+        '500':
+          $ref: '#/components/responses/Problem'
+  /partner/accept:
+    post:
+      tags: [Partner]
+      operationId: acceptPartnerInvite
+      summary: Accept an invite code
+      description: Links the caller with the user who issued the code. A wrong, expired, own or already used code all return the same `404 invite_invalid`. A caller who already has a partner gets `409 partner_already_linked` whatever the code. Case, spaces and dashes in the code do not matter. This route has its own, tighter rate limit (per user and per client IP).
+      requestBody:
+        required: true
+        content:
+          application/json:
+            schema:
+              $ref: '#/components/schemas/AcceptPartnerInviteRequest'
+      responses:
+        '200':
+          description: The new partnership.
+          content:
+            application/json:
+              schema:
+                $ref: '#/components/schemas/Partnership'
+        '400':
+          $ref: '#/components/responses/BadRequest'
+        '401':
+          $ref: '#/components/responses/Unauthorized'
+        '404':
+          $ref: '#/components/responses/NotFound'
+        '409':
+          $ref: '#/components/responses/Conflict'
+        '429':
+          $ref: '#/components/responses/TooManyRequests'
+        '500':
+          $ref: '#/components/responses/Problem'
   /healthz:
     get:
       tags: [Health]
@@ -2099,6 +2190,44 @@ components:
           type: string
         current:
           $ref: '#/components/schemas/ShoppingItem'
+    Partnership:
+      type: object
+      description: The caller's partnership. While `status` is `active`, `display_name` is the partner's name and `linked_at` says since when; while it is `pending`, `expires_at` is when the invite code stops working.
+      required: [status, display_name, linked_at, expires_at]
+      properties:
+        status:
+          type: string
+          enum: [pending, active]
+        display_name:
+          type: string
+          nullable: true
+        linked_at:
+          type: string
+          format: date-time
+          nullable: true
+        expires_at:
+          type: string
+          format: date-time
+          nullable: true
+    PartnerInvite:
+      type: object
+      required: [code, expires_at]
+      properties:
+        code:
+          type: string
+          description: Shown once; only its hash is stored.
+        expires_at:
+          type: string
+          format: date-time
+    AcceptPartnerInviteRequest:
+      type: object
+      additionalProperties: false
+      required: [code]
+      properties:
+        code:
+          type: string
+          minLength: 1
+          maxLength: 64
   responses:
     Problem:
       description: An error occurred.
```

Create `backend/internal/httpapi/partner.go`:

```go
package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"

	"github.com/InzKazik/mealplanner/backend/internal/api"
	"github.com/InzKazik/mealplanner/backend/internal/service"
)

// PartnerService is what the partner handlers need from the partners service.
type PartnerService interface {
	Invite(ctx context.Context, callerID uuid.UUID) (service.Invite, error)
	Accept(ctx context.Context, callerID uuid.UUID, code string) (service.Partnership, error)
	Get(ctx context.Context, callerID uuid.UUID) (service.Partnership, error)
	Unlink(ctx context.Context, callerID uuid.UUID) error
}

func (s *server) CreatePartnerInvite(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	inv, err := s.partners.Invite(r.Context(), userID)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, api.PartnerInvite{Code: inv.Code, ExpiresAt: inv.ExpiresAt})
}

func (s *server) AcceptPartnerInvite(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req api.AcceptPartnerInviteRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	p, err := s.partners.Accept(r.Context(), userID, req.Code)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPIPartnership(p))
}

func (s *server) GetPartner(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	p, err := s.partners.Get(r.Context(), userID)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPIPartnership(p))
}

func (s *server) UnlinkPartner(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	if err := s.partners.Unlink(r.Context(), userID); err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func toAPIPartnership(p service.Partnership) api.Partnership {
	return api.Partnership{
		Status:      api.PartnershipStatus(p.Status),
		DisplayName: toNullableString(p.DisplayName),
		LinkedAt:    toNullableTime(p.LinkedAt),
		ExpiresAt:   toNullableTime(p.ExpiresAt),
	}
}

func toNullableTime(v *time.Time) nullable.Nullable[time.Time] {
	if v == nil {
		return nullable.NewNullNullable[time.Time]()
	}
	return nullable.NewNullableWithValue(*v)
}
```

Modify `backend/internal/httpapi/problem.go` (unified diff against the current file):

```diff
--- a/backend/internal/httpapi/problem.go
+++ b/backend/internal/httpapi/problem.go
@@ -7,28 +7,31 @@ import (
 
 // Stable, machine-readable problem codes. Clients map these to localized text.
 const (
-	CodeValidationFailed    = "validation_failed"
-	CodeNotFound            = "not_found"
-	CodeMethodNotAllowed    = "method_not_allowed"
-	CodeNotReady            = "not_ready"
-	CodeInternal            = "internal_error"
-	CodeUnauthorized        = "unauthorized"
-	CodeRateLimited         = "rate_limited"
-	CodeEmailTaken          = "email_taken"
-	CodeInvalidCredentials  = "invalid_credentials" //nolint:gosec // an error code, not a credential
-	CodeInvalidRefreshToken = "invalid_refresh_token"
-	CodeIngredientInUse     = "ingredient_in_use"
-	CodeInvalidIngredient   = "invalid_ingredient"
-	CodeUnitNotConvertible  = "unit_not_convertible"
-	CodeMealInUse           = "meal_in_use"
-	CodeDayIndexOutOfRange  = "day_index_out_of_range"
-	CodeInvalidMeal         = "invalid_meal"
-	CodeDuplicateSlot       = "duplicate_slot"
-	CodePlanConflict        = "plan_conflict"
-	CodePlanRangeTooLong    = "plan_range_too_long"
-	CodePlanRangeInvalid    = "plan_range_invalid"
-	CodeVersionConflict     = "version_conflict"
-	CodeVersionRequired     = "version_required"
+	CodeValidationFailed     = "validation_failed"
+	CodeNotFound             = "not_found"
+	CodeMethodNotAllowed     = "method_not_allowed"
+	CodeNotReady             = "not_ready"
+	CodeInternal             = "internal_error"
+	CodeUnauthorized         = "unauthorized"
+	CodeRateLimited          = "rate_limited"
+	CodeEmailTaken           = "email_taken"
+	CodeInvalidCredentials   = "invalid_credentials" //nolint:gosec // an error code, not a credential
+	CodeInvalidRefreshToken  = "invalid_refresh_token"
+	CodeIngredientInUse      = "ingredient_in_use"
+	CodeInvalidIngredient    = "invalid_ingredient"
+	CodeUnitNotConvertible   = "unit_not_convertible"
+	CodeMealInUse            = "meal_in_use"
+	CodeDayIndexOutOfRange   = "day_index_out_of_range"
+	CodeInvalidMeal          = "invalid_meal"
+	CodeDuplicateSlot        = "duplicate_slot"
+	CodePlanConflict         = "plan_conflict"
+	CodePlanRangeTooLong     = "plan_range_too_long"
+	CodePlanRangeInvalid     = "plan_range_invalid"
+	CodeVersionConflict      = "version_conflict"
+	CodeVersionRequired      = "version_required"
+	CodePartnerNotLinked     = "partner_not_linked"
+	CodePartnerAlreadyLinked = "partner_already_linked"
+	CodeInviteInvalid        = "invite_invalid"
 )
 
 // Stable codes for FieldError.Code.
```

Modify `backend/internal/httpapi/account.go` (unified diff against the current file):

```diff
--- a/backend/internal/httpapi/account.go
+++ b/backend/internal/httpapi/account.go
@@ -199,6 +199,12 @@ func (s *server) writeServiceError(w http.ResponseWriter, r *http.Request, err e
 		WriteProblem(w, http.StatusBadRequest, CodeInvalidIngredient, "")
 	case errors.Is(err, service.ErrShoppingItemVersionRequired):
 		WriteProblem(w, http.StatusBadRequest, CodeVersionRequired, "version is required to change name, quantity, unit or category")
+	case errors.Is(err, service.ErrPartnerNotLinked):
+		WriteProblem(w, http.StatusNotFound, CodePartnerNotLinked, "")
+	case errors.Is(err, service.ErrPartnerAlreadyLinked):
+		WriteProblem(w, http.StatusConflict, CodePartnerAlreadyLinked, "")
+	case errors.Is(err, service.ErrInviteInvalid):
+		WriteProblem(w, http.StatusNotFound, CodeInviteInvalid, "")
 	case errors.Is(err, service.ErrEventStreamsClosed):
 		// Only reachable while the server is shutting down.
 		WriteProblem(w, http.StatusServiceUnavailable, CodeNotReady, "the server is shutting down")
```

Modify `backend/internal/httpapi/server.go` (unified diff against the current file):

```diff
--- a/backend/internal/httpapi/server.go
+++ b/backend/internal/httpapi/server.go
@@ -22,6 +22,7 @@ type server struct {
 	dietTemplates DietTemplatesService
 	plan          PlanService
 	shoppingLists ShoppingListsService
+	partners      PartnerService
 }
 
 var _ api.ServerInterface = (*server)(nil)
```

Modify `backend/internal/httpapi/router.go` (unified diff against the current file):

```diff
--- a/backend/internal/httpapi/router.go
+++ b/backend/internal/httpapi/router.go
@@ -37,6 +37,8 @@ type Deps struct {
 	// ShoppingLists implements the shopping-list endpoints and their event
 	// streams.
 	ShoppingLists ShoppingListsService
+	// Partners implements the partner link endpoints.
+	Partners PartnerService
 	// Tokens validates access tokens for secured operations.
 	Tokens TokenParser
 	// Limits are the rate limits; zero values use the defaults.
@@ -49,8 +51,8 @@ type Deps struct {
 // NewRouter returns the root handler with every /v1 route and all middleware.
 func NewRouter(d Deps) http.Handler {
 	if d.Logger == nil || d.Ready == nil || d.Auth == nil || d.Ingredients == nil || d.Meals == nil || d.DietTemplates == nil || d.Plan == nil ||
-		d.ShoppingLists == nil || d.Tokens == nil || d.WebOrigin == "" || d.WebOrigin == "*" {
-		panic("httpapi: Deps.Logger, Ready, Auth, Ingredients, Meals, DietTemplates, Plan, ShoppingLists and Tokens are required, and WebOrigin must be a single origin (not empty or *)")
+		d.ShoppingLists == nil || d.Partners == nil || d.Tokens == nil || d.WebOrigin == "" || d.WebOrigin == "*" {
+		panic("httpapi: Deps.Logger, Ready, Auth, Ingredients, Meals, DietTemplates, Plan, ShoppingLists, Partners and Tokens are required, and WebOrigin must be a single origin (not empty or *)")
 	}
 	limits := d.Limits.withDefaults()
 	spec, err := api.GetSpec()
@@ -76,6 +78,7 @@ func NewRouter(d Deps) http.Handler {
 		MaxAge:         300,
 	}))
 	r.Use(authIPLimiter(limits.AuthPerMinute))
+	r.Use(acceptIPLimiter(limits.AcceptPerHour))
 
 	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
 		WriteProblem(w, http.StatusNotFound, CodeNotFound, "")
@@ -89,15 +92,16 @@ func NewRouter(d Deps) http.Handler {
 
 	srv := &server{
 		logger: d.Logger, ready: d.Ready, auth: d.Auth, ingredients: d.Ingredients, meals: d.Meals,
-		dietTemplates: d.DietTemplates, plan: d.Plan, shoppingLists: d.ShoppingLists,
+		dietTemplates: d.DietTemplates, plan: d.Plan, shoppingLists: d.ShoppingLists, partners: d.Partners,
 	}
 	api.HandlerWithOptions(srv, api.ChiServerOptions{
 		BaseURL:    "/v1",
 		BaseRouter: r,
 		// The generated wrapper applies these in order, so the last one is the
 		// outermost: the validator (which authenticates) runs first, then the
-		// per-user rate limit, then the handler.
+		// per-user rate limits, then the handler.
 		Middlewares: []api.MiddlewareFunc{
+			acceptUserLimiter(limits.AcceptPerHour),
 			userLimiter(limits.UserPerMinute),
 			openAPIValidator(spec, d.Tokens, d.Logger),
 		},
```

Modify `backend/internal/httpapi/ratelimit.go` (unified diff against the current file):

```diff
--- a/backend/internal/httpapi/ratelimit.go
+++ b/backend/internal/httpapi/ratelimit.go
@@ -17,11 +17,20 @@ type RateLimits struct {
 	AuthPerMinute int
 	// UserPerMinute limits every other authenticated request per user.
 	UserPerMinute int
+	// AcceptPerHour limits POST /v1/partner/accept, per user and, separately,
+	// per client IP. An invite code has about 39 bits, so guessing one is only
+	// hopeless while this stays small and covers many accounts behind one IP.
+	AcceptPerHour int
 }
 
 const (
 	defaultAuthPerMinute = 10
 	defaultUserPerMinute = 300
+	defaultAcceptPerHour = 10
+	// acceptWindow is the window AcceptPerHour counts over.
+	acceptWindow = time.Hour
+	// acceptPath is the one route the accept limiters apply to.
+	acceptPath = "/v1/partner/accept"
 )
 
 func (l RateLimits) withDefaults() RateLimits {
@@ -31,6 +40,9 @@ func (l RateLimits) withDefaults() RateLimits {
 	if l.UserPerMinute <= 0 {
 		l.UserPerMinute = defaultUserPerMinute
 	}
+	if l.AcceptPerHour <= 0 {
+		l.AcceptPerHour = defaultAcceptPerHour
+	}
 	return l
 }
 
@@ -83,3 +95,53 @@ func userLimiter(perMinute int) func(http.Handler) http.Handler {
 		})
 	}
 }
+
+// acceptIPLimiter limits POST /v1/partner/accept per client IP. Like
+// authIPLimiter it runs before routing and validation, so unauthenticated and
+// malformed attempts count too, and IPv6 clients are bucketed by their /64.
+// Counters are in memory: with several API replicas the effective limit is
+// per replica.
+func acceptIPLimiter(perHour int) func(http.Handler) http.Handler {
+	limit := httprate.LimitBy(perHour, acceptWindow,
+		func(r *http.Request) (string, error) { return rateLimitKey(ClientIP(r.Context())), nil },
+		httprate.WithLimitHandler(rateLimited))
+	return onlyForPath(acceptPath, limit)
+}
+
+// acceptUserLimiter limits POST /v1/partner/accept per user. It must run after
+// the validator, which is what identifies the user; unauthenticated requests
+// pass through untouched (acceptIPLimiter has already counted them).
+func acceptUserLimiter(perHour int) func(http.Handler) http.Handler {
+	limit := httprate.LimitBy(perHour, acceptWindow,
+		func(r *http.Request) (string, error) {
+			id, _ := UserID(r.Context())
+			return id.String(), nil
+		},
+		httprate.WithLimitHandler(rateLimited))
+	authenticated := func(next http.Handler) http.Handler {
+		limited := limit(next)
+		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
+			if _, ok := UserID(r.Context()); ok {
+				limited.ServeHTTP(w, r)
+				return
+			}
+			next.ServeHTTP(w, r)
+		})
+	}
+	return onlyForPath(acceptPath, authenticated)
+}
+
+// onlyForPath applies mw to requests for exactly path and passes every other
+// request straight through.
+func onlyForPath(path string, mw func(http.Handler) http.Handler) func(http.Handler) http.Handler {
+	return func(next http.Handler) http.Handler {
+		wrapped := mw(next)
+		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
+			if r.URL.Path == path {
+				wrapped.ServeHTTP(w, r)
+				return
+			}
+			next.ServeHTTP(w, r)
+		})
+	}
+}
```

Modify `backend/cmd/api/main.go` (unified diff against the current file):

```diff
--- a/backend/cmd/api/main.go
+++ b/backend/cmd/api/main.go
@@ -79,6 +79,10 @@ func serve(ctx context.Context, cfg config.Config, logger *slog.Logger, ln net.L
 	plan := service.NewPlan(st, meals)
 	listEvents := service.NewListEventHub()
 	shoppingLists := service.NewShoppingLists(st, listEvents)
+	partners := service.NewPartners(st, listEvents, time.Now)
+	// DeleteUser removes an account's lists with a raw DELETE, which publishes
+	// no list_deleted event, so the hub is told to close the streams itself.
+	accounts.OnUserDeleted(listEvents.CloseUser)
 
 	srv := &http.Server{
 		Handler: httpapi.NewRouter(httpapi.Deps{
@@ -91,6 +95,7 @@ func serve(ctx context.Context, cfg config.Config, logger *slog.Logger, ln net.L
 			DietTemplates:  dietTemplates,
 			Plan:           plan,
 			ShoppingLists:  shoppingLists,
+			Partners:       partners,
 			Tokens:         tokens,
 			TrustedProxies: cfg.TrustedProxies,
 		}),
```

- [ ] **Step 4: Regenerate**

Run `make generate` from the repo root. It rewrites `backend/internal/store/sqlc/` and `backend/internal/api/api.gen.go`. Commit the output and never edit it by hand. `make check-generated` fails if it is stale.

- [ ] **Step 5: Run the tests and see them pass**

Run: `make lint-api && cd backend && go test ./internal/httpapi/ -run 'TestPartner|TestNewRouter' -count=1`

Expected: PASS (`ok  	github.com/InzKazik/mealplanner/backend/internal/httpapi`).

- [ ] **Step 6: Commit**

```bash
git add backend/cmd/api/main.go backend/internal/api/api.gen.go backend/internal/httpapi/account.go backend/internal/httpapi/contract_test.go backend/internal/httpapi/partner.go backend/internal/httpapi/partner_flow_test.go backend/internal/httpapi/problem.go backend/internal/httpapi/ratelimit.go backend/internal/httpapi/router.go backend/internal/httpapi/server.go openapi.yaml
git commit -m "feat(api): partner invite, accept, get and unlink routes"
```

### Task 9: HTTP: the partner listings and `is_owner`

**Files:**
- Modify: `openapi.yaml`
- Modify: `backend/internal/httpapi/meals.go`, `backend/internal/httpapi/diet_templates.go`, `backend/internal/httpapi/shopping_lists.go`
- Modify: `backend/internal/httpapi/partner_flow_test.go`
- Generated: `backend/internal/api/api.gen.go`

**Interfaces:**
- Consumes: `Meals.ListPartner`, `DietTemplates.ListPartner`, `ShoppingLists.ListPartner`, the `OwnerID` fields (Tasks 4 to 6), `newPartnerEnv` (Task 8).
- Produces: operations `listPartnerMeals`, `listPartnerDietTemplates`, `listPartnerShoppingLists`; `is_owner` (required boolean) on `Meal`, `DietTemplate` and `ShoppingList`; the three service interfaces in `httpapi` gain `ListPartner`; `toAPIMeal(m, viewer)`, `toAPIDietTemplate(t, viewer)`, `toAPIShoppingList(l, viewer)`.

`is_owner` is `OwnerID == viewer`, so `Create`, `Update` and every read of one's own resource say `true`, and a partner's says `false`. The three list handlers share one helper each with the own-list handlers; only the service call differs. The descriptions of `listMeals`, `getMeal`, `updateMeal`, `copyMeal`, `listDietTemplates`, `getDietTemplate`, `copyDietTemplate`, `listShoppingLists` and `getShoppingList` change so the contract says who can see and do what.

- [ ] **Step 1: Write the failing tests**

Modify `backend/internal/httpapi/partner_flow_test.go` (unified diff against the current file):

```diff
--- a/backend/internal/httpapi/partner_flow_test.go
+++ b/backend/internal/httpapi/partner_flow_test.go
@@ -3,6 +3,7 @@ package httpapi_test
 import (
 	"context"
 	"net/http"
+	"net/http/httptest"
 	"strings"
 	"testing"
 	"time"
@@ -272,3 +273,242 @@ func TestPartnerAcceptIsRateLimitedPerUserAndPerIP(t *testing.T) {
 		}
 	})
 }
+
+// create posts body to path as token and returns the decoded 201 response.
+func create[T any](t *testing.T, router http.Handler, path, token, body string) T {
+	t.Helper()
+	rec := contract(t, router, http.MethodPost, path, withBearer(token), withBody(body))
+	if rec.Code != http.StatusCreated {
+		t.Fatalf("POST %s: status = %d, body = %s", path, rec.Code, rec.Body.String())
+	}
+	return decodeAs[T](t, rec)
+}
+
+// TestPartnerSharedMealsAndTemplatesThroughTheRoutes drives the read-only-plus-
+// copy rules over HTTP: what a partner sees, what is 404, and what a copy
+// carries over.
+func TestPartnerSharedMealsAndTemplatesThroughTheRoutes(t *testing.T) {
+	e := newPartnerEnv(t)
+	router := e.router
+	e.link(t)
+
+	// Alice: a custom ingredient, a shared meal using it, a private meal, and a
+	// shared template over the private meal.
+	flour := create[api.Ingredient](t, router, "/ingredients", e.token1,
+		`{"name":"Alice's Flour","category":"grains_bread","nutrients":{"calories":360}}`)
+	shared := create[api.Meal](t, router, "/meals", e.token1, `{"name":"Flatbread","servings":2,"shared_with_partner":true}`)
+	private := create[api.Meal](t, router, "/meals", e.token1, `{"name":"Secret Snack","servings":1}`)
+	rec := contract(t, router, http.MethodPut, "/meals/"+shared.Id.String()+"/ingredients", withBearer(e.token1),
+		withBody(`{"items":[{"ingredient_id":"`+flour.Id.String()+`","quantity":200,"unit":"g"}]}`))
+	if rec.Code != http.StatusOK {
+		t.Fatalf("replace ingredients: status = %d, body = %s", rec.Code, rec.Body.String())
+	}
+	template := create[api.DietTemplate](t, router, "/diet-templates", e.token1, `{"name":"Baking Week","day_count":2,"shared_with_partner":true}`)
+	rec = contract(t, router, http.MethodPut, "/diet-templates/"+template.Id.String()+"/slots", withBearer(e.token1),
+		withBody(`{"items":[{"day_index":0,"slot":"breakfast","meal_id":"`+private.Id.String()+`"}]}`))
+	if rec.Code != http.StatusOK {
+		t.Fatalf("replace slots: status = %d, body = %s", rec.Code, rec.Body.String())
+	}
+
+	// Meals: Bob reads the shared one (is_owner false), not the private one.
+	mealPath := "/meals/" + shared.Id.String()
+	rec = contract(t, router, http.MethodGet, mealPath, withBearer(e.token2))
+	got := decodeAs[api.Meal](t, rec)
+	if rec.Code != http.StatusOK || got.IsOwner || got.Ingredients[0].IngredientName != "Alice's Flour" || got.NutritionPerServing.Calories.MustGet() != 360 {
+		t.Errorf("partner GET of the shared meal: status = %d, %+v, want it readable with is_owner false and 360 kcal per serving", rec.Code, got)
+	}
+	if rec = contract(t, router, http.MethodGet, mealPath, withBearer(e.token1)); !decodeAs[api.Meal](t, rec).IsOwner {
+		t.Error("the owner's GET has is_owner false, want true")
+	}
+	if rec = contract(t, router, http.MethodGet, "/meals/"+private.Id.String(), withBearer(e.token2)); rec.Code != http.StatusNotFound {
+		t.Errorf("partner GET of a private meal: status = %d, want 404", rec.Code)
+	}
+	if rec = contract(t, router, http.MethodGet, mealPath, withBearer(e.token3)); rec.Code != http.StatusNotFound {
+		t.Errorf("stranger GET of a shared meal: status = %d, want 404", rec.Code)
+	}
+	for _, tc := range []struct{ method, path, body string }{
+		{http.MethodPatch, mealPath, `{"name":"Mine"}`},
+		{http.MethodDelete, mealPath, ""},
+		{http.MethodPut, mealPath + "/ingredients", `{"items":[]}`},
+	} {
+		opts := []requestOption{withBearer(e.token2)}
+		if tc.body != "" {
+			opts = append(opts, withBody(tc.body))
+		}
+		if rec := contract(t, router, tc.method, tc.path, opts...); rec.Code != http.StatusNotFound {
+			t.Errorf("partner %s %s: status = %d, want 404 (read-only + copy)", tc.method, tc.path, rec.Code)
+		}
+	}
+
+	// Listings: own lists stay own; partner lists are separate.
+	rec = contract(t, router, http.MethodGet, "/meals", withBearer(e.token2))
+	if items := decodeAs[api.MealList](t, rec).Items; len(items) != 0 {
+		t.Errorf("Bob's own /meals = %+v, want empty", items)
+	}
+	rec = contract(t, router, http.MethodGet, "/partner/meals", withBearer(e.token2))
+	if items := decodeAs[api.MealList](t, rec).Items; rec.Code != http.StatusOK || len(items) != 1 || items[0].Id != shared.Id {
+		t.Errorf("Bob's /partner/meals = %+v (status %d), want only the shared meal", items, rec.Code)
+	}
+	rec = contract(t, router, http.MethodGet, "/partner/meals", withBearer(e.token3))
+	if rec.Code != http.StatusNotFound || problemCode(t, rec) != "partner_not_linked" {
+		t.Errorf("stranger /partner/meals: status = %d, body = %s, want 404 partner_not_linked", rec.Code, rec.Body.String())
+	}
+	if rec = contract(t, router, http.MethodGet, "/partner/meals?cursor=not-a-cursor", withBearer(e.token2)); rec.Code != http.StatusBadRequest {
+		t.Errorf("partner meals with a bad cursor: status = %d, want 400", rec.Code)
+	}
+
+	// Copy a partner meal: Bob's own private meal, on a duplicated ingredient.
+	cp := create[api.Meal](t, router, mealPath+"/copy", e.token2, "")
+	if !cp.IsOwner || cp.SharedWithPartner || cp.Ingredients[0].IngredientId == flour.Id || cp.NutritionPerServing.Calories.MustGet() != 360 {
+		t.Errorf("copy = %+v, want Bob's private meal on a duplicated flour with the same 360 kcal per serving", cp)
+	}
+	if rec = contract(t, router, http.MethodPost, "/meals/"+private.Id.String()+"/copy", withBearer(e.token2)); rec.Code != http.StatusNotFound {
+		t.Errorf("copy of a private meal: status = %d, want 404", rec.Code)
+	}
+
+	// Templates: readable with the slot's meal name, its meal still 404, copy carries the meal.
+	tplPath := "/diet-templates/" + template.Id.String()
+	rec = contract(t, router, http.MethodGet, tplPath, withBearer(e.token2))
+	gotTpl := decodeAs[api.DietTemplate](t, rec)
+	if rec.Code != http.StatusOK || gotTpl.IsOwner || len(gotTpl.Slots) != 1 || gotTpl.Slots[0].MealName != "Secret Snack" {
+		t.Errorf("partner GET of the shared template: status = %d, %+v, want it readable, is_owner false, with the slot's meal name", rec.Code, gotTpl)
+	}
+	if rec = contract(t, router, http.MethodPost, tplPath+"/apply", withBearer(e.token2), withBody(`{"start_date":"2026-06-01"}`)); rec.Code != http.StatusNotFound {
+		t.Errorf("partner apply of Alice's template: status = %d, want 404 (copy it first)", rec.Code)
+	}
+	rec = contract(t, router, http.MethodGet, "/partner/diet-templates", withBearer(e.token2))
+	if items := decodeAs[api.DietTemplateList](t, rec).Items; rec.Code != http.StatusOK || len(items) != 1 || items[0].Id != template.Id {
+		t.Errorf("Bob's /partner/diet-templates = %+v (status %d), want the shared template", items, rec.Code)
+	}
+	tplCopy := create[api.DietTemplate](t, router, tplPath+"/copy", e.token2, "")
+	if !tplCopy.IsOwner || tplCopy.SharedWithPartner || len(tplCopy.Slots) != 1 || tplCopy.Slots[0].MealId == private.Id {
+		t.Errorf("template copy = %+v, want Bob's private copy whose slot points at a meal copy", tplCopy)
+	}
+	if rec = contract(t, router, http.MethodPost, "/diet-templates/"+tplCopy.Id.String()+"/apply", withBearer(e.token2), withBody(`{"start_date":"2026-06-01"}`)); rec.Code != http.StatusNoContent {
+		t.Errorf("apply of the copy: status = %d, body = %s, want 204", rec.Code, rec.Body.String())
+	}
+
+	// Unlinking ends all of it at once.
+	if rec = contract(t, router, http.MethodDelete, "/partner", withBearer(e.token1)); rec.Code != http.StatusNoContent {
+		t.Fatalf("unlink: status = %d", rec.Code)
+	}
+	if rec = contract(t, router, http.MethodGet, mealPath, withBearer(e.token2)); rec.Code != http.StatusNotFound {
+		t.Errorf("partner GET of a shared meal after the unlink: status = %d, want 404", rec.Code)
+	}
+	if rec = contract(t, router, http.MethodGet, "/partner/diet-templates", withBearer(e.token2)); rec.Code != http.StatusNotFound {
+		t.Errorf("/partner/diet-templates after the unlink: status = %d, want 404", rec.Code)
+	}
+	if rec = contract(t, router, http.MethodGet, "/meals/"+cp.Id.String(), withBearer(e.token2)); rec.Code != http.StatusOK {
+		t.Errorf("Bob's copy after the unlink: status = %d, want 200 (copies are independent)", rec.Code)
+	}
+}
+
+// TestPartnerSharedShoppingListsThroughTheRoutes covers the shopping-list
+// half: both partners edit items, list-level actions are owner-only, and the
+// list keeps its owner after an unlink.
+func TestPartnerSharedShoppingListsThroughTheRoutes(t *testing.T) {
+	e := newPartnerEnv(t)
+	router := e.router
+	e.link(t)
+
+	shared := create[api.ShoppingList](t, router, "/shopping-lists", e.token1, `{"name":"Groceries","shared_with_partner":true}`)
+	create[api.ShoppingList](t, router, "/shopping-lists", e.token1, `{"name":"Private"}`)
+	listPath := "/shopping-lists/" + shared.Id.String()
+	milk := create[api.ShoppingItem](t, router, listPath+"/items", e.token1, `{"name":"Milk"}`)
+
+	rec := contract(t, router, http.MethodGet, listPath, withBearer(e.token2))
+	if got := decodeAs[api.ShoppingList](t, rec); rec.Code != http.StatusOK || got.IsOwner || len(got.Items) != 1 {
+		t.Errorf("partner GET of the shared list = %+v (status %d), want it readable with is_owner false", got, rec.Code)
+	}
+	rec = contract(t, router, http.MethodGet, "/partner/shopping-lists", withBearer(e.token2))
+	if items := decodeAs[api.ShoppingListPage](t, rec).Items; rec.Code != http.StatusOK || len(items) != 1 || items[0].Id != shared.Id {
+		t.Errorf("Bob's /partner/shopping-lists = %+v (status %d), want only the shared list", items, rec.Code)
+	}
+	rec = contract(t, router, http.MethodGet, "/shopping-lists", withBearer(e.token2))
+	if items := decodeAs[api.ShoppingListPage](t, rec).Items; len(items) != 0 {
+		t.Errorf("Bob's own /shopping-lists = %+v, want empty", items)
+	}
+
+	// The partner adds, checks, edits and deletes items.
+	eggs := create[api.ShoppingItem](t, router, listPath+"/items", e.token2, `{"name":"Eggs"}`)
+	if eggs.Origin != api.ShoppingItemOriginManual {
+		t.Errorf("partner-added item origin = %q, want manual", eggs.Origin)
+	}
+	rec = contract(t, router, http.MethodPatch, listPath+"/items/"+milk.Id.String(), withBearer(e.token2), withBody(`{"checked":true}`))
+	checked := decodeAs[api.ShoppingItem](t, rec)
+	if rec.Code != http.StatusOK || !checked.Checked || checked.CheckedBy.MustGet() != e.bob {
+		t.Errorf("partner check = %+v (status %d), want checked_by Bob", checked, rec.Code)
+	}
+	rec = contract(t, router, http.MethodPatch, listPath+"/items/"+milk.Id.String(), withBearer(e.token2), withBody(`{"version":1,"name":"Oat milk"}`))
+	if rec.Code != http.StatusConflict {
+		t.Errorf("partner edit at a stale version: status = %d, want 409", rec.Code)
+	}
+	if rec = contract(t, router, http.MethodDelete, listPath+"/items/"+eggs.Id.String(), withBearer(e.token2)); rec.Code != http.StatusNoContent {
+		t.Errorf("partner delete of an item: status = %d, want 204", rec.Code)
+	}
+
+	// List-level actions are owner-only.
+	for _, tc := range []struct{ method, path, body string }{
+		{http.MethodPatch, listPath, `{"name":"Mine"}`},
+		{http.MethodPatch, listPath, `{"shared_with_partner":false}`},
+		{http.MethodDelete, listPath, ""},
+		{http.MethodPost, "/shopping-lists/generate", `{"from":"2026-06-01","to":"2026-06-02","list_id":"` + shared.Id.String() + `"}`},
+	} {
+		opts := []requestOption{withBearer(e.token2)}
+		if tc.body != "" {
+			opts = append(opts, withBody(tc.body))
+		}
+		if rec := contract(t, router, tc.method, tc.path, opts...); rec.Code != http.StatusNotFound {
+			t.Errorf("partner %s %s %s: status = %d, want 404 (owner-only)", tc.method, tc.path, tc.body, rec.Code)
+		}
+	}
+	if rec = contract(t, router, http.MethodGet, listPath, withBearer(e.token3)); rec.Code != http.StatusNotFound {
+		t.Errorf("stranger GET of the shared list: status = %d, want 404", rec.Code)
+	}
+
+	// After the unlink the list stays with its owner, partner edits included.
+	if rec = contract(t, router, http.MethodDelete, "/partner", withBearer(e.token2)); rec.Code != http.StatusNoContent {
+		t.Fatalf("unlink: status = %d", rec.Code)
+	}
+	if rec = contract(t, router, http.MethodGet, listPath, withBearer(e.token2)); rec.Code != http.StatusNotFound {
+		t.Errorf("partner GET after the unlink: status = %d, want 404", rec.Code)
+	}
+	rec = contract(t, router, http.MethodGet, listPath, withBearer(e.token1))
+	if got := decodeAs[api.ShoppingList](t, rec); rec.Code != http.StatusOK || len(got.Items) != 1 || !got.Items[0].Checked {
+		t.Errorf("owner GET after the unlink = %+v (status %d), want the list intact with the partner's check", got, rec.Code)
+	}
+}
+
+// TestPartnerEventStreamEndsWhenThePartnerIsUnlinked opens the partner's
+// stream on a shared list and unlinks from the other side while it is open.
+func TestPartnerEventStreamEndsWhenThePartnerIsUnlinked(t *testing.T) {
+	e := newPartnerEnv(t)
+	router := e.router
+	e.link(t)
+	shared := create[api.ShoppingList](t, router, "/shopping-lists", e.token1, `{"name":"Groceries","shared_with_partner":true}`)
+	eventsPath := "/shopping-lists/" + shared.Id.String() + "/events"
+
+	// contract() serves the request synchronously, so unlink from the side once
+	// the partner's stream has subscribed.
+	go func() {
+		deadline := time.Now().Add(5 * time.Second)
+		for e.events.Subscribers(shared.Id) == 0 && time.Now().Before(deadline) {
+			time.Sleep(5 * time.Millisecond)
+		}
+		req := httptest.NewRequest(http.MethodDelete, "/v1/partner", nil)
+		req.Header.Set("Authorization", "Bearer "+e.token1)
+		if rec := do(t, router, req); rec.Code != http.StatusNoContent {
+			t.Errorf("unlink from the side: status = %d, want 204", rec.Code)
+		}
+	}()
+	rec := contract(t, router, http.MethodGet, eventsPath, withBearer(e.token2))
+	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/event-stream" {
+		t.Fatalf("partner stream: status = %d, content type %q, want 200 text/event-stream", rec.Code, rec.Header().Get("Content-Type"))
+	}
+	if rec.Body.String() != ": connected\n\n" {
+		t.Errorf("stream body = %q, want only the connected comment before the unlink closed it", rec.Body.String())
+	}
+	if rec = contract(t, router, http.MethodGet, eventsPath, withBearer(e.token2)); rec.Code != http.StatusNotFound {
+		t.Errorf("partner reconnect after the unlink: status = %d, want 404", rec.Code)
+	}
+}
```

- [ ] **Step 2: Run them to see them fail**

Run: `make lint-api && cd backend && go test ./... -count=1`

Expected: FAIL, for the right reason:

```
internal/httpapi/partner_flow_test.go:317:38: got.IsOwner undefined (type api.Meal has no field or method IsOwner)
internal/httpapi/partner_flow_test.go:420:81: got.IsOwner undefined (type api.ShoppingList has no field or method IsOwner)
```

- [ ] **Step 3: Implement**

Modify `openapi.yaml` (unified diff against the current file):

```diff
--- a/openapi.yaml
+++ b/openapi.yaml
@@ -156,7 +156,7 @@ paths:
       tags: [DietTemplates]
       operationId: listDietTemplates
       summary: List the caller's diet templates
-      description: Cursor-paginated, alphabetical by name. Does not include slots; fetch a single template for those.
+      description: Cursor-paginated, alphabetical by name. Lists only the caller's own templates (the partner's shared ones are at `GET /partner/diet-templates`). Does not include slots; fetch a single template for those.
       parameters:
         - name: cursor
           in: query
@@ -222,7 +222,7 @@ paths:
       tags: [DietTemplates]
       operationId: getDietTemplate
       summary: Get a diet template
-      description: Includes the slot list.
+      description: Includes the slot list. Returns the caller's own template, or a template the partner has shared (`is_owner` false), whose slots show each meal's id and name even if that meal is not shared itself (its detail stays `404`). Any other template is `404`.
       responses:
         '200':
           description: The template.
@@ -366,7 +366,7 @@ paths:
       tags: [DietTemplates]
       operationId: copyDietTemplate
       summary: Copy a diet template
-      description: Creates a new template, owned by the caller, with the same name, day_count and slots. The copy always starts with `shared_with_partner` false.
+      description: Creates a new template, owned by the caller, with the same name, day_count and slots. The original may be the caller's own or one the partner has shared. Copying the partner's template also copies its meals (each once, with the same ingredient rules as `POST /meals/{id}/copy`), so the copy can be applied to the caller's own plan. The copy always starts with `shared_with_partner` false.
       responses:
         '201':
           description: The copy.
@@ -387,7 +387,7 @@ paths:
       tags: [Meals]
       operationId: listMeals
       summary: List the caller's meals
-      description: Cursor-paginated, alphabetical by name. Does not include ingredients or computed nutrition; fetch a single meal for those.
+      description: Cursor-paginated, alphabetical by name. Lists only the caller's own meals (the partner's shared ones are at `GET /partner/meals`). Does not include ingredients or computed nutrition; fetch a single meal for those.
       parameters:
         - name: cursor
           in: query
@@ -453,7 +453,7 @@ paths:
       tags: [Meals]
       operationId: getMeal
       summary: Get a meal
-      description: Includes the ingredient list and nutrition per serving, computed on read.
+      description: Includes the ingredient list and nutrition per serving, computed on read. Returns the caller's own meal, or a meal the partner has shared (`is_owner` false). Any other meal is `404`.
       responses:
         '200':
           description: The meal.
@@ -477,7 +477,7 @@ paths:
       tags: [Meals]
       operationId: updateMeal
       summary: Update a meal
-      description: Fields that are absent are left unchanged. Only the owner can update a meal.
+      description: Fields that are absent are left unchanged. Only the owner can update a meal; for anyone else, including the partner, it is `404`.
       requestBody:
         required: true
         content:
@@ -570,7 +570,7 @@ paths:
       tags: [Meals]
       operationId: copyMeal
       summary: Copy a meal
-      description: Creates a new meal, owned by the caller, with the same name, notes, servings and ingredients. The copy always starts with `shared_with_partner` false.
+      description: Creates a new meal, owned by the caller, with the same name, notes, servings and ingredients. The original may be the caller's own or one the partner has shared. Copying the partner's meal also copies each custom ingredient it uses into the caller's library (once per copy; global ingredients are shared). The copy always starts with `shared_with_partner` false.
       responses:
         '201':
           description: The copy.
@@ -686,7 +686,7 @@ paths:
       tags: [ShoppingLists]
       operationId: listShoppingLists
       summary: List the caller's shopping lists
-      description: Cursor-paginated, newest first. Does not include items; fetch a single list for those.
+      description: Cursor-paginated, newest first. Lists only the caller's own lists (the partner's shared ones are at `GET /partner/shopping-lists`). Does not include items; fetch a single list for those.
       parameters:
         - name: cursor
           in: query
@@ -787,7 +787,7 @@ paths:
       tags: [ShoppingLists]
       operationId: getShoppingList
       summary: Get a shopping list
-      description: Includes every item, ordered by position.
+      description: Includes every item, ordered by position. Returns the caller's own list, or a list the partner has shared (`is_owner` false). Any other list is `404`. Renaming, deleting, sharing and regenerating are owner-only (`404` for the partner); the partner may add, edit, check and delete items, and open the event stream.
       responses:
         '200':
           description: The list.
@@ -1075,6 +1075,111 @@ paths:
           $ref: '#/components/responses/TooManyRequests'
         '500':
           $ref: '#/components/responses/Problem'
+  /partner/meals:
+    get:
+      tags: [Partner]
+      operationId: listPartnerMeals
+      summary: List the meals the partner has shared
+      description: Cursor-paginated, alphabetical by name, like `GET /meals`, but only the meals the partner has marked `shared_with_partner`, never the caller's own. Open one with `GET /meals/{id}` (`is_owner` is false) or copy it with `POST /meals/{id}/copy`. `404 partner_not_linked` without an active partner.
+      parameters:
+        - name: cursor
+          in: query
+          schema:
+            type: string
+        - name: limit
+          in: query
+          schema:
+            type: integer
+            minimum: 1
+            maximum: 100
+            default: 20
+      responses:
+        '200':
+          description: The partner's shared meals.
+          content:
+            application/json:
+              schema:
+                $ref: '#/components/schemas/MealList'
+        '400':
+          $ref: '#/components/responses/BadRequest'
+        '401':
+          $ref: '#/components/responses/Unauthorized'
+        '404':
+          $ref: '#/components/responses/NotFound'
+        '429':
+          $ref: '#/components/responses/TooManyRequests'
+        '500':
+          $ref: '#/components/responses/Problem'
+  /partner/diet-templates:
+    get:
+      tags: [Partner]
+      operationId: listPartnerDietTemplates
+      summary: List the diet templates the partner has shared
+      description: Cursor-paginated, alphabetical by name, like `GET /diet-templates`, but only the templates the partner has marked `shared_with_partner`, never the caller's own. Open one with `GET /diet-templates/{id}` (`is_owner` is false) or copy it with `POST /diet-templates/{id}/copy`, which copies its meals too. `404 partner_not_linked` without an active partner.
+      parameters:
+        - name: cursor
+          in: query
+          schema:
+            type: string
+        - name: limit
+          in: query
+          schema:
+            type: integer
+            minimum: 1
+            maximum: 100
+            default: 20
+      responses:
+        '200':
+          description: The partner's shared diet templates.
+          content:
+            application/json:
+              schema:
+                $ref: '#/components/schemas/DietTemplateList'
+        '400':
+          $ref: '#/components/responses/BadRequest'
+        '401':
+          $ref: '#/components/responses/Unauthorized'
+        '404':
+          $ref: '#/components/responses/NotFound'
+        '429':
+          $ref: '#/components/responses/TooManyRequests'
+        '500':
+          $ref: '#/components/responses/Problem'
+  /partner/shopping-lists:
+    get:
+      tags: [Partner]
+      operationId: listPartnerShoppingLists
+      summary: List the shopping lists the partner has shared
+      description: Cursor-paginated, newest first, like `GET /shopping-lists`, but only the lists the partner has marked `shared_with_partner`, never the caller's own. Open one with `GET /shopping-lists/{id}` (`is_owner` is false); both partners may edit its items. `404 partner_not_linked` without an active partner.
+      parameters:
+        - name: cursor
+          in: query
+          schema:
+            type: string
+        - name: limit
+          in: query
+          schema:
+            type: integer
+            minimum: 1
+            maximum: 100
+            default: 20
+      responses:
+        '200':
+          description: The partner's shared shopping lists.
+          content:
+            application/json:
+              schema:
+                $ref: '#/components/schemas/ShoppingListPage'
+        '400':
+          $ref: '#/components/responses/BadRequest'
+        '401':
+          $ref: '#/components/responses/Unauthorized'
+        '404':
+          $ref: '#/components/responses/NotFound'
+        '429':
+          $ref: '#/components/responses/TooManyRequests'
+        '500':
+          $ref: '#/components/responses/Problem'
   /healthz:
     get:
       tags: [Health]
@@ -1609,7 +1714,7 @@ components:
           type: integer
     Meal:
       type: object
-      required: [id, name, notes, servings, shared_with_partner, ingredients, nutrition_per_serving, created_at, updated_at]
+      required: [id, name, notes, servings, shared_with_partner, is_owner, ingredients, nutrition_per_serving, created_at, updated_at]
       properties:
         id:
           type: string
@@ -1624,6 +1729,9 @@ components:
           format: double
         shared_with_partner:
           type: boolean
+        is_owner:
+          type: boolean
+          description: False when the meal belongs to the caller's partner, who shared it. It is then read-only for the caller (writes are `404`); copy it to change it.
         ingredients:
           type: array
           items:
@@ -1758,7 +1866,7 @@ components:
           format: double
     DietTemplate:
       type: object
-      required: [id, name, day_count, shared_with_partner, slots, created_at, updated_at]
+      required: [id, name, day_count, shared_with_partner, is_owner, slots, created_at, updated_at]
       properties:
         id:
           type: string
@@ -1769,6 +1877,9 @@ components:
           type: integer
         shared_with_partner:
           type: boolean
+        is_owner:
+          type: boolean
+          description: False when the diet template belongs to the caller's partner, who shared it. It is then read-only for the caller (writes are `404`); copy it to change it, or to apply it.
         slots:
           type: array
           items:
@@ -2020,7 +2131,7 @@ components:
           format: date-time
     ShoppingList:
       type: object
-      required: [id, name, shared_with_partner, source_from, source_to, items, created_at, updated_at]
+      required: [id, name, shared_with_partner, is_owner, source_from, source_to, items, created_at, updated_at]
       properties:
         id:
           type: string
@@ -2029,6 +2140,9 @@ components:
           type: string
         shared_with_partner:
           type: boolean
+        is_owner:
+          type: boolean
+          description: False when the list belongs to the caller's partner, who shared it. It is then read-only for the caller (writes are `404`); the caller may still add, edit, check and delete its items.
         source_from:
           type: string
           format: date
```

Modify `backend/internal/httpapi/meals.go` (unified diff against the current file):

```diff
--- a/backend/internal/httpapi/meals.go
+++ b/backend/internal/httpapi/meals.go
@@ -22,29 +22,41 @@ type MealsService interface {
 	List(ctx context.Context, ownerID uuid.UUID, in service.ListMealsInput) (service.MealPage, error)
 	ReplaceIngredients(ctx context.Context, ownerID, id uuid.UUID, items []service.MealIngredientInput) (service.Meal, error)
 	Copy(ctx context.Context, callerID, id uuid.UUID) (service.Meal, error)
+	ListPartner(ctx context.Context, callerID uuid.UUID, in service.ListMealsInput) (service.MealPage, error)
 }
 
 const defaultMealLimit = 20
 
 func (s *server) ListMeals(w http.ResponseWriter, r *http.Request, params api.ListMealsParams) {
+	s.serveMealList(w, r, params.Cursor, params.Limit, s.meals.List)
+}
+
+func (s *server) ListPartnerMeals(w http.ResponseWriter, r *http.Request, params api.ListPartnerMealsParams) {
+	s.serveMealList(w, r, params.Cursor, params.Limit, s.meals.ListPartner)
+}
+
+// serveMealList answers a cursor-paginated meal listing; list is either the
+// caller's own listing or the partner's.
+func (s *server) serveMealList(w http.ResponseWriter, r *http.Request, cursorParam *string, limitParam *int,
+	list func(context.Context, uuid.UUID, service.ListMealsInput) (service.MealPage, error)) {
 	userID, ok := requireUser(w, r)
 	if !ok {
 		return
 	}
 	limit := defaultMealLimit
-	if params.Limit != nil {
-		limit = *params.Limit
+	if limitParam != nil {
+		limit = *limitParam
 	}
 	var cursor *service.MealCursor
-	if params.Cursor != nil {
-		c, ok := decodeMealCursor(*params.Cursor)
+	if cursorParam != nil {
+		c, ok := decodeMealCursor(*cursorParam)
 		if !ok {
 			WriteValidationProblem(w, "cursor is invalid", []FieldError{{Field: "cursor", Code: FieldInvalidForm}})
 			return
 		}
 		cursor = &c
 	}
-	page, err := s.meals.List(r.Context(), userID, service.ListMealsInput{Cursor: cursor, Limit: limit})
+	page, err := list(r.Context(), userID, service.ListMealsInput{Cursor: cursor, Limit: limit})
 	if err != nil {
 		s.writeServiceError(w, r, err)
 		return
@@ -76,7 +88,7 @@ func (s *server) CreateMeal(w http.ResponseWriter, r *http.Request) {
 		s.writeServiceError(w, r, err)
 		return
 	}
-	writeJSON(w, http.StatusCreated, toAPIMeal(meal))
+	writeJSON(w, http.StatusCreated, toAPIMeal(meal, userID))
 }
 
 func (s *server) GetMeal(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
@@ -89,7 +101,7 @@ func (s *server) GetMeal(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
 		s.writeServiceError(w, r, err)
 		return
 	}
-	writeJSON(w, http.StatusOK, toAPIMeal(meal))
+	writeJSON(w, http.StatusOK, toAPIMeal(meal, userID))
 }
 
 func (s *server) UpdateMeal(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
@@ -109,7 +121,7 @@ func (s *server) UpdateMeal(w http.ResponseWriter, r *http.Request, id uuid.UUID
 		s.writeServiceError(w, r, err)
 		return
 	}
-	writeJSON(w, http.StatusOK, toAPIMeal(meal))
+	writeJSON(w, http.StatusOK, toAPIMeal(meal, userID))
 }
 
 func (s *server) DeleteMeal(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
@@ -142,7 +154,7 @@ func (s *server) ReplaceMealIngredients(w http.ResponseWriter, r *http.Request,
 		s.writeServiceError(w, r, err)
 		return
 	}
-	writeJSON(w, http.StatusOK, toAPIMeal(meal))
+	writeJSON(w, http.StatusOK, toAPIMeal(meal, userID))
 }
 
 func (s *server) CopyMeal(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
@@ -155,7 +167,7 @@ func (s *server) CopyMeal(w http.ResponseWriter, r *http.Request, id uuid.UUID)
 		s.writeServiceError(w, r, err)
 		return
 	}
-	writeJSON(w, http.StatusCreated, toAPIMeal(meal))
+	writeJSON(w, http.StatusCreated, toAPIMeal(meal, userID))
 }
 
 func nullableStringToPtr(n nullable.Nullable[string]) *string {
@@ -205,7 +217,7 @@ func toAPIMealSummary(m service.MealSummary) api.MealSummary {
 	}
 }
 
-func toAPIMeal(m service.Meal) api.Meal {
+func toAPIMeal(m service.Meal, viewer uuid.UUID) api.Meal {
 	ingredients := make([]api.MealIngredient, len(m.Ingredients))
 	for i, mi := range m.Ingredients {
 		ingredients[i] = api.MealIngredient{
@@ -216,7 +228,7 @@ func toAPIMeal(m service.Meal) api.Meal {
 	}
 	return api.Meal{
 		Id: m.ID, Name: m.Name, Notes: toNullableString(m.Notes), Servings: m.Servings,
-		SharedWithPartner: m.SharedWithPartner, Ingredients: ingredients,
+		SharedWithPartner: m.SharedWithPartner, IsOwner: m.OwnerID == viewer, Ingredients: ingredients,
 		NutritionPerServing: nutrientsToAPI(m.NutritionPerServing),
 		CreatedAt:           m.CreatedAt, UpdatedAt: m.UpdatedAt,
 	}
```

Modify `backend/internal/httpapi/diet_templates.go` (unified diff against the current file):

```diff
--- a/backend/internal/httpapi/diet_templates.go
+++ b/backend/internal/httpapi/diet_templates.go
@@ -24,29 +24,41 @@ type DietTemplatesService interface {
 	ReplaceSlots(ctx context.Context, ownerID, id uuid.UUID, slots []service.TemplateSlotInput) (service.DietTemplate, error)
 	Copy(ctx context.Context, callerID, id uuid.UUID) (service.DietTemplate, error)
 	Apply(ctx context.Context, ownerID, id uuid.UUID, in service.ApplyTemplateInput) (int, error)
+	ListPartner(ctx context.Context, callerID uuid.UUID, in service.ListDietTemplatesInput) (service.DietTemplatePage, error)
 }
 
 const defaultDietTemplateLimit = 20
 
 func (s *server) ListDietTemplates(w http.ResponseWriter, r *http.Request, params api.ListDietTemplatesParams) {
+	s.serveDietTemplateList(w, r, params.Cursor, params.Limit, s.dietTemplates.List)
+}
+
+func (s *server) ListPartnerDietTemplates(w http.ResponseWriter, r *http.Request, params api.ListPartnerDietTemplatesParams) {
+	s.serveDietTemplateList(w, r, params.Cursor, params.Limit, s.dietTemplates.ListPartner)
+}
+
+// serveDietTemplateList answers a cursor-paginated template listing; list is
+// either the caller's own listing or the partner's.
+func (s *server) serveDietTemplateList(w http.ResponseWriter, r *http.Request, cursorParam *string, limitParam *int,
+	list func(context.Context, uuid.UUID, service.ListDietTemplatesInput) (service.DietTemplatePage, error)) {
 	userID, ok := requireUser(w, r)
 	if !ok {
 		return
 	}
 	limit := defaultDietTemplateLimit
-	if params.Limit != nil {
-		limit = *params.Limit
+	if limitParam != nil {
+		limit = *limitParam
 	}
 	var cursor *service.DietTemplateCursor
-	if params.Cursor != nil {
-		c, ok := decodeDietTemplateCursor(*params.Cursor)
+	if cursorParam != nil {
+		c, ok := decodeDietTemplateCursor(*cursorParam)
 		if !ok {
 			WriteValidationProblem(w, "cursor is invalid", []FieldError{{Field: "cursor", Code: FieldInvalidForm}})
 			return
 		}
 		cursor = &c
 	}
-	page, err := s.dietTemplates.List(r.Context(), userID, service.ListDietTemplatesInput{Cursor: cursor, Limit: limit})
+	page, err := list(r.Context(), userID, service.ListDietTemplatesInput{Cursor: cursor, Limit: limit})
 	if err != nil {
 		s.writeServiceError(w, r, err)
 		return
@@ -76,7 +88,7 @@ func (s *server) CreateDietTemplate(w http.ResponseWriter, r *http.Request) {
 		s.writeServiceError(w, r, err)
 		return
 	}
-	writeJSON(w, http.StatusCreated, toAPIDietTemplate(tpl))
+	writeJSON(w, http.StatusCreated, toAPIDietTemplate(tpl, userID))
 }
 
 func (s *server) GetDietTemplate(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
@@ -89,7 +101,7 @@ func (s *server) GetDietTemplate(w http.ResponseWriter, r *http.Request, id uuid
 		s.writeServiceError(w, r, err)
 		return
 	}
-	writeJSON(w, http.StatusOK, toAPIDietTemplate(tpl))
+	writeJSON(w, http.StatusOK, toAPIDietTemplate(tpl, userID))
 }
 
 func (s *server) UpdateDietTemplate(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
@@ -106,7 +118,7 @@ func (s *server) UpdateDietTemplate(w http.ResponseWriter, r *http.Request, id u
 		s.writeServiceError(w, r, err)
 		return
 	}
-	writeJSON(w, http.StatusOK, toAPIDietTemplate(tpl))
+	writeJSON(w, http.StatusOK, toAPIDietTemplate(tpl, userID))
 }
 
 func (s *server) DeleteDietTemplate(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
@@ -143,7 +155,7 @@ func (s *server) ReplaceTemplateSlots(w http.ResponseWriter, r *http.Request, id
 		s.writeServiceError(w, r, err)
 		return
 	}
-	writeJSON(w, http.StatusOK, toAPIDietTemplate(tpl))
+	writeJSON(w, http.StatusOK, toAPIDietTemplate(tpl, userID))
 }
 
 func (s *server) ApplyDietTemplate(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
@@ -176,7 +188,7 @@ func (s *server) CopyDietTemplate(w http.ResponseWriter, r *http.Request, id uui
 		s.writeServiceError(w, r, err)
 		return
 	}
-	writeJSON(w, http.StatusCreated, toAPIDietTemplate(tpl))
+	writeJSON(w, http.StatusCreated, toAPIDietTemplate(tpl, userID))
 }
 
 func toDietTemplateList(items []service.DietTemplateSummary, nextCursor string) api.DietTemplateList {
@@ -199,7 +211,7 @@ func toAPIDietTemplateSummary(t service.DietTemplateSummary) api.DietTemplateSum
 	}
 }
 
-func toAPIDietTemplate(t service.DietTemplate) api.DietTemplate {
+func toAPIDietTemplate(t service.DietTemplate, viewer uuid.UUID) api.DietTemplate {
 	slots := make([]api.TemplateSlot, len(t.Slots))
 	for i, sl := range t.Slots {
 		slots[i] = api.TemplateSlot{
@@ -208,7 +220,7 @@ func toAPIDietTemplate(t service.DietTemplate) api.DietTemplate {
 		}
 	}
 	return api.DietTemplate{
-		Id: t.ID, Name: t.Name, DayCount: t.DayCount, SharedWithPartner: t.SharedWithPartner,
+		Id: t.ID, Name: t.Name, DayCount: t.DayCount, SharedWithPartner: t.SharedWithPartner, IsOwner: t.OwnerID == viewer,
 		Slots: slots, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
 	}
 }
```

Modify `backend/internal/httpapi/shopping_lists.go` (unified diff against the current file):

```diff
--- a/backend/internal/httpapi/shopping_lists.go
+++ b/backend/internal/httpapi/shopping_lists.go
@@ -32,6 +32,7 @@ type ShoppingListsService interface {
 	UpdateItem(ctx context.Context, ownerID, listID, itemID uuid.UUID, in service.UpdateShoppingItemInput) (service.ShoppingItem, error)
 	DeleteItem(ctx context.Context, ownerID, listID, itemID uuid.UUID) error
 	Subscribe(ctx context.Context, ownerID, listID uuid.UUID) (*service.ListSubscription, error)
+	ListPartner(ctx context.Context, callerID uuid.UUID, in service.ListShoppingListsInput) (service.ShoppingListPage, error)
 }
 
 const (
@@ -43,40 +44,51 @@ const (
 )
 
 func (s *server) ListShoppingLists(w http.ResponseWriter, r *http.Request, params api.ListShoppingListsParams) {
+	s.serveShoppingListPage(w, r, params.Cursor, params.Limit, s.shoppingLists.List)
+}
+
+func (s *server) ListPartnerShoppingLists(w http.ResponseWriter, r *http.Request, params api.ListPartnerShoppingListsParams) {
+	s.serveShoppingListPage(w, r, params.Cursor, params.Limit, s.shoppingLists.ListPartner)
+}
+
+// serveShoppingListPage answers a cursor-paginated list of shopping lists;
+// list is either the caller's own listing or the partner's.
+func (s *server) serveShoppingListPage(w http.ResponseWriter, r *http.Request, cursorParam *string, limitParam *int,
+	list func(context.Context, uuid.UUID, service.ListShoppingListsInput) (service.ShoppingListPage, error)) {
 	userID, ok := requireUser(w, r)
 	if !ok {
 		return
 	}
 	limit := defaultShoppingListLimit
-	if params.Limit != nil {
-		limit = *params.Limit
+	if limitParam != nil {
+		limit = *limitParam
 	}
 	var cursor *service.ShoppingListCursor
-	if params.Cursor != nil {
-		c, ok := decodeShoppingListCursor(*params.Cursor)
+	if cursorParam != nil {
+		c, ok := decodeShoppingListCursor(*cursorParam)
 		if !ok {
 			WriteValidationProblem(w, "cursor is invalid", []FieldError{{Field: "cursor", Code: FieldInvalidForm}})
 			return
 		}
 		cursor = &c
 	}
-	page, err := s.shoppingLists.List(r.Context(), userID, service.ListShoppingListsInput{Cursor: cursor, Limit: limit})
+	page, err := list(r.Context(), userID, service.ListShoppingListsInput{Cursor: cursor, Limit: limit})
 	if err != nil {
 		s.writeServiceError(w, r, err)
 		return
 	}
-	list := api.ShoppingListPage{Items: make([]api.ShoppingListSummary, len(page.Items)), NextCursor: nullable.NewNullNullable[string]()}
+	out := api.ShoppingListPage{Items: make([]api.ShoppingListSummary, len(page.Items)), NextCursor: nullable.NewNullNullable[string]()}
 	for i, l := range page.Items {
-		list.Items[i] = api.ShoppingListSummary{
+		out.Items[i] = api.ShoppingListSummary{
 			Id: l.ID, Name: l.Name, SharedWithPartner: l.SharedWithPartner,
 			SourceFrom: toNullableDate(l.SourceFrom), SourceTo: toNullableDate(l.SourceTo),
 			CreatedAt: l.CreatedAt, UpdatedAt: l.UpdatedAt,
 		}
 	}
 	if page.NextCursor != nil {
-		list.NextCursor = nullable.NewNullableWithValue(encodeShoppingListCursor(*page.NextCursor))
+		out.NextCursor = nullable.NewNullableWithValue(encodeShoppingListCursor(*page.NextCursor))
 	}
-	writeJSON(w, http.StatusOK, list)
+	writeJSON(w, http.StatusOK, out)
 }
 
 func (s *server) CreateShoppingList(w http.ResponseWriter, r *http.Request) {
@@ -97,7 +109,7 @@ func (s *server) CreateShoppingList(w http.ResponseWriter, r *http.Request) {
 		s.writeServiceError(w, r, err)
 		return
 	}
-	writeJSON(w, http.StatusCreated, toAPIShoppingList(list))
+	writeJSON(w, http.StatusCreated, toAPIShoppingList(list, userID))
 }
 
 func (s *server) GenerateShoppingList(w http.ResponseWriter, r *http.Request) {
@@ -120,7 +132,7 @@ func (s *server) GenerateShoppingList(w http.ResponseWriter, r *http.Request) {
 	if created {
 		status = http.StatusCreated
 	}
-	writeJSON(w, status, toAPIShoppingList(list))
+	writeJSON(w, status, toAPIShoppingList(list, userID))
 }
 
 func (s *server) GetShoppingList(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
@@ -133,7 +145,7 @@ func (s *server) GetShoppingList(w http.ResponseWriter, r *http.Request, id uuid
 		s.writeServiceError(w, r, err)
 		return
 	}
-	writeJSON(w, http.StatusOK, toAPIShoppingList(list))
+	writeJSON(w, http.StatusOK, toAPIShoppingList(list, userID))
 }
 
 func (s *server) UpdateShoppingList(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
@@ -152,7 +164,7 @@ func (s *server) UpdateShoppingList(w http.ResponseWriter, r *http.Request, id u
 		s.writeServiceError(w, r, err)
 		return
 	}
-	writeJSON(w, http.StatusOK, toAPIShoppingList(list))
+	writeJSON(w, http.StatusOK, toAPIShoppingList(list, userID))
 }
 
 func (s *server) DeleteShoppingList(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
@@ -344,13 +356,13 @@ func writeVersionConflict(w http.ResponseWriter, current service.ShoppingItem) {
 	})
 }
 
-func toAPIShoppingList(l service.ShoppingList) api.ShoppingList {
+func toAPIShoppingList(l service.ShoppingList, viewer uuid.UUID) api.ShoppingList {
 	items := make([]api.ShoppingItem, len(l.Items))
 	for i, it := range l.Items {
 		items[i] = toAPIShoppingItem(it)
 	}
 	return api.ShoppingList{
-		Id: l.ID, Name: l.Name, SharedWithPartner: l.SharedWithPartner,
+		Id: l.ID, Name: l.Name, SharedWithPartner: l.SharedWithPartner, IsOwner: l.OwnerID == viewer,
 		SourceFrom: toNullableDate(l.SourceFrom), SourceTo: toNullableDate(l.SourceTo),
 		Items: items, CreatedAt: l.CreatedAt, UpdatedAt: l.UpdatedAt,
 	}
```

- [ ] **Step 4: Regenerate**

Run `make generate` from the repo root. It rewrites `backend/internal/store/sqlc/` and `backend/internal/api/api.gen.go`. Commit the output and never edit it by hand. `make check-generated` fails if it is stale.

- [ ] **Step 5: Run the tests and see them pass**

Run: `make lint-api && cd backend && go test ./... -count=1`

Expected: PASS (`ok  	... (every package)`).

- [ ] **Step 6: Commit**

```bash
git add backend/internal/api/api.gen.go backend/internal/httpapi/diet_templates.go backend/internal/httpapi/meals.go backend/internal/httpapi/partner_flow_test.go backend/internal/httpapi/shopping_lists.go openapi.yaml
git commit -m "feat(api): list what the partner shared and mark resources with is_owner"
```

### Task 10: Docs and the final gate

**Files:**
- Modify: `backend/CLAUDE.md`
- Modify: `docs/superpowers/specs/2026-09-21-meal-planner-design.md` (sections 3.1, 4.1, 10)
- Modify: `docs/superpowers/specs/2026-09-23-backend-partner-sharing-design.md` (its status line)

The parent spec and the partner spec disagree on the partner domain in three places (the `partnerships` columns, the endpoint list, an open item the partner spec settles); the partner spec says the parent is updated in the same change. `backend/CLAUDE.md` gets the partner domain in place of the "Not built yet" note that this work retires.

- [ ] **Step 1: Apply the doc edits**

Save this as a scratch file outside the repo (for example `/tmp/partner_docs.py`) and run `python3 /tmp/partner_docs.py` from the repo root. Every replacement is guarded by an `assert` that its target text occurs exactly once, so it fails loudly if a file was not merged up in Task 0.

```python
"""Docs edits for the partner-sharing plan. Run from the repo root."""
import re

def edit(path, pairs):
    s = open(path).read()
    for old, new in pairs:
        assert s.count(old) == 1, (path, s.count(old), old[:70])
        s = s.replace(old, new, 1)
    open(path, "w").write(s)

# ---------------------------------------------------------------- backend/CLAUDE.md
p = "backend/CLAUDE.md"
s = open(p).read()

edit(p, [
    (
        "- `internal/service/`: business rules. `Auth` = registration, login, refresh-token sessions, signed-in user's account. Never speaks HTTP.",
        "- `internal/service/`: business rules. `Auth` = registration, login, refresh-token sessions, signed-in user's account. `Partners` = the invite-code link between two users. `Meals`, `DietTemplates` and `ShoppingLists` apply the partner visibility rules (see below). Never speaks HTTP.",
    ),
    (
        "`plan_entries`, `shopping_lists`, but `meal_ingredients.ingredient_id`",
        "`plan_entries`, `shopping_lists`, `partnerships`, but `meal_ingredients.ingredient_id`",
    ),
    (
        "needs same treatment, in right position in chain.",
        "needs same treatment, in right position in chain. `partnerships` needs no step: nothing references it `NO ACTION`, and its three user columns cascade, which ends the partner's access at once.",
    ),
    (
        "- Rate limits (10 auth req/min per client IP, 300 req/min per user) in memory: with several replicas, effective limit per replica.",
        "- Rate limits (10 auth req/min per client IP, 300 req/min per user, and 10 `POST /partner/accept` per hour per user and, separately, per client IP) in memory: with several replicas, effective limit per replica. The accept limit (`RateLimits.AcceptPerHour`) is tight because an invite code has only about 39 bits.",
    ),
])

partner_bullets = """- **Partner link (`Partners`).** `POST partner/invite` creates a `pending` `partnerships` row holding only the SHA-256 of an 8-character code (alphabet `23456789ABCDEFGHJKMNPQRSTUVWXYZ`), valid 48 h; a new invite replaces the caller's old one. `POST partner/accept` activates it and clears code and expiry, so a spent code cannot replay. Wrong, expired, own and used codes all answer `404 invite_invalid`. A caller who is already linked gets `409 partner_already_linked` before any code lookup, so `accept` is no oracle for guessing codes. `Invite` and `Accept` lock the involved `users` rows (`FOR NO KEY UPDATE`, in id order) and check again, because "a user appears once across both columns of active rows" cannot be an index; `Accept` also drops the caller's own pending invite. `DELETE partner` (either side) hard-deletes the row; re-linking is a fresh invite. `GET partner` reads an expired pending invite as `404 partner_not_linked`.
- **Partner visibility is one read predicate.** `owner_id = @user_id OR (owner_id = @partner_id AND shared_with_partner)`, with `partner_id` NULL when the caller has no partner. `activePartnerID`/`partnerOrNil` (package functions in `service/partners.go`) resolve the partner once per transaction, uncached, so an unlink shows in the next transaction. Only `Get` of a meal, template or list, `Copy`, and shopping-item writes and `Subscribe` use it. `List` stays owner-only, and `GET partner/{meals,diet-templates,shopping-lists}` list only what the partner shared (`shared_only` in the list queries). Every other write, `DietTemplates.Apply` and the plan stay owner-only (`Plan` reads meals through `Meals.GetOwn`, and `SetEntry` calls `GetMealForUser` without a partner), so a partner gets `404`. Meals, templates and lists carry `is_owner`.
- **Sharing a template shares what its slots show.** A partner reading a shared template sees each slot's `meal_id` and meal name even when the meal is not shared; `GET /meals/{id}` on it stays `404`. Copy reads the meals through the template.
- **Copying a partner's meal or template duplicates what it needs.** `ingredientCopier` (`service/copy.go`) duplicates each distinct custom ingredient, with its nutrient rows, into the caller's library once per copy operation and keeps global (USDA) ingredients as shared references. A partner template copy also copies each distinct meal once, private. An own copy is unchanged (same meals, same ingredients). Copying twice duplicates twice; there is no dedupe across copies. Copies never change with the original.
- **Shared shopping lists.** The partner may add, edit, check and delete items and open the event stream; rename, delete, toggling `shared_with_partner` and regenerate are owner-only (`404` for the partner), and regenerate reads the owner's `plan_entries`. `checked_by` is the acting user (cleared on uncheck, `ON DELETE SET NULL`). Partner-added items are `manual`, so the owner's regenerate keeps them. Item writes take the partnership row `FOR SHARE`, so an unlink waits for edits in flight and any edit after it finds no partner.
- **Unlink, unshare and account deletion close event streams.** `ListEventHub` subscriptions record watcher and list owner. `Partners.Unlink` calls `CloseAccess`, turning sharing off calls `CloseListForNonOwners`, and `Auth.OnUserDeleted(listEvents.CloseUser)` (wired in `cmd/api`) closes every stream touching a deleted account, whose lists are removed by a raw `DELETE` that publishes nothing. `Subscribe` registers with the hub first and then re-checks access under `FOR SHARE`, so an unlink cannot slip between them. Like the rest of the hub this is per process.
"""
marker = "## Decide before the domain plans"
assert s.count(marker) == 1
s = s.replace(marker, partner_bullets + "\n" + marker, 1)
open(p, "w").write(s)

s = open(p).read()
start = s.index("- Domain beyond shopping lists: partners and sharing")
end = s.index("- Contract now declares `500` for every auth op")
s = s[:start] + "- Out of scope for the partner domain in v1: sharing or applying `plan_entries`, more than one partner or groups, notifications and email invites, and cross-replica event delivery (`ListEventHub`, and so `CloseAccess`, is per process).\n" + s[end:]
open(p, "w").write(s)

# ---------------------------------------------------------------- parent spec
p = "docs/superpowers/specs/2026-09-21-meal-planner-design.md"
edit(p, [
    (
        "- **partnerships:** `user_a`, `user_b` (nullable until accepted), `status` (`pending` | `active`), `invite_code` (unique, expiring), `created_by`. A partial unique index limits each user to one active partnership.",
        "- **partnerships:** `user_a` (the inviter), `user_b` (nullable until accepted), `status` (`pending` | `active`), `invite_code_hash` and `invite_expires_at` (set while pending, cleared on accept), `created_by`. The invite code has 8 characters from `23456789ABCDEFGHJKMNPQRSTUVWXYZ`, expires after 48 hours, is stored hashed and shown once. Partial unique indexes limit each user to one active row per column and one pending invite; `accept` locks both users so nobody ends up in two active rows. Details: `2026-09-23-backend-partner-sharing-design.md`.",
    ),
    (
        "`GET partner/meals`; `GET partner/diet-templates`.",
        "`GET partner/meals`; `GET partner/diet-templates`; `GET partner/shopping-lists`. `Meal`, `DietTemplate` and `ShoppingList` carry `is_owner`, so clients can render a partner's resource read-only.",
    ),
    ("- Invite code format and expiry duration.\n", ""),
])

# ---------------------------------------------------------------- partner spec
p = "docs/superpowers/specs/2026-09-23-backend-partner-sharing-design.md"
s = open(p).read()
start = s.index("Status: ")
end = s.index("Extends `", start)
s = s[:start] + "Status: implemented by `docs/superpowers/plans/2026-09-24-backend-partner-sharing.md`. " + s[end:]
open(p, "w").write(s)
```

- [ ] **Step 2: Read the result**

Run: `git diff -- backend/CLAUDE.md docs/`

Expected: `backend/CLAUDE.md` gains six bullets (partner link, visibility, template sharing, copying, shared lists, streams) before "Decide before the domain plans", one clause each in the layout, cascade and rate-limit bullets, and loses the long partner bullet and its four sub-bullets under "Not built yet" in favour of one out-of-scope bullet. The parent spec's `partnerships` bullet lists the hash and expiry columns, its Partner endpoint line gains `GET partner/shopping-lists` and `is_owner`, and the invite-code open item is gone. The partner spec's status line says it is implemented.

- [ ] **Step 3: Run the whole gate**

Run: `make check`

Expected: `lint-api`, `test-backend`, `lint-backend` and `check-generated` all pass. This needs Docker (the tests start Postgres with testcontainers). If `check-generated` fails, run `make generate` and commit the output with the task that changed the spec or queries.

- [ ] **Step 4: Commit**

```bash
git add backend/CLAUDE.md docs/superpowers/specs
git commit -m "docs: document the partner domain and update the parent spec"
```

- [ ] **Step 5: Mark the plan complete**

Tick every checkbox above, then commit the plan itself:

```bash
git add docs/superpowers/plans/2026-09-24-backend-partner-sharing.md
git commit -m "docs: mark all backend-partner-sharing plan tasks complete"
```

Then use `superpowers:finishing-a-development-branch`: one PR from `feat/backend-partner-sharing`.
