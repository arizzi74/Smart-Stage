"""Exercise close/Cancel/Confirm against the actual shipped Windows process."""
import ctypes
from ctypes import wintypes
import os
import socket
import subprocess
import time


def shipped_close_confirmation_checks(executable, work, config, media, helpers, report):
    user = ctypes.WinDLL("user32", use_last_error=True)
    callback_type = ctypes.WINFUNCTYPE(wintypes.BOOL, wintypes.HWND, wintypes.LPARAM)
    user.EnumWindows.argtypes = [callback_type, wintypes.LPARAM]
    user.EnumWindows.restype = wintypes.BOOL
    user.GetWindowThreadProcessId.argtypes = [wintypes.HWND, ctypes.POINTER(wintypes.DWORD)]
    user.GetWindowThreadProcessId.restype = wintypes.DWORD
    user.GetClassNameW.argtypes = [wintypes.HWND, wintypes.LPWSTR, ctypes.c_int]
    user.GetWindowTextW.argtypes = [wintypes.HWND, wintypes.LPWSTR, ctypes.c_int]
    user.GetWindow.argtypes = [wintypes.HWND, wintypes.UINT]
    user.GetWindow.restype = wintypes.HWND
    user.GetDlgItem.argtypes = [wintypes.HWND, ctypes.c_int]
    user.GetDlgItem.restype = wintypes.HWND
    user.PostMessageW.argtypes = [wintypes.HWND, wintypes.UINT, wintypes.WPARAM, wintypes.LPARAM]
    user.PostMessageW.restype = wintypes.BOOL
    for name in ("IsWindow", "IsWindowVisible", "IsWindowEnabled", "IsIconic"):
        function = getattr(user, name)
        function.argtypes = [wintypes.HWND]
        function.restype = wintypes.BOOL

    process = None
    log_path = work / "shipped-close.log"
    try:
        admin_port, remote_port = helpers.unused_port(), helpers.unused_port()
        while remote_port == admin_port:
            remote_port = helpers.unused_port()
        startup = subprocess.STARTUPINFO()
        startup.dwFlags |= subprocess.STARTF_USESHOWWINDOW
        startup.wShowWindow = subprocess.SW_HIDE
        with log_path.open("wb") as log:
            process = subprocess.Popen(
                [str(executable), "--admin-port", str(admin_port), "--port", str(remote_port),
                 "--bind", "127.0.0.1", "--no-auto-update", "--config-dir", str(config),
                 "--media-root", str(media)], stdin=subprocess.DEVNULL, stdout=log,
                stderr=subprocess.STDOUT, env=os.environ.copy(), startupinfo=startup)
            admin = helpers.Admin(admin_port)
            helpers.wait_for(lambda: admin.get("/api/state")["state"], 40, "the shipped close-test Admin host")

            def windows(kind, title, owner=None):
                found = []

                @callback_type
                def collect(hwnd, _):
                    pid = wintypes.DWORD()
                    user.GetWindowThreadProcessId(hwnd, ctypes.byref(pid))
                    if pid.value != process.pid or not user.IsWindowVisible(hwnd):
                        return True
                    class_name, caption = ctypes.create_unicode_buffer(128), ctypes.create_unicode_buffer(256)
                    user.GetClassNameW(hwnd, class_name, len(class_name))
                    user.GetWindowTextW(hwnd, caption, len(caption))
                    if class_name.value == kind and caption.value == title and (owner is None or user.GetWindow(hwnd, 4) == owner):
                        found.append(hwnd)
                    return True

                assert user.EnumWindows(collect, 0), "Could not enumerate shipped app windows"
                assert len(found) <= 1, f"Unexpected duplicate shipped app window: {title}"
                return found

            helpers.wait_for(lambda: windows("SmartStageAdmin-View", "Smart Stage — Admin"), 40, "the shipped native Admin window")
            original = windows("SmartStageAdmin-View", "Smart Stage — Admin")[0]

            def close_prompt():
                assert user.PostMessageW(original, 0x0112, 0xF060, 0), "Could not invoke native window Close"
                helpers.wait_for(lambda: windows("#32770", "Quit Smart Stage?", original), 8, "the shipped quit confirmation")
                assert user.IsWindowVisible(original) and not user.IsWindowEnabled(original), "Confirmation must own the visible Admin window"
                return windows("#32770", "Quit Smart Stage?", original)[0]

            def click(dialog, identifier):
                button = user.GetDlgItem(dialog, identifier)
                # BN_CLICKED to the real MessageBox/control; unlike BM_CLICK,
                # this works when an unrelated hosted shell window is active.
                assert button and user.PostMessageW(dialog, 0x0111, identifier, button), "Could not deliver the native confirmation button notification"

            state = admin.get("/api/state")["state"]
            expected = {key: state[key] for key in ("state", "stopEpoch", "stageEnabled", "activeCueId")}
            confirmation = close_prompt()
            assert process.poll() is None, "Close terminated the app before confirmation"
            click(confirmation, 2)  # IDCANCEL
            helpers.wait_for(lambda: not user.IsWindow(confirmation) and user.IsWindowEnabled(original), 8, "Cancel to restore Admin")
            assert process.poll() is None and user.IsWindowVisible(original) and not user.IsIconic(original), "Cancel did not retain the running visible app"
            state = admin.get("/api/state")["state"]
            assert {key: state[key] for key in expected} == expected, "Cancel changed playback or stage state"
            report.update(cancelKeptProcessAndAdminRunning=True, cancelPreservedHostPlaybackState=True)

            confirmation = close_prompt()
            started = time.monotonic()
            click(confirmation, 1)  # IDOK
            assert process.wait(timeout=30) == 0, "Confirmed close did not exit the shipped app cleanly"
            assert not user.IsWindow(original) and not user.IsWindow(confirmation), "Confirmed close left native windows open"
            with socket.socket() as connection:
                connection.settimeout(2)
                assert connection.connect_ex(("127.0.0.1", admin_port)) != 0, "Confirmed close left the Admin listener open"
            report.update(status="passed", method="Actual shipped process; native SC_CLOSE and owned MessageBox button notifications (not physical mouse input)",
                          confirmExitedProcessCleanly=True, confirmClosedAdminListener=True, confirmDestroyedNativeWindows=True,
                          shutdownMilliseconds=round((time.monotonic() - started) * 1000))
    except Exception as error:
        report.update(status="failed", error=str(error))
        raise
    finally:
        if process is not None and process.poll() is None:
            process.terminate()
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=5)
        if log_path.exists():
            report["hostLog"] = log_path.read_text(encoding="utf-8", errors="replace")[-18000:]
