#!/usr/bin/env python3
"""Exercise production startup updates on an ephemeral native CI runner.

The fixture is the release's exact source, built as v0.0.0-preview.1. It uses
the real public GitHub API, downloads and updater. There is no alternate feed,
transport, helper or startup acknowledgement. Gateway mode is the default;
updates must leave incoming LAN access closed without firewall authorization.
"""
import argparse
import ctypes
import hashlib
import http.client
import json
import os
from pathlib import Path
import platform
import plistlib
import re
import shutil
import signal
import socket
import stat
import subprocess
import sys
import tempfile
import time
import wave
import zipfile


ROOT = Path(__file__).resolve().parents[1]
FIXTURE_VERSION = "v0.0.0-preview.1"


def gateway_default(version):
    legacy = re.fullmatch(r"v0[.]1[.]0-preview[.](\d+)", version)
    return not legacy or int(legacy[1]) >= 15


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def run(command, **options):
    return subprocess.run(command, check=True, capture_output=True, text=True,
                          timeout=options.pop("timeout", 90), **options)


def unused_port():
    with socket.socket() as candidate:
        candidate.bind(("127.0.0.1", 0))
        return candidate.getsockname()[1]


def process_image(pid):
    if sys.platform == "darwin":
        library = ctypes.CDLL("/usr/lib/libproc.dylib")
        library.proc_pidpath.argtypes = [ctypes.c_int, ctypes.c_void_p, ctypes.c_uint32]
        library.proc_pidpath.restype = ctypes.c_int
        buffer = ctypes.create_string_buffer(4096)
        return Path(os.fsdecode(buffer.value)) if library.proc_pidpath(pid, buffer, len(buffer)) > 0 else None
    from ctypes import wintypes
    kernel = ctypes.WinDLL("kernel32", use_last_error=True)
    kernel.OpenProcess.argtypes = [wintypes.DWORD, wintypes.BOOL, wintypes.DWORD]
    kernel.OpenProcess.restype = wintypes.HANDLE
    kernel.CloseHandle.argtypes = [wintypes.HANDLE]
    kernel.QueryFullProcessImageNameW.argtypes = [wintypes.HANDLE, wintypes.DWORD, wintypes.LPWSTR, ctypes.POINTER(wintypes.DWORD)]
    handle = kernel.OpenProcess(0x1000, False, pid)
    if not handle:
        return None
    try:
        buffer = ctypes.create_unicode_buffer(32768)
        length = wintypes.DWORD(len(buffer))
        if kernel.QueryFullProcessImageNameW(handle, 0, buffer, ctypes.byref(length)):
            return Path(buffer.value)
        return None
    finally:
        kernel.CloseHandle(handle)


def process_alive(pid):
    if not pid:
        return False
    if os.name != "nt":
        try:
            os.kill(pid, 0)
            # Reaped via the Popen owner when available. Zombies are no longer
            # running and cannot own the executable or a listener.
            result = subprocess.run(["/bin/ps", "-p", str(pid), "-o", "stat="],
                                    capture_output=True, text=True, timeout=5)
            return result.returncode == 0 and not result.stdout.strip().startswith("Z")
        except ProcessLookupError:
            return False
    from ctypes import wintypes
    kernel = ctypes.WinDLL("kernel32", use_last_error=True)
    kernel.OpenProcess.argtypes = [wintypes.DWORD, wintypes.BOOL, wintypes.DWORD]
    kernel.OpenProcess.restype = wintypes.HANDLE
    kernel.WaitForSingleObject.argtypes = [wintypes.HANDLE, wintypes.DWORD]
    kernel.CloseHandle.argtypes = [wintypes.HANDLE]
    handle = kernel.OpenProcess(0x100000, False, pid)
    if not handle:
        return False
    try:
        return kernel.WaitForSingleObject(handle, 0) == 0x102
    finally:
        kernel.CloseHandle(handle)


def listener_pid(port, executable):
    if sys.platform == "darwin":
        result = subprocess.run(["/usr/sbin/lsof", "-nP", f"-iTCP:{port}", "-sTCP:LISTEN", "-Fp"],
                                capture_output=True, text=True, timeout=15)
        candidates = [int(line[1:]) for line in result.stdout.splitlines() if line.startswith("p")]
    else:
        result = run(["pwsh", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command",
                      f"@(Get-NetTCPConnection -State Listen -LocalPort {port} -ErrorAction SilentlyContinue | Select-Object -ExpandProperty OwningProcess -Unique) | ConvertTo-Json -Compress"], timeout=30)
        values = json.loads(result.stdout) if result.stdout.strip() else []
        candidates = values if isinstance(values, list) else [values]
    for pid in candidates:
        image = process_image(pid)
        if image:
            try:
                if os.path.samefile(image, executable):
                    return pid
            except OSError:
                pass
    return None


