Smart Stage preview for macOS Apple Silicon/Intel and Windows ARM64/AMD64.

This preview adds a Smart Stage application icon. Both Windows executables
embed the icon at nine sizes. Both Mac architectures also have an optional
`smartstage-darwin-<arch>.app.zip` download: extract `Smart Stage.app`, move it
to Applications if desired, and open it in Finder. It starts the bundled
executable in Terminal so pairing keys, logs and Ctrl+C remain visible.
The standalone Mac binaries and curl installer retain their existing behavior.

Native AVFoundation/Media Foundation host playback, embedded Admin and Command
interfaces, paired LAN access, manual cue playlists, output selection, persistent
blackout, atomic local persistence and one-command installers are implemented.

The playback implementation retains preview 4's release of the macOS video rendering layer with each stopped or
replaced cue. The stage window and opaque black overlay remain present across
STOP. That change targets the caption-timer/timebase accumulation found in
preview 3's two-hour native tests. Detailed before/after heap measurements and
long-run status are recorded in
[native resource verification](https://github.com/arizzi74/Smart-Stage/blob/main/docs/soak-results.md).

Windows retains preview 3's per-stream STOP muting. Physical Windows audio
testing is still pending. If an older preview left Smart Stage's entry muted in
the Windows volume mixer, unmute that application entry before rehearsal.
Native checks cover unavailable output IDs, natural completion, saved-show
restart, shutdown during validation and repeated PLAY/replacement/STOP.

The attached executables embed the web interfaces and native bridges. They
require only supported OS libraries at runtime. Each has an individual SHA-256
checksum and an import audit. Native and application smoke records are attached,
along with native icon checks and SHA-256 checksums for the Mac app archives.

This is a **preview, not an accepted production release**. CI exercises real
native APIs and HTTP controls, but does not prove physical speaker/projector
routing, clean-machine installation, phone/LAN behavior, hotplug safety, visible
blackout, physical A/V drift or latency targets. Automated memory/handle evidence
is separate from physical acceptance; consult the linked results for the tested
source and duration. macOS builds are ad-hoc signed,
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
