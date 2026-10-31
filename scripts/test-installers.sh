#!/usr/bin/env bash
# Installs the .deb and runs the AppImage on this (Linux) machine, checks
# that each serves orOS from the installed copy with its data in a fresh
# home folder, then removes the .deb. Needs sudo and curl.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$ROOT/out/installers"
DEB="$(ls "$OUT"/oros_*_amd64.deb)"
IMG="$(ls "$OUT"/orOS-*-x86_64.AppImage)"

check_serves() { # check_serves <name> <command...>
  local name="$1"; shift
  local home; home="$(mktemp -d)"
  if curl -fs http://localhost:47800/ >/dev/null; then echo "port 47800 is busy before the test"; exit 1; fi
  HOME="$home" APPIMAGE_EXTRACT_AND_RUN=1 "$@" serve > "$home/serve.log" 2>&1 &
  local pid=$!
  trap 'kill '"$pid"' 2>/dev/null || true' EXIT
  for _ in $(seq 30); do curl -fs http://localhost:47800/ >/dev/null && break; sleep 1; done
  # Read whole pages first: grep -q closing the pipe early fails curl under pipefail.
  local page js
  page="$(curl -fs http://localhost:47800/)"
  js="$(curl -fs http://localhost:47800/shell.js)"
  grep -q "<title>orOS" <<<"$page"
  grep -q 'APP_VERSION = "' <<<"$js"
  test -f "$home/.local/share/oros/launcher.log"
  # The AppImage runtime starts the launcher as its child.
  pkill -P "$pid" 2>/dev/null || true
  kill "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true
  trap - EXIT
  echo "ok: $name serves orOS, data in ~/.local/share/oros"
}

sudo apt-get install -y -q "$DEB"
test -x /opt/oros/browser/linux/chrome && test -f /opt/oros/installed
for f in CREDITS.txt Go-BSD.txt Chromium-BSD.txt ungoogled-chromium-BSD.txt; do test -s "/opt/oros/LICENSES/$f"; done
grep -q ungoogled-chromium /usr/share/doc/oros/copyright
echo "ok: deb carries the licences"
if [ -e "$ROOT/out/orOS-Portable/bridge" ]; then
  # orOS Bridge (launcher/bridge.go): there, bundled, and it leaves updates to orOS
  b=/opt/oros/bridge/linux/oros-bridge
  test -x "$b" && test -f /opt/oros/bridge/linux/oros-bridge.bundled && test ! -e /opt/oros/bridge/windows
  for f in orOS-Bridge-MIT.txt orOS-Bridge-THIRD_PARTY.md; do test -s "/opt/oros/LICENSES/$f"; done
  "$b" --help | grep -q "orOS Bridge 0\."
  if "$b" --update > /tmp/bridge-update.log 2>&1; then echo "the bundled bridge updated itself"; exit 1; fi
  grep -q "orOS updates this copy" /tmp/bridge-update.log
  bhome="$(mktemp -d)"
  HOME="$bhome" "$b" --bundled --port 0 --code-file "$bhome/code" > "$bhome/bridge.log" 2>&1 &
  bpid=$!
  for _ in $(seq 20); do grep -q "is ready on 127.0.0.1:" "$bhome/bridge.log" && break; sleep 0.5; done
  kill "$bpid"; wait "$bpid" 2>/dev/null || true
  grep -q "is ready on 127.0.0.1:" "$bhome/bridge.log"
  echo "ok: deb carries orOS Bridge, bundled (no updates of its own), and it starts"
fi
check_serves deb oros
sudo apt-get remove -y -q oros
test ! -e /opt/oros && test ! -e /usr/bin/oros
echo "ok: deb removed cleanly"

chmod +x "$IMG"
# The runtime's LGPL notice travels inside the AppImage.
(x="$(mktemp -d)" && cd "$x" && "$IMG" --appimage-extract 'usr/lib/oros/LICENSES/*' >/dev/null &&
  test -s squashfs-root/usr/lib/oros/LICENSES/AppImage-runtime.txt &&
  test -s squashfs-root/usr/lib/oros/LICENSES/LGPL-2.1.txt && rm -rf "$x")
echo "ok: AppImage carries the runtime's licences"
check_serves AppImage "$IMG"
