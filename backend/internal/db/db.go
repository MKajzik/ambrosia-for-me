// Package db opens Postgres connections and applies schema migrations.
package db

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/InzKazik/mealplanner/backend/migrations"
)

// Connect opens a pgx pool and verifies the database is reachable.
func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("open pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

// Migrate applies every pending migration in backend/migrations and returns
// how many it applied.
func Migrate(ctx context.Context, url string) (int, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return 0, fmt.Errorf("parse database url: %w", err)
	}
	sqlDB := stdlib.OpenDB(*cfg.ConnConfig)
	defer func() { _ = sqlDB.Close() }()

	return migrate(ctx, sqlDB)
}

func migrate(ctx context.Context, sqlDB *sql.DB) (int, error) {
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations.FS)
	if err != nil {
		return 0, fmt.Errorf("create migration provider: %w", err)
	}
	results, err := provider.Up(ctx)
	if err != nil {
		return 0, fmt.Errorf("apply migrations: %w", err)
	}
	return len(results), nil
}
