-- +goose Up
CREATE TABLE events (
    id        BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name      TEXT NOT NULL,
    venue     TEXT NOT NULL,
    starts_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE seats (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    event_id    BIGINT NOT NULL REFERENCES events (id),
    section     TEXT NOT NULL,
    row         TEXT NOT NULL,
    number      INT NOT NULL,
    price_cents INT NOT NULL CHECK (price_cents >= 0),
    UNIQUE (event_id, section, row, number)
);

-- Deliberately NOT unique: Task 2 needs double-bookings to be possible so it
-- can count them. Task 3 adds the uniqueness guarantee.
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
DROP TABLE events;
