# 6. Migrations run as one-off jobs, not at startup

Date: 2026-11-02 · Status: Accepted · Epic 1

## Context

If each service instance migrated on startup, 20 booking instances starting on sale day would race to change the schema. Startup would also be slower and depend on DDL succeeding.

## Decision

Each service embeds its own migrations (goose, `embed.FS`) and runs them through a subcommand: `booking-api migrate`. It runs once before a deploy, as its own container (`booking-migrate` in Compose, later a one-off ECS task), connecting as the service's own role. Servers never migrate.

Rejected: migrating on startup, which races across instances and couples deploys to DDL.

## Consequences

- \+ One migrator per deploy, with no race. A failed migration stops the deploy before any new instance starts.
- \+ The same binary and image does both, so the migrations always match the code version.
- − Deploys have an ordering step: migrate, then roll out. Schema changes have to stay backwards compatible with the version still running (expand, then contract).

**Reopen if:** never. This is standard practice.