class Admin:
    def __init__(self, port):
        self.port, self.cookie, self.csrf = port, "", ""
        self.origin = f"http://127.0.0.1:{port}"

    def request(self, method, path, body=None):
        connection = http.client.HTTPConnection("127.0.0.1", self.port, timeout=3)
        headers = {"Origin": self.origin, "Cookie": self.cookie}
        data = None
        if body is not None:
            data = json.dumps(body)
            headers.update({"Content-Type": "application/json", "X-CSRF-Token": self.csrf})
        try:
            connection.request(method, path, body=data, headers=headers)
            response = connection.getresponse()
            data = response.read()
            if response.getheader("Set-Cookie"):
                self.cookie = response.getheader("Set-Cookie").split(";", 1)[0]
            return response.status, json.loads(data)
        finally:
            connection.close()

    def session(self):
        status, value = self.request("POST", "/api/local-session", {})
        assert status == 200 and value["role"] == "admin", (status, value)
        self.csrf = value["csrfToken"]

    def get(self, path):
        status, value = self.request("GET", path)
        if status == 401:
            self.session()
            status, value = self.request("GET", path)
        assert status == 200, (status, value)
        return value


def wait_for(predicate, timeout, description):
    deadline = time.monotonic() + timeout
    last_error = None
    while time.monotonic() < deadline:
        try:
            result = predicate()
            if result:
                return result
        except (OSError, http.client.HTTPException, json.JSONDecodeError) as error:
            last_error = error
        time.sleep(0.25)
    raise AssertionError(f"Timed out waiting for {description}; last connection error: {last_error}")


def belongs_to_installation(executable, installation):
    if not executable:
        return False
    # Compare directory identity rather than spelling: Windows short/long paths
    # and macOS NFC/NFD names can identify the same private installation.
    for parent in executable.resolve().parents:
        try:
            if os.path.samefile(parent, installation):
                return True
        except OSError:
            continue
    return False


def owned_pids(installation):
    if os.name == "nt":
        # Enumerate numeric IDs only. PowerShell's UTF-8 path output decoded
        # with a Windows locale can corrupt accented names and hide the test's
        # processes. Read executable paths through the Unicode kernel API.
        result = run(["pwsh", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command",
                      "@(Get-Process | Select-Object -ExpandProperty Id) | ConvertTo-Json -Compress"], timeout=30)
        pids = json.loads(result.stdout) if result.stdout.strip() else []
        if isinstance(pids, int):
            pids = [pids]
        candidates = [(pid, process_image(pid)) for pid in pids]
    else:
        result = run(["/bin/ps", "-axo", "pid="], timeout=10)
        candidates = [(int(value), process_image(int(value))) for value in result.stdout.split()]
    result = []
    for pid, executable in candidates:
        if belongs_to_installation(executable, installation):
            result.append(pid)
    return result


def stop_owned(pid, installation):
    executable = process_image(pid)
    if not belongs_to_installation(executable, installation):
        return
    if os.name == "nt":
        # Cleanup applies only to a verified fixture/replacement path in this
        # test's private directory. The normal updater's shutdown is tested
        # independently by the old process exiting with code zero.
        result = subprocess.run(["taskkill", "/PID", str(pid), "/F"],
                                capture_output=True, text=True, timeout=15)
        if result.returncode and process_alive(pid):
            raise RuntimeError(f"Could not stop test process {pid}: {result.stderr}")
        wait_for(lambda: not process_alive(pid), 10, f"test process {pid} to exit")
        return
    try:
        os.kill(pid, signal.SIGTERM)
        deadline = time.monotonic() + 10
        while process_alive(pid) and time.monotonic() < deadline:
            time.sleep(0.1)
        if process_alive(pid):
            os.kill(pid, signal.SIGKILL)
    except ProcessLookupError:
        pass


