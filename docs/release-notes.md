Smart Stage 1.8.0 adds playlist sections and independent audio mixing.

- **Do not stop video** keeps the current video playing while the audio cue replaces its soundtrack. The video stays silent after the audio finishes or is paused, until that video ends or is restarted.
- **Mix audio** plays the cue alongside existing music and video audio. It keeps the video running even when Do not stop video is unchecked. Several audio cues can play together.
- **Playlist sections** organize cues into named, collapsible groups in Admin. Move cues between sections with their Section selector. Names, order and collapsed state are saved with the show and exported playlist files.
- The remote displays section tabs below its top controls. Tap a tab or swipe left/right across the cue area; navigation keeps playback running.
- Each selected audio/video button pauses or resumes its own source. STOP clears every playing cue and returns to the selected background. Per-track volume, master volume and audio fades apply to the mix.
- Both options appear only on audio cues in Admin, in English or Italian, and are included in saved `.smartstage.json` playlists. Existing playlists keep their previous playback behavior by default.

Builds include the Go 1.26.11 HTTP-server security fixes.

Quit Smart Stage fully and reopen it to receive the desktop update before playback starts. Saved shows, language and connection settings are preserved.

If upgrading from a version older than 1.6.6 whose updater is rate-limited, use the installer below once. Subsequent checks use the website manifest.

Install on Mac:

```sh
curl -fsSL https://raw.githubusercontent.com/arizzi74/Smart-Stage/main/install.sh | sh
```

Install on Windows from a normal PowerShell window:

```powershell
irm https://raw.githubusercontent.com/arizzi74/Smart-Stage/main/install.ps1 | iex
```

[Download ZIPs](https://github.com/arizzi74/Smart-Stage/releases/latest) · [Website](https://arizzi74.github.io/Smart-Stage/) · [Gateway guide](https://github.com/arizzi74/Smart-Stage/blob/main/docs/gateway-install.md).

Use a dedicated gateway hostname and share its registration token only with mutually trusted Smart Stage hosts; endpoint paths on one gateway still share a browser origin. Mac builds remain ad-hoc signed and unnotarized; Windows binaries are not Authenticode signed. Windows uses Microsoft WebView2, supplied by the installer if missing. [Verification and known limits](https://github.com/arizzi74/Smart-Stage/blob/main/docs/release-verification.md).
