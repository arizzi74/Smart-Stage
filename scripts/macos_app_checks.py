"""Native checks shared by Finder-bundle and published-installer verification."""
import os
from pathlib import Path
import signal
import socket
import subprocess
import time


LOG_PATH = Path.home() / "Library/Logs/Smart Stage/smartstage.log"
BUNDLE_ID = "com.github.arizzi74.smartstage"


def terminal_pids():
    result = subprocess.run(["/usr/bin/pgrep", "-x", "Terminal"],
                            text=True, capture_output=True, timeout=10)
    if result.returncode not in (0, 1):
        raise RuntimeError(f"Cannot inspect Terminal processes: {result.stderr}")
    return set(result.stdout.split())


def launch_snapshot():
    return {"terminalPIDs": terminal_pids(),
            "logOffset": LOG_PATH.stat().st_size if LOG_PATH.exists() else 0}


def appended_log(snapshot):
    with LOG_PATH.open("rb") as stream:
        offset = snapshot["logOffset"]
        if LOG_PATH.stat().st_size >= offset:
            stream.seek(offset)
        return stream.read().decode("utf-8", errors="replace")


def background_launch_checks(bundle, pid, snapshot, find_core_pid):
    """Inspect the actual running core, then exercise LaunchServices reopen."""
    assert not (terminal_pids() - snapshot["terminalPIDs"]), "Finder launch started Terminal"
    tty = subprocess.check_output(["/bin/ps", "-p", str(pid), "-o", "tty="],
                                  text=True, timeout=10).strip()
    assert tty == "??", f"The app has a controlling terminal: {tty}"
    descriptors = subprocess.check_output(
        ["/usr/sbin/lsof", "-a", "-p", str(pid), "-d", "0,1,2", "-Ffn"],
        text=True, timeout=15)
    paths = {}
    descriptor = None
    for line in descriptors.splitlines():
        if line.startswith("f"):
            descriptor = line[1:]
        elif line.startswith("n") and descriptor is not None:
            paths[descriptor] = line[1:]
    assert paths.get("0") == "/dev/null", paths
    assert all(paths.get(fd) == str(LOG_PATH) for fd in ("1", "2")), paths
    deadline = time.monotonic() + 10
    while time.monotonic() < deadline:
        log = appended_log(snapshot)
        if "Admin: http://127.0.0.1:8787/admin" in log and "Smart Stage menu bar ready" in log:
            break
        time.sleep(0.25)
    else:
        raise AssertionError("Startup URL and menu readiness were not written to the app log")

    reopen_snapshot = {"logOffset": LOG_PATH.stat().st_size}
    subprocess.run(["/usr/bin/open", str(bundle)], check=True, timeout=15)
    deadline = time.monotonic() + 15
    while time.monotonic() < deadline:
        assert find_core_pid(bundle) == pid, "Reopening the app replaced its running core"
        if "Reopened Admin in the system browser" in appended_log(reopen_snapshot):
            break
        time.sleep(0.25)
    else:
        raise AssertionError("Reopening the app did not dispatch Admin to the system browser")
    assert not (terminal_pids() - snapshot["terminalPIDs"]), "Reopening the app started Terminal"
    return {"finderDidNotStartTerminal": True, "coreHasNoControllingTerminal": True,
            "standardInputIsDevNull": True, "stdoutAndStderrUseAppLog": True,
            "startupURLWrittenToAppLog": True, "appLogPath": str(LOG_PATH),
            "reopenKeptSameCorePID": True, "reopenDispatchedAdminBrowser": True}


def quit_background_app(pid):
    # The standard Quit AppleEvent reaches NSApplication's real termination
    # handler without Accessibility or synthetic clicks on a menu item.
    snapshot = {"logOffset": LOG_PATH.stat().st_size}
    quit_result = subprocess.run(["/usr/bin/osascript", "-e",
                                  f'tell application id "{BUNDLE_ID}" to quit'],
                                 text=True, capture_output=True, timeout=20)
    # AppKit receives an affirmative delayed reply after Go and the native
    # backend finish cleanup. A cancelled Quit is a failure, including -128.
    if quit_result.returncode != 0:
        raise AssertionError(f"The standard Quit event failed: {quit_result.stderr}")
    deadline = time.monotonic() + 20
    while time.monotonic() < deadline:
        try:
            os.kill(pid, 0)
        except ProcessLookupError:
            break
        time.sleep(0.25)
    else:
        raise AssertionError("The standard Quit event did not stop the app core")
    assert "Quitting Smart Stage from the app menu" in appended_log(snapshot), "Quit did not reach the app termination handler"
    for port in (8787, 8788):
        with socket.socket() as probe:
            probe.settimeout(2)
            assert probe.connect_ex(("127.0.0.1", port)) != 0, f"Port {port} remained open after Quit"
    return {"quitEventReachedAppHandler": True, "quitAppleEventStoppedCore": True,
            "defaultPortsClosedAfterQuit": True,
            "quitAppleEventExitCode": quit_result.returncode}


def stop_core(pid):
    """Best-effort cleanup, including failures before normal Quit can be tested."""
    if not pid:
        return
    try:
        os.kill(pid, signal.SIGTERM)
    except ProcessLookupError:
        return
    for _ in range(60):
        try:
            os.kill(pid, 0)
        except ProcessLookupError:
            return
        time.sleep(0.25)
    try:
        os.kill(pid, signal.SIGKILL)
    except ProcessLookupError:
        pass
