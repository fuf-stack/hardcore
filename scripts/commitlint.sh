#!/usr/bin/env bash
set -euo pipefail

source "$(dirname "${BASH_SOURCE[0]}")/go-env.sh"

REPOSITORY_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${REPOSITORY_ROOT}"

exec go run -mod=readonly -modfile=tools/go.mod github.com/conventionalcommit/commitlint \
  lint --config .commitlint.yaml "$@"
