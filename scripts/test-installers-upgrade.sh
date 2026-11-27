#!/usr/bin/env bash
# Upgrade test for the Linux installers: the latest released .deb and
# AppImage first, then the new ones over them, with the same home folder.
# Checks that the data folder (~/.local/share/oros), the browser profile
# folder in it (where orOS keeps its own data) and a setting
# (no-auto-update) survive, and that the new program starts and serves
# its own orOS. Run after test-installers.sh (which removes its .deb).
# Needs sudo, curl and gh (GH_TOKEN).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$ROOT/out/installers"
NEW_DEB="$(ls "$OUT"/oros_*_amd64.deb)"
NEW_IMG="$(ls "$OUT"/orOS-*-x86_64.AppImage)"
OLD="$(mktemp -d)"
REPO="${GITHUB_REPOSITORY:-koulaxizis/oros-portable}"

# The latest release (what people have installed), checked against its .sha256 files.
gh release download --repo "$REPO" --dir "$OLD" --pattern 'oros_*_amd64.deb*' --pattern 'orOS-*-x86_64.AppImage' --pattern 'orOS-*-x86_64.AppImage.sha256'
(cd "$OLD" && for f in *.sha256; do sha256sum -c "$f"; done)
OLD_DEB="$(ls "$OLD"/oros_*_amd64.deb)"
OLD_IMG="$(ls "$OLD"/orOS-*-x86_64.AppImage)"
chmod +x "$OLD_IMG" "$NEW_IMG"
echo "upgrade: $(basename "$OLD_DEB") -> $(basename "$NEW_DEB"), $(basename "$OLD_IMG") -> $(basename "$NEW_IMG")"

app_version() { grep -o 'APP_VERSION = "[^"]*"' | head -1; }

# serve_once <home> <command...>: starts "serve", waits for the page, and
# prints the served APP_VERSION line; stops it again.
serve_once() {
  local home="$1"; shift
  if curl -fs http://localhost:47800/ >/dev/null; then echo "port 47800 is busy before the test" >&2; exit 1; fi
  HOME="$home" APPIMAGE_EXTRACT_AND_RUN=1 "$@" serve >> "$home/serve.log" 2>&1 &
  local pid=$! ok=""
  for _ in $(seq 30); do curl -fs http://localhost:47800/ >/dev/null && ok=1 && break; sleep 1; done
  local js=""
  [ -n "$ok" ] && js="$(curl -fs http://localhost:47800/shell.js || true)"
  # The AppImage runtime starts the launcher as its child.
  pkill -P "$pid" 2>/dev/null || true
  kill "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true
  for _ in $(seq 15); do curl -fs http://localhost:47800/ >/dev/null || break; sleep 1; done
  if [ -z "$ok" ]; then echo "$* does not serve http://localhost:47800/" >&2; tail -20 "$home/serve.log" >&2; exit 1; fi
  app_version <<<"$js"
}

# mark <home> <from>: the data and a setting, as a person's orOS has them.
mark() {
  local d="$1/.local/share/oros"
  test -f "$d/launcher.log" || { echo "the old orOS wrote no data folder" >&2; exit 1; }
  mkdir -p "$d/profile/Default"
  printf 'kept from %s' "$2" > "$d/upgrade-marker.txt"
  printf 'kept from %s' "$2" > "$d/profile/Default/upgrade-marker.txt"
}

# check_kept <home> <from>
check_kept() {
  local d="$1/.local/share/oros" f
  for f in upgrade-marker.txt profile/Default/upgrade-marker.txt; do
    [ "$(cat "$d/$f" 2>/dev/null)" = "kept from $2" ] || { echo "lost or changed in the upgrade: $f" >&2; exit 1; }
  done
  [ "$(cat "$d/no-auto-update" 2>/dev/null)" = "off for the upgrade test" ] || { echo "the upgrade changed the no-auto-update setting" >&2; exit 1; }
}

# settings <home>: automatic updates off, so only the packages change the program.
settings() {
  mkdir -p "$1/.local/share/oros"
  printf 'off for the upgrade test' > "$1/.local/share/oros/no-auto-update"
}

echo "== .deb"
home="$(mktemp -d)"; settings "$home"
sudo apt-get install -y -q "$OLD_DEB"
old_ver="$(dpkg-query -W -f '${Version}' oros)"
serve_once "$home" oros >/dev/null
mark "$home" "deb $old_ver"
# dpkg -i: a test build's version (0.0.0-<sha>) is lower than a release's,
# and a person's upgrade runs the same maintainer scripts (upgrade, not remove).
sudo dpkg -i "$NEW_DEB" || sudo apt-get install -f -y -q
new_ver="$(dpkg-query -W -f '${Version}' oros)"
[ "$new_ver" = "$(dpkg-deb -f "$NEW_DEB" Version)" ] || { echo "dpkg has oros $new_ver after the upgrade" >&2; exit 1; }
# the package version writes a test build's "-" as "~"
grep -q "^orOS ${new_ver/\~/-}," /opt/oros/installed
test -x /opt/oros/orOS && test -x /opt/oros/browser/linux/chrome && test -x /usr/bin/oros
check_kept "$home" "deb $old_ver"
served="$(serve_once "$home" oros)"
[ -n "$served" ] && [ "$served" = "$(app_version < /opt/oros/app/shell.js)" ] || { echo "serves '$served', the new package carries $(app_version < /opt/oros/app/shell.js)" >&2; exit 1; }
check_kept "$home" "deb $old_ver"
echo "ok: deb $old_ver -> $new_ver keeps the data and settings and serves $served"
sudo apt-get remove -y -q oros
test ! -e /opt/oros && test ! -e /usr/bin/oros
check_kept "$home" "deb $old_ver"
echo "ok: the upgraded deb removes cleanly, data kept"

echo "== AppImage"
home="$(mktemp -d)"; settings "$home"
serve_once "$home" "$OLD_IMG" >/dev/null
mark "$home" "$(basename "$OLD_IMG")"
# The AppImage's own update replaces the file (launcher/autoupdate.go
# replaceAppImage); the home folder is all that carries over.
served="$(serve_once "$home" "$NEW_IMG")"
x="$(mktemp -d)"
(cd "$x" && "$NEW_IMG" --appimage-extract usr/lib/oros/app/shell.js >/dev/null)
[ -n "$served" ] && [ "$served" = "$(app_version < "$x/squashfs-root/usr/lib/oros/app/shell.js")" ] || { echo "the new AppImage serves '$served'" >&2; exit 1; }
rm -rf "$x"
check_kept "$home" "$(basename "$OLD_IMG")"
echo "ok: $(basename "$OLD_IMG") -> $(basename "$NEW_IMG") keeps the data and settings and serves $served"
