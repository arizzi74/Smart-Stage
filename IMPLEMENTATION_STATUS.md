# Smart Stage implementation status

The application is implemented and a four-target **preview** is published.
The full specification's physical acceptance is still incomplete; the project
is not declared production-ready or complete.

Repository: https://github.com/arizzi74/Smart-Stage
Release: https://github.com/arizzi74/Smart-Stage/releases/tag/v0.1.0-preview.2

## Implemented

- Compiled-in AVFoundation/AppKit/Core Audio backend on macOS and Media
  Foundation/EVR/Win32 backend on Windows; real native harness and media fixtures.
- Routed single-timeline playback, persistent native black stage, STOP/end/error
  handling, output enumeration, hotplug handling, Escape and power assertions.
- Go coordinator with generations, stop epochs, latest-only native/load mailboxes,
  bounded request idempotency and SSE subscribers. Atomic per-user persistence,
  backup, corruption diagnostics and native process lock.
- Embedded responsive Admin/Command pages, host filesystem browsing/inspection,
  cue CRUD/labels/order/revisions, separate output controls and stage enablement.
- LAN startup URLs/address refresh, admin/command pairing, session/CSRF/Origin/
  Host enforcement, bounds, path restrictions and command-role path redaction.
- Reproducible matching-OS builds, import audits, checksums, release automation,
  per-user curl/irm installers and operating/API/architecture documentation.

## Built and automatically tested

All four: macOS ARM64/AMD64, Windows ARM64/AMD64. Windows ARM64 was added by
explicit user request. Tagged workflow
[`35469479616`](https://github.com/arizzi74/Smart-Stage/actions/runs/35469479616)
passed builds, dependency checks, native media checks and real-application
HTTP/native smoke tests, published `v0.1.0-preview.2`, and then verified curl/irm
installation on all four platforms. Each installed binary reported its version
and served the Command page. Downloaded release files also passed SHA-256 and
executable architecture checks locally.

Preview 2 includes draining native inspection before framework shutdown, a
bounded interactive-inspection timeout, and a shutdown-during-validation check.

- Mac CI: all four fixture cues played/stopped through actual AVFoundation, on
  virtual/null audio devices and one virtual display; saved-show restart passed.
- Windows CI: all four formats natively inspected; silent video played/stopped
  on a Hyper-V display; restart passed. Runners have **no audio render endpoints**,
  so Windows audio renderer playback/routing is still unverified.
- Native natural completion and stage-enabled persistence are checked through
  native events. No CI assertion proves physical black pixels or actual sound.
- Linux ARM64: `go test -race ./...`, `go vet ./...`, Chromium browser checks.
  Shared tests also passed on the four target OS runners. Browser checks cover
  widths 320/390/768/844/1280, STOP usability, escaping and reconnect semantics.

## Still open

Physical non-default audio routing, projector/second-monitor blackout, hotplug/
window relocation, mixed DPI, phones on real LANs, timing targets, clean-machine
acceptance, production signing/notarization and full physical two-hour soak.
A two-hour native CI soak is running separately in
[`35468888430`](https://github.com/arizzi74/Smart-Stage/actions/runs/35468888430).
It uses commit `20bcf35`, before the preview 2 shutdown change. Its result must
be inspected, not assumed. It cannot verify physical A/V drift.

See `docs/release-verification.md` for exact environments/evidence and the
remaining checklist, and `docs/acceptance-audit.md` for a specification-wide
evidence audit. Local final release files are downloaded under
`dist/releases/v0.1.0-preview.2/`; local cross-build outputs are under `dist/`.
