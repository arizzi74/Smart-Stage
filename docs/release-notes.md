Smart Stage 1.5.0 adds independent audio and video/image fade settings on Mac and Windows.

- In Admin → Stage & sound, enable **Fade audio** and **Fade video and images** independently.
- Set a separate duration for each, from 0.1 to 30 seconds. Each defaults to one second.
- Audio fading includes music, video soundtracks and background-video sound. Visual fading controls only the picture.
- Older saved settings and `.smartstage.json` playlist files keep their previous behavior: the original fade switch and duration migrate to both groups.
- Video playback stays alive until both its picture and soundtrack fades have finished, even when they use different durations. Pause, seeking, STOP and emergency Escape remain available.

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
