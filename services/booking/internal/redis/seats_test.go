package redis

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"ticketmaster-mvp/internal/testredis"
	"ticketmaster-mvp/services/booking/internal/booking"
)

const event = 42

// newState returns seat state for an event with 10 seats, seat 3 already sold.
func newState(t *testing.T) *SeatState {
	t.Helper()
	s := NewSeatState(testredis.New(t))
	load := func(context.Context) (booking.Inventory, error) {
		return booking.Inventory{Seats: 10, Sold: []int{3}}, nil
	}
	if err := s.Rebuild(t.Context(), event, load); err != nil {
		t.Fatal(err)
	}
	return s
}

func hold(order string, seat int) booking.Hold {
	return booking.Hold{OrderID: order, EventID: event, Seat: seat, UserID: "user-" + order}
}

func TestReserve(t *testing.T) {
	s := newState(t)
	ctx := t.Context()

	exp, err := s.Reserve(ctx, hold("o1", 5), time.Minute)
	if err != nil {
		t.Fatalf("reserve free seat: %v", err)
	}
	// The expiry comes from Redis's clock, which can be slightly ahead of
	// ours (Redis runs in Docker), so allow up to a second of skew.
	if until := time.Until(exp); until < 50*time.Second || until > time.Minute+time.Second {
		t.Errorf("expiry in %s, want about 1m", until)
	}

	for _, tc := range []struct {
		name string
		h    booking.Hold
		want error
	}{
		{"held seat", hold("o2", 5), booking.ErrSeatHeld},
		{"sold seat", hold("o2", 3), booking.ErrSeatTaken},
		{"seat past the end", hold("o2", 10), booking.ErrSeatNotFound},
		{"negative seat", hold("o2", -1), booking.ErrSeatNotFound},
		{"event with no state", booking.Hold{OrderID: "o2", EventID: 7, Seat: 0, UserID: "u"}, booking.ErrInventoryMissing},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.Reserve(ctx, tc.h, time.Minute); !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}

	got, err := s.HeldBy(ctx, event, 5)
	if err != nil {
		t.Fatalf("held by: %v", err)
	}
	if got.OrderID != "o1" || got.UserID != "user-o1" {
		t.Errorf("held by %+v, want order o1 / user-o1", got)
	}
	if _, err := s.HeldBy(ctx, event, 6); !errors.Is(err, booking.ErrHoldNotFound) {
		t.Errorf("held by on a free seat: err = %v, want ErrHoldNotFound", err)
	}
}

func TestAvailability(t *testing.T) {
	s := newState(t)
	ctx := t.Context()
	if _, err := s.Reserve(ctx, hold("o1", 5), time.Minute); err != nil {
		t.Fatal(err)
	}

	a, err := s.Availability(ctx, event)
	if err != nil {
		t.Fatal(err)
	}
	if a.Seats != 10 {
		t.Errorf("seats = %d, want 10", a.Seats)
	}
	for seat := range 10 {
		want := seat != 3 && seat != 5 // 3 is sold, 5 is held
		if a.Available(seat) != want {
			t.Errorf("seat %d available = %v, want %v", seat, a.Available(seat), want)
		}
	}

	if _, err := s.Availability(ctx, 7); !errors.Is(err, booking.ErrInventoryMissing) {
		t.Errorf("event with no state: err = %v, want ErrInventoryMissing", err)
	}
}

// An expired hold frees the seat the moment it expires, for both the seat
// map and the next reserve, with no cleanup step in between.
func TestHoldExpiry(t *testing.T) {
	s := newState(t)
	ctx := t.Context()
	if _, err := s.Reserve(ctx, hold("o1", 5), 50*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)

	a, err := s.Availability(ctx, event)
	if err != nil {
		t.Fatal(err)
	}
	if !a.Available(5) {
		t.Error("seat 5 still unavailable after its hold expired")
	}
	if _, err := s.HeldBy(ctx, event, 5); !errors.Is(err, booking.ErrHoldNotFound) {
		t.Errorf("held by after expiry: err = %v, want ErrHoldNotFound", err)
	}
	if _, err := s.Reserve(ctx, hold("o2", 5), time.Minute); err != nil {
		t.Errorf("reserve after expiry: %v", err)
	}
}

