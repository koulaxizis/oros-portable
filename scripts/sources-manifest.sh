#!/usr/bin/env bash
# Prints SOURCES.txt: the exact source of every program in an orOS Portable build.
# Usage: sources-manifest.sh <version> <oros-commit> <ugc-windows-tag> <ugc-linux-tag>
set -euo pipefail
VERSION="$1" OROS_COMMIT="$2" WIN_TAG="$3" LIN_TAG="$4"
REL="https://github.com/koulaxizis/oros-portable/releases/tag/v$VERSION"
CHROMIUM="${LIN_TAG%%-*}"
cat <<TXT
orOS Portable $VERSION: sources
================================

Every program on this stick is free software. The complete source code of
this exact build is listed below. Copies of the third-party sources are also
attached to the release page (files ending in -src):
  $REL

orOS Portable launcher (MIT)
  https://github.com/koulaxizis/oros-portable/tree/v$VERSION

orOS web app (MIT), commit $OROS_COMMIT
  https://github.com/koulaxizis/oros/tree/$OROS_COMMIT

ungoogled-chromium for Windows, release $WIN_TAG (BSD-3-Clause, with LGPL and
other open-source components; full list at chrome://credits in the browser)
  https://github.com/ungoogled-software/ungoogled-chromium-windows/tree/$WIN_TAG

ungoogled-chromium for Linux, release $LIN_TAG
  https://github.com/ungoogled-software/ungoogled-chromium-portablelinux/tree/$LIN_TAG

Both are built from Chromium $CHROMIUM with the ungoogled-chromium patches:
  https://github.com/ungoogled-software/ungoogled-chromium
  https://commondatastorage.googleapis.com/chromium-browser-official/chromium-$CHROMIUM.tar.xz

Licences: see the LICENSES folder. Written offer for source code: WRITTEN-OFFER.txt.
TXT
