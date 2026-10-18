# Places and maps

Current behavior for the single-instance demo. See the
[current backend guide](README.md) for shared HTTP/auth rules and scheduling.
Group meetings and travel-time estimates are not implemented.

## Provider boundary

`internal/places` owns the provider interface, Nominatim adapter, normalization,
repository, and service. Production wiring uses a fixed server-side Nominatim
endpoint, identifying User-Agent, and a five-second HTTP timeout. Clients cannot
supply provider URLs. Convert vendor data immediately into bounded internal
models; do not return/store raw provider payloads.

Search is **explicit submit only**, never autocomplete. The current public
provider arrangement assumes one API process and modest human demo traffic.
The client uses MapLibre with OpenFreeMap tiles; no provider key is required.
Revalidate provider usage policies before changing traffic patterns or deployment.

## Routes and destination flow

All routes require authentication:

| Method | Route | Behavior |
| --- | --- | --- |
| GET | `/api/v1/places/search?q=...` | Normalized candidates, at most 10 |
| GET | `/api/v1/places/search?q=...&scope=destination` | City/country candidates only |
| POST | `/api/v1/places/resolve` | Refresh/upsert a stable OSM reference, returning 200 |
| GET | `/api/v1/places/{placeID}` | Read a saved normalized place |

Resolve accepts `{"provider_place_id":"N123"}`; N/W/R references must use a
positive OSM ID. Select a destination candidate, resolve it, then send its local
`id` as `destination_place_id` in a trip write. The service validates city/country
kind and derives the trip's destination label and IANA time zone.

Activity writes accept nullable `place_id`. Reads include saved `place` data
from PostgreSQL without provider calls. Deleting a place clears activity/trip
references via `ON DELETE SET NULL`; activity titles/notes and trip destination
snapshots survive. No same-trip ownership is implied for globally saved places.

## Storage, validation, and limits

- `places` uses generated BIGINT IDs and unique `(provider, provider_place_id)`;
  the provider is currently `osm`. Snapshots include kind, name/address,
  coordinates, IANA zone, refresh time, and audit timestamps. Schema constraints
  and service validation protect coordinates and required/length-bounded fields.
- Zones derive from coordinates using the embedded tzf dataset, not browser
  location. Reject invalid/unknown zones and malformed provider data safely.
- Trim queries and require 2-200 bytes. Cache normalized results in PostgreSQL
  for 24 hours with destination searches separated from ordinary searches.
- The service counts at most 10 searches per user per minute, including cache
  hits. Counters are process-local; they are not a distributed quota.
- Nominatim search/lookup calls share a process-local one-request-per-second
  throttle. Excess queued work returns 429 rather than waiting indefinitely.
- Provider failures/timeouts/malformed responses return safe 503 responses;
  throttling returns 429. Preserve request cancellation and bounded payload reads.
- Explicit resolve refreshes the snapshot; normal saved-place reads continue
  during provider outages. No Redis, spatial extension, or routes cache exists.

## Verification and future work

Use fake HTTP providers for success, empty responses, timeout, quota, malformed
payloads, and destination classification. Service tests cover query validation,
cache behavior, coordinate/zone normalization, and stable references. PostgreSQL
integration tests cover resolved destinations and activity attachments. Use the
[README's integration command](../../README.md#verification-and-migrations).

Before multiple API replicas or higher traffic, revisit the provider arrangement
and shared throttling. Route estimates, meeting-place references, PostGIS, and
another cache require an actual feature/scale need; they are not scaffolding
requirements for ordinary changes.

The original deferred estimate sketch proposed a PostgreSQL cache keyed by
origin/destination place IDs, travel mode, and departure bucket, storing distance
meters, duration seconds, provider timestamp, and expiry. Preserve uniqueness
over that input tuple if a client later justifies this feature; no estimate
endpoint or table currently exists.
