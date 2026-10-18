# Travel Planner API

Independent Go API using Chi, pgx, PostgreSQL, Goose, and Auth0. No web checkout
is required to build or test it. Current contracts and domain rules are in the
[backend guide](docs/backend/README.md); provider rules are in the
[places guide](docs/backend/06-places-and-maps.md).

## Run locally

Use the Go version declared in `go.mod`, PostgreSQL 18, and Goose **v3.28.0**.
Install the migration CLI if needed:

```bash
go install github.com/pressly/goose/v3/cmd/goose@v3.28.0
go mod download
```

The existing local database launchers target **Git Bash on Windows** and call
PowerShell with PostgreSQL tools at `C:\Program Files\PostgreSQL\18\bin`.
If PostgreSQL is elsewhere, update that explicit path in `scripts/local-db.ps1`.
Run all commands from this repository's root. On PowerShell, use the installed
Git Bash explicitly if `bash` resolves to WSL.

For a fresh checkout with neither `.env` nor a project database:

```bash
bash scripts/local-db.sh setup
```

This initializes ignored `.local/postgres/`, creates development and `_test`
databases on `127.0.0.1:55432`, and saves generated local connection settings in
ignored `.env`. It refuses to overwrite an existing `.env` or silently recreate
an initialized database. Add `AUTH0_ISSUER_URL` and `AUTH0_AUDIENCE` in your editor.
Do not copy `.env.example` over the generated connection settings.

Alternatively, use your own PostgreSQL instance: create separate development
and `_test` databases, copy [.env.example](.env.example) to `.env` only when none
exists, and edit the placeholders. Apply development migrations explicitly and
run `go run ./cmd/api`; do not use the project database launcher for that instance.

Create an Auth0 API using RS256. Configure its issuer and audience, then start
the project-managed instance, apply development migrations, and run the API:

```bash
bash scripts/run.sh
```

The script sources the local `.env` for Goose; treat that file as trusted local
shell configuration. The API itself reads dotenv without executing shell code.
Existing process environment variables take precedence in the API. Missing
`.env` is allowed when required settings are exported; invalid syntax fails
without printing its contents.

| Setting | Behavior |
| --- | --- |
| `PORT` | Defaults to 8080; integer from 1 to 65535 |
| `DATABASE_URL` | Required PostgreSQL URL; checked at startup |
| `AUTH0_ISSUER_URL` | Required HTTPS tenant URL with trailing slash |
| `AUTH0_AUDIENCE` | Required Auth0 API identifier; must match client audience |
| `TEST_DATABASE_URL` | Integration tests only; database name must end in `_test` |

The API binds to `127.0.0.1`. `/health` is public liveness; `/api/v1` requires
bearer access tokens. Auth0 handles signup/login; the API provisions local users.
No Auth0 client secret is required by this API.

```bash
curl --fail --show-error http://127.0.0.1:8080/health
# Expected: {"status":"ok"}
bash scripts/local-db.sh status
bash scripts/local-db.sh stop
```

Ctrl+C gracefully stops the API with up to ten seconds for active requests.
Stopping the database preserves its files and does not affect a service on 5432.

## Architecture

Requests follow `router -> handler -> service -> repository -> PostgreSQL`.
Feature services own validation and use small repository/authorizer interfaces;
repositories own SQL. Keep responsibilities separate where they exist without
adding empty files or abstractions just to match a template.

| Location | Responsibility |
| --- | --- |
| `cmd/api` | Configuration, dependency wiring, startup, shutdown |
| `internal/auth` | Auth0 verification, local user provisioning, identity context |
| `internal/trips`, `memberships`, `itinerary`, `places` | Feature models, handlers, services, repositories, tests |
| `internal/httpx`, `apperror` | Strict JSON/ID/pagination helpers and safe error mapping |
| `internal/config`, `database`, `server` | Shared runtime infrastructure |
| `migrations` | Ordered Goose SQL migrations |
| `integration` | HTTP-to-PostgreSQL tests with fixture cleanup |
| `docs/backend` | Current domain/provider guidance |
| `docs/agents` | Issue tracker, triage labels, and domain-doc conventions |

Register routes in `internal/server` and wire dependencies in `cmd/api`.
Reuse shared helpers before introducing another; durable invariants belong in
PostgreSQL constraints and transactions. Extend `.env.example` only when code
actually uses a new setting. Do not extract services or add infrastructure for
hypothetical scale.

## Verification and migrations

```bash
bash scripts/check.sh
```

