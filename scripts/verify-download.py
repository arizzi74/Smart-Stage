#!/usr/bin/env python3
"""Download, checksum and extract a release ZIP; optionally verify native startup.

Development/CI helper only. Users download and extract ZIPs or use the installer.
Native Windows Admin verification requires Microsoft Evergreen WebView2.
"""
import argparse
import hashlib
import http.client
import json
import os
from pathlib import Path
import platform
import re
import signal
import socket
import stat
import subprocess
import tempfile
import time
import urllib.request
import zipfile


def gateway_default(version):
    legacy = re.fullmatch(r"v0[.]1[.]0-preview[.](\d+)", version)
    return not legacy or int(legacy[1]) >= 15


def fetch(url, destination):
    request = urllib.request.Request(url, headers={"User-Agent": "Smart-Stage-release-verification"})
    with urllib.request.urlopen(request, timeout=120) as response:
        destination.write_bytes(response.read())


def extract(archive, checksum, destination, target_os):
    text = checksum.read_text(encoding="utf-8").strip()
    match = re.fullmatch(r"([0-9a-fA-F]{64})\s+\*?([^\r\n]+)", text)
    if not match or match[2] != archive.name:
        raise ValueError("Invalid archive checksum record")
    digest = hashlib.sha256(archive.read_bytes()).hexdigest()
    if digest != match[1].lower():
        raise ValueError("Release ZIP checksum mismatch")
    filename = "smartstage.exe" if target_os == "windows" else "smartstage"
    with zipfile.ZipFile(archive) as source:
        entries = source.infolist()
        if len(entries) != 1 or entries[0].filename != filename:
            raise ValueError("Release ZIP must contain exactly its named executable")
        mode = entries[0].external_attr >> 16
        if not stat.S_ISREG(mode) or stat.S_IMODE(mode) != 0o755:
            raise ValueError("Release ZIP executable permissions are missing")
        executable = source.read(entries[0])
    destination.mkdir(parents=True, exist_ok=True)
    binary = destination / filename
    if binary.exists():
        raise FileExistsError(f"Refusing to replace {binary}")
    binary.write_bytes(executable)
    binary.chmod(0o755)
    return binary, {
        "archive": archive.name,
        "archiveSHA256": digest,
        "archiveEntries": [filename],
        "executableSHA256": hashlib.sha256(executable).hexdigest(),
        "executablePermissions": "0755",
    }


def unused_port():
    with socket.socket() as candidate:
        candidate.bind(("127.0.0.1", 0))
        return candidate.getsockname()[1]


def get(port, path):
    connection = http.client.HTTPConnection("127.0.0.1", port, timeout=1)
    try:
        connection.request("GET", path)
        response = connection.getresponse()
        return response.status, response.read().decode("utf-8", errors="replace")
    finally:
        connection.close()


