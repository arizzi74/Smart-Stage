# Smart Stage implementation status

The application is implemented and a four-target **preview** is published.
The full specification's physical acceptance is still incomplete; the project
is not declared production-ready or complete.

Repository: https://github.com/arizzi74/Smart-Stage
Release: https://github.com/arizzi74/Smart-Stage/releases/tag/v0.1.0-preview.3

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
[`35471043955`](https://github.com/arizzi74/Smart-Stage/actions/runs/35471043955)
passed builds, dependency checks, native media checks and real-application
HTTP/native smoke tests, published `v0.1.0-preview.3`, and then verified curl/irm
installation on all four platforms. Each installed binary reported its version
and served the Command page. Downloaded release files also passed SHA-256 and
executable architecture, module version and source commit checks locally.
Default installer run
[`35471309014`](https://github.com/arizzi74/Smart-Stage/actions/runs/35471309014)
also passed on all four platforms after switching the defaults to preview 3.

Real browser run
[`35472402760`](https://github.com/arizzi74/Smart-Stage/actions/runs/35472402760)
also passed against the four published preview 3 binaries. Chromium paired
separate Admin/Command sessions over a real non-loopback HTTP address, selected
four host files, saved labels/order, selected outputs and controlled native
PLAY/STOP/natural completion. No media was sent to the browser. Mac runners
played all four cues; Windows played silent video and showed a recoverable
missing-audio-output error. Screenshots/results were inspected. The browser
ran on each host, not on a physical phone across Wi-Fi.

The current release includes draining native inspection before framework
shutdown, a bounded inspection timeout, IPv4 link-local discovery, a 256 TCP
connection cap, and a save-time configuration size limit. Windows STOP now uses
per-stream channel volume instead of shared session mute; this is implemented
and compiled, but Windows audio playback remains unverified on real hardware.

- Mac CI: all four fixture cues played/stopped through actual AVFoundation, on
  virtual/null audio devices and one virtual display; saved-show restart passed.
- Windows CI: all four formats natively inspected; silent video played/stopped
  on a Hyper-V display; restart passed. Runners have **no audio render endpoints**,
  so Windows audio renderer playback/routing is still unverified.
  [Capability checks](https://github.com/arizzi74/Smart-Stage/actions/runs/35473613211)
  confirmed running Windows audio services but no installed sound devices on
  either runner. They tested the published preview without changing OS settings.
- [Captured native pixels](docs/native-display-results.md) at source `505a1e4`
  show moving/restarted video and STOP/end blackout on both Macs and Windows
  AMD64. Mac captures retain a small OS indicator. Windows ARM64 captures show
  first-run Windows setup, so its visual test failed. No physical projector,
  sound or latency claim follows from these virtual-desktop checks.
- Unavailable audio/display IDs cause native errors without a playing event on
  all four targets. Detailed harness records are attached to preview 3.
- Linux ARM64: `go test -race ./...`, `go vet ./...`, Chromium browser checks.
  Shared tests also passed on the four target OS runners. Browser checks cover
  widths 320/390/768/844/1280, STOP usability, escaping and reconnect semantics.

## Still open

Physical non-default audio routing, projector/second-monitor blackout, hotplug/
window relocation, mixed DPI, phones on real LANs, timing targets, clean-machine
acceptance, production signing/notarization and full physical two-hour soak.
A two-hour native CI soak of the exact preview 3 source, commit `d70b3e2`,
completed all four loops and restart checks in
[`35471045100`](https://github.com/arizzi74/Smart-Stage/actions/runs/35471045100).
The earlier [`35468888430`](https://github.com/arizzi74/Smart-Stage/actions/runs/35468888430)
at commit `20bcf35` completed all four two-hour loops and restart checks. Its
[resource analysis](docs/soak-results.md) shows continuing Mac RSS growth and
higher Windows handle counts. The preview 3 results also show continuing Mac
growth. Completed 20-minute profiles show native malloc allocations increasing
while the reported live Go heap remains around 1 MiB. A Mac autorelease-pool
change at `67a10d2` passed all four native build/smoke checks but did not reduce
retained allocations in a comparable 20-minute profile. Heap attribution found
accumulating native caption timers/timebases. Releasing the video layer with
its cue at `ae439c8` removed those classes from stopped snapshots on both Macs
and passed all four native regressions. A 20-minute release-style profile at
`505a1e4` returned native allocation counts slightly below their starting levels
after two stopped idle minutes on both Macs. The exact-source two-hour soak
remains in progress; it must finish before long-run conclusions are drawn.
Windows per-type diagnostics showed substantial event/thread/I/O
handle cleanup during two stopped idle minutes, with file counts unchanged;
no Windows change was justified by these observations. Mac stability remains
under investigation. None verifies physical A/V
drift or Windows audio routing.

See `docs/release-verification.md` for exact environments/evidence and the
remaining checklist, and `docs/acceptance-audit.md` for a specification-wide
evidence audit. Local final release files are downloaded under
`dist/releases/v0.1.0-preview.3/`; local cross-build outputs are under `dist/`.
