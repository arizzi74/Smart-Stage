# Preview 7 verification records

Release source: `7ca8c0685ed237849406fbf2371d44a9c500ce8d`.

[Tagged run 35503933513](https://github.com/arizzi74/Smart-Stage/actions/runs/35503933513)
passed all 16 jobs and published 36 assets. The records are:

| Files | Observed checks |
| --- | --- |
| `archive-verification.json` | Independent downloads of all six ZIPs, SHA-256, exact single-file standalone contents/permissions, Mach-O/PE architecture, Go 1.26.5 and clean source revision; app/core/icon identity and background-launch metadata |
| `download-*.json` | Published ZIP download, checksum, extraction, automatic browser dispatch, local Admin bootstrap and remote Admin rejection on all four targets |
| `browser-*.json` | Real Admin and remote browser flows through each published native executable |
| `native-icon-*.json` | Native icon decoding/extraction; both Mac Finder launches without Terminal, log redirection, same-PID reopen and successful Quit with closed listeners |
| `native-http-*.json` | Native HTTP/media fixture checks, ten-second workload, restart and token rotation |
| `installer-darwin-*.json` | Explicit preview 7 installation, 54 true checks each including app lifecycle, scoped quarantine removal and firewall-rule behavior |

Available executable/archive hashes in all reports match the independent
archive record. The tag's installer checks explicitly select preview 7 while
the bootstrap still defaults to preview 6, so their
`defaultReleaseSelectionTested` fields are intentionally false.

`installer-default-darwin-*.json` records
[run 35504365951](https://github.com/arizzi74/Smart-Stage/actions/runs/35504365951)
at verification source `84337bd7113f535e2bac7d34e6b13e278e007f33`. Both native
Mac checks select preview 7 without a version override. CI downloads the exact
published installer from its immutable source revision, avoiding branch/CDN
timing mismatches. `public-bootstrap-verification.json` separately records a
plain request to the unversioned public command URL, whose bytes match that
installer and select preview 7. Its SHA-256 is
`0970a14512627aab1cef624274ec41df9f57255edbf724dc3aac1f0c7079d409`.

`installer-firewall-preview6-darwin-*.json` records
[run 35503879234](https://github.com/arizzi74/Smart-Stage/actions/runs/35503879234):
the new installer with the existing preview 6 app on both Mac architectures.
The reports establish targeted quarantine removal, atomic replacement/recovery,
configuration preservation and real firewall-rule changes. Their filenames and
`release` fields intentionally identify preview 6.

Firewall verification creates actual blocked rules for an isolated test bundle
and its core, then uses the installer's AppleScript command construction with
only interactive elevation replaced by passwordless sudo on the hosted runner.
The operating system reports each rule as permitted afterward. Global firewall
settings and all unrelated app rules remain unchanged; test rules are removed.
Cancellation leaves the verified installation available and explains recovery.
The CI hosts start with the global firewall disabled. These reports establish
rule configuration, not physical network traffic or password-dialog behavior.

All hosted results remain distinct from physical speaker/projector routing,
phone/camera/Wi-Fi acceptance and clean-machine permissions. The user's Mac
firewall block has been identified; a successful user LAN retest remains to be
recorded in the acceptance audit.
