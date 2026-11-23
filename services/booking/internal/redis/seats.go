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

// SeatState keeps each event's seat state in Redis (ADR 11, ADR 12). Every key
// for an event shares the {event:N} hash tag, so they live on one Redis
// Cluster shard and one Lua script can use them together:
//
//	booking:seats:count:{event:N}       seat count; its absence means "rebuild"
//	booking:seats:sold:{event:N}        bitmap, 1 bit per seat idx: 1 = sold
//	booking:prices:{event:N}            hash: "list" = distinct prices ("8500,12000"),
//	                                    "seats" = 1 byte per seat, an index into list
//	booking:holds:{event:N}             sorted set: seat idx → hold expiry (ms)
//	booking:hold:{event:N}:seat:{idx}   the lock: "order_id|user_id", with a TTL
//
// Scripts read the time from Redis (TIME), so locks and the holds set agree
// on "now". The sold bitmap and prices are a cache of Postgres; the UNIQUE
// constraint on bookings.seat_id is the final say.
type SeatState struct {
	rdb *goredis.Client
}

func NewSeatState(rdb *goredis.Client) *SeatState {
	return &SeatState{rdb: rdb}
}

func tag(eventID int64) string { return "{event:" + strconv.FormatInt(eventID, 10) + "}" }

func countKey(eventID int64) string   { return "booking:seats:count:" + tag(eventID) }
func soldKey(eventID int64) string    { return "booking:seats:sold:" + tag(eventID) }
func pricesKey(eventID int64) string  { return "booking:prices:" + tag(eventID) }
func loadKey(eventID int64) string    { return "booking:seats:load:" + tag(eventID) }
func holdsKey(eventID int64) string   { return "booking:holds:" + tag(eventID) }
func rebuildKey(eventID int64) string { return "booking:rebuild:" + tag(eventID) }
func lockKey(eventID int64, seat int) string {
	return "booking:hold:" + tag(eventID) + ":seat:" + strconv.Itoa(seat)
}

// lockValue is what a lock stores: the order and its buyer. Order IDs are
// UUIDs, so the first "|" always separates them.
func lockValue(h booking.Hold) string { return h.OrderID + "|" + h.UserID }

// parseLock splits a lock value back into a hold on the given seat.
func parseLock(value string, eventID int64, seat int, expiresAtMs int64) (booking.Hold, error) {
	orderID, userID, ok := strings.Cut(value, "|")
	if !ok {
		return booking.Hold{}, fmt.Errorf("malformed lock %q on event %d seat %d", value, eventID, seat)
	}
	return booking.Hold{
		OrderID:   orderID,
		EventID:   eventID,
		Seat:      seat,
		UserID:    userID,
		ExpiresAt: time.UnixMilli(expiresAtMs).UTC(),
	}, nil
}

// unavailable marks an error from Redis itself (unreachable, timed out), so
// the API fails closed with 503 (ADR 11). Domain outcomes such as a held seat
// are never wrapped this way.
func unavailable(op string, err error) error {
	return fmt.Errorf("%s: %w: %w", op, booking.ErrSeatStateUnavailable, err)
}

// Lua scripts receive their keys and arguments as two arrays, KEYS and ARGV,
// in the order the Go caller passes them. Each script names them on its first
// lines, so the rest reads in plain words.

// redisNow is the shared Lua prelude: the current time in ms, from Redis's
// clock, so every script agrees on "now" whatever the app servers' clocks say.
const redisNow = `
local redisTime = redis.call("TIME") -- {seconds, microseconds}
local nowMs = tonumber(redisTime[1]) * 1000 + math.floor(tonumber(redisTime[2]) / 1000)
`

// reserveScript claims a seat in one atomic step. Returns {lock owner, lock
// expiry in ms} for the hold now on the seat, or a status string the Go side
// maps to a domain error.
var reserveScript = goredis.NewScript(redisNow + `
local seatCountKey = KEYS[1]
local soldBitmapKey = KEYS[2]
local liveHoldsKey = KEYS[3]
local seatLockKey = KEYS[4]
local seat = tonumber(ARGV[1])
local lockOwner = ARGV[2] -- "order_id|user_id"
local holdMs = tonumber(ARGV[3])
local buyer = ARGV[4] -- user_id

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
if not locked then
	-- The same buyer again (a retry, a double click, a second tab) gets their
	-- existing hold back, with its original expiry: a retry never extends it.
	local currentOwner = redis.call("GET", seatLockKey)
	local separator = string.find(currentOwner, "|", 1, true)
	if separator and string.sub(currentOwner, separator + 1) == buyer then
		return {currentOwner, redis.call("PEXPIRETIME", seatLockKey)}
	end
	return "HELD"
end

-- Record the live hold, scored by when it expires.
redis.call("ZADD", liveHoldsKey, expiresAtMs, seat)

-- The set expires with its latest hold. GT only ever extends the expiry, but it
-- does nothing while the key has no expiry yet, so set it in that case.
redis.call("PEXPIREAT", liveHoldsKey, expiresAtMs, "GT")
if redis.call("PTTL", liveHoldsKey) < 0 then
	redis.call("PEXPIREAT", liveHoldsKey, expiresAtMs)
end

return {lockOwner, expiresAtMs}
`)

