#!/usr/bin/env python3
"""Upload a release binary to GitHub as a release asset (not committed to git).

Uses the GitHub Contents API proxy at github.int.exe.xyz which injects auth.

Usage: release-upload.py <version> <release-dir> <api-base> <repo>
e.g.   release-upload.py v0.3.0 release-build https://github.int.exe.xyz/api/v3 AshikNesin/exebox
"""
import sys, json, urllib.request, os

version = sys.argv[1]
release_dir = sys.argv[2]
api_base = sys.argv[3]
repo = sys.argv[4]

binary_path = f"{release_dir}/exebox-{version}-linux-amd64"
asset_name = "exebox-linux-amd64"

# 1. Create the release (or find an existing one for this tag).
tag = version.lstrip("v")
create_url = f"{api_base}/repos/{repo}/releases"
create_body = json.dumps({
    "tag_name": version,
    "name": version,
    "body": f"exebox {version}",
    "draft": False,
    "prerelease": False,
}).encode()

req = urllib.request.Request(
    create_url,
    data=create_body,
    headers={"Content-Type": "application/json"},
    method="POST",
)
try:
    resp = urllib.request.urlopen(req)
    release = json.loads(resp.read())
    print(f"  created release: {release['tag_name']} (id {release['id']})")
except urllib.error.HTTPError as e:
    if e.code == 422:
        # Release already exists for this tag — fetch it.
        get_url = f"{api_base}/repos/{repo}/releases/tags/{version}"
        resp = urllib.request.urlopen(get_url)
        release = json.loads(resp.read())
        print(f"  using existing release: {release['tag_name']} (id {release['id']})")
    else:
        raise

release_id = release["id"]

# 2. Upload the binary as a release asset.
upload_url = release["upload_url"].replace("{?name,label}", "")
asset_url = f"{upload_url}?name={asset_name}"

size = os.path.getsize(binary_path)
with open(binary_path, "rb") as f:
    data = f.read()

req = urllib.request.Request(
    asset_url,
    data=data,
    headers={
        "Content-Type": "application/octet-stream",
        "Content-Length": str(size),
    },
    method="POST",
)
resp = urllib.request.urlopen(req)
result = json.loads(resp.read())
print(f"  uploaded asset: {result['name']} (size: {result['size']}, downloads: {result['download_count']})")
print(f"  download url: {result['browser_download_url']}")