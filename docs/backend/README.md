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
| GET | `/api/v1/invitations` | Pending invitations for the caller's verified email |
| POST | `/api/v1/invitations/{invitationID}/accept` | Accept by ID, HTTP 200 with participant |
| POST | `/api/v1/invitations/{invitationID}/decline` | Decline by ID, HTTP 204 |
| POST | `/api/v1/invitations/accept` | Legacy token acceptance using a verified matching email |
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
| GET | `/api/v1/expense-currencies` | Supported currency codes and decimal precision |
| POST / GET | `/api/v1/trips/{tripID}/expenses` | Create / list planned or covered costs |
| GET / PUT / DELETE | `/api/v1/trips/{tripID}/expenses/{expenseID}` | Read / replace / soft-delete an expense |
| POST / GET | `/api/v1/trips/{tripID}/settlements` | Record / list repayments made outside the app |
| GET / PUT / DELETE | `/api/v1/trips/{tripID}/settlements/{settlementID}` | Read / replace / soft-delete a repayment |
| GET | `/api/v1/trips/{tripID}/expense-ledger` | Activity costs, contributions, loans, receipts, and direct debts |
| POST / GET | `/api/v1/trips/{tripID}/expense_payments` | Record / list business contributions (GET/PUT/DELETE by ID also supported) |
| POST / GET | `/api/v1/trips/{tripID}/expense_loans` | Record / list cash loans (GET/PUT/DELETE by ID also supported) |
| GET | `/api/v1/trips/{tripID}/balances` | Spending totals, balances, suggested repayments, and historical identities |

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

Profile claims are optional in API access tokens. After JWT validation, a token
with `openid` scope and no email claim uses the configured issuer's `/userinfo`
endpoint with that same access token. The returned `sub` must exactly match the
validated subject before any email/verification data is persisted. The request
has a five-second timeout, a 64 KiB response limit, and never follows redirects.
Invalid/mismatched identity returns 401; a profile-provider failure returns a
safe, recoverable 503 without changing the local user.

Profiles are cached for one minute in a bounded 256-entry cache keyed by a hash
of the access token; bearer tokens are never retained in the cache. Tokens without
`openid` retain the existing optional-profile behavior. Unverified emails stay
unverified. After following an account-verification email, sign out and sign in
again for fresh tokens and profile data before accepting an invitation. No Auth0
Management API secret or custom token-claim Action is required for this lookup.

`trips.owner_user_id` is the sole owner record. `trip_members` contains editor,
member, or viewer roles. Reuse the memberships authorizer in feature services.

| Action | Owner | Editor | Member | Viewer |
| --- | --- | --- | --- | --- |
| Read trip, activities, conflicts, participants | Yes | Yes | Yes | Yes |
| Edit trip or create/replace/delete activities | Yes | Yes | No | No |
| Read expenses, repayments, and balances | Yes | Yes | Yes | Yes |
| Plan shared costs / record business payments | Yes | Yes | No | No |
| Record own covered purchases, lending, or received repayments | Yes | Yes | Yes | No |
| Replace/delete financial records | All | All | Own covered costs/lending and received repayments | No |
| Invite, revoke, change roles, remove others | Yes | No | No | No |
| Transfer ownership or delete trip | Yes | No | No | No |
| Leave trip | Transfer first | Yes | Yes | Yes |

Trip-scoped endpoints return 404 to nonparticipants, including lists under a
hidden trip. Establish visibility before returning 403 for insufficient role.
Owner/creator identities come from request context, not JSON. Trips deleted by
owners cascade to their activities, memberships, invitations, expenses, shares,
and settlements through foreign keys. Trip deletion rechecks ownership under a
trip lock. Removing a participant preserves historical authorship and financial
records, but immediately removes their access.

## Memberships and invitations

- Normalize invitation emails by trimming/lowercasing and validate the role.
  Inviting an existing participant returns 409. Creating a replacement revokes
  the prior active invite for that trip/email in the same transaction.
- Tokens contain 32 cryptographically random bytes encoded URL-safely. Persist
  only SHA-256 hashes; return raw tokens once on creation. Expiry is seven days.
  Hashes are unique; a partial unique index permits only one active invitation
  per trip/email, excluding declined records. Email delivery remains future work.
