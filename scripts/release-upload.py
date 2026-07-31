#!/usr/bin/env python3
"""Upload a release binary to GitHub via the Contents API.

Usage: release-upload.py <version> <release-dir> <api-base> <repo>
e.g.   release-upload.py v0.2.0 release-build https://api.github.com AshikNesin/exebox
"""
import sys, json, base64, urllib.request

version = sys.argv[1]
release_dir = sys.argv[2]
api_base = sys.argv[3]
repo = sys.argv[4]

binary_path = f"{release_dir}/exebox-{version}-linux-amd64"
content_path = f"releases/{version}/exebox-linux-amd64"
url = f"{api_base}/repos/{repo}/contents/{content_path}"

with open(binary_path, "rb") as f:
    b64 = base64.b64encode(f.read()).decode()

# Check if file already exists (need SHA to update)
sha = None
try:
    req = urllib.request.Request(url)
    resp = urllib.request.urlopen(req)
    existing = json.loads(resp.read())
    sha = existing["sha"]
except Exception:
    pass  # new file, no SHA needed

payload = {"message": f"Add exebox {version} binary", "content": b64}
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
print(f"  uploaded: {result['content']['path']} (size: {result['content']['size']})")
