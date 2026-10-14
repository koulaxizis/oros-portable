#!/usr/bin/env bash
# Builds the orOS Portable folders:
#   out/orOS-Portable       launchers + orOS + bundled ungoogled-chromium
#   out/orOS-Portable-lite  launchers + orOS, uses an installed browser
# Needs: go, curl, unzip, xz, and gh (with GH_TOKEN) for the browser download.
set -euo pipefail

VERSION="${1:-0.0.0-dev}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$ROOT/out"
LITE="$OUT/orOS-Portable-lite"
FULL="$OUT/orOS-Portable"
rm -rf "$OUT" && mkdir -p "$LITE"

echo "== launchers $VERSION"
(cd "$ROOT/launcher" &&
  GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-H=windowsgui -s -w -X main.version=$VERSION" -o "$LITE/orOS.exe" . &&
  GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$LITE/orOS" .)
cp "$ROOT/dist-extra/"* "$LITE/"
chmod +x "$LITE/orOS" "$LITE/update-oros.sh"

echo "== orOS from main"
# The launcher's own update command fetches and unpacks orOS.
OROS_PORTABLE_ROOT="$LITE" "$LITE/orOS" update
rm -rf "$LITE/data" "$LITE/app.old"

echo "== bundled browser"
cp -r "$LITE" "$FULL"
TMP="$(mktemp -d)"
gh release download --repo ungoogled-software/ungoogled-chromium-windows \
  --pattern '*_windows_x64.zip' --dir "$TMP/win"
gh release download --repo ungoogled-software/ungoogled-chromium-portablelinux \
  --pattern '*x86_64_linux.tar.xz' --dir "$TMP/linux"
unzip -q "$TMP"/win/*.zip -d "$TMP/win/x"
tar -xJf "$TMP"/linux/*.tar.xz -C "$TMP/linux"
WIN_DIR="$(dirname "$(find "$TMP/win/x" -name chrome.exe | head -1)")"
LIN_DIR="$(dirname "$(find "$TMP/linux" -type f -name chrome | head -1)")"
mkdir -p "$FULL/browser"
mv "$WIN_DIR" "$FULL/browser/windows"
mv "$LIN_DIR" "$FULL/browser/linux"
ls "$FULL/browser/windows/chrome.exe" "$FULL/browser/linux/chrome" >/dev/null
rm -rf "$TMP"

echo "== zips"
(cd "$OUT" && zip -qry "orOS-Portable-$VERSION.zip" orOS-Portable && zip -qry "orOS-Portable-lite-$VERSION.zip" orOS-Portable-lite)
ls -lh "$OUT"/*.zip