def smoke(binary, target_os, target_arch, version, report):
    native_windows = False
    if target_os == "windows":
        from windows_app_checks import dedicated_windows_release, inspect_gui_executable, wait_for_admin_window
        native_windows = dedicated_windows_release(version)
        if native_windows:
            report["windowsExecutable"] = inspect_gui_executable(binary)
            failed = subprocess.run([str(binary), "--no-browser", "--admin-port", "-1"],
                                    capture_output=True, text=True, timeout=15)
            assert failed.returncode != 0 and "must be between 0 and 65535" in failed.stderr, \
                f"Headless startup did not report its error and exit: {failed}"
            report["headlessStartupErrorsExitWithoutDialog"] = True
    actual = subprocess.check_output([str(binary), "--version"], text=True, timeout=30).strip()
    if not actual.endswith(f"{target_os}/{target_arch}") or f" {version} " not in actual:
        raise AssertionError(f"Unexpected downloaded executable version: {actual}")
    report["version"] = actual
    admin_port, remote_port = unused_port(), unused_port()
    while remote_port == admin_port:
        remote_port = unused_port()
    with tempfile.TemporaryDirectory(prefix="smartstage-download-config-") as config:
        log_path = binary.parent / "startup.log"
        with log_path.open("wb") as log:
            process = subprocess.Popen([
                str(binary), "--admin-port", str(admin_port), "--port", str(remote_port),
                "--bind", "127.0.0.1", "--config-dir", config,
            ], stdout=log, stderr=subprocess.STDOUT)
            try:
                deadline = time.monotonic() + 60
                admin_ready = False
                browser_dispatched = False
                native_requested = False
                while time.monotonic() < deadline:
                    if process.poll() is not None:
                        raise AssertionError(f"Downloaded application exited: {log_path.read_text()}")
                    startup = log_path.read_text(encoding="utf-8", errors="replace")
                    if "Could not open the system browser" in startup:
                        raise AssertionError(f"Automatic system browser dispatch failed: {startup}")
                    browser_dispatched = "Opened Admin in the system browser" in startup
                    native_requested = "Requested the dedicated Admin window" in startup
                    try:
                        status, page = get(admin_port, "/admin")
                        admin_ready = status == 200 and "Smart Stage" in page
                        if admin_ready and (native_requested if native_windows else browser_dispatched):
                            break
                    except OSError:
                        pass
                    time.sleep(0.25)
                else:
                    raise AssertionError(
                        f"Downloaded application startup incomplete (Admin={admin_ready}, "
                        f"system browser dispatch={browser_dispatched}, native window request={native_requested}): {log_path.read_text()}"
                    )
                report["adminServedOnLoopback"] = True
                report["adminURL"] = f"http://127.0.0.1:{admin_port}/admin"
                if native_windows:
                    assert not browser_dispatched, "Windows app unexpectedly opened an external browser"
                    report["nativeAdminWindow"] = wait_for_admin_window(process.pid, timeout=60)
                    report["automaticDedicatedAdminWindowVisible"] = True
                    report["automaticSystemBrowserDispatchAccepted"] = False
                else:
                    # The OS accepted the URL hand-off. Browser UI/control behavior
                    # is independently exercised by the native browser workflow.
                    report["automaticSystemBrowserDispatchAccepted"] = True
                if gateway_default(version):
                    with socket.socket() as remote_probe:
                        remote_probe.settimeout(2)
                        assert remote_probe.connect_ex(("127.0.0.1", remote_port)) != 0, \
                            "A fresh installation must not listen for incoming LAN connections"
                    report.update(gatewayModeDefault=True, lanListenerClosedByDefault=True)
                else:
                    status, _ = get(remote_port, "/admin")
                    assert status in (403, 404), f"Legacy remote listener served Admin: HTTP {status}"
                    report.update(legacyLANRelease=True, remoteListenerRejectsAdmin=True)
            finally:
                if process.poll() is None:
                    process.terminate() if os.name == "nt" else process.send_signal(signal.SIGINT)
                try:
                    process.wait(timeout=15)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=5)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", required=True, help="Published release tag")
    parser.add_argument("--os", choices=("darwin", "windows"), default=platform.system().lower())
    parser.add_argument("--arch", choices=("arm64", "amd64"), default={
        "aarch64": "arm64", "arm64": "arm64", "x86_64": "amd64", "amd64": "amd64",
    }.get(platform.machine().lower()))
    parser.add_argument("--destination", required=True, type=Path)
    parser.add_argument("--archive", type=Path, help="Verify a locally built ZIP instead of downloading")
    parser.add_argument("--smoke", action="store_true", help="Check native version, automatic Admin presentation, loopback binding and closed default LAN listener")
    args = parser.parse_args()
    if args.os not in ("darwin", "windows") or not args.arch:
        parser.error("Select a supported target with --os and --arch")
    if not re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9.]+)?|dev", args.version):
        parser.error("Invalid release tag")
    destination = args.destination.resolve()
    destination.mkdir(parents=True, exist_ok=True)
    report_path = destination / "download-verification.json"
    report = {"status": "incomplete", "target": f"{args.os}/{args.arch}", "release": args.version}
    try:
        name = f"smartstage-{args.os}-{args.arch}.zip"
        with tempfile.TemporaryDirectory(prefix="smartstage-release-") as scratch:
            archive = args.archive.resolve() if args.archive else Path(scratch) / name
            checksum = archive.with_name(archive.name + ".sha256")
            if args.archive and archive.name != name:
                raise ValueError(f"Expected archive named {name}")
            if not args.archive:
                base = f"https://github.com/arizzi74/Smart-Stage/releases/download/{args.version}"
                fetch(f"{base}/{name}", archive)
                fetch(f"{base}/{name}.sha256", checksum)
                report["downloadURL"] = f"{base}/{name}"
            binary, details = extract(archive, checksum, destination, args.os)
            report.update(details)
        if args.smoke:
            smoke(binary, args.os, args.arch, args.version, report)
        report["status"] = "passed"
        print(json.dumps(report, indent=2))
    except Exception as error:
        report["status"] = "failed"
        report["error"] = str(error)
        raise
    finally:
        report_path.write_text(json.dumps(report, indent=2) + "\n")


if __name__ == "__main__":
    main()