Default checks verify gofmt without rewriting files, run uncached Go tests,
vet, and build into ignored `.local/check/`. They explicitly clear the test
connection variable in the child process and report skipped database coverage.
They do not load `.env`, launch servers, or migrate databases.

For integration coverage, export the connection for a separate disposable
`_test` database, apply migrations, and select integration mode:

```bash
# Only source your trusted local .env; never print its contents or credentials.
set -a
source .env
set +a
# Confirm TEST_DATABASE_URL targets your disposable _test database first.
goose -dir migrations postgres "$TEST_DATABASE_URL" up
bash scripts/check.sh --integration
```

Missing `TEST_DATABASE_URL` fails integration mode before running checks.
Existing tests verify the actual database name ends in `_test` and remove their
own fixtures. Auth0 and place-provider tests use local fakes; no live accounts
or Nominatim requests are required.

Add a new numbered migration for schema changes; never edit an applied one.
Verify rollback only on a disposable database. CI uses a fresh PostgreSQL 18
service for `up -> down-to 0 -> up`, then runs integration checks. Never run
`down-to 0` against a database containing data you need to preserve.

Migration `00003` deliberately rejects legacy trips without explicit owners;
use a reviewed owner assignment before retrying it. Migration `00005` backfills
creators from current trip owners. Migrations `00006`/`00007` add places,
destination references, and optional exact trip windows. Production schema
repairs require forward migrations rather than blindly reversing data changes.

[GitHub Actions](.github/workflows/check.yml) runs on pushes and pull requests.
It checks script preflight, migration reversibility, and the same integration
command. It does not deploy or configure branch protection. Check failures
should be required in repository settings if you want merges blocked.

## Working with a coding agent

Start in this repository. [AGENTS.md](AGENTS.md) is the shared instruction entry
point; [CLAUDE.md](CLAUDE.md) imports it for Claude. Read only relevant domain
sections and the actual code/tests for the task. Plans are proposals requiring
revalidation, not instructions to implement the next numbered feature.

For example: "Fix the trip-window rejection; keep existing activities intact
and add a focused regression test." Describe the outcome, let the agent inspect
code, make a focused change, run checks, review the diff, report evidence and
gaps, then review the result yourself. Update the existing guide when a rule
changes rather than adding another competing document.

The project skill [travel-api-change](.agents/skills/travel-api-change/SKILL.md)
helps trace API changes through their contract, authorization, and persistence
effects. In Codex, for example: `$travel-api-change Fix trip-window rejection
while preserving saved activities and verify the SQL behavior.` Codex can also
select the skill for matching tasks. In Claude, ask it to read and follow that
linked file for the task; Codex's skill discovery does not configure Claude
automatically.

`AGENTS.md` holds essential rules; `.agents/skills/` groups reusable task
workflows; `docs/backend` holds current domain knowledge. Shared plans belong
in GitHub Issues. Optional notes in `docs/plans`, historical copies in
`docs/archive`, and scratch work in `.scratch` stay local and are ignored by
Git. Scripts and GitHub workflows remain in their normal tool locations.

## As this project grows

The check modes, PostgreSQL-backed CI workflow, and API-change skill exist
today. The upgrades below are optional next steps when their trigger occurs,
not installed tools or instructions to implement future product features.

| Trigger | Small next step |
| --- | --- |
| A change spans sessions or requires coordinated decisions | Save a task implementation plan using the format below |
| Regressions escape existing checks | Extend HTTP/PostgreSQL coverage at the failing boundary, including transaction and authorization cases |
| Client/server payloads frequently drift | Own a versioned OpenAPI contract, compatibility checks, and generated client/types for consumers |
| A module develops genuinely different conventions | Add concise instructions near that module; keep shared rules at the root |
| A recurring task needs the same explanation | Refine this skill or add one focused skill with a distinct trigger |
| Stable module boundaries repeatedly get violated | Add focused dependency-boundary checks for the specific allowed imports |
| More contributors need consistent review ownership | Configure required CI checks and add ownership rules for the relevant areas |

For a substantial requested task, record a plan in GitHub Issues when needed.
Local working notes may use ignored `docs/plans/<short-task>.md` files.
Record: **status** (proposed, active, or completed), **goal**, **acceptance
criteria**, **affected areas**, **implementation steps**, **decisions/progress**,
and **verification**. Keep it current so another session can resume. Existing
future-feature proposals remain proposed until explicitly authorized and
revalidated against current code. Small fixes can stay in the conversation.

Review these triggers when a recurring failure or coordination cost appears.
Assess future deployment, monitoring, security, and recovery requirements
separately from this coding-workflow roadmap. Agent review services, model
APIs, and observability stacks are not prerequisites here.
