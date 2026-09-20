"""Run real Admin assets in the production WKWebView with own-process Cocoa observation."""
import importlib.util
import json
import os
from pathlib import Path
import plistlib
import subprocess
import tempfile


def admin_window_checks(executable, evidence_path):
    root = Path(__file__).resolve().parents[1]
    spec = importlib.util.spec_from_file_location("smartstage_admin_host_checks", Path(__file__).with_name("verify-auto-update.py"))
    helpers = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(helpers)
    report = {"status": "incomplete",
              "method": "Production Cocoa bridge with real loopback Admin assets/API/SSE; own-process WebKit observation and native NSPasteboard file URL delivery, not a physical mouse gesture"}
    process = None
    try:
        with tempfile.TemporaryDirectory(prefix="smartstage-native-admin-") as temporary:
            work = Path(temporary)
            config, media = work / "Saved show", work / "Originals – café"
            show = helpers.seed_show(config, media)
            original = Path(show["cues"][0]["path"])
            second = media / "Second original – 演出.wav"
            second.write_bytes(original.read_bytes())
            original_hashes = [helpers.digest(path) for path in (original, second)]
            bundle = work / "Smart Stage Admin Probe.app"
            contents = bundle / "Contents"
            binary = contents / "MacOS/native-admin-probe"
            binary.parent.mkdir(parents=True)
            with (contents / "Info.plist").open("wb") as stream:
                plistlib.dump({
                    "CFBundleIdentifier": "com.github.arizzi74.smartstage.admin-probe",
                    "CFBundleName": "Smart Stage Admin Probe", "CFBundlePackageType": "APPL",
                    "CFBundleExecutable": binary.name, "LSUIElement": False,
                    "NSAppTransportSecurity": {
                        "NSAllowsLocalNetworking": True,
                        "NSExceptionDomains": {"127.0.0.1": {"NSExceptionAllowsInsecureHTTPLoads": True}},
                    },
                }, stream)
            command = ["/usr/bin/clang", "-fobjc-arc", "-fblocks", "-mmacosx-version-min=12.0",
                       "-Wall", "-Wextra", str(root / "scripts/native-admin-darwin.m"),
                       str(root / "internal/platform/bridge_darwin.m"), "-o", str(binary)]
            for framework in ("AppKit", "AVFoundation", "CoreAudio", "CoreMedia", "CoreVideo",
                              "QuartzCore", "CoreGraphics", "IOKit", "UniformTypeIdentifiers", "WebKit"):
                command.extend(["-framework", framework])
            built = subprocess.run(command, capture_output=True, text=True, timeout=120)
            assert built.returncode == 0, f"Native Admin probe compilation failed: {built.stderr}"
            subprocess.run(["/usr/bin/codesign", "--force", "--sign", "-", str(bundle)], check=True,
                           capture_output=True, text=True, timeout=30)
            admin_port, remote_port = helpers.unused_port(), helpers.unused_port()
            while remote_port == admin_port:
                remote_port = helpers.unused_port()
            environment = os.environ.copy()
            environment.pop("SMARTSTAGE_APP_LAUNCH", None)
            with (work / "host.log").open("wb") as log:
                process = subprocess.Popen([
                    str(executable), "--admin-port", str(admin_port), "--port", str(remote_port),
                    "--bind", "127.0.0.1", "--no-browser", "--no-auto-update",
                    "--config-dir", str(config), "--media-root", str(media),
                ], stdin=subprocess.DEVNULL, stdout=log, stderr=subprocess.STDOUT, env=environment)
                try:
                    admin = helpers.Admin(admin_port)
                    helpers.wait_for(lambda: admin.get("/api/state")["state"], 30, "the isolated real Admin host")
                    environment["SMARTSTAGE_APP_LAUNCH"] = "1"
                    environment["SMARTSTAGE_APP_ICON"] = str(root / "assets/icon/smartstage.icns")
                    result = subprocess.run([
                        str(binary), f"http://127.0.0.1:{admin_port}/admin", str(original), str(second),
                    ], env=environment, capture_output=True, text=True, timeout=80)
                    report["probeLog"] = result.stderr[-16000:]
                    assert result.returncode == 0, f"Native Admin probe failed ({result.returncode}): {result.stderr}"
                    report.update(json.loads(result.stdout))
                    queued = report["queuedOriginalPaths"]
                    assert len(queued) == 2
                    assert all(Path(value).samefile(path) for value, path in zip(queued, (original, second)))
                    assert original_hashes == [helpers.digest(path) for path in (original, second)]
                    assert not list(config.rglob("*.wav")), "Native drop copied media into configuration"
                    assert process.wait(timeout=15) == 0, "Real Admin Quit did not stop the isolated host cleanly"
                    report["originalDropPathsAndBytesPreserved"] = True
                    report["realAdminQuitStoppedHost"] = True
                finally:
                    if process.poll() is None:
                        process.terminate()
                        try:
                            process.wait(timeout=15)
                        except subprocess.TimeoutExpired:
                            process.kill()
                            process.wait(timeout=5)
                    report["hostLog"] = (work / "host.log").read_text(encoding="utf-8", errors="replace")[-16000:]
            report["status"] = "passed"
    except Exception as error:
        report.update(status="failed", error=str(error))
        raise
    finally:
        evidence_path.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    return report
