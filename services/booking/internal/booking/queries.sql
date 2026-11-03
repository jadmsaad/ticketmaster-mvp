-- A seat is available if it has no booking. NOT EXISTS rather than a LEFT
-- JOIN: until Epic 3 a seat can have several bookings, and a join would
-- return that seat once per booking.
-- name: ListSeats :many
SELECT s.id, s.section, s.row, s.number, s.price_cents,
       NOT EXISTS (SELECT 1 FROM bookings b WHERE b.seat_id = s.id) AS available
FROM seats s
WHERE s.event_id = $1
ORDER BY s.section, s.row, s.number;
