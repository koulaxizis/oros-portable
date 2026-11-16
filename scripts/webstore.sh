#!/bin/sh
# Unpacks chromium-web-store (NeverDecaf, MIT) into the folder $1, from the
# latest release of its repository (github.com/NeverDecaf/chromium-web-store),
# checked against the SHA-256 GitHub lists for the file. ungoogled-chromium
# cannot install from the Chrome Web Store nor update what came from there;
# with this extension (and the launcher's --extension-mime-request-handling
# switch, launcher/extpolicy.go) the store's pages get an "Add to Chromium"
# button and installed extensions are checked for updates. The bundled
# browser reaches Google only when the user installs or updates something
# from the store. Prints the version.
#   scripts/webstore.sh OUT_DIR
set -eu
OUT="$1"
API="https://api.github.com/repos/NeverDecaf/chromium-web-store/releases/latest"
T="$(mktemp -d)"
trap 'rm -rf "$T"' EXIT
TOKEN="${GITHUB_TOKEN:-${GH_TOKEN:-}}"
if [ -n "$TOKEN" ]; then
	curl -fsSL --retry 3 -H "Authorization: Bearer $TOKEN" -o "$T/release.json" "$API"
else
	curl -fsSL --retry 3 -o "$T/release.json" "$API"
fi
jq -r '.tag_name as $t | [.assets[] | select(.name | ascii_downcase | endswith(".crx"))][0]
	| if . == null then error("no .crx in the chromium-web-store release \($t)") else "\($t) \(.browser_download_url) \((.digest // "") | sub("^sha256:"; ""))" end' \
	"$T/release.json" > "$T/asset"
read -r TAG URL SUM < "$T/asset"
case "$URL" in
	https://github.com/NeverDecaf/chromium-web-store/releases/download/*) ;;
	*) echo "unexpected download address $URL" >&2; exit 1 ;;
esac
[ -n "$SUM" ] || { echo "GitHub lists no SHA-256 for chromium-web-store $TAG" >&2; exit 1; }
curl -fsSL --retry 3 -o "$T/ws.crx" "$URL"
echo "$SUM  $T/ws.crx" | sha256sum -c --quiet
# A CRX3 file is "Cr24", version 3, the header's length, the header, then a zip.
[ "$(head -c 4 "$T/ws.crx")" = "Cr24" ] || { echo "chromium-web-store $TAG is not a CRX file" >&2; exit 1; }
HLEN="$(od -An -tu4 -j8 -N4 "$T/ws.crx" | tr -d ' ')"
tail -c +"$((12 + HLEN + 1))" "$T/ws.crx" > "$T/ws.zip"
rm -rf "$OUT" && mkdir -p "$OUT"
unzip -q "$T/ws.zip" -d "$OUT"
jq -e '.manifest_version == 3 and (.key | length > 0)' "$OUT/manifest.json" > /dev/null ||
	{ echo "chromium-web-store $TAG: not a Manifest V3 extension with a key" >&2; exit 1; }
# Its own update check would look for a newer .crx; orOS Portable brings the
# next one with its own update, so the copy stays as built.
jq 'del(.update_url)' "$OUT/manifest.json" > "$OUT/manifest.json.new" && mv "$OUT/manifest.json.new" "$OUT/manifest.json"
curl -fsSL --retry 3 -o "$OUT/LICENSE" "https://raw.githubusercontent.com/NeverDecaf/chromium-web-store/$TAG/LICENSE"
grep -q "MIT License" "$OUT/LICENSE"
echo "chromium-web-store $TAG -> $OUT"
