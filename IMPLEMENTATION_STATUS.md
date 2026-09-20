# Smart Stage implementation status

The desktop application and Linux gateway are implemented and a **preview** is published.
The full specification's physical acceptance is still incomplete; the project
is not declared production-ready or complete.

Repository: https://github.com/arizzi74/Smart-Stage
Release: https://github.com/arizzi74/Smart-Stage/releases/tag/v0.1.0-preview.16

## Implemented

- Static Linux AMD64/ARM64 public gateway, authenticated outbound WSS relay,
  strict remote-only routes, isolated pairing sessions, bounded HTTP/SSE relay
  and automatic desktop reconnection. Public HTTPS mode is the default and
  closes direct LAN access. Admin URL/token setup and public link/QR are local.
- Interactive one-command Linux installer enumerates suitable nginx HTTPS
  virtual hosts, adds `/smartstage`, or offers Caddy; validates and backs up
  proxy configuration and installs an unprivileged systemd service.
- Explicit Local LAN selection reserves stopped playback, persists the choice,
  restarts the native app and configures its scoped firewall rule. Gateway-mode
  installation/updates leave incoming rules unchanged.

- Compiled-in AVFoundation/AppKit/Core Audio backend on macOS and Media
  Foundation/EVR/Win32 backend on Windows; real native harness and media fixtures.
- Routed foreground audio/video playback with independent stage visibility and
  image cues, looping image/video backgrounds, optional background sound,
  bounded native audio crossfades, output enumeration, hotplug handling,
  emergency Escape and power assertions. The pointer hides over the stage.
- Saved background selection and session background cue buttons, hidden remote
  buttons, configurable fades (default duration one second, initially disabled),
  and an optional selected-music-button toggle that keeps the image displayed.
  STOP returns to the background; emergency Escape stops everything and hides
  the stage.
- Go coordinator with generations, stop epochs, latest-only native/load mailboxes,
  bounded request idempotency and SSE subscribers. Atomic per-user persistence,
  backup, corruption diagnostics and native process lock.
- Embedded responsive Admin/Command pages, native media selection/inspection,
  cue CRUD/labels/order/revisions, separate output controls and stage enablement.
- Localhost-only Admin with dedicated Mac and Windows app windows, or a
  system-browser launch for standalone Mac, and a local session;
  separate LAN remote listener with an eight-digit per-launch token in its URL,
  Admin QR code/address selection, session/CSRF/Origin/Host enforcement and
  command-role path redaction.
- Reproducible matching-OS builds, import audits, checksums, release automation,
  four portable ZIP downloads containing one executable each, and operating/API/
  architecture documentation. The Mac curl installer verifies and installs
  the icon-bearing app bundle and removes quarantine only from that app. The
  Windows irm installer detects native ARM64/AMD64, checks the archive, installs
  per user with Desktop/Start menu shortcuts, and checks Microsoft WebView2.
- Embedded Windows executable icons and optional Mac Finder apps with the same
  artwork; Finder starts the bundled executable without Terminal and opens local
  Admin. The Mac app has a visible Dock icon and standard application menu with
  Quit (Command-Q); its status menu also provides Admin, log access and graceful
  Quit. Windows uses the GUI subsystem, retained taskbar window, tray menu,
  original-file Explorer drops, native chooser, and graceful Quit. Its embedded
  Microsoft loader uses the separately serviced Evergreen WebView2 runtime.
- Automatic startup updates from the published GitHub releases, with matching
  architecture, checksum and native executable validation. Installation reserves
  playback, preserves the saved show, restarts without autoplay and rolls back
  failed startup. Periodic checks defer installation until the next launch;
  Admin also offers an update action while playback and stage output are stopped.

- Admin omits the Host files browser. Finder/Dock and Explorer drops, plus
  Choose Media, append original file references
  atomically without copying or autoplay, respecting configured media roots.
  Filesystem identity handles actual Unicode/case aliases without broadening roots.
- Remote connection settings start collapsed while the link, QR and status stay
  visible. Local LAN save guidance explains firewall access; public gateway
  remains the default with no firewall authorization. Browser-only hosts without
  a native chooser retain a compact, collapsed absolute-path import form.
