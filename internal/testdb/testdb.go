// Package testdb gives integration tests a fresh, migrated database.
package testdb

import (
	"context"
	"io/fs"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"ticketmaster-mvp/internal/postgres"
)

// New recreates a test database named after schema, creates the schema in it,
// applies migrations, and returns a pool whose search_path is that schema,
// matching how the service connects in production.
//
// It skips the test unless TEST_POSTGRES_URL points at a Postgres server.
func New(t *testing.T, schema string, migrations fs.FS) *pgxpool.Pool {
	t.Helper()
	adminURL := os.Getenv("TEST_POSTGRES_URL")
	if adminURL == "" {
		t.Skip("TEST_POSTGRES_URL not set")
	}
	ctx := context.Background()
	dbName := "test_" + schema

	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = admin.Close(ctx) }()
	quoted := pgx.Identifier{dbName}.Sanitize()
	for _, stmt := range []string{
		"DROP DATABASE IF EXISTS " + quoted + " WITH (FORCE)",
		"CREATE DATABASE " + quoted,
	} {
		if _, err := admin.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}

	u, err := url.Parse(adminURL)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + dbName
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	testURL := u.String()

	conn, err := pgx.Connect(ctx, testURL)
	if err != nil {
		t.Fatal(err)
	}
	_, err = conn.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize())
	if closeErr := conn.Close(ctx); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}

	if err := postgres.Migrate(ctx, testURL, migrations); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, testURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}
