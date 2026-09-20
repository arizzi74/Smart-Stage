#!/usr/bin/env python3
"""Regenerate committed icons from SVG; development tools only (see assets/icon/README.md)."""
import io
from pathlib import Path

import cairosvg
from PIL import Image

root = Path(__file__).resolve().parents[1] / "assets" / "icon"
png = cairosvg.svg2png(url=str(root / "smartstage.svg"), output_width=1024, output_height=1024)
(root / "smartstage.png").write_bytes(png)
with Image.open(io.BytesIO(png)) as icon:
    icon.save(root / "smartstage.ico", sizes=[(s, s) for s in (16, 20, 24, 32, 40, 48, 64, 128, 256)])
    icon.save(root / "smartstage.icns")