- The recipient inbox matches the authenticated user's normalized verified email,
  including invitations created before signup. Missing/unverified email returns
  403. Only pending, unexpired, unrevoked, undeclined invitations appear, newest
  first. Each summary contains `id`, `trip_id`, `trip_name`, `destination`,
  `start_date`, `end_date`, `role`, `inviter_name`, and `expires_at`; no itinerary
  or recipient email is exposed before joining.
- Accept/decline by positive invitation ID without a request body. Both require
  the verified matching email and recheck terminal state and expiry during the
  write. Unavailable or unrelated IDs return the same generic 404. Accept returns
  the participant; decline returns 204 and grants no membership. Owner history
  includes nullable `declined_at`, and a fresh invite can follow a decline.
- Legacy acceptance still uses `{"token":"..."}` in the POST body, never an API
  URL query, sharing the same acceptance transaction and verified-email checks.
- Accepted invites replay only for the same user with an existing membership.
  Unaccepted invites must be unexpired, unrevoked, and undeclined; invalid tokens
  return a generic 400 without disclosing which condition failed.
- Migration 00009 adds decline history. Its rollback converts declined records
  to revoked before restoring the older active-invitation uniqueness rule.
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
  POST/PUT never accepts creator or audit fields. PUT replaces the original
  fields; omitted or null category/checklist additions preserve saved values.
- No separate day or manual-order API exists. Days derive from start instants
  in the trip zone. Activity lists optionally accept an ordered pair of
  `start_from`/`start_before` UTC instants with an exclusive upper bound.

Example activity input:

```json
{"title":"Breakfast","starts_at":"2026-10-01T01:00:00Z","ends_at":"2026-10-01T02:00:00Z","notes":"Near the hotel","place_id":null,"category":"Eating","checklist":[{"text":"Reserve a table","done":true}]}
```

Activities expose a `category` string and ordered `checklist` array of
`{text, done}` items through existing create, update, and read routes. Presets
are Rest, Sleeping, Eating, Travel, and Activity; custom per-entry labels are
also accepted, without a category library. Categories and item text are trimmed
and must contain 1–100 and 1–200 UTF-8 bytes respectively; lists allow at most
50 items. Duplicate item text is allowed. Items have no independent time/place.

Missing/null additions on creation default to `Activity` and `[]`; existing
rows receive those defaults in migration `00008`. Missing/null additions on
PUT preserve saved values atomically, while explicit `[]` clears a checklist.
Responses always contain a category string and checklist array. Completion is
shared and follows existing owner/editor write permissions. Checklist edits
save with the whole form; concurrent edits remain last-write-wins.

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

## Expenses and repayments

