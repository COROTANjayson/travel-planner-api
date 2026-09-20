# Travel Planner API Agent Guide

## Start here

1. Read `docs/backend/README.md`.
2. Read the numbered guide for the module you are changing.
3. Inspect the current implementation and tests before editing.
4. Implement one guide or one explicitly requested slice at a time.

## Project rules

- Preserve the request flow: router -> handler -> service -> repository -> PostgreSQL.
- Keep feature code in its owning `internal/<feature>` package.
- Keep distinct responsibilities in separate files when present (for example `routes.go`, `middleware.go`, `handler.go`, `service.go`, and `repository.go`); do not add empty or pass-through layers just to match the filenames.
- Reuse `internal/httpx`, `internal/apperror`, configuration, database, and server patterns before adding helpers.
- Keep public resource IDs as positive `BIGINT` values and routes under `/api/v1`.
- Prefer PostgreSQL constraints and transactions for durable invariants.
- Do not add Redis, queues, PostGIS, or another service until the active guide names a concrete requirement for it.
- Do not add artificial-intelligence, language-model, vector-search, or automated-planning features.
- Treat Auth0 as the identity provider. Authentication verifies identity; this API remains responsible for authorization.
- Never return credentials, provider tokens, raw database errors, or private provider payloads.
- Add migrations; never rewrite an applied migration.
- Do not implement work from later guides as speculative scaffolding.

## Completion gate

Run these from the repository root:

```bash
go test ./...
go vet ./...
go build ./cmd/api
```

For a database change, apply the migration to a disposable test database and verify both `up` and `down`. Update the active guide's status only when its completion criteria are met.
