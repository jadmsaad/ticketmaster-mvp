// Package booking is the booking domain: seats, holds, orders, and the rules
// that connect them (service.go). It imports no infrastructure; adapters
// (postgres, redis, httpapi) depend on it, never the reverse. See ADR 10 and 11.
package booking

import (
	"errors"
	"slices"
	"time"
)

var (
	// ErrNoInventory means the event has no seats in booking's inventory,
	// either because the event doesn't exist or because it isn't on sale.
	ErrNoInventory = errors.New("no seat inventory for event")
	// ErrSeatNotFound miseans the event has no seat at that index.
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

// Available reports whether a seat can be reserved: it exists, isn't sold,
// and isn't held.
func (a Availability) Available(seat int) bool {
	exists := seat >= 0 && seat < a.Seats
	return exists && !a.isSold(seat) && !a.isHeld(seat)
}

// isSold reports whether the seat's bit is 1 in the sold bitmap. The bitmap
// packs 8 seats per byte, numbered from the left, the same way Redis's SETBIT
// and GETBIT count. Example with seats 0 and 9 sold:
//
//	            byte 0                  byte 1
//	bit:    [ 1 0 0 0 0 0 0 0 ]   [ 0 1 0 0 0 0 0 0 ]
//	seat:     0 1 2 3 4 5 6 7       8 9 . . . . . .
//
// The comments below follow isSold(9).
func (a Availability) isSold(seat int) bool {
	// Which byte? Integer division drops the remainder: 9 / 8 = 1 → byte 1.
	byteIndex := seat / 8
	// Which bit inside that byte? The remainder: 9 % 8 = 1 → 2nd bit from the left.
	bitInByte := seat % 8
	// Redis drops trailing empty bytes (and the whole key if nothing is sold),
	// so a byte past the end means no seat in it was ever sold. Checking first
	// also avoids indexing past the end of the slice.
	if byteIndex >= len(a.Sold) {
		return false
	}
	// A mask with only this seat's bit on: 0x80 is 1000_0000, and shifting it
	// right by 1 gives 0100_0000.
	mask := byte(0x80) >> bitInByte
	// AND keeps a bit only where both sides have a 1, so this keeps just the
	// seat's bit: 0100_0000 & 0100_0000 = 0100_0000 ≠ 0 → sold. For seat 10 the
	// mask is 0010_0000, and 0100_0000 & 0010_0000 = 0 → not sold.
	return a.Sold[byteIndex]&mask != 0
}

// isHeld reports whether the seat has a live hold. Held is small (only active
// checkouts), so a linear scan is fine.
func (a Availability) isHeld(seat int) bool {
	return slices.Contains(a.Held, seat)
}
