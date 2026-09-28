Smart Stage 1.5.1 improves background buttons, saving stage settings and update visibility on Mac and Windows.

- Press a selected **Background button** again to deselect it and return to the saved default background, or black if none is configured. Independent music keeps playing. If the button is also the default background, deselection leaves that default visible.
- Saving stage settings after hiding or editing a playlist button preserves your draft without an unnecessary playlist revision error. Real conflicting changes remain protected, with recovery available inside Admin.
- A prominent update notice makes startup checks, downloading/verifying and automatic restart visible in Admin, in English and Italian.
- Your independent audio and video/image fade switches and durations remain available. Saved shows and playlist files keep their settings.

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
