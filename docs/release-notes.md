Smart Stage 1.9.0 adds smooth section swiping and a compact playlist sorter.

- The remote cue pages follow your finger while swiping left or right, reveal the adjacent section and smoothly snap into place. Short swipes settle back; vertical scrolling and normal cue taps remain available. Reduced-motion preferences are respected.
- **Sort Playlist** at the top of Admin → Playlist opens a names-only overview. Drag sections as groups, reorder cues or move them between sections, including empty ones.
- **Save order** applies the rearrangement together; **Cancel** discards it. Paths, colors, levels, audio options, stage settings and section collapse metadata stay in place. Sorting and section navigation keep playback running.
- The sorter supports English/Italian, keyboard alternatives and safe revision conflicts. Its assets load only in local Admin; existing public gateways need no changes.

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
