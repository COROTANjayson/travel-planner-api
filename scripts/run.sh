#!/usr/bin/env bash
set -e

cd "$(dirname "$0")/.."
./scripts/local-db.sh start
set -a
source .env
set +a
goose -dir migrations postgres "$DATABASE_URL" up
exec go run ./cmd/api
