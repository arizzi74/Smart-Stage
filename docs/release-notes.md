Smart Stage 1.9.2 gives each remote section independent vertical scrolling.

- Scrolling stays inside the selected section. Switching from the bottom of a long section to a shorter one shows its buttons immediately; returning restores the long section's previous scroll position.
- Sections and buttons stay loaded while swiping or tapping tabs. The top controls and playback-position bar remain visible, and the layout fits portrait and landscape screens.
- Scroll positions are remembered while that remote page stays open. Playback and playlist settings are unaffected by navigation.

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
