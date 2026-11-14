# Architecture decision records

An ADR records a decision that is hard to reverse or that shapes the architecture: the context, what was chosen and what was rejected, the consequences, and what would make us reopen it. Smaller decisions live in commit messages.

| # | Decision | Status |
|---|---|---|
| 1–8 | Decisions made before ADRs were adopted: Go; two services; a schema and database user per service; no cross-service foreign key; sqlc instead of an ORM; migrations as one-off jobs; no local gateway; apps run natively | To be written |
| 9 | [Handler tests: fakes behind consumer-side interfaces, sent through the mux](0009-handler-tests.md) | Accepted |
