#!/usr/bin/env bash
#
# Creates an exe.dev HTTPS API token scoped to ONLY "domain add".
# The token can add custom domains but cannot do anything else (no rm, ls,
# new, share, ssh-key, etc). Used by exebox to automate domain registration.
#
# Usage:  bash scripts/create-scoped-api-key.sh
#
# Ref: https://exe.dev/docs/https-api-local-key.md
#
set -euo pipefail

KEY_FILE="${KEY_FILE:-$HOME/.ssh/exe_dev_domain_add}"

# --- 1. Create a dedicated SSH key (revocable independently) ---
if [ ! -f "$KEY_FILE" ]; then
  echo "→ generating SSH key: $KEY_FILE"
  ssh-keygen -t ed25519 -C "exebox-domain-add" -f "$KEY_FILE" -N ""
else
  echo "✓ key exists: $KEY_FILE"
fi

# --- 2. Add the public key to exe.dev (if not already added) ---
echo "→ adding public key to exe.dev ..."
cat "${KEY_FILE}.pub" | ssh exe.dev ssh-key add || echo "  (may already be added — that's fine)"

# --- 3. Build the scoped token ---
# cmds: only "domain add" is permitted.
# exp:  Jan 1 2100 (4102444800). Change to a nearer timestamp to shorten.
b64url() { tr -d '\n=' | tr '+/' '-_'; }

PERMISSIONS='{"cmds":["domain add"],"exp":4102444800}'
PAYLOAD=$(printf '%s' "$PERMISSIONS" | base64 | b64url)
SIG=$(printf '%s' "$PERMISSIONS" | ssh-keygen -Y sign -f "$KEY_FILE" -n v0@exe.dev)
SIGBLOB=$(echo "$SIG" | sed '1d;$d' | b64url)
TOKEN="exe0.$PAYLOAD.$SIGBLOB"

echo ""
echo "✓ token created (scoped to 'domain add' only)"
echo ""
echo "Add this to your shell profile / exebox env:"
echo "  export EXE_API_TOKEN='$TOKEN'"
echo ""
echo "Permissions embedded in token:"
echo "  $PERMISSIONS"
echo ""
echo "Verify the scoping (should succeed):"
echo "  curl -s -X POST https://exe.dev/exec -H 'Authorization: Bearer $TOKEN' -d 'domain add --help'"
echo "Verify a disallowed command (should get 403):"
echo "  curl -s -X POST https://exe.dev/exec -H 'Authorization: Bearer $TOKEN' -d 'whoami'"
