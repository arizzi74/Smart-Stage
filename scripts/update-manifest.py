#!/usr/bin/env python3
"""Publish bounded official release metadata; only CI accesses the GitHub API."""
import argparse
import json
import os
from pathlib import Path
import re
import time
import urllib.error
import urllib.parse
import urllib.request


REPOSITORY = "arizzi74/Smart-Stage"
API = f"https://api.github.com/repos/{REPOSITORY}/releases"
WEBSITE = "https://arizzi74.github.io/Smart-Stage/updates.json"
MAX_METADATA = 8 << 20
ARCHIVES = tuple(
    f"smartstage-{system}-{arch}{suffix}.zip"
    for system, suffix in (("darwin", ""), ("darwin", ".app"), ("windows", ""))
    for arch in ("arm64", "amd64")
)
VERSION = re.compile(r"v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?")


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, file, code, message, headers, newurl):
        # The CI token belongs only on the fixed GitHub API origin.
        return None


def read_json(url, token=None):
    headers = {"User-Agent": "Smart-Stage-update-manifest", "Accept": "application/json", "Cache-Control": "no-cache"}
    if token:
        if not url.startswith(API + "?"):
            raise ValueError("Refusing to send a CI token outside the releases API")
        headers.update(Authorization="Bearer " + token, **{"X-GitHub-Api-Version": "2022-11-28"})
    request = urllib.request.Request(url, headers=headers)
    with urllib.request.build_opener(NoRedirect).open(request, timeout=30) as response:
        data = response.read(MAX_METADATA + 1)
    if len(data) > MAX_METADATA:
        raise ValueError("Release metadata exceeds its size limit")
    return json.loads(data)


def release_entry(release):
    if not isinstance(release, dict) or release.get("draft") is not False:
        return None
    tag = release.get("tag_name", "")
    if not isinstance(tag, str) or not VERSION.fullmatch(tag) or type(release.get("prerelease")) is not bool:
        return None
    assets = release.get("assets")
    if not isinstance(assets, list):
        return None
    base = f"https://github.com/{REPOSITORY}/releases/download/{urllib.parse.quote(tag, safe='+')}/"
    selected = []
    for archive in ARCHIVES:
        pair = []
        for name, maximum in ((archive, 512 << 20), (archive + ".sha256", 4096)):
            matches = [asset for asset in assets if isinstance(asset, dict) and asset.get("name") == name]
            if len(matches) != 1:
                break
            asset = matches[0]
            size = asset.get("size")
            if type(size) is not int or not 0 < size <= maximum or asset.get("browser_download_url") != base + name:
                break
            pair.append({"name": name, "browser_download_url": base + name, "size": size})
        if len(pair) == 2:
            selected.extend(pair)
    if not selected:
        return None
    return {"tag_name": tag, "draft": False, "prerelease": release["prerelease"], "assets": selected}


def build_manifest(releases, required_release=None):
    if not isinstance(releases, list) or len(releases) > 500:
        raise ValueError("Expected at most 500 releases")
    entries, tags = [], set()
    for release in releases:
        entry = release_entry(release)
        if entry is None:
            continue
        if entry["tag_name"] in tags:
            raise ValueError("Duplicate release version")
        tags.add(entry["tag_name"])
        entries.append(entry)
    if not entries:
        raise ValueError("No complete official release downloads found")
    if required_release:
        matching = [entry for entry in entries if entry["tag_name"] == required_release]
        if len(matching) != 1 or len(matching[0]["assets"]) != 2 * len(ARCHIVES):
            raise ValueError("Required release does not contain all six desktop archives and checksums")
    manifest = {"schemaVersion": 1, "releases": entries}
    encode_manifest(manifest)
    return manifest


def encode_manifest(manifest):
    data = (json.dumps(manifest, indent=2, ensure_ascii=True) + "\n").encode()
    if len(data) > MAX_METADATA:
        raise ValueError("Update manifest exceeds client size limit")
    return data


def published_manifest(required_release, timeout):
    deadline = time.monotonic() + timeout
    while True:
        try:
            manifest = read_json(WEBSITE)
            if not isinstance(manifest, dict) or manifest.get("schemaVersion") != 1:
                raise ValueError("Unsupported public manifest schema")
            checked = build_manifest(manifest.get("releases"), required_release)
            if checked != manifest:
                raise ValueError("Public manifest contains invalid or unexpected release metadata")
            return manifest
        except (OSError, ValueError) as error:
            if time.monotonic() >= deadline:
                raise RuntimeError("Published update manifest is not ready: " + str(error)) from error
            time.sleep(min(10, max(0, deadline - time.monotonic())))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path)
    parser.add_argument("--required-release")
    parser.add_argument("--verify-public", action="store_true")
    parser.add_argument("--timeout", type=int, default=480)
    args = parser.parse_args()
    if args.verify_public:
        manifest = published_manifest(args.required_release, args.timeout)
        print(f"Verified public update manifest: {len(manifest['releases'])} releases")
        return
    if not args.output:
        parser.error("--output is required when generating the manifest")
    releases = []
    for page in range(1, 6):
        batch = read_json(API + f"?per_page=100&page={page}", os.environ.get("GH_TOKEN"))
        if not isinstance(batch, list) or len(batch) > 100:
            raise ValueError("Unexpected GitHub release response")
        releases.extend(batch)
        if len(batch) < 100:
            break
    manifest = build_manifest(releases, args.required_release)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    temporary = args.output.with_suffix(args.output.suffix + ".tmp")
    temporary.write_bytes(encode_manifest(manifest))
    temporary.replace(args.output)
    print(f"Generated {args.output}: {len(manifest['releases'])} published releases")


if __name__ == "__main__":
    main()
