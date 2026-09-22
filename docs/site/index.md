# Smart Stage — live show control for Mac and Windows

Play audio, video and image cues on your computer, controlled from a phone or tablet. Build a playlist in the dedicated desktop Admin window, choose your speakers and stage display, then trigger colored cue buttons from the remote. Media files stay on your computer.

The app supports English and Italian, following the system language with a manual selector in Admin’s top bar. The remote follows the phone or tablet language.

Smart Stage is for live performance playback and stage presentation. It supports image or looping-video backgrounds, optional background sound, adjustable audio fades and crossfades, and independent Stage on/off controls.

[English website](https://arizzi74.github.io/Smart-Stage/en/) · [Italiano](https://arizzi74.github.io/Smart-Stage/it/) · [GitHub repository](https://github.com/arizzi74/Smart-Stage) · [Latest release](https://github.com/arizzi74/Smart-Stage/releases/latest)

## Install

Mac builds target macOS 12 or newer. Windows 11 is the intended Windows baseline. Both platforms have ARM64 and Intel/AMD64 downloads.

On Mac, paste into Terminal:

```sh
curl -fsSL https://raw.githubusercontent.com/arizzi74/Smart-Stage/main/install.sh | sh
```

On Windows, paste into a normal PowerShell window:

```powershell
irm https://raw.githubusercontent.com/arizzi74/Smart-Stage/main/install.ps1 | iex
```

The installers detect your architecture, install the app and open Admin. You can close the terminal afterward. The Windows installer checks the required Microsoft WebView2 runtime. Desktop updates install automatically on launch, before playback starts.

Prefer a download? Extract the ZIP for your computer and open the app:

| Computer | Download |
| --- | --- |
| Mac — Apple Silicon | [ARM64 app ZIP](https://github.com/arizzi74/Smart-Stage/releases/latest/download/smartstage-darwin-arm64.app.zip) |
| Mac — Intel | [AMD64 app ZIP](https://github.com/arizzi74/Smart-Stage/releases/latest/download/smartstage-darwin-amd64.app.zip) |
| Windows — ARM64 | [ARM64 executable ZIP](https://github.com/arizzi74/Smart-Stage/releases/latest/download/smartstage-windows-arm64.zip) |
| Windows — Intel / AMD | [AMD64 executable ZIP](https://github.com/arizzi74/Smart-Stage/releases/latest/download/smartstage-windows-amd64.zip) |

Mac browser downloads may require **Open Anyway** because the app is not notarized. The Mac installation command removes quarantine from this app. See the [installation guide](https://github.com/arizzi74/Smart-Stage/blob/main/docs/user-guide.md#download-zips) for permissions and runtime requirements.

## Start a show

1. Drop media from Finder or File Explorer into the dedicated Admin window, or choose **Choose Media…**. Files remain in their original locations.
2. Choose your audio output and stage display. Set cue labels, colors and an optional image or video background.
3. Open **Remote control → Connection settings**. Configure your gateway URL and token, or select **Local network** and follow the firewall guidance.
4. Scan the QR code on your phone or tablet and tap a cue to play.

Images can change the stage picture while music keeps playing. **Stage on/off** controls the stage independently. **STOP** returns to the current background; **Escape** immediately stops all sound and closes the stage when Smart Stage's native window has focus. **Quit Smart Stage** in Admin stops playback and exits.

## Remote control and Linux gateway

Public gateway mode is the default. It needs a configured public server and internet access; it keeps the desktop's inbound LAN remote port closed. Admin stays local on `127.0.0.1`. Alternatively, choose local LAN control without a gateway.

On your public Linux server with systemd, install the separate ARM64 or AMD64 gateway:

```sh
curl -fsSL https://github.com/arizzi74/Smart-Stage/releases/latest/download/install-gateway.sh | sh
```

The installer lists suitable existing nginx HTTPS hosts and can add a `/smartstage` route. If nginx is absent, it offers Caddy. Enter the generated URL and registration token in desktop Admin; Admin then displays a separate remote URL and QR code for phones and tablets. The gateway carries control traffic, while media and playback stay on the desktop. Server updates use the gateway installer.

## Documentation

- [User guide](https://github.com/arizzi74/Smart-Stage/blob/main/docs/user-guide.md): installation, show setup and troubleshooting.
- [Gateway deployment](https://github.com/arizzi74/Smart-Stage/blob/main/docs/gateway-install.md): HTTPS proxy setup, service management and updates.
- [Media compatibility](https://github.com/arizzi74/Smart-Stage/blob/main/docs/media-compatibility.md): native formats and playback limits.
- [Architecture](https://github.com/arizzi74/Smart-Stage/blob/main/docs/architecture.md): desktop, playback and remote-control design.
- [Release verification](https://github.com/arizzi74/Smart-Stage/blob/main/docs/release-verification.md): evidence and known testing limits.
- [Report a bug or request a feature](https://github.com/arizzi74/Smart-Stage/issues/new/choose).
