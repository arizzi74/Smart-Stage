# Mac physical test

Use **v0.1.0-preview.14** (or a newer recorded version) on the Mac that will run the show.
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

Put two audio files (WAV and MP3), two videos (H.264/AAC and a silent H.264
video), and two images (PNG and JPEG) in a local folder, including a filename
with spaces and Unicode. Use
known-good show files, or the self-authored [media fixtures](../testdata/media/).
Those fixtures are only three seconds long; use longer audio and a video with a
recognizable soundtrack for crossfades, background looping, extended playback
and A/V drift observations. No encoder or media player needs
to be installed for this test.

## Download and start an isolated test show

Install the architecture-matched app with its icon, suppressing automatic launch
so that this test can use its own saved-show directory:

```sh
curl -fsSL https://raw.githubusercontent.com/arizzi74/Smart-Stage/main/install.sh | SMARTSTAGE_VERSION=v0.1.0-preview.14 SMARTSTAGE_NO_LAUNCH=1 sh
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

A direct [Apple Silicon app ZIP](https://github.com/arizzi74/Smart-Stage/releases/download/v0.1.0-preview.14/smartstage-darwin-arm64.app.zip)
is an alternative download. Use the app bundle for these
window and Finder-drop checks; the standalone executable opens a system-browser
Admin page. Browser-downloaded files may require
**Privacy & Security → Open Anyway**. Mac builds remain ad-hoc signed rather than
Developer ID signed/notarized; record any security or local-network prompt and
whether launch succeeds. No developer tools are required.

## First pass: routing, STOP and restart

Keep this baseline silent when stopped: in **Stage & sound**, select
**None — black** as the default background, leave background video audio, fades,
and the selected-music-button stop option off, then **Save stage & sound**.
The background and fade checks follow separately below.

1. Confirm **Admin opens automatically in Smart Stage's own window**, with no
   new system-browser tab or Terminal window. Drag the four audio/video files from Finder
   onto **Drop Finder files here** in Playlist. Verify all four cues appear,
   their displayed source paths point to the original folders, and those files
   stay in place without a copied media library. Give each cue a custom label
   and change their order. Native validation should finish without sound or
   video playback.
2. In Outputs, select the intended external audio device and second display,
   then Save outputs. Enable stage output: the selected screen should turn
   black while the Mac's control screen remains usable. Move the pointer over
   the stage: it should disappear there. Move back to Admin: it should reappear
   and remain usable. Disable the stage and confirm the pointer is visible on
   that display again, then re-enable it.
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

## Stage backgrounds, images and audio transitions

Use the same external audio output, extended display and phone controller. For
each check, listen to the physical output and observe the actual stage, as well
as the selected buttons and status in Admin/remote control.

1. Add the PNG and JPEG images to Playlist. In **Stage & sound**, choose one as
   **Default stage background**, save, and turn stage on. The image should fill
   the stage while preserving its proportions, with no desktop visible. Check
   the pointer disappears only over the stage and returns over Admin.
2. Play a longer music cue. Turn stage off and on from the remote, then repeat
   with Admin's stage controls. Music must continue without restarting, and its
   button must remain selected; only the display closes/reopens. Press the other
   image cue: it should appear while the same music continues and stays selected.
   Start a different music cue: the temporary image should clear and the current
   background should return.
3. In Admin, enable **Press the selected music button again to stop it**, then
   save. Start music, show the other image, and press the selected music button
   again. Only that music should stop; the image should remain. Press STOP:
   the temporary image should clear and the current background should return.
   Turn this option off and save after testing; a second music press should
   again restart the cue.
4. Mark the video with a soundtrack as a **Background button** in its playlist
   row. Enable **Play background video audio** in Stage & sound and save. Start
   music, then press the video background button. The background should change
   without replacing the music or its selection. Let the video run past its
   duration: it should loop. The **Current background** should change, while the
   saved **Default stage background** remains the image selected in step 1.
5. Press STOP. Foreground music and any temporary image should clear; the looping
   background video and its soundtrack should remain. Start a music cue: hear
   that music in place of the background soundtrack. STOP should return to the
   background soundtrack. With no foreground music, turn stage off: its video
   and background sound should stop; turn stage on and confirm both return.
   Disable background video audio, save, and confirm the background is silent.
6. Enable **Fade and crossfade audio**, leave **Transition duration** at its
   default **1 second**, and save. With stage off and all foreground sound
   stopped, start music: it should start immediately without a deliberate fade
   from silence. While it plays, press the other music cue: hear the old sound
   fade down as the new sound rises, over approximately one second. Press STOP
   with no background soundtrack active: hear a fade to silence.
7. Re-enable background video audio, save, and turn stage on. While the background
   soundtrack plays, start music, replace it with another music cue, then STOP.
   Each change should crossfade to the next sound, with STOP returning to the
   background soundtrack. Change the duration to **2 seconds**, save, and repeat
   to hear the longer transition. Record glitches, unintended full-volume
   overlap, gaps, or an old sound returning after the transition.
8. During a fade or crossfade, press Escape with Admin or the stage window
   focused. All sound must stop immediately and the stage must close, without
   waiting for the configured fade. Wait beyond the fade duration and confirm
   no old sound or image returns. Repeat while background video audio is playing.
9. In a playlist row, enable **Hide remote button**. The button should disappear
   from the phone/tablet but the cue should remain editable and playable in
   Admin. Hide the current background's button, turn stage on, and confirm that
   background still works. Unhide it and confirm the button returns in playlist
   order.
10. Quit and reopen the same isolated test show. Confirm background selection,
    soundtrack/fade/toggle settings and hidden/background flags are saved, but
    playback and stage stay off. Turn stage on: the saved default background
    should appear, rather than the session-only background chosen by a button.

These are physical acceptance checks to perform, not results. Record each as
pass, fail, or not tested. Restore the first-pass settings before repeating its
silence/blackout checks.

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
  focus. It should immediately silence foreground and background audio and
  close the stage window, even during a fade. Check the remote Stage on/off
  control while music plays: it must leave that music running. Ordinary STOP
  returns to the current background, or black when none is selected, and uses
  the configured audio transition.
  Separately check inaccessible media,
  alternate display arrangements/scaling and idle-sleep behavior.
- Run a two-hour rehearsal with repeated cue starts, replacements and STOP,
  including longer synchronized A/V material. Record resource usage in Activity
  Monitor at the start, 15, 30, 60 and 120 minutes, plus freezes, unintended
  playback, sound glitches and changes in A/V sync. Short clips alone cannot
  establish long-file A/V stability.

Initial observations are qualitative. Precise latency testing needs a recorded
method. The earlier targets of 200 ms from server receipt of STOP to physical
silence/blackout and 500 ms from a tap to the controller's stopped state apply to
the baseline with fades off and no background. A configured one- or two-second
fade intentionally changes ordinary STOP timing, and background audio can remain
audible after STOP. Record the fade setting and whether STOP or emergency Escape
was used; measure emergency silence separately on the documented wired-audio/LAN
setup. A stopwatch or tap-to-output recording does
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
Stage/background steps 1–10 (pass / fail / not tested, with observations):
Pointer hidden over stage / visible over Admin and after stage off:
Music continues across stage off/on / image selection keeps music selected:
Selected music second-press stop / image retained:
Background video loop / soundtrack replacement and return:
Start from silence / 1-second and 2-second crossfades / STOP fade:
Escape during transition / no late sound or image:
Hidden buttons / Admin access / saved defaults versus session background:
Output disconnect / default-device changes:
Phone sleep / reconnect / concurrent controls:
Two-hour rehearsal (duration and observations, or not tested):
Latency / A/V drift (method and measurements, or not measured):
Remaining equipment or scenarios:
```

Remote-control tokens and cookies are not needed in the report. Keep unavailable tests
pending; a pass on this Mac establishes results for its recorded hardware and
macOS version, not for untested Macs or Windows.
