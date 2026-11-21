package booking

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeState and fakeStore let each test plug in the behaviour it needs. A
// method whose function isn't set panics, so unexpected calls fail the test.
type fakeState struct {
	reserveFn      func(context.Context, Hold, time.Duration) (time.Time, error)
	heldByFn       func(context.Context, int64, int) (Hold, error)
	markSoldFn     func(context.Context, Hold) error
	availabilityFn func(context.Context, int64) (Availability, error)
	rebuildFn      func(context.Context, int64, func(context.Context) (Inventory, error)) error
}

func (f *fakeState) Reserve(ctx context.Context, h Hold, ttl time.Duration) (time.Time, error) {
	if f.reserveFn == nil {
		panic("fakeState: unexpected call to Reserve")
	}
	return f.reserveFn(ctx, h, ttl)
}

func (f *fakeState) HeldBy(ctx context.Context, eventID int64, seat int) (Hold, error) {
	if f.heldByFn == nil {
		panic("fakeState: unexpected call to HeldBy")
	}
	return f.heldByFn(ctx, eventID, seat)
}

func (f *fakeState) MarkSold(ctx context.Context, h Hold) error {
	if f.markSoldFn == nil {
		panic("fakeState: unexpected call to MarkSold")
	}
	return f.markSoldFn(ctx, h)
}

func (f *fakeState) Availability(ctx context.Context, eventID int64) (Availability, error) {
	if f.availabilityFn == nil {
		panic("fakeState: unexpected call to Availability")
	}
	return f.availabilityFn(ctx, eventID)
}

func (f *fakeState) Rebuild(ctx context.Context, eventID int64, load func(context.Context) (Inventory, error)) error {
	if f.rebuildFn == nil {
		panic("fakeState: unexpected call to Rebuild")
	}
	return f.rebuildFn(ctx, eventID, load)
}

type fakeStore struct {
	createBookingFn  func(context.Context, NewBooking) (Booking, error)
	bookingByOrderFn func(context.Context, string) (Booking, error)
	inventoryFn      func(context.Context, int64) (Inventory, error)
}

func (f *fakeStore) CreateBooking(ctx context.Context, b NewBooking) (Booking, error) {
	if f.createBookingFn == nil {
		panic("fakeStore: unexpected call to CreateBooking")
	}
	return f.createBookingFn(ctx, b)
}

func (f *fakeStore) BookingByOrder(ctx context.Context, orderID string) (Booking, error) {
	if f.bookingByOrderFn == nil {
		panic("fakeStore: unexpected call to BookingByOrder")
	}
	return f.bookingByOrderFn(ctx, orderID)
}

func (f *fakeStore) Inventory(ctx context.Context, eventID int64) (Inventory, error) {
	if f.inventoryFn == nil {
		panic("fakeStore: unexpected call to Inventory")
	}
	return f.inventoryFn(ctx, eventID)
}

var errRedisDown = errors.New("connection refused")

// aliceHold is a live hold by alice on event 42 seat 6.
var aliceHold = Hold{OrderID: "ord-1", EventID: 42, Seat: 6, UserID: "alice"}

func heldBy(h Hold) func(context.Context, int64, int) (Hold, error) {
	return func(context.Context, int64, int) (Hold, error) { return h, nil }
}

func noHold(context.Context, int64, int) (Hold, error) { return Hold{}, ErrHoldNotFound }

func TestReserveRebuildsMissingSeatState(t *testing.T) {
	expiry := time.Date(2027, 3, 1, 20, 10, 0, 0, time.UTC)
	calls := 0
	state := &fakeState{
		reserveFn: func(_ context.Context, h Hold, ttl time.Duration) (time.Time, error) {
			calls++
			if calls == 1 {
				return time.Time{}, ErrInventoryMissing
			}
			if ttl != DefaultHoldTTL || h.UserID != "alice" || h.Seat != 6 {
				t.Errorf("reserve got hold %+v, ttl %s", h, ttl)
			}
			return expiry, nil
		},
		rebuildFn: func(ctx context.Context, eventID int64, load func(context.Context) (Inventory, error)) error {
			_, err := load(ctx)
			return err
		},
	}
	store := &fakeStore{inventoryFn: func(context.Context, int64) (Inventory, error) { return Inventory{Seats: 10}, nil }}
	svc := NewService(state, store, DefaultHoldTTL)
	svc.newID = func() string { return "ord-1" }

	order, err := svc.Reserve(t.Context(), "alice", 42, 6)
	if err != nil {
		t.Fatal(err)
	}
	want := Order{ID: "ord-1", EventID: 42, Seat: 6, UserID: "alice", Status: StatusHeld, ExpiresAt: expiry}
	if order != want {
		t.Errorf("order = %+v, want %+v", order, want)
	}
	if calls != 2 {
		t.Errorf("reserve called %d times, want 2 (before and after the rebuild)", calls)
	}
}

