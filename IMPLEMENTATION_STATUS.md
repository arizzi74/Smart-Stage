# Smart Stage implementation status

Specification: `SMART_STAGE_CODEX_PROMPT.md` version 1.0. This is an incomplete
implementation, not a validated release.

## Environment

Development host: Ubuntu 22.04, Linux ARM64, Go 1.26.5. No Windows interactive
desktop, Apple SDK, macOS runtime, physical audio endpoints, or stage monitors
are available on this host.

## Milestones

1. Native feasibility — real Media Foundation/AVFoundation bridge implementations
   and native harness committed. Windows AMD64/ARM64 cross-builds and OS-only PE
   import audits pass. Both macOS targets compiled on GitHub runners; startup
   exposed a JSON boolean encoding bug, now corrected and awaiting re-run.
   Native opening, routing and blackout acceptance are not yet established.
2. Core — implemented: generation/epoch coordinator, bounded idempotency and
   subscriber mailboxes, async inspection, optimistic playlist revisions,
   canonical read-only browsing, atomic persistence/backup and process lock.
   Go race tests pass on Linux ARM64.
3. Web — implemented: embedded responsive Admin/Command pages, LAN discovery,
   pairing and roles, CSRF/Origin/Host checks, filesystem browser, playlist and
   output editing. HTTP authorization, redaction, malformed-request and SSE
   reconnect tests pass on Linux ARM64. Browser/native app end-to-end tests next.
4. Reliability — in progress; native device loss and window behavior require
   physical verification. Both Windows native runner smoke tests pass. Mac
   native smoke tests have passed; an ARM64 run exposed an overly short
   start-to-STOP test timer, now replaced with STOP after the playing event.
5. Release — pending; physical acceptance and clean-machine verification require
   Windows 11 and both macOS architectures.

Repository: https://github.com/arizzi74/Smart-Stage. User additionally requested
Windows ARM64 binaries, GitHub releases and curl/irm installers. CI builds all
four targets. Release/installer publication remains pending the application.

No successful native playback, physical routing, latency, soak, or clean-machine
result has yet been recorded. Builds do not establish those capabilities.