func TestMarkSold(t *testing.T) {
	s := newState(t)
	ctx := t.Context()
	h := hold("o1", 5)
	if _, err := s.Reserve(ctx, h, time.Minute); err != nil {
		t.Fatal(err)
	}

	if err := s.MarkSold(ctx, h); err != nil {
		t.Fatal(err)
	}
	if _, err := s.HeldBy(ctx, event, 5); !errors.Is(err, booking.ErrHoldNotFound) {
		t.Errorf("lock not released: err = %v", err)
	}
	if _, err := s.Reserve(ctx, hold("o2", 5), time.Minute); !errors.Is(err, booking.ErrSeatTaken) {
		t.Errorf("reserve sold seat: err = %v, want ErrSeatTaken", err)
	}
	a, err := s.Availability(ctx, event)
	if err != nil {
		t.Fatal(err)
	}
	if a.Available(5) || len(a.Held) != 0 {
		t.Errorf("after mark sold: available(5) = %v, held = %v; want false, []", a.Available(5), a.Held)
	}
}

// A stale order (its hold expired and someone else claimed the seat) can't
// release the new order's lock.
func TestMarkSoldKeepsAnotherOrdersLock(t *testing.T) {
	s := newState(t)
	ctx := t.Context()
	if _, err := s.Reserve(ctx, hold("fresh", 5), time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkSold(ctx, hold("stale", 5)); err != nil {
		t.Fatal(err)
	}
	got, err := s.HeldBy(ctx, event, 5)
	if err != nil || got.OrderID != "fresh" {
		t.Errorf("held by = %+v, %v; want the fresh order's lock untouched", got, err)
	}
}

// Rebuilding merges with existing sold bits and never clears them, so a seat
// sold while a rebuild was reading Postgres stays sold.
func TestRebuildNeverClearsSoldSeats(t *testing.T) {
	s := newState(t)
	ctx := t.Context()
	if err := s.MarkSold(ctx, hold("o1", 8)); err != nil {
		t.Fatal(err)
	}
	if err := s.rdb.Del(ctx, countKey(event)).Err(); err != nil {
		t.Fatal(err)
	}
	stale := func(context.Context) (booking.Inventory, error) {
		return booking.Inventory{Seats: 10, Sold: []int{3}}, nil // doesn't know about seat 8
	}
	if err := s.Rebuild(ctx, event, stale); err != nil {
		t.Fatal(err)
	}
	a, err := s.Availability(ctx, event)
	if err != nil {
		t.Fatal(err)
	}
	if a.Available(8) || a.Available(3) {
		t.Errorf("available(8) = %v, available(3) = %v; want both sold", a.Available(8), a.Available(3))
	}
}

// buyers is more than DB_MAX_CONNS (20) and the Redis client's default pool,
// so some goroutines wait for a connection, as in real traffic. Load at scale
// is the k6 test's job (T4).
const buyers = 50

// Many buyers claiming one seat at the same instant: exactly one wins.
func TestReserveRace(t *testing.T) {
	s := newState(t)
	ctx := t.Context()

	start := make(chan struct{}) // closed to release every buyer at once
	errs := make(chan error, buyers)
	for i := range buyers {
		go func() {
			<-start
			_, err := s.Reserve(ctx, hold(fmt.Sprintf("o%d", i), 7), time.Minute)
			errs <- err
		}()
	}
	close(start)

	won := 0
	for range buyers {
		switch err := <-errs; {
		case err == nil:
			won++
		case !errors.Is(err, booking.ErrSeatHeld):
			t.Errorf("unexpected error: %v", err)
		}
	}
	if won != 1 {
		t.Fatalf("%d buyers won seat 7, want exactly 1", won)
	}
}
