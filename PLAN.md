# Ticketmaster MVP: Plan

Last updated: 2026-11-02

**Goal:** learn how a professional Go backend for Ticketmaster is built, and the system design behind it, by building a trimmed version. Use production patterns rather than tutorial shortcuts. The UI is out of scope.

**Current status:** Epic 1 is nearly done. The split into `event-api` and `booking-api` is built and checked. Next step: review the split, commit it, then plan Epic 2.

## How we work

- The plan is split into epics. An epic is broken into tasks only when we start it.
- Work one task at a time: plan it together, Claude writes the code, then review the result together.
- Don't start the next task until its approach has been agreed.
- Before each experiment, the user predicts the outcome. Measure, then compare.
- At the end of each session, update the status, epics, and session log in this file.

## Decisions

- [x] **Language:** Go, chosen by the user on 2026-11-02.
- [x] **Service layout:** two services, `event-api` and `booking-api`, so booking can scale on its own. Chosen by the user on 2026-11-02, replacing the earlier choice of one service.
- [x] **Gateway:** none locally, because each service runs on its own port. On AWS, an ALB routes by path, and API Gateway is added only for rate limiting. Chosen by the user on 2026-11-02.
- [x] **Database access:** sqlc, generating typed Go from hand-written SQL. No ORM, because concurrency control needs exact SQL. Chosen by the user on 2026-11-02.
- [x] **Cloud:** AWS, deployed after Epic 4, chosen by the user on 2026-11-02.

## Development environment

- **Editor:** VS Code, with the extensions for Go, Docker, and Terraform. Claude Code runs in the VS Code extension panel.
- **Local services:** Postgres, Redis, and Elasticsearch run in Docker Compose, not installed directly on the Mac. The database and Redis CLIs run through their containers.
- **Running the apps:** the services run natively with `go run` for fast restarts: `make setup`, then `make run-event` and `make run-booking`. `make up-all` runs everything in containers, as in production.
- **Installed on the Mac when needed (Homebrew):**
  - Epic 1: Go
  - Epic 2: k6
  - Epic 5: Terraform, AWS CLI
  - Epic 6: Stripe CLI
- **Version control:** a git repo in this folder, with a `.gitignore` for secrets, binaries, and coverage output. Secrets stay in a local `.env` and are never committed.

## Architecture

**Request flow:** client -> ALB (path routing; locally, direct to each port) -> `event-api` or `booking-api` -> Postgres and Redis. A fake user ID stands in for real auth. Stripe calls back through webhooks. Elasticsearch/OpenSearch serves search.

| Service | Owns | Endpoints | Load profile |
|---|---|---|---|
| `event-api` | events | `GET /events`, `GET /events/{id}` | Read-heavy, cacheable, steady |
| `booking-api` | seats, bookings | `GET /events/{id}/seats`, `POST /bookings` | Write-heavy, contended, spikes at on-sale |

Each service owns its tables in its own Postgres schema and connects as its own database user, which has no access to the other schema.

| Component | Local | AWS | Role |
|---|---|---|---|
| Routing | Separate ports (8081, 8082) | ALB, plus API Gateway for rate limiting | Send each path to the right service |
| Services | Containers | Separate ECS Fargate services | Scale independently |
| Main database | Postgres container | RDS | Source of truth |
| Holds | Redis container | ElastiCache | Temporary seat holds with a TTL |
| Payments | Stripe test mode | Stripe test mode | Checkout, webhooks, refunds |
| Search | Elasticsearch | OpenSearch | Event search, synced from Postgres |
| Monitoring | Logs | CloudWatch | Metrics and logs |
| Infrastructure | Docker Compose | Terraform | Reproducible setup and teardown |

### Scope

- **In the first version:** assigned seats, one seat per booking.
- **Cut:** real auth, CDN (CloudFront is an optional add-on after deployment), UI.
- **Later:** multi-seat bookings (all-or-nothing holds).

### Core design rules

1. Postgres has the final say on who owns a seat, enforced by a uniqueness constraint or conditional update. A Redis lock alone isn't enough, because it can expire or be lost in a failover.
2. Redis handles temporary holds and keeps load off the database.
3. Abandoned checkouts release their seats when the hold expires.
4. A payment that succeeds after its hold has expired must be handled, with an automatic refund if the seat is gone.
5. Stripe webhooks must be safe to process more than once.

## Epics

### Epic 1: Foundation (in progress)
- [x] Schema, migrations, and seed data (1 event, 100 seats)
- [x] Event and seat-map endpoints, Docker Compose
- [x] Review: shared helpers moved to `internal/httpx`, database access switched to sqlc
- [x] Split into `event-api` (:8081) and `booking-api` (:8082) in a monorepo with per-service folders (`services/<name>/{cmd,internal,migrations}`). Each service has its own schema and database user, migrations run as one-off jobs, and the seat map moved to booking.
- [ ] Review the split together, then commit.

### Epic 2: Naive booking
No protection, on purpose. Race 1,000 concurrent users for the same seats and count the double-bookings.

### Epic 3: Database protection
Add the Postgres guarantee, confirm double-bookings drop to zero, and measure the performance cost.

### Epic 4: Redis holds
Add holds with a TTL. Compare Postgres load and latency against Epic 3.

### Epic 5: AWS deployment
Set up the billing alert, write the Terraform, deploy both services, and repeat the load test. See "AWS cost rules."

### Epic 6: Payments (Stripe)
Checkout, webhooks, duplicate handling, and refunds for payments that arrive late.

### Epic 7: Failure testing
Kill Redis and a service mid-checkout, and trigger an RDS failover. Check what happens to holds and payments.

### Epic 8: Search
Add Elasticsearch/OpenSearch and sync it from Postgres.

### Epic 9: Virtual waiting room (stretch)
Admit users fairly during an on-sale surge.

## AWS cost rules

- The user creates the AWS account and enters payment details.
- Set up a budget alert before creating anything else.
- Don't use a NAT gateway.
- Run `terraform destroy` at the end of every session.
- Rough estimate: $60-120 a month if left running, or a few dollars a month if run only during sessions. Check current pricing before deploying.

## Session log

### 2026-11-02

- Chose Ticketmaster as the first project and agreed on the trimmed scope and order of work, and chose Go and AWS.
- Built the single-service foundation: seat ownership lives in a `bookings` table with no unique constraint on `seat_id` until Epic 3. The `seed` command also runs migrations.
- During review: moved shared HTTP helpers into `internal/httpx`, and switched database access to sqlc. The user knows SQL and wants production Go patterns, not basics.
- Restructured the plan into epics. Decided to split into `event-api` and `booking-api` so booking can scale on its own, with no local gateway and an ALB on AWS.
- Built the split as a monorepo: one Postgres server with a schema and database user per service, no cross-service foreign key (so booking can later move to its own database or be sharded by `event_id`), and shared `internal/service`, `internal/postgres`, and `internal/testdb` packages.
