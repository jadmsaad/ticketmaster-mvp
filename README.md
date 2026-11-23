# Ticketmaster MVP

[![CI](https://github.com/jadmsaad/ticketmaster-mvp/actions/workflows/ci.yml/badge.svg)](https://github.com/jadmsaad/ticketmaster-mvp/actions/workflows/ci.yml)

A working backend for selling assigned seats, built in Go. The hard part of ticketing is the on-sale spike: millions of buyers going after the same few seats at the same moment. This project builds the system up in stages: database guarantees, then Redis holds, then a waiting room. Each stage is load-tested, and the results record what it costs and what it fixes.

**Status:** Epic 1 (foundation) is done. Epic 2 is in progress: the reserve-then-confirm booking flow and the seat map API are built, with Redis holds in front of a Postgres guarantee that each seat is sold once. Next is the k6 load-test harness.

## Architecture

```
                 ┌──────────────┐     schema "event"
  client ─┬────► │  event-api   │ ──► events
          │      │  :8081       │
          │      └──────────────┘          one Postgres server,
          │      ┌──────────────┐          one database user per service
          └────► │ booking-api  │ ──► seats, bookings
                 │  :8082       │     schema "booking"
                 └──────┬───────┘
                        └────────────► Redis: holds, seat state, prices

  On AWS (planned): an ALB routes by path, with ECS Fargate per service and RDS.
```

| Service | Owns | Endpoints | Load profile |
|---|---|---|---|
| `event-api` | events | `GET /events`, `GET /events/{id}` | Read-heavy, cacheable |
| `booking-api` | seats, bookings | `GET /events/{id}/layout`, `GET /events/{id}/availability`, `POST /orders`, `POST /orders/{id}/confirm` | Write-heavy, contended, spiky |

Each service also serves `GET /healthz` and Prometheus `GET /metrics`.

## Key decisions

Each decision has an ADR with the options we rejected and what would make us revisit it. The full list is in [docs/adr](docs/adr/README.md).

- **Two services, split by load profile** ([ADR 2](docs/adr/0002-two-services.md)). Booking scales on its own on sale day, and a booking failure doesn't take down browsing.
- **Each service owns its data** ([ADR 3](docs/adr/0003-schema-and-user-per-service.md), [ADR 4](docs/adr/0004-no-cross-service-foreign-keys.md)). Each service has its own schema and database user, and there are no foreign keys across services, so booking can move to its own database or be sharded by `event_id`.
- **Hand-written SQL with generated Go (sqlc), not an ORM** ([ADR 5](docs/adr/0005-sqlc.md)). Concurrency control needs exact control over the SQL.
- **Migrations run as one-off jobs** ([ADR 6](docs/adr/0006-migrations-as-one-off-jobs.md)). Twenty instances starting at once never race to change the schema.
- **Reserve, then confirm; Postgres has the final say** ([ADR 11](docs/adr/0011-booking-flow.md)). Holds and seat state live in Redis, so the on-sale spike never reaches the database. A unique constraint on `bookings.seat_id` still guarantees one booking per seat if a Redis lock expires or is lost in a failover.
- **The seat map is a static layout plus a small live poll** ([ADR 12](docs/adr/0012-seat-map-api.md)). The layout is cached at the CDN for a day. Availability is a bitmap and prices are dictionary-encoded, so the 1-second poll stays at about 13 KB for a stadium.

Measured results from the load tests will be added here as each epic lands.

## Run it locally

You need Go 1.27 or later and Docker. Postgres runs in Docker, and the services run natively for fast restarts ([ADR 8](docs/adr/0008-native-apps-docker-db.md)).

```bash
make setup         # start Postgres and Redis, apply migrations, seed 1 event with 100 seats
make run-event     # event-api on :8081
make run-booking   # booking-api on :8082 (second terminal)
```

```bash
curl localhost:8081/events/1
curl localhost:8082/events/1/layout
curl localhost:8082/events/1/availability

# Reserve seat 7 as alice, then confirm with the returned order ID
curl -X POST localhost:8082/orders -H 'X-User-ID: alice' -d '{"event_id": 1, "seat": 7}'
curl -X POST localhost:8082/orders/<order-id>/confirm -H 'X-User-ID: alice' -d '{"event_id": 1, "seat": 7}'
```

`X-User-ID` stands in for authentication.

To run everything in containers, the way it runs in production, use `make up-all`. Stop it with `make down`.

| Command | What it does |
|---|---|
| `make test` | All tests with the race detector, against the local Postgres and Redis |
| `make lint` | golangci-lint |
| `make ci` | The same checks as CI, except the Docker builds |
| `make generate` | Regenerate sqlc code after changing queries or migrations |
| `make reset` | Wipe all data, including bookings, and re-seed |

## Repository layout

```
services/
  event/      cmd/event-api      internal/{event, postgres, httpapi}          migrations/
  booking/    cmd/booking-api    internal/{booking, postgres, redis, httpapi} migrations/
internal/     code shared by both services: httpx, postgres, service, testdb, testredis
cmd/seed/     dev-only seed tool
deploy/       Postgres roles and schemas
docs/adr/     architecture decision records
```

Inside each service, the domain package (`event`, `booking`) holds the types and rules and imports no infrastructure. Each adapter (`postgres`, `redis`, `httpapi`) depends on the domain, never the reverse ([ADR 10](docs/adr/0010-ports-and-adapters-layout.md)). Go's `internal/` rule also stops one service importing the other's code. Both boundaries are checked at compile time.

## Testing

- **Store tests** run real SQL against a fresh, migrated Postgres database for each test.
- **Handler tests** use fakes behind small interfaces and send requests through the real routes, so they check status codes, route patterns, and the exact JSON contract ([ADR 9](docs/adr/0009-handler-tests.md)).
- **CI** (GitHub Actions) runs on every PR: lint, tests with `-race` against Postgres, build, `go mod tidy` and `sqlc diff` drift checks, govulncheck, and Docker image builds.

## Roadmap

| Epic | What it shows |
|---|---|
| 1. Foundation | Services, schemas, migrations, tests, CI ✅ |
| 2. Booking | Reserve a seat with a time-limited hold, then complete. Postgres guarantees one booking per seat, a concurrency test proves it, and a k6 harness measures the flow |
| 5. AWS | Terraform, ECS Fargate, RDS, ALB, and the load test repeated in the cloud |
| 6. Payments | Stripe checkout, webhooks that are safe to process twice, refunds for payments that arrive late |
| 7. Failure testing | Kill Redis or a service mid-checkout, and fail over RDS |
| 8–10 | Search, a virtual waiting room, an observability stack |

AI features build on these: natural-language event search, a support agent that can issue refunds with human approval, and Q&A over venue policies. Each will have a labelled test set and pass lines set before any code is written.
