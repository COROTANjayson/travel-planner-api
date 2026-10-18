#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

if [ "$#" -gt 1 ]; then
  echo 'Usage: bash scripts/check.sh [--integration]' >&2
  exit 2
fi
case "${1:-}" in
  '')
    unset TEST_DATABASE_URL
    echo 'Database integration tests skipped; use --integration with exported TEST_DATABASE_URL.'
    ;;
  --integration)
    if [ -z "${TEST_DATABASE_URL:-}" ]; then
      echo 'Export TEST_DATABASE_URL for an already migrated database ending in _test.' >&2
      exit 1
    fi
    echo 'Database integration tests enabled; existing tests enforce the _test database name.'
    ;;
  *)
    echo 'Usage: bash scripts/check.sh [--integration]' >&2
    exit 2
    ;;
esac

unformatted="$(gofmt -l cmd internal integration)"
if [ -n "$unformatted" ]; then
  printf 'Run gofmt on these files:\n%s\n' "$unformatted" >&2
  exit 1
fi

go test -count=1 ./...
go vet ./...
mkdir -p .local/check
go build -o .local/check/api ./cmd/api
