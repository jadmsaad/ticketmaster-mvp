# Ticketmaster MVP: Plan

Last updated: 2026-11-15

**Goal:** build a working backend MVP of Ticketmaster: not a throwaway proof of concept, but trimmed in scope. Along the way, learn how professionals build a Go backend like this and the system design behind it. Use production patterns rather than tutorial shortcuts. The UI is out of scope.

**Current status:** Epic 2 is in progress. The epic design is agreed (5 decisions below), and T1 (Redis holds plus the ports-and-adapters layout, ADR 10) is merged. Next step: T2's decision steps. AI lessons 1–2 in `~/ai-dev-learning` interleave from here.

## How we work

- The plan is split into epics. An epic is broken into tasks only when we start it.
- **Epic:** design the overall approach together, with a short scope: goal, constraints, pass lines, and what we're not doing.
- **Task:** Claude breaks it into decision steps. Each step is one real decision with a genuine trade-off, shown with code sketches. The user decides and explains why first, then Claude gives its view. Obvious defaults are stated in one line rather than made into steps.
- **Code:** once every decision is made, Claude writes all the code, then lists any small decisions it made while coding.
- **Format:** diagrams for systems and flows, code snippets with a linked file name, concise text.
- **Shipping:** each task lands through its own branch and a squash-merged PR, with `main` protected and CI required, following Marina's style (chosen 2026-11-15). The PR body says why and lists the smaller decisions. CI also runs weekly, for new vulnerabilities.
- **Recording decisions:** decisions that are hard to reverse or architectural get a short ADR in `docs/adr/NNNN-title.md` (Context, Decision with rejected options, Consequences, "Reopen if"), listed in `docs/adr/README.md`. Smaller task decisions go in the commit or PR description. The README will get a "Key decisions" section that links to the ADRs and includes the measured numbers.
- **Pacing:** Ticketmaster work and the AI lessons in `~/ai-dev-learning` are interleaved, and Ticketmaster is the project the lessons are applied to.
- Before each experiment, the user predicts the outcome. Measure, then compare.
- At the end of each session, update the status, epics, and session log in this file.

## Decisions

- [x] **Language:** Go, chosen by the user on 2026-11-02.
- [x] **Service layout:** two services, `event-api` and `booking-api`, so booking can scale on its own. Chosen by the user on 2026-11-02, replacing the earlier choice of one service.
- [x] **Gateway:** none locally, because each service runs on its own port. On AWS, an ALB routes by path, and API Gateway is added only for rate limiting. Chosen by the user on 2026-11-02.
- [x] **Database access:** sqlc, generating typed Go from hand-written SQL. No ORM, because concurrency control needs exact SQL. Chosen by the user on 2026-11-02.
- [x] **Cloud:** AWS, deployed after the booking flow (Epic 2), chosen by the user on 2026-11-02.

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

### Epic 1: Foundation (done)
- [x] Schema, migrations, and seed data (1 event, 100 seats)
- [x] Event and seat-map endpoints, Docker Compose
- [x] Review: shared helpers moved to `internal/httpx`, database access switched to sqlc
- [x] Split into `event-api` (:8081) and `booking-api` (:8082) in a monorepo with per-service folders (`services/<name>/{cmd,internal,migrations}`). Each service has its own schema and database user, migrations run as one-off jobs, and the seat map moved to booking.
- [x] Audit fixes for load testing: explicit pool size (`DB_MAX_CONNS`, default 20), server timeouts, request logging with request IDs, and Prometheus `/metrics` (request latency by route, connection pool stats).
- [x] CI (GitHub Actions, on every PR and push to `main`): lint, tests with `-race` against Postgres, build, `go mod tidy`, `sqlc diff`, govulncheck, and Docker builds. golangci-lint v2 config in `.golangci.yml`. `make ci` runs the same checks locally. Won't run until the repo is pushed to GitHub.
- [x] Handler tests: each handler declares a small store interface, tests use function-field fakes, requests go through the real routes, and bodies are compared as JSON (ADR 9). Shared helpers are in `internal/httpx/httpxtest`.
- [x] README: pitch, architecture, key decisions linking the ADRs, how to run it locally, and the roadmap.
- [x] ADRs 1–8 written for the decisions made before ADRs were adopted.
- [x] Reviewed together and closed. Before closing, govulncheck flagged GO-2026-6859 in the Go 1.27.3 standard library, so `go.mod` moved to Go 1.27.4.
- Deferred to later epics: separate liveness and readiness checks (Epic 5), `/v1` API versioning, a slimmer image, moving tool dependencies out of `go.mod`, and pushing to GitHub so CI runs (plus a scheduled govulncheck run).

### Epic 2: Booking with holds and database protection, plus a load-test harness
Two-step booking:
- **Reserve:** hold a seat with a TTL while the buyer pays.
- **Complete:** turn a held seat into a booking.

