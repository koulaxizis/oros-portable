#!/usr/bin/env bash
# Builds the Linux installers from the full portable folder:
#   out/installers/orOS-<version>-x86_64.AppImage (+ .zsync for updates)
#   out/installers/oros_<version>_amd64.deb
# and stages out/installers/windows/ for the Windows setup (built by iscc
# on a Windows runner, see packaging/windows/orOS.iss).
# Needs: curl, dpkg-deb, and the output of scripts/build.sh.
set -euo pipefail

VERSION="${1:-0.0.0-dev}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
FULL="${2:-$ROOT/out/orOS-Portable}"
OUT="$ROOT/out/installers"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
rm -rf "$OUT" && mkdir -p "$OUT"
[ -x "$FULL/orOS" ] && [ -f "$FULL/app/index.html" ] && [ -x "$FULL/browser/linux/chrome" ]
# app/.oros-commit (written by the launcher's update in build.sh) tells the
# automatic update which orOS an install starts with.
[ -f "$FULL/app/.oros-commit" ] || echo "warning: no app/.oros-commit; the first automatic update downloads orOS again" >&2

# Program files shared by every installer: the launcher, orOS, licences and
# the "installed" marker that moves the data to the user's own folder.
# chromedriver is a test tool and is left out.
stage() { # stage <dest> <os: linux|windows>
  local dest="$1" os="$2"
  mkdir -p "$dest/browser"
  cp -r "$FULL/app" "$FULL/LICENSES" "$FULL/SOURCES.txt" "$FULL/WRITTEN-OFFER.txt" "$dest/"
  cp -r "$FULL/browser/$os" "$dest/browser/$os"
  if [ -d "$FULL/system" ]; then cp -r "$FULL/system" "$dest/"; fi
  if [ -d "$FULL/ubol" ]; then cp -r "$FULL/ubol" "$dest/"; fi
  # The orOS add-on for the Internet window (launcher/sysweb.go)
  if [ -d "$FULL/addon" ]; then cp -r "$FULL/addon" "$dest/"; fi
  # Tor Browser's signing key (launcher/tor.go)
  if [ -f "$FULL/tor-browser-developers.gpg" ]; then cp "$FULL/tor-browser-developers.gpg" "$dest/"; fi
  # orOS Bridge for this system only (launcher/bridge.go)
  if [ -d "$FULL/bridge/$os" ]; then mkdir -p "$dest/bridge" && cp -r "$FULL/bridge/$os" "$dest/bridge/$os"; fi
  rm -f "$dest/browser/$os/chromedriver" "$dest/browser/$os/chromedriver.exe"
  if [ "$os" = linux ]; then cp "$FULL/orOS" "$dest/"; else cp "$FULL/orOS.exe" "$dest/"; fi
  printf 'orOS %s, installed by its setup. Delete this file to run portable.\n' "$VERSION" > "$dest/installed"
}

echo "== Windows staging"
stage "$OUT/windows/files" windows
cp "$ROOT/packaging/windows/orOS.iss" "$ROOT/packaging/windows/oros.ico" "$ROOT/packaging/windows/Greek.isl" "$OUT/windows/"
echo "$VERSION" > "$OUT/windows/VERSION"

echo "== AppImage"
APPDIR="$WORK/orOS.AppDir"
stage "$APPDIR/usr/lib/oros" linux
# The AppImage runtime in front of the file is linked with libfuse (LGPL-2.1).
cp "$ROOT/packaging/linux/licenses/"* "$APPDIR/usr/lib/oros/LICENSES/"
cp "$ROOT/packaging/linux/AppRun" "$APPDIR/AppRun"
cp "$ROOT/packaging/linux/oros.desktop" "$APPDIR/oros.desktop"
cp "$ROOT/packaging/linux/oros.png" "$APPDIR/oros.png"
mkdir -p "$APPDIR/usr/share/icons/hicolor/512x512/apps"
cp "$ROOT/packaging/linux/oros.png" "$APPDIR/usr/share/icons/hicolor/512x512/apps/oros.png"
TOOL="$WORK/appimagetool"
curl -fsSL --retry 3 -o "$TOOL" \
  https://github.com/AppImage/appimagetool/releases/download/1.9.0/appimagetool-x86_64.AppImage
