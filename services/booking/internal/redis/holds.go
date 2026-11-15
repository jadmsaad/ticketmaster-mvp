// Package redis is booking's Redis adapter: seat holds.
package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"ticketmaster-mvp/services/booking/internal/booking"
)

// HoldStore stores holds in Redis, using two keys that share one TTL:
//
//	booking:hold:seat:{seat_id}  = order_id   the lock: at most one order per seat
//	booking:order:{order_id}     = Hold JSON  the order's details
//
// Both keys are written and removed in one Lua script, so a crash can't leave
// a lock without its order, or the reverse. Holds keep load off Postgres
// during an on-sale; they don't guarantee correctness on their own, because a
// key can expire or be lost in a failover. The unique constraint on
// bookings.seat_id is the final say.
type HoldStore struct {
	rdb *goredis.Client
}

func NewHoldStore(rdb *goredis.Client) *HoldStore {
	return &HoldStore{rdb: rdb}
}

func seatKey(seatID int64) string    { return "booking:hold:seat:" + strconv.FormatInt(seatID, 10) }
func orderKey(orderID string) string { return "booking:order:" + orderID }

// claimScript sets the seat lock only if the seat is free, then stores the order.
// KEYS: seat key, order key. ARGV: order ID, order JSON, TTL in milliseconds.
var claimScript = goredis.NewScript(`
if redis.call("SET", KEYS[1], ARGV[1], "NX", "PX", ARGV[3]) then
	redis.call("SET", KEYS[2], ARGV[2], "PX", ARGV[3])
	return 1
end
return 0
`)

// Claim holds h.SeatID for order h.OrderID until h.ExpiresAt. It returns
// booking.ErrSeatHeld if another order already holds the seat.
func (r *HoldStore) Claim(ctx context.Context, h booking.Hold) error {
	ttl := time.Until(h.ExpiresAt)
	if ttl <= 0 {
		return fmt.Errorf("claim hold for seat %d: expiry %s is in the past", h.SeatID, h.ExpiresAt)
	}
	body, err := json.Marshal(h)
	if err != nil {
		return fmt.Errorf("encode hold: %w", err)
	}
	ok, err := claimScript.Run(ctx, r.rdb,
		[]string{seatKey(h.SeatID), orderKey(h.OrderID)},
		h.OrderID, body, ttl.Milliseconds(),
	).Int()
	if err != nil {
		return fmt.Errorf("claim hold for seat %d: %w", h.SeatID, err)
	}
	if ok == 0 {
		return booking.ErrSeatHeld
	}
	return nil
}

// Get returns the live hold for orderID. The order must still own its seat's
// lock, so a stale order can never be confirmed. That makes the order ID a
// fencing token. It returns booking.ErrHoldNotFound otherwise.
func (r *HoldStore) Get(ctx context.Context, orderID string) (booking.Hold, error) {
	body, err := r.rdb.Get(ctx, orderKey(orderID)).Bytes()
	if errors.Is(err, goredis.Nil) {
		return booking.Hold{}, booking.ErrHoldNotFound
	}
	if err != nil {
		return booking.Hold{}, fmt.Errorf("get hold %s: %w", orderID, err)
	}
	var h booking.Hold
	if err := json.Unmarshal(body, &h); err != nil {
		return booking.Hold{}, fmt.Errorf("decode hold %s: %w", orderID, err)
	}
	owner, err := r.rdb.Get(ctx, seatKey(h.SeatID)).Result()
	if errors.Is(err, goredis.Nil) || (err == nil && owner != orderID) {
		return booking.Hold{}, booking.ErrHoldNotFound
	}
	if err != nil {
		return booking.Hold{}, fmt.Errorf("get seat lock for hold %s: %w", orderID, err)
	}
	return h, nil
}

// releaseScript deletes the order, and the seat lock only if this order owns it.
// KEYS: seat key, order key. ARGV: order ID.
var releaseScript = goredis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
	redis.call("DEL", KEYS[1])
end
redis.call("DEL", KEYS[2])
return 1
`)

// Release ends hold h. A stale hold can't release a seat that another order
// has since claimed.
func (r *HoldStore) Release(ctx context.Context, h booking.Hold) error {
	err := releaseScript.Run(ctx, r.rdb, []string{seatKey(h.SeatID), orderKey(h.OrderID)}, h.OrderID).Err()
	if err != nil {
		return fmt.Errorf("release hold %s: %w", h.OrderID, err)
	}
	return nil
}
