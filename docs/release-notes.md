Smart Stage 1.3.1 makes the Admin interface more compact so the full remote-control QR code fits in a desktop app window without scrolling.

- The smaller header and page title leave more room for your show. Empty message areas no longer reserve space.
- The full-size, 256 × 256 QR code sits at the top-right of the connection panel, beside its instructions, with connection settings still collapsed by default.
- The duplicate gateway-connected message is removed; errors and connection details remain visible.
- The layout was checked in English and Italian, including 1095 × 758 and 900 × 600 desktop viewports. Narrow Admin screens put the QR above the link; the phone remote layout is unchanged.

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
