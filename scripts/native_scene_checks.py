"""Observe real Mac scene players/layers in a test process, without production hooks."""
import json
import math
import os
from pathlib import Path
import struct
import subprocess
import tempfile
import wave
import zlib


def scene_checks(devices):
    if not devices["audio"] or not devices["displays"]:
        raise AssertionError("Mac scene checks require a native audio output and display")
    root = Path(__file__).resolve().parents[1]
    audio = next((item for item in devices["audio"] if item["default"]), devices["audio"][0])
    with tempfile.TemporaryDirectory(prefix="smartstage-native-scene-") as temporary:
        work = Path(temporary)
        first, second = work / "first.wav", work / "second.wav"
        for path, frequency in ((first, 330), (second, 550)):
            with wave.open(str(path), "wb") as stream:
                stream.setparams((1, 2, 22050, 0, "NONE", "not compressed"))
                stream.writeframes(b"".join(struct.pack("<h", int(1400 * math.sin(2 * math.pi * frequency * i / 22050)))
                                          for i in range(22050 * 20)))
        # Generate an ordinary opaque PNG, decoded by the real ImageIO path.
        def png_chunk(kind, data):
            return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind + data))
        image = work / "background.png"
        image.write_bytes(b"\x89PNG\r\n\x1a\n" +
                          png_chunk(b"IHDR", struct.pack(">IIBBBBB", 16, 16, 8, 2, 0, 0, 0)) +
                          png_chunk(b"IDAT", zlib.compress((b"\0" + b"\xff\x44\x11" * 16) * 16)) +
                          png_chunk(b"IEND", b""))
        binary = work / "scene-probe"
        command = ["/usr/bin/clang", "-fobjc-arc", "-fblocks", "-mmacosx-version-min=12.0", "-Wall", "-Wextra",
                   str(root / "scripts/native-scene-darwin.m"), "-o", str(binary)]
        for framework in ("AppKit", "AVFoundation", "CoreAudio", "CoreMedia", "CoreVideo", "QuartzCore",
                          "CoreGraphics", "IOKit", "UniformTypeIdentifiers", "WebKit", "ImageIO"):
            command.extend(["-framework", framework])
        built = subprocess.run(command, capture_output=True, text=True, timeout=120)
        assert built.returncode == 0, f"Native scene probe compilation failed: {built.stderr}"
        environment = os.environ.copy()
        environment.update({"SMARTSTAGE_SCENE_PROBE_AUDIO": audio["id"],
                            "SMARTSTAGE_SCENE_PROBE_DISPLAY": devices["displays"][0]["id"],
                            "SMARTSTAGE_SCENE_PROBE_FIRST": str(first), "SMARTSTAGE_SCENE_PROBE_SECOND": str(second),
                            "SMARTSTAGE_SCENE_PROBE_BACKGROUND": str(root / "testdata/media/video-aac-1080p.mp4"),
                            "SMARTSTAGE_SCENE_PROBE_IMAGE": str(image)})
        result = subprocess.run([str(binary)], env=environment, capture_output=True, text=True, timeout=50)
        assert result.returncode == 0, f"Native scene probe failed: {result.stdout}\n{result.stderr}"
        reports = [json.loads(line) for line in result.stdout.splitlines() if line.startswith("{")]
        assert len(reports) == 1 and reports[0]["status"] == "passed", result.stdout
        return reports[0]
