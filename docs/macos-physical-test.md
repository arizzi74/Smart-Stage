# Mac physical test

Use the published **v0.1.0-preview.13** on the Mac that will run the show.
The available physical test machine is an **Apple Silicon Mac**; its macOS
version is not yet recorded. An external audio output and a second
monitor/projector are available. Use the **darwin/arm64** release. Windows physical checks remain pending. This is a procedure and result
template, not a record of passed tests.
See the [acceptance audit](acceptance-audit.md) for the full remaining scope.

## Equipment and setup

Record the Mac chip (Apple Silicon or Intel), macOS version, audio output,
monitor/projector connection and phone/tablet browser. Also note whether the
Mac already has development tools or extra media software; testing that Mac
does not establish clean-machine acceptance if those are installed.

For the routing test, connect external speakers or headphones through an
available wired/USB/HDMI output and connect a second monitor/projector. Use an
extended desktop with mirroring off. Keep the Mac's system-default audio output
different from the output selected in Smart Stage, so the test can detect
unintended fallback. Start at a comfortable low volume.

A Mac with only built-in outputs can still test playback, phone control, STOP
and saved-show restart. Record non-default audio and second-display checks as
**not tested** when that equipment is unavailable. Only-display stage output
requires the application's explicit acknowledgement; keep Command available on
the phone before enabling it.

Put two audio files (WAV and MP3) and two videos (H.264/AAC and a silent H.264
video) in a local folder, including a filename with spaces and Unicode. Use
known-good show files, or the self-authored [media fixtures](../testdata/media/).
Those fixtures are only three seconds long; longer show files are needed for
extended playback and A/V drift observations. No encoder or media player needs
to be installed for this test.

## Download and start an isolated test show

Install the architecture-matched app with its icon, suppressing automatic launch
so that this test can use its own saved-show directory:

```sh
curl -fsSL https://raw.githubusercontent.com/arizzi74/Smart-Stage/main/install.sh | SMARTSTAGE_VERSION=v0.1.0-preview.13 SMARTSTAGE_NO_LAUNCH=1 sh
```

The installer verifies the app ZIP, installs into `~/Applications`, and removes
quarantine only from Smart Stage. Its per-app firewall rule can require an
administrator authorization prompt. It requires no additional runtime. For this
isolated physical test, record the version and launch the actual app bundle with
its own saved-show directory:

```sh
"$HOME/Applications/Smart Stage.app/Contents/MacOS/smartstage" --version
open -n -a "$HOME/Applications/Smart Stage.app" --args --config-dir "$HOME/Library/Application Support/SmartStage-Physical-Test" --no-auto-update
```

Record the complete version line. You can close Terminal after launch; the app
runs independently and should show its icon in the Dock. The separate test
configuration preserves the normal Smart Stage show's saved data. Stop any other
Smart Stage instance first so local Admin port 8787 and remote-control port 8788
are available, including before repeating the launch command. The
`--no-auto-update` flag keeps this test on the recorded version. Admin opens
automatically in **Smart Stage's own window**, using
`http://127.0.0.1:8787/admin`; it remains accessible only on the Mac itself.

Closing the Admin window or pressing Command-W hides it and keeps the app
running. Click the Dock icon or choose **Open Admin** from Smart Stage's menu bar
control to restore the same window. **Quit Smart Stage** in Admin, the app menu,
or the Dock stops playback and exits. After quitting, repeat the `open` command
above to return to this isolated show. Opening the app directly in Finder after
quitting instead starts its normal configuration.

