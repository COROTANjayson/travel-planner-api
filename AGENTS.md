# Travel Planner API

## Start here

- [README](README.md): setup, architecture, migrations, and check commands.
- [Current backend guide](docs/backend/README.md): contracts, authorization,
  invitations, scheduling, and focused tests.
- [Places](docs/backend/06-places-and-maps.md): provider boundary and limits.
- [travel-api-change](.agents/skills/travel-api-change/SKILL.md): reusable workflow
  for endpoint, business-rule, SQL, and persistence changes and their checks.
- Inspect the affected implementation, callers, migrations, and tests before
  editing. This is an independent Go/Git project; no web checkout is required.

## Working rules

- Keep router -> handler -> service -> repository -> PostgreSQL. Feature code
  belongs in its owning `internal/<feature>` package; services use small existing
  repository/authorizer interfaces. Do not add empty or pass-through layers.
- Reuse `httpx`, `apperror`, config, database, and server patterns. Keep HTTP
  decoding in handlers, business validation in services, and SQL in repositories.
- Auth0 establishes identity; reuse memberships authorization for trip access.
  Filter lists before pagination and preserve hidden-resource 404 behavior.
  Never trust owner, creator, or role fields supplied by clients.
- Keep `/api/v1` contracts, positive BIGINT resource IDs, JSON errors, strict
  input decoding, UTC instants, and the documented trip-zone/window behavior.
  Coordinate requested contract changes with clients; no OpenAPI generator exists.
- Preserve PostgreSQL constraints, transactions, and historical authorship.
  Add migrations; never rewrite an applied one. Never reset development or
  production data to make a test pass.
- Never return or log credentials, invitation/access tokens, connection URLs,
  raw database errors, or private provider payloads. Leave local env files alone.
- Preserve provider timeouts, submit-only search, throttling, and normalized
  snapshots. Tests use fake providers rather than live services.
- Implement only the requested slice. Shared plans belong in GitHub Issues.
  Optional `docs/plans` and `docs/archive` are ignored local notes; only
  explicitly authorized work becomes active.
  Do not add AI, queues, Redis, PostGIS, or other infrastructure speculatively.
- Update existing authoritative documentation when behavior changes; do not
  create another competing guide.

## Completion

Run `bash scripts/check.sh` for code/configuration/script changes. For database,
repository SQL, authorization, or API behavior changes, also migrate a disposable
`_test` database and run `bash scripts/check.sh --integration` with exported
`TEST_DATABASE_URL`. See the README for setup; checks never apply migrations.

Schema changes also need up/down verification on a disposable database. Add a
focused regression test for changed behavior using existing tests as examples.
Documentation-only changes need link and accuracy review, not a Go build.
Review the diff and report exact checks run, skipped database coverage, and any
remaining risks. A green default check is not proof of database behavior.
