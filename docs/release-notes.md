Smart Stage 1.2.0 adds playlist files you can save and load from Admin.

- Choose **Save playlist…** to create a `.smartstage.json` file using the native Mac/Windows Save dialog, or a browser download when no native dialog is available.
- Choose **Load playlist…** after stopping playback and turning Stage off. Confirm replacement, then select the file. Loading keeps playback stopped and revalidates media.
- Files preserve cue order, labels, colors, hidden/background flags and stage/sound settings. Media stays at its original paths; it is not copied or embedded. Output devices, gateway credentials and language preferences stay local.
- Cancelled dialogs, invalid files, missing media and conflicting edits preserve the current playlist. Controls and dialogs support English and Italian.

Quit Smart Stage fully and reopen it to receive the desktop update before playback starts. Saved shows, language and connection settings are preserved.

This feature works with the existing 1.1.2 gateway daemon; no gateway upgrade or restart is required. The URL-only gateway save and earlier security fixes remain included.

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
