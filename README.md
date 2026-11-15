# Ticketmaster MVP

[![CI](https://github.com/jadmsaad/ticketmaster-mvp/actions/workflows/ci.yml/badge.svg)](https://github.com/jadmsaad/ticketmaster-mvp/actions/workflows/ci.yml)

A working backend for selling assigned seats, built in Go. The hard part of ticketing is the on-sale spike: millions of buyers going after the same few seats at the same moment. This project builds the system up in stages: database guarantees, then Redis holds, then a waiting room. Each stage is load-tested, and the results record what it costs and what it fixes.

**Status:** Epic 1 (foundation) is done: two services, schemas, migrations, tests, and CI. Next is Epic 2: reserve-then-complete booking, with time-limited seat holds and Postgres guaranteeing each seat is sold once, plus a load-test harness with 1,000 concurrent buyers.

## Architecture

```
                 ┌──────────────┐     schema "event"
  client ─┬────► │  event-api   │ ──► events
          │      │  :8081       │
          │      └──────────────┘          one Postgres server,
          │      ┌──────────────┐          one database user per service
          └────► │ booking-api  │ ──► seats, bookings
                 │  :8082       │     schema "booking"
                 └──────────────┘

  On AWS (planned): an ALB routes by path, with ECS Fargate per service and RDS.
```

| Service | Owns | Endpoints | Load profile |
|---|---|---|---|
| `event-api` | events | `GET /events`, `GET /events/{id}` | Read-heavy, cacheable |
| `booking-api` | seats, bookings | `GET /events/{id}/seats` (`POST /bookings` in Epic 2) | Write-heavy, contended, spiky |

Each service also serves `GET /healthz` and Prometheus `GET /metrics`.

## Key decisions

Each decision has an ADR with the options we rejected and what would make us revisit it. The full list is in [docs/adr](docs/adr/README.md).

- **Two services, split by load profile** ([ADR 2](docs/adr/0002-two-services.md)). Booking scales on its own on sale day, and a booking failure doesn't take down browsing.
- **Each service owns its data** ([ADR 3](docs/adr/0003-schema-and-user-per-service.md), [ADR 4](docs/adr/0004-no-cross-service-foreign-keys.md)). Each service has its own schema and database user, and there are no foreign keys across services, so booking can move to its own database or be sharded by `event_id`.
- **Hand-written SQL with generated Go (sqlc), not an ORM** ([ADR 5](docs/adr/0005-sqlc.md)). Concurrency control needs exact control over the SQL.
- **Migrations run as one-off jobs** ([ADR 6](docs/adr/0006-migrations-as-one-off-jobs.md)). Twenty instances starting at once never race to change the schema.
- **Postgres has the final say on who owns a seat.** Seat holds with a time limit keep load off the database but can't guarantee correctness on their own, because a lock can expire or be lost in a failover.

Measured results from the load tests will be added here as each epic lands.

## Run it locally

You need Go 1.27 or later and Docker. Postgres runs in Docker, and the services run natively for fast restarts ([ADR 8](docs/adr/0008-native-apps-docker-db.md)).

```bash
make setup         # start Postgres, apply migrations, seed 1 event with 100 seats
make run-event     # event-api on :8081
make run-booking   # booking-api on :8082 (second terminal)
```

```bash
curl localhost:8081/events/1
curl localhost:8082/events/1/seats
```

To run everything in containers, the way it runs in production, use `make up-all`. Stop it with `make down`.

| Command | What it does |
|---|---|
| `make test` | All tests with the race detector, against the local Postgres |
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
