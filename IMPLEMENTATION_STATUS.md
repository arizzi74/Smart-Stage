# Smart Stage implementation status

Specification: `SMART_STAGE_CODEX_PROMPT.md` version 1.0. This is an incomplete
implementation, not a validated release.

## Environment

Development host: Ubuntu 22.04, Linux ARM64, Go 1.26.5. No Windows interactive
desktop, Apple SDK, macOS runtime, physical audio endpoints, or stage monitors
are available on this host.

## Milestones

1. Native feasibility — in progress: implementing compiled-in Windows Media
   Foundation and macOS AVFoundation backends and a real native harness first.
2. Core — pending.
3. Web — pending.
4. Reliability — pending.
5. Release — pending; physical acceptance and clean-machine verification require
   Windows 11 and both macOS architectures.

No native playback, physical routing, latency, soak, or clean-machine result has
yet been recorded. A build or fake-backend unit test will not establish those.
