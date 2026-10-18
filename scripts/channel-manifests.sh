#!/usr/bin/env bash
# Writes the Scoop manifest for a release from the template in
# packaging/: scripts/channel-manifests.sh <version> <sha256 of the setup> <out dir>
set -euo pipefail
VERSION="$1" SHA="$2" OUT="$3"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
[[ "$SHA" =~ ^[0-9a-f]{64}$ ]] || { echo "bad sha256: $SHA"; exit 1; }
[[ "$VERSION" =~ ^[0-9]+(\.[0-9]+)*$ ]] || { echo "bad version: $VERSION"; exit 1; }
fill() { sed -e "s/@VERSION@/$VERSION/g" -e "s/@SHA256@/$SHA/g" -e "s/@SHA256UPPER@/${SHA^^}/g" -e "s/@DATE@/$(date -u +%F)/g" "$1"; }
mkdir -p "$OUT/bucket"
fill "$ROOT/packaging/scoop/oros.json.in" > "$OUT/bucket/oros.json"
python3 -c 'import json,sys; json.load(open(sys.argv[1]))' "$OUT/bucket/oros.json"
if grep -l '@[A-Z0-9]*@' "$OUT/bucket/oros.json"; then echo "unfilled placeholder"; exit 1; fi
ls -R "$OUT"
