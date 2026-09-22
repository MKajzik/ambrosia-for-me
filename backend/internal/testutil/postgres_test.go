package testutil_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/InzKazik/mealplanner/backend/internal/testutil"
)

func connect(t *testing.T, url string) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(context.Background(), url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

func TestNewMigratedDatabaseIsMigratedAndIsolated(t *testing.T) {
	ctx := context.Background()
	urlA := testutil.NewMigratedDatabase(t)
	urlB := testutil.NewMigratedDatabase(t)
	if urlA == urlB {
		t.Fatal("two calls returned the same database URL")
	}
	a, b := connect(t, urlA), connect(t, urlB)

	// Migrated: the first migration's trigger function exists.
	var migrated bool
	if err := a.QueryRow(ctx, `SELECT to_regprocedure('set_updated_at()') IS NOT NULL`).Scan(&migrated); err != nil || !migrated {
		t.Fatalf("set_updated_at() missing in a migrated database (err %v)", err)
	}

	// Isolated: a table created in one database is invisible in the other.
	if _, err := a.Exec(ctx, `CREATE TABLE only_in_a (x int)`); err != nil {
		t.Fatalf("create table in a: %v", err)
	}
	var visibleInB bool
	if err := b.QueryRow(ctx, `SELECT to_regclass('public.only_in_a') IS NOT NULL`).Scan(&visibleInB); err != nil {
		t.Fatalf("check b: %v", err)
	}
	if visibleInB {
		t.Error("a table created in database a is visible in database b")
	}
}
