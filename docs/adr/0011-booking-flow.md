# 11. Booking flow: Redis holds and seat state in front of a Postgres backstop

Date: 2026-11-18 · Status: Accepted · Epic 2

## Context

An on-sale is a spike. Our estimate for one stadium is about 2M buyers in the first minute, which is about 33k reserve attempts/s and 200k seat-map reads/s. One Postgres server handles about 10–30k simple writes/s, and far fewer when the writes compete for the same rows. Most reserves never become purchases: buyers abandon checkout or lose the race. Within a minute or two most seats are sold, so most reserve attempts are for seats that are already gone.

## Decision

Booking happens in two steps, **reserve** then **confirm**. Temporary, high-volume state lives in Redis, and permanent state lives in Postgres.

```
POST reserve {event, seat idx} ─► Redis: prune expired · sold? · lock NX · add to live holds
                                  ─► {order_id (UUIDv7), expires_at}          (no Postgres)
POST confirm {order_id, event, seat idx}
   ─► Redis: lock == "order_id|user"?
   ─► Postgres: INSERT booking … UNIQUE (seat_id)                            (the final say)
   ─► Redis: set sold bit · remove from live holds · delete lock
```

Redis keys per event. All of them share the `{event:N}` hash tag, so they sit on one Redis Cluster shard and one Lua script can use them together:

| Key | Type | Purpose | Lifetime |
|---|---|---|---|
| `seats:sold:{event:N}` | Bitmap, 1 bit per `seats.idx` | Is this seat sold? | Until `starts_at + 1 day`. Rebuilt from Postgres if missing |
| `holds:{event:N}` | Sorted set: seat idx → expiry (ms) | Which seats are held right now? | Expired entries are filtered out on read and pruned on each reserve |
| `hold:{event:N}:seat:{idx}` | String `order_id\|user_id` | The lock, and the fencing token at confirm | TTL = buyer's deadline + 1 min grace (11 min) |

Postgres: `bookings` has `UNIQUE (seat_id)` and `UNIQUE (order_id)`. `seats` has `idx`, numbered 0…N within each event, with `UNIQUE (event_id, idx)`. Confirm resolves the seat inside its single `INSERT … SELECT id FROM seats WHERE event_id = $1 AND idx = $2`.

The decisions, and what we rejected:

1. **Postgres's unique constraint is the backstop.** A Redis lock can expire mid-payment, be lost in a failover, or be skipped by a buggy code path. Rejected: a row lock (`FOR UPDATE`), which queues requests and ties up pool connections, and only protects code paths that remember to take it. Rejected: a conditional update on a seat status column.
2. **Holds live in Redis.** Holds exist to absorb the spike in front of Postgres, so they can't live in it, and the TTL means no cleanup job. Rejected: a Postgres `holds` table with an expiry column, which puts the spike back on the database and leaves dead rows to clean up.
3. **Orders are a lifecycle, `held` → `confirmed` or `expired`, under one app-generated UUIDv7.** The ID doubles as the fencing token. A stale browser tab can't confirm under a newer hold. This is the same shape as a Stripe Checkout Session. Rejected: confirm keyed by seat + user, which can't tell holds apart, and a separate hold token resource.
   - *Amended 2026-11-21: confirm grace period.* The lock lives 1 minute longer than the deadline the buyer sees: the API returns `expires_at` = 10 minutes, and the lock expires at 11. Confirm refuses an order once its deadline has passed, even if the lock still exists. A confirm therefore always finishes while its lock has at least a minute left. Without this, a hold could expire between confirm's lock check and its `INSERT`, and another buyer could reserve the seat in that gap. That buyer would hold a seat that was about to be sold, and fail at confirm (and be refunded once payments exist). The rule is enforced on the server, so bots and API callers that ignore the UI countdown follow it too. The cost is that abandoned seats come back 1 minute later.
4. **Reserve checks seat state in Redis, never Postgres.** Sold seats are common during an on-sale, and the seat map needs this state anyway. Rejected: reading Postgres on every reserve (DB load in the spike), and no check at all (buyers would hold and pay for sold seats).
5. **Seat state is a 1-bit sold bitmap plus a sorted set of live holds scored by expiry.** Reads are `GET` plus `ZRANGEBYSCORE (now +inf`, so a seat is available the instant its hold expires, with no job and no stale state. Rejected: a 2-bit free/held/sold bitmap, because TTL expiry can't reset bits and finding stale ones means checking every held seat. Rejected: a sweeper job, which adds lag and a cron.
6. **Seats are addressed by `(event_id, idx)`.** Every Redis key can then be built from the request, with no lookup. Rejected: global seat IDs plus a seat directory in Redis, which adds a key per seat and an extra read per reserve.
7. **No order key in Redis.** The lock stores `order_id|user_id`, and confirm sends the event and seat, which the client already has. That's 2 writes per reserve instead of 3. Rejected: an order key looked up by ID alone.
   - *Amended 2026-11-22: reserve is idempotent per user and seat.* If the seat's lock already belongs to the same user, reserve returns that existing order with its original deadline instead of "seat held". A retry after a lost response, a double click, or a second tab all get the same order. The deadline is never extended, so re-sending reserve can't hold a seat forever. The seat address plus the user acts as a natural idempotency key. Rejected: an `Idempotency-Key` header (Stripe-style), which adds a Redis write per reserve and client-side key handling for a problem the lock already solves. Payments (Epic 6) will need real idempotency keys, because "charge $200" has no natural key.
8. **Fail closed.** If Redis is unreachable, reserve returns 503. A whole Redis node failing is handled by high availability (ElastiCache Multi-AZ failover); a full outage pauses sales safely. Rejected: falling back to Postgres, which would take the full spike and lead to many paid-but-lost refunds.
9. **A service layer** (`booking/service.go`, in the domain package per ADR 10) holds these rules. Handlers fake it in tests.

## Consequences

- \+ Postgres sees only confirms (about the ticket count, spread over the sale), plus rebuilds. Reserve and seat-map traffic stays in Redis.
- \+ No cleanup jobs anywhere: TTLs, score filters, and `EXPIREAT` handle expiry.
- \+ Double-sales are impossible whatever happens in Redis. The worst case is a buyer reaching confirm and getting "seat taken", which means a refund once payments exist.
- − Seat state exists in two places. If Redis is behind Postgres (a lost sold bit), a sold seat can be held, then rejected at confirm. Rebuild-from-Postgres repairs it.
- − Live holds are lost if Redis fails, so those buyers must reserve again.
- − The client must send the event and seat at confirm.
- Lua scripts use Redis's own clock (`TIME`), so the lock and the holds set agree on "now".

**Reopen if:**
- Epic 6 needs a durable record of orders with payment attempts. Then persist an order when payment starts.
- Multi-seat orders arrive. One order then claims several seats atomically, in the same script.
- A per-user cap on active orders is needed against hoarding (planned with the Epic 9 waiting room).
