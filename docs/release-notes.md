Smart Stage preview for macOS Apple Silicon/Intel and Windows ARM64/AMD64.

Native AVFoundation/Media Foundation host playback, embedded Admin and Command
interfaces, paired LAN access, manual cue playlists, output selection, persistent
blackout, atomic local persistence and one-command installers are implemented.

This preview includes IPv4 link-local LAN discovery, a total HTTP connection
cap, and a save-time size check that prevents writing a show too large to reopen.
Outstanding native inspection drains before framework shutdown. Native checks
also cover unavailable output IDs and shutdown during media validation.

The attached executables embed the web interfaces and native bridges. They
require only supported OS libraries at runtime. Each has an individual SHA-256
checksum and an import audit. Native and application smoke records are attached.

This is a **preview, not an accepted production release**. CI exercises real
native APIs and HTTP controls, but does not prove physical speaker/projector
routing, clean-machine installation, phone/LAN behavior, hotplug safety, visible
blackout, the two-hour soak or latency targets. macOS builds are ad-hoc signed,
not Developer ID signed or notarized. Windows executables are not Authenticode
signed. See `docs/release-verification.md` for the exact evidence and limitations.

Install on macOS (Apple Silicon or Intel):

```sh
curl -fsSL https://raw.githubusercontent.com/arizzi74/Smart-Stage/main/install.sh | sh
```

Install in Windows PowerShell (ARM64 or AMD64):

```powershell
irm https://raw.githubusercontent.com/arizzi74/Smart-Stage/main/install.ps1 | iex
```

The installer selects your architecture and verifies the executable's SHA-256.
Run `smartstage` afterward (open a new login terminal on Mac, or use the full
path printed by its installer). Alternatively, download the matching executable
below. No Go, Node, Python, media player or extra runtime is required.

[First-show setup](https://github.com/arizzi74/Smart-Stage#first-show) ·
[Build and verification details](https://github.com/arizzi74/Smart-Stage/blob/main/docs/release-verification.md)
