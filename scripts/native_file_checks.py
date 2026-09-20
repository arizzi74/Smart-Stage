"""Exercise real LaunchServices file-open delivery to the packaged Mac app.

This covers the Apple event used by Finder/Dock drops and Open With, including
delivery when the app is closed. It does not claim a physical mouse gesture.
"""
import importlib.util
import contextlib
import json
from pathlib import Path
import subprocess
import tempfile
import wave

from macos_app_checks import appended_log, launch_snapshot, terminal_pids


def file_open_checks(bundle, evidence_path):
    spec = importlib.util.spec_from_file_location("smartstage_update_checks", Path(__file__).with_name("verify-auto-update.py"))
    helpers = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(helpers)
    report = {"status": "incomplete", "method": "LaunchServices file-open events; no physical Finder/Dock drag assertion"}
    snapshot = launch_snapshot()
    pid = None
    core = bundle / "Contents/MacOS/smartstage"
    try:
        with contextlib.ExitStack() as stack:
            temporary = stack.enter_context(tempfile.TemporaryDirectory(prefix="smartstage-original-files-"))
            # Close processes before deleting the test's saved show/media.
            def cleanup_processes():
                for owned in helpers.owned_pids(bundle):
                    helpers.stop_owned(owned, bundle)
            stack.callback(cleanup_processes)
            work = Path(temporary).resolve()
            config, media = work / "Saved show", work / "Original media – café"
            expected_show = helpers.seed_show(config, media)
            originals = [Path(expected_show["cues"][0]["path"])]
            for directory in ("Opening's media", "Finale", "Launch from closed"):
                parent = media / directory
                parent.mkdir()
                path = parent / "Original – 演出.wav"
                with wave.open(str(path), "wb") as audio:
                    audio.setnchannels(1)
                    audio.setsampwidth(2)
                    audio.setframerate(8000)
                    audio.writeframes(b"\x00\x00" * 8000)
                originals.append(path)
            hashes = {path: helpers.digest(path) for path in originals}
            admin_port, remote_port = helpers.unused_port(), helpers.unused_port()
            while remote_port == admin_port:
                remote_port = helpers.unused_port()
            arguments = ["--admin-port", str(admin_port), "--port", str(remote_port), "--bind", "127.0.0.1",
                         "--config-dir", str(config), "--media-root", str(media), "--no-browser", "--no-auto-update"]
            admin = helpers.Admin(admin_port)

            def launch(files=()):
                subprocess.run(["/usr/bin/open", "-n", "-a", str(bundle), *map(str, files), "--args", *arguments],
                               check=True, capture_output=True, text=True, timeout=15)
                return helpers.wait_for(lambda: helpers.listener_pid(admin_port, core), 30, "the Finder-launched core")

            def playlist_count(count):
                value = admin.get("/api/playlist")
                return value if len(value["cues"]) == count else None

            def check_originals(show, count):
                assert len(show["cues"]) == count, show
                assert show["cues"][0]["id"] == expected_show["cues"][0]["id"], "Existing cue identity changed"
                assert show["cues"][0]["label"] == expected_show["cues"][0]["label"], "Existing cue label changed"
                assert len({cue["id"] for cue in show["cues"]}) == count, "Imported cue IDs collide"
                for cue, original in zip(show["cues"], originals):
                    assert Path(cue["path"]).samefile(original), (cue["path"], str(original))
                    assert helpers.digest(original) == hashes[original], "Import changed original media bytes"
                assert not list(config.rglob("*.wav")), "Native import copied media into app configuration"
                state = admin.get("/api/state")["state"]
                assert state["state"] == "stopped" and not state["stageEnabled"] and not state["activeCueId"], state

            def quit_app(process):
                # Target this exact bundle; no Accessibility or menu scripting.
                escaped = str(bundle).replace("\\", "\\\\").replace('"', '\\"')
                result = subprocess.run(["/usr/bin/osascript", "-e", f'tell application "{escaped}" to quit'],
                                        capture_output=True, text=True, timeout=20)
                assert result.returncode == 0, result.stderr
                helpers.wait_for(lambda: not helpers.process_alive(process), 15, "normal Quit to close the app")

            pid = launch()
            initial = helpers.wait_for(lambda: playlist_count(1), 20, "the saved show")
            check_originals(initial, 1)
            subprocess.run(["/usr/bin/open", "-a", str(bundle), *map(str, originals[1:3])],
                           check=True, capture_output=True, text=True, timeout=15)
            appended = helpers.wait_for(lambda: playlist_count(3), 20, "files opened into the running app")
            check_originals(appended, 3)
            assert helpers.listener_pid(admin_port, core) == pid, "File delivery restarted the running app"
            report.update(runningAppAcceptedNativeFileOpen=True, runningAppKeptSamePID=True,
                          duplicateBasenamesAndUnicodePathsAccepted=True, originalsReferencedWithoutCopying=True,
                          existingCuePreserved=True, importDidNotStartPlayback=True)

            # An invalid request produces a modeless error. Standard app Quit
            # must still complete even when the operator leaves it unattended.
            invalid = work / "Outside the allowed media folder.wav"
            invalid.write_bytes(originals[0].read_bytes())
            error_snapshot = launch_snapshot()
            subprocess.run(["/usr/bin/open", "-a", str(bundle), str(invalid)],
                           check=True, capture_output=True, text=True, timeout=15)
            helpers.wait_for(lambda: "Native media import failed:" in appended_log(error_snapshot),
                             15, "the rejected file-open error")
            assert len(admin.get("/api/playlist")["cues"]) == 3, "Rejected input changed the show"
            quit_app(pid)
            pid = None
            report["quitCompletedWithUnattendedFileError"] = True

            # The native queue is initialized before finishLaunching and retains
            # this event until the Go service can append and save it.
            admin = helpers.Admin(admin_port)
            pid = launch([originals[3]])
            restored = helpers.wait_for(lambda: playlist_count(4), 25, "the startup file-open event")
            check_originals(restored, 4)
            assert [cue["id"] for cue in restored["cues"][:3]] == [cue["id"] for cue in appended["cues"]], "Restart changed saved cue identities"
            report.update(closedAppAcceptedStartupFileOpen=True, savedImportsSurvivedRestart=True)
            quit_app(pid)
            pid = None
            assert not (terminal_pids() - snapshot["terminalPIDs"]), "Native file-open delivery opened Terminal"
            report.update(fileOpenDidNotStartTerminal=True, status="passed")
    except Exception as error:
        report.update(status="failed", error=str(error))
        raise
    finally:
        # Stop only a process whose executable belongs to this extracted bundle.
        if pid:
            helpers.stop_owned(pid, bundle)
        for owned in helpers.owned_pids(bundle):
            helpers.stop_owned(owned, bundle)
        try:
            report["appLogTail"] = appended_log(snapshot)[-12000:]
        except OSError:
            pass
        evidence_path.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    return report
