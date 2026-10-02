# Backend Implementation Roadmap

These guides turn the current Go API into the non-automated backend described by the product architecture. They are implementation specifications, not aspirational technology lists.

## How to use the guides

1. Work in numeric order unless a guide explicitly says otherwise.
2. Read the guide's current-state and prerequisite sections.
3. Implement one coherent step, including its migration and tests.
4. Run the repository completion gate before moving to another module.
5. Mark a guide complete only when every item in its completion criteria is true.

Do not install infrastructure merely because a later guide mentions it. PostgreSQL remains the default durable store. Each guide states when another dependency becomes justified.

## Roadmap

| Order | Guide | Status | Depends on |
| --- | --- | --- | --- |
| 00 | [Backend architecture](00-backend-architecture.md) | Current baseline documented | Existing API |
| 01 | [Authentication](01-authentication.md) | Planned | 00 |
| 02 | [Trips](02-trips.md) | Complete | 01 |
| 03 | [Memberships and invitations](03-memberships-and-invitations.md) | Complete | 01, 02 |
| 04 | [Itinerary](04-itinerary.md) | Complete | 02, 03 |
| 05 | [Groups](05-groups.md) | Planned | 03, 04 |
| 06 | [Places and maps](06-places-and-maps.md) | Places and map implemented; estimates deferred | 04 |
| 07 | [Templates](07-templates.md) | Planned | 03, 04, 06 |
| 08 | [Expenses](08-expenses.md) | Planned | 03 |
| 09 | [Realtime collaboration](09-realtime-collaboration.md) | Planned | 03, 04, 05, 08 |
| 10 | [Offline sync](10-offline-sync.md) | Planned | 09 |
| 11 | [Notifications and jobs](11-notifications-and-jobs.md) | Planned | 01, 03 |
| 12 | [Operations](12-operations.md) | Planned throughout; production gate last | All shipped modules |

## Shared definitions

- **Owner:** the user referenced by `trips.owner_user_id`.
- **Editor:** may change shared trip content but cannot transfer or delete the trip.
- **Member:** may read the trip and contribute only where a feature explicitly permits it.
- **Viewer:** read-only access.
- **Public route:** requires no access token.
- **Protected route:** requires a valid access token and an application user.
- **Trip-scoped route:** additionally requires the role documented by that module.

All resource IDs remain positive integers. Timestamps are RFC3339 in API payloads and `TIMESTAMPTZ` in PostgreSQL. Scheduled activities preserve an IANA time-zone name in addition to UTC instants.

## Standard HTTP behavior

| Situation | Status |
| --- | --- |
| Successful read or replacement | `200` |
| Successful creation | `201` with `Location` |
| Successful deletion | `204` |
| Invalid JSON, ID, query, or domain input | `400` |
| Missing or invalid authentication | `401` |
| Authenticated but insufficient permission | `403` |
| Resource absent or hidden from the caller | `404` |
| Duplicate, stale version, or invalid state transition | `409` |
| Replay cursor is older than retained events | `410` |
| Version precondition is missing | `428` |
| Unexpected failure | `500` without internal details |

Errors retain the existing shape:

```json
{"error":"message"}
```

Lists return `[]` when empty and use `limit`/`offset` until a measured need justifies cursor pagination.
