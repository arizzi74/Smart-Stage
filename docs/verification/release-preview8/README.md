# Preview 8 verification

Preview 8 changes Finder launches from a menu-bar-only application to a regular
Mac application with a Dock icon and standard application menu. It preserves
Terminal-free execution, log redirection, automatic local Admin, remote pairing
and scoped firewall setup.

Release source: `00f0c659dc01fc94432b4394e703e6b68f2a53b0`.
[Tagged run 35508890135](https://github.com/arizzi74/Smart-Stage/actions/runs/35508890135)
passed all 16 jobs and published 36 assets.

| Files | Observed checks |
| --- | --- |
| `archive-verification.json` | Independent downloads of all six ZIPs, SHA-256, exact single-file standalone contents/permissions, native architecture, Go 1.26.5 and clean source revision; matching app/core/icon bytes and Dock metadata |
| `download-*.json` | Published ZIP download, checksum, extraction, automatic browser dispatch, local Admin bootstrap and remote Admin rejection on all four targets |
| `browser-*.json` | Real Admin and remote browser flows through each published native executable |
| `native-icon-*.json` | Native icon decoding/extraction; both Mac Finder launches without Terminal, actual running-app icon and activation policy, log redirection, same-PID reopen and successful Quit with closed listeners |
| `native-http-*.json` | Native HTTP/media fixture checks, ten-second workload, restart and token rotation |
| `installer-darwin-*.json` | Explicit preview 8 installation, 60 true checks each including the Dock app lifecycle, scoped quarantine removal and firewall-rule behavior |

The tag's installer checks explicitly select preview 8 while its bootstrap
source still defaults to preview 7, so `defaultReleaseSelectionTested` is false.

`installer-default-darwin-*.json` records
[run 35509117225](https://github.com/arizzi74/Smart-Stage/actions/runs/35509117225)
at installer source `1c42f555ec6dd5a8de7a00fea7f228d05bb5e725`. Both native Mac
jobs passed using the preview 8 default, without a release override. CI downloads
the exact published installer from its immutable source revision.
`public-bootstrap-verification.json` separately records a plain request to the
unversioned public command URL, whose bytes match that installer and select
preview 8. Its SHA-256 is
`764bc7eaba66f7f2500528934cebcf84d8c33e997f48eacffd68bb73110d6ff4`.

The native observer queries the actual core process through
`NSRunningApplication`, checks its bundle identity and regular activation policy,
and renders its registered icon and the source ICNS through the same AppKit
128-pixel RGBA pipeline. The report records the pixel difference and both hashes.
This establishes Dock eligibility and matching registered artwork; it does not
capture the Dock's on-screen pixels. A standard Quit AppleEvent exercises the
real termination handler and verifies process exit and closed HTTP listeners.
It does not synthesize a Dock-menu click or keyboard shortcut.

Firewall verification changes real blocked rules for the isolated test bundle
and its core. Only interactive elevation is replaced by passwordless sudo on
the hosted runner; the installer's AppleScript command construction runs
normally. The operating system reports both rules as permitted afterward.
Global settings and unrelated app rules remain unchanged, and test rules are
removed. The CI hosts start with the global firewall disabled, so this verifies
rule configuration rather than packet filtering or the password dialog.

Hosted checks remain distinct from physical speaker/projector routing,
phone/camera/Wi-Fi acceptance and clean-machine permissions. The user's positive
preview 7 operational report and subsequent missing-icon complaint are recorded
in the acceptance audit.
