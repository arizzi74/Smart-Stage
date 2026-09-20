#!/usr/bin/env python3
"""Create the portable, single-executable ZIP for a release target."""
import argparse
import hashlib
from pathlib import Path
import stat
import zipfile


def package(artifact, target_os, target_arch):
    name = "smartstage.exe" if target_os == "windows" else "smartstage"
    archive = artifact.parent / f"smartstage-{target_os}-{target_arch}.zip"
    executable = artifact.read_bytes()
    entry = zipfile.ZipInfo(name, date_time=(1980, 1, 1, 0, 0, 0))
    entry.create_system = 3
    entry.external_attr = (stat.S_IFREG | 0o755) << 16
    entry.compress_type = zipfile.ZIP_DEFLATED
    with zipfile.ZipFile(archive, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as output:
        output.writestr(entry, executable)
    with zipfile.ZipFile(archive) as output:
        assert output.namelist() == [name], "Archive must contain exactly the executable"
        assert output.read(name) == executable, "Archived executable changed"
        assert output.getinfo(name).external_attr >> 16 & 0o777 == 0o755
    digest = hashlib.sha256(archive.read_bytes()).hexdigest()
    archive.with_name(archive.name + ".sha256").write_text(f"{digest}  {archive.name}\n")
    print(f"Created {archive} ({name}, SHA-256 {digest})")
    return archive


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("target_os", choices=("darwin", "windows"))
    parser.add_argument("target_arch", choices=("arm64", "amd64"))
    args = parser.parse_args()
    suffix = ".exe" if args.target_os == "windows" else ""
    root = Path(__file__).resolve().parents[1]
    artifact = root / "dist" / f"smartstage-{args.target_os}-{args.target_arch}{suffix}"
    package(artifact, args.target_os, args.target_arch)


if __name__ == "__main__":
    main()
