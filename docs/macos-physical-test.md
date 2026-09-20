# Mac physical test

Use the published **v0.1.0-preview.5** on the Mac that will run the show.
The available physical test machine is an **Apple Silicon Mac**; its macOS
version and attached outputs are not yet recorded. Use the **darwin/arm64**
release. Windows physical checks remain pending. This is a procedure and result
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

## Install and start an isolated test show

In Mac Terminal:

```sh
curl -fsSL https://raw.githubusercontent.com/arizzi74/Smart-Stage/main/install.sh | SMARTSTAGE_VERSION=v0.1.0-preview.5 sh
"$HOME/.local/bin/smartstage" --version
"$HOME/.local/bin/smartstage" --config-dir "$HOME/Library/Application Support/SmartStage-Physical-Test"
```

These commands use the installer's default location. If you previously set
`SMARTSTAGE_INSTALL_DIR`, use the executable path printed by the installer.
Record the version line; this procedure expects preview 5 and commit
`9e72b4cc614f`. Keep Terminal running. The separate test configuration preserves
the normal Smart Stage show's saved data. Stop any other Smart Stage instance
first so port 8787 is available.

The optional Finder app starts the normal configuration. Use the Terminal
command above for this isolated test. Mac builds are ad-hoc signed rather than
Developer ID signed/notarized; record any per-app security or local-network
prompt and whether launch succeeds. No developer tools are required.

## First pass: routing, STOP and restart

1. Open the **Admin URL printed by this Mac** and pair with the Admin key.
   Browse the Mac's files, add the four cues, give each a custom label and change
   their order. Native validation should finish without sound or video playback.
2. In Outputs, select the intended external audio device and second display,
   then Save outputs. Enable stage output: the selected screen should turn
   black while the Mac's control screen remains usable.
3. On a phone/tablet connected to the same LAN, open the printed **Command URL**
   and pair using the separate Command key. Check that four labelled buttons
   appear in the saved order and STOP remains visible without scrolling.
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
9. Exit with Ctrl+C in Terminal, then repeat the same start command. Verify the
   labels, order and output choices return, but playback stays stopped and the
   stage stays disabled. Pair again with the newly printed keys.

For the first report, record pass/fail/not tested for each step and describe any
unexpected sound or visible frame. Observing a stopped label alone does not
establish physical silence or blackout.

## Follow-up checks after the first pass

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
- Exercise Escape while the stage window has keyboard focus. It should stop
  and blacken the stage without closing it. Separately check inaccessible media,
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
First-pass steps 1–9 (pass / fail / not tested, with observations):
Output disconnect / default-device changes:
Phone sleep / reconnect / concurrent controls:
Two-hour rehearsal (duration and observations, or not tested):
Latency / A/V drift (method and measurements, or not measured):
Remaining equipment or scenarios:
```

Pairing keys and cookies are not needed in the report. Keep unavailable tests
pending; a pass on this Mac establishes results for its recorded hardware and
macOS version, not for untested Macs or Windows.
