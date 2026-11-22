package booking

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

// DefaultHoldTTL is how long a buyer has to confirm: the deadline the API
// returns and the UI counts down.
const DefaultHoldTTL = 10 * time.Minute

// confirmGrace is how much longer the Redis lock lives than the deadline the
// buyer sees. Confirms are refused once the deadline passes, so a confirm
// always finishes while its lock still has at least this long left: the lock
// can't expire mid-confirm and let another buyer take the seat (ADR 11).
const confirmGrace = time.Minute

// seatState is the fast seat state: holds and sold seats (Redis in production).
type seatState interface {
	// Reserve claims the seat for h's order for ttl and returns the hold now on
	// the seat, with ExpiresAt set to when its lock expires. If h's user already
	// holds the seat, it returns that existing hold, unchanged: a retry never
	// extends a hold.
	// It returns ErrSeatTaken, ErrSeatHeld, ErrSeatNotFound, or
	// ErrInventoryMissing.
	Reserve(ctx context.Context, h Hold, ttl time.Duration) (Hold, error)
	// HeldBy returns the live hold on a seat (ExpiresAt is when its lock
	// expires), or ErrHoldNotFound.
	HeldBy(ctx context.Context, eventID int64, seat int) (Hold, error)
	// MarkSold records the seat as sold and releases h's hold.
	MarkSold(ctx context.Context, h Hold) error
	// Availability returns the event's seat state, or ErrInventoryMissing.
	Availability(ctx context.Context, eventID int64) (Availability, error)
	// Rebuild loads the event's seat state from load. Concurrent callers
	// for the same event share one rebuild.
	Rebuild(ctx context.Context, eventID int64, load func(context.Context) (Inventory, error)) error
}

// bookingStore is the system of record for seats and bookings (Postgres).
type bookingStore interface {
	// CreateBooking returns ErrSeatTaken, ErrSeatNotFound, or
	// ErrOrderAlreadyConfirmed.
	CreateBooking(ctx context.Context, b NewBooking) (Booking, error)
	// BookingByOrder returns ErrBookingNotFound.
	BookingByOrder(ctx context.Context, orderID string) (Booking, error)
	// Inventory returns ErrNoInventory if the event has no seats.
	Inventory(ctx context.Context, eventID int64) (Inventory, error)
}

// Service holds the booking rules. Reserve and seat state stay in Redis;
// only confirm writes to Postgres, whose UNIQUE (seat_id) is the final say.
type Service struct {
	state   seatState
	store   bookingStore
	holdTTL time.Duration
	newID   func() string
}

func NewService(state seatState, store bookingStore, holdTTL time.Duration) *Service {
	return &Service{state: state, store: store, holdTTL: holdTTL, newID: newOrderID}
}

// newOrderID returns a UUIDv7: unique without coordination, and time-ordered,
// so confirmed orders insert into Postgres indexes in order.
func newOrderID() string {
	return uuid.Must(uuid.NewV7()).String()
}

// Reserve holds a seat for userID and returns the held order. It never
// touches Postgres, except to rebuild missing seat state.
//
// Reserve is idempotent per user and seat (ADR 11, amended): if userID already
// holds the seat (a retried request, a double click, a second tab), it returns
// that existing order with its original deadline.
func (s *Service) Reserve(ctx context.Context, userID string, eventID int64, seat int) (Order, error) {
	h := Hold{OrderID: s.newID(), EventID: eventID, Seat: seat, UserID: userID}
	var held Hold
	err := s.withSeatState(ctx, eventID, func() error {
		var err error
		// The lock outlives the buyer's deadline by confirmGrace.
		held, err = s.state.Reserve(ctx, h, s.holdTTL+confirmGrace)
		return err
	})
	if err != nil {
		return Order{}, err
	}
	// Only an existing hold can be past its deadline: the buyer's order can no
	// longer be confirmed, and the seat frees up when the grace period ends.
	if !held.beforeDeadline() {
		return Order{}, ErrSeatHeld
	}
	return held.heldOrder(), nil
}

