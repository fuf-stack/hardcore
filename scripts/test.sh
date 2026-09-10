#!/usr/bin/env bash
set -euo pipefail

source "$(dirname "${BASH_SOURCE[0]}")/go-env.sh"

REPOSITORY_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${REPOSITORY_ROOT}"

TEST_TIMEOUT="${TEST_TIMEOUT:-120s}"
COVERAGE_FILE="${COVERAGE_FILE:-coverage.out}"
MIN_COVERAGE="${MIN_COVERAGE:-95.0}"

run_gotestsum() {
  go run -mod=readonly -modfile=tools/go.mod gotest.tools/gotestsum --no-color=false "$@"
}

run_gotestsum \
  --format testname \
  -- \
  -race \
  -coverprofile="${COVERAGE_FILE}" \
  -covermode=atomic \
  -timeout="${TEST_TIMEOUT}" \
  ./... \
  "$@"

TOTAL_COVERAGE="$(go tool cover -func="${COVERAGE_FILE}" | awk '/^total:/ {gsub("%", "", $3); print $3}')"
if ! awk -v total="${TOTAL_COVERAGE}" -v minimum="${MIN_COVERAGE}" 'BEGIN { exit !(total >= minimum) }'; then
  printf 'coverage %.1f%% is below the required %.1f%%\n' "${TOTAL_COVERAGE}" "${MIN_COVERAGE}" >&2
  exit 1
fi
printf 'total coverage: %.1f%% (minimum %.1f%%)\n' "${TOTAL_COVERAGE}" "${MIN_COVERAGE}"
