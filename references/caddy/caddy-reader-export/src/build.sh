#!/usr/bin/env bash
#
# Build caddy-reader.
#
#   ./build.sh            just this machine's architecture
#   ./build.sh release    every architecture a home lab runs on, plus checksums
#
# Static and dependency-free: CGO off means there is no libc to match, so one
# binary runs on any Linux of that architecture — Debian, Alpine, a Synology, a
# Raspberry Pi. That is the whole point of it being a binary rather than a script
# whose interpreter you also have to keep working.
#
set -euo pipefail
cd "$(dirname "$0")"

VERSION="${VERSION:-$(cat VERSION 2>/dev/null || echo dev)}"
LDFLAGS="-s -w -X main.Version=${VERSION}"

# Find Go, including where the official tarball puts it.
#
# `go install` from go.dev unpacks to /usr/local/go and does NOT touch PATH, so
# a machine with a perfectly good toolchain reports "Go is not installed" until
# someone adds a line to their profile. Looking in the standard location turns
# that dead end into a build.
if command -v go >/dev/null; then
  GO=go
elif [ -x /usr/local/go/bin/go ]; then
  GO=/usr/local/go/bin/go
elif [ -x "${HOME}/go/bin/go" ]; then
  GO="${HOME}/go/bin/go"
else
  cat >&2 <<'MSG'
Go is not installed, or is not on PATH.

  Install:  https://go.dev/dl/
  Already installed to /usr/local/go? It is not on your PATH. Either:

      export PATH=$PATH:/usr/local/go/bin          # this shell
      echo 'export PATH=$PATH:/usr/local/go/bin' | sudo tee /etc/profile.d/go.sh   # every shell
MSG
  exit 1
fi

"$GO" vet ./...
"$GO" test ./...

if [ "${1:-}" != "release" ]; then
  CGO_ENABLED=0 "$GO" build -trimpath -ldflags "$LDFLAGS" -o caddy-reader .
  ls -la caddy-reader
  exit 0
fi

rm -rf dist && mkdir -p dist
build() {
  local name="$1" goarch="$2" goarm="${3:-}"
  echo "  linux/${name}"
  CGO_ENABLED=0 GOOS=linux GOARCH="$goarch" GOARM="$goarm" \
    "$GO" build -trimpath -ldflags "$LDFLAGS" -o "dist/caddy-reader-linux-${name}" .
}

build amd64 amd64
build arm64 arm64
build armv7 arm 7

# Checksums, so someone downloading a binary for their reverse proxy can check
# they got what was built.
( cd dist && sha256sum caddy-reader-linux-* > SHA256SUMS )
ls -la dist
