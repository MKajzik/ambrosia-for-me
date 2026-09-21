// Package testutil holds helpers shared by integration tests.
package testutil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

var (
	startOnce sync.Once
	adminURL  string
	startErr  error
)

// NewDatabase returns the URL of a fresh, empty database inside a Postgres
// container shared by every test in the package. The database is dropped when
// the test ends. The test is skipped when Docker is not available.
func NewDatabase(t *testing.T) string {
	t.Helper()
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	startOnce.Do(func() {
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

	name := "t_" + randomHex(t)
	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatalf("connect to admin database: %v", err)
	}
	defer func() { _ = admin.Close(ctx) }()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		c, err := pgx.Connect(ctx, adminURL)
		if err != nil {
			return
		}
		defer func() { _ = c.Close(ctx) }()
		_, _ = c.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})

	u, err := url.Parse(adminURL)
	if err != nil {
		t.Fatalf("parse admin url: %v", err)
	}
	u.Path = "/" + name
	return u.String()
}

func randomHex(t *testing.T) string {
	t.Helper()
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("random: %v", err)
	}
	return hex.EncodeToString(b)
}
