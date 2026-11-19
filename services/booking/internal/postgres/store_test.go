package postgres

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"ticketmaster-mvp/internal/testdb"
	"ticketmaster-mvp/services/booking/internal/booking"
	"ticketmaster-mvp/services/booking/migrations"
)

const eventID = 42

// Fixed UUIDv7-shaped order IDs for tests.
const (
	orderA = "0193f2c4-0000-7000-8000-00000000000a"
	orderB = "0193f2c4-0000-7000-8000-00000000000b"
)

// newStore returns a store over a fresh database with seats idx 0–2 for eventID.
func newStore(t *testing.T) (*Store, *pgxpool.Pool) {
	t.Helper()
	pool := testdb.New(t, "booking", migrations.FS)
	_, err := pool.Exec(t.Context(), `
		INSERT INTO seats (event_id, idx, section, row, number, price_cents)
		SELECT $1, n - 1, 'A', '1', n, 5000 FROM generate_series(1, 3) AS n`, eventID)
	if err != nil {
		t.Fatal(err)
	}
	return NewStore(pool), pool
}

func TestCreateBooking(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	b, err := store.CreateBooking(ctx, booking.NewBooking{OrderID: orderA, EventID: eventID, Seat: 1, UserID: "alice"})
	if err != nil {
		t.Fatalf("first booking: %v", err)
	}
	if b.ID == 0 || b.OrderID != orderA || b.Seat != 1 || b.UserID != "alice" || b.CreatedAt.IsZero() {
		t.Errorf("booking = %+v", b)
	}

	for _, tc := range []struct {
		name string
		nb   booking.NewBooking
		want error
	}{
		// The backstop: a second booking for the same seat, whatever the order.
		{"seat already booked", booking.NewBooking{OrderID: orderB, EventID: eventID, Seat: 1, UserID: "bob"}, booking.ErrSeatTaken},
		{"order already confirmed", booking.NewBooking{OrderID: orderA, EventID: eventID, Seat: 2, UserID: "alice"}, booking.ErrOrderAlreadyConfirmed},
		{"no such seat", booking.NewBooking{OrderID: orderB, EventID: eventID, Seat: 9, UserID: "bob"}, booking.ErrSeatNotFound},
		{"no such event", booking.NewBooking{OrderID: orderB, EventID: 7, Seat: 0, UserID: "bob"}, booking.ErrSeatNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := store.CreateBooking(ctx, tc.nb); !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestBookingByOrder(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()
	if _, err := store.CreateBooking(ctx, booking.NewBooking{OrderID: orderA, EventID: eventID, Seat: 2, UserID: "alice"}); err != nil {
		t.Fatal(err)
	}

	got, err := store.BookingByOrder(ctx, orderA)
	if err != nil {
		t.Fatal(err)
	}
	if got.OrderID != orderA || got.EventID != eventID || got.Seat != 2 || got.UserID != "alice" {
		t.Errorf("booking = %+v", got)
	}
	for _, order := range []string{orderB, "not-a-uuid"} {
		if _, err := store.BookingByOrder(ctx, order); !errors.Is(err, booking.ErrBookingNotFound) {
			t.Errorf("order %q: err = %v, want ErrBookingNotFound", order, err)
		}
	}
}

func TestInventory(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()
	if _, err := store.CreateBooking(ctx, booking.NewBooking{OrderID: orderA, EventID: eventID, Seat: 2, UserID: "alice"}); err != nil {
		t.Fatal(err)
	}

	inv, err := store.Inventory(ctx, eventID)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Seats != 3 || !slices.Equal(inv.Sold, []int{2}) {
		t.Errorf("inventory = %+v, want 3 seats with seat 2 sold", inv)
	}
	if _, err := store.Inventory(ctx, 7); !errors.Is(err, booking.ErrNoInventory) {
		t.Errorf("unknown event: err = %v, want ErrNoInventory", err)
	}
}

func TestListSeats(t *testing.T) {
	store, _ := newStore(t)
	ctx := context.Background()
	if _, err := store.CreateBooking(ctx, booking.NewBooking{OrderID: orderA, EventID: eventID, Seat: 1, UserID: "alice"}); err != nil {
		t.Fatal(err)
	}

	seats, err := store.ListSeats(ctx, eventID)
	if err != nil {
		t.Fatal(err)
	}
	if len(seats) != 3 {
		t.Fatalf("got %d seats, want 3", len(seats))
	}
	for _, seat := range seats {
		if want := seat.Idx != 1; seat.Available != want {
			t.Errorf("seat %d: available = %v, want %v", seat.Idx, seat.Available, want)
		}
	}
	if _, err := store.ListSeats(ctx, 999); !errors.Is(err, booking.ErrNoInventory) {
		t.Fatalf("unknown event: err = %v, want ErrNoInventory", err)
	}
}
