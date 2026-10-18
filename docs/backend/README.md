# Current backend guide

This describes implemented behavior. For setup, architecture, migrations, and
commands, use the [project README](../../README.md). Models, handlers, services,
SQL constraints, and tests are the authoritative executable contracts; update
this guide alongside intentional contract changes. No OpenAPI generator exists.

## HTTP contracts

All feature routes are under `/api/v1` and require an Auth0 bearer token;
`/health` is public. Strict decoding rejects unknown fields, multiple JSON
values, and bodies over 1 MiB. Resource IDs are positive BIGINT integers.

| Method | Path | Result |
| --- | --- | --- |
| GET | `/health` | Server status, HTTP 200 |
| GET | `/api/v1/me` | Current local user, HTTP 200 |
| POST | `/api/v1/trips` | Create an owned trip, HTTP 201 |
| GET | `/api/v1/trips` | List trips the caller owns or has joined, HTTP 200 |
| GET / PUT / DELETE | `/api/v1/trips/{tripID}` | Read / replace / delete according to the caller's role |
| GET | `/api/v1/trips/{tripID}/members` | List all participants, including the owner |
| POST / GET | `/api/v1/trips/{tripID}/invitations` | Create or list invitations as owner |
| DELETE | `/api/v1/trips/{tripID}/invitations/{invitationID}` | Revoke an invitation as owner |
| POST | `/api/v1/invitations/accept` | Accept an invitation using a verified matching email |
| PUT / DELETE | `/api/v1/trips/{tripID}/members/{userID}` | Change a role or remove/leave a trip |
| POST | `/api/v1/trips/{tripID}/transfer-ownership` | Transfer ownership to an existing member |
| POST | `/api/v1/trips/{tripID}/activities` | Create scheduled activity, HTTP 201 |
| GET | `/api/v1/trips/{tripID}/activities` | List activities in schedule order |
| GET | `/api/v1/trips/{tripID}/activities/conflicts` | List advisory overlapping activity pairs as any participant |
| GET | `/api/v1/trips/{tripID}/activities/overlaps?starts_at=...&ends_at=...&exclude_id=...` | Check a proposed interval against up to 10 saved activities; `exclude_id` is optional |
| GET / PUT / DELETE | `/api/v1/trips/{tripID}/activities/{activityID}` | Read / replace / delete an activity within that trip |
| GET | `/api/v1/places/search?q=...` | Submit-only OSM place search, at most 10 results; use `scope=destination` for cities and countries |
| POST | `/api/v1/places/resolve` | Refresh and save an OSM object reference such as `{"provider_place_id":"N123"}` |
| GET | `/api/v1/places/{placeID}` | Read a normalized saved place |

Successful reads/replacements and place resolution return 200; trip, activity,
and invitation creation return 201 with `Location`; deletion returns 204 without
a body. Errors use `{"error":"message"}`:
400 invalid input, 401 missing/invalid authentication, 403 insufficient role
for an accessible trip, 404 missing/hidden resource, 409 conflicting state,
429 place rate limit, 503 unavailable provider, and safe 500 for unexpected
errors. Do not expose SQL errors, tokens, or provider payloads.

Lists return arrays, including `[]`. Trip, activity, and conflict lists use
`limit`/`offset`; member and invitation
lists currently return all matching records without pagination. Shared pagination
defaults to 50 with a 1-100 limit and nonnegative offset. Trips sort by newest
ID; activities by start instant then ID; conflicts by overlap start then IDs.
Authorization filtering precedes pagination.

## Identity and authorization

Auth0 owns passwords, signup, verification, and recovery. The API verifies token
signature, issuer, audience, expiry, and a nonempty bounded subject with the
maintained Auth0 middleware. A subject maps uniquely to one local user; email
is profile data, never the identity key. `/me` returns ID, nullable email,
`email_verified`, display name, and audit timestamps, never `auth_subject`.
Tokens and passwords are never stored or logged.

`trips.owner_user_id` is the sole owner record. `trip_members` contains editor,
member, or viewer roles. Reuse the memberships authorizer in feature services.

| Action | Owner | Editor | Member | Viewer |
| --- | --- | --- | --- | --- |
| Read trip, activities, conflicts, participants | Yes | Yes | Yes | Yes |
| Edit trip or create/replace/delete activities | Yes | Yes | No | No |
| Invite, revoke, change roles, remove others | Yes | No | No | No |
| Transfer ownership or delete trip | Yes | No | No | No |
| Leave trip | Transfer first | Yes | Yes | Yes |

Trip-scoped endpoints return 404 to nonparticipants, including lists under a
hidden trip. Establish visibility before returning 403 for insufficient role.
Owner/creator identities come from request context, not JSON. Trips deleted by
owners cascade to their activities, memberships, and invitations through foreign
keys. Removing a participant preserves historical activity authorship.

## Memberships and invitations

