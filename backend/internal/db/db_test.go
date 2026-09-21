package db_test

import (
	"context"
	"testing"

	"github.com/InzKazik/mealplanner/backend/internal/db"
	"github.com/InzKazik/mealplanner/backend/internal/testutil"
)

func TestMigrateAppliesAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	url := testutil.NewDatabase(t)

	applied, err := db.Migrate(ctx, url)
	if err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	if applied != 1 {
		t.Errorf("first Migrate applied %d migrations, want 1", applied)
	}

	again, err := db.Migrate(ctx, url)
	if err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	if again != 0 {
		t.Errorf("second Migrate applied %d migrations, want 0", again)
	}

	pool, err := db.Connect(ctx, url)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer pool.Close()

	var n int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM pg_extension WHERE extname IN ('citext', 'pg_trgm')`).Scan(&n)
	if err != nil {
		t.Fatalf("query extensions: %v", err)
	}
	if n != 2 {
		t.Errorf("found %d of 2 required extensions", n)
	}

	var hasFn bool
	err = pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_proc WHERE proname = 'set_updated_at')`).Scan(&hasFn)
	if err != nil {
		t.Fatalf("query function: %v", err)
	}
	if !hasFn {
		t.Error("set_updated_at() trigger function missing")
	}
}

func TestConnectFailsForUnreachableDatabase(t *testing.T) {
	_, err := db.Connect(context.Background(), "postgres://u:p@127.0.0.1:1/none?sslmode=disable&connect_timeout=1")
	if err == nil {
		t.Fatal("Connect succeeded against an unreachable database, want error")
	}
}
