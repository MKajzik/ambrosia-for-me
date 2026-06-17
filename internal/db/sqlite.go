package db

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}

	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	if err := migrate(db); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return db, nil
}

func migrate(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS ingredients (
		id                    INTEGER PRIMARY KEY AUTOINCREMENT,
		name                  TEXT NOT NULL,
		base_unit             TEXT NOT NULL CHECK(base_unit IN ('g', 'ml', 'pcs')),
		category              TEXT NOT NULL DEFAULT 'Inne' CHECK(category IN (
			'Owoce','Warzywa','Mięso','Ryby','Nabiał','Pieczywo',
			'Produkty zbożowe','Przyprawy','Napoje','Słodycze','Tłuszcze','Inne'
		)),
		calories_per_100      REAL NOT NULL DEFAULT 0,
		protein_per_100       REAL NOT NULL DEFAULT 0,
		fat_per_100           REAL NOT NULL DEFAULT 0,
		carbs_per_100         REAL NOT NULL DEFAULT 0,
		fiber_per_100         REAL NOT NULL DEFAULT 0,
		salt_per_100          REAL NOT NULL DEFAULT 0,
		sugars_per_100        REAL NOT NULL DEFAULT 0,
		saturated_fat_per_100 REAL NOT NULL DEFAULT 0,
		package_size          REAL,
		created_at            DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at            DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS meals (
		id           INTEGER PRIMARY KEY AUTOINCREMENT,
		name         TEXT NOT NULL,
		meal_type    TEXT NOT NULL CHECK(meal_type IN ('breakfast', 'lunch', 'dinner', 'snack')),
		instructions TEXT DEFAULT '',
		created_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at   DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS meal_ingredients (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		meal_id       INTEGER NOT NULL REFERENCES meals(id) ON DELETE CASCADE,
		ingredient_id INTEGER NOT NULL REFERENCES ingredients(id) ON DELETE CASCADE,
		quantity      REAL NOT NULL CHECK(quantity > 0),
		unit          TEXT NOT NULL CHECK(unit IN ('g', 'ml', 'l', 'pcs', 'pkg')),
		created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_meal_ingredients_meal ON meal_ingredients(meal_id);
	CREATE INDEX IF NOT EXISTS idx_meal_ingredients_ingredient ON meal_ingredients(ingredient_id);

	CREATE TABLE IF NOT EXISTS meal_plans (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		name       TEXT NOT NULL,
		start_date TEXT NOT NULL,
		end_date   TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS meal_plan_entries (
		id           INTEGER PRIMARY KEY AUTOINCREMENT,
		meal_plan_id INTEGER NOT NULL REFERENCES meal_plans(id) ON DELETE CASCADE,
		date         TEXT NOT NULL,
		meal_type    TEXT NOT NULL CHECK(meal_type IN ('breakfast', 'lunch', 'dinner', 'snack')),
		meal_id      INTEGER NOT NULL REFERENCES meals(id) ON DELETE CASCADE,
		created_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at   DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_meal_plan_entries_plan ON meal_plan_entries(meal_plan_id);
	CREATE INDEX IF NOT EXISTS idx_meal_plan_entries_date ON meal_plan_entries(date);
	`

	_, err := db.Exec(schema)
	return err
}
