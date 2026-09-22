Smart Stage 1.1.2 is a security maintenance update for the desktop app and Linux gateway.

- The gateway rejects unpaired STOP and emergency-stop requests before they can occupy reserved command capacity. Slow request bodies are bounded separately from command execution.
- Phones with the correct public access key can pair even after incorrect guesses have exhausted a shared rate limit. Invalid keys remain limited; the shorter Local LAN code retains its existing guessing protections.
- All binaries are rebuilt with Go 1.26.8. Release publication now requires successful vulnerability scans of Go dependencies and all six desktop/gateway executables.

Quit Smart Stage fully and reopen it to receive the desktop update before playback starts. Saved shows, language and connection settings are preserved.

The public Linux gateway also needs an update. On its server, rerun:

```sh
curl -fsSL https://github.com/arizzi74/Smart-Stage/releases/latest/download/install-gateway.sh | sh
```

The gateway installer preserves its registration settings. After it restarts, reconnect phones using the current QR code in Admin. The desktop updater does not update the server daemon.

Install on Mac:

```sh
curl -fsSL https://raw.githubusercontent.com/arizzi74/Smart-Stage/main/install.sh | sh
```

Install on Windows from a normal PowerShell window:

```powershell
irm https://raw.githubusercontent.com/arizzi74/Smart-Stage/main/install.ps1 | iex
```

[Download ZIPs](https://github.com/arizzi74/Smart-Stage/releases/latest) · [Website](https://arizzi74.github.io/Smart-Stage/) · [Gateway guide](https://github.com/arizzi74/Smart-Stage/blob/main/docs/gateway-install.md).

The gateway origin-isolation finding remains open pending the separate-domain migration. Mac builds remain ad-hoc signed and unnotarized; Windows binaries are not Authenticode signed. Windows uses Microsoft WebView2, supplied by the installer if missing. [Verification and known limits](https://github.com/arizzi74/Smart-Stage/blob/main/docs/release-verification.md).