Money is signed 64-bit integer minor units in PostgreSQL/Go and **decimal integer
strings in JSON**, including balances and transfer suggestions. Never convert
money to JavaScript numbers or combine currencies. `/expense-currencies` returns
`[{"code":"PHP","decimals":2}, ...]`. Supported codes are PHP, USD, EUR, GBP,
JPY, SGD, THB, VND, IDR, MYR, AUD, CAD, CNY, HKD, KRW, TWD, CHF. JPY/VND/KRW
have zero decimal places; the remaining supported codes have two. Metadata uses
[SIX ISO 4217 List One](https://www.six-group.com/en/products-services/financial-information/market-reference-data/data-standards.html).

An expense creation body is:

```json
{"description":"Hotel","currency":"PHP","amount_minor":"101","incurred_on":"2026-09-01","payer_user_id":1,"split_mode":"equal","shares":[{"user_id":1},{"user_id":2}]}
```

`equal` divides across unique selected users, assigning leftover minor units by
ascending user ID. Omit share amounts for equal input. `exact` requires each
share's `amount_minor` string; nonnegative shares must total the positive expense.
Zero shares are retained. Select 1–1000 users; descriptions contain 1–200 bytes.
Dates use `YYYY-MM-DD` (years 0001–9999), independently of trip dates.

Repayment bodies contain `currency`, positive `amount_minor`, `paid_on`, and
distinct `from_user_id` / `to_user_id`. They only record money already sent;
legacy unallocated overpayments can reverse balances; linked repayments cannot
exceed the selected debt. There is no payment provider.

Both resource responses include `id`, `trip_id`, server-derived
`created_by_user_id`, `version`, UTC audit timestamps, and `people` identities
(`user_id`, `display_name`, nullable `email`). Expenses additionally return exact
shares and their split mode. Creator/payer identity never grants rights beyond
the caller's current role. Members manage their own covered purchases/lending
and repayments they received; owners/editors manage all records.

Creation requires an `Idempotency-Key` of 1–128 bytes without surrounding
whitespace, scoped by trip, creator, and resource type. Canonical normalized
requests are hashed. New creation returns 201/Location; matching retries return
200 with the current representation of the same resource. Changed payloads or
deleted resources return 409. Keys/hashes remain reserved on soft-deleted rows.
Do not generate a new key to retry an uncertain save.

Replacement requires the previously read positive `version` in the body;
deletion requires `?version=N`. Both increment the version. Stale mutations
return 409; missing/invalid versions return 400. Deletion returns 204 and removes
the record from calculations, while retaining its internal tombstone.

New expense parties must be current participants. Replacements may retain their
existing former payer/sharers. Repayments accept current participants or users
referenced in this trip's financial history, including soft-deleted records.
Remaining authorized travelers can record former travelers' repayments.

Ledger writes lock the trip and current membership rows, then recheck roles,
participants, versions, and aggregate limits in the transaction. Expenses and
shares commit together; failures or overflow roll back. Membership deletion and
role updates wait on those membership locks; ownership transfer and trip deletion
serialize through the trip lock.

Lists use shared `limit`/`offset`, optional `currency`, and date descending then ID
descending order. `/balances` uses the entire live ledger in a consistent read
snapshot. It returns `people` and `currencies`, each with `currency`,
`total_spent_minor`, `balances` (`user_id`, signed `amount_minor`) and
`suggested_transfers` (`from_user_id`, `to_user_id`, positive `amount_minor`).
Positive balances are receivables, negative balances debts. Repayments add to the
sender and subtract from the recipient. Each currency remains zero-sum.
Suggestions greedily match debtors/creditors by ascending user ID; they can
simplify indirect debts and never create ledger entries.

### Activity costs, contributions, and loans

Migration `00011` separates planned costs from business payments. Existing
expenses are backfilled as fully paid purchases with their exact saved shares;
old repayments retain their original movement and remain unallocated. Their
balances stay unchanged. New planned costs provide `activity_id`, description,
currency, total `amount_minor`, `incurred_on`, `amount_basis` (`group` or
`per_person`), split mode and shares, and omit or zero `payer_user_id`.
Per-person costs require equal, whole minor-unit shares. Responses additionally
include `activity_id`, `activity_name`, and `amount_basis`. JSON money remains
integer strings. All selected activities must belong to the same trip.

`GET /trips/{tripID}/expense-ledger` returns a consistent snapshot containing
`expenses`, `payments`, `loans`, `settlements`, `reversed_settlements`, historical `people`, and direct
`debts`. Each debt identifies its `payment_id` or `loan_id`, activity, description,
currency, borrower (`from_user_id`), creditor (`to_user_id`), and outstanding
`amount_minor`. Zero debts remain available for receipt history. Negative
amounts after corrections identify credits requiring review.

`/trips/{tripID}/expense_payments` and `/expense_loans` support the same
POST/GET and GET/PUT/DELETE-by-ID conventions, pagination, version checks,
idempotency headers, and soft deletion as existing resources. Payment bodies
contain `expense_id`, `payer_user_id`, currency, positive amount, `paid_on`, and
optional `allocations` (`user_id`, `amount_minor`). Without allocations the
server covers the payer first and apportions the rest proportionally, with
rounding remainder assigned in ascending user ID order. Coverage cannot exceed
uncovered shares and must exactly equal the payment. Contributions remain
unchanged when cost shares change; paid-cost currencies cannot change.

Loan bodies contain activity, description, currency, amount, `paid_on`, lender
`from_user_id`, and borrower `to_user_id`. Loans add debt but no purchase spending.
A covered purchase uses the expense endpoint with the current member as payer;
the cost and full contribution commit together. Legacy fully paid purchase
bodies remain supported for existing clients. Members cannot record another
person as payer/lender. Owners/editors manage shared planning and business
payments; viewers only read.

Repayments add exactly one `payment_id` or `loan_id`, match its currency and
people, and cannot exceed that source's outstanding debt. Only its recipient
may record a new linked repayment. Owners/editors may correct existing receipts.
There is no submission queue.
An editor may associate an existing unallocated repayment through versioned PUT
without duplicating the movement. Unallocated legacy repayments permit the old
trip-level overpayment behavior; new linked repayments do not. Former travelers
remain valid recipients/borrowers for retained debts.

Paid costs and sources with repayments cannot be deleted until related records
are reversed. Correct business payments and receipts by reversing them and
recording the correction. Activity deletion clears the live activity ID while
preserving its last title and all financial records. Membership changes never
recalculate historical shares. `/balances.total_spent_minor` now sums business
payments; a planned unpaid cost creates no member debt or spending. Legacy
repayment movements and simplified trip balances remain supported.

Migration `00012` adds optional `planned_coverer_user_id` and
`planned_cover_amount_minor` to cost input/output. Supply both or neither. The
coverer belongs to the trip and the amount is positive and no greater than the
cost total. Planning never creates a payment or debt.

Only the recipient may create a linked repayment receipt, even when another
actor is owner/editor. Recipients and owners/editors may update/reverse an existing
receipt; PUT and DELETE require `correction_reason` (1–500 characters; DELETE uses
a query parameter). Linked source, currency and people are immutable. Existing
unallocated historical receipts may still be associated by editors; newly created
unallocated receipts cannot be used by a different editor to bypass recipient
confirmation.

Receipt creation, correction, association and reversal append actor, timestamp,
reason and before/after snapshots transactionally. `GET
/trips/{tripID}/settlements/{recordID}/history` is participant-readable, includes
reversed receipts, and returns an ordered array with `id`, `actor_user_id`, `action`,
`reason`, `before`, `after`, and `created_at`. Historical receipts have no invented
audit events. Receipt audit entries follow the trip's normal deletion lifecycle.

The web presents activity-based cost setup and payment tracking. New loan and
covered-food UI entry are deferred; existing records and API compatibility remain.
No payment provider, receipt attachments, percentage splits, guests, or exchange
rates are implemented.

### Activity expense planning and payment screens

Migration `00013_activity_plan_requests.sql` adds request replay for activity
saves. Activity POST/PUT accept optional `expense_changes`: expense input fields
plus `id` and `version` for an existing cost. New costs omit both. Changes require
an `Idempotency-Key` (maximum 128 bytes). The activity and all costs commit in one
transaction; invalid shares, stale cost versions, or forbidden participants roll
everything back. Retry the same key/payload for the same saved result. A changed
payload or removed saved activity with that key returns 409. Costs in a batch
must belong to that activity; planning cannot create business payments.

`GET /trips/{tripID}/expenses/{recordID}/payment-status` returns the cost,
business paid/remaining totals, historical people, payments, receipts, and
per-member share, paid, remaining business amount, reimbursements, receivables
and credits. All trip participants can read this summary.

Editors may POST a normal business-payment input to the same cost's
`/payment-preview` endpoint. It returns allocations, own covered amount,
reimbursements and resulting payment status without writing a payment. Optional
`member_user_id` pins the payment to that member's remaining business share; it
must equal the payer, and the amount cannot spill into anyone else's share.
Omit it for a reservation/advance with own-share-first proportional allocation.

Editors may POST an expense input with its current version to `/cost-preview`
for before/after payment statuses. Recorded payments are preserved in both the
preview and actual save. Excess coverage becomes a credit requiring correction;
this preview creates no record. Both preview endpoints enforce the same current
roles, participant, currency, amount and version rules as the corresponding write.

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
code before implementation. Groups, templates, realtime collaboration,
offline sync, and notifications remain future work. Local checks/CI exist;
production deployment, readiness, observability, and recovery remain proposals.

Optional copies under `docs/plans/` and `docs/archive/` are ignored local notes
and are absent from fresh checkouts. They are not current implementation rules
or authorization to build features.
