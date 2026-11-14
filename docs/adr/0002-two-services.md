# 2. Split into event-api and booking-api

Date: 2026-11-02 · Status: Accepted · Epic 1

## Context

Browsing events and booking seats have opposite load profiles:

```
event-api     read-heavy, cacheable, steady            → few instances + cache
booking-api   write-heavy, contended, huge spikes      → scales out on its own
              when tickets go on sale
```

With one service, an on-sale spike would slow browsing for every other event, and booking couldn't be scaled on its own.

## Decision

Two services in one monorepo, `services/<name>/{cmd,internal,migrations}`. `event-api` owns events. `booking-api` owns seats (event inventory), bookings, and the seat map, because availability changes on every booking. Go's `internal/` packages stop one service importing the other's code at compile time.

Rejected: one service. It's simpler, but it can't scale booking separately, and splitting later costs more.

## Consequences

- \+ Booking can run 20 instances on sale day while event runs 2. A booking failure doesn't take down browsing.
- − Two deploys, two sets of config. Data that crosses the boundary needs messages, not joins (see ADR 4).
- Shared code (`internal/httpx`, `postgres`, `service`, `testdb`) lives at the repo root.

**Reopen if:** a third clearly separate workload appears (search, payments), which would mean a new service.
