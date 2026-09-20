"""Observe production WebView2, real Admin HTTP/SSE, OLE file data and a real chooser."""
import importlib.util
import json
import os
from pathlib import Path
import shutil
import struct
import subprocess
import tempfile


def admin_window_checks(executable, evidence_path):
    executable = Path(executable).resolve()
    evidence_path = Path(evidence_path)
    root = Path(__file__).resolve().parents[1]
    spec = importlib.util.spec_from_file_location("smartstage_windows_admin_host_checks", Path(__file__).with_name("verify-auto-update.py"))
    helpers = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(helpers)
    report = {"status": "incomplete", "method": "Production Windows STA/DirectComposition WebView2 with real loopback Admin HTTP/session/CSRF/SSE; own-process COM observation, CF_HDROP delivery to the production OLE target and actual IFileOpenDialog selection; not a physical Explorer mouse gesture"}
    process = None
    try:
        data = executable.read_bytes()
        offset = struct.unpack_from("<I", data, 0x3C)[0]
        machine = struct.unpack_from("<H", data, offset + 4)[0]
        triple = {0x8664: "x86_64", 0xAA64: "aarch64"}[machine]
        compiler = shutil.which(f"{triple}-w64-mingw32-clang++")
        assert compiler, "Pinned LLVM-MinGW compiler must be on PATH for the native Admin probe"
        with tempfile.TemporaryDirectory(prefix="smartstage-native-admin-") as temporary:
            work = Path(temporary)
            config, media = work / "Saved show", work / "Originals – café"
            show = helpers.seed_show(config, media)
            original = Path(show["cues"][0]["path"])
            second = media / "Second original – 演出.wav"
            second.write_bytes(original.read_bytes())
            hashes = [helpers.digest(path) for path in (original, second)]
            binary = work / "native-admin-probe.exe"
            command = [compiler, "-std=c++17", "-D_WIN32_WINNT=0x0A00", "-DUNICODE", "-D_UNICODE", "-Wall", "-Wextra",
                       str(root / "scripts/native-admin-windows.cpp"), "-o", str(binary),
                       "-static-libstdc++", "-static-libgcc", "-Wl,-Bstatic", "-lwinpthread", "-Wl,-Bdynamic", "-lole32", "-loleaut32",
                       "-luuid", "-luser32", "-lgdi32", "-lshell32", "-ldcomp", "-lbcrypt", "-ladvapi32"]
            built = subprocess.run(command, capture_output=True, text=True, timeout=180)
            assert built.returncode == 0, f"Native Admin probe compilation failed: {built.stderr}"
            report["probeMachine"] = "arm64" if machine == 0xAA64 else "amd64"
            report["sourceExecutableSha256"] = helpers.digest(executable)
            admin_port, remote_port = helpers.unused_port(), helpers.unused_port()
            while remote_port == admin_port:
                remote_port = helpers.unused_port()
            environment = os.environ.copy()
            with (work / "host.log").open("wb") as log:
                process = subprocess.Popen([str(executable), "--admin-port", str(admin_port), "--port", str(remote_port),
                                            "--bind", "127.0.0.1", "--no-browser", "--no-auto-update",
                                            "--config-dir", str(config), "--media-root", str(media)],
                                           stdin=subprocess.DEVNULL, stdout=log, stderr=subprocess.STDOUT, env=environment)
                try:
                    admin = helpers.Admin(admin_port)
                    helpers.wait_for(lambda: admin.get("/api/state")["state"], 40, "the isolated real Admin host")
                    environment.update(SMARTSTAGE_PROBE_ADMIN_URL=f"http://127.0.0.1:{admin_port}/admin",
                                       SMARTSTAGE_PROBE_ORIGINAL_ONE=str(original), SMARTSTAGE_PROBE_ORIGINAL_TWO=str(second))
                    try:
                        result = subprocess.run([str(binary)], env=environment, capture_output=True, text=True,
                                                encoding="utf-8", errors="replace", timeout=150)
                    except subprocess.TimeoutExpired as error:
                        captured = error.stderr or b""
                        report["probeLog"] = (captured.decode("utf-8", "replace") if isinstance(captured, bytes) else captured)[-24000:]
                        raise AssertionError(f"Native Admin probe timed out; captured native progress:\n{report['probeLog']}") from error
                    report["probeLog"] = result.stderr[-18000:]
                    assert result.returncode == 0, f"Native Admin probe failed ({result.returncode}): {result.stderr}"
                    report.update(json.loads(result.stdout))
                    dropped = report["dropRequest"]["paths"]
                    chosen = report["chooserRequest"]["paths"]
                    assert len(dropped) == 2 and len(chosen) == 1
                    assert all(Path(value).samefile(path) for value, path in zip(dropped, (original, second)))
                    assert Path(chosen[0]).samefile(second)
                    assert hashes == [helpers.digest(path) for path in (original, second)]
                    assert not list(config.rglob("*.wav")), "Native import copied originals into configuration"
                    assert process.wait(timeout=20) == 0, "Authenticated Admin Quit did not stop the real host cleanly"
                    report.update(originalDropPathsAndBytesPreserved=True, nativeChooserOriginalPathPreserved=True,
                                  realAdminQuitStoppedHost=True)
                finally:
                    if process.poll() is None:
                        process.terminate()
                        try:
                            process.wait(timeout=15)
                        except subprocess.TimeoutExpired:
                            process.kill()
                            process.wait(timeout=5)
                    report["hostLog"] = (work / "host.log").read_text(encoding="utf-8", errors="replace")[-18000:]
            report["status"] = "passed"
    except Exception as error:
        report.update(status="failed", error=str(error))
        raise
    finally:
        evidence_path.parent.mkdir(parents=True, exist_ok=True)
        evidence_path.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    return report


if __name__ == "__main__":
    import argparse
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("executable", type=Path)
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    print(json.dumps(admin_window_checks(args.executable, args.output or args.executable.with_name(args.executable.name + ".native-admin.json")), indent=2))
