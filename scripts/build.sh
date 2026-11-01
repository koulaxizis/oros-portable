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
# Windows version info and icon: product name and version must be set in
# every build (SignPath Foundation's signing conditions). Numbers only, so
# test builds "0.0.0-<sha>" carry 0.0.0.
NUMVER="${VERSION%%-*}"
(cd "$ROOT/launcher" && rm -f rsrc_windows_*.syso &&
  go run github.com/tc-hib/go-winres@v0.3.3 simply --arch amd64 --manifest gui \
    --icon "$ROOT/packaging/windows/oros.ico" --product-name orOS --file-description orOS \
    --product-version "$NUMVER" --file-version "$NUMVER" --original-filename orOS.exe \
    --copyright "MIT License, orOS contributors" --out rsrc)
(cd "$ROOT/launcher" &&
  GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-H=windowsgui -s -w -X main.version=$VERSION" -o "$LITE/orOS.exe" . &&
  GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$LITE/orOS" .)
rm -f "$ROOT/launcher"/rsrc_windows_*.syso
cp "$ROOT/dist-extra/"* "$LITE/"
chmod +x "$LITE/orOS" "$LITE/update-oros.sh"

echo "== orOS from main"
# The launcher's own update command fetches and unpacks orOS.
OROS_COMMIT="$(gh api repos/koulaxizis/oros/commits/main -q .sha 2>/dev/null || echo unknown)"
OROS_PORTABLE_ROOT="$LITE" "$LITE/orOS" update
rm -rf "$LITE/data" "$LITE/app.old"

echo "== bundled browser"
cp -r "$LITE" "$FULL"
TMP="$(mktemp -d)"
# Pin the release tags first, so SOURCES.txt names exactly what was bundled.
WIN_TAG="$(gh release view --repo ungoogled-software/ungoogled-chromium-windows --json tagName -q .tagName)"
LIN_TAG="$(gh release view --repo ungoogled-software/ungoogled-chromium-portablelinux --json tagName -q .tagName)"
gh release download "$WIN_TAG" --repo ungoogled-software/ungoogled-chromium-windows \
  --pattern '*_windows_x64.zip' --dir "$TMP/win"
gh release download "$LIN_TAG" --repo ungoogled-software/ungoogled-chromium-portablelinux \
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

echo "== System extension"
# The orOS USB System extension (sound, brightness, power, window, updates
# in Quick settings); the launcher is its native host (launcher/syshost.go).
# Only the bundled browser loads it, so the lite edition has none.
# Needs jq for ubol.sh.
TMP="$(mktemp -d)"
gh api repos/koulaxizis/oros-usb/tarball/main > "$TMP/usb.tar.gz"
tar -xzf "$TMP/usb.tar.gz" -C "$TMP"
cp -r "$(find "$TMP" -maxdepth 3 -type d -path '*/system/extension' | head -1)" "$FULL/system"
ls "$FULL/system/manifest.json" "$FULL/system/content.js" >/dev/null
# uBlock Origin Lite for the Internet window (launcher/sysweb.go), with
# orOS USB's own script: checked against the release's SHA-256.
UBOL="$(sh "$(find "$TMP" -maxdepth 3 -type f -path '*/scripts/ubol.sh' | head -1)" "$FULL/ubol" | tail -1)"
# The orOS add-on (Passwords fill and save in the Internet window, which
# opens in orOS's profile: launcher/sysweb.go), from the same orOS commit
# as app/, with orOS USB's own script presetting its address to the
# launcher's.
OROS_REF="$OROS_COMMIT"; [ "$OROS_REF" != unknown ] || OROS_REF=main
gh api "repos/koulaxizis/oros/tarball/$OROS_REF" > "$TMP/oros.tar.gz"
mkdir -p "$TMP/oros"
tar -xzf "$TMP/oros.tar.gz" -C "$TMP/oros" --wildcards '*/extension/*'
ADDON="$(sh "$(find "$TMP" -maxdepth 3 -type f -path '*/scripts/addon.sh' | head -1)" \
  "$(find "$TMP/oros" -maxdepth 2 -type d -name extension | head -1)" "$FULL/addon" | tail -1)"
# Tor Browser's signing key for the Tor button (launcher/tor.go), with
# orOS USB's own script: fetched from torproject.org and kept only with the
# fingerprint pinned there. Needs gpg.
sh "$(find "$TMP" -maxdepth 3 -type f -path '*/scripts/tor-key.sh' | head -1)" "$FULL/tor-browser-developers.gpg"
USB_COMMIT="$(gh api repos/koulaxizis/oros-usb/commits/main -q .sha 2>/dev/null || echo unknown)"
rm -rf "$TMP"

echo "== orOS Bridge"
# Plugins and the sound card for orOS Studio (launcher/bridge.go starts it
# with --bundled), from the latest orOS Bridge release, checked against its
# SHA256SUMS.txt. 0.8.0 is the first with --bundled; a release build
# (OROS_REQUIRE_BRIDGE=1) stops without it, a test build goes on without one.
# oros-bridge.bundled beside each program: orOS updates it, not the bridge.
BRIDGE_TAG="$(gh release view --repo koulaxizis/oros-bridge --json tagName -q .tagName)"
if [ "$(printf '%s\n' 0.8.0 "${BRIDGE_TAG#v}" | sort -V | head -1)" != 0.8.0 ]; then
  echo "orOS Bridge $BRIDGE_TAG is older than 0.8.0 (no --bundled)" >&2
  if [ -n "${OROS_REQUIRE_BRIDGE:-}" ]; then exit 1; fi
  echo "warning: this build has no orOS Bridge" >&2
  BRIDGE_TAG=""
else
  TMP="$(mktemp -d)"
  gh release download "$BRIDGE_TAG" --repo koulaxizis/oros-bridge --dir "$TMP" \
    --pattern oros-bridge-windows-x64.zip --pattern oros-bridge-linux-x64.tar.gz --pattern SHA256SUMS.txt
  # the Windows job writes CRLF lines, the Linux job LF
  tr -d '\r' < "$TMP/SHA256SUMS.txt" | grep -E '  (oros-bridge-windows-x64\.zip|oros-bridge-linux-x64\.tar\.gz)$' > "$TMP/sums"
  [ "$(wc -l < "$TMP/sums")" -eq 2 ]
  (cd "$TMP" && sha256sum -c sums)
  mkdir -p "$FULL/bridge/windows" "$TMP/linux"
  unzip -q "$TMP/oros-bridge-windows-x64.zip" -d "$FULL/bridge/windows"
  tar -xzf "$TMP/oros-bridge-linux-x64.tar.gz" -C "$TMP/linux"
  mv "$TMP/linux/oros-bridge" "$FULL/bridge/linux"
  chmod +x "$FULL/bridge/linux/oros-bridge"
  ls "$FULL/bridge/windows/oros-bridge.exe" "$FULL/bridge/windows/oros-bridge-asio.dll" \
    "$FULL/bridge/windows/LICENSE-asio.txt" "$FULL/bridge/linux/oros-bridge" >/dev/null
  for d in windows linux; do
    printf 'orOS Portable %s carries this orOS Bridge; a new orOS brings a new one.\n' "$VERSION" > "$FULL/bridge/$d/oros-bridge.bundled"
  done
  # The ASIO Compatible logo goes wherever ASIO is named (Steinberg's guidelines).
  curl -fsSL --retry 3 -o "$TMP/asio-compatible.png" \
    "https://raw.githubusercontent.com/koulaxizis/oros-bridge/$BRIDGE_TAG/asio/logo/asio-compatible-bw.png"
  rm -rf "$TMP/linux"
  BRIDGE_TMP="$TMP"
fi

echo "== licences and sources"
# Both editions carry the licences and a manifest of the exact sources, so a
# copy sold on a stick meets the GPL/LGPL source terms (see WRITTEN-OFFER.txt).
"$ROOT/scripts/sources-manifest.sh" "$VERSION" "$OROS_COMMIT" "$WIN_TAG" "$LIN_TAG" > "$OUT/SOURCES.txt"
printf '%s\n' "" "Full edition and installers also carry:" \
  "  system/  orOS USB System extension (MIT), https://github.com/koulaxizis/oros-usb/tree/$USB_COMMIT/system/extension" \
  "  ubol/    ${UBOL%% ->*} (GPL-3.0), https://github.com/uBlockOrigin/uBOL-home/releases" \
  "  addon/   $ADDON (MIT), https://github.com/koulaxizis/oros/tree/$OROS_COMMIT/extension, its address preset to this launcher" \
  "  tor-browser-developers.gpg  Tor Browser's public signing key (EF6E 286D DA85 EA2A 4BA7 DE68 4E2C 6E87 9329 8290)," \
  "    for the Tor button; Tor Browser itself is not carried, the button downloads it from torproject.org" >> "$OUT/SOURCES.txt"
if [ -n "$BRIDGE_TAG" ]; then
  printf '%s\n' "" "Full edition and installers also carry orOS Bridge (bridge/), $BRIDGE_TAG:" \
    "  oros-bridge, oros-bridge.exe  MIT, https://github.com/koulaxizis/oros-bridge/tree/$BRIDGE_TAG" \
    "  oros-bridge-asio.dll (Windows)  GPL-3.0, its source in asio/ of the same tree, built with the" \
    "    Steinberg ASIO SDK 2.3.4 under the SDK's GPLv3 option:" \
    "    https://download.steinberg.net/sdk_downloads/ASIO-SDK_2.3.4_2025-10-15.zip" \
    "    and JSON for Modern C++ 3.11.3 (MIT): https://github.com/nlohmann/json/releases/download/v3.11.3/json.tar.xz" \
    "    ASIO is a registered trademark of Steinberg Media Technologies GmbH (logo: LICENSES/asio-compatible.png)." \
    "  Its other parts (VST 3 SDK, CLAP, miniaudio) are listed in LICENSES/orOS-Bridge-THIRD_PARTY.md." >> "$OUT/SOURCES.txt"
fi
printf 'VERSION=%q\nOROS_COMMIT=%q\nWIN_TAG=%q\nLIN_TAG=%q\nCHROMIUM=%q\nBRIDGE_TAG=%q\n' \
  "$VERSION" "$OROS_COMMIT" "$WIN_TAG" "$LIN_TAG" "${LIN_TAG%%-*}" "$BRIDGE_TAG" > "$OUT/sources.env"
for dir in "$LITE" "$FULL"; do
  mkdir -p "$dir/LICENSES"
  cp "$ROOT/LICENSE" "$dir/LICENSES/orOS-Portable-MIT.txt"
  if [ -f "$dir/app/LICENSE" ]; then cp "$dir/app/LICENSE" "$dir/LICENSES/orOS-MIT.txt"; fi
  cp "$ROOT/packaging/CREDITS.txt" "$dir/LICENSES/CREDITS.txt"
  # The launcher is a Go program: the Go standard library's licence goes with it,
  # and those of the Go modules it is built with (go.mod: the tray icon, and
  # the Tor button's signature check and .tar.xz unpacking).
  cp "$(go env GOROOT)/LICENSE" "$dir/LICENSES/Go-BSD.txt"
  for m in "fyne.io/systray:systray-Apache-2.0.txt" "github.com/godbus/dbus/v5:godbus-BSD-2.txt" \
           "golang.org/x/sys:golang-x-sys-BSD.txt" "github.com/ProtonMail/go-crypto:go-crypto-BSD.txt" \
           "github.com/cloudflare/circl:circl-BSD.txt" "golang.org/x/crypto:golang-x-crypto-BSD.txt" \
           "github.com/ulikunitz/xz:xz-BSD.txt"; do
    mdir="$(cd "$ROOT/launcher" && go list -m -f '{{.Dir}}' "${m%%:*}")"
    cp "$mdir/LICENSE" "$dir/LICENSES/${m#*:}"
  done
  cp "$OUT/SOURCES.txt" "$dir/SOURCES.txt"
done
# The browser packages carry no licence files of their own (the full list of
# Chromium's components is built in at chrome://credits), so fetch the two
# top-level licences for this exact version.
CHROMIUM="${LIN_TAG%%-*}"
UG_TAG="$(echo "$LIN_TAG" | cut -d- -f1-2)"
curl -fsSL --retry 3 "https://raw.githubusercontent.com/ungoogled-software/ungoogled-chromium/$UG_TAG/LICENSE" \
  -o "$FULL/LICENSES/ungoogled-chromium-BSD.txt"
curl -fsSL --retry 3 "https://chromium.googlesource.com/chromium/src/+/refs/tags/$CHROMIUM/LICENSE?format=TEXT" |
  base64 -d > "$FULL/LICENSES/Chromium-BSD.txt"
grep -q "Copyright" "$FULL/LICENSES/Chromium-BSD.txt"
printf '%s\n' "The complete list of the open-source components inside the browser, with" \
  "their licences, is built into it: open chrome://credits in the bundled browser." \
  > "$FULL/LICENSES/Chromium-components.txt"
if [ -n "$BRIDGE_TAG" ]; then
  cp "$FULL/bridge/windows/LICENSE" "$FULL/LICENSES/orOS-Bridge-MIT.txt"
  cp "$FULL/bridge/windows/LICENSE-asio.txt" "$FULL/LICENSES/orOS-Bridge-ASIO-module-GPL-3.0.txt"
  cp "$FULL/bridge/windows/THIRD_PARTY.md" "$FULL/LICENSES/orOS-Bridge-THIRD_PARTY.md"
  cp "$BRIDGE_TMP/asio-compatible.png" "$FULL/LICENSES/asio-compatible.png"
  rm -rf "$BRIDGE_TMP"
fi

cat "$OUT/SOURCES.txt"
ls "$FULL/LICENSES"

echo "== zips"
(cd "$OUT" && zip -qry "orOS-Portable-$VERSION.zip" orOS-Portable && zip -qry "orOS-Portable-lite-$VERSION.zip" orOS-Portable-lite)
# A .sha256 beside each zip, like the installers (sha256sum -c checks it).
(cd "$OUT" && for f in "orOS-Portable-$VERSION.zip" "orOS-Portable-lite-$VERSION.zip"; do sha256sum "$f" > "$f.sha256"; done)
(cd "$OUT" && sha256sum -c ./*.zip.sha256)
ls -lh "$OUT"/*.zip "$OUT"/*.zip.sha256
