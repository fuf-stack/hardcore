#!/usr/bin/env bash
set -euo pipefail

# This script installs golangci-lint from the official GitHub release assets.
# It verifies the downloaded archive against the release checksum file using an
# exact filename match so related assets like "*.tar.gz.sbom.json" cannot affect
# the expected checksum.

VERSION="${1:-}"
BINDIR="${2:-}"

if [ -z "$VERSION" ] || [ -z "$BINDIR" ]; then
  echo "Usage: $0 <version-without-v-prefix> <install-dir>" >&2
  exit 1
fi

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    echo "Unable to find sha256sum or shasum for checksum verification" >&2
    exit 1
  fi
}

golangci_lint_platform() {
  case "$(uname -s)" in
    Darwin)
      goos="darwin"
      ;;
    Linux)
      goos="linux"
      ;;
    *)
      echo "Unsupported OS for golangci-lint: $(uname -s)" >&2
      exit 1
      ;;
  esac

  case "$(uname -m)" in
    x86_64|amd64)
      goarch="amd64"
      ;;
    arm64|aarch64)
      goarch="arm64"
      ;;
    i386|i686)
      goarch="386"
      ;;
    *)
      echo "Unsupported architecture for golangci-lint: $(uname -m)" >&2
      exit 1
      ;;
  esac

  printf '%s %s\n' "$goos" "$goarch"
}

install_golangci_lint() {
  version="$1"
  bindir="$2"
  platform="$(golangci_lint_platform)"
  goos="${platform% *}"
  goarch="${platform#* }"

  archive="golangci-lint-${version}-${goos}-${goarch}.tar.gz"
  checksum_file="golangci-lint-${version}-checksums.txt"
  base_url="https://github.com/golangci/golangci-lint/releases/download/v${version}"
  tmpdir="$(mktemp -d)"

  cleanup_golangci_lint_tmp() {
    rm -rf "$tmpdir"
  }
  trap cleanup_golangci_lint_tmp EXIT

  echo "Downloading $archive..."
  curl -sSfL -o "$tmpdir/$archive" "$base_url/$archive"
  curl -sSfL -o "$tmpdir/$checksum_file" "$base_url/$checksum_file"

  expected="$(awk -v filename="$archive" '$2 == filename {print $1}' "$tmpdir/$checksum_file")"
  if [ -z "$expected" ]; then
    echo "Failed to find checksum for $archive in $checksum_file" >&2
    exit 1
  fi

  actual="$(sha256_file "$tmpdir/$archive")"
  if [ "$actual" != "$expected" ]; then
    echo "Checksum verification failed for $archive" >&2
    echo "$actual vs $expected" >&2
    exit 1
  fi

  tar -xzf "$tmpdir/$archive" -C "$tmpdir"
  install -d "$bindir"
  install "$tmpdir/golangci-lint-${version}-${goos}-${goarch}/golangci-lint" "$bindir/golangci-lint"
}

install_golangci_lint "$VERSION" "$BINDIR"
