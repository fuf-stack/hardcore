#!/usr/bin/env bash

# -----------------------------------------------------------------------------
# Go environment for non-login shells
# -----------------------------------------------------------------------------
# Purpose: Make Go and gofmt available to GUI Git hooks without sourcing user
# shell profiles or installing anything. Existing PATH entries take precedence.
# Usage: source scripts/go-env.sh
# -----------------------------------------------------------------------------
for go_path_dir in \
  "$HOME/.asdf/shims" \
  "$HOME/.local/share/mise/shims" \
  "/opt/homebrew/bin" \
  "/usr/local/go/bin" \
  "/usr/local/bin" \
  "$HOME/go/bin"; do
  if [[ -d "$go_path_dir" ]]; then
    case ":$PATH:" in
      *":$go_path_dir:"*) ;;
      *) PATH="${PATH:+$PATH:}$go_path_dir" ;;
    esac
  fi
done
export PATH

if ! command -v go >/dev/null 2>&1; then
  echo "Go is not available. Install Go or expose its bin directory to the Git client." >&2
  return 1
fi

# A version-manager shim may expose Go without exposing its companion formatter.
go_root_dir="$(go env GOROOT)" || return 1
if [[ -d "$go_root_dir/bin" ]]; then
  case ":$PATH:" in
    *":$go_root_dir/bin:"*) ;;
    *) PATH="$PATH:$go_root_dir/bin" ;;
  esac
fi
export PATH
if ! command -v gofmt >/dev/null 2>&1; then
  echo "gofmt is missing from the selected Go toolchain." >&2
  return 1
fi