def seed_show(config, media):
    config.mkdir()
    media.mkdir()
    audio = media / "Operator's opening – café.wav"
    with wave.open(str(audio), "wb") as output:
        output.setnchannels(1)
        output.setsampwidth(2)
        output.setframerate(8000)
        output.writeframes(b"\x00\x00" * 8000)
    show = {"schema": 1, "playlistRevision": 19,
            "outputs": {"audioId": "default", "displayId": "", "allowPrimary": False},
            "cues": [{"id": "preserve-cue", "label": "Opening cue – keep this", "path": str(audio),
                      "cache": {"status": "unchecked", "media": {}, "size": 0, "modified": 0}}]}
    (config / "state.json").write_text(json.dumps(show, ensure_ascii=False) + "\n", encoding="utf-8")
    (config / "operator-note.txt").write_text("Saved show data stays outside the app.\n")
    return show


def prepare_fixture(fixture, installation, target_os, arch, report, fixture_source=None):
    manifest = json.loads((fixture / "fixture.json").read_text())
    name = f"fixture-{target_os}-{arch}{'.app.zip' if target_os == 'darwin' else '.zip'}"
    assert manifest["version"] == FIXTURE_VERSION and manifest["target"] == f"{target_os}/{arch}", manifest
    assert manifest["archive"] == name, manifest
    expected_source = fixture_source or os.environ.get("GITHUB_SHA")
    if expected_source:
        assert manifest["sourceSHA"] == expected_source, "Fixture must use the selected release's exact source commit"
        report["expectedFixtureSource"] = expected_source
    archive = fixture / name
    assert digest(archive) == manifest["archiveSHA256"], "Fixture archive checksum mismatch"
    installation.mkdir()
    if target_os == "darwin":
        run(["/usr/bin/ditto", "-x", "-k", str(archive), str(installation)])
        target = installation / "Smart Stage.app"
        core = target / "Contents/MacOS/smartstage"
        run(["/usr/bin/codesign", "--verify", "--deep", "--strict", str(target)])
    else:
        with zipfile.ZipFile(archive) as source:
            assert source.namelist() == ["smartstage.exe"], "Portable fixture must contain one executable"
            info = source.infolist()[0]
            assert stat.S_ISREG(info.external_attr >> 16)
            core = installation / "smartstage.exe"
            core.write_bytes(source.read(info))
            core.chmod(0o755)
        target = core
    assert digest(core) == manifest["executableSHA256"], "Fixture core checksum mismatch"
    version = run([str(core), "--version"]).stdout.strip()
    assert f" {FIXTURE_VERSION} " in version and version.endswith(f"{target_os}/{arch}"), version
    report.update(fixture=manifest, fixtureVersionOutput=version)
    return target, core


