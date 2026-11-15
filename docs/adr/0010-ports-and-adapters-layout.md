# 10. Ports-and-adapters package layout inside each service

Date: 2026-11-15 · Status: Accepted · Epic 2

## Context

Booking was one package holding handlers, the Postgres store, and the new Redis holds, separated only by file names. Booking will soon add a service layer and Stripe (Epic 6), and the AI 2 agent will call it. With everything in one package, nothing stops business logic from calling Redis or SQL directly; only code review would.

## Decision

Each service splits its `internal/` into a domain package plus one package per adapter:

```
services/<svc>/internal/
  <svc>/      domain: types, errors, rules, and the interfaces it needs (no infrastructure imports)
  postgres/   Store + sqlc-generated code       imports the domain
  redis/      HoldStore (booking only)          imports the domain
  httpapi/    handlers and their tests          imports the domain
cmd/<svc>-api/main.go  wires adapters into the domain
```

```
        httpapi ──┐
                  ▼
postgres ──► <svc> (domain) ◄── redis
```

Types are named without repeating the package (`postgres.Store`, `redis.HoldStore`). Interfaces stay where they're used (ADR 9).

Rejected: one flat package per service. It's common in Go for small packages, and adapters are just as easy to swap because the interfaces do that. But the boundary is only a convention. Also rejected: a layer-first layout across the whole repo (`controllers/`, `models/`), which Go teams avoid because it spreads every feature across every folder.

## Consequences

- \+ The compiler enforces the boundary: the domain can't import an adapter without an import cycle. Checked with `go list -deps`: neither domain package imports pgx, Redis, or `net/http`.
- \+ Each folder shows what talks to what. Adding Stripe or swapping Redis for Valkey means a new adapter package, with the domain untouched.
- − More packages and imports. Shared types must live in the domain. The go-redis import needs an alias (`goredis`) inside our `redis` package.

**Reopen if:** never for services that keep growing. A tiny new service could start flat.
