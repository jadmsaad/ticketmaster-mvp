// Package booking is the booking domain: seats, holds, and their errors. It
// imports no infrastructure; adapters (postgres, redis, httpapi) depend on it,
// never the reverse.
package booking

import (
	"errors"
	"time"
)

var (
	// ErrNoInventory means the event has no seats in booking's inventory,
	// either because the event doesn't exist or because it isn't on sale.
	ErrNoInventory = errors.New("no seat inventory for event")
	// ErrSeatHeld means another order currently holds the seat.
	ErrSeatHeld = errors.New("seat is held by another order")
	// ErrHoldNotFound means the order has no live hold: it expired, was
	// released, or never existed.
	ErrHoldNotFound = errors.New("hold not found or expired")
)

// Seat is the API's type, kept separate from the sqlc-generated row type.
type Seat struct {
	ID         int64  `json:"id"`
	Section    string `json:"section"`
	Row        string `json:"row"`
	Number     int    `json:"number"`
	PriceCents int    `json:"price_cents"`
	Available  bool   `json:"available"`
}

// Hold is a held order: one seat reserved for one buyer until ExpiresAt.
type Hold struct {
	OrderID   string    `json:"order_id"`
	SeatID    int64     `json:"seat_id"`
	UserID    string    `json:"user_id"`
	ExpiresAt time.Time `json:"expires_at"`
}