def verify(args, output, report):
    # Independently download the expected public portable artifact. The updater
    # will perform its own discovery/download; this establishes its result bytes.
    reference = output / "reference"
    run([sys.executable, str(ROOT / "scripts/verify-download.py"), "--version", args.version,
         "--os", args.os, "--arch", args.arch, "--destination", str(reference)], timeout=240)
    reference_report = json.loads((reference / "download-verification.json").read_text())
    report["publishedDownload"] = reference_report
    native_windows = False
    if args.os == "windows":
        from windows_app_checks import dedicated_windows_release, inspect_gui_executable, wait_for_admin_window
        native_windows = dedicated_windows_release(args.version)
    with tempfile.TemporaryDirectory(prefix="smartstage update verification ") as temporary:
        work = Path(temporary).resolve()
        installation = work / "Operator's applications – café"
        config, media = work / "Saved show", work / "Host media"
        expected_show = seed_show(config, media)
        note_hash = digest(config / "operator-note.txt")
        audio_hash = digest(Path(expected_show["cues"][0]["path"]))
        target, core = prepare_fixture(args.fixture.resolve(), installation, args.os, args.arch, report, args.fixture_source)
        admin_port, command_port = unused_port(), unused_port()
        while command_port == admin_port:
            command_port = unused_port()
        admin = Admin(admin_port)
        arguments = ["--admin-port", str(admin_port), "--port", str(command_port), "--bind", "127.0.0.1",
                     "--config-dir", str(config), "--media-root", str(media)]
        if not native_windows:
            arguments.append("--no-browser")
        environment = os.environ.copy()
        environment.pop("SMARTSTAGE_SKIP_FIREWALL", None)
        environment.pop("SMARTSTAGE_CONFIGURE_LAN_FIREWALL", None)
        if not gateway_default(args.version):
            environment["SMARTSTAGE_SKIP_FIREWALL"] = "1"
        old_pid = new_pid = None
        process = None
        snapshot = None
        if args.os == "darwin":
            from macos_app_checks import launch_snapshot
            snapshot = launch_snapshot()
        try:
            with (output / "fixture-startup.log").open("wb") as log:
                command = ["/usr/bin/open", "-n", "-W", str(target), "--args", *arguments] if args.os == "darwin" else [str(core), *arguments]
                process = subprocess.Popen(command, cwd=work, env=environment, stdin=subprocess.DEVNULL,
                                           stdout=log, stderr=subprocess.STDOUT)
                if args.os == "windows":
                    old_pid = process.pid
                old_state = wait_for(lambda: admin.get("/api/state")["state"], 45, "the fixture's local Admin")
                old_update = admin.get("/api/update")
                report["initialUpdateStatus"] = old_update
                assert old_update["currentVersion"] == FIXTURE_VERSION, old_update
                report.update(oldInstanceID=old_state["instanceId"],
                              startupUpdatePending=old_state.get("updatePending", False), initialUpdatePhase=old_update["phase"])
                assert old_state["state"] == "stopped" and not old_state["stageEnabled"], old_state
                assert old_state["updatePending"], "Automatic startup update must reserve playback before serving Admin"
                play_status, play_result = admin.request("POST", "/api/play", {
                    "requestId": "ci-update-guard-request", "instanceId": old_state["instanceId"],
                    "stopEpoch": old_state["stopEpoch"], "cueId": "preserve-cue"})
                assert play_status == 409 and play_result["error"]["code"] == "updating", (play_status, play_result)
                report["startupReservationRejectedPLAY"] = True
                if old_pid is None:
                    old_pid = wait_for(lambda: listener_pid(admin_port, core), 15, "the fixture's actual core PID")
                report["oldPID"] = old_pid
                outcome_path = config / "update-result.json"
                observed_phases = [old_update["phase"]]
                deadline = time.monotonic() + 420
                outcome = None
                while time.monotonic() < deadline:
                    if outcome_path.exists():
                        outcome = json.loads(outcome_path.read_text())
                        if outcome["status"] != "updated":
                            raise AssertionError(f"Updater did not install the release: {outcome}")
                        break
                    try:
                        status = admin.get("/api/update")
                        report["lastUpdateStatus"] = status
                        if status["phase"] not in observed_phases:
                            observed_phases.append(status["phase"])
                        if status["phase"] in ("error", "unsupported"):
                            raise AssertionError(f"Production updater failed: {status}")
                    except (OSError, http.client.HTTPException, json.JSONDecodeError):
                        pass
                    time.sleep(0.25)
                assert outcome is not None, "No durable update result was written"
                assert outcome["version"] == args.version, outcome
                report.update(outcome=outcome, observedUpdatePhases=observed_phases)
                process.wait(timeout=15)
                assert process.returncode == 0, f"Fixture did not exit cleanly: {process.returncode}"
                assert not process_alive(old_pid), "Old executable process remains running"
                new_state = wait_for(lambda: admin.get("/api/state")["state"], 20, "the updated local Admin")
                new_pid = listener_pid(admin_port, core)
                assert new_pid and new_pid != old_pid, "Updated server must belong to a new executable process"
                assert new_state["instanceId"] != old_state["instanceId"], "Application state did not restart"
                assert new_state["state"] == "stopped" and not new_state["activeCueId"] and not new_state["stageEnabled"], new_state
                assert not new_state["updatePending"], "Updated application is still reserved"
                current = admin.get("/api/update")
                report["finalUpdateStatus"] = current
                assert current["currentVersion"] == args.version, current
                assert digest(core) == reference_report["executableSHA256"], "Updated executable differs from the independently verified public release"
                version = run([str(core), "--version"]).stdout.strip()
                assert f" {args.version} " in version and version.endswith(f"{args.os}/{args.arch}"), version
                actual_show = admin.get("/api/playlist")
                for field in ("schema", "playlistRevision", "outputs"):
                    assert actual_show[field] == expected_show[field], f"Saved show {field} changed"
                assert [{k: c[k] for k in ("id", "label", "path")} for c in actual_show["cues"]] == [
                    {k: c[k] for k in ("id", "label", "path")} for c in expected_show["cues"]], "Saved cue identity, order, label or source changed"
                assert digest(config / "operator-note.txt") == note_hash, "Other saved-show files changed"
                assert digest(Path(expected_show["cues"][0]["path"])) == audio_hash, "Host media changed"
                if gateway_default(args.version):
                    with socket.socket() as remote_probe:
                        remote_probe.settimeout(2)
                        assert remote_probe.connect_ex(("127.0.0.1", command_port)) != 0, \
                            "Default gateway mode opened an incoming LAN listener after updating"
                    report.update(gatewayModeDefault=True, lanListenerClosedAfterUpdate=True)
                else:
                    remote = http.client.HTTPConnection("127.0.0.1", command_port, timeout=3)
                    try:
                        remote.request("GET", "/command")
                        response = remote.getresponse()
                        assert response.status == 200 and b"Smart Stage" in response.read()
                    finally:
                        remote.close()
                    report.update(legacyLANRelease=True, adminAndCommandPortsPreserved=True)
                report.update(newPID=new_pid, newInstanceID=new_state["instanceId"], installedVersionOutput=version,
                              installedExecutableSHA256=digest(core), oldProcessExitedCleanly=True,
                              actualPublishedBytesInstalled=True, adminPortPreserved=True,
                              savedShowPreserved=True, hostMediaPreserved=True, restartedWithoutAutoplay=True,
                              updateHTTPInstallRequestsSent=0, productionReleaseDiscovery=True,
                              productionArchiveValidation=True, productionHelperHandoff=True,
                              startupAcknowledgedBeforeBackupRemoval=True)
                assert not (Path(outcome["work"]) / "previous").exists(), "Successful update left its previous-version backup"
                if args.os == "darwin":
                    from macos_app_checks import dedicated_admin_checks, dock_app_checks, terminal_pids
                    assert not (terminal_pids() - snapshot["terminalPIDs"]), "Automatic update opened Terminal"
                    if gateway_default(args.version):
                        assert not outcome.get("message", ""), "Gateway-mode update must not require a firewall approval or skip override"
                    else:
                        assert "skipped" in outcome.get("message", "").lower(), "Legacy LAN update test must explicitly skip firewall approval"
                    run(["/usr/bin/codesign", "--verify", "--deep", "--strict", str(target)])
                    report.update(dock_app_checks(target, new_pid))
                    info = plistlib.loads((target / "Contents/Info.plist").read_bytes())
                    if info.get("SmartStageNativeAdminWindow"):
                        report.update(dedicated_admin_checks(
                            target, new_pid, snapshot,
                            lambda _bundle: listener_pid(admin_port, core), request_open=True))
                    report.update(restartedWithoutTerminal=True, appSignatureVerified=True, firewallAuthorizationExercised=False)
                else:
                    report["windowsReplacementAfterOldExecutableExit"] = True
                    if native_windows:
                        report["windowsExecutable"] = inspect_gui_executable(core)
                        window = wait_for_admin_window(new_pid, timeout=60)
                        report["nativeAdminWindow"] = window
                        wait_for(lambda: "Loaded native Admin page" in (config / "update.log").read_text(
                            encoding="utf-8", errors="replace"), 60, "the updated native Admin navigation")
                        assert "Opened Admin in the system browser" not in (config / "update.log").read_text(
                            encoding="utf-8", errors="replace"), "Windows update launched an external browser"
                        report.update(nativeAdminPageNavigationCompleted=True,
                                      nativeAdminWindowVisible=True, nativeAdminWindowIconPresent=True,
                                      externalBrowserDispatched=False,
                                      nativePageJavaScriptVerified=False)
                        # A second launch must activate the retained GUI process,
                        # not create another server or another Admin window.
                        run([str(core), *arguments, "--no-auto-update"], cwd=work, env=environment, timeout=30)
                        reopened = wait_for_admin_window(new_pid, timeout=30)
                        assert reopened["hwnd"] == window["hwnd"], "Relaunch replaced the native Admin window"
                        assert listener_pid(admin_port, core) == new_pid, "Relaunch replaced the native host process"
                        assert admin.get("/api/state")["state"]["instanceId"] == new_state["instanceId"], "Relaunch restarted playback state"
                        report.update(relaunchPreservedNativeAdminWindow=True,
                                      relaunchPreservedNativeProcess=True,
                                      relaunchPreservedInstance=True,
                                      nativeAdminWindowAfterRelaunch=reopened)
                        status, quitting = admin.request("POST", "/api/quit", {})
                        assert status == 202 and quitting.get("quitting") is True, (status, quitting)
                        wait_for(lambda: not process_alive(new_pid), 30, "the updated app to exit after Admin Quit")
                        for port in (admin_port, command_port):
                            with socket.socket() as probe:
                                probe.settimeout(2)
                                assert probe.connect_ex(("127.0.0.1", port)) != 0, "Quit left a native listener open"
                        report.update(authenticatedAdminQuitVerified=True,
                                      nativeProcessExitedAfterQuit=True,
                                      noListenersAfterQuit=True)
        finally:
            # Capture diagnostics before cleanup removes the private installation.
            for name in ("update.log", "update.log.1", "update-result.json"):
                source = config / name
                if source.is_file():
                    shutil.copy2(source, output / name)
            if args.os == "darwin" and snapshot:
                from macos_app_checks import appended_log
                try:
                    (output / "mac-app.log").write_text(appended_log(snapshot), encoding="utf-8")
                except OSError:
                    pass
            cleanup_errors = []
            # Stop helpers first, so they cannot relaunch during test cleanup.
            try:
                known_pids = {pid for pid in (old_pid, new_pid) if pid}
                outcome_file = config / "update-result.json"
                if outcome_file.is_file():
                    helper_pid = json.loads(outcome_file.read_text()).get("helperPID")
                    if helper_pid:
                        known_pids.add(helper_pid)
                pids = list(set(owned_pids(installation)) | known_pids)
                pids.sort(key=lambda pid: "update-helper" not in str(process_image(pid)))
                for pid in pids:
                    try:
                        stop_owned(pid, installation)
                    except (OSError, subprocess.SubprocessError, RuntimeError, AssertionError) as error:
                        cleanup_errors.append(str(error))
                remaining = owned_pids(installation)
                remaining = sorted(set(remaining) | {pid for pid in known_pids if process_alive(pid)})
                if remaining:
                    cleanup_errors.append(f"Test-owned processes still running: {remaining}")
            except (OSError, subprocess.SubprocessError) as error:
                cleanup_errors.append(str(error))
            if process is not None:
                try:
                    process.wait(timeout=15)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=5)
            if not cleanup_errors:
                # WebView2 releases its per-user data files asynchronously after
                # controller shutdown; still require complete private cleanup.
                deadline = time.monotonic() + (30 if native_windows else 8)
                while work.exists():
                    try:
                        shutil.rmtree(work)
                    except OSError as error:
                        if time.monotonic() >= deadline:
                            cleanup_errors.append(f"Test directory could not be removed after process exit: {error}")
                            break
                        time.sleep(0.1)
                report["testDirectoryRemoved"] = not work.exists()
            report["testProcessCleanupPassed"] = not cleanup_errors
            if cleanup_errors:
                report["cleanupErrors"] = cleanup_errors
                raise AssertionError(f"Could not clean up test-owned processes: {cleanup_errors}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", required=True)
    parser.add_argument("--os", choices=("darwin", "windows"), required=True)
    parser.add_argument("--arch", choices=("amd64", "arm64"), required=True)
    parser.add_argument("--fixture", type=Path, required=True)
    parser.add_argument("--fixture-source", help="Expected source commit for a fixture downloaded from an earlier release run")
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    if not re.fullmatch(r"v\d+\.\d+\.\d+(?:-[A-Za-z0-9.]+)?", args.version):
        parser.error("Expected a published release version")
    if args.fixture_source and not re.fullmatch(r"[0-9a-f]{40}", args.fixture_source):
        parser.error("Expected a full lowercase fixture source commit SHA")
    actual_os = "windows" if os.name == "nt" else sys.platform
    actual_arch = {"aarch64": "arm64", "arm64": "arm64", "amd64": "amd64", "x86_64": "amd64"}.get(platform.machine().lower())
    if (args.os, args.arch) != (actual_os, actual_arch):
        parser.error("Run this check natively on the selected OS and architecture")
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    report = {"status": "incomplete", "target": f"{args.os}/{args.arch}", "release": args.version,
              "fixtureScope": "Exact release source with older version metadata; actual public GitHub discovery/download/replacement/relaunch",
              "physicalPlaybackVerified": False}
    try:
        verify(args, output, report)
        report["status"] = "passed"
    except Exception as error:
        report.update(status="failed", error=str(error))
        raise
    finally:
        (output / "result.json").write_text(json.dumps(report, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
        print(json.dumps(report, indent=2, ensure_ascii=False))


if __name__ == "__main__":
    main()
