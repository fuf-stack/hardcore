#!/usr/bin/env bash
set -euo pipefail

# GUI Git clients do not inherit the terminal's Go toolchain PATH.
source "$(dirname "${BASH_SOURCE[0]}")/go-env.sh"

# -----------------------------------------------------------------------------
# Install the pinned development tools into the Go build cache and configure
# repository-managed hooks. Run with: make setup-go-tools
# -----------------------------------------------------------------------------

cd "$(dirname "${BASH_SOURCE[0]}")/.."

echo "Preparing pinned Go tools..."
bash scripts/setup-lint.sh
go run -mod=readonly -modfile=tools/go.mod github.com/conventionalcommit/commitlint --version
go run -mod=readonly -modfile=tools/go.mod gotest.tools/gotestsum --version

CURRENT_HOOKS_PATH="$(git config --get core.hooksPath || true)"
if [[ -n "$CURRENT_HOOKS_PATH" && "$CURRENT_HOOKS_PATH" != '.commitlint/hooks' ]]; then
  printf 'Existing hooksPath is %s; integrate the commit-msg hook before changing it.\n' "$CURRENT_HOOKS_PATH" >&2
  exit 1
fi

git config --local core.hooksPath .commitlint/hooks
chmod +x .commitlint/hooks/commit-msg .commitlint/hooks/pre-commit
echo "Tool setup complete."