// Reserve claims h's seat for h's order and returns the hold now on the seat:
// h itself, or the same user's existing hold.
func (s *SeatState) Reserve(ctx context.Context, h booking.Hold, ttl time.Duration) (booking.Hold, error) {
	res, err := reserveScript.Run(ctx, s.rdb,
		[]string{countKey(h.EventID), soldKey(h.EventID), holdsKey(h.EventID), lockKey(h.EventID, h.Seat)},
		h.Seat, lockValue(h), ttl.Milliseconds(), h.UserID,
	).Result()
	if err != nil {
		return booking.Hold{}, unavailable(fmt.Sprintf("reserve event %d seat %d", h.EventID, h.Seat), err)
	}
	switch v := res.(type) {
	case []any:
		// {lock owner, expiry in ms}: the hold on the seat.
		owner, okOwner := v[0].(string)
		expiresAtMs, okExpiry := v[1].(int64)
		if okOwner && okExpiry {
			return parseLock(owner, h.EventID, h.Seat, expiresAtMs)
		}
	case string:
		switch v {
		case "MISSING":
			return booking.Hold{}, booking.ErrInventoryMissing
		case "NOSEAT":
			return booking.Hold{}, booking.ErrSeatNotFound
		case "SOLD":
			return booking.Hold{}, booking.ErrSeatTaken
		case "HELD":
			return booking.Hold{}, booking.ErrSeatHeld
		}
	}
	return booking.Hold{}, fmt.Errorf("reserve event %d seat %d: unexpected reply %v", h.EventID, h.Seat, res)
}

// HeldBy returns the live hold on a seat, or booking.ErrHoldNotFound. Its
// expiry comes from Redis's clock (PEXPIRETIME), like the expiry Reserve
// returns.
func (s *SeatState) HeldBy(ctx context.Context, eventID int64, seat int) (booking.Hold, error) {
	key := lockKey(eventID, seat)
	pipe := s.rdb.Pipeline()
	owner := pipe.Get(ctx, key)
	expiresAtMs := pipe.Do(ctx, "PEXPIRETIME", key)
	_, err := pipe.Exec(ctx)
	if errors.Is(err, goredis.Nil) {
		return booking.Hold{}, booking.ErrHoldNotFound
	}
	if err != nil {
		return booking.Hold{}, unavailable(fmt.Sprintf("get hold on event %d seat %d", eventID, seat), err)
	}
	ms, err := expiresAtMs.Int64()
	// Negative means the lock expired between the two commands.
	if err != nil || ms < 0 {
		return booking.Hold{}, booking.ErrHoldNotFound
	}
	return parseLock(owner.Val(), eventID, seat, ms)
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
		return unavailable(fmt.Sprintf("mark event %d seat %d sold", h.EventID, h.Seat), err)
	}
	return nil
}

// availabilityScript reads an event's seat state and prices in one consistent
// step. Returns {seat count, sold bitmap, live held seats, price list, seat
// price bytes}, or nil if the state is missing.
var availabilityScript = goredis.NewScript(redisNow + `
local seatCountKey = KEYS[1]
local soldBitmapKey = KEYS[2]
local liveHoldsKey = KEYS[3]
local seatPricesKey = KEYS[4]

local seatCount = redis.call("GET", seatCountKey)
if not seatCount then return false end

-- Prices are written with the seat count, so they're only missing if they
-- were lost on their own: rebuild both.
local prices = redis.call("HMGET", seatPricesKey, "list", "seats")
if not prices[1] or not prices[2] then return false end

-- A missing bitmap means nothing is sold yet.
local soldBitmap = redis.call("GET", soldBitmapKey) or ""

-- Only holds that expire after now ("(" means strictly after): expired ones
-- never count, whether or not they've been pruned yet.
local heldSeats = redis.call("ZRANGEBYSCORE", liveHoldsKey, "(" .. nowMs, "+inf")

return {seatCount, soldBitmap, heldSeats, prices[1], prices[2]}
`)

