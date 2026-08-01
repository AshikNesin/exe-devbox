#!/usr/bin/env python3
"""Upload a release binary to GitHub via the Contents API on a releases branch.

The exe.dev GitHub proxy injects auth for github.int.exe.xyz but does not
support the asset-upload endpoint (uploads.github.com). So binaries go into
a dedicated `releases` branch via the Contents API — keeping main clean.

Usage: release-upload.py <version> <release-dir> <api-base> <repo>
e.g.   release-upload.py v0.3.0 release-build https://github.int.exe.xyz/api/v3 AshikNesin/exebox
"""
import sys, json, base64, urllib.request, urllib.error, os

version = sys.argv[1]
release_dir = sys.argv[2]
api_base = sys.argv[3]
repo = sys.argv[4]

binary_path = f"{release_dir}/exebox-{version}-linux-amd64"
content_path = f"releases/{version}/exebox-linux-amd64"
branch = "releases"
url = f"{api_base}/repos/{repo}/contents/{content_path}?ref={branch}"

with open(binary_path, "rb") as f:
    b64 = base64.b64encode(f.read()).decode()

# Check if file already exists (need SHA to update)
sha = None
try:
    req = urllib.request.Request(url)
    resp = urllib.request.urlopen(req)
    existing = json.loads(resp.read())
    sha = existing["sha"]
except urllib.error.HTTPError as e:
    if e.code != 404:
        raise
except Exception:
    pass  # treat as new file

payload = {
    "message": f"Add exebox {version} binary",
    "content": b64,
    "branch": branch,
}
if sha:
    payload["sha"] = sha

req = urllib.request.Request(
    url,
    data=json.dumps(payload).encode(),
    headers={"Content-Type": "application/json"},
    method="PUT",
)
resp = urllib.request.urlopen(req)
result = json.loads(resp.read())
print(f"  uploaded: {result['content']['path']} on branch '{branch}' (size: {result['content']['size']})")
print(f"  download: https://raw.githubusercontent.com/{repo}/{branch}/{content_path}")