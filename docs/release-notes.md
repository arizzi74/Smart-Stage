Smart Stage 1.6.0 adds track and master volume controls on Mac and Windows.

- Every audio/video cue has a **Track volume** slider in Admin. It starts in the center at 50%, with **− / +** directions for balancing tracks. Background video soundtracks use their cue's level too.
- A speaker slider at the top of the remote adjusts **Master volume**, starting at 75% each launch. Set it to 0% to mute while playback continues, or raise it to 100%.
- Live volume changes preserve playback position, pause state and independent audio/visual fades. The master also scales outgoing sound during crossfades without changing system volume.
- Track levels are saved with your show and `.smartstage.json` playlist files. Existing shows and playlist files receive centered defaults.
- Both controls work in English and Italian. Master control works through existing public gateways without a gateway upgrade.

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
