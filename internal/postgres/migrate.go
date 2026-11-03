// Package postgres holds Postgres setup shared by all services.
package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	"github.com/pressly/goose/v3"
)

// Migrate applies any of the given migrations that haven't run yet.
//
// Each service connects with search_path set to its own schema, so its tables
// and goose's version table are created in that schema.
func Migrate(ctx context.Context, databaseURL string, migrations fs.FS) error {
	// goose works with database/sql, while the services use pgxpool.
	conn, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer conn.Close()

	provider, err := goose.NewProvider(goose.DialectPostgres, conn, migrations)
	if err != nil {
		return fmt.Errorf("create migration provider: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}
