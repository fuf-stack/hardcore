#!/usr/bin/env bash
set -euo pipefail

# -----------------------------------------------------------------------------
# Prepare the pinned repo-local linter; never called from commit hooks.
# Usage: bash scripts/setup-lint.sh
# -----------------------------------------------------------------------------
cd "$(dirname "${BASH_SOURCE[0]}")/.."
version=$(bash scripts/version.sh GOLANGCI_LINT_TAG)
version=${version#v}
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "Invalid linter pin" >&2; exit 1; }
current=""
if [[ -x tools/bin/golangci-lint ]]; then
  current=$(tools/bin/golangci-lint version | sed -nE 's/.*version ([0-9.]+).*/\1/p')
fi
if [[ "$current" != "$version" ]]; then
  bash scripts/setup-golangci-lint.sh "$version" "$PWD/tools/bin"
fi
