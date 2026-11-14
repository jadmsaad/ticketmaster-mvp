# 3. One Postgres server, with a schema and database user per service

Date: 2026-11-02 · Status: Accepted · Epic 1

## Context

Each service should own its data, so that booking can later move to its own database (ADR 4). Running a separate Postgres server per service locally adds cost and setup without teaching much yet.

## Decision

One Postgres server and one database, with schemas `event` and `booking`. Each service connects as its own role (`event_api`, `booking_api`), whose `search_path` is its own schema and which has no access to the other schema or to `public` ([deploy/postgres/init.sql](../../deploy/postgres/init.sql)).

Rejected: shared tables with no boundary, which would invite cross-service joins. Also rejected for now: a server per service, which is overkill before the load tests show it's needed.

## Consequences

- \+ Ownership is enforced by Postgres: `booking_api` gets "permission denied" on `event.events`.
- \+ Moving a schema to its own server later is a connection-string change.
- − The services still share one server's CPU, memory, and IO, so an on-sale spike in booking slows event reads.
- The dev seed tool connects as admin and writes to both schemas. Production would create inventory from an "event created" message.

**Reopen if:** load tests show booking's write load hurting event reads. Then move booking to its own server.
