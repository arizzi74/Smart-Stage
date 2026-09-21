"""Observe production Windows stage cursor on a real runner input desktop."""
import json
import os
from pathlib import Path
import platform
import struct
import subprocess
import tempfile
import zlib


def cursor_checks(display):
    root = Path(__file__).resolve().parents[1]
    with tempfile.TemporaryDirectory(prefix="smartstage-native-cursor-") as directory:
        temporary = Path(directory)
        probe = temporary / "cursor.exe"
        arch = "aarch64" if platform.machine().lower() in ("arm64", "aarch64") else "x86_64"
        command = [os.environ.get("CXX", f"{arch}-w64-mingw32-clang++"), "-std=c++17", "-municode",
                   "-D_WIN32_WINNT=0x0A00", "-DUNICODE", "-D_UNICODE", "-Wall", "-Wextra",
                   str(root / "scripts/native-cursor-windows.cpp"),
                   str(root / "internal/platform/desktop_windows.cpp"), "-o", str(probe),
                   "-static-libstdc++", "-static-libgcc", "-Wl,-Bstatic", "-lwinpthread", "-Wl,-Bdynamic"]
        command.extend("-l" + library for library in
                       ("mfplat", "mf", "mfuuid", "mfreadwrite", "evr", "ole32", "oleaut32",
                        "uuid", "propsys", "user32", "gdi32", "shcore", "shell32", "dcomp", "bcrypt", "advapi32"))
        build = subprocess.run(command, capture_output=True, text=True, timeout=90)
        assert build.returncode == 0, f"Native cursor probe compilation failed: {build.stderr}"

        def chunk(kind, data):
            return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind + data))

        image = temporary / "background.png"
        png = b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", 64, 64, 8, 2, 0, 0, 0))
        png += chunk(b"IDAT", zlib.compress((b"\0" + b"\xff\0\0" * 64) * 64)) + chunk(b"IEND", b"")
        image.write_bytes(png)
        result = subprocess.run([str(probe), display["id"], str(root / "testdata/media/silent-1080p.mp4"), str(image)],
                                capture_output=True, text=True, encoding="utf-8", timeout=90)
        assert result.returncode == 0, f"Native cursor probe failed ({result.returncode}): {result.stdout}\n{result.stderr}"
        # Native shutdown diagnostics go to stderr; the report is the last JSON line.
        reports = [json.loads(line) for line in result.stdout.splitlines() if line.startswith("{")]
        assert len(reports) == 1, result.stdout
        report = reports[0]
        assert report["transparentCursorBitmapVerified"] and report["stageCursorMessageHandling"] and report["operatorUsesSeparateInputThread"], report
        global_cursor = report["globalCursor"]
        if report["status"] == "unavailable":
            assert global_cursor["status"] == "unavailable" and not global_cursor["available"], report
            assert global_cursor["unavailableReason"] and global_cursor["baseline"], report
        else:
            assert report["status"] == "passed" and global_cursor["status"] == "passed" and global_cursor["available"], report
            required = {"independentDesktopCursorBaseline", "stationaryStageActivation", "stationaryRendererResetRecovery",
                        "movingOverBlankStage", "operatorWindowVisibleWhileStageEnabled", "stationaryReturnFromOperatorWindow",
                        "stationaryStageOffRestoresCursor", "repeatedStageTogglesRestoreCursor", "backgroundVideoCursor",
                        "foregroundVideoCursor", "imageOverlayCursor", "imageBackgroundCursor", "sceneStageOffRestoresCursor",
                        "nativeEscapeRestoresCursor", "applicationQuitRestoresCursor"}
            assert required <= set(global_cursor["passedChecks"]), report
        report["method"] = "Production cursor leaves checkerboard pixels unchanged under DrawIconEx; actual SetCursorPos/WindowFromPoint/GetCursorInfo checks with an independent operator window on a separate STA/input thread; desktop capability reported separately; not a physical mouse or VM viewer observation"
        return report
