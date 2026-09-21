#!/usr/bin/env python3
"""Build the public static website; Python's standard library is sufficient."""
from pathlib import Path
import shutil


ROOT = Path(__file__).resolve().parents[1]
DESTINATION = ROOT / "dist" / "site"


def main():
    if DESTINATION.exists():
        shutil.rmtree(DESTINATION)
    shutil.copytree(ROOT / "docs" / "site", DESTINATION)
    assets = DESTINATION / "assets"
    assets.mkdir(exist_ok=True)
    for name in ("admin.png", "remote.png"):
        shutil.copyfile(ROOT / "docs" / "screenshots" / name, assets / name)
    shutil.copyfile(ROOT / "assets" / "icon" / "smartstage.png", assets / "icon.png")
    shutil.copyfile(ROOT / "assets" / "icon" / "smartstage.svg", assets / "favicon.svg")
    (DESTINATION / ".nojekyll").touch()
    print(f"Built public website: {DESTINATION}")


if __name__ == "__main__":
    main()
