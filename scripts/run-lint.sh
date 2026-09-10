#!/usr/bin/env bash
set -euo pipefail

# -----------------------------------------------------------------------------
# Purpose: Run the repo-local linter consistently in terminals and GUI hooks.
# Usage: bash scripts/run-lint.sh [additional golangci-lint run arguments]
# Setup owns installation; hooks never download tools or modify module files.
# -----------------------------------------------------------------------------
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "$ROOT_DIR/scripts/go-env.sh"
cd "$ROOT_DIR"
runner="$ROOT_DIR/tools/bin/golangci-lint"
if [[ ! -x "$runner" ]]; then
  echo "Repo-local golangci-lint missing; run make setup-go-tools." >&2
  exit 1
fi
echo "Using $runner"
exec "$runner" run --config "$ROOT_DIR/.golangci.yml" "$@"
