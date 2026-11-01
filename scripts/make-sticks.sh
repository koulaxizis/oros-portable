#!/usr/bin/env bash
# Writes orOS Portable to one or more USB sticks at once, then verifies every
# file against a checksum list after re-reading it from the stick.
#
# Usage: sudo scripts/make-sticks.sh [--yes] [--label NAME] SOURCE DEVICE...
#   SOURCE  orOS-Portable-<version>.zip (checked against the .sha256 beside
#           it, when there is one) or an unpacked orOS-Portable folder
#   DEVICE  whole disks, e.g. /dev/sdb /dev/sdc (everything on them is erased)
#   --yes   skip the typed confirmation (for scripted runs)
#   --label volume name, default OROS (max 11 characters)
#
# Each stick gets one MBR partition formatted as NTFS (Windows and Linux can
# both run programs from it, and on Linux the browser needs the symbolic
# links that exFAT and FAT cannot store), the orOS Portable files and
# SHA256SUMS.txt.
# Needs: sfdisk, wipefs, mkntfs (ntfs-3g), unzip, sha256sum.
set -euo pipefail

YES="" LABEL="OROS" ALLOW_ANY="${OROS_STICKS_ALLOW_ANY:-}"
while [ $# -gt 0 ]; do
  case "$1" in
    --yes) YES=1; shift ;;
    --label) LABEL="$2"; shift 2 ;;
    -h|--help) sed -n '2,16p' "$0"; exit 0 ;;
    *) break ;;
  esac
done
[ $# -ge 2 ] || { sed -n '2,16p' "$0"; exit 2; }
[ "$(id -u)" = 0 ] || { echo "run as root (sudo)"; exit 2; }
for cmd in sfdisk wipefs mkntfs unzip sha256sum lsblk; do
  command -v "$cmd" >/dev/null || { echo "missing $cmd"; exit 2; }
done
[ ${#LABEL} -le 11 ] || { echo "label longer than 11 characters"; exit 2; }

SOURCE="$1"; shift
WORK="$(mktemp -d)"
trap 'umount "$WORK"/mnt-* 2>/dev/null || true; rm -rf "$WORK"' EXIT

# Unpack once, find the folder that holds the launcher, list its checksums.
if [ -d "$SOURCE" ]; then
  FILES="$(cd "$SOURCE" && pwd)"
else
  # A release zip comes with a .sha256 beside it: check the download first.
  if [ -f "$SOURCE.sha256" ]; then
    want="$(tr -d '\r' < "$SOURCE.sha256" | cut -d' ' -f1)"
    have="$(sha256sum "$SOURCE" | cut -d' ' -f1)"
    [ "$want" = "$have" ] || { echo "$SOURCE does not match $SOURCE.sha256 (damaged download?)"; exit 2; }
    echo "zip checksum OK"
  fi
  unzip -q "$SOURCE" -d "$WORK/src"
  FILES="$(dirname "$(find "$WORK/src" -name orOS.exe -print -quit)")"
fi
[ -f "$FILES/orOS.exe" ] && [ -f "$FILES/orOS" ] || { echo "no orOS launcher in $SOURCE"; exit 2; }
(cd "$FILES" && find . -type f ! -name SHA256SUMS.txt -print0 | sort -z | xargs -0 sha256sum) > "$WORK/SHA256SUMS.txt"
NEED=$(du -sb "$FILES" | cut -f1)
echo "source: $FILES ($((NEED / 1024 / 1024)) MiB, $(wc -l < "$WORK/SHA256SUMS.txt") files)"

# Safety checks: whole disks only, removable USB, nothing mounted from them,
# big enough, and not larger than any stick we would sell (256 GB).
for dev in "$@"; do
  [ -b "$dev" ] || { echo "$dev: not a block device"; exit 2; }
  [ "$(lsblk -dno TYPE "$dev")" = disk ] || [ "$(lsblk -dno TYPE "$dev")" = loop ] || { echo "$dev: not a whole disk"; exit 2; }
  rm_flag="$(lsblk -dno RM "$dev" | tr -d ' ')"; tran="$(lsblk -dno TRAN "$dev" | tr -d ' ')"
  if [ -z "$ALLOW_ANY" ] && { [ "$rm_flag" != 1 ] || [ "$tran" != usb ]; }; then
    echo "$dev: not a removable USB disk (RM=$rm_flag TRAN=$tran); refusing"; exit 2
  fi
  if lsblk -nro MOUNTPOINT "$dev" | grep -qE '^/(boot.*)?$'; then
    echo "$dev: holds a system mount; refusing"; exit 2
  fi
  size=$(lsblk -bdno SIZE "$dev")
  [ "$size" -gt $((NEED + 64 * 1024 * 1024)) ] || { echo "$dev: too small"; exit 2; }
  [ "$size" -le $((256 * 1000 * 1000 * 1000)) ] || { echo "$dev: larger than 256 GB; refusing"; exit 2; }
done

echo
echo "These disks will be ERASED:"
for dev in "$@"; do
  printf '  %-14s %8s  %s\n' "$dev" "$(lsblk -dno SIZE "$dev" | tr -d ' ')" "$(lsblk -dno VENDOR,MODEL "$dev" | xargs)"
done
if [ -z "$YES" ]; then
  read -r -p "Type ERASE to continue: " answer
  [ "$answer" = ERASE ] || { echo "cancelled"; exit 1; }
fi

part1() { # first partition node of a disk: sdb -> sdb1, loop0/mmcblk0/nvme0n1 -> ...p1
  case "$1" in *[0-9]) echo "${1}p1" ;; *) echo "${1}1" ;; esac
}

write_one() { # device log
  local dev="$1" mnt="$WORK/mnt-$(basename "$1")" part
  part="$(part1 "$dev")"
  mkdir -p "$mnt"
  lsblk -nro NAME,MOUNTPOINT "$dev" | awk '$2!=""{print "/dev/"$1}' | xargs -r umount
  wipefs -qa "$dev"
  echo 'type=7' | sfdisk -q --label dos "$dev"
  # Let udev create the partition node (losetup -P / partx for loop devices).
  partx -u "$dev" 2>/dev/null || true
  udevadm settle 2>/dev/null || true
  for _ in $(seq 50); do [ -b "$part" ] && break; sleep 0.1; done
  [ -b "$part" ] || { echo "no partition node $part"; return 1; }
  mkntfs -Q -q -L "$LABEL" "$part" >/dev/null
  mount "$part" "$mnt"
  cp -r "$FILES"/. "$mnt"/
  cp "$WORK/SHA256SUMS.txt" "$mnt/SHA256SUMS.txt"
  sync
  umount "$mnt"
  # Re-read from the stick itself, not the page cache.
  blockdev --flushbufs "$part" 2>/dev/null || true
  echo 3 > /proc/sys/vm/drop_caches 2>/dev/null || true
  mount -o ro "$part" "$mnt"
  (cd "$mnt" && sha256sum --quiet -c SHA256SUMS.txt)
  umount "$mnt"
}

echo
declare -A PIDS
for dev in "$@"; do
  log="$WORK/$(basename "$dev").log"
  write_one "$dev" > "$log" 2>&1 &
  PIDS[$dev]=$!
  echo "writing $dev"
done

FAIL=0
echo
for dev in "$@"; do
  if wait "${PIDS[$dev]}"; then
    echo "OK    $dev"
  else
    echo "FAIL  $dev"; sed 's/^/      /' "$WORK/$(basename "$dev").log"; FAIL=1
  fi
done
exit $FAIL
