# 7. No local gateway. An ALB routes by path on AWS

Date: 2026-11-02 · Status: Accepted · Epic 1

## Context

Clients need one entry point that sends `/events/{id}/seats` and `/bookings` to booking-api, and everything else to event-api. On AWS that's an Application Load Balancer, configured in Terraform. Locally, Nginx could imitate it.

## Decision

No gateway locally: each service listens on its own port (event-api :8081, booking-api :8082). On AWS (Epic 5), an ALB routes by path. API Gateway is added only when we need rate limiting.

```
local:  client ─┬─► :8081 event-api
                └─► :8082 booking-api
AWS:    client ─► ALB ─┬─ /events/{id}/seats, /bookings ─► booking-api
                       └─ everything else              ─► event-api
```

Rejected: Nginx locally. Its config wouldn't carry over to the ALB, so it would be work spent imitating production rather than using it.

## Consequences

- \+ Less local setup. Each service can be called and debugged directly.
- − Local and production routing differ. Path routing is first tested in Epic 5.

**Reopen if:** we run several booking instances locally to test scaling, which needs a local load balancer.
