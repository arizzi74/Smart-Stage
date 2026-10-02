Smart Stage 1.6.1 improves remote volume response and makes track adjustments easier to read on Mac and Windows.

- Remote master volume sends the latest value after the slider has stayed still for **200 ms**, or immediately when you release it. Pending changes replace earlier values, and successful adjustments no longer wait for an extra status request.
- Track volume displays **0%** in the center, negative percentages to the left and positive percentages to the right. Halfway left is **−50%**; halfway right is **+50%**. These are adjustments relative to the centered level.
- Existing saved track levels and playlist files retain the same playback volume. The remote master remains an absolute percentage, starting at **75%** each launch.
- Playback position, pause state and independent audio/visual fades are preserved. Both interfaces support English and Italian.

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