- Compact remote Stage/Disconnect controls, Escape stop-and-close behavior in
  native app/stage and connected browser pages, and persisted per-cue colors.
- Remote cue buttons directly below the top bar, with ordinary command
  acknowledgements retained as live announcements and errors still visible.
- Authenticated Admin Quit requests graceful shutdown. The open page waits and
  reconnects after relaunch; fresh page presence suppresses another automatic
  browser launch, and a new host instance refreshes the interface once. Native
  Mac and Windows app actions instead restore one retained native Admin window. Suspended
  external browser tabs may not be detected.
- Bundled Mac Admin uses the system WKWebView and accepts native Finder file
  drops and chooser selections, preserving original file paths. Closing the
  window hides it without interrupting playback; Dock/Open Admin restores it.
  External-browser file drops offer platform-appropriate native chooser guidance.
- Windows Admin uses a separate COM STA and DirectComposition while Media
  Foundation keeps its existing MTA. The WebView has no privileged script bridge;
  normal loopback sessions, CSRF and SSE remain in use. Second launches reuse the
  same process/window; hidden windows retain drafts. Normal Quit cleans up the
  private WebView profile, including while a media dialog is open.
- Optional Screen Wake Lock with truthful status, opt-in and release/reacquire
  handling. The public HTTPS gateway enables this browser API; Local LAN HTTP cannot use it;
  device Auto-Lock/Screen timeout settings remain the alternative for that URL.

## Built and automatically tested

