package db

import (
	"testing"
)

func TestOpen(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer database.Close()

	if err := database.Ping(); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
}

func TestMigrateCreatesTables(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer database.Close()

	tables := []string{"ingredients", "meals", "meal_ingredients", "meal_plans", "meal_plan_entries"}
	for _, table := range tables {
		var name string
		err := database.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", table, err)
		}
	}
}

func TestForeignKeysEnabled(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer database.Close()

	var fk string
	if err := database.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatalf("PRAGMA foreign_keys error = %v", err)
	}
	if fk != "1" {
		t.Errorf("foreign_keys = %q, want %q", fk, "1")
	}
}

func TestMigrateIdempotent(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer database.Close()

	if err := migrate(database); err != nil {
		t.Fatalf("second migrate() error = %v", err)
	}
}