// Confirm turns a held order into a booking. A retry after success returns
// the same confirmed order.
func (s *Service) Confirm(ctx context.Context, userID, orderID string, eventID int64, seat int) (Order, error) {
	h, err := s.state.HeldBy(ctx, eventID, seat)
	switch {
	case err == nil && h.OrderID == orderID && h.UserID == userID && h.beforeDeadline():
		return s.book(ctx, h)
	case err != nil && !errors.Is(err, ErrHoldNotFound):
		// Redis is unreachable: fail closed rather than guess.
		return Order{}, err
	}
	// No live hold for this order: it may already have been confirmed.
	return s.alreadyConfirmed(ctx, userID, orderID, eventID, seat)
}

// book inserts the booking for a verified hold, then updates Redis.
func (s *Service) book(ctx context.Context, h Hold) (Order, error) {
	_, err := s.store.CreateBooking(ctx, NewBooking{OrderID: h.OrderID, EventID: h.EventID, Seat: h.Seat, UserID: h.UserID})
	switch {
	case errors.Is(err, ErrOrderAlreadyConfirmed):
		// An earlier confirm committed but didn't finish updating Redis.
		s.markSold(ctx, h)
		return s.alreadyConfirmed(ctx, h.UserID, h.OrderID, h.EventID, h.Seat)
	case errors.Is(err, ErrSeatTaken):
		// Redis didn't know the seat was sold (a lost sold bit). Repair it
		// and release this hold; the backstop has done its job.
		s.markSold(ctx, h)
		return Order{}, ErrSeatTaken
	case err != nil:
		return Order{}, err
	}
	// The booking is committed, so the order is confirmed even if Redis
	// fails now: the hold expires, and a later confirm attempt or rebuild
	// repairs the sold bit.
	s.markSold(ctx, h)
	return h.confirmedOrder(), nil
}

func (s *Service) markSold(ctx context.Context, h Hold) {
	if err := s.state.MarkSold(ctx, h); err != nil {
		slog.WarnContext(ctx, "mark seat sold in seat state", "event_id", h.EventID, "seat", h.Seat, "order_id", h.OrderID, "error", err)
	}
}

// alreadyConfirmed returns the order if it was already booked by this user for
// this seat, and ErrHoldNotFound otherwise, so another user who knows the
// order ID learns nothing.
func (s *Service) alreadyConfirmed(ctx context.Context, userID, orderID string, eventID int64, seat int) (Order, error) {
	b, err := s.store.BookingByOrder(ctx, orderID)
	if errors.Is(err, ErrBookingNotFound) {
		return Order{}, ErrHoldNotFound
	}
	if err != nil {
		return Order{}, err
	}
	if b.UserID != userID || b.EventID != eventID || b.Seat != seat {
		return Order{}, ErrHoldNotFound
	}
	return b.order(), nil
}

// Availability returns the event's current seat state and prices, for the
// seat map. Like Reserve, it reads only Redis (ADR 12).
func (s *Service) Availability(ctx context.Context, eventID int64) (Availability, error) {
	var a Availability
	err := s.withSeatState(ctx, eventID, func() error {
		var err error
		a, err = s.state.Availability(ctx, eventID)
		return err
	})
	return a, err
}

// withSeatState runs op, rebuilding the event's seat state from Postgres and
// retrying once if Redis doesn't have it.
func (s *Service) withSeatState(ctx context.Context, eventID int64, op func() error) error {
	err := op()
	if !errors.Is(err, ErrInventoryMissing) {
		return err
	}
	load := func(ctx context.Context) (Inventory, error) { return s.store.Inventory(ctx, eventID) }
	if err := s.state.Rebuild(ctx, eventID, load); err != nil {
		return fmt.Errorf("rebuild seat state for event %d: %w", eventID, err)
	}
	return op()
}
