Smart Stage 1.1.3 lets you change the public gateway URL without re-entering its saved token.

- In Admin → Remote control → Connection settings, change the URL and leave the token blank to keep its saved value. Save connects to the selected HTTPS gateway using that token.
- Enter a token when configuring a gateway for the first time, or when you want to replace the saved token. Stored secrets are never returned to the Admin page.
- English and Italian guidance explains the behavior. Invalid settings and failed saves preserve the previous configuration.

Quit Smart Stage fully and reopen it to receive the desktop update before playback starts. Saved shows, language and connection settings are preserved.

This desktop settings change works with the existing 1.1.2 gateway daemon. After changing the gateway URL, reconnect phones using the current QR code in Admin.

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
