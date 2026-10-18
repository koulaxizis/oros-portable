#!/usr/bin/env bash
# Writes the Scoop, winget and AUR manifests for a release from the templates
# in packaging/:
#   scripts/channel-manifests.sh <version> <sha256 of the setup> <out dir> [sha256 of the .deb]
# Without the .deb checksum the AUR files are left out.
set -euo pipefail
VERSION="$1" SHA="$2" OUT="$3" DEBSHA="${4:-}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
[[ "$SHA" =~ ^[0-9a-f]{64}$ ]] || { echo "bad sha256: $SHA"; exit 1; }
[[ -z "$DEBSHA" || "$DEBSHA" =~ ^[0-9a-f]{64}$ ]] || { echo "bad .deb sha256: $DEBSHA"; exit 1; }
[[ "$VERSION" =~ ^[0-9]+(\.[0-9]+)*$ ]] || { echo "bad version: $VERSION"; exit 1; }
fill() { sed -e "s/@VERSION@/$VERSION/g" -e "s/@SHA256@/$SHA/g" -e "s/@SHA256UPPER@/${SHA^^}/g" -e "s/@DEBSHA256@/$DEBSHA/g" -e "s/@DATE@/$(date -u +%F)/g" "$1"; }
mkdir -p "$OUT/bucket" "$OUT/winget"
fill "$ROOT/packaging/scoop/oros.json.in" > "$OUT/bucket/oros.json"
for f in "$ROOT"/packaging/winget/*.yaml.in; do
  fill "$f" > "$OUT/winget/$(basename "${f%.in}")"
done
CHECK=("$OUT"/winget/*.yaml "$OUT/bucket/oros.json")
if [ -n "$DEBSHA" ]; then
  mkdir -p "$OUT/aur"
  fill "$ROOT/packaging/aur/PKGBUILD.in" > "$OUT/aur/PKGBUILD"
  fill "$ROOT/packaging/aur/SRCINFO.in" > "$OUT/aur/.SRCINFO"
  CHECK+=("$OUT/aur/PKGBUILD" "$OUT/aur/.SRCINFO")
fi
python3 -c 'import json,sys; json.load(open(sys.argv[1]))' "$OUT/bucket/oros.json"
if grep -l '@[A-Z0-9]*@' "${CHECK[@]}"; then echo "unfilled placeholder"; exit 1; fi
ls -R "$OUT"
