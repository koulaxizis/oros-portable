#!/usr/bin/env bash
# Collects the source archives named in out/SOURCES.txt into out/src/, so a
# release carries the complete corresponding source next to the binaries.
# Run after build.sh. Needs curl, git and gh (with GH_TOKEN).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$ROOT/out"
SRC="$OUT/src"
# shellcheck source=/dev/null
. "$OUT/sources.env"
rm -rf "$SRC" && mkdir -p "$SRC"
MAX=$((2000 * 1024 * 1024)) # GitHub release assets must stay under 2 GiB

get() { # url file
  echo "  $2"
  curl -fsSL --retry 3 -o "$SRC/$2" "$1"
}

echo "== launcher"
git -C "$ROOT" archive --format=tar.gz --prefix="oros-portable-$VERSION/" -o "$SRC/oros-portable-$VERSION-src.tar.gz" HEAD

echo "== orOS web app"
get "https://github.com/koulaxizis/oros/archive/$OROS_COMMIT.tar.gz" "oros-$OROS_COMMIT-src.tar.gz"

echo "== ungoogled-chromium"
get "https://github.com/ungoogled-software/ungoogled-chromium-windows/archive/refs/tags/$WIN_TAG.tar.gz" "ungoogled-chromium-windows-$WIN_TAG-src.tar.gz"
get "https://github.com/ungoogled-software/ungoogled-chromium-portablelinux/archive/refs/tags/$LIN_TAG.tar.gz" "ungoogled-chromium-portablelinux-$LIN_TAG-src.tar.gz"
# The platform repos pin the patch set as a submodule; take the matching tag.
UG_TAG="$(echo "$LIN_TAG" | cut -d- -f1-2)"
get "https://github.com/ungoogled-software/ungoogled-chromium/archive/refs/tags/$UG_TAG.tar.gz" "ungoogled-chromium-$UG_TAG-src.tar.gz" ||
  echo "  WARNING: ungoogled-chromium tag $UG_TAG not found"

echo "== Chromium $CHROMIUM"
base="https://commondatastorage.googleapis.com/chromium-browser-official"
done=""
for name in "chromium-$CHROMIUM-lite.tar.xz" "chromium-$CHROMIUM.tar.xz"; do
  size="$(curl -fsSIL "$base/$name" | tr -d '\r' | awk 'tolower($1)=="content-length:"{n=$2} END{print n+0}')" || size=0
  if [ "$size" -gt 0 ] && [ "$size" -lt "$MAX" ]; then
    get "$base/$name" "$name"
    done=1
    break
  fi
  echo "  $name: size $size, skipped"
done
if [ -z "$done" ]; then
  echo "  WARNING: no Chromium tarball under 2 GiB; WRITTEN-OFFER.txt covers it"
fi

(cd "$SRC" && sha256sum -- * > SHA256SUMS-src.txt)
ls -lh "$SRC"