A unique constraint on `bookings.seat_id` is the final say (Step 1, decided 2026-11-14), because a hold can expire mid-payment or be lost in a failover. A concurrency test proves the protection works, because it fails if the protection is removed. Build the k6 harness, reused in Epics 5 and 7, and measure the flow under 1,000 concurrent buyers.

Epic design decisions (2026-11-14/15), recorded in ADR 11:
1. ✅ **Database guarantee:** `UNIQUE (seat_id)` on `bookings`, with error 23505 mapped to "seat already booked". It's the backstop when a hold expires, is lost, or a code path skips the check.
2. ✅ **Holds live in Redis** (`SET NX EX`), because holds exist to absorb the on-sale spike in front of Postgres. The TTL also means no cleanup job.
3. ✅ **Service layer:** `handler → service → store (Postgres) + holds (Redis)`. Handler tests fake the service.
4. ✅ **The API is an order resource with a lifecycle:** `POST /orders` (status held, with a TTL), `POST /orders/{id}/confirm` (status confirmed), and expiry. The order ID is an app-generated UUIDv7 that doubles as the fencing token. Held orders live only in Redis; only confirmed bookings reach Postgres (`bookings.order_id UNIQUE`). An expired or unknown order returns 404. `X-User-ID` is the stand-in for auth.
5. ✅ **Load scenarios:** one-seat, uniform, and skewed seat choice, all arriving as a spike, plus skewed arriving as a ramp. Each run records double-bookings (must be 0), p50/p95/p99, error and 409 rates, and pool waits. The user predicts before each run.

Tasks:
- [x] T1: Redis in Compose and CI, plus `redis.HoldStore`, tested against real Redis. Also restructured both services into ports and adapters (ADR 10): `internal/{<svc>, postgres, redis, httpapi}`. Wiring Redis into `booking-api` happens in T3.
- [ ] T2: The seat-state read model in Redis (free, held, or sold per seat), plus the booking migration, the service (`Reserve`, `Confirm`), and concurrency tests.
  - ✅ Step 1, what `Reserve` checks: seat state in Redis (option C), never Postgres. Sold seats are common during an on-sale, not an edge case, and the seat map needs this read model anyway. Seats are loaded into Redis when an event is published (the seed for now), rebuilt from Postgres if the state goes missing, and expire after the event (`EXPIREAT starts_at + 1 day`). The unique constraint is still the backstop if a "sold" marker is lost.
  - ✅ Step 2, encoding: a 1-bit sold bitmap `seats:sold:{event:N}` at position `seats.idx` (0…N within each event), plus a sorted set of live holds `holds:{event:N}` (member = seat idx, score = expiry in ms). A 2-bit held state was rejected because TTL expiry can't reset bits, so abandoned holds would show as held forever, and finding expired bits means checking every held seat. The sorted set is effectively an index on expiry: reads use `ZRANGEBYSCORE (now +inf`, so a seat is available the instant its hold expires, and the reserve script prunes with `ZREMRANGEBYSCORE -inf now`. Lua uses Redis's own clock (`TIME`). All of an event's keys share the `{event:N}` hash tag, so they live on one Redis Cluster shard and can be used together in one script. The bitmap is a cache: it's rebuilt from Postgres bookings when missing, and expires at `starts_at + 1 day`.
  - ✅ Step 3, the service lives in the domain package (`booking/service.go`), which is the Go default. Moving it to its own package later means moving one file.
  - ✅ Step 4, seats are addressed by `(event_id, idx)` in the API and in Redis (holds become `hold:{event:e}:seat:{idx}`). Confirm resolves the seat inside its single `INSERT … SELECT id FROM seats WHERE event_id AND idx`.
  - ✅ Step 5, no order key in Redis. The lock `hold:{event:N}:seat:{idx}` stores `orderID|userID`. Confirm sends `{order_id, event_id, seat idx}`, which the client already has from the reserve response, and the server checks the lock value before inserting. That means 2 Redis writes per reserve (the lock and the holds-set entry) instead of 3.
  - Deferred: a per-user cap on active orders (against hoarding) goes with the Epic 9 waiting room.
  - Agreed tests: duplicate booking returns `ErrSeatTaken` (store, plain); 50 buyers `Claim` one seat and exactly 1 wins (Redis, start gate); 50 buyers `Reserve` and `Confirm` and exactly 1 booking results (service with real Redis and Postgres, start gate); wrong user, retry, and expiry (service with fakes). 50 is more than `DB_MAX_CONNS`, so some goroutines wait for a connection. No forced-interleaving test, because it would only re-test Postgres's own unique index.
- [ ] T3: HTTP endpoints for orders, with handler tests (ADR 9 pattern, faking the service). The seat map moves to reading from the Redis seat state.
- [ ] T4: k6 harness and `make loadtest`, predictions, runs, `docs/experiments/02-booking.md`.

