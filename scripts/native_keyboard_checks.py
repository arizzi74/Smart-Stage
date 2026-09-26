"""Compile test-only event drivers against the production native bridge."""
import json
import os
from pathlib import Path
import platform
import subprocess
import sys
import tempfile


def escape_checks(display):
    root = Path(__file__).resolve().parents[1]
    with tempfile.TemporaryDirectory(prefix="smartstage-native-keyboard-") as temporary:
        probe = Path(temporary) / ("keyboard.exe" if os.name == "nt" else "keyboard")
        if sys.platform == "darwin":
            command = ["/usr/bin/clang", "-fobjc-arc", "-fblocks", "-mmacosx-version-min=12.0",
                       "-Wall", "-Wextra", str(root / "scripts/native-keyboard-darwin.m"),
                       str(root / "internal/platform/bridge_darwin.m"), "-o", str(probe)]
            for framework in ("AppKit", "AVFoundation", "CoreAudio", "CoreMedia", "CoreVideo",
                              "QuartzCore", "CoreGraphics", "IOKit", "UniformTypeIdentifiers", "WebKit", "ImageIO"):
                command.extend(["-framework", framework])
        else:
            arch = "aarch64" if platform.machine().lower() in ("arm64", "aarch64") else "x86_64"
            command = [os.environ.get("CXX", f"{arch}-w64-mingw32-clang++"), "-std=c++17",
                       "-D_WIN32_WINNT=0x0A00", "-DUNICODE", "-D_UNICODE", "-Wall", "-Wextra",
                       str(root / "scripts/native-keyboard-windows.cpp"),
                       str(root / "internal/platform/bridge_windows.cpp"),
                       str(root / "internal/platform/desktop_windows.cpp"), "-o", str(probe),
                       "-static-libstdc++", "-static-libgcc", "-Wl,-Bstatic", "-lwinpthread", "-Wl,-Bdynamic"]
            command.extend("-l" + library for library in
                           ("mfplat", "mf", "mfuuid", "mfreadwrite", "evr", "ole32", "oleaut32",
                            "uuid", "propsys", "user32", "gdi32", "shcore", "shell32", "dcomp", "bcrypt", "advapi32"))
        build = subprocess.run(command, capture_output=True, text=True, timeout=90)
        assert build.returncode == 0, f"Native keyboard probe compilation failed: {build.stderr}"
        evidence = []
        for target in ("stage", "app"):
            result = subprocess.run([str(probe), display["id"], target], capture_output=True, text=True, timeout=20)
            assert result.returncode == 0, f"Native {target} Escape probe failed ({result.returncode}): {result.stderr}"
            evidence.append(json.loads(result.stdout))
        if sys.platform == "darwin":
            environment = os.environ.copy()
            environment["SMARTSTAGE_APP_ICON"] = str(root / "assets/icon/smartstage.icns")
            result = subprocess.run([str(probe), display["id"], "desktop"], env=environment,
                                    capture_output=True, text=True, timeout=20)
            assert result.returncode == 0, f"Native desktop action probe failed ({result.returncode}): {result.stderr}"
            evidence.append({"nativeDesktopActions": json.loads(result.stdout)})
            try:
                result = subprocess.run([str(probe), display["id"], "playlist"], env=environment,
                                        capture_output=True, text=True, timeout=35)
            except subprocess.TimeoutExpired as error:
                captured = error.stderr or b""
                detail = captured.decode("utf-8", "replace") if isinstance(captured, bytes) else captured
                raise AssertionError(f"Native playlist dialog probe timed out; captured progress:\n{detail[-18000:]}") from error
            assert result.returncode == 0, f"Native playlist dialog probe failed ({result.returncode}): {result.stderr}"
            evidence.append({"nativePlaylistDialogs": json.loads(result.stdout)})
        return evidence
