// Package booking is the booking domain: seats, holds, orders, and the rules
// that connect them (service.go). It imports no infrastructure; adapters
// (postgres, redis, httpapi) depend on it, never the reverse. See ADR 10 and 11.
package booking

import (
	"errors"
	"time"
)

var (
	// ErrNoInventory means the event has no seats in booking's inventory,
	// either because the event doesn't exist or because it isn't on sale.
	ErrNoInventory = errors.New("no seat inventory for event")
	// ErrSeatNotFound means the event has no seat at that index.
	ErrSeatNotFound = errors.New("seat not found")
	// ErrSeatHeld means another order currently holds the seat.
	ErrSeatHeld = errors.New("seat is held by another order")
	// ErrSeatTaken means the seat is already sold.
	ErrSeatTaken = errors.New("seat already booked")
	// ErrHoldNotFound means the order has no live hold on the seat: it
	// expired, belongs to someone else, or never existed.
	ErrHoldNotFound = errors.New("hold not found or expired")
	// ErrBookingNotFound means no booking exists for the order.
	ErrBookingNotFound = errors.New("booking not found")
	// ErrOrderAlreadyConfirmed means a booking already exists for the order.
	ErrOrderAlreadyConfirmed = errors.New("order already confirmed")
	// ErrInventoryMissing means Redis has no seat state for the event, so it
	// must be rebuilt from Postgres. The service handles it; callers never see it.
	ErrInventoryMissing = errors.New("seat state missing for event")
	// ErrSeatStateUnavailable means the seat state store (Redis) can't be
	// reached. Booking fails closed: the API answers 503 rather than guess.
	ErrSeatStateUnavailable = errors.New("seat state unavailable")
	// ErrTooManyPrices means an event has more distinct prices than one byte
	// per seat can index (MaxPrices).
	ErrTooManyPrices = errors.New("too many distinct prices for one event")
)

// Order statuses. A held order lives only in Redis; a confirmed order is a
// booking in Postgres. An expired order simply no longer exists.
const (
	StatusHeld      = "held"
	StatusConfirmed = "confirmed"
)

// Seat is one seat in an event's layout: where it is, never its price or
// availability, which change and are served separately (ADR 12). Idx is its
// position within the event (0…N-1); the API addresses seats as (event_id, idx).
type Seat struct {
	Idx     int    `json:"idx"`
	Section string `json:"section"`
	Row     string `json:"row"`
	Number  int    `json:"number"`
}

// Hold is a live claim on one seat by one order. ExpiresAt is when the Redis
// lock expires, which is confirmGrace after the deadline the buyer sees.
type Hold struct {
	OrderID   string
	EventID   int64
	Seat      int
	UserID    string
	ExpiresAt time.Time
}

// Order is a purchase in progress (held) or complete (confirmed).
type Order struct {
	ID        string    `json:"id"`
	EventID   int64     `json:"event_id"`
	Seat      int       `json:"seat"`
	UserID    string    `json:"user_id"`
	Status    string    `json:"status"`
	ExpiresAt time.Time `json:"expires_at,omitzero"`
}

// NewBooking is what confirm inserts.
type NewBooking struct {
	OrderID string
	EventID int64
	Seat    int
	UserID  string
}

// Booking is a confirmed order, stored in Postgres.
type Booking struct {
	ID        int64
	OrderID   string
	EventID   int64
	Seat      int
	UserID    string
	CreatedAt time.Time
}

// deadline is when the buyer must confirm by: the lock's expiry minus
// confirmGrace (ADR 11).
func (h Hold) deadline() time.Time {
	return h.ExpiresAt.Add(-confirmGrace)
}

// beforeDeadline reports whether the buyer's deadline hasn't passed. Past it,
// the order can't be confirmed even though the lock still exists, so the lock
// never expires during a confirm.
func (h Hold) beforeDeadline() bool {
	return time.Now().Before(h.deadline())
}

// heldOrder is the order a live hold represents, showing the buyer's deadline.
func (h Hold) heldOrder() Order {
	return Order{ID: h.OrderID, EventID: h.EventID, Seat: h.Seat, UserID: h.UserID, Status: StatusHeld, ExpiresAt: h.deadline()}
}

// confirmedOrder is the order once the hold's booking has committed.
func (h Hold) confirmedOrder() Order {
	return Order{ID: h.OrderID, EventID: h.EventID, Seat: h.Seat, UserID: h.UserID, Status: StatusConfirmed}
}

// order is the confirmed order a booking represents.
func (b Booking) order() Order {
	return Order{ID: b.OrderID, EventID: b.EventID, Seat: b.Seat, UserID: b.UserID, Status: StatusConfirmed}
}

// Inventory is what Redis seat state is rebuilt from: the seat count, which
// seats are sold, and each seat's price in cents (Prices[idx]).
type Inventory struct {
	Seats  int
	Sold   []int
	Prices []int
}
