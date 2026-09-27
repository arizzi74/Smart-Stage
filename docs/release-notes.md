Smart Stage 1.4.0 adds audio/video seeking and pause/resume controls on Mac and Windows, in both Admin and the phone/tablet remote.

- Use the bottom slider to seek. While touching or dragging, a large centered time preview fades in; release to jump to that position and fade the preview out.
- Press the selected audio or video button to pause, then press it again to resume. The cue stays selected, and paused video keeps its frame. Seeking while paused keeps it paused.
- STOP ends playback and returns the stage to its background or black. Image buttons retain their existing toggle behavior, and Stage on/off remains independent.
- The controls support touch, mouse, keyboard, English and Italian. Unknown-duration media shows an unavailable seek bar; stale or interrupted gestures are cancelled.

Quit Smart Stage fully and reopen it to receive the desktop update before playback starts. Saved shows, language and connection settings are preserved.

This feature works with the existing 1.1.2 gateway daemon; no gateway upgrade or restart is required. Playlist files, URL-only gateway changes and earlier security fixes remain included.

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
