#!/usr/bin/env bash
set -euo pipefail

# -----------------------------------------------------------------------------
# Check staged Go formatting without rewriting files or changing the index.
# Lint and tests run against the working tree, using the normal Makefile targets.
# Run with: make lint-staged
# -----------------------------------------------------------------------------
cd "$(git rev-parse --show-toplevel)"

bash scripts/format.sh --staged

# Full lint checks working-tree formatting; hooks intentionally check the index only.
make vet test
