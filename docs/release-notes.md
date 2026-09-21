Smart Stage 1.0.0 is the first stable release for Mac and Windows, with a Linux public gateway.

- Dedicated desktop Admin with Finder/Explorer drag-and-drop, media chooser, app icons and Quit controls.
- Audio, video and image cues, configurable button colors, stage backgrounds and audio fades/crossfades.
- Phone/tablet controls with a QR code, Stage controls and optional Keep awake over HTTPS.
- Public gateway by default, or Local LAN with guided firewall setup. Admin remains local to the host.
- Automatic updates on launch before playback, preserving saved shows. Preview installations can upgrade to 1.0.0; stable installations receive stable updates.
- Includes the Windows media-format detection, close confirmation and transparent color-cursor changes from the previews.
- Shorter README with Admin and Remote screenshots, quick setup and installation commands.

Install on Mac:

```sh
curl -fsSL https://raw.githubusercontent.com/arizzi74/Smart-Stage/main/install.sh | sh
```

Install on Windows from a normal PowerShell window:

```powershell
irm https://raw.githubusercontent.com/arizzi74/Smart-Stage/main/install.ps1 | iex
```

Install the gateway on a public Linux server with systemd:

```sh
curl -fsSL https://github.com/arizzi74/Smart-Stage/releases/download/v1.0.0/install-gateway.sh | sh
```

Downloads include Mac app bundles, standalone desktop ZIPs for ARM64/AMD64, static Linux gateway binaries and checksums. The desktop installers choose the architecture automatically. [First-show setup](https://github.com/arizzi74/Smart-Stage#start-a-show) · [Gateway guide](https://github.com/arizzi74/Smart-Stage/blob/main/docs/gateway-install.md).

Mac builds remain ad-hoc signed and unnotarized; the Mac installer removes quarantine only from Smart Stage. Windows binaries are not Authenticode signed. Windows uses Microsoft WebView2, supplied by the installer if missing.

The stable release label does not change the recorded test scope. Physical speakers/projectors and the remaining UTM cursor observation still need confirmation on the affected equipment. [Verification and known limits](https://github.com/arizzi74/Smart-Stage/blob/main/docs/release-verification.md).
