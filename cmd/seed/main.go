// Command seed applies migrations and creates the sample event with its seats.
// It is safe to re-run. Pass -reset to wipe all data first, including bookings.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5"

	"ticketmaster-mvp/internal/db"
)

const (
	eventName  = "The Goroutines: Live"
	venue      = "Demo Arena"
	rows       = 10
	seatsInRow = 10
	priceCents = 5000
)

func main() {
	reset := flag.Bool("reset", false, "delete all events, seats, and bookings first")
	flag.Parse()

	if err := run(context.Background(), os.Getenv("DATABASE_URL"), *reset); err != nil {
		slog.Error("seed failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, databaseURL string, reset bool) error {
	if err := db.Migrate(ctx, databaseURL); err != nil {
		return err
	}

	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close(ctx)

	// One transaction, so an event can never exist without its seats.
	return pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		if reset {
			if _, err := tx.Exec(ctx, `TRUNCATE events, seats, bookings RESTART IDENTITY`); err != nil {
				return fmt.Errorf("reset: %w", err)
			}
		}

		var exists bool
		err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM events WHERE name = $1)`, eventName).Scan(&exists)
		if err != nil {
			return fmt.Errorf("check event: %w", err)
		}
		if exists {
			slog.Info("already seeded", "event", eventName)
			return nil
		}

		var eventID int64
		startsAt := time.Date(2027, time.March, 1, 20, 0, 0, 0, time.UTC)
		err = tx.QueryRow(ctx, `
			INSERT INTO events (name, venue, starts_at)
			VALUES ($1, $2, $3)
			RETURNING id`, eventName, venue, startsAt).Scan(&eventID)
		if err != nil {
			return fmt.Errorf("insert event: %w", err)
		}

		tag, err := tx.Exec(ctx, `
			INSERT INTO seats (event_id, section, row, number, price_cents)
			SELECT $1::bigint, 'A', r::text, n, $2::int
			FROM generate_series(1, $3::int) AS r, generate_series(1, $4::int) AS n`,
			eventID, priceCents, rows, seatsInRow)
		if err != nil {
			return fmt.Errorf("insert seats: %w", err)
		}
		slog.Info("seeded", "event", eventName, "seats", tag.RowsAffected())
		return nil
	})
}
