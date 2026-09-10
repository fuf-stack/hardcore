#!/usr/bin/env bash
set -euo pipefail

# -----------------------------------------------------------------------------
# Read one central version pin without installing a YAML parser or Go tools.
# Usage: bash scripts/version.sh VARIABLE_NAME
# Supports the quoted scalar format used in versions.yaml; fails on missing,
# duplicate, or malformed pins rather than falling back to an unpinned version.
# -----------------------------------------------------------------------------

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
key="${1:-}"

if [[ $# -ne 1 || ! "$key" =~ ^[A-Z][A-Z0-9_]*$ ]]; then
  echo "Usage: $0 VARIABLE_NAME" >&2
  exit 2
fi

value=$(sed -nE "s/^  ${key}: \"([a-zA-Z0-9._+-]+)\"$/\\1/p" "$root_dir/versions.yaml")

if [[ ! "$value" =~ ^[a-zA-Z0-9._+-]+$ ]]; then
  echo "Missing, duplicate, or invalid version pin: $key" >&2
  exit 1
fi

printf '%s\n' "$value"
