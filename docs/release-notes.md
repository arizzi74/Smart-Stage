Smart Stage preview for macOS Apple Silicon/Intel and Windows ARM64/AMD64.

Download the ZIP for your computer, extract it, and open its executable. Each
primary ZIP contains just one application binary; no installer or additional
runtime is required.

| Computer | Download |
| --- | --- |
| Mac — Apple Silicon (ARM64) | [smartstage-darwin-arm64.zip](https://github.com/arizzi74/Smart-Stage/releases/download/v0.1.0-preview.6/smartstage-darwin-arm64.zip) |
| Mac — Intel (AMD64) | [smartstage-darwin-amd64.zip](https://github.com/arizzi74/Smart-Stage/releases/download/v0.1.0-preview.6/smartstage-darwin-amd64.zip) |
| Windows — ARM64 | [smartstage-windows-arm64.zip](https://github.com/arizzi74/Smart-Stage/releases/download/v0.1.0-preview.6/smartstage-windows-arm64.zip) |
| Windows — Intel / AMD (AMD64) | [smartstage-windows-amd64.zip](https://github.com/arizzi74/Smart-Stage/releases/download/v0.1.0-preview.6/smartstage-windows-amd64.zip) |

Launching Smart Stage automatically opens Admin in the system browser. Admin
listens only on **127.0.0.1:8787**. Its Remote control area displays the phone/tablet
URL and a QR code. Scan the code on the same network to connect; the URL contains
a short random numeric token and no separate pairing-key entry is required.
Remote controls listen on port **8788**, with no access to administration APIs.
The token changes at every launch, so scan the current QR code after restarting.

Windows executables retain the embedded Smart Stage icon. The optional Mac
`smartstage-darwin-<arch>.app.zip` contains a Finder bundle with the same core
executable, launcher and icon. Opening it starts Smart Stage in Terminal and
opens Admin in your system browser. The primary Mac ZIP contains only the
standalone executable.

Native AVFoundation/Media Foundation playback, manual cue playlists, output
selection, persistent blackout and atomic local persistence remain available.
Playback retains preview 4's macOS rendering-layer cleanup and preview 3's
Windows per-stream STOP muting. Prior native resource and physical-test limits
remain documented in
[verification](https://github.com/arizzi74/Smart-Stage/blob/main/docs/release-verification.md).

Each ZIP has an individual SHA-256 checksum. Native build/import/media/icon
checks and extracted-executable startup checks gate publication. Published ZIPs
are then downloaded, checksum-verified, extracted and started on all four
native targets; browser checks exercise the real Admin and remote interfaces.

This is a **preview, not an accepted production release**. Hosted tests cannot
establish physical speaker/projector routing, clean-machine launch, phone/LAN
behavior, hotplug safety, physical A/V drift or latency targets. Mac builds are
ad-hoc signed, not Developer ID signed or notarized. Windows executables are not
Authenticode signed. HTTP control is intended for a trusted local network; share
the remote URL only with the show team.

[First-show setup](https://github.com/arizzi74/Smart-Stage#first-show) ·
[Mac physical test](https://github.com/arizzi74/Smart-Stage/blob/main/docs/macos-physical-test.md) ·
[Build and verification details](https://github.com/arizzi74/Smart-Stage/blob/main/docs/release-verification.md)
