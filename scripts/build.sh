#!/usr/bin/env bash
set -euo pipefail

# Build or install distributable Go binaries from the repository root.
# Usage: bash scripts/build.sh {build|install} [Go flags and packages...]
# GO, GOARCH, GOOS, and CGO_ENABLED may be supplied by the caller.
cd "$(dirname "${BASH_SOURCE[0]}")/.."

case "${1:-}" in
  build|install) command="$1"; shift ;;
  env|version) exec "${GO:-go}" "$@" ;;
  *) echo "Usage: $0 {build|install} [Go flags and packages...]" >&2; exit 2 ;;
esac

# Remove developer filesystem paths and automatic Git revision/time/dirty data.
# Strip symbol and DWARF tables (-s/-w), and omit the Go build ID (-buildid=).
# Runtime names, types, strings, and module versions remain: this is not obfuscation.
# Go accepts only the last -ldflags value, so merge caller flags (such as the
# release version) before adding the policy. Preserve argument boundaries.
ldflags=""
args=()
while (( $# )); do
  case "$1" in
    -ldflags)
      if (( $# < 2 )); then
        echo "-ldflags requires a value" >&2
        exit 2
      fi
      ldflags="$2"
      shift 2
      ;;
    -ldflags=*) ldflags="${1#-ldflags=}"; shift ;;
    *) args+=("$1"); shift ;;
  esac
done
exec "${GO:-go}" "$command" -trimpath -buildvcs=false \
  -ldflags="${ldflags:+$ldflags }-s -w -buildid=" "${args[@]}"
