#!/usr/bin/env bash
# Build the exact updater source with deliberately older release metadata.
# Fixtures are CI inputs, never published release assets.
set -euo pipefail
cd "$(dirname "$0")/.."
target_os=${1:?Target OS is required}
target_arch=${2:?Target architecture is required}
output=${3:?Fixture output directory is required}
mkdir -p "$output"
output=$(cd "$output" && pwd -P)
source_root=$(pwd -P)
fixture_source=$(mktemp -d)
cleanup() {
    git -C "$source_root" worktree remove --force "$fixture_source" >/dev/null 2>&1 || rm -rf -- "$fixture_source"
}
trap cleanup EXIT
git worktree add --detach "$fixture_source" HEAD
(
    cd "$fixture_source"
    VERSION=v0.0.0-preview.1 bash scripts/build.sh "$target_os" "$target_arch"
)
python3 - "$fixture_source" "$output" "$target_os" "$target_arch" <<'PY'
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import sys

source, output = map(Path, sys.argv[1:3])
target_os, arch = sys.argv[3:]
suffix = ".app.zip" if target_os == "darwin" else ".zip"
name = f"fixture-{target_os}-{arch}{suffix}"
archive = output / name
shutil.copy2(source / "dist" / f"smartstage-{target_os}-{arch}{suffix}", archive)
binary = source / "dist" / f"smartstage-{target_os}-{arch}{'.exe' if target_os == 'windows' else ''}"
manifest = {
    "version": "v0.0.0-preview.1",
    "sourceSHA": subprocess.check_output(["git", "-C", str(source), "rev-parse", "HEAD"], text=True).strip(),
    "target": f"{target_os}/{arch}",
    "archive": name,
    "archiveSHA256": hashlib.sha256(archive.read_bytes()).hexdigest(),
    "executableSHA256": hashlib.sha256(binary.read_bytes()).hexdigest(),
    "purpose": "Native production updater verification with older version metadata; not a published release",
}
(output / "fixture.json").write_text(json.dumps(manifest, indent=2) + "\n")
print(json.dumps(manifest, indent=2))
PY
