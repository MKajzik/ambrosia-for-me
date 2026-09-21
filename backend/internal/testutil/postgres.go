// Package testutil holds helpers shared by integration tests.
package testutil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/InzKazik/mealplanner/backend/internal/db"
)

var (
	startOnce sync.Once
	adminURL  string
	startErr  error
)

// requireDocker skips the test when Docker is unavailable, except on CI (the
// CI environment variable is non-empty) where a missing Docker fails the test
// so integration tests cannot pass while testing nothing.
func requireDocker(t *testing.T) {
	t.Helper()
	if os.Getenv("CI") == "" {
		testcontainers.SkipIfProviderIsNotHealthy(t)
		return
	}
	provider, err := testcontainers.ProviderDocker.GetProvider()
	if err != nil {
		t.Fatalf("Docker is required on CI: %v", err)
	}
	if err := provider.Health(context.Background()); err != nil {
		t.Fatalf("Docker is required on CI: %v", err)
	}
}

func startContainer(t *testing.T) {
	t.Helper()
	requireDocker(t)
	startOnce.Do(func() {
		ctx := context.Background()
		var ctr *tcpostgres.PostgresContainer
		ctr, startErr = tcpostgres.Run(ctx, "postgres:17-alpine",
			tcpostgres.WithDatabase("postgres"),
			tcpostgres.WithUsername("test"),
			tcpostgres.WithPassword("test"),
			tcpostgres.BasicWaitStrategies(),
		)
		if startErr != nil {
			return
		}
		// The container is removed by testcontainers' reaper when the test
		// process exits.
		adminURL, startErr = ctr.ConnectionString(ctx, "sslmode=disable")
	})
	if startErr != nil {
		t.Fatalf("start postgres container: %v", startErr)
	}
}

func databaseURL(t *testing.T, name string) string {
	t.Helper()
	u, err := url.Parse(adminURL)
	if err != nil {
		t.Fatalf("parse admin url: %v", err)
	}
	u.Path = "/" + name
	return u.String()
}

// createDatabase creates a database named name (from template when it is not
// empty) and returns its URL.
func createDatabase(t *testing.T, name, template string) string {
	t.Helper()
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatalf("connect to admin database: %v", err)
	}
	defer func() { _ = admin.Close(ctx) }()

	stmt := "CREATE DATABASE " + name
	if template != "" {
		stmt += " TEMPLATE " + template
	}
	if _, err := admin.Exec(ctx, stmt); err != nil {
		t.Fatalf("create database: %v", err)
	}
	return databaseURL(t, name)
}

func dropDatabase(name string) {
	ctx := context.Background()
	c, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return
	}
	defer func() { _ = c.Close(ctx) }()
	_, _ = c.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
}

// NewDatabase returns the URL of a fresh, empty database inside a Postgres
// container shared by every test in the package. The database is dropped when
// the test ends. The test is skipped when Docker is not available, except when
// the CI environment variable is set, where it fails.
func NewDatabase(t *testing.T) string {
	t.Helper()
	startContainer(t)

	name := "t_" + randomHex(t)
	dbURL := createDatabase(t, name, "")
	t.Cleanup(func() { dropDatabase(name) })
	return dbURL
}

var (
	templateOnce sync.Once
	templateName string
)

// NewMigratedDatabase returns the URL of a fresh database with every
// migration applied. Migrations run once per test package into a template
// database, and each call copies it, which is much cheaper than migrating per
// test. The database is dropped when the test ends. It skips or fails without
// Docker exactly like NewDatabase.
func NewMigratedDatabase(t *testing.T) string {
	t.Helper()
	startContainer(t)

	templateOnce.Do(func() {
		templateName = "tmpl_" + randomHex(t)
		tmplURL := createDatabase(t, templateName, "")
		if _, err := db.Migrate(context.Background(), tmplURL); err != nil {
			t.Fatalf("migrate template database: %v", err)
		}
	})
	if templateName == "" {
		t.Fatal("template database was not created by an earlier test")
	}

	name := "t_" + randomHex(t)
	dbURL := createDatabase(t, name, templateName)
	t.Cleanup(func() { dropDatabase(name) })
	return dbURL
}

func randomHex(t *testing.T) string {
	t.Helper()
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("random: %v", err)
	}
	return hex.EncodeToString(b)
}