echo "46fdd785094c7f6e545b61afcfb0f3d98d8eab243f644b4b17698c01d06083d1  $TOOL" | sha256sum -c -
chmod +x "$TOOL"
IMG="$OUT/orOS-$VERSION-x86_64.AppImage"
# The update information lets AppImageUpdate fetch only the changed blocks
# of a newer release.
(cd "$OUT" && ARCH=x86_64 APPIMAGE_EXTRACT_AND_RUN=1 "$TOOL" --no-appstream \
  -u "gh-releases-zsync|koulaxizis|oros-portable|latest|orOS-*-x86_64.AppImage.zsync" \
  "$APPDIR" "$IMG")
chmod +x "$IMG"

echo "== .deb"
DEB="$WORK/deb"
stage "$DEB/opt/oros" linux
mkdir -p "$DEB/usr/bin" "$DEB/usr/share/applications" "$DEB/usr/share/icons/hicolor/512x512/apps" \
  "$DEB/usr/share/oros" "$DEB/usr/share/doc/oros" "$DEB/DEBIAN"
ln -s /opt/oros/orOS "$DEB/usr/bin/oros"
cp "$ROOT/packaging/linux/oros.desktop" "$DEB/usr/share/applications/oros.desktop"
cp "$ROOT/packaging/linux/oros.png" "$DEB/usr/share/icons/hicolor/512x512/apps/oros.png"
cp "$ROOT/packaging/linux/apparmor-oros-chromium" "$DEB/usr/share/oros/"
{
  printf '%s\n' "orOS (this package): MIT, see below." \
    "/opt/oros/browser/linux is ungoogled-chromium, BSD-3-Clause, with open-source" \
    "components under their own licences (LGPL, MPL, Apache-2.0, MIT and more);" \
    "the launcher is built with Go (BSD-3-Clause); /opt/oros/bridge is orOS Bridge," \
    "MIT. Credits and every licence" \
    "text: /opt/oros/LICENSES (CREDITS.txt first). Sources: /opt/oros/SOURCES.txt," \
    "written offer: /opt/oros/WRITTEN-OFFER.txt. The orOS web app's own credits:" \
    "/opt/oros/app/CREDITS.md." ""
  cat "$ROOT/LICENSE"
} > "$DEB/usr/share/doc/oros/copyright"
cp "$ROOT/packaging/linux/debian/postinst" "$ROOT/packaging/linux/debian/postrm" "$DEB/DEBIAN/"
# Debian versions may not contain "-<sha>" from test builds as is; "~"
# sorts them below the real release.
DEBVER="${VERSION/-/\~}"
cat > "$DEB/DEBIAN/control" <<CONTROL
Package: oros
Version: $DEBVER
Architecture: amd64
Maintainer: koulaxizis <koulaxizis@users.noreply.github.com>
Installed-Size: $(du -sk "$DEB" | cut -f1)
Depends: libnss3, libnspr4, libgbm1, libdrm2, libxkbcommon0, libdbus-1-3, libexpat1,
 libx11-6, libxcb1, libxcomposite1, libxdamage1, libxext6, libxfixes3, libxrandr2,
 libpango-1.0-0, libcairo2, libasound2 | libasound2t64, libgtk-3-0 | libgtk-3-0t64,
 libatk-bridge2.0-0 | libatk-bridge2.0-0t64, libatspi2.0-0 | libatspi2.0-0t64,
 libcups2 | libcups2t64
Section: utils
Priority: optional
Homepage: https://useoros.online
Description: orOS, the offline-first workspace
 Notes, calendar, files, office apps and games in one private workspace
 that works without internet. Runs the orOS web app from this computer
 in a bundled ungoogled-chromium window; data stays in ~/.local/share/oros
 and syncs encrypted through your own Dropbox when you choose to.
CONTROL
dpkg-deb --root-owner-group -Zxz --build "$DEB" "$OUT/oros_${DEBVER}_amd64.deb"

# The launcher checks each download against its .sha256 file.
(cd "$OUT" && for f in *.AppImage *.deb; do sha256sum "$f" > "$f.sha256"; done)
ls -lh "$OUT"
