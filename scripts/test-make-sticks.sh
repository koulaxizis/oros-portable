#!/usr/bin/env bash
# Tests make-sticks.sh on two loop devices with a fake orOS Portable folder:
# both must be written and verified, and a flipped byte must be caught.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
T="$(mktemp -d)"
DEVS=()
cleanup() {
  umount "$T/m" 2>/dev/null || true
  for d in "${DEVS[@]}"; do losetup -d "$d" 2>/dev/null || true; done
  rm -rf "$T"
}
trap cleanup EXIT

mkdir -p "$T/orOS-Portable/app/apps" "$T/m"
echo exe > "$T/orOS-Portable/orOS.exe"
printf '#!/bin/sh\n' > "$T/orOS-Portable/orOS"
head -c 3000000 /dev/urandom > "$T/orOS-Portable/app/apps/big.bin"
echo "Ελληνικά" > "$T/orOS-Portable/app/Σημειώσεις.txt"
(cd "$T" && zip -qr portable.zip orOS-Portable)

for i in 1 2; do
  truncate -s 128M "$T/disk$i.img"
  DEVS+=("$(losetup -fP --show "$T/disk$i.img")")
done

OROS_STICKS_ALLOW_ANY=1 "$ROOT/scripts/make-sticks.sh" --yes "$T/portable.zip" "${DEVS[@]}"

part="${DEVS[0]}p1"
[ "$(blkid -o value -s LABEL "$part")" = OROS ] || { echo "label missing"; exit 1; }
mount "$part" "$T/m"
grep -q "Σημειώσεις.txt" "$T/m/SHA256SUMS.txt"
echo tampered >> "$T/m/app/apps/big.bin"
if (cd "$T/m" && sha256sum --quiet -c SHA256SUMS.txt) >/dev/null 2>&1; then
  echo "tampering not detected"; exit 1
fi
umount "$T/m"

# Refuses a non-USB disk without the test override.
if "$ROOT/scripts/make-sticks.sh" --yes "$T/portable.zip" "${DEVS[1]}" >/dev/null 2>&1; then
  echo "non-USB disk accepted"; exit 1
fi
echo "make-sticks: all checks passed"
