Smart Stage preview for macOS Apple Silicon/Intel and Windows ARM64/AMD64.

Native AVFoundation/Media Foundation host playback, embedded Admin and Command
interfaces, paired LAN access, manual cue playlists, output selection, persistent
blackout, atomic local persistence and one-command installers are implemented.

This preview also drains outstanding native inspection before framework
shutdown and bounds interactive inspection requests. Shutdown during media
validation is included in the native application checks.

The attached executables embed the web interfaces and native bridges. They
require only supported OS libraries at runtime. Each has an individual SHA-256
checksum and an import audit. Native and application smoke records are attached.

This is a **preview, not an accepted production release**. CI exercises real
native APIs and HTTP controls, but does not prove physical speaker/projector
routing, clean-machine installation, phone/LAN behavior, hotplug safety, visible
blackout, the two-hour soak or latency targets. macOS builds are ad-hoc signed,
not Developer ID signed or notarized. Windows executables are not Authenticode
signed. See `docs/release-verification.md` for the exact evidence and limitations.

Download the executable matching your OS and architecture, or use the installer
instructions in README. No Go, Node, Python, media player or extra runtime is
required to run Smart Stage.