Preview 16 is source `918830774f1ddefebed2551a28bcb9e499d939f4`.
[Release run 35532965821](https://github.com/arizzi74/Smart-Stage/actions/runs/35532965821)
passed all 21 jobs: shared checks, gateway, four native builds, publication,
four public downloads/browser checks, both Mac installers and four actual
automatic updates. Independent public archive checks match the exact clean
source, architecture and installed executable hashes. Both Linux binaries and
daemon checks also pass.

Browser checks verify that Admin has no Host files section or directory-listing
requests, connection settings remain collapsed through polling, and the remote
link/QR and saved Local LAN firewall instructions remain accessible. Original
path imports, native chooser integration, same-window Mac reopening and saved
drafts still pass. Gateway-mode installers and updaters require no firewall-skip
override and leave LAN closed. Reports are in
[`docs/verification/release-preview16`](docs/verification/release-preview16/).

Historical preview 15 evidence follows.

Preview 15 is source `580cbb63db18e32b4946abe49c5363eb00e50cc2`.
[Candidate run 35530454441](https://github.com/arizzi74/Smart-Stage/actions/runs/35530454441)
passed shared race/vet, the Linux gateway and all four native/browser targets.
[Release run 35530818664](https://github.com/arizzi74/Smart-Stage/actions/runs/35530818664)
passed all 21 jobs, publishing 48 assets and verifying public downloads, real
browser controls, both Mac installers and actual updates on all four desktop
targets. Gateway-mode update checks use no firewall-skip override and leave LAN
closed. Independent download checks verify all
six desktop ZIPs against checksums, architecture and clean tagged source metadata;
both Linux binaries are static ELF files with matching architecture and checksums.
The actual Linux daemon starts, preserves its private configuration/token, rejects
untrusted plaintext requests, serves loopback health and shuts down on SIGTERM
on both architectures.

All four desktop targets verify default gateway startup with the LAN port closed,
explicit LAN selection through authenticated Admin, native restart with a new PID,
session rotation, saved show/ports/media preservation, remote access boundaries
and graceful Quit. Separate Windows tests create, read back and remove program-only
Private/LocalSubnet rules on both architectures without changing global settings.
They cover ordinary, Unicode/apostrophe/space and literal PowerShell-special paths;
expanding Windows short paths fixes the observed Error 87.

Actual nginx and Caddy HTTPS proxy tests verify outbound WebSocket registration,
streaming status, cancellation, STOP forwarding and private-route rejection.
The TLS browser check configures Admin, follows its remote link, pairs with scoped
Secure cookies, sends CSRF-protected PLAY/STOP, receives SSE and decodes the actual
Admin QR. Native playback and the device wake-lock grant are simulated in that
browser check. Real public DNS/ACME, privileged server installation, interactive
UAC and physical phone power behavior are not established by these automated tests.
Reports are in
[`docs/verification/release-preview15`](docs/verification/release-preview15/).
Both Macs also passed [default-install run 35531329120](https://github.com/arizzi74/Smart-Stage/actions/runs/35531329120)
at installer commit `61e0fc2`, using preview 15 without a version or firewall-skip
override. Public bootstrap, bundle and installed core hashes match. The additional
[default updater run 35531329116](https://github.com/arizzi74/Smart-Stage/actions/runs/35531329116)
passed both Macs and Windows ARM64; its Windows AMD64 attempt hit GitHub's anonymous
API rate limit before replacement. That failure remains recorded separately from
the successful tagged Windows AMD64 updater check.

Historical preview 14 evidence follows.

Preview 14 is source `49c9bf7051c2a5a70196dabc7e585690e7dee515`.
[Candidate run 35525447112](https://github.com/arizzi74/Smart-Stage/actions/runs/35525447112)
passed shared checks and all four native/browser targets.
[Release run 35525752618](https://github.com/arizzi74/Smart-Stage/actions/runs/35525752618)
passed all 20 jobs: native builds, publication, public downloads/browser checks,
both Mac installers and actual automatic updates on all four targets. Mac scene
probes measured native AVPlayer gains, crossfades, looping, timeline preservation,
image layers and emergency cleanup. Both Mac browser tests kept music selected
through images and stage toggles, then stopped it with the selected-button option.
Windows probes verified real image pixels, looping, stage/timeline independence,
cursor handling and hard stop; their runners lack audio endpoints, so Windows
audio crossfades remain unverified at runtime. All six public ZIPs match the exact
clean source and architecture. Reports are in
[`docs/verification/release-preview14`](docs/verification/release-preview14/).
Both Macs also passed [default-install run 35526039651](https://github.com/arizzi74/Smart-Stage/actions/runs/35526039651)
without a version override, matching the public archive and installer hashes.
Physical speaker/projector and pointer checks still need the event Mac; long-session
acceptance of the new mixer has not been established.

Historical preview 13 evidence follows.

Preview 13 is source `824698e90b360822f713840df60ae914767e21df`.
[Candidate run 35521516191](https://github.com/arizzi74/Smart-Stage/actions/runs/35521516191)
passed shared checks and all four native/browser targets.
[Release run 35521992281](https://github.com/arizzi74/Smart-Stage/actions/runs/35521992281)
passed all 20 jobs: native builds, publication, public downloads/browser checks,
both Mac installers and actual automatic updates on all four targets. Both Macs
verify the real WKWebView Admin session/CSRF/live controls, original-file native
drop queue, navigation boundaries, retained close/reopen state and graceful Quit.
Packaged apps retain one observed window ID across Dock reopen without opening
Terminal or an external Admin browser. All six public ZIPs match the exact clean
source and architecture. Evidence is in
[`docs/verification/release-preview13`](docs/verification/release-preview13/).
Both default Mac installs passed
[run 35522396312](https://github.com/arizzi74/Smart-Stage/actions/runs/35522396312),
with matching public installer and installed-core hashes. Physical Finder
gestures and speaker/projector acceptance remain unverified.

Historical preview 12 evidence follows.

Preview 12 is source `ea9245c2b1a4d095d02f87faaa879cf434bdea77`.
[Candidate run 35519125516](https://github.com/arizzi74/Smart-Stage/actions/runs/35519125516)
passed shared checks and all four native/browser targets.
[Release run 35519418601](https://github.com/arizzi74/Smart-Stage/actions/runs/35519418601)
published all six architecture-specific ZIPs after native checks. Its public
download/browser jobs passed on all four targets, and both Mac installs passed.
One Apple Silicon updater job hit GitHub's anonymous rate limit; the separate
default updater run below passed on all four targets. Admin Quit
stopped native video, exited cleanly and released both listeners; the same Admin
tab then reconnected across relaunch with exactly one reload and no additional OS
browser launch. Both Macs tested chooser reuse, cancellation and unattended Quit.
[Default update run 35519683976](https://github.com/arizzi74/Smart-Stage/actions/runs/35519683976)
installed the actual published bytes on all four targets. Archive/source/hash
checks, both default Mac installs and synthetic browser evidence are in
[`docs/verification/release-preview12`](docs/verification/release-preview12/).

Historical preview 11 evidence follows.

Preview 11 is source `9e7919547038372adaa8b7a3b028817928df62a0`.
[Candidate run 35515364477](https://github.com/arizzi74/Smart-Stage/actions/runs/35515364477)
passed shared checks and all four native/browser targets.
[Tagged run 35515606727](https://github.com/arizzi74/Smart-Stage/actions/runs/35515606727)
passed all 20 jobs, including publication, four public downloads/browser checks,
both Mac installers and all four actual automatic updates. Both Macs accepted
native file-open events into running/closed apps with original paths and saved
playlist preservation. Native stage/app Escape events disabled and hid the stage
on all four platforms. Browser checks covered hidden files, cue colors, Stage
controls, browser Escape and truthful Needs HTTPS status on the LAN remote.
Independent archive checks and runtime hashes agree on the exact published bytes.
Details and physical-test limitations are in [release verification](docs/release-verification.md).
The default Mac installer changed to preview 11 at `b6556cd`; both architectures
passed [run 35515883852](https://github.com/arizzi74/Smart-Stage/actions/runs/35515883852)
without an override. The public bootstrap exactly matches the verified installer.
Updated standalone verification defaults passed automatic updates on all four
targets in [run 35515883876](https://github.com/arizzi74/Smart-Stage/actions/runs/35515883876).
The current installer/verification commit also passed the full four-target
native/browser run [35515883967](https://github.com/arizzi74/Smart-Stage/actions/runs/35515883967).

Preview 10 is source `3c8492f036840f64612cef12045bd718a96c9d81`.
[Native/browser run 35512154212](https://github.com/arizzi74/Smart-Stage/actions/runs/35512154212)
passed shared race/vet and all four native targets. The
[tagged run](https://github.com/arizzi74/Smart-Stage/actions/runs/35512155124)
passed its 16 build, publication, public-download, browser and Mac-installer
jobs, publishing 36 assets. Its four initial updater verification failures are
retained separately and explained in [release verification](docs/release-verification.md).
After correcting the verification scripts, all four targets passed actual
automatic discovery, download, replacement and restart against the same
published binaries in
[run 35512568301](https://github.com/arizzi74/Smart-Stage/actions/runs/35512568301).
The checks observed exact published bytes, new process/instance identities,
preserved shows/media/ports, and no autoplay. Both Mac updates also retained
their registered icon and Dock identity without opening Terminal. All six
independently downloaded ZIPs match checksums, architecture and clean source.
Native subprocess checks cover failed-startup and wrong-runtime-version rollback.
The default Mac installer changed to preview 10 at `454324a`; both architectures
passed [run 35512624089](https://github.com/arizzi74/Smart-Stage/actions/runs/35512624089)
without a version override. The unversioned public bootstrap was checked
separately and matched the verified installer bytes. Earlier previews need one
manual upgrade; subsequent launches update automatically.

Preview 8 is source `00f0c659dc01fc94432b4394e703e6b68f2a53b0`.
[Native/browser run 35508597483](https://github.com/arizzi74/Smart-Stage/actions/runs/35508597483)
passed shared checks and all four target builds. On both Macs an independent
AppKit observer found the running core registered with the expected bundle
identity, regular activation policy and matching icon artwork. The rendered
128-pixel RGBA images had zero mean channel difference. Terminal-free launch,
log redirection, same-process Admin reopen and the standard Quit event also
passed; Quit returned exit code 0 and closed both HTTP listeners. These checks
establish Dock eligibility and matching registered artwork, without claiming a
Dock screenshot, menu click or keyboard-shortcut test.
[Tagged run 35508890135](https://github.com/arizzi74/Smart-Stage/actions/runs/35508890135)
passed all 16 jobs: shared checks, four native builds, publication of 36 assets,
four fresh ZIP downloads/startups, four published browser checks and both Mac
installers. All six independently downloaded ZIPs match their checksums,
architectures and clean source metadata. After the installer default changed
to preview 8 at `1c42f55`, both Mac architectures passed
[default-install run 35509117225](https://github.com/arizzi74/Smart-Stage/actions/runs/35509117225).
The unversioned public installer bytes also match the checked source and select
preview 8. Recorded evidence is in
[release verification](docs/release-verification.md).

Preview 7 is source `7ca8c0685ed237849406fbf2371d44a9c500ce8d`. The Mac app
runs without Terminal, saves logs and provides Admin/Quit in the menu bar.
All four release builds and native application/icon checks passed in
[run 35503933513](https://github.com/arizzi74/Smart-Stage/actions/runs/35503933513),
which published the release. All six independently downloaded ZIPs match their
checksums, architectures and clean source metadata. Native Mac checks observed
no controlling terminal, log redirection, same-process reopen and graceful Quit.
The entire tagged workflow passed, including all four published-browser and
fresh-download checks plus both Mac installers. Default preview 7 installation
passed on both Macs in [run 35504365951](https://github.com/arizzi74/Smart-Stage/actions/runs/35504365951).
The installer also repairs Smart Stage's blocked app-firewall rule with
administrator authorization; native rule/cancellation/preservation checks passed
on both Mac architectures. Details and test limits are in
[release verification](docs/release-verification.md).

The Mac app installer passed on Apple Silicon and Intel in
[run 35501797274](https://github.com/arizzi74/Smart-Stage/actions/runs/35501797274).
It downloads the existing preview 6 bundles, verifies them, clears only the app's
quarantine, preserves configuration and unrelated attributes, safely restores
failed/interrupted replacements, and launches the installed app with local Admin.
The [native reports](docs/verification/macos-app-install/) distinguish these
hosted tests from physical and clean-machine acceptance.

All four: macOS ARM64/AMD64, Windows ARM64/AMD64. Windows ARM64 was added by
explicit user request. Preview 6 is source `4aa3529`; its localhost Admin,
automatic browser launch, numeric QR/link pairing, isolated remote listener,
single-executable ZIPs and native playback controls passed on all four targets in
[native/browser run 35500115900](https://github.com/arizzi74/Smart-Stage/actions/runs/35500115900).
[Tagged run 35500115581](https://github.com/arizzi74/Smart-Stage/actions/runs/35500115581)
published the verified release and passed fresh ZIP downloads/startup on all
four target OS/architectures. Browser checks against those published ZIPs also
passed on all four targets, and the complete tagged workflow succeeded.
Independent archive checks confirm one executable,
permissions, checksums, native architecture and clean source metadata. QR decode,
session isolation and spoofed-header denial tests passed. Physical camera,
phone/LAN and event-output acceptance remain open.

Preview 5 is source `9e72b4c`; its icon and packaging
checks passed on all four targets in
[native run 35492226235](https://github.com/arizzi74/Smart-Stage/actions/runs/35492226235).
Windows Shell extracted both icon sizes, and every embedded image matched the
source. Both Mac apps passed signature/icon decoding, binary identity and real
Finder-to-Terminal startup checks from paths with spaces, quotes and Unicode.
The playback implementation is unchanged from preview 4. See
[release verification](docs/release-verification.md) for publication evidence.
The tagged release workflow, fresh-download checks, default curl/irm installers
(including Windows PowerShell 5.1) and browser checks also passed on all four
targets. Those installer results describe preview 5; current distribution uses ZIPs.

Preview 4 is source `505a1e4`.
[Tag run 35479742963](https://github.com/arizzi74/Smart-Stage/actions/runs/35479742963)
passed shared race/vet, all four native build/import/media/HTTP checks,
publication, curl/irm installation and real Admin/Command browser checks on all
four targets. The downloaded files passed SHA-256, architecture, Go/module
version and source-commit checks. After switching the default to preview 4,
[installer run 35480086360](https://github.com/arizzi74/Smart-Stage/actions/runs/35480086360)
and [browser run 35480086365](https://github.com/arizzi74/Smart-Stage/actions/runs/35480086365)
also passed on all four targets without a version override.
The later [installer run 35484067090](https://github.com/arizzi74/Smart-Stage/actions/runs/35484067090)
also verified the built-in Windows PowerShell 5.1 on AMD64 and ARM64: default
per-user installation, correct native binary/version, Command-page startup,
reinstallation with the same checksum and a single user PATH entry all passed.
This does not replace clean-machine testing.

Preview 4 releases the Mac video layer with each cue while retaining the black
stage window. [Memory evidence](docs/soak-results.md) supports the fix for
caption-timer/timebase retention. The exact-source two-hour run completed all
four workloads and restart checks, with 121 samples per target. Mac fitted RSS
trends after minute 15 fell from +8.76/+7.72 to +0.34/+1.08 MiB/hour on
AMD64/ARM64. [Virtual pixel evidence](docs/native-display-results.md) and its
Windows ARM64 limitation are recorded separately.

Earlier preview 3 evidence is retained: tagged workflow
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
connection cap per listener, and a save-time configuration size limit. Windows STOP now uses
per-stream channel volume instead of shared session mute; this is implemented
and compiled, but Windows audio playback remains unverified on real hardware.

- Mac CI: all four fixture cues played/stopped through actual AVFoundation, on
  virtual/null audio devices and one virtual display; saved-show restart passed.
- Windows release CI: all four formats natively inspected; silent video
  played/stopped on a Hyper-V display; restart passed. Stock runners have
  **no audio render endpoints**.
  [Capability checks](https://github.com/arizzi74/Smart-Stage/actions/runs/35473613211)
  confirmed running Windows audio services but no installed sound devices on
  either runner. They tested the published preview without changing OS settings.
- A separate [Windows virtual-audio evaluation](docs/windows-audio-results.md)
  installed a signed vendor driver on disposable runners and passed actual
  native audio/video playback/STOP for all four cues on AMD64 and ARM64, plus
  120 seconds of transitions and restart. A later diagnostic observed signal,
  STOP silence and replay across six audio cycles per architecture. Its strict
  isolation check still failed: both endpoints expose driver-provided meters
  with matching readings, while the application session was observed on the
  selected endpoint. Physical sound and exclusive isolation are not claimed.
- [Captured native pixels](docs/native-display-results.md) at source `505a1e4`
  show moving/restarted video and STOP/end blackout on both Macs and Windows
  AMD64. Mac captures retain a small OS indicator. Windows ARM64 captures show
  first-run Windows setup, so its visual test failed. No physical projector,
  sound or latency claim follows from these virtual-desktop checks.
- Unavailable audio/display IDs cause native errors without a playing event on
  all four targets. Detailed harness records are attached to preview 4.
- Linux ARM64: `go test -race ./...`, `go vet ./...`, Chromium browser checks.
  Shared tests also passed on the four target OS runners. Browser checks cover
  widths 320/390/768/844/1280, STOP usability, escaping and reconnect semantics.

## Still open

The user reported that preview 7 now works after the firewall change, followed
by the missing-icon complaint addressed in preview 8. This is a positive
operational report; the structured phone, audio and display test matrix remains
incomplete. The [Mac physical procedure](docs/macos-physical-test.md) now targets
preview 11 on the available Apple Silicon Mac with external audio and a second
monitor/projector.

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
[35478342253](https://github.com/arizzi74/Smart-Stage/actions/runs/35478342253)
completed all four workloads and restart checks at `505a1e4`, with substantially
flatter Mac RSS traces. Windows RSS and handle trends remained positive in
this workload; the raw records and fitted statistics are retained.
Windows per-type diagnostics showed substantial event/thread/I/O
handle cleanup during two stopped idle minutes, with file counts unchanged;
no Windows change was justified by these observations. These measurements
support the Mac renderer fix but do not prove stability for every workload.
None verifies physical A/V drift or independent physical audio routing.

See `docs/release-verification.md` for exact environments/evidence and the
remaining checklist, and `docs/acceptance-audit.md` for a specification-wide
evidence audit. Local final release files are downloaded under
`dist/releases/v0.1.0-preview.8/`; local cross-build outputs are under `dist/`.
