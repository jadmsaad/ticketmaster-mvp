-- Runs with search_path set to the "booking" schema.

-- +goose Up

-- Seat inventory for one event. event_id deliberately has no foreign key: events
-- belong to the event service, and a cross-service FK would block moving
-- booking to its own database or sharding it by event_id.
CREATE TABLE seats (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    event_id    BIGINT NOT NULL,
    section     TEXT NOT NULL,
    row         TEXT NOT NULL,
    number      INT NOT NULL,
    price_cents INT NOT NULL CHECK (price_cents >= 0),
    UNIQUE (event_id, section, row, number)
);

-- Deliberately NOT unique on seat_id: Epic 2 needs double-bookings to be
-- possible so it can count them. Epic 3 adds the uniqueness guarantee.
CREATE TABLE bookings (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    seat_id    BIGINT NOT NULL REFERENCES seats (id),
    user_id    TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX bookings_seat_id_idx ON bookings (seat_id);

-- +goose Down
DROP TABLE bookings;
DROP TABLE seats;
