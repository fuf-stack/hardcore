#!/usr/bin/env bash
# =============================================================================
# Go formatting
# =============================================================================
# Format repository Go files, or report drift without changing files or staging.
# Usage: bash scripts/format.sh [--check | --staged]
# --check includes tracked/untracked files, integration tests, and tools.
# --staged checks only the index; unstaged edits are never included.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."
case "${1:-}" in
  "") mode=write ;;
  --check) mode=check ;;
  --staged) mode=staged ;;
  *) echo "Usage: $0 [--check | --staged]" >&2; exit 2 ;;
esac

# Materialize Git's result so discovery errors cannot disappear in a pipeline.
file_list="$(mktemp)"
trap 'rm -f "$file_list"' EXIT
if [[ "$mode" == staged ]]; then
  git diff --cached --name-only --diff-filter=ACMRT -z -- '*.go' > "$file_list"
else
  git ls-files -z --cached --others --exclude-standard -- '*.go' > "$file_list"
fi
status=0
while IFS= read -r -d '' file; do
  if [[ "$mode" == staged ]]; then
    # Skip symlinks and submodules using the index, not the working tree.
    entry="$(git ls-files --stage -- "$file")"
    case "$entry" in
      100644\ *|100755\ *) ;;
      *) continue ;;
    esac
    pending="$(git show ":$file" | gofmt -s -l)"
  else
    # Deleted working-tree files and symlinks must not be formatted.
    [[ -f "$file" && ! -L "$file" ]] || continue
    if [[ "$mode" == write ]]; then
      gofmt -s -w "$file"
      continue
    fi
    pending="$(gofmt -s -l "$file")"
  fi
  if [[ -n "$pending" ]]; then
    printf '%s\n' "$file"
    status=1
  fi
done < "$file_list"
if [[ "$status" != 0 ]]; then
  if [[ "$mode" == staged ]]; then
    echo "Staged Go formatting differs; run make fmt and review what you stage before retrying." >&2
  else
    echo "Go formatting differs; run make fmt." >&2
  fi
fi
exit "$status"
