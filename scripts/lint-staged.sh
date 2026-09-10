#!/usr/bin/env bash
set -euo pipefail

# -----------------------------------------------------------------------------
# Check staged Go formatting without rewriting files or changing the index.
# Lint and tests run against the working tree, using the normal Makefile targets.
# Run with: make lint-staged
# -----------------------------------------------------------------------------
cd "$(git rev-parse --show-toplevel)"

git diff --cached --name-only --diff-filter=ACMR -z -- '*.go' |
  while IFS= read -r -d '' file; do
    formatting=$(git show ":${file}" | gofmt -l)
    if [[ -n "$formatting" ]]; then
      printf 'Staged Go file needs gofmt: %s\nFormat it and review what you stage before retrying.\n' "$file" >&2
      exit 1
    fi
  done

make lint test
