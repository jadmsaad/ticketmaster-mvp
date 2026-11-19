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
)

// Order statuses. A held order lives only in Redis; a confirmed order is a
// booking in Postgres. An expired order simply no longer exists.
const (
	StatusHeld      = "held"
	StatusConfirmed = "confirmed"
)

// Seat is one seat in an event's inventory. Idx is its position within the
// event (0…N-1); the API addresses seats as (event_id, idx).
type Seat struct {
	ID         int64  `json:"id"`
	Idx        int    `json:"idx"`
	Section    string `json:"section"`
	Row        string `json:"row"`
	Number     int    `json:"number"`
	PriceCents int    `json:"price_cents"`
	Available  bool   `json:"available"`
}

// Hold is a live claim on one seat by one order.
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

// Inventory is what Redis seat state is rebuilt from: the seat count and
// which seats are sold.
type Inventory struct {
	Seats int
	Sold  []int
}

// Availability is an event's seat state at one moment: Sold is a bitmap in
// Redis bit order (seat 0 is the most significant bit of byte 0), and Held
// lists seats with a live hold.
type Availability struct {
	Seats int
	Sold  []byte
	Held  []int
}

// Available reports whether a seat can be reserved: not sold and not held.
func (a Availability) Available(seat int) bool {
	if seat < 0 || seat >= a.Seats {
		return false
	}
	if byteIdx := seat / 8; byteIdx < len(a.Sold) && a.Sold[byteIdx]&(0x80>>(seat%8)) != 0 {
		return false
	}
	for _, h := range a.Held {
		if h == seat {
			return false
		}
	}
	return true
}
