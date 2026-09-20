#!/usr/bin/env python3
"""Build an optional Finder app around the already-built standalone executable."""
import hashlib
import os
from pathlib import Path
import plistlib
import re
import shutil
import subprocess
import sys

root = Path(__file__).resolve().parents[1]
arch, version = sys.argv[1:]
native_arch = {"arm64": "arm64", "amd64": "x86_64"}[arch]
artifact = root / "dist" / f"smartstage-darwin-{arch}"
bundle = root / "dist" / f"macos-{arch}" / "Smart Stage.app"
if bundle.exists():
    shutil.rmtree(bundle)
contents = bundle / "Contents"
macos = contents / "MacOS"
resources = contents / "Resources"
macos.mkdir(parents=True)
resources.mkdir()
shutil.copy2(artifact, macos / "smartstage")
shutil.copy2(root / "assets/icon/smartstage.icns", resources / "smartstage.icns")
command = resources / "Start Smart Stage.command"
shutil.copy2(root / "packaging/macos/Start Smart Stage.command", command)
command.chmod(0o755)
match = re.fullmatch(r"v?(\d+\.\d+\.\d+)(?:-preview\.(\d+))?", version)
short_version = match[1] if match else "0.0.0"
bundle_version = short_version + (f"b{match[2]}" if match and match[2] else "")
with (contents / "Info.plist").open("wb") as f:
    plistlib.dump({
        "CFBundleIdentifier": "com.github.arizzi74.smartstage",
        "CFBundleName": "Smart Stage",
        "CFBundleDisplayName": "Smart Stage",
        "CFBundlePackageType": "APPL",
        "CFBundleExecutable": "SmartStageLauncher",
        "CFBundleIconFile": "smartstage.icns",
        "CFBundleShortVersionString": short_version,
        "CFBundleVersion": bundle_version,
        "SmartStageVersion": version,
        "LSMinimumSystemVersion": "12.0",
        "LSUIElement": True,
        "NSHighResolutionCapable": True,
    }, f)
subprocess.run([
    os.environ.get("CC", "clang"), "-arch", native_arch,
    "-mmacosx-version-min=12.0", "-Os", "-Wall", "-Wextra", "-Werror",
    "-framework", "Foundation", str(root / "packaging/macos/launcher.m"),
    "-o", str(macos / "SmartStageLauncher"),
], check=True)
subprocess.run(["codesign", "--force", "--sign", "-", str(bundle)], check=True)
subprocess.run(["codesign", "--verify", "--deep", "--strict", str(bundle)], check=True)
archive = artifact.with_name(artifact.name + ".app.zip")
if archive.exists():
    archive.unlink()
subprocess.run(["ditto", "-c", "-k", "--sequesterRsrc", "--keepParent", str(bundle), str(archive)], check=True)
archive.with_name(archive.name + ".sha256").write_text(
    hashlib.sha256(archive.read_bytes()).hexdigest() + "  " + archive.name + "\n"
)
print(f"Created {archive}")
