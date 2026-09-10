#!/usr/bin/env bash
set -euo pipefail

source "$(dirname "${BASH_SOURCE[0]}")/go-env.sh"

# -----------------------------------------------------------------------------
# PostgreSQL integration and end-to-end tests
# -----------------------------------------------------------------------------
# Usage: make test-integration (all), or make test-e2e (HTTP lifecycle only).
# Supply HARDCORE_TEST_DATABASE_URL to use an existing test database. Otherwise
# start a disposable PostgreSQL container; only that container is removed.
# -----------------------------------------------------------------------------

cd "$(dirname "${BASH_SOURCE[0]}")/.."

case "${1:---all}" in
  --all) pattern='Test(Integration|E2E)' ;;
  --e2e) pattern='TestE2E' ;;
  *) echo "Usage: $0 [--all | --e2e]" >&2; exit 2 ;;
esac

container=""
cleanup() {
  if [[ -n "$container" ]]; then
    docker stop "$container" >/dev/null
  fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

if [[ -z "${HARDCORE_TEST_DATABASE_URL:-}" ]]; then
  postgres_tag="$(bash scripts/version.sh POSTGRES_TAG)"
  container="$(docker run --rm -d --tmpfs /var/lib/postgresql \
    -e POSTGRES_PASSWORD=integration-test-only -p 127.0.0.1::5432 "postgres:$postgres_tag")"
  ready=false
  for ((attempt=0; attempt<30; attempt++)); do
    if docker exec "$container" pg_isready -h 127.0.0.1 -U postgres >/dev/null 2>&1; then
      ready=true
      break
    fi
    sleep 1
  done
  if [[ "$ready" != true ]]; then echo "PostgreSQL did not become ready" >&2; exit 1; fi
  endpoint="$(docker port "$container" 5432/tcp)"
  export HARDCORE_TEST_DATABASE_URL="postgres://postgres:integration-test-only@$endpoint/postgres?sslmode=disable"
fi

# Read the existing runner pin without resolving the developer-tools module.
version="$(awk '$1 == "gotest.tools/gotestsum" {print $2; exit}' tools/go.mod)"
if [[ "$version" != v[0-9]* ]]; then echo "Missing gotestsum pin" >&2; exit 1; fi
cd integration
export GOWORK=off
go vet -mod=readonly ./...
go run "gotest.tools/gotestsum@$version" --no-color=false --format testname -- \
  -mod=readonly -race -count=1 -timeout=120s -run "$pattern" ./...
