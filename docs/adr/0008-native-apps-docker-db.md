# 8. Apps run natively in development, with only infrastructure in Docker

Date: 2026-11-02 · Status: Accepted · Epic 1

## Context

Rebuilding an image on every code change (`docker compose up --build`) takes about 30 seconds. `go run` restarts in about 1 second. Postgres, and later Redis and Elasticsearch, are slow to set up natively and should match production versions.

## Decision

Postgres runs in Docker Compose (`make db`). The services run natively with `go run` (`make run-event`, `make run-booking`). `make up-all` runs everything in containers, as in production, using the Compose `apps` profile.

Rejected: running everything in Compose all the time, which makes the edit-run loop slow.

## Consequences

- \+ Fast feedback while developing. Infrastructure versions match production.
- − Native runs can differ from containers. This caught one real bug: times came back in the Mac's local time zone until the API was fixed to always return UTC. `make up-all` and CI's Docker builds cover the container path.

**Reopen if:** a dependency can't easily run natively (for example CGO or system libraries).
