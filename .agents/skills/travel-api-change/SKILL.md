---
name: travel-api-change
description: Implement or fix Travel Planner Go API endpoints, business rules, SQL, or persistence, with focused contract, authorization, and database verification. Excludes frontend changes, documentation-only tasks, and unrelated APIs.
---

# Travel Planner API changes

Use this workflow for the requested backend behavior. Start in this repository;
source paths below are relative to its root. Follow [AGENTS.md](../../../AGENTS.md)
and the relevant sections of the [current backend guide](../../../docs/backend/README.md).
Use the [README](../../../README.md) for architecture, setup, and verification.
For provider work, consult the [places guide](../../../docs/backend/06-places-and-maps.md).
Keep those documents authoritative rather than copying their rules here.

## Trace the contract and durable effects

Identify the owning feature and follow its registered route, handler input,
service/authorizer calls, repository query, scans, and schema constraints.
Search callers and tests of the changed behavior before editing. A service
interface change may also reach fake stores, dependency wiring, and handlers.

Decide which surfaces the requested change actually affects:

- Contract: input/response fields, status codes, pagination, or permissions.
  Update the current guide and affected tests when a contract deliberately
  changes. Record client coordination needed; do not assume access or permission
  to modify the separate web repository.
- Persistence: changed SQL, transactions, locks, or relationships need actual
  PostgreSQL coverage even when no migration is required. Add a migration only
  when the schema changes, following the existing migration procedure.
- Authorization: trace the existing memberships checks and caller-filtered
  queries; cover an authorized caller and a denied/hidden-resource case.
- Providers: use the existing fake HTTP provider tests and preserve the
  documented boundary; do not verify by making live provider requests.

For example, a trip-window fix can affect `internal/trips/service.go`,
`internal/trips/repository.go`, and itinerary writes that read the same window.
Inspect both the service tests and `integration/api_test.go` rather than
assuming a service-only test proves the repository's behavior.

## Verify at the affected boundary

Use the owning package's existing tests for business validation and error paths;
use handler/auth tests for HTTP identity and response behavior; extend existing
`integration` tests for SQL, transaction, or authorization effects. Add the
smallest regression that reproduces the requested behavior and its relevant
failure path.

Run the default completion command from AGENTS.md and its required integration
mode for affected API, authorization, or database behavior. Follow the README
to prepare an already migrated disposable `_test` database. Schema changes
also require up/down verification there. This skill does not implicitly start
databases, load local credentials, apply migrations, reset data, or deploy.
If integration prerequisites are missing, report database coverage as unverified;
default test success is not a substitute.

Finish with contract/durable behavior changes, exact verification evidence, and
any required client coordination or gaps. For substantial work, use the README's
task-plan format. Existing future-feature proposals are not active task plans.
