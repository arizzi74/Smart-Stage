Smart Stage preview for macOS Apple Silicon/Intel and Windows ARM64/AMD64.

Preview 14 adds independent stage presentation and sound:

- Hide the pointer over the fullscreen stage. Stage on/off works while music or
  video plays, without restarting the foreground timeline.
- Select a saved image or looping video background in **Stage and sound**.
  Background cue buttons can change it for the current session. Enable background
  video audio to hear it when no foreground sound is playing.
- Add image cues to the playlist. Showing an image keeps the music playing and
  its button selected. An optional setting makes a second press of the selected
  music button stop only that music.
- Enable audio fades/crossfades and choose their duration (default one second).
  New audio starts immediately from silence; otherwise it crossfades from the
  outgoing sound. STOP returns to background audio or fades to silence.
- Hide individual remote buttons without removing their cues from Admin.
- STOP returns to the current background; Escape immediately silences everything
  and closes the stage. Quit still stops playback and exits.

The Mac app retains its dedicated Admin window, Finder drag-and-drop of original
files, Dock icon and Terminal-free launch. Standalone Mac and Windows executables
use the system browser. Saved backgrounds and options persist; background buttons
change the current session without overwriting the saved default. Media is fitted
inside the stage while preserving its proportions.

Automatic updates continue to install on launch, before playback. Existing
preview 10 and later installations can update on their next launch. Saved shows and output
preferences are preserved; no playback starts automatically. The Mac updater
refreshes Smart Stage’s firewall rule and may request an administrator password
because current builds are ad-hoc signed. No manual Firewall Settings change
should be needed. Restart creates a new remote link/QR code.

On Mac, install the app with its icon using:

```sh
curl -fsSL https://raw.githubusercontent.com/arizzi74/Smart-Stage/main/install.sh | sh
```

The installer chooses Apple Silicon or Intel, verifies the download, installs in
`~/Applications` and removes quarantine only from Smart Stage. Quit the old app
before updating. It also allows Smart Stage's incoming connections through the
macOS firewall, requesting administrator authorization when required. Other
firewall rules stay intact. The app opens Admin automatically and runs independently of
Terminal. The Smart Stage icon appears in the Dock while it runs.
Use **Quit Smart Stage** in Admin or right-click the Dock icon and choose
**Quit** to close it. Clicking the icon opens or reuses Admin. The standard application menu and the additional menu bar control
also provide Quit.
Logs are saved in `~/Library/Logs/Smart Stage/smartstage.log`.

Direct ZIPs also remain available. Each primary ZIP contains just one application
binary; no additional runtime is required.

| Computer | Download |
| --- | --- |
| Mac — Apple Silicon (ARM64) | [smartstage-darwin-arm64.zip](https://github.com/arizzi74/Smart-Stage/releases/download/v0.1.0-preview.14/smartstage-darwin-arm64.zip) |
| Mac — Intel (AMD64) | [smartstage-darwin-amd64.zip](https://github.com/arizzi74/Smart-Stage/releases/download/v0.1.0-preview.14/smartstage-darwin-amd64.zip) |
| Windows — ARM64 | [smartstage-windows-arm64.zip](https://github.com/arizzi74/Smart-Stage/releases/download/v0.1.0-preview.14/smartstage-windows-arm64.zip) |
| Windows — Intel / AMD (AMD64) | [smartstage-windows-amd64.zip](https://github.com/arizzi74/Smart-Stage/releases/download/v0.1.0-preview.14/smartstage-windows-amd64.zip) |

The Mac app displays Admin in its dedicated window; standalone Mac and Windows
launches use the system browser. Admin listens only on **127.0.0.1:8787**. Its Remote control area displays the phone/tablet
URL and a QR code. Scan the code on the same network to connect; the URL contains
a short random numeric token and no separate pairing-key entry is required.
Remote controls listen on port **8788**, with no access to administration APIs.
The token changes at every launch, so scan the current QR code after restarting.

Windows executables retain the embedded Smart Stage icon. The optional Mac
`smartstage-darwin-<arch>.app.zip` contains a Finder bundle with the same core
executable, launcher and icon. Opening it starts Smart Stage without Terminal and
opens its dedicated Admin window. The primary Mac ZIP contains only the
standalone executable.

Native AVFoundation/Media Foundation playback, manual cue playlists, output
selection and atomic local persistence remain available.
Playback retains preview 4's macOS rendering-layer cleanup and preview 3's
Windows per-stream STOP muting. Prior native resource and physical-test limits
remain documented in
[verification](https://github.com/arizzi74/Smart-Stage/blob/main/docs/release-verification.md).

Each ZIP has an individual SHA-256 checksum. Native build/import/media/icon
checks and extracted-executable startup checks gate publication. Published ZIPs
are then downloaded, checksum-verified, extracted and started on all four
native targets; browser checks exercise the real Admin and remote interfaces.

This is a **preview, not an accepted production release**. Hosted tests cannot
establish physical speaker/projector routing, clean-machine launch, phone/LAN
behavior, hotplug safety, physical A/V drift or latency targets. Mac builds are
ad-hoc signed, not Developer ID signed or notarized. Windows executables are not
Authenticode signed. HTTP control is intended for a trusted local network; share
the remote URL only with the show team.

[First-show setup](https://github.com/arizzi74/Smart-Stage#first-show) ·
[Mac physical test](https://github.com/arizzi74/Smart-Stage/blob/main/docs/macos-physical-test.md) ·
[Build and verification details](https://github.com/arizzi74/Smart-Stage/blob/main/docs/release-verification.md)
