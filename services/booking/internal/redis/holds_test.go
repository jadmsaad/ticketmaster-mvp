package redis

import (
	"errors"
	"testing"
	"time"

	"ticketmaster-mvp/internal/testredis"
	"ticketmaster-mvp/services/booking/internal/booking"
)

func newHold(orderID string, seatID int64, ttl time.Duration) booking.Hold {
	return booking.Hold{
		OrderID:   orderID,
		SeatID:    seatID,
		UserID:    "alice",
		ExpiresAt: time.Now().Add(ttl).UTC().Truncate(time.Millisecond),
	}
}

func TestHoldsClaimGetRelease(t *testing.T) {
	holds := NewHoldStore(testredis.New(t))
	ctx := t.Context()
	first := newHold("order-1", 7, time.Minute)

	if err := holds.Claim(ctx, first); err != nil {
		t.Fatalf("claim free seat: %v", err)
	}
	got, err := holds.Get(ctx, first.OrderID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !got.ExpiresAt.Equal(first.ExpiresAt) || got.OrderID != first.OrderID || got.SeatID != first.SeatID || got.UserID != first.UserID {
		t.Errorf("get = %+v, want %+v", got, first)
	}

	// A second order can't claim a held seat.
	if err := holds.Claim(ctx, newHold("order-2", 7, time.Minute)); !errors.Is(err, booking.ErrSeatHeld) {
		t.Fatalf("claim held seat: err = %v, want ErrSeatHeld", err)
	}

	// After release, the order is gone and the seat is free again.
	if err := holds.Release(ctx, first); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, err := holds.Get(ctx, first.OrderID); !errors.Is(err, booking.ErrHoldNotFound) {
		t.Fatalf("get released: err = %v, want ErrHoldNotFound", err)
	}
	if err := holds.Claim(ctx, newHold("order-2", 7, time.Minute)); err != nil {
		t.Fatalf("claim released seat: %v", err)
	}
}

func TestHoldsExpire(t *testing.T) {
	holds := NewHoldStore(testredis.New(t))
	ctx := t.Context()
	short := newHold("order-1", 7, 50*time.Millisecond)

	if err := holds.Claim(ctx, short); err != nil {
		t.Fatalf("claim: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	if _, err := holds.Get(ctx, short.OrderID); !errors.Is(err, booking.ErrHoldNotFound) {
		t.Fatalf("get expired: err = %v, want ErrHoldNotFound", err)
	}
	if err := holds.Claim(ctx, newHold("order-2", 7, time.Minute)); err != nil {
		t.Fatalf("claim after expiry: %v", err)
	}
}

// A stale order (its hold expired and the seat was re-claimed) can neither be
// read as live nor release the new order's lock.
func TestHoldsStaleOrderIsFenced(t *testing.T) {
	rdb := testredis.New(t)
	holds := NewHoldStore(rdb)
	ctx := t.Context()
	stale := newHold("order-1", 7, time.Minute)
	if err := holds.Claim(ctx, stale); err != nil {
		t.Fatalf("claim: %v", err)
	}
	// Simulate the lock expiring before the order key, then another buyer claiming.
	if err := rdb.Del(ctx, seatKey(7)).Err(); err != nil {
		t.Fatal(err)
	}
	fresh := newHold("order-2", 7, time.Minute)
	if err := holds.Claim(ctx, fresh); err != nil {
		t.Fatalf("claim by second order: %v", err)
	}

	if _, err := holds.Get(ctx, stale.OrderID); !errors.Is(err, booking.ErrHoldNotFound) {
		t.Errorf("get stale order: err = %v, want ErrHoldNotFound", err)
	}
	if err := holds.Release(ctx, stale); err != nil {
		t.Fatalf("release stale: %v", err)
	}
	if _, err := holds.Get(ctx, fresh.OrderID); err != nil {
		t.Errorf("fresh order lost its hold after a stale release: %v", err)
	}
}

func TestHoldsClaimRejectsPastExpiry(t *testing.T) {
	holds := NewHoldStore(testredis.New(t))
	if err := holds.Claim(t.Context(), newHold("order-1", 7, -time.Second)); err == nil {
		t.Fatal("claim with past expiry succeeded")
	}
}
