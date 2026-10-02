Smart Stage 1.6.4 fixes playlist track-volume dragging on Mac and saves drag adjustments on release.

- Click and drag a track-volume slider to preview its adjustment. Moving the pointer over it without pressing does not move it, and its cursor stays an arrow.
- Mouse/touch adjustments save only when released, including a release outside the slider. A stationary held drag does not save intermediate revisions. Keyboard adjustments save on key release; unchanged levels are skipped.
- Canceled drags clear the native slider's tracking state. Playlist updates and pending saves preserve the newest released adjustment.
- The remote master keeps its existing 200 ms behavior and 75% launch default. Centered 0% track readouts and saved volume levels stay compatible.

Quit Smart Stage fully and reopen it to receive the desktop update before playback starts. Saved shows, language and connection settings are preserved.

This feature works with the existing gateway daemon; no gateway upgrade or restart is required.

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