### Epic 3: merged into Epic 2
On 2026-11-14 we decided not to build an unprotected version on purpose. We already know it races, and following Marina's principle, experiments are kept for real unknowns. The race shows up as the test that the protection must pass.

### Epic 4: merged into Epic 2
On 2026-11-15 we decided to build holds as part of the booking flow rather than add them later. The trade-off is that there's no before/after measurement of how much holds help.

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

### Epic 10: Observability stack (end game)
The services already expose Prometheus `/metrics`. Add something that scrapes and stores them: Prometheus and Grafana in Docker Compose locally, and an ADOT collector sidecar pushing to Amazon Managed Prometheus or CloudWatch on AWS. Until then, load tests rely on k6's client-side numbers, logs, and `curl /metrics`. Leaning towards switching to OpenTelemetry for metrics and traces here (the metrics code lives in `internal/httpx/middleware.go` and `internal/postgres/pool.go`). Pull/scrape is preferred over push. Decide all of this when the epic starts.

## AI extensions

Added on 2026-11-09. On 2026-11-14 we decided to interleave these with the epics instead of leaving them until the end: each one starts as soon as the epic it depends on is done, and the AI lessons in `~/ai-dev-learning` are applied to them. Each extension is broken into tasks only when we start it, like an epic.

Each one follows the light version of the AI project approach in `~/ai-dev-learning/start-here.md`:

- Write a one-page scope first, including why AI makes sense for it.
- Build a small labelled test set, and set pass lines before writing any code.
- Keep prompts in versioned files, and log every model call: model, prompt version, tokens, cost, latency, and errors.
- Rerun the test set whenever a prompt or model changes.
- The service code stays in Go, using an official provider SDK. Evaluation scripts may be in Python.

### AI 1: Natural-language search

Turn a query like "jazz this weekend under $50 near downtown" into structured filters, then run them through the real event search. The model only produces filters, so it can't invent events, prices, or availability.

- **When:** after Epic 2. The filters run as a Postgres query in `event-api` at first. Epic 8 later swaps in Elasticsearch behind the same filter type, without changing the AI part.
- **Lessons first:** lessons 1–2 (purpose, scoping) are applied to this feature.
- **Learn:** structured output, tool calling, guarding against invented or invalid filters.
- **Test set:** labelled queries, each with the filters it should produce, scored for exact match per filter.

### AI 2: Support agent

Handle requests like "refund my ticket for Friday" with tools that look up bookings, cancel them, and issue Stripe refunds.

- **Builds on:** Epic 6.
- **Learn:** agents, tool use, guardrails, limits on steps and cost, human approval before risky actions such as refunds.
- **Test set:** scripted support requests, measured on whether each is completed correctly and whether any unsafe action was attempted.

### AI 3: Venue policy Q&A (optional)

Answer questions like "Can I bring a bag?" from venue policy documents, and say so when the documents don't cover it.

- **Learn:** RAG: chunking, embeddings, retrieval, and answering only from the documents.
- **Test set:** questions with known answers, scored separately on whether retrieval found the right passage and whether the answer was correct.

AI features that were considered and rejected:

- **Q&A over structured event data** (date, price, seats left): an event page already shows it, and a model could misstate prices or availability.
- **Seat recommendations:** an ordinary algorithm handles this.
- **Bot detection:** a classical ML problem rather than an LLM one.

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

### 2026-11-09

- Added an "AI extensions" section after the epics: natural-language search, a support agent, and optional venue policy Q&A. Recorded the AI ideas that were rejected, and why.

### 2026-11-14

- Walked through `ci.yml` block by block, and discussed continuous deployment, canaries, reverts, and test layers.
- Changed how we work to decision steps: design each epic together, break each task into real trade-off decisions the user makes first, and Claude writes all the code afterwards. Diagrams for systems and flows.
- Decided to interleave the AI work: AI 1 (natural-language search) moves to right after Epic 3, using Postgres filters until Epic 8.
- Chose ADRs for recording decisions (over a single decision log or Marina's per-change folders). Backfilling the 8 existing decisions is in the Epic 1 checklist.
- Designed the handler tests in four decision steps: an interface plus a fake, function-field fakes, requests through the real routes, and bodies compared as JSON. Built them for both services, and recorded the reasoning in ADR 9. Checked that they fail on a renamed JSON field and on a mismatched path parameter.
- Wrote ADRs 1–8 and the README, which the user reviewed and committed.
- Closed Epic 1 after a full green `make ci` run. Moved to Go 1.27.4 for GO-2026-6859.
- Merged Epics 2 and 3. The user questioned building a naive version on purpose: its outcome is already known, so it isn't a real experiment. Epic 2 now builds booking correctly and adds a concurrency test and a k6 harness. AI 1 now follows Epic 2.
