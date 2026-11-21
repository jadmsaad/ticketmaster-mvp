// Package redis is booking's Redis adapter: seat state for on-sale traffic.
package redis

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"ticketmaster-mvp/services/booking/internal/booking"
)

const (
	// inventoryTTL bounds how long seat state lives without a rebuild. It's a
	// cache of Postgres, so expiring it only costs one rebuild.
	inventoryTTL = 24 * time.Hour
	// rebuildLockTTL caps how long one rebuild can block others.
	rebuildLockTTL = 10 * time.Second
	// rebuildWait is how long a caller waits for someone else's rebuild.
	rebuildWait = 2 * time.Second
)

// SeatState keeps each event's seat state in Redis (ADR 11). Every key for an
// event shares the {event:N} hash tag, so they live on one Redis Cluster shard
// and one Lua script can use them together:
//
//	booking:seats:count:{event:N}       seat count; its absence means "rebuild"
//	booking:seats:sold:{event:N}        bitmap, 1 bit per seat idx: 1 = sold
//	booking:holds:{event:N}             sorted set: seat idx → hold expiry (ms)
//	booking:hold:{event:N}:seat:{idx}   the lock: "order_id|user_id", with a TTL
//
// Scripts read the time from Redis (TIME), so locks and the holds set agree
// on "now". The sold bitmap is a cache of Postgres; the UNIQUE constraint on
// bookings.seat_id is the final say.
type SeatState struct {
	rdb *goredis.Client
}

func NewSeatState(rdb *goredis.Client) *SeatState {
	return &SeatState{rdb: rdb}
}

func tag(eventID int64) string { return "{event:" + strconv.FormatInt(eventID, 10) + "}" }

func countKey(eventID int64) string   { return "booking:seats:count:" + tag(eventID) }
func soldKey(eventID int64) string    { return "booking:seats:sold:" + tag(eventID) }
func loadKey(eventID int64) string    { return "booking:seats:load:" + tag(eventID) }
func holdsKey(eventID int64) string   { return "booking:holds:" + tag(eventID) }
func rebuildKey(eventID int64) string { return "booking:rebuild:" + tag(eventID) }
func lockKey(eventID int64, seat int) string {
	return "booking:hold:" + tag(eventID) + ":seat:" + strconv.Itoa(seat)
}

// lockValue is what a lock stores: the order and its buyer. Order IDs are
// UUIDs, so the first "|" always separates them.
func lockValue(h booking.Hold) string { return h.OrderID + "|" + h.UserID }

// Lua scripts receive their keys and arguments as two arrays, KEYS and ARGV,
// in the order the Go caller passes them. Each script names them on its first
// lines, so the rest reads in plain words.

// redisNow is the shared Lua prelude: the current time in ms, from Redis's
// clock, so every script agrees on "now" whatever the app servers' clocks say.
const redisNow = `
local redisTime = redis.call("TIME") -- {seconds, microseconds}
local nowMs = tonumber(redisTime[1]) * 1000 + math.floor(tonumber(redisTime[2]) / 1000)
`

// reserveScript claims a seat in one atomic step. Returns the hold's expiry
// in ms, or a status string the Go side maps to a domain error.
var reserveScript = goredis.NewScript(redisNow + `
local seatCountKey = KEYS[1]
local soldBitmapKey = KEYS[2]
local liveHoldsKey = KEYS[3]
local seatLockKey = KEYS[4]
local seat = tonumber(ARGV[1])
local lockOwner = ARGV[2] -- "order_id|user_id"
local holdMs = tonumber(ARGV[3])

-- No seat count means Redis has no state for this event: the service rebuilds it.
local seatCount = redis.call("GET", seatCountKey)
if not seatCount then return "MISSING" end

-- Reject seats outside 0…count-1 (also stops SETBIT allocating a huge bitmap).
if seat < 0 or seat >= tonumber(seatCount) then return "NOSEAT" end

-- Drop holds that have already expired, so the set only contains live ones.
redis.call("ZREMRANGEBYSCORE", liveHoldsKey, "-inf", nowMs)

if redis.call("GETBIT", soldBitmapKey, seat) == 1 then return "SOLD" end

-- Take the lock only if nobody holds the seat (NX); it expires by itself (PXAT).
local expiresAtMs = nowMs + holdMs
local locked = redis.call("SET", seatLockKey, lockOwner, "NX", "PXAT", expiresAtMs)
if not locked then return "HELD" end

-- Record the live hold, scored by when it expires.
redis.call("ZADD", liveHoldsKey, expiresAtMs, seat)

-- The set expires with its latest hold. GT only ever extends the expiry, but it
-- does nothing while the key has no expiry yet, so set it in that case.
redis.call("PEXPIREAT", liveHoldsKey, expiresAtMs, "GT")
if redis.call("PTTL", liveHoldsKey) < 0 then
	redis.call("PEXPIREAT", liveHoldsKey, expiresAtMs)
end

return expiresAtMs
`)

