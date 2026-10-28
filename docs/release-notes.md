Smart Stage 1.7.0 adds account-free Cloudflare remote control with live WebSocket status.

- New installations without gateway settings use Cloudflare on supported platforms. Smart Stage downloads and verifies the official native helper, runs it in the background, and displays an HTTPS remote URL and QR code. No Cloudflare account, server or administrator privileges are needed.
- Configured gateways and explicit Local network choices remain selected. Switch between Cloudflare, Public gateway and Local network without losing the saved gateway URL or token.
- Cloudflare is grayed out on Windows ARM64 because the official distribution has no native helper. Gateway and Local network remain available; no emulated helper is substituted.
- Cloudflare uses WebSockets for live status and authenticated HTTPS requests for commands. Only remote controls are exposed: Admin stays local, LAN access stays closed, and a separate high-entropy pairing secret protects each connection.
- Quitting, reconnecting or switching modes closes the helper and its remote sessions. Cloudflare Quick Tunnel URLs change on reconnection; scan the new QR code. Quick Tunnels require internet access and have no uptime guarantee.
- Update checks continue to use the website manifest without a GitHub account or token. Existing gateways work without a server upgrade.

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
