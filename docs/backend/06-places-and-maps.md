# 06 - Places and Maps

## Goal and prerequisites

Normalize OpenStreetMap place references, attach them to itinerary records, and provide bounded place search for a small, single-instance demo. Complete [itinerary](04-itinerary.md). Group meeting references and travel estimates are deferred until their clients exist.

## Schema

Add `places`:

| Column | Rule |
| --- | --- |
| `id` | Generated `BIGINT` primary key |
| `provider` | Checked to `osm` initially |
| `provider_place_id` | Non-empty; unique with provider |
| `name`, `address` | Normalized display snapshots |
| `latitude`, `longitude` | Double precision with valid range checks |
| `time_zone` | Non-empty IANA name derived from coordinates with the embedded tzf dataset |
| `refreshed_at` | Provider refresh time |
| timestamps | Creation/update audit fields |

Add nullable `place_id` to `itinerary_items` with `ON DELETE SET NULL`. Activity titles and notes remain display snapshots when a provider record changes or is deleted. Add `meeting_place_id` only after groups exist.

Cache normalized search results in PostgreSQL for 24 hours; do not persist raw provider payloads.

Add `route_estimates` only when the estimate endpoint is implemented: origin/destination place IDs, travel mode, departure bucket, distance meters, duration seconds, provider timestamp, and expiry. Uniqueness covers the input tuple.

PostGIS is not required for this module. Add it later only when the backend must execute radius, containment, or clustering queries at measured scale.

## Configuration and provider boundary

Use OpenFreeMap tiles with MapLibre in the browser and Nominatim search/lookup through the Go server. No provider key is required. Identify the app with a User-Agent, keep the provider URL fixed server-side, and use an HTTP client with a five-second timeout. Public Nominatim permits at most one request per second for the whole app and forbids autocomplete; this release assumes one API instance and modest human traffic.

Define a small provider interface inside `internal/places` for search and lookup. Convert provider responses immediately into internal models. Do not expose or persist full provider payloads.

## Routes

| Method | Path | Permission |
| --- | --- | --- |
| `GET` | `/api/v1/places/search?q=...` | Authenticated, rate-limited |
| `POST` | `/api/v1/places/resolve` | Authenticated |
| `GET` | `/api/v1/places/{placeID}` | Authenticated |

Resolve accepts a stable OSM object reference such as `N123`, refreshes provider details, upserts the normalized place, and returns it. Activity POST/PUT accepts nullable `place_id`; reads include the saved place data from PostgreSQL. Route estimates remain deferred.

## Validation and caching

- Trim search text, require 2-200 bytes, and cap returned results at 10. Search only on explicit submit.
- Allow at most 10 searches per user per minute and one outbound Nominatim request per second per API instance.
- Validate coordinates and IANA zones before persistence.
- Refresh place details on explicit resolve. Saved activity reads do not depend on provider availability.
- Provider timeout, quota, and malformed-response failures map to a safe `503` without leaking keys or payloads.
- Never accept arbitrary provider URLs from clients.

## Implementation steps

1. Add place migrations, constraints, and attachment columns.
2. Add a provider HTTP client with an explicit timeout and application-wide outbound throttle.
3. Implement `internal/places` provider adapter, repository, service, and handlers.
4. Implement normalized upsert by `(provider, provider_place_id)`.
5. Extend itinerary/group inputs with optional local `place_id` values and same-trip authorization.
6. Add route estimates and their PostgreSQL cache only when a client needs them.
7. Add per-user and provider rate limits at the API edge.

## Tests

- Use a fake provider server for successful, empty, timeout, quota, and malformed responses.
- Test normalization, deduplication, stale refresh, coordinate/zone validation, and cache expiry.
- Verify inaccessible trips cannot be used to request estimates.
- Verify deleting a normalized place preserves activities/groups and clears only their reference.

## Completion criteria

- API contracts contain normalized place data, not vendor response objects.
- Provider calls have timeouts, safe errors, rate limits, and bounded caching.
- Activities and group meetings can reference places without depending on provider availability for normal reads.
- No spatial extension or separate cache is added without a demonstrated query need.
