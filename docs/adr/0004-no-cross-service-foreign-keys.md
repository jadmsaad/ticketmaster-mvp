# 4. No foreign keys across services

Date: 2026-11-02 · Status: Accepted · Epic 1

## Context

`booking.seats.event_id` refers to `event.events.id`. A foreign key would tie the two services' data together in one database.

Rough numbers for one stadium on-sale without a waiting room: about 2M buyers in the first minute, which is about 33k hold writes/s and 200k seat-map reads/s. One large Postgres server handles about 10–30k simple writes/s, and far fewer when the writes fight over the same rows. Booking will eventually need its own database, sharded by `event_id`, and a cross-database foreign key isn't possible.

## Decision

`seats.event_id` is a plain `BIGINT` with no foreign key. Booking never asks the event service whether an event exists. No inventory means 404 "no seats for this event".

Rejected: keeping the foreign key while both schemas share a server. It would work today, but the schema would then depend on co-location.

## Consequences

- \+ Booking can move to its own database, or be sharded by `event_id`, without schema changes.
- − Nothing stops seats from referencing an event that doesn't exist. Consistency becomes the job of the "event created" or "event cancelled" messages, once those exist.
- Inside a service, foreign keys stay (`bookings.seat_id → seats`).

**Reopen if:** never while the services are separate.
