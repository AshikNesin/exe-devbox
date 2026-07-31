#!/usr/bin/env bash
#
# exebox installer — downloads the latest release binary for linux/amd64.
# Usage:
#   curl -sfL -H 'Accept: application/vnd.github.raw' \
#       -o /tmp/install.sh \
#       https://github.int.exe.xyz/api/v3/repos/AshikNesin/exebox/contents/install.sh \
#     && bash /tmp/install.sh
#
set -euo pipefail

REPO="AshikNesin/exebox"
API="https://github.int.exe.xyz/api/v3"

# Resolve latest version
VERSION=$(curl -sf "$API/repos/$REPO/releases/latest" | python3 -c "import sys,json;print(json.load(sys.stdin)['tag_name'])" 2>/dev/null || echo "")
if [ -z "$VERSION" ]; then
  echo "✗ could not determine latest version" >&2; exit 1
fi
echo "→ installing exebox $VERSION"

# Download binary via Contents API (raw)
DEST="${DEST:-$HOME/.local/bin/exebox}"
mkdir -p "$(dirname "$DEST")"
curl -sfL -H 'Accept: application/vnd.github.raw' \
  -o "$DEST" \
  "$API/repos/$REPO/contents/releases/$VERSION/exebox-linux-amd64"
chmod +x "$DEST"

echo "✓ installed to $DEST"
echo "  Run: exebox setup"
