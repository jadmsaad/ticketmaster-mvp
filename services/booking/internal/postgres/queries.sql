-- A seat is available if it has no booking. Since migration 00002 a seat has
-- at most one booking (UNIQUE seat_id).
-- name: ListSeats :many
SELECT s.id, s.idx, s.section, s.row, s.number, s.price_cents,
       NOT EXISTS (SELECT 1 FROM bookings b WHERE b.seat_id = s.id) AS available
FROM seats s
WHERE s.event_id = $1
ORDER BY s.idx;

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

-- Inventory for rebuilding Redis seat state: how many seats, and which are sold.
-- name: CountSeats :one
SELECT count(*) FROM seats WHERE event_id = $1;

-- name: ListSoldSeatIdxs :many
SELECT s.idx
FROM bookings b
JOIN seats s ON s.id = b.seat_id
WHERE s.event_id = $1;
