-- The static seat layout: where each seat is. Prices and availability change,
-- so they're served from Redis instead (ADR 12).
-- name: ListLayout :many
SELECT idx, section, row, number
FROM seats
WHERE event_id = $1
ORDER BY idx;

-- Confirm: resolve (event_id, idx) to the seat and insert its booking in one
-- statement. UNIQUE (seat_id) rejects a second booking; no row means no such seat.
-- name: CreateBooking :one
INSERT INTO bookings (seat_id, order_id, user_id)
SELECT s.id, sqlc.arg(order_id)::uuid, sqlc.arg(user_id)
FROM seats s
WHERE s.event_id = sqlc.arg(event_id) AND s.idx = sqlc.arg(idx)
RETURNING id, created_at;

-- name: GetBookingByOrder :one
SELECT b.id, b.order_id::text AS order_id, b.user_id, b.created_at, s.event_id, s.idx
FROM bookings b
JOIN seats s ON s.id = b.seat_id
WHERE b.order_id = sqlc.arg(order_id)::uuid;

-- Inventory for rebuilding Redis seat state: every seat's price (whose count is
-- the seat count), and which seats are sold.
-- name: ListSeatPrices :many
SELECT idx, price_cents
FROM seats
WHERE event_id = $1
ORDER BY idx;

-- name: ListSoldSeatIdxs :many
SELECT s.idx
FROM bookings b
JOIN seats s ON s.id = b.seat_id
WHERE s.event_id = $1;
