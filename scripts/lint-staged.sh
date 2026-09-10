#!/usr/bin/env bash
set -euo pipefail

# GUI Git clients do not inherit the terminal's Go toolchain PATH.
source "$(dirname "${BASH_SOURCE[0]}")/go-env.sh"

# -----------------------------------------------------------------------------
# Check staged Go formatting without rewriting files or changing the index.
# Lint and tests run against the working tree, using the normal Makefile targets.
# Run with: make lint-staged
# -----------------------------------------------------------------------------
cd "$(git rev-parse --show-toplevel)"

bash scripts/format.sh --staged

# Full lint checks working-tree formatting; hooks intentionally check the index only.
make vet test
