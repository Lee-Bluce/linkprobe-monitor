#!/usr/bin/env bash
# Build the front end, then cross-compile the single binary.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GOOS="${GOOS:-linux}"
GOARCH="${GOARCH:-arm64}"
VERSION="${VERSION:-1.0.1}"
OUT="build/linkprobe-${GOOS}-${GOARCH}"

cd "$root/web"
[ -d node_modules ] || npm install --no-audit --no-fund
npm run build

cd "$root"
mkdir -p build
build_time="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" go build -trimpath -buildvcs=false \
  -ldflags "-s -w -X main.version=${VERSION} -X main.buildTime=${build_time}" \
  -o "$OUT" ./cmd/linkprobe

ls -lh "$OUT"
