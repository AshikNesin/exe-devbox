#!/usr/bin/env bash
#
# devbox installer — downloads the latest release binary for linux/amd64.
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/AshikNesin/exe-devbox/main/install.sh | bash
#
set -euo pipefail

REPO="AshikNesin/exe-devbox"
API="https://api.github.com"

# Resolve latest version
VERSION=$(curl -sf "$API/repos/$REPO/releases/latest" | python3 -c "import sys,json;print(json.load(sys.stdin)['tag_name'])" 2>/dev/null || echo "")
if [ -z "$VERSION" ]; then
  echo "✗ could not determine latest version" >&2; exit 1
fi
echo "→ installing devbox $VERSION"

# Download binary from GitHub Release assets
DEST="${DEST:-$HOME/.local/bin/devbox}"
mkdir -p "$(dirname "$DEST")"
curl -sfL -o "$DEST" \
  "https://github.com/$REPO/releases/download/$VERSION/devbox-linux-amd64"
chmod +x "$DEST"

# Create exe-devbox symlink alias
ln -sf devbox "$(dirname "$DEST")/exe-devbox"

echo "✓ installed to $DEST"
echo "  Run: devbox setup"