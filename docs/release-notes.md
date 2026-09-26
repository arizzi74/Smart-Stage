Smart Stage 1.3.0 adds video/image cue toggles and visual transitions.

- Press an already selected video or image button again to stop it. Stage stays on and returns to the current background, or black if no background is selected.
- Stopping an image preserves independent music. If the image covers a running video, that video also stops instead of reappearing.
- Enable **Fade audio, video and images** in **Admin → Stage & sound**. The existing duration controls visual fades and crossfades as well as sound (default one second; configurable from 0.1 to 30 seconds). Starting from silence or black stays immediate.
- Video/image replacements crossfade; stopping returns smoothly to the background or fades to black. Stage off and Escape remain immediate and cancel visual transitions.
- The optional music toggle and background-selection buttons keep their existing behavior. Admin and remote controls explain repeat-press actions in English and Italian.

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