// Reserve claims h's seat for h's order until the returned time.
func (s *SeatState) Reserve(ctx context.Context, h booking.Hold, ttl time.Duration) (time.Time, error) {
	res, err := reserveScript.Run(ctx, s.rdb,
		[]string{countKey(h.EventID), soldKey(h.EventID), holdsKey(h.EventID), lockKey(h.EventID, h.Seat)},
		h.Seat, lockValue(h), ttl.Milliseconds(),
	).Result()
	if err != nil {
		return time.Time{}, fmt.Errorf("reserve event %d seat %d: %w", h.EventID, h.Seat, err)
	}
	switch v := res.(type) {
	case int64:
		return time.UnixMilli(v).UTC(), nil
	case string:
		switch v {
		case "MISSING":
			return time.Time{}, booking.ErrInventoryMissing
		case "NOSEAT":
			return time.Time{}, booking.ErrSeatNotFound
		case "SOLD":
			return time.Time{}, booking.ErrSeatTaken
		case "HELD":
			return time.Time{}, booking.ErrSeatHeld
		}
	}
	return time.Time{}, fmt.Errorf("reserve event %d seat %d: unexpected reply %v", h.EventID, h.Seat, res)
}

// HeldBy returns the live hold on a seat, or booking.ErrHoldNotFound.
func (s *SeatState) HeldBy(ctx context.Context, eventID int64, seat int) (booking.Hold, error) {
	key := lockKey(eventID, seat)
	pipe := s.rdb.Pipeline()
	val := pipe.Get(ctx, key)
	ttl := pipe.PTTL(ctx, key)
	_, err := pipe.Exec(ctx)
	if errors.Is(err, goredis.Nil) {
		return booking.Hold{}, booking.ErrHoldNotFound
	}
	if err != nil {
		return booking.Hold{}, fmt.Errorf("get hold on event %d seat %d: %w", eventID, seat, err)
	}
	orderID, userID, ok := strings.Cut(val.Val(), "|")
	if !ok {
		return booking.Hold{}, fmt.Errorf("get hold on event %d seat %d: malformed lock %q", eventID, seat, val.Val())
	}
	return booking.Hold{
		OrderID:   orderID,
		EventID:   eventID,
		Seat:      seat,
		UserID:    userID,
		ExpiresAt: time.Now().Add(ttl.Val()).UTC(),
	}, nil
}

// markSoldScript records a seat as sold and releases its hold.
var markSoldScript = goredis.NewScript(`
local soldBitmapKey = KEYS[1]
local liveHoldsKey = KEYS[2]
local seatLockKey = KEYS[3]
local seat = ARGV[1]
local lockOwner = ARGV[2] -- "order_id|user_id"

redis.call("SETBIT", soldBitmapKey, seat, 1)
redis.call("ZREM", liveHoldsKey, seat)

-- Delete the lock only if it's still this order's: a stale order must never
-- release a hold someone else has taken since.
if redis.call("GET", seatLockKey) == lockOwner then
	redis.call("DEL", seatLockKey)
end
return 1
`)

// MarkSold records h's seat as sold and releases h's hold. Another order's
// hold on the seat is left alone.
func (s *SeatState) MarkSold(ctx context.Context, h booking.Hold) error {
	err := markSoldScript.Run(ctx, s.rdb,
		[]string{soldKey(h.EventID), holdsKey(h.EventID), lockKey(h.EventID, h.Seat)},
		h.Seat, lockValue(h),
	).Err()
	if err != nil {
		return fmt.Errorf("mark event %d seat %d sold: %w", h.EventID, h.Seat, err)
	}
	return nil
}

// availabilityScript reads an event's seat state in one consistent step.
// Returns {seat count, sold bitmap, live held seats}, or nil if the state is
// missing.
var availabilityScript = goredis.NewScript(redisNow + `
local seatCountKey = KEYS[1]
local soldBitmapKey = KEYS[2]
local liveHoldsKey = KEYS[3]

local seatCount = redis.call("GET", seatCountKey)
if not seatCount then return false end

-- A missing bitmap means nothing is sold yet.
local soldBitmap = redis.call("GET", soldBitmapKey) or ""

-- Only holds that expire after now ("(" means strictly after): expired ones
-- never count, whether or not they've been pruned yet.
local heldSeats = redis.call("ZRANGEBYSCORE", liveHoldsKey, "(" .. nowMs, "+inf")

return {seatCount, soldBitmap, heldSeats}
`)

