#!/usr/bin/env bash
# Builds the stick speed test for Windows and Linux into out/sticktest/.
set -euo pipefail
VERSION="${1:-0.0.0-dev}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$ROOT/out/sticktest"
mkdir -p "$OUT"
cd "$ROOT/tools/sticktest"
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$OUT/orOS-sticktest-$VERSION-windows.exe" .
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$OUT/orOS-sticktest-$VERSION-linux" .
ls -lh "$OUT"
