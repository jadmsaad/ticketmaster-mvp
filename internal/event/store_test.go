package event

import (
	"context"
	"errors"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"ticketmaster-mvp/internal/db"
)

const testDatabase = "ticketmaster_test"

// setup recreates a dedicated test database, so tests never touch app data.
// TEST_POSTGRES_URL must point at the server's maintenance database.
func setup(t *testing.T) (*Store, *pgxpool.Pool) {
	t.Helper()
	adminURL := os.Getenv("TEST_POSTGRES_URL")
	if adminURL == "" {
		t.Skip("TEST_POSTGRES_URL not set")
	}
	ctx := context.Background()

	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	for _, stmt := range []string{
		"DROP DATABASE IF EXISTS " + testDatabase + " WITH (FORCE)",
		"CREATE DATABASE " + testDatabase,
	} {
		if _, err := admin.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}

	u, err := url.Parse(adminURL)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + testDatabase
	testURL := u.String()

	if err := db.Migrate(ctx, testURL); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, testURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return NewStore(pool), pool
}

func TestListSeats(t *testing.T) {
	store, pool := setup(t)
	ctx := context.Background()

	var eventID int64
	err := pool.QueryRow(ctx, `
		INSERT INTO events (name, venue, starts_at)
		VALUES ('Test', 'Hall', $1) RETURNING id`, time.Now()).Scan(&eventID)
	if err != nil {
		t.Fatal(err)
	}
	var free, booked, doubleBooked int64
	for number, id := range []*int64{&free, &booked, &doubleBooked} {
		err := pool.QueryRow(ctx, `
			INSERT INTO seats (event_id, section, row, number, price_cents)
			VALUES ($1, 'A', '1', $2, 5000) RETURNING id`, eventID, number+1).Scan(id)
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, seatID := range []int64{booked, doubleBooked, doubleBooked} {
		_, err := pool.Exec(ctx, `INSERT INTO bookings (seat_id, user_id) VALUES ($1, 'u')`, seatID)
		if err != nil {
			t.Fatal(err)
		}
	}

	seats, err := store.ListSeats(ctx, eventID)
	if err != nil {
		t.Fatal(err)
	}

	// The double-booked seat must appear once, not once per booking.
	if len(seats) != 3 {
		t.Fatalf("got %d seats, want 3: %+v", len(seats), seats)
	}
	want := map[int64]bool{free: true, booked: false, doubleBooked: false}
	for _, seat := range seats {
		if seat.Available != want[seat.ID] {
			t.Errorf("seat %d: available = %v, want %v", seat.ID, seat.Available, want[seat.ID])
		}
	}
}

func TestListSeatsUnknownEvent(t *testing.T) {
	store, _ := setup(t)
	_, err := store.ListSeats(context.Background(), 999)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("got error %v, want ErrNotFound", err)
	}
}
