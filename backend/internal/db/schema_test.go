package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/InzKazik/mealplanner/backend/internal/testutil"
)

func migratedConn(t *testing.T) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(context.Background(), testutil.NewMigratedDatabase(t))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

func TestUsersSchemaEnforcesItsConstraints(t *testing.T) {
	ctx := context.Background()
	conn := migratedConn(t)
	insert := func(sql string, args ...any) error {
		_, err := conn.Exec(ctx, "INSERT INTO users (email, password_hash, apple_sub, display_name, target_kcal) VALUES "+sql, args...)
		return err
	}

	if err := insert(`('a@example.com', 'hash', NULL, 'A', 2000)`); err != nil {
		t.Fatalf("valid insert: %v", err)
	}
	tests := []struct {
		name string
		sql  string
	}{
		{"email is unique case-insensitively", `('A@EXAMPLE.COM', 'hash', NULL, 'B', NULL)`},
		{"a credential is required", `('c@example.com', NULL, NULL, 'C', NULL)`},
		{"kcal target must be positive", `('d@example.com', 'hash', NULL, 'D', 0)`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := insert(tt.sql); err == nil {
				t.Error("insert succeeded, want a constraint violation")
			}
		})
	}
	if err := insert(`('e@example.com', NULL, 'apple-sub-1', 'E', NULL)`); err != nil {
		t.Errorf("an Apple-only account (no password hash) was rejected: %v", err)
	}
	if err := insert(`('f@example.com', NULL, 'apple-sub-1', 'F', NULL)`); err == nil {
		t.Error("two accounts with the same apple_sub were accepted")
	}
}

func TestSchemaObjectsHaveStableNames(t *testing.T) {
	ctx := context.Background()
	conn := migratedConn(t)

	for _, name := range []string{"users_email_key", "users_apple_sub_key"} {
		var n int
		if err := conn.QueryRow(ctx, `SELECT count(*) FROM pg_constraint WHERE conname = $1`, name).Scan(&n); err != nil || n != 1 {
			t.Errorf("constraints named %s = %d (err %v), want 1", name, n, err)
		}
	}
	var exists bool
	if err := conn.QueryRow(ctx, `SELECT to_regclass('public.refresh_tokens_expires_at_idx') IS NOT NULL`).Scan(&exists); err != nil || !exists {
		t.Errorf("index refresh_tokens_expires_at_idx exists = %v (err %v), want true", exists, err)
	}
}

