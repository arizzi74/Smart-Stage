#!/usr/bin/env python3
"""Verify production Windows source opening and decoding without an audio device."""
import json
import os
from pathlib import Path
import platform
import shutil
import subprocess
import sys
import tempfile


def source_checks():
    root = Path(__file__).resolve().parents[1]
    media = root / "testdata/media"
    with tempfile.TemporaryDirectory(prefix="smartstage-native-source-") as directory:
        temporary = Path(directory)
        probe = temporary / "source.exe"
        arch = "aarch64" if platform.machine().lower() in ("arm64", "aarch64") else "x86_64"
        command = [os.environ.get("CXX", f"{arch}-w64-mingw32-clang++"), "-std=c++17", "-municode",
                   "-D_WIN32_WINNT=0x0A00", "-DUNICODE", "-D_UNICODE", "-Wall", "-Wextra",
                   str(root / "scripts/native-source-windows.cpp"),
                   str(root / "internal/platform/desktop_windows.cpp"), "-o", str(probe),
                   "-static-libstdc++", "-static-libgcc", "-Wl,-Bstatic", "-lwinpthread", "-Wl,-Bdynamic"]
        command.extend("-l" + library for library in
                       ("mfplat", "mf", "mfuuid", "mfreadwrite", "evr", "ole32", "oleaut32",
                        "uuid", "propsys", "user32", "gdi32", "shcore", "shell32", "dcomp", "bcrypt", "advapi32"))
        build = subprocess.run(command, capture_output=True, text=True, timeout=90)
        assert build.returncode == 0, f"Native source probe compilation failed: {build.stderr}"
        # Preserve the fixture bytes exactly: only its extension is wrong.
        wave = media / "Opening – café's tone.wav"
        renamed = temporary / "unchanged-wave.mp3"
        shutil.copyfile(wave, renamed)
        assert renamed.read_bytes() == wave.read_bytes()
        m4a = media / "tone-aac.m4a"
        renamed_m4a = temporary / "unchanged-mp4-audio.mp3"
        shutil.copyfile(m4a, renamed_m4a)
        assert renamed_m4a.read_bytes() == m4a.read_bytes()
        result = subprocess.run([str(probe), str(media / "tone.mp3"), str(wave), str(renamed),
                                 str(m4a), str(renamed_m4a),
                                 str(media / "damaged.mp4"), str(temporary / "does-not-exist.mp3")],
                                capture_output=True, text=True, encoding="utf-8", timeout=45)
        assert result.returncode == 0, f"Native source probe failed ({result.returncode}): {result.stdout}\n{result.stderr}"
        report = json.loads(result.stdout)
        assert report["status"] == "passed", report
        cases = {case["fixture"]: case for case in report["cases"]}
        assert set(cases) == {"mp3", "wav-unicode-path", "wav-renamed-mp3", "m4a", "m4a-renamed-mp3", "damaged", "missing"}, report
        for name in ("mp3", "wav-unicode-path", "wav-renamed-mp3", "m4a", "m4a-renamed-mp3"):
            case = cases[name]
            assert case["productionOpenSucceeded"] and case["sourceHasAudio"], case
            inspection = case["inspection"]
            assert inspection["kind"] == "audio" and inspection["hasAudio"] and not inspection["hasVideo"], case
            assert 2.5 < inspection["duration"] < 3.5, case
        for name in ("wav-renamed-mp3", "m4a-renamed-mp3"):
            renamed_case = cases[name]
            assert renamed_case["strictHRESULT"] == "0xc00d36c4" and renamed_case["strictUnsupportedByteStreamObserved"], renamed_case
        for name in ("damaged", "missing"):
            assert cases[name]["productionOpenRejected"] and cases[name]["inspection"]["error"], cases[name]
        report["method"] = "Production Media Foundation source opener and first-sample decoder inspection; unchanged WAV and AAC/MP4 bytes renamed .mp3 reproduce the prior strict resolver failure; no audio renderer or physical output"
        return report


if __name__ == "__main__":
    if sys.platform != "win32":
        raise SystemExit("This native probe must run on Windows")
    print(json.dumps(source_checks(), ensure_ascii=False))
