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