func TestConfirm(t *testing.T) {
	confirmed := Order{ID: "ord-1", EventID: 42, Seat: 6, UserID: "alice", Status: StatusConfirmed}
	bookingOf := func(h Hold) Booking {
		return Booking{ID: 1, OrderID: h.OrderID, EventID: h.EventID, Seat: h.Seat, UserID: h.UserID}
	}

	tests := []struct {
		name         string
		user         string
		state        *fakeState
		store        *fakeStore
		want         Order
		wantErr      error
		wantMarkSold bool
	}{
		{
			name:         "live hold by the caller is booked",
			user:         "alice",
			state:        &fakeState{heldByFn: heldBy(aliceHold)},
			store:        &fakeStore{createBookingFn: func(_ context.Context, b NewBooking) (Booking, error) { return bookingOf(aliceHold), nil }},
			want:         confirmed,
			wantMarkSold: true,
		},
		{
			name:  "another user's hold is not theirs to confirm",
			user:  "bob",
			state: &fakeState{heldByFn: heldBy(aliceHold)},
			store: &fakeStore{bookingByOrderFn: func(context.Context, string) (Booking, error) { return Booking{}, ErrBookingNotFound }},
			// bob never reaches CreateBooking (it isn't set, so calling it would panic).
			wantErr: ErrHoldNotFound,
		},
		{
			name:    "expired hold that was never confirmed",
			user:    "alice",
			state:   &fakeState{heldByFn: noHold},
			store:   &fakeStore{bookingByOrderFn: func(context.Context, string) (Booking, error) { return Booking{}, ErrBookingNotFound }},
			wantErr: ErrHoldNotFound,
		},
		{
			name:  "retry after success returns the confirmed order",
			user:  "alice",
			state: &fakeState{heldByFn: noHold},
			store: &fakeStore{bookingByOrderFn: func(context.Context, string) (Booking, error) { return bookingOf(aliceHold), nil }},
			want:  confirmed,
		},
		{
			name:         "seat already sold in Postgres: backstop wins, Redis repaired",
			user:         "alice",
			state:        &fakeState{heldByFn: heldBy(aliceHold)},
			store:        &fakeStore{createBookingFn: func(context.Context, NewBooking) (Booking, error) { return Booking{}, ErrSeatTaken }},
			wantErr:      ErrSeatTaken,
			wantMarkSold: true,
		},
		{
			name:  "earlier confirm committed but didn't update Redis",
			user:  "alice",
			state: &fakeState{heldByFn: heldBy(aliceHold)},
			store: &fakeStore{
				createBookingFn:  func(context.Context, NewBooking) (Booking, error) { return Booking{}, ErrOrderAlreadyConfirmed },
				bookingByOrderFn: func(context.Context, string) (Booking, error) { return bookingOf(aliceHold), nil },
			},
			want:         confirmed,
			wantMarkSold: true,
		},
		{
			name:  "Redis unreachable fails closed",
			user:  "alice",
			state: &fakeState{heldByFn: func(context.Context, int64, int) (Hold, error) { return Hold{}, errRedisDown }},
			store: &fakeStore{}, // Postgres must not be touched
			// The error passes through unchanged.
			wantErr: errRedisDown,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			markedSold := false
			if tc.state.markSoldFn == nil {
				tc.state.markSoldFn = func(_ context.Context, h Hold) error {
					markedSold = true
					if h != aliceHold {
						t.Errorf("mark sold got %+v, want alice's hold", h)
					}
					return nil
				}
			}
			svc := NewService(tc.state, tc.store, DefaultHoldTTL)

			got, err := svc.Confirm(t.Context(), tc.user, "ord-1", 42, 6)

			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("order = %+v, want %+v", got, tc.want)
			}
			if markedSold != tc.wantMarkSold {
				t.Errorf("mark sold called = %v, want %v", markedSold, tc.wantMarkSold)
			}
		})
	}
}

// Once the booking is committed the order is confirmed, even if updating
// Redis fails afterwards.
func TestConfirmSucceedsWhenRedisFailsAfterCommit(t *testing.T) {
	state := &fakeState{
		heldByFn:   heldBy(aliceHold),
		markSoldFn: func(context.Context, Hold) error { return errRedisDown },
	}
	store := &fakeStore{createBookingFn: func(context.Context, NewBooking) (Booking, error) { return Booking{ID: 1}, nil }}

	got, err := NewService(state, store, DefaultHoldTTL).Confirm(t.Context(), "alice", "ord-1", 42, 6)
	if err != nil || got.Status != StatusConfirmed {
		t.Fatalf("got %+v, %v; want a confirmed order", got, err)
	}
}
