# Ticketmaster MVP: Plan

Last updated: 2026-11-02

**Goal:** learn system design and get hands-on with the technologies by building a trimmed Ticketmaster backend. Learning matters more than feature completeness. The UI is out of scope.

**Current status:** Task 1 is built and checked, not yet committed. Next step: review it together, then plan Task 2.

## How we work

- Work one task at a time: plan it together, Claude writes the code, then review the result together.
- Don't start the next task until its approach has been agreed.
- Before each experiment, the user predicts the outcome. Measure, then compare.
- At the end of each session, update the status, task checklist, and session log in this file.

## Decisions

- [x] **Language:** Go, chosen by the user on 2026-11-02.
- [x] **Service layout:** one service with separate event and booking modules, chosen by the user on 2026-11-02.
- [x] **Cloud:** AWS, deployed after Task 4, chosen by the user on 2026-11-02.

## Development environment

- **Editor:** VS Code, with the extensions for Go, Docker, and Terraform. Claude Code runs in its integrated terminal.
- **Local services:** Postgres, Redis, Nginx, and Elasticsearch run in Docker Compose, not installed directly on the Mac. The database and Redis CLIs run through their containers.
- **Installed on the Mac when needed (Homebrew):**
  - Task 1: Go
  - Task 2: k6
  - AWS deployment: Terraform, AWS CLI
  - Task 5: Stripe CLI
- **Version control:** a git repo in this folder, with a `.gitignore` for secrets. Secrets stay in a local `.env` and are never committed.

## Architecture

**Request flow:** client -> gateway (routing, rate limiting, fake user ID instead of real auth) -> app -> Postgres and Redis. Stripe calls back through webhooks. Elasticsearch/OpenSearch serves search.

| Component | Local | AWS | Role |
|---|---|---|---|
| Gateway | Nginx | API Gateway | Routing and rate limiting |
| App | Container | ECS Fargate behind a load balancer | Event and booking logic |
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

## Tasks

- [ ] **1. Foundation:** schema, seed one event with seats, event and seat-map endpoints, Docker Compose.
- [ ] **2. Naive booking:** no protection on purpose. Race 1,000 concurrent users for the same seats and count the double-bookings.
- [ ] **3. Database protection:** add the Postgres guarantee, confirm double-bookings drop to zero, and measure the performance cost.
- [ ] **4. Redis holds:** add holds with a TTL. Compare Postgres load and latency against Task 3.
- [ ] **AWS deployment:** set up the billing alert, write the Terraform, deploy, and repeat the load test. See "AWS cost rules."
- [ ] **5. Stripe:** checkout, webhooks, duplicate handling, late-payment refund.
- [ ] **6. Failure testing:** kill Redis and the app mid-checkout, and trigger an RDS failover. Check what happens to holds and payments.
- [ ] **7. Search:** add Elasticsearch/OpenSearch and sync it from Postgres.
- [ ] **8. Stretch: virtual waiting room:** admit users fairly during an on-sale surge.

## AWS cost rules

- The user creates the AWS account and enters payment details.
- Set up a budget alert before creating anything else.
- Don't use a NAT gateway.
- Run `terraform destroy` at the end of every session.
- Rough estimate: $60-120 a month if left running, or a few dollars a month if run only during sessions. Check current pricing before deploying.

## Session log

### 2026-11-02

- Chose Ticketmaster as the first project and agreed on the trimmed scope and task order, and chose Go, one service, and AWS after Task 4.
- Agreed the Task 1 approach and built it: seat ownership lives in a `bookings` table with no unique constraint on `seat_id` until Task 3. The `seed` command also runs migrations. Waiting on review, so the Task 1 checkbox stays open.
- Still undecided: when Nginx gets added (at the AWS deployment, or whenever rate limiting is first needed).
