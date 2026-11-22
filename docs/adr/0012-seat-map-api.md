# 12. Seat map API: a static layout, plus live availability and prices as encoded arrays

Date: 2026-11-22 · Status: Accepted · Epic 2

## Context

The seat map is the most-read thing in an on-sale. ADR 11 estimates about 200k seat-map reads/s for one stadium, against about 33k reserve attempts/s. A stadium has about 80k seats. Seat maps on large ticketing sites show two zoom levels: sections with a price and "N left" when zoomed out, and one dot per seat, available or not, when zoomed in. Held and sold seats look the same to buyers.

The data behind the map changes at three very different rates:

| Data | Changes | Size for 80k seats |
|---|---|---|
| Layout: section, row, number per seat | Almost never (per event) | ~5 MB JSON |
| Prices per seat | Occasionally (admin edits, dynamic pricing) | ~500 KB as a JSON list |
| Availability per seat | Every second during an on-sale | ~10 KB as a bitmap |

Native apps (ADR 8) and the web client code against this contract, so changing it later is expensive.

## Decision

Two endpoints, split by rate of change. Clients join them on the seat's `idx` (ADR 11, decision 6).

```
GET /events/{id}/layout        → { "seats": [ {idx, section, row, number}, … ] }
                                 Cache-Control: long (static), served by the CDN
GET /events/{id}/availability  → { "seats": N,
                                   "unavailable": "<base64 bitmap>",
                                   "prices": [8500, 12000, …],
                                   "seat_price": "<base64, 1 byte per seat>" }
                                 Cache-Control: public, max-age=1
```

```
client ──► CDN ──► booking-api ──► Redis (one script: sold bitmap, live holds, prices)
   │                                 └─ missing? rebuild from Postgres (ADR 11)
   └─ joins layout[idx] + unavailable bit idx + prices[seat_price[idx]]
```

The decisions, and what we rejected:

1. **Layout and availability are separate endpoints.** The layout is loaded once and cached for a long time at the edge; only the small availability response is polled. Rejected: one merged response (`[{idx, section, row, number, price, available}]`), which re-sends ~5 MB of unchanged layout on every poll and can only be cached for 1 s.
2. **The layout is a flat list of seats; clients group it.** Sections and rows are fields on each seat. Each client builds whatever nesting its UI needs (section → seats, section → rows → seats). Rejected: server-side nesting, which bakes one UI's shape into the API. The list is wrapped in an object, so fields like venue geometry can be added without breaking clients.
3. **Availability is a packed bitmap, base64-encoded.** One bit per seat, MSB-first (the same layout as Redis's sold bitmap), set when the seat is sold or held. It's a fixed ~13 KB for 80k seats, however many are sold. Rejected: a list of unavailable `idx` values, which is readable but grows to ~400 KB late in the sale, when traffic peaks. At 200k reads/s that's ~2.6 GB/s of edge traffic for the bitmap versus up to ~80 GB/s for the list.
4. **Prices are per seat, dictionary-encoded, and ride on the availability poll.** Price is dynamic, so it leaves the cached layout. Per-seat is the most granular model; groupings like "update section 104" are bulk writes, not part of the model. Encoding: `prices` lists the distinct prices, and `seat_price` holds one byte per seat, the index into `prices`. A venue has few distinct prices, and neighbouring seats share them, so 80k seats compress to a few hundred bytes. That's small enough to poll every second, so displayed prices are never more than ~1 s stale. Indices are only meaningful within one response. Rejected: a long-cached layout with prices inside it (stale prices, or versioned URLs and ETags to work around that), a bitmap per price (grows with the number of prices, and allows a seat with two prices or none), and a plain list of prices (~500 KB per poll).
5. **The poll is served from Redis only.** Prices are stored already encoded in `booking:prices:{event:N}`, written by the same rebuild-from-Postgres as the sold bitmap (ADR 11), with the same hash tag and TTL. One script returns the bitmap, live holds, and prices in one round trip. The price actually charged is read from Postgres at checkout (Epic 6); the polled price is display only. Rejected: reading prices from Postgres on each poll, which puts Postgres back in the on-sale read path.

## Consequences

- \+ Postgres stays out of the seat-map path. Each CDN edge sends at most ~1 request/s per event to booking-api.
- \+ Fixed, small payloads whatever the sales level: ~13 KB for availability plus a few hundred bytes for prices.
- \+ Price changes reach every buyer within about a second, with no cache invalidation.
- − Clients must decode a bitmap and a byte array (a few lines each) and join three arrays on `idx`. The responses aren't readable with `curl` alone.
- − At most 256 distinct prices per event, because indices are one byte.
- − A price change has two places to update: Postgres, then the Redis prices key. Until a pricing write path exists, only the seed and the rebuild write prices.

**Reopen if:**
- An event needs more than 256 distinct prices: move to 2-byte indices.
- Buyers need to tell held seats from sold ones: availability needs 2 bits per seat, or a second bitmap.
- Seat layouts change after an event is published: version the layout URL.
- Polling becomes the bottleneck: push changes over WebSockets or SSE instead.
