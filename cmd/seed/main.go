// Command seed creates the sample event and its seat inventory. Dev only.
//
// It connects as the admin user and writes to both services' schemas. That
// crosses the service boundary on purpose: in production, booking would create
// inventory when it receives an "event created" message.
//
// It is safe to re-run. Pass -reset to wipe all data first, including bookings.
// Run it after both services' migrations.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
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
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	// One transaction, so an event can never exist without its seats.
	return pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		if reset {
			_, err := tx.Exec(ctx, `TRUNCATE event.events, booking.seats, booking.bookings RESTART IDENTITY`)
			if err != nil {
				return fmt.Errorf("reset: %w", err)
			}
		}

		var exists bool
		err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM event.events WHERE name = $1)`, eventName).Scan(&exists)
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
			INSERT INTO event.events (name, venue, starts_at)
			VALUES ($1, $2, $3)
			RETURNING id`, eventName, venue, startsAt).Scan(&eventID)
		if err != nil {
			return fmt.Errorf("insert event: %w", err)
		}

		tag, err := tx.Exec(ctx, `
			INSERT INTO booking.seats (event_id, section, row, number, price_cents)
			SELECT $1::bigint, 'A', r::text, n, $2::int
			FROM generate_series(1, $3::int) AS r, generate_series(1, $4::int) AS n`,
			eventID, priceCents, rows, seatsInRow)
		if err != nil {
			return fmt.Errorf("insert seats: %w", err)
		}
		slog.Info("seeded", "event", eventName, "event_id", eventID, "seats", tag.RowsAffected())
		return nil
	})
}
