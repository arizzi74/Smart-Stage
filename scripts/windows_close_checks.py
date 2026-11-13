"""Exercise close/Cancel/Confirm against the actual shipped Windows process."""
import ctypes
from ctypes import wintypes
import os
import socket
import subprocess
import time

from smoke_shutdown import stop_application


def _window_category(kind, title):
    """Report known UI identities without retaining arbitrary window captions."""
    known = {
        ("SmartStageAdmin-View", "Smart Stage — Admin"): "admin_en",
        ("SmartStageAdmin-View", "Smart Stage — Amministrazione"): "admin_it",
        ("#32770", "Quit Smart Stage?"): "quit_en",
        ("#32770", "Uscire da Smart Stage?"): "quit_it",
        ("#32770", "Choose media for Smart Stage — files stay in their original folders"): "media_chooser_en",
        ("#32770", "Scegli i file per Smart Stage — resteranno nelle cartelle originali"): "media_chooser_it",
    }
    return known.get((kind, title), "other_dialog" if kind == "#32770" else "other_window")


class _CloseObservations:
    """Bounded verifier observations, not a native chooser-idle acknowledgement."""

    def __init__(self, report, clock=time.monotonic):
        self.clock, self.started = clock, clock()
        self.data = report["closeObservations"] = {
            "scope": "Same-process window categories and existing verifier predicates; no native chooserActive readback",
            "sampleLimit": 96, "windowLimit": 16, "samples": [], "omittedSamples": 0,
        }

    def record(self, phase, **state):
        sample = {"phase": phase, "elapsedMilliseconds": round((self.clock() - self.started) * 1000), **state}
        self.data["last"] = sample
        if "windows" in state:
            self.data["lastWindowSearch"] = sample
        if len(self.data["samples"]) < self.data["sampleLimit"]:
            self.data["samples"].append(sample)
        else:
            self.data["omittedSamples"] += 1


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
    observations = _CloseObservations(report)
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

            def windows(kind, title, owner=None, observation=None):
                found = []
                sampled, total = [], 0
                category_counts, visible_category_counts = {}, {}

                @callback_type
                def collect(hwnd, _):
                    nonlocal total
                    pid = wintypes.DWORD()
                    user.GetWindowThreadProcessId(hwnd, ctypes.byref(pid))
                    if pid.value != process.pid:
                        return True
                    visible = bool(user.IsWindowVisible(hwnd))
                    if not visible and observation is None:
                        return True
                    class_name, caption = ctypes.create_unicode_buffer(128), ctypes.create_unicode_buffer(256)
                    user.GetClassNameW(hwnd, class_name, len(class_name))
                    user.GetWindowTextW(hwnd, caption, len(caption))
                    matches_owner = owner is None or user.GetWindow(hwnd, 4) == owner
                    matches = visible and class_name.value == kind and caption.value == title and matches_owner
                    if observation is not None:
                        total += 1
                        category = _window_category(class_name.value, caption.value)
                        category_counts[category] = category_counts.get(category, 0) + 1
                        if visible:
                            visible_category_counts[category] = visible_category_counts.get(category, 0) + 1
                        if len(sampled) < observations.data["windowLimit"]:
                            sampled.append({"category": category,
                                            "visible": visible, "enabled": bool(user.IsWindowEnabled(hwnd)),
                                            "minimized": bool(user.IsIconic(hwnd)), "matchesExpectedOwner": matches_owner,
                                            "matchesExpectedDialog": matches})
                    if matches:
                        found.append(hwnd)
                    return True

                assert user.EnumWindows(collect, 0), "Could not enumerate shipped app windows"
                if observation is not None:
                    observations.record(observation, processAlive=process.poll() is None,
                                        ownerExists=bool(user.IsWindow(owner)), ownerVisible=bool(user.IsWindowVisible(owner)),
                                        ownerEnabled=bool(user.IsWindowEnabled(owner)), expectedCategory=_window_category(kind, title),
                                        matchingWindows=len(found), ownWindowsObserved=total, windows=sampled,
                                        windowCategoryCounts=category_counts, visibleWindowCategoryCounts=visible_category_counts)
                assert len(found) <= 1, f"Unexpected duplicate shipped app window: {title}"
                return found

            helpers.wait_for(lambda: windows("SmartStageAdmin-View", "Smart Stage — Admin"), 40, "the shipped native Admin window")
            original = windows("SmartStageAdmin-View", "Smart Stage — Admin")[0]

            def close_prompt(title="Quit Smart Stage?"):
                dispatched = bool(user.PostMessageW(original, 0x0112, 0xF060, 0))
                observations.record("close-dispatched", dispatchAccepted=dispatched, processAlive=process.poll() is None,
                                    ownerExists=bool(user.IsWindow(original)), ownerEnabled=bool(user.IsWindowEnabled(original)),
                                    expectedCategory=_window_category("#32770", title))
                assert dispatched, "Could not invoke native window Close"
                try:
                    helpers.wait_for(lambda: windows("#32770", title, original, "quit-confirmation-search"), 8, "the shipped quit confirmation")
                except Exception:
                    observations.record("quit-confirmation-failed", processAlive=process.poll() is None,
                                        ownerExists=bool(user.IsWindow(original)), ownerEnabled=bool(user.IsWindowEnabled(original)),
                                        expectedCategory=_window_category("#32770", title))
                    raise
                observations.record("quit-confirmation-found", processAlive=process.poll() is None,
                                    ownerVisible=bool(user.IsWindowVisible(original)), ownerEnabled=bool(user.IsWindowEnabled(original)),
                                    expectedCategory=_window_category("#32770", title))
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
            expected_labels = {"title": "Smart Stage — Amministrazione", "open": "Apri Admin",
                               "quit": "Esci da Smart Stage\tCtrl+Q"}
            readbacks = report["italianMenuRefreshReadbacks"] = []

            def italian_menu_ready():
                labels = {"title": caption(original), "open": menu_label(101), "quit": menu_label(103)}
                if len(readbacks) < 32:
                    readbacks.append(labels)
                report["lastItalianMenuLabels"] = labels
                return labels == expected_labels

            # The language HTTP response posts work to the native UI thread.
            # Caption and Open update before Quit; wait for every label that
            # this probe asserts instead of treating that prefix as complete.
            try:
                helpers.wait_for(italian_menu_ready, 8, "the running app's complete Italian title and menu")
            except AssertionError as error:
                raise AssertionError(f"{error}; last native labels: {report.get('lastItalianMenuLabels')}") from error
            assert menu_label(103) == "Esci da Smart Stage\tCtrl+Q"
            report["italianMenuRefreshCompletedBeforeQuitAssertion"] = True
            assert user.PostMessageW(original, 0x0111, 102, 0), "Could not open the native media chooser"
            chooser_title = "Scegli i file per Smart Stage — resteranno nelle cartelle originali"
            helpers.wait_for(lambda: windows("#32770", chooser_title, original), 10, "the Italian native chooser")
            chooser = windows("#32770", chooser_title, original)[0]
            dispatched = bool(user.PostMessageW(chooser, 0x0111, 2, 0))
            observations.record("italian-chooser-cancel-dispatched", dispatchAccepted=dispatched,
                                chooserExists=bool(user.IsWindow(chooser)), ownerEnabled=bool(user.IsWindowEnabled(original)))
            assert dispatched, "Could not cancel the native media chooser"

            def chooser_cancelled():
                exists, enabled = bool(user.IsWindow(chooser)), bool(user.IsWindowEnabled(original))
                ready = not exists and enabled
                observations.record("italian-chooser-cancel-search", chooserExists=exists, ownerEnabled=enabled,
                                    processAlive=process.poll() is None, existingCancelPredicateReady=ready)
                return ready

            try:
                helpers.wait_for(chooser_cancelled, 8, "the native chooser to cancel")
            except Exception:
                observations.record("italian-chooser-cancel-failed", processAlive=process.poll() is None)
                raise
            observations.record("italian-chooser-cancel-ready", processAlive=process.poll() is None)
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
        if process is not None:
            stop_application(process, admin_port)
        if log_path.exists():
            report["hostLog"] = log_path.read_text(encoding="utf-8", errors="replace")[-18000:]
