# Travel Planner API

A local Go REST API using Chi, pgx, PostgreSQL, and Goose. The API is its own Go module inside the overall travel planner project.

## Run locally (Git Bash on Windows)

Run this from `travel-planner-api/`. Go and Goose must be on your `PATH`:

```console
./scripts/run.sh
```

The script starts the project PostgreSQL instance on `127.0.0.1:55432`, applies migrations, and runs the API. Its data lives in ignored `.local/postgres/`; connection settings live in ignored `.env`.

If you prefer another existing PostgreSQL instance, create a development database and a separate database ending in `_test`, then configure their connection URLs in `.env`. Skip the `pg_ctl` commands for the project instance in that case.

The API automatically reads `.env` from the current working directory using [godotenv](https://github.com/joho/godotenv). Run it from `travel-planner-api/`. Existing shell environment variables take precedence; a missing `.env` is allowed when the required settings are supplied by the environment. Invalid `.env` syntax fails startup without printing file contents.

Once the database is running and migrations are applied, starting the API needs only:

```bash
go run ./cmd/api
```

The `source .env` commands in this README are still needed for Goose and integration tests, which read their connection settings from the shell.

| Setting | Behavior |
| --- | --- |
| `PORT` | Defaults to `8080`; accepts integers from 1 to 65535. |
| `DATABASE_URL` | Required PostgreSQL connection URL. Startup checks the connection and fails with a clear error if it cannot connect. |
| `AUTH0_ISSUER_URL` | Required Auth0 issuer URL, including `https://` and the trailing slash. |
| `AUTH0_AUDIENCE` | Required Auth0 API identifier expected in access tokens. |
| `TEST_DATABASE_URL` | Used only by integration tests; the database name must end in `_test`. |

Create an Auth0 API using RS256. Set `AUTH0_ISSUER_URL` to its tenant domain with a trailing slash and `AUTH0_AUDIENCE` to its API Identifier. Auth0 handles signup and login; the API creates a local user on the first authenticated request.

The API listens on `127.0.0.1`. Auth0 protects `/api/v1`; `/health` remains public. Trip ownership and permissions are not implemented yet, so authenticated users are not isolated from each other's trip data and this version remains for local development.

In another Git Bash terminal:

```bash
curl --fail --show-error http://127.0.0.1:8080/health
```

Expected response: `{"status":"ok"}`. This endpoint reports HTTP server liveness; database connectivity is checked at startup.

Stop a foreground API with Ctrl+C. It allows up to ten seconds for active requests to finish, then closes its PostgreSQL pool. To manage the project database:

```bash
pg_ctl -D .local/postgres status
pg_ctl -D .local/postgres -m fast -w stop
```

Stopping it preserves the database files. These commands target only `.local/postgres/`; the existing PostgreSQL service on port 5432 is unchanged.

### Database setup on a fresh checkout

Skip this section on the current machine. For a fresh checkout with no `.local/postgres/` cluster, run the following in Git Bash from `travel-planner-api/`:

```bash
export PATH="/c/Program Files/PostgreSQL/18/bin:$PATH"
mkdir -p .local
initdb -D .local/postgres -U travel_planner_dev \
  --auth=scram-sha-256 --encoding=UTF8 --locale=C -W
```

`initdb` prompts you to choose the local database password. Once initialization succeeds, start PostgreSQL and create the databases; enter that password when prompted:

```bash
pg_ctl -D .local/postgres -l .local/postgres.log \
  -o "-h 127.0.0.1 -p 55432" -w start
createdb -h 127.0.0.1 -p 55432 -U travel_planner_dev -W travel_planner
createdb -h 127.0.0.1 -p 55432 -U travel_planner_dev -W travel_planner_test
if [ ! -f .env ]; then
  cp .env.example .env
fi
```

Edit `.env` in your editor to use these settings, replacing `YOUR_PASSWORD` with the password you chose (URL-encode special characters in the password):

```dotenv
PORT=8080
DATABASE_URL=postgres://travel_planner_dev:YOUR_PASSWORD@127.0.0.1:55432/travel_planner?sslmode=disable
AUTH0_ISSUER_URL=https://YOUR_AUTH0_DOMAIN/
AUTH0_AUDIENCE=https://travel-planner-api
TEST_DATABASE_URL=postgres://travel_planner_dev:YOUR_PASSWORD@127.0.0.1:55432/travel_planner_test?sslmode=disable
```

Then follow the migration and API startup commands above. Keep `.env` local; it is already ignored by Git.

## Architecture and folders

Requests follow `router -> handler -> service -> repository -> PostgreSQL`. Each feature owns its business rules and SQL; services depend on small repository interfaces.

```text
travel-planner-api/
├── cmd/api/main.go        # Startup, dependency wiring, graceful shutdown
├── internal/
│   ├── config/            # Environment configuration
│   ├── database/          # Shared pgx connection pool and startup check
│   ├── server/            # Chi routes, timeouts, JSON logs, recovery
│   ├── httpx/             # JSON responses, request decoding, IDs, pagination
│   ├── apperror/          # Shared validation and not-found errors
│   ├── trips/             # Model, handler, service, repository, unit tests
│   └── itinerary/         # Model, handler, service, repository, unit tests
├── migrations/            # Goose SQL migrations
├── integration/           # HTTP-to-PostgreSQL lifecycle test
├── scripts/               # Optional local database helper
├── .env.example           # Example settings without real credentials
├── .gitignore
├── go.mod
└── go.sum
```

Authentication lives in `internal/auth/`; it validates Auth0 tokens and provisions local users.

## API

All feature endpoints use the `/api/v1` prefix and require an Auth0 bearer access token. Request bodies and successful resource responses are JSON.

| Method | Path | Result |
| --- | --- | --- |
| GET | `/health` | Server status, HTTP 200 |
| GET | `/api/v1/me` | Current local user, HTTP 200 |
| POST | `/api/v1/trips` | Create trip, HTTP 201 |
| GET | `/api/v1/trips` | List trips, HTTP 200 |
| GET / PUT / DELETE | `/api/v1/trips/{tripID}` | Read / replace / delete a trip |
| POST | `/api/v1/trips/{tripID}/activities` | Create scheduled activity, HTTP 201 |
| GET | `/api/v1/trips/{tripID}/activities` | List activities in schedule order |
| GET / PUT / DELETE | `/api/v1/trips/{tripID}/activities/{activityID}` | Read / replace / delete an activity within that trip |

Reads and replacements return HTTP 200; deletion returns HTTP 204 with no body. Creation also returns a `Location` header. IDs are positive integers. Missing or invalid authentication returns HTTP 401, invalid input returns HTTP 400, missing resources return HTTP 404, and unexpected errors return HTTP 500. Errors use `{"error":"message"}` without database details.

List endpoints return arrays, including `[]` when empty. Both accept `?limit=50&offset=0`; limit defaults to 50 and is capped at 100. Trips sort by newest ID first; activities sort by start instant and then ID. Listing activities for a nonexistent trip returns HTTP 404.

Create a trip:

```bash
ACCESS_TOKEN=YOUR_AUTH0_ACCESS_TOKEN
curl --fail-with-body http://127.0.0.1:8080/api/v1/trips \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Cebu weekend","destination":"Cebu","start_date":"2026-10-01","end_date":"2026-10-03","time_zone":"Asia/Manila"}'
```

Create an activity using the trip ID returned above:

```bash
TRIP_ID=1  # Replace with the returned trip ID.
curl --fail-with-body "http://127.0.0.1:8080/api/v1/trips/$TRIP_ID/activities" \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"title":"Breakfast","starts_at":"2026-10-01T09:00:00+08:00","ends_at":"2026-10-01T10:00:00+08:00","time_zone":"Asia/Manila","notes":"Near the hotel"}'
```

Use the same fields with `PUT` to replace a resource. Trip fields are required. Activity fields are required except `notes`, which defaults to an empty string.

Dates use `YYYY-MM-DD`; a trip's end date cannot precede its start date. Activity times require RFC3339 timestamps with an explicit offset or `Z`, and the end must be after the start. The API stores timestamp instants in PostgreSQL `TIMESTAMPTZ` and returns UTC, while preserving an explicit IANA zone such as `Asia/Manila`. Activities can have their own zones for travel across time zones. Unknown JSON fields and bodies over 1 MiB are rejected.

Deleting a trip also deletes its activities. Activities are always addressed within their parent trip. Days can be derived from activity timestamps in their saved time zones; this first version has no separate day-management endpoint or manual ordering.

Trip dates are planning metadata: this version does not enforce activity containment within those dates or detect overlapping activities. Trip ownership, group split/rejoin, memberships, templates, expenses, and realtime collaboration remain future work.

## Migrations, tests, and build

Apply migrations to the separate test database before integration tests:

```bash
set -a
source .env
set +a
goose -dir migrations postgres "$TEST_DATABASE_URL" up
go test ./...
go vet ./...
go build -o travel-planner-api.exe ./cmd/api
```

Without `TEST_DATABASE_URL`, the integration test explicitly skips; unit and handler tests still run. With it, tests exercise actual PostgreSQL persistence, trip and activity CRUD, UTC conversion, pagination, parent-trip isolation, and cascading deletion. They remove only the trips they create. Validation tests cover invalid dates, time zones, IDs, JSON, and pagination.

The migrations create trips, itinerary items, and local users. To check the latest migration rollback on the disposable test database only:

```bash
goose -dir migrations postgres "$TEST_DATABASE_URL" down
goose -dir migrations postgres "$TEST_DATABASE_URL" up
```

One rollback removes the latest `users` table and its contents. Development migrations are run explicitly before starting the API.
