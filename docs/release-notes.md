Smart Stage preview for macOS Apple Silicon/Intel and Windows ARM64/AMD64.

Preview 12 simplifies the remote and improves desktop controls:

- Cue buttons now sit directly below the remote top bar. The large “Show
  control” heading and ordinary command acknowledgement block are removed;
  playback and connection errors remain visible.
- **Quit Smart Stage** in Admin stops playback, closes the stage and exits the
  application. The page stays open and quietly waits for the next launch.
- An existing Admin tab reconnects on relaunch and reloads the current interface.
  Startup waits up to six seconds for it before opening another page. Mac
  Dock/menu actions and native imports also reuse a detected Admin page. Browser
  suspension can prevent detection; selecting an exact browser tab is controlled
  by the browser and is not guaranteed.
- **Choose files on this Mac…** in the bundled Mac app's Admin opens the native
  file chooser. Files remain at their original paths. Finder drops onto the
  Smart Stage Dock icon still work. A browser cannot reveal original Finder
  paths, so a drop into the web page explains the native chooser/Dock options.

Compact host browsing, hidden-file control, cue colors, Stage on/off, Escape and
Keep awake behavior from preview 11 are retained. The HTTP LAN remote still
cannot use the standard HTTPS-only screen wake-lock API.

After upgrading from preview 11 or earlier, refresh an already open Admin tab
once to load these new controls. Subsequent relaunches refresh it automatically.

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
| Mac — Apple Silicon (ARM64) | [smartstage-darwin-arm64.zip](https://github.com/arizzi74/Smart-Stage/releases/download/v0.1.0-preview.12/smartstage-darwin-arm64.zip) |
| Mac — Intel (AMD64) | [smartstage-darwin-amd64.zip](https://github.com/arizzi74/Smart-Stage/releases/download/v0.1.0-preview.12/smartstage-darwin-amd64.zip) |
| Windows — ARM64 | [smartstage-windows-arm64.zip](https://github.com/arizzi74/Smart-Stage/releases/download/v0.1.0-preview.12/smartstage-windows-arm64.zip) |
| Windows — Intel / AMD (AMD64) | [smartstage-windows-amd64.zip](https://github.com/arizzi74/Smart-Stage/releases/download/v0.1.0-preview.12/smartstage-windows-amd64.zip) |

Launching Smart Stage automatically opens Admin in the system browser. Admin
listens only on **127.0.0.1:8787**. Its Remote control area displays the phone/tablet
URL and a QR code. Scan the code on the same network to connect; the URL contains
a short random numeric token and no separate pairing-key entry is required.
Remote controls listen on port **8788**, with no access to administration APIs.
The token changes at every launch, so scan the current QR code after restarting.

Windows executables retain the embedded Smart Stage icon. The optional Mac
`smartstage-darwin-<arch>.app.zip` contains a Finder bundle with the same core
executable, launcher and icon. Opening it starts Smart Stage without Terminal and
opens Admin in your system browser. The primary Mac ZIP contains only the
standalone executable.

Native AVFoundation/Media Foundation playback, manual cue playlists, output
selection, persistent blackout and atomic local persistence remain available.
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