// Availability returns which seats are sold and held right now, and the seat
// prices.
func (s *SeatState) Availability(ctx context.Context, eventID int64) (booking.Availability, error) {
	res, err := availabilityScript.Run(ctx, s.rdb,
		[]string{countKey(eventID), soldKey(eventID), holdsKey(eventID), pricesKey(eventID)},
	).Slice()
	if errors.Is(err, goredis.Nil) {
		return booking.Availability{}, booking.ErrInventoryMissing
	}
	if err != nil {
		return booking.Availability{}, unavailable(fmt.Sprintf("availability for event %d", eventID), err)
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
	if a.Prices, err = parsePriceList(fmt.Sprint(res[3])); err != nil {
		return booking.Availability{}, fmt.Errorf("availability for event %d: %w", eventID, err)
	}
	a.SeatPrice = []byte(fmt.Sprint(res[4]))
	return a, nil
}

// loadScript writes rebuilt seat state from Postgres.
var loadScript = goredis.NewScript(`
local soldBitmapKey = KEYS[1]
local seatCountKey = KEYS[2]
local scratchKey = KEYS[3]
local seatPricesKey = KEYS[4]
local rebuiltBitmap = ARGV[1]
local seatCount = ARGV[2]
local stateTtlMs = ARGV[3]
local priceList = ARGV[4]
local seatPriceBytes = ARGV[5]

-- Merge the rebuilt bits into the existing bitmap with OR, so a seat sold while
-- the rebuild was reading Postgres is never cleared. BITOP needs both inputs
-- as keys, so the rebuilt bitmap goes into a short-lived scratch key first.
redis.call("SET", scratchKey, rebuiltBitmap, "PX", 60000)
redis.call("BITOP", "OR", soldBitmapKey, soldBitmapKey, scratchKey)
redis.call("DEL", scratchKey)
redis.call("PEXPIRE", soldBitmapKey, stateTtlMs)

-- Prices come from Postgres, which is the truth for them: replace, don't merge.
redis.call("HSET", seatPricesKey, "list", priceList, "seats", seatPriceBytes)
redis.call("PEXPIRE", seatPricesKey, stateTtlMs)

-- Writing the seat count last marks the state as present.
redis.call("SET", seatCountKey, seatCount, "PX", stateTtlMs)
return 1
`)

// Rebuild loads an event's seat state from load. One caller per event does
// the rebuild; others wait up to rebuildWait for it to finish.
func (s *SeatState) Rebuild(ctx context.Context, eventID int64, load func(context.Context) (booking.Inventory, error)) error {
	won, err := s.rdb.SetNX(ctx, rebuildKey(eventID), 1, rebuildLockTTL).Result()
	if err != nil {
		return unavailable("take rebuild lock", err)
	}
	if !won {
		return s.waitForRebuild(ctx, eventID)
	}
	defer s.rdb.Del(context.WithoutCancel(ctx), rebuildKey(eventID))

	inv, err := load(ctx)
	if err != nil {
		return err
	}
	prices, seatPrice, err := booking.EncodePrices(inv.Prices)
	if err != nil {
		return fmt.Errorf("encode prices for event %d: %w", eventID, err)
	}
	err = loadScript.Run(ctx, s.rdb,
		[]string{soldKey(eventID), countKey(eventID), loadKey(eventID), pricesKey(eventID)},
		soldBitmap(inv), inv.Seats, inventoryTTL.Milliseconds(), formatPriceList(prices), string(seatPrice),
	).Err()
	if err != nil {
		return unavailable("load seat state", err)
	}
	return nil
}

// waitForRebuild waits until another caller's rebuild has written the seat
// count and prices.
func (s *SeatState) waitForRebuild(ctx context.Context, eventID int64) error {
	deadline := time.Now().Add(rebuildWait)
	for time.Now().Before(deadline) {
		n, err := s.rdb.Exists(ctx, countKey(eventID), pricesKey(eventID)).Result()
		if err != nil {
			return unavailable("wait for rebuild", err)
		}
		if n == 2 {
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

// formatPriceList stores the distinct prices as "8500,12000".
func formatPriceList(prices []int) string {
	parts := make([]string, len(prices))
	for i, p := range prices {
		parts[i] = strconv.Itoa(p)
	}
	return strings.Join(parts, ",")
}

func parsePriceList(s string) ([]int, error) {
	if s == "" {
		return []int{}, nil
	}
	parts := strings.Split(s, ",")
	prices := make([]int, len(parts))
	for i, part := range parts {
		p, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("bad price list %q", s)
		}
		prices[i] = p
	}
	return prices, nil
}