// Availability returns which seats are sold and which are held right now.
func (s *SeatState) Availability(ctx context.Context, eventID int64) (booking.Availability, error) {
	res, err := availabilityScript.Run(ctx, s.rdb,
		[]string{countKey(eventID), soldKey(eventID), holdsKey(eventID)},
	).Slice()
	if errors.Is(err, goredis.Nil) {
		return booking.Availability{}, booking.ErrInventoryMissing
	}
	if err != nil {
		return booking.Availability{}, fmt.Errorf("availability for event %d: %w", eventID, err)
	}
	n, err := strconv.Atoi(fmt.Sprint(res[0]))
	if err != nil {
		return booking.Availability{}, fmt.Errorf("availability for event %d: bad seat count %v", eventID, res[0])
	}
	a := booking.Availability{Seats: n, Sold: []byte(fmt.Sprint(res[1]))}
	held, _ := res[2].([]any)
	for _, m := range held {
		seat, err := strconv.Atoi(fmt.Sprint(m))
		if err != nil {
			return booking.Availability{}, fmt.Errorf("availability for event %d: bad held seat %v", eventID, m)
		}
		a.Held = append(a.Held, seat)
	}
	return a, nil
}

// loadScript writes rebuilt seat state from Postgres.
var loadScript = goredis.NewScript(`
local soldBitmapKey = KEYS[1]
local seatCountKey = KEYS[2]
local scratchKey = KEYS[3]
local rebuiltBitmap = ARGV[1]
local seatCount = ARGV[2]
local stateTtlMs = ARGV[3]

-- Merge the rebuilt bits into the existing bitmap with OR, so a seat sold while
-- the rebuild was reading Postgres is never cleared. BITOP needs both inputs
-- as keys, so the rebuilt bitmap goes into a short-lived scratch key first.
redis.call("SET", scratchKey, rebuiltBitmap, "PX", 60000)
redis.call("BITOP", "OR", soldBitmapKey, soldBitmapKey, scratchKey)
redis.call("DEL", scratchKey)
redis.call("PEXPIRE", soldBitmapKey, stateTtlMs)

-- Writing the seat count last marks the state as present.
redis.call("SET", seatCountKey, seatCount, "PX", stateTtlMs)
return 1
`)

// Rebuild loads an event's seat state from load. One caller per event does
// the rebuild; others wait up to rebuildWait for it to finish.
func (s *SeatState) Rebuild(ctx context.Context, eventID int64, load func(context.Context) (booking.Inventory, error)) error {
	won, err := s.rdb.SetNX(ctx, rebuildKey(eventID), 1, rebuildLockTTL).Result()
	if err != nil {
		return fmt.Errorf("take rebuild lock: %w", err)
	}
	if !won {
		return s.waitForRebuild(ctx, eventID)
	}
	defer s.rdb.Del(context.WithoutCancel(ctx), rebuildKey(eventID))

	inv, err := load(ctx)
	if err != nil {
		return err
	}
	err = loadScript.Run(ctx, s.rdb,
		[]string{soldKey(eventID), countKey(eventID), loadKey(eventID)},
		soldBitmap(inv), inv.Seats, inventoryTTL.Milliseconds(),
	).Err()
	if err != nil {
		return fmt.Errorf("load seat state: %w", err)
	}
	return nil
}

func (s *SeatState) waitForRebuild(ctx context.Context, eventID int64) error {
	deadline := time.Now().Add(rebuildWait)
	for time.Now().Before(deadline) {
		n, err := s.rdb.Exists(ctx, countKey(eventID)).Result()
		if err != nil {
			return fmt.Errorf("wait for rebuild: %w", err)
		}
		if n == 1 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
	return fmt.Errorf("seat state for event %d is still being rebuilt", eventID)
}

// soldBitmap encodes the sold seats in Redis bit order: seat 0 is the most
// significant bit of byte 0, matching SETBIT and GETBIT.
func soldBitmap(inv booking.Inventory) string {
	b := make([]byte, (inv.Seats+7)/8)
	for _, seat := range inv.Sold {
		if seat >= 0 && seat < inv.Seats {
			b[seat/8] |= 0x80 >> (seat % 8)
		}
	}
	return string(b)
}
