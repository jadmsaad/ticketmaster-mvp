# Architecture decision records

An ADR records a decision that is hard to reverse or that shapes the architecture: the context, what was chosen and what was rejected, the consequences, and what would make us reopen it. Smaller decisions live in commit messages.

| # | Decision | Status |
|---|---|---|
| 1 | [Go for the backend services](0001-go.md) | Accepted |
| 2 | [Split into event-api and booking-api](0002-two-services.md) | Accepted |
| 3 | [One Postgres server, with a schema and database user per service](0003-schema-and-user-per-service.md) | Accepted |
| 4 | [No foreign keys across services](0004-no-cross-service-foreign-keys.md) | Accepted |
| 5 | [sqlc for database access, not an ORM](0005-sqlc.md) | Accepted |
| 6 | [Migrations run as one-off jobs, not at startup](0006-migrations-as-one-off-jobs.md) | Accepted |
| 7 | [No local gateway; an ALB routes by path on AWS](0007-no-local-gateway.md) | Accepted |
| 8 | [Apps run natively in development, with only infrastructure in Docker](0008-native-apps-docker-db.md) | Accepted |
| 9 | [Handler tests: fakes behind consumer-side interfaces, sent through the mux](0009-handler-tests.md) | Accepted |