- Normalize invitation emails by trimming/lowercasing and validate the role.
  Inviting an existing participant returns 409. Creating a replacement revokes
  the prior active invite for that trip/email in the same transaction.
- Tokens contain 32 cryptographically random bytes encoded URL-safely. Persist
  only SHA-256 hashes; return raw tokens once on creation. Expiry is seven days.
  Hashes are unique; a partial unique index permits only one active invitation
  per trip/email. Email delivery remains future work.
- Accept uses `{"token":"..."}` in the POST body, never an API URL query.
  The authenticated user's verified email must match the invitation.
- Accepted invites replay only for the same user with an existing membership.
  Unaccepted invites must be unexpired and unrevoked; invalid tokens return a
  generic 400 without disclosing which condition failed.
- Transfer uses `{"user_id":123}` and requires an existing member. One
  transaction updates the trip owner, removes the new owner's member row, and
  makes the previous owner an editor. Owners must transfer before leaving.
- Keep state changes and durable uniqueness checks transactional, including
  concurrent invitations, acceptance, and ownership changes.

## Trips and scheduling

Trip writes require `name` (trimmed, at most 200 bytes) and a resolved
`destination_place_id` for a city/country. The service derives destination and
IANA zone from the place. Inclusive `start_date`/`end_date` use `YYYY-MM-DD`;
same-day trips are valid. `starts_at`/`ends_at` are optional as a pair of ordered
RFC3339 instants with an explicit offset or Z. If exact times and dates are both
supplied, their local dates must match. Timestamp-only requests remain supported;
a date-only replacement with null/omitted times clears precise restrictions.

Example date-only trip input (resolve a real local destination ID first):

```json
{"name":"Cebu weekend","destination_place_id":123,"start_date":"2026-10-01","end_date":"2026-10-03"}
```

Activities require a trimmed title up to 200 bytes and ordered, nonzero exact
start/end instants; notes default to empty and allow up to 10,000 bytes. Nullable
`place_id` attaches an existing normalized place. The API derives each activity's
zone from the current trip; client `time_zone` does not override it. Instants
are persisted as TIMESTAMPTZ and returned in UTC with IANA zone metadata.

- For date-only trips, both activity endpoint dates in the trip zone must fall
  within the trip dates. Midnight crossing is valid when both endpoints fit.
- For precise trips, the complete activity interval must fit inside the exact
  trip interval. Valid parallel activities remain allowed.
- A trip edit that excludes an existing activity returns 409 transactionally.
  Changing the trip zone updates saved activity zone metadata while preserving
  their UTC instants; date-only boundaries are checked in the new trip zone.
- Activity responses include immutable `created_by_user_id`, derived from the
  authenticated creator and retained through edits, member removal, and transfer.
  POST/PUT never accepts creator or audit fields. PUT remains full replacement.
- No separate day or manual-order API exists. Days derive from start instants
  in the trip zone. Activity lists optionally accept an ordered pair of
  `start_from`/`start_before` UTC instants with an exclusive upper bound.

Example activity input:

```json
{"title":"Breakfast","starts_at":"2026-10-01T01:00:00Z","ends_at":"2026-10-01T02:00:00Z","notes":"Near the hotel","place_id":null}
```

## Advisory conflicts

Overlaps never block saving. Touching endpoints do not overlap. Conflict output
returns each pair once with ascending IDs and UTC overlap endpoints:

```json
[{"activity_ids":[1,2],"overlap_starts_at":"2026-10-01T01:30:00Z","overlap_ends_at":"2026-10-01T02:00:00Z"}]
```

The proposed-interval `overlaps` endpoint returns at most 10 saved activities,
with optional `exclude_id` for editing. Register static conflict/overlap routes
before `/{activityID}`. There is no group-specific filtering or automatic
rescheduling. Concurrent edits remain last-write-wins; ETags and version
preconditions are proposals, not current requirements.

## Focused verification

Use service tests for validation, permission rules, and interval calculations;
HTTP/auth tests for safe responses, strict decoding, and token validation;
PostgreSQL integration tests for durable behavior and concurrency. See the
README for the explicit integration mode and disposable-database requirements.

Preserve coverage for two-user isolation and role matrices; invitation expiry,
revocation, verified email, idempotency and transfer; CRUD/cascades; date-only
and precise windows; DST, midnight and zone changes; creator retention;
conflict ordering, touching endpoints, pagination and parent-trip isolation.
Use fake HTTP providers for [places](06-places-and-maps.md), not live services.

## Future proposals and history

Track shared proposals in GitHub Issues and revalidate them against current
code before implementation. Groups, templates, expenses, realtime collaboration,
offline sync, and notifications remain future work. Local checks/CI exist;
production deployment, readiness, observability, and recovery remain proposals.

Optional copies under `docs/plans/` and `docs/archive/` are ignored local notes
and are absent from fresh checkouts. They are not current implementation rules
or authorization to build features.
