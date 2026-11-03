// Package db holds database setup shared by the commands.
package db

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	"github.com/pressly/goose/v3"

	"ticketmaster-mvp/migrations"
)

// Migrate applies any migrations that haven't run yet.
func Migrate(ctx context.Context, databaseURL string) error {
	// goose works with database/sql, while the app itself uses pgxpool.
	conn, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer conn.Close()

	provider, err := goose.NewProvider(goose.DialectPostgres, conn, migrations.FS)
	if err != nil {
		return fmt.Errorf("create migration provider: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}