func TestUpdatedAtTriggerFires(t *testing.T) {
	ctx := context.Background()
	conn := migratedConn(t)
	var id string
	var created, updated time.Time
	err := conn.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, display_name) VALUES ('a@example.com', 'h', 'A') RETURNING id, created_at, updated_at`,
	).Scan(&id, &created, &updated)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if !updated.Equal(created) {
		t.Fatalf("updated_at %v differs from created_at %v on insert", updated, created)
	}

	// now() is frozen inside a transaction, so wait between statements.
	time.Sleep(20 * time.Millisecond)
	if err := conn.QueryRow(ctx, `UPDATE users SET display_name = 'B' WHERE id = $1 RETURNING updated_at`, id).Scan(&updated); err != nil {
		t.Fatalf("update: %v", err)
	}

	if !updated.After(created) {
		t.Errorf("updated_at %v was not advanced past created_at %v by the trigger", updated, created)
	}
}

func TestRefreshTokensCascadeWithTheirUser(t *testing.T) {
	ctx := context.Background()
	conn := migratedConn(t)
	var userID string
	if err := conn.QueryRow(ctx, `INSERT INTO users (email, password_hash, display_name) VALUES ('a@example.com', 'h', 'A') RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`INSERT INTO refresh_tokens (user_id, family_id, token_hash, expires_at) VALUES ($1, gen_random_uuid(), '\x01', now() + interval '1 day')`, userID); err != nil {
		t.Fatalf("insert refresh token: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`INSERT INTO refresh_tokens (user_id, family_id, token_hash, expires_at) VALUES ($1, gen_random_uuid(), '\x01', now() + interval '1 day')`, userID); err == nil {
		t.Error("a duplicate token_hash was accepted, want a unique violation")
	}

	if _, err := conn.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	var n int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM refresh_tokens`).Scan(&n); err != nil || n != 0 {
		t.Errorf("refresh_tokens rows after deleting the user = %d (err %v), want 0", n, err)
	}
}

func TestIngredientsSchemaEnforcesItsConstraints(t *testing.T) {
	ctx := context.Background()
	conn := migratedConn(t)

	var userID string
	if err := conn.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, display_name) VALUES ('a@example.com', 'h', 'A') RETURNING id`,
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	insert := func(sql string, args ...any) error {
		_, err := conn.Exec(ctx, "INSERT INTO ingredients (name, category, owner_id, usda_fdc_id) VALUES "+sql, args...)
		return err
	}
	if err := insert(`('Apple', 'produce', NULL, 100)`); err != nil {
		t.Fatalf("valid global insert: %v", err)
	}
	if err := insert(`('My Mix', 'other', $1, NULL)`, userID); err != nil {
		t.Fatalf("valid custom insert: %v", err)
	}
	if err := insert(`('Bad Category', 'not_a_category', NULL, 101)`); err == nil {
		t.Error("an invalid category was accepted, want a constraint violation")
	}
	if err := insert(`('Duplicate FDC', 'produce', NULL, 100)`); err == nil {
		t.Error("a duplicate usda_fdc_id was accepted, want a unique violation")
	}
	if err := insert(`('Ghost', 'other', gen_random_uuid(), NULL)`); err == nil {
		t.Error("an owner_id that does not reference a user was accepted, want a foreign key violation")
	}

	var ingredientID string
	if err := conn.QueryRow(ctx,
		`INSERT INTO ingredients (name, category) VALUES ('Banana', 'produce') RETURNING id`,
	).Scan(&ingredientID); err != nil {
		t.Fatalf("insert ingredient: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`INSERT INTO ingredient_nutrients (ingredient_id, nutrient_key, amount_per_100g) VALUES ($1, 'calories', 89)`, ingredientID,
	); err != nil {
		t.Fatalf("insert nutrient: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`INSERT INTO ingredient_nutrients (ingredient_id, nutrient_key, amount_per_100g) VALUES ($1, 'not_a_nutrient', 1)`, ingredientID,
	); err == nil {
		t.Error("an invalid nutrient_key was accepted, want an enum violation")
	}
	if _, err := conn.Exec(ctx,
		`INSERT INTO ingredient_nutrients (ingredient_id, nutrient_key, amount_per_100g) VALUES ($1, 'protein', -1)`, ingredientID,
	); err == nil {
		t.Error("a negative amount was accepted, want a constraint violation")
	}

	if _, err := conn.Exec(ctx, `DELETE FROM ingredients WHERE id = $1`, ingredientID); err != nil {
		t.Fatalf("delete ingredient: %v", err)
	}
	var n int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM ingredient_nutrients WHERE ingredient_id = $1`, ingredientID).Scan(&n); err != nil || n != 0 {
		t.Errorf("ingredient_nutrients rows after deleting the ingredient = %d (err %v), want 0", n, err)
	}
}

func TestMealsSchemaEnforcesItsConstraints(t *testing.T) {
	ctx := context.Background()
	conn := migratedConn(t)

	var userID string
	if err := conn.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, display_name) VALUES ('a@example.com', 'h', 'A') RETURNING id`,
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var ingredientID string
	if err := conn.QueryRow(ctx,
		`INSERT INTO ingredients (name, category) VALUES ('Chicken Breast', 'meat_seafood') RETURNING id`,
	).Scan(&ingredientID); err != nil {
		t.Fatalf("insert ingredient: %v", err)
	}

	var mealID string
	if err := conn.QueryRow(ctx,
		`INSERT INTO meals (owner_id, name, servings) VALUES ($1, 'Chicken and Rice', 2) RETURNING id`, userID,
	).Scan(&mealID); err != nil {
		t.Fatalf("valid insert: %v", err)
	}

	if _, err := conn.Exec(ctx,
		`INSERT INTO meals (owner_id, name, servings) VALUES (gen_random_uuid(), 'Ghost Meal', 1)`,
	); err == nil {
		t.Error("an owner_id that does not reference a user was accepted, want a foreign key violation")
	}
	if _, err := conn.Exec(ctx,
		`INSERT INTO meals (owner_id, name, servings) VALUES ($1, 'Bad Servings', 0)`, userID,
	); err == nil {
		t.Error("zero servings was accepted, want a constraint violation")
	}

	insertLine := func(sql string, args ...any) error {
		_, err := conn.Exec(ctx, "INSERT INTO meal_ingredients (meal_id, ingredient_id, quantity, unit, position) VALUES "+sql, args...)
		return err
	}
	if err := insertLine(`($1, $2, 300, 'g', 0)`, mealID, ingredientID); err != nil {
		t.Fatalf("valid line: %v", err)
	}
	if err := insertLine(`($1, gen_random_uuid(), 1, 'g', 1)`, mealID); err == nil {
		t.Error("an ingredient_id that does not reference an ingredient was accepted, want a foreign key violation")
	}
	if err := insertLine(`($1, $2, 0, 'g', 2)`, mealID, ingredientID); err == nil {
		t.Error("zero quantity was accepted, want a constraint violation")
	}
	if err := insertLine(`($1, $2, 1, 'litres', 3)`, mealID, ingredientID); err == nil {
		t.Error("an invalid unit was accepted, want a constraint violation")
	}
	if err := insertLine(`($1, $2, 1, 'g', 0)`, mealID, ingredientID); err == nil {
		t.Error("a duplicate (meal_id, position) was accepted, want a unique violation")
	}

	if _, err := conn.Exec(ctx, `DELETE FROM ingredients WHERE id = $1`, ingredientID); err == nil {
		t.Error("deleting an ingredient referenced by a meal_ingredients row was accepted, want a foreign key violation")
	}

	if _, err := conn.Exec(ctx, `DELETE FROM meals WHERE id = $1`, mealID); err != nil {
		t.Fatalf("delete meal: %v", err)
	}
	var n int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM meal_ingredients WHERE meal_id = $1`, mealID).Scan(&n); err != nil || n != 0 {
		t.Errorf("meal_ingredients rows after deleting the meal = %d (err %v), want 0", n, err)
	}
}

func TestDietsAndPlanSchemaEnforcesItsConstraints(t *testing.T) {
	ctx := context.Background()
	conn := migratedConn(t)

	var userID string
	if err := conn.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, display_name) VALUES ('a@example.com', 'h', 'A') RETURNING id`,
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var mealID string
	if err := conn.QueryRow(ctx,
		`INSERT INTO meals (owner_id, name, servings) VALUES ($1, 'Oatmeal', 1) RETURNING id`, userID,
	).Scan(&mealID); err != nil {
		t.Fatalf("insert meal: %v", err)
	}

	var templateID string
	if err := conn.QueryRow(ctx,
		`INSERT INTO diet_templates (owner_id, name, day_count) VALUES ($1, 'Week One', 7) RETURNING id`, userID,
	).Scan(&templateID); err != nil {
		t.Fatalf("valid insert: %v", err)
	}

	if _, err := conn.Exec(ctx,
		`INSERT INTO diet_templates (owner_id, name, day_count) VALUES (gen_random_uuid(), 'Ghost Template', 7)`,
	); err == nil {
		t.Error("an owner_id that does not reference a user was accepted, want a foreign key violation")
	}
	if _, err := conn.Exec(ctx,
		`INSERT INTO diet_templates (owner_id, name, day_count) VALUES ($1, 'Bad Day Count', 0)`, userID,
	); err == nil {
		t.Error("day_count 0 was accepted, want a constraint violation")
	}

	insertSlot := func(sql string, args ...any) error {
		_, err := conn.Exec(ctx, "INSERT INTO template_slots (template_id, day_index, slot, meal_id, portion) VALUES "+sql, args...)
		return err
	}
	if err := insertSlot(`($1, 0, 'breakfast', $2, 1)`, templateID, mealID); err != nil {
		t.Fatalf("valid slot: %v", err)
	}
	if err := insertSlot(`($1, -1, 'lunch', $2, 1)`, templateID, mealID); err == nil {
		t.Error("a negative day_index was accepted, want a constraint violation")
	}
	if err := insertSlot(`($1, 0, 'brunch', $2, 1)`, templateID, mealID); err == nil {
		t.Error("an invalid slot was accepted, want a constraint violation")
	}
	if err := insertSlot(`($1, 0, 'lunch', $2, 0)`, templateID, mealID); err == nil {
		t.Error("zero portion was accepted, want a constraint violation")
	}
	if err := insertSlot(`($1, 0, 'breakfast', $2, 1)`, templateID, mealID); err == nil {
		t.Error("a duplicate (template_id, day_index, breakfast) was accepted, want a unique violation")
	}
	if err := insertSlot(`($1, 0, 'snack', $2, 1)`, templateID, mealID); err != nil {
		t.Fatalf("first snack on day 0: %v", err)
	}
	if err := insertSlot(`($1, 0, 'snack', $2, 1)`, templateID, mealID); err != nil {
		t.Errorf("a second snack on the same day was rejected, want it accepted: %v", err)
	}
	if err := insertSlot(`($1, 0, 'breakfast', gen_random_uuid(), 1)`, templateID); err == nil {
		t.Error("a meal_id that does not reference a meal was accepted, want a foreign key violation")
	}

	if _, err := conn.Exec(ctx, `DELETE FROM meals WHERE id = $1`, mealID); err == nil {
		t.Error("deleting a meal referenced by a template_slots row was accepted, want a foreign key violation")
	}

	var planEntryID string
	if err := conn.QueryRow(ctx,
		`INSERT INTO plan_entries (owner_id, date, slot, meal_id, from_template_id) VALUES ($1, '2026-01-01', 'breakfast', $2, $3) RETURNING id`,
		userID, mealID, templateID,
	).Scan(&planEntryID); err != nil {
		t.Fatalf("valid plan entry: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`INSERT INTO plan_entries (owner_id, date, slot, meal_id) VALUES ($1, '2026-01-01', 'breakfast', $2)`, userID, mealID,
	); err == nil {
		t.Error("a duplicate (owner_id, date, breakfast) was accepted, want a unique violation")
	}
	if _, err := conn.Exec(ctx,
		`INSERT INTO plan_entries (owner_id, date, slot, meal_id) VALUES ($1, '2026-01-01', 'snack', $2)`, userID, mealID,
	); err != nil {
		t.Errorf("a second snack plan entry on the same day was rejected, want it accepted: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`INSERT INTO plan_entries (owner_id, date, slot, meal_id) VALUES ($1, '2026-01-02', 'breakfast', gen_random_uuid())`, userID,
	); err == nil {
		t.Error("a meal_id that does not reference a meal was accepted, want a foreign key violation")
	}
	if _, err := conn.Exec(ctx, `DELETE FROM meals WHERE id = $1`, mealID); err == nil {
		t.Error("deleting a meal referenced by a plan_entries row was accepted, want a foreign key violation")
	}

	if _, err := conn.Exec(ctx, `DELETE FROM diet_templates WHERE id = $1`, templateID); err != nil {
		t.Fatalf("delete template: %v", err)
	}
	var slotCount int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM template_slots WHERE template_id = $1`, templateID).Scan(&slotCount); err != nil || slotCount != 0 {
		t.Errorf("template_slots rows after deleting the template = %d (err %v), want 0", slotCount, err)
	}
	var fromTemplate *string
	if err := conn.QueryRow(ctx, `SELECT from_template_id FROM plan_entries WHERE id = $1`, planEntryID).Scan(&fromTemplate); err != nil {
		t.Fatalf("read plan entry after template delete: %v", err)
	}
	if fromTemplate != nil {
		t.Errorf("from_template_id after deleting the template = %v, want NULL (ON DELETE SET NULL)", *fromTemplate)
	}
}

func TestShoppingListsSchemaEnforcesItsConstraints(t *testing.T) {
	ctx := context.Background()
	conn := migratedConn(t)

	var userID, ingredientID string
	if err := conn.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, display_name) VALUES ('a@example.com', 'h', 'A') RETURNING id`,
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := conn.QueryRow(ctx,
		`INSERT INTO ingredients (name, category, owner_id) VALUES ('Tofu', 'legumes_nuts_seeds', $1) RETURNING id`, userID,
	).Scan(&ingredientID); err != nil {
		t.Fatalf("insert ingredient: %v", err)
	}

	var listID string
	if err := conn.QueryRow(ctx,
		`INSERT INTO shopping_lists (owner_id, name, source_from, source_to) VALUES ($1, 'Week', '2026-06-01', '2026-06-07') RETURNING id`, userID,
	).Scan(&listID); err != nil {
		t.Fatalf("valid list: %v", err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO shopping_lists (owner_id, name) VALUES (gen_random_uuid(), 'Ghost')`); err == nil {
		t.Error("an owner_id that does not reference a user was accepted, want a foreign key violation")
	}
	if _, err := conn.Exec(ctx, `INSERT INTO shopping_lists (owner_id, name, source_from) VALUES ($1, 'Half', '2026-06-01')`, userID); err == nil {
		t.Error("source_from without source_to was accepted, want a constraint violation")
	}
	if _, err := conn.Exec(ctx, `INSERT INTO shopping_lists (owner_id, name, source_from, source_to) VALUES ($1, 'Backwards', '2026-06-07', '2026-06-01')`, userID); err == nil {
		t.Error("source_to before source_from was accepted, want a constraint violation")
	}

	insertItem := func(values string, args ...any) error {
		_, err := conn.Exec(ctx, "INSERT INTO shopping_items (list_id, ingredient_id, name, quantity, unit, category, position, origin) VALUES "+values, args...)
		return err
	}
	if err := insertItem(`($1, $2, 'Tofu', 400, 'g', 'legumes_nuts_seeds', 0, 'generated')`, listID, ingredientID); err != nil {
		t.Fatalf("valid generated item: %v", err)
	}
	if err := insertItem(`($1, NULL, 'Paper towels', NULL, NULL, 'other', 1, 'manual')`, listID); err != nil {
		t.Fatalf("valid free-text item: %v", err)
	}
	for name, values := range map[string]string{
		"zero quantity":     `($1, NULL, 'X', 0, 'g', 'other', 2, 'manual')`,
		"invalid unit":      `($1, NULL, 'X', 1, 'cup', 'other', 2, 'manual')`,
		"invalid category":  `($1, NULL, 'X', 1, 'g', 'aisle_9', 2, 'manual')`,
		"negative position": `($1, NULL, 'X', 1, 'g', 'other', -1, 'manual')`,
		"invalid origin":    `($1, NULL, 'X', 1, 'g', 'other', 2, 'imported')`,
	} {
		if err := insertItem(values, listID); err == nil {
			t.Errorf("%s was accepted, want a constraint violation", name)
		}
	}

	var version int
	if err := conn.QueryRow(ctx, `SELECT version FROM shopping_items WHERE list_id = $1 AND position = 0`, listID).Scan(&version); err != nil || version != 1 {
		t.Errorf("a new item's version = %d (err %v), want 1", version, err)
	}

	// Deleting an ingredient a shopping item references is allowed and only
	// forgets the link (unlike meal_ingredients, which blocks it).
	if _, err := conn.Exec(ctx, `DELETE FROM ingredients WHERE id = $1`, ingredientID); err != nil {
		t.Fatalf("delete an ingredient a shopping item references: %v", err)
	}
	var linked *string
	var name string
	if err := conn.QueryRow(ctx, `SELECT ingredient_id, name FROM shopping_items WHERE list_id = $1 AND position = 0`, listID).Scan(&linked, &name); err != nil {
		t.Fatalf("read item after ingredient delete: %v", err)
	}
	if linked != nil || name != "Tofu" {
		t.Errorf("item after ingredient delete = (%v, %q), want (NULL, Tofu): ON DELETE SET NULL, item kept", linked, name)
	}

	if _, err := conn.Exec(ctx, `DELETE FROM shopping_lists WHERE id = $1`, listID); err != nil {
		t.Fatalf("delete list: %v", err)
	}
	var n int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM shopping_items WHERE list_id = $1`, listID).Scan(&n); err != nil || n != 0 {
		t.Errorf("shopping_items rows after deleting the list = %d (err %v), want 0", n, err)
	}
}

func TestPartnershipsSchemaEnforcesItsConstraints(t *testing.T) {
	ctx := context.Background()
	conn := migratedConn(t)

	newUser := func(email string) string {
		t.Helper()
		var id string
		if err := conn.QueryRow(ctx,
			`INSERT INTO users (email, password_hash, display_name) VALUES ($1, 'h', 'U') RETURNING id`, email,
		).Scan(&id); err != nil {
			t.Fatalf("insert user %s: %v", email, err)
		}
		return id
	}
	alice, bob, carol, dave := newUser("alice@example.com"), newUser("bob@example.com"), newUser("carol@example.com"), newUser("dave@example.com")

	insertPending := func(user, hash string) error {
		_, err := conn.Exec(ctx,
			`INSERT INTO partnerships (user_a, status, invite_code_hash, invite_expires_at, created_by)
			 VALUES ($1, 'pending', $2::bytea, now() + interval '48 hours', $1)`, user, hash)
		return err
	}
	insertActive := func(a, b string) error {
		_, err := conn.Exec(ctx,
			`INSERT INTO partnerships (user_a, user_b, status, created_by) VALUES ($1, $2, 'active', $1)`, a, b)
		return err
	}

	if err := insertPending(alice, "hash-alice"); err != nil {
		t.Fatalf("valid pending invite: %v", err)
	}
	if err := insertPending(alice, "hash-alice-2"); err == nil {
		t.Error("a second pending invite for one user was accepted, want a unique violation")
	}
	if err := insertPending(bob, "hash-alice"); err == nil {
		t.Error("a reused invite_code_hash was accepted, want a unique violation")
	}

	for name, stmt := range map[string]string{
		"pending with a user_b": `INSERT INTO partnerships (user_a, user_b, status, invite_code_hash, invite_expires_at, created_by)
			VALUES ('` + carol + `', '` + dave + `', 'pending', 'x'::bytea, now(), '` + carol + `')`,
		"pending without a code hash": `INSERT INTO partnerships (user_a, status, invite_expires_at, created_by)
			VALUES ('` + carol + `', 'pending', now(), '` + carol + `')`,
		"pending without an expiry": `INSERT INTO partnerships (user_a, status, invite_code_hash, created_by)
			VALUES ('` + carol + `', 'pending', 'y'::bytea, '` + carol + `')`,
		"active without a user_b": `INSERT INTO partnerships (user_a, status, created_by)
			VALUES ('` + carol + `', 'active', '` + carol + `')`,
		"active that keeps its code hash": `INSERT INTO partnerships (user_a, user_b, status, invite_code_hash, invite_expires_at, created_by)
			VALUES ('` + carol + `', '` + dave + `', 'active', 'z'::bytea, now(), '` + carol + `')`,
		"a user linked to themselves": `INSERT INTO partnerships (user_a, user_b, status, created_by)
			VALUES ('` + carol + `', '` + carol + `', 'active', '` + carol + `')`,
		"an unknown status": `INSERT INTO partnerships (user_a, user_b, status, created_by)
			VALUES ('` + carol + `', '` + dave + `', 'blocked', '` + carol + `')`,
	} {
		if _, err := conn.Exec(ctx, stmt); err == nil {
			t.Errorf("%s was accepted, want a constraint violation", name)
		}
	}

	if err := insertActive(carol, dave); err != nil {
		t.Fatalf("valid active partnership: %v", err)
	}
	if err := insertActive(carol, alice); err == nil {
		t.Error("a second active partnership for user_a was accepted, want a unique violation")
	}
	if err := insertActive(bob, dave); err == nil {
		t.Error("a second active partnership for user_b was accepted, want a unique violation")
	}
	// Deleting a user deletes every partnership row they are in.
	if _, err := conn.Exec(ctx, `DELETE FROM users WHERE id = $1`, carol); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	var n int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM partnerships WHERE user_a = $1 OR user_b = $1 OR created_by = $1`, carol).Scan(&n); err != nil || n != 0 {
		t.Errorf("partnerships left after deleting one of the users = %d (err %v), want 0", n, err)
	}
}
