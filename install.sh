#!/usr/bin/env bash
#
# exebox installer — downloads the latest release binary for linux/amd64.
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/AshikNesin/exebox/main/install.sh | bash
#
set -euo pipefail

REPO="AshikNesin/exebox"
API="https://api.github.com"

# Resolve latest version
VERSION=$(curl -sf "$API/repos/$REPO/releases/latest" | python3 -c "import sys,json;print(json.load(sys.stdin)['tag_name'])" 2>/dev/null || echo "")
if [ -z "$VERSION" ]; then
  echo "✗ could not determine latest version" >&2; exit 1
fi
echo "→ installing exebox $VERSION"

# Download binary from GitHub Release assets
DEST="${DEST:-$HOME/.local/bin/exebox}"
mkdir -p "$(dirname "$DEST")"
curl -sfL -o "$DEST" \
  "https://github.com/$REPO/releases/download/$VERSION/exebox-linux-amd64"
chmod +x "$DEST"

echo "✓ installed to $DEST"
echo "  Run: exebox setup"