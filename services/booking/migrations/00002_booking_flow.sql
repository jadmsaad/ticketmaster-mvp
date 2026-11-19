-- Runs with search_path set to the "booking" schema. See ADR 11.

-- +goose Up

-- Each seat's position within its event (0…N-1). Redis seat state is a bitmap
-- indexed by it, and the API addresses seats as (event_id, idx).
ALTER TABLE seats ADD COLUMN idx INT;
UPDATE seats s
SET idx = numbered.i
FROM (
    SELECT id, row_number() OVER (PARTITION BY event_id ORDER BY section, row, number) - 1 AS i
    FROM seats
) numbered
WHERE s.id = numbered.id;
ALTER TABLE seats
    ALTER COLUMN idx SET NOT NULL,
    ADD CONSTRAINT seats_idx_check CHECK (idx >= 0),
    ADD CONSTRAINT seats_event_id_idx_key UNIQUE (event_id, idx);

-- The backstop: at most one booking per seat, whatever happens in Redis.
-- Each booking also records the order it confirmed, so a retried confirm finds
-- it instead of failing. order_id is NOT NULL with no default, so this
-- migration expects an empty bookings table (no deployed data exists; run
-- make reset locally first).
DROP INDEX bookings_seat_id_idx;
ALTER TABLE bookings
    ADD COLUMN order_id UUID NOT NULL,
    ADD CONSTRAINT bookings_seat_id_key UNIQUE (seat_id),
    ADD CONSTRAINT bookings_order_id_key UNIQUE (order_id);

-- +goose Down
ALTER TABLE bookings
    DROP CONSTRAINT bookings_order_id_key,
    DROP CONSTRAINT bookings_seat_id_key,
    DROP COLUMN order_id;
CREATE INDEX bookings_seat_id_idx ON bookings (seat_id);
ALTER TABLE seats
    DROP CONSTRAINT seats_event_id_idx_key,
    DROP CONSTRAINT seats_idx_check,
    DROP COLUMN idx;
