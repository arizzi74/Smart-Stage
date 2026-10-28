Smart Stage 1.7.1 improves Cloudflare connections on networks that block QUIC.

- Cloudflare still tries QUIC first, but switches to HTTP/2 sooner when connection attempts fail. Previously the app's startup timeout could restart the helper before it reached that fallback.
- Admin retains a clear connection-timeout notice while retrying instead of immediately replacing it with a generic connecting message. The notice clears when connected or when you explicitly reconnect/change modes. English and Italian are supported.
- Admin stays local; helper verification, encrypted connections, gateway settings and Windows ARM64 availability are unchanged. Networks must still permit Cloudflare Tunnel traffic on UDP or TCP port 7844; the app does not bypass network policy or disable TLS checks.

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
