Smart Stage 1.6.2 reduces repeated playlist saves while adjusting track volume on Mac and Windows.

- Playlist track sliders save the latest value after it has stayed still for **200 ms**, or immediately when you release the slider. Continuous dragging no longer saves intermediate levels every 120 ms.
- Each track keeps its own save deadline. A pending save retains the latest adjustment, and returning to an unchanged saved level does not create another revision.
- Track volume still displays **0%** in the center, with negative/positive percentages; existing saved levels and playlist files retain the same playback volume. The remote master keeps its existing 200 ms behavior and **75%** launch default.
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
