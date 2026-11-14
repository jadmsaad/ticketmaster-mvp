# 9. Handler tests: fakes behind consumer-side interfaces, sent through the mux

Date: 2026-11-14 · Status: Accepted · Epic 1

## Context

Handlers translate between HTTP and our code: they parse the path, call the store, and map the result to a status and a JSON body. That mapping is the API contract, and nothing tested it. Store tests already cover the SQL against a real Postgres.

## Decision

1. **Each handler declares a small interface** listing only the store methods it calls (`eventStore`, `seatStore`). `*Store` satisfies it in production.
   Rejected: testing through a real Postgres. It's slower, makes 500s hard to trigger, and duplicates the store tests.
2. **Fakes use function fields.** A test plugs in only the functions it needs. If a function isn't set, calling it panics with "unexpected call", so a stray store call fails the test.
   Rejected: simple canned-value fakes, which drift into a different shape in each package. Also rejected: gomock, which adds a dependency and a codegen step, and breaks on refactors because it checks calls rather than outcomes.
3. **Requests go through the real routes.** `httpxtest.Serve` calls `Register` on a fresh mux, so route patterns, methods, and path parameter names are tested too.
   Rejected: calling the handler method directly, which can't catch a mismatched `{id}` or a wrong method.
4. **Bodies are compared as JSON values.** `httpxtest.AssertJSON` ignores key order and whitespace, so expected bodies can be pretty-printed.
   Rejected: decoding into the Go struct, which can't see a renamed JSON field because the tag changes on both sides. Also rejected: exact string comparison, which fails on harmless whitespace.

## Consequences

- \+ Tests run in milliseconds without Docker, and any store outcome can be set up in one line.
- \+ Both mistakes this targets were checked by deliberately breaking the code: a renamed JSON tag and a mismatched path parameter both make the tests fail.
- − Handler tests don't catch SQL bugs. The store tests do.
- When a service layer appears (booking, Epic 2), handlers fake the service instead of the store, using the same pattern.

**Reopen if:** a test needs to verify that a call did *not* happen, or happened exactly once (for example Stripe refunds in Epic 6). A call-checking mock may be worth it there.