A direct [Apple Silicon app ZIP](https://github.com/arizzi74/Smart-Stage/releases/download/v0.1.0-preview.13/smartstage-darwin-arm64.app.zip)
is an alternative download. Use the app bundle for these
window and Finder-drop checks; the standalone executable opens a system-browser
Admin page. Browser-downloaded files may require
**Privacy & Security → Open Anyway**. Mac builds remain ad-hoc signed rather than
Developer ID signed/notarized; record any security or local-network prompt and
whether launch succeeds. No developer tools are required.

## First pass: routing, STOP and restart

1. Confirm **Admin opens automatically in Smart Stage's own window**, with no
   new system-browser tab or Terminal window. Drag the four files from Finder
   onto **Drop Finder files here** in Playlist. Verify all four cues appear,
   their displayed source paths point to the original folders, and those files
   stay in place without a copied media library. Give each cue a custom label
   and change their order. Native validation should finish without sound or
   video playback.
2. In Outputs, select the intended external audio device and second display,
   then Save outputs. Enable stage output: the selected screen should turn
   black while the Mac's control screen remains usable.
3. On a phone/tablet connected to the same LAN, scan the QR code displayed in
   Admin using the camera, or open the displayed **Remote control URL**. The
   numeric token is already included; no pairing key should need typing. Check
   that four labelled buttons appear in the saved order and STOP remains visible
   without scrolling. Confirm that replacing `/command` with `/admin` on that
   remote address does not expose Admin.
4. Play each audio cue. Confirm sound comes only from the selected physical
   output. Press STOP during playback and listen for silence. The enabled stage
   should remain black.
5. Play the video with a soundtrack. Confirm moving video appears only on the
   selected display, preserves its aspect ratio, and its sound uses the selected
   audio output. Press STOP: sound must stop and the stage must remain black,
   without revealing the desktop or retaining the final video frame.
6. Play the silent video and repeat STOP. Start one cue, replace it with another,
   then STOP; check for overlapping sound, late starts or a returning old frame.
   Pressing a playing cue again should restart that cue.
7. Press STOP immediately after PLAY. If loading is observable, also stop during
   loading and wait longer than the usual startup time: nothing should start
   later. If loading finishes too quickly to observe, record that particular
   loading scenario as **not tested**.
8. Allow audio and video cues to end naturally. Nothing should advance to the
   next cue; sound should be silent and an enabled stage should remain black.
9. Start a cue, then use **Quit Smart Stage** in Admin. Confirm sound stops, the
   stage and Admin windows close, and the app exits. Repeat the same `open`
   command above. Verify the labels, order and output choices return, but
   playback stays stopped and the stage stays disabled. Scan the new QR code;
   the previous launch's remote URL should no longer grant control.

For the first report, record pass/fail/not tested for each step and describe any
unexpected sound or visible frame. Observing a stopped label alone does not
establish physical silence or blackout.

## Follow-up checks after the first pass

- While a longer cue plays, close the Admin window with its close button, then
  click the Dock icon. Playback should continue and the same Admin window should
  return with the current show state, without opening a browser tab or duplicate
  window. Repeat with Command-W while Admin has focus and with **Open Admin**
  from the menu bar control. Use STOP after checking continued playback.
- Add another file using **Choose files on this Mac…**, another by dropping it
  onto the Dock icon, and another by dragging a row from **Host files** into
  Playlist. Each should retain its original source path and leave playback
  stopped. Confirm native drop feedback appears, and try a filename containing
  spaces and Unicode. Check Command-A/C/V in a cue-label field and **Copy link**
  for sharing the remote URL. Record a cancelled chooser separately from a
  failed import; cancellation should add nothing.
- During playback, disconnect the selected external audio device and separately
  the second display. Playback should stop and report the loss, without moving
  sound to another speaker or video to the control screen. Reconnection must
  not resume playback; explicitly select/save outputs and issue a new PLAY.
- Change the Mac's system-default audio output while Smart Stage uses a specific
  connected output. Confirm sound stays on the selected device.
- While playback runs, browse files and Validate all cues from Admin, then STOP
  from the phone. Check that STOP remains usable. Try another controller, phone
  sleep and a Wi-Fi disconnect/reconnect. Playback should continue through a
  controller disconnection; reconnect must not replay old PLAY
  commands. An unacknowledged STOP must not be displayed as confirmed.
- Exercise Escape while the stage window or connected Admin page has keyboard
  focus. It should stop playback and close the stage window. Check the remote
  Stage on/off control as well; STOP should still retain an enabled black stage.
  Separately check inaccessible media,
  alternate display arrangements/scaling and idle-sleep behavior.
- Run a two-hour rehearsal with repeated cue starts, replacements and STOP,
  including longer synchronized A/V material. Record resource usage in Activity
  Monitor at the start, 15, 30, 60 and 120 minutes, plus freezes, unintended
  playback, sound glitches and changes in A/V sync. Short clips alone cannot
  establish long-file A/V stability.

Initial observations are qualitative. Precise latency testing needs a recorded
method: the specification targets 200 ms from server receipt of STOP to physical
silence/blackout, and 500 ms from a tap to the controller's stopped state on a
documented wired-audio/LAN setup. A stopwatch or tap-to-output recording does
not isolate the server-receipt interval. Leave these measurements pending until
they are actually measured; Bluetooth buffering is a separate condition.

## Result template

```text
Date:
Mac chip / macOS:
Smart Stage version / commit:
Existing development tools or extra media software:
Selected audio output / connection:
Different system-default audio output:
Stage display / connection / extended or mirrored:
Phone or tablet / OS / browser / LAN connection:
Media tested:
Launch and permission prompts:
Dedicated Admin window / Dock icon / Terminal closed:
Finder, Dock, chooser and Host files additions / original paths preserved:
Close hides / same-window Dock and menu reopening / Quit exits:
Copy link / Command-A/C/V:
First-pass steps 1–9 (pass / fail / not tested, with observations):
Output disconnect / default-device changes:
Phone sleep / reconnect / concurrent controls:
Two-hour rehearsal (duration and observations, or not tested):
Latency / A/V drift (method and measurements, or not measured):
Remaining equipment or scenarios:
```

Remote-control tokens and cookies are not needed in the report. Keep unavailable tests
pending; a pass on this Mac establishes results for its recorded hardware and
macOS version, not for untested Macs or Windows.
