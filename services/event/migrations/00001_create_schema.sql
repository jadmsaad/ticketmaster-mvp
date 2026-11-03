-- Runs with search_path set to the "event" schema.

-- +goose Up
CREATE TABLE events (
    id        BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name      TEXT NOT NULL,
    venue     TEXT NOT NULL,
    starts_at TIMESTAMPTZ NOT NULL
);

-- +goose Down
DROP TABLE events;
