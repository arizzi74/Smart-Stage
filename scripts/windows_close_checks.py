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
    user.GetMenu.argtypes = [wintypes.HWND]
    user.GetMenu.restype = wintypes.HMENU
    user.GetSubMenu.argtypes = [wintypes.HMENU, ctypes.c_int]
    user.GetSubMenu.restype = wintypes.HMENU
    user.GetMenuStringW.argtypes = [wintypes.HMENU, wintypes.UINT, wintypes.LPWSTR, ctypes.c_int, wintypes.UINT]
    user.GetMenuStringW.restype = ctypes.c_int
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
        arguments = [str(executable), "--admin-port", str(admin_port), "--port", str(remote_port),
                     "--bind", "127.0.0.1", "--no-auto-update", "--config-dir", str(config),
                     "--media-root", str(media)]
        with log_path.open("wb") as log:
            process = subprocess.Popen(
                arguments, stdin=subprocess.DEVNULL, stdout=log,
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

            def close_prompt(title="Quit Smart Stage?"):
                assert user.PostMessageW(original, 0x0112, 0xF060, 0), "Could not invoke native window Close"
                helpers.wait_for(lambda: windows("#32770", title, original), 8, "the shipped quit confirmation")
                assert user.IsWindowVisible(original) and not user.IsWindowEnabled(original), "Confirmation must own the visible Admin window"
                return windows("#32770", title, original)[0]

            def click(dialog, identifier):
                button = user.GetDlgItem(dialog, identifier)
                # BN_CLICKED to the real MessageBox/control; unlike BM_CLICK,
                # this works when an unrelated hosted shell window is active.
                assert button and user.PostMessageW(dialog, 0x0111, identifier, button), "Could not deliver the native confirmation button notification"

            def caption(hwnd):
                text = ctypes.create_unicode_buffer(512)
                user.GetWindowTextW(hwnd, text, len(text))
                return text.value

            def menu_label(identifier):
                text = ctypes.create_unicode_buffer(256)
                menu = user.GetSubMenu(user.GetMenu(original), 0)
                assert menu and user.GetMenuStringW(menu, identifier, text, len(text), 0), "Missing native app menu item"
                return text.value

            def language(mode):
                status, value = admin.request("PUT", "/api/language", {"mode": mode})
                assert status == 200 and value["effective"] == mode and value["mode"] == mode, (status, value)

            state = admin.get("/api/state")["state"]
            expected = {key: state[key] for key in ("state", "stopEpoch", "stageEnabled", "activeCueId")}
            language("it")
            helpers.wait_for(lambda: caption(original) == "Smart Stage — Amministrazione" and menu_label(101) == "Apri Admin",
                             8, "the running app's Italian title and menu")
            assert menu_label(103) == "Esci da Smart Stage\tCtrl+Q"
            assert user.PostMessageW(original, 0x0111, 102, 0), "Could not open the native media chooser"
            chooser_title = "Scegli i file per Smart Stage — resteranno nelle cartelle originali"
            helpers.wait_for(lambda: windows("#32770", chooser_title, original), 10, "the Italian native chooser")
            chooser = windows("#32770", chooser_title, original)[0]
            assert user.PostMessageW(chooser, 0x0111, 2, 0), "Could not cancel the native media chooser"
            helpers.wait_for(lambda: not user.IsWindow(chooser) and user.IsWindowEnabled(original), 8, "the native chooser to cancel")
            confirmation = close_prompt("Uscire da Smart Stage?")
            helpers.wait_for(lambda: caption(user.GetDlgItem(confirmation, 2)) == "Annulla", 5, "the Italian Cancel button")
            language("en")
            helpers.wait_for(lambda: caption(confirmation) == "Quit Smart Stage?" and
                             caption(user.GetDlgItem(confirmation, 2)) == "Cancel", 5, "the open confirmation to change to English")
            language("it")
            helpers.wait_for(lambda: caption(confirmation) == "Uscire da Smart Stage?" and
                             caption(user.GetDlgItem(confirmation, 2)) == "Annulla", 5, "the same confirmation to return to Italian")
            click(confirmation, 2)
            helpers.wait_for(lambda: not user.IsWindow(confirmation) and user.IsWindowEnabled(original), 8, "Italian Cancel to restore Admin")
            assert process.poll() is None, "Italian Cancel terminated the host"
            state = admin.get("/api/state")["state"]
            assert {key: state[key] for key in expected} == expected, "Language or chooser cancellation changed show state"
            report.update(italianPreferenceAppliedThroughAuthenticatedAPI=True,
                          italianNativeWindowAndMenusObserved=True, italianNativeChooserObserved=True,
                          italianCloseConfirmationAndCancelObserved=True,
                          nativeCloseConfirmationRelocalizedWhileOpen=True, languageChangePreservedHostState=True)

            # Retain Italian on disk, quit normally, then inspect the actual
            # shipped process after a restart with no language override.
            confirmation = close_prompt("Uscire da Smart Stage?")
            click(confirmation, 1)
            assert process.wait(timeout=30) == 0, "Italian confirmed close did not exit cleanly"
            process = subprocess.Popen(arguments, stdin=subprocess.DEVNULL, stdout=log,
                                       stderr=subprocess.STDOUT, env=os.environ.copy(), startupinfo=startup)
            admin = helpers.Admin(admin_port)
            helpers.wait_for(lambda: admin.get("/api/state")["state"], 40, "the restarted localized Admin host")
            helpers.wait_for(lambda: windows("SmartStageAdmin-View", "Smart Stage — Amministrazione"),
                             40, "the persisted Italian Admin window")
            original = windows("SmartStageAdmin-View", "Smart Stage — Amministrazione")[0]
            assert admin.get("/api/language")["mode"] == "it" and menu_label(101) == "Apri Admin"
            report["italianNativePreferenceSurvivedProcessRestart"] = True
            language("en")
            helpers.wait_for(lambda: caption(original) == "Smart Stage — Admin" and menu_label(101) == "Open Admin",
                             8, "the same running window to return to English")
            report["englishNativeLabelsRestoredAtRuntime"] = True

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
