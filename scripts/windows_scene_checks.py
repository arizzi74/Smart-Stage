"""Run real Windows scene renderers and read back native per-stream gains."""
import json
import math
import os
from pathlib import Path
import platform
import struct
import subprocess
import tempfile
import wave
import zlib


def scene_checks(audio, display):
    root = Path(__file__).resolve().parents[1]
    with tempfile.TemporaryDirectory(prefix="smartstage-native-scene-") as directory:
        temporary = Path(directory)
        probe = temporary / "scene.exe"
        arch = "aarch64" if platform.machine().lower() in ("arm64", "aarch64") else "x86_64"
        command = [os.environ.get("CXX", f"{arch}-w64-mingw32-clang++"), "-std=c++17",
                   "-O2", "-D_WIN32_WINNT=0x0A00", "-DUNICODE", "-D_UNICODE", "-Wall", "-Wextra",
                   str(root / "scripts/native-scene-windows.cpp"),
                   str(root / "internal/platform/desktop_windows.cpp"), "-o", str(probe),
                   "-static-libstdc++", "-static-libgcc", "-Wl,-Bstatic", "-lwinpthread", "-Wl,-Bdynamic"]
        command.extend("-l" + library for library in
                       ("mfplat", "mf", "mfuuid", "mfreadwrite", "evr", "ole32", "oleaut32",
                        "uuid", "propsys", "user32", "gdi32", "shcore", "shell32", "dcomp", "bcrypt", "advapi32"))
        build = subprocess.run(command, capture_output=True, text=True, timeout=90)
        assert build.returncode == 0, f"Native scene probe compilation failed: {build.stderr}"
        for name, frequency in (("first.wav", 440), ("second.wav", 660)):
            with wave.open(str(temporary / name), "wb") as output:
                output.setnchannels(1)
                output.setsampwidth(2)
                output.setframerate(48000)
                second = b"".join(struct.pack("<h", round(5000 * math.sin(2 * math.pi * frequency * i / 48000)))
                                  for i in range(48000))
                output.writeframes(second * 30)
        def chunk(kind, data):
            return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind + data))
        for name, color in (("background.png", b"\xff\0\0"), ("green.png", b"\0\xff\0")):
            png = b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", 64, 64, 8, 2, 0, 0, 0))
            png += chunk(b"IDAT", zlib.compress((b"\0" + color * 64) * 64)) + chunk(b"IEND", b"")
            (temporary / name).write_bytes(png)
        result = subprocess.run([str(probe), audio["id"] if audio else "-", display["id"], str(temporary / "first.wav"),
                                 str(temporary / "second.wav"), str(root / "testdata/media/video-aac-1080p.mp4"),
                                 str(temporary / "background.png"), str(temporary / "green.png")], capture_output=True, text=True, timeout=90)
        assert result.returncode == 0, f"Native scene probe failed ({result.returncode}): {result.stderr}"
        return json.loads(result.stdout)
