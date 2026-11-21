// External test package: it wires the real adapters into the service, which
// the booking package itself can't import (adapters depend on booking).
package booking_test

import (
	"errors"
	"fmt"
	"testing"

	"ticketmaster-mvp/internal/testdb"
	"ticketmaster-mvp/internal/testredis"
	"ticketmaster-mvp/services/booking/internal/booking"
	"ticketmaster-mvp/services/booking/internal/postgres"
	"ticketmaster-mvp/services/booking/internal/redis"
	"ticketmaster-mvp/services/booking/migrations"
)

const event = 42

// newService wires the service to a fresh Postgres (event 42 with 10 seats)
// and an empty Redis, so the first call rebuilds seat state from Postgres.
func newService(t *testing.T) *booking.Service {
	t.Helper()
	pool := testdb.New(t, "booking", migrations.FS)
	_, err := pool.Exec(t.Context(), `
		INSERT INTO seats (event_id, idx, section, row, number, price_cents)
		SELECT $1, n - 1, 'A', '1', n, 5000 FROM generate_series(1, 10) AS n`, event)
	if err != nil {
		t.Fatal(err)
	}
	return booking.NewService(redis.NewSeatState(testredis.New(t)), postgres.NewStore(pool), booking.DefaultHoldTTL)
}

func TestReserveConfirmFlow(t *testing.T) {
	svc := newService(t)
	ctx := t.Context()

	held, err := svc.Reserve(ctx, "alice", event, 6)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if held.Status != booking.StatusHeld || held.ID == "" || held.ExpiresAt.IsZero() {
		t.Fatalf("held order = %+v", held)
	}
	if _, err := svc.Reserve(ctx, "bob", event, 6); !errors.Is(err, booking.ErrSeatHeld) {
		t.Fatalf("bob reserves a held seat: err = %v, want ErrSeatHeld", err)
	}

	confirmed, err := svc.Confirm(ctx, "alice", held.ID, event, 6)
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if confirmed.Status != booking.StatusConfirmed || confirmed.ID != held.ID {
		t.Fatalf("confirmed order = %+v", confirmed)
	}

	// A retried confirm (lost response, double click) returns the same order.
	again, err := svc.Confirm(ctx, "alice", held.ID, event, 6)
	if err != nil || again != confirmed {
		t.Fatalf("retried confirm = %+v, %v; want %+v", again, err, confirmed)
	}

	a, err := svc.Availability(ctx, event)
	if err != nil {
		t.Fatal(err)
	}
	if a.Available(6) || !a.Available(5) {
		t.Errorf("available(6) = %v, available(5) = %v; want false, true", a.Available(6), a.Available(5))
	}
	if _, err := svc.Reserve(ctx, "bob", event, 6); !errors.Is(err, booking.ErrSeatTaken) {
		t.Errorf("reserve a sold seat: err = %v, want ErrSeatTaken", err)
	}
	if _, err := svc.Reserve(ctx, "bob", 7, 0); !errors.Is(err, booking.ErrNoInventory) {
		t.Errorf("reserve for an event with no seats: err = %v, want ErrNoInventory", err)
	}
}

// buyers is more than DB_MAX_CONNS (20), so some goroutines wait for a
// connection, as in real traffic. Load at scale is the k6 test's job (T4).
const buyers = 50

// Many buyers try to reserve and confirm one seat at the same instant: one
// booking results, and everyone else is told the seat is held.
func TestOneBookingPerSeatUnderContention(t *testing.T) {
	svc := newService(t)
	ctx := t.Context()
	// Build seat state before the race, so the race tests booking, not rebuilds.
	if _, err := svc.Availability(ctx, event); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{}) // closed to release every buyer at once
	errs := make(chan error, buyers)
	for i := range buyers {
		go func() {
			<-start
			user := fmt.Sprintf("buyer-%d", i)
			order, err := svc.Reserve(ctx, user, event, 7)
			if err == nil {
				_, err = svc.Confirm(ctx, user, order.ID, event, 7)
			}
			errs <- err
		}()
	}
	close(start)

	booked := 0
	for range buyers {
		switch err := <-errs; {
		case err == nil:
			booked++
		case !errors.Is(err, booking.ErrSeatHeld) && !errors.Is(err, booking.ErrSeatTaken):
			t.Errorf("unexpected error: %v", err)
		}
	}
	if booked != 1 {
		t.Fatalf("%d buyers booked seat 7, want exactly 1", booked)
	}
}
