# Release verification

Status: **preview; physical/clean-machine acceptance incomplete**. CI executes
real native APIs but cannot verify what a human sees or hears on event hardware.

## Distribution and control access by release

Preview 6 changes the primary downloads to four ZIPs, each containing exactly
one executable. Launch opens local Admin in the system browser. Admin binds
only to `127.0.0.1:8787`; the separate remote-control listener uses port `8788`.
Admin displays remote URLs and a QR code containing the per-launch numeric token.
The original standalone-binary installers were retired in preview 6. A later
Mac-only installer uses the existing preview 6 `.app.zip` assets, installs the
app with its icon under `~/Applications`, and removes quarantine only from that
app before launch. Preview 7 adds a Mac app that runs without Terminal, with
menu bar controls for Admin, logs and Quit. The Mac installer now also requests
administrator authorization to allow the installed executable through the app
firewall, and repairs an existing blocked bundle rule. Global firewall settings
and other applications' rules are preserved. Historical release and installer
results below remain evidence for their named older versions.

Preview 8 changes the Finder app to a regular Mac application with a Dock icon
and standard application menu, including Quit Smart Stage (Command-Q). The
status menu remains available, and clicking the Dock icon reopens local Admin.
Launch still uses one core process without Terminal and sends output to
`~/Library/Logs/Smart Stage/smartstage.log`. Direct command-line execution keeps
its existing lifecycle.

Preview 10 adds automatic startup updates, verified downloads and native
replacement/restart with rollback on failed startup. Playback is reserved during
installation. Checks during an existing session leave installation for the next
launch, and saved shows remain outside the replaced application. Restart begins
stopped with stage output disabled. Admin shows update status and an optional
update action; existing releases need one manual upgrade to acquire the updater.

Preview 11 adds compact hidden-file browsing, original-path Finder/Dock imports,
per-cue colors, compact remote Stage/Disconnect controls and Escape stage-off.
Mac media-root checks compare filesystem identity when actual Unicode/case aliases
use different spellings. Wake Lock reports its real state when supported; the
ordinary HTTP LAN page reports Needs HTTPS and does not promise to prevent sleep.

Preview 12 removes the remote heading/ordinary acknowledgement block and adds
Admin Quit, authenticated page presence and native Mac file selection from Admin.
Quit flushes its acknowledgement before graceful cleanup. Existing Admin tabs
reconnect and refresh their assets after a new host instance; startup waits up to
six seconds before dispatching a browser URL. Native Dock/menu/import actions use
the same policy. A fully suspended browser tab may not be detected, and exact-tab
selection is not guaranteed. Older already-open pages need one manual refresh.

Preview 13 gives the Mac app one retained AppKit Admin window containing the
system WKWebView. Dock/menu actions restore that window; closing it hides the
interface while playback continues. Native Finder drops use the specific drag
session's file URLs and the existing original-path validation/save flow. The
web view loads real loopback Admin assets and keeps session/Origin/CSRF checks,
with no privileged JavaScript filesystem bridge. Standalone Mac and Windows
executables retain the external-browser interface.

Preview 14 separates foreground sound, image presentation and stage visibility.
Admin saves a default image/looping-video background; background cue buttons change
it for the session. Background audio is optional, music can remain selected under
an image, and a selected-music toggle stops only music. Optional configurable fades
crossfade outgoing sound or fade it to silence; silence-to-play starts immediately.
STOP returns to the background. Escape/Quit stop all native sources and close the
stage. The stage uses a transparent cursor on Mac and hidden cursor on Windows.

Preview 15 defaults to public gateway mode with the direct LAN listener closed,
including before gateway setup and during disconnection. Admin stays loopback-only.
The local Admin registers over authenticated outbound WSS and displays a separate
HTTPS phone URL and QR code. Reconnection rotates the endpoint and pairing secret.
Only remote-control routes traverse the relay; Admin, host files and media remain
local. The Linux daemon has static AMD64/ARM64 builds and an interactive curl
installer that lists eligible nginx HTTPS hosts or offers Caddy.

Gateway-mode installs and updates do not configure incoming firewall rules.
Explicitly choosing Local LAN saves the choice and restarts the desktop app to
configure its scoped rule. The older preview 14 updater can still request its
previous firewall approval during the first upgrade. HTTPS permits the existing
optional Keep awake feature in supported browsers while the page remains visible;
the operating system can release the lock.

Preview 16 removes the Host files panel, navigation entry and background folder
listing requests. The Mac app retains native Finder/Dock imports and Choose Media;
browser-only hosts without a native chooser have a compact absolute-path import
form. Connection settings are collapsed on a fresh Admin page, with status,
errors, the remote link and QR code outside the disclosure. Polling and reconnect
preserve its state. Saved Local LAN mode shows firewall instructions outside the
collapsed settings after restart. Gateway-mode installer/updater behavior remains
unchanged: no firewall authorization or open LAN listener by default.

The native release workflow verifies ZIP contents/permissions and runs the
extracted binary before publication. Startup must serve local Admin and report
that the operating system accepted the automatic browser launch. Default gateway
startup must leave the LAN port closed; an explicit LAN restart must preserve the
show and reject private routes on the remote listener. Published-archive checksum/extraction/startup
and native browser checks run for the explicit new release tag. These checks do
not establish physical output routing or clean-machine acceptance.

## Recorded evidence

[Preview 16](https://github.com/arizzi74/Smart-Stage/releases/tag/v0.1.0-preview.16)
is source `918830774f1ddefebed2551a28bcb9e499d939f4`.
[Tagged run 35532965821](https://github.com/arizzi74/Smart-Stage/actions/runs/35532965821)
passed **all 21 jobs**, publishing 48 assets and verifying public downloads,
real browser controls, both Mac installers and actual automatic updates on all
four desktop targets. All six independently downloaded ZIPs match checksums,
architecture and clean source metadata; native, download, installer and update
hashes agree. Both static Linux binaries and actual daemon startup checks pass.

Admin UI checks establish removal of the Host files panel/nav and background
folder requests, initially collapsed connection settings, preserved disclosure
state through polling, visible link/QR and saved-mode LAN firewall guidance.
The original-path fallback adds real files without copying on browser-only hosts;
Mac scenarios with an available native chooser seed playback fixtures through
the authenticated API and record that limitation. Separate native Mac probes
retain real WebKit authentication, file-URL drops, chooser lifecycle, close/reopen
and unsaved draft preservation. The real TLS gateway test also decodes the actual
Admin QR while connection settings remain collapsed.

The default Mac installer changes to preview 16 only after those public checks.
Gateway-mode installation and actual update tests use no firewall-skip override
and leave incoming LAN access closed. Physical Finder gestures, speaker/projector
output and phone power behavior remain outside automated acceptance.
Reports are retained in
[`verification/release-preview16`](verification/release-preview16/).

Historical preview 15 evidence follows.

[Preview 15](https://github.com/arizzi74/Smart-Stage/releases/tag/v0.1.0-preview.15)
is source `580cbb63db18e32b4946abe49c5363eb00e50cc2`.
[Candidate run 35530454441](https://github.com/arizzi74/Smart-Stage/actions/runs/35530454441)
passed shared race/vet, the Linux gateway and all four native/browser targets.
[Tagged run 35530818664](https://github.com/arizzi74/Smart-Stage/actions/runs/35530818664)
passed **all 21 jobs**, publishing 48 assets and verifying public downloads,
real browser controls, both Mac installers and actual automatic updates on all
four desktop targets. Gateway-mode updater checks use no firewall-skip override
and leave the LAN listener closed. All six independently downloaded desktop
ZIPs match their checksums, architecture and clean source metadata; Mac app cores
match standalone cores. Both Linux gateway binaries have matching checksums,
architecture and `CGO_ENABLED=0`, with no ELF interpreter or linked shared
libraries. Actual AMD64 and ARM64 daemon processes verify private configuration,
token preservation, loopback health, plaintext rejection and clean SIGTERM exit.
The published gateway installer exactly matches the source bytes.

Four native network-mode reports exercise default gateway startup with no LAN
listener, authenticated Local LAN selection, a native restart to a different PID,
rotated Admin sessions, preserved show/media/ports, LAN pairing, private-route
rejection and graceful Quit. Those restart checks explicitly bypass OS elevation;
separate [Windows firewall run 35530454269](https://github.com/arizzi74/Smart-Stage/actions/runs/35530454269)
creates, reads back and removes program-only Private/LocalSubnet rules on both
architectures while preserving global settings. Actual Windows short-path
expansion resolves Error 87. Ordinary filenames, Unicode/apostrophe/spaces and a
literal `$(...)` filename all pass. Mac installer checks separately exercise scoped
firewall repair with a test elevation adapter. Interactive authorization dialogs
and physical LAN traffic are not established by those tests.

The gateway job tests real nginx/Caddy TLS proxies with outbound WSS registration,
streaming status, request cancellation, STOP forwarding and private-route denial.
Caddy's negative `flush_interval` override was removed after the unchanged
cancellation test exposed its behavior on Ubuntu Caddy 2.6.2; both that version
and Caddy 2.11.4 then passed. A real TLS browser configures Admin, opens the remote
link, pairs with an endpoint-scoped Secure cookie, sends CSRF-protected PLAY/STOP,
receives authoritative SSE and decodes the real Admin QR PNG. That browser test
uses a simulated native backend and device wake-lock grant; it does not establish
physical phone power management. Public DNS/ACME and privileged installation on
an end-user server were not exercised.

The default Mac installer changed to preview 15 at `61e0fc2` after public archive
and tagged installer verification. The public bootstrap fetched at 19:09:19 UTC
on 20 September 2026 matches the repository and SHA-256
`55a6e49b1b90a74586ce9e39c967a0ba775aeb408eb4f75e1b0964a8b6abe035`.
Both Macs passed [default-install run 35531329120](https://github.com/arizzi74/Smart-Stage/actions/runs/35531329120)
without a version or firewall-skip override. They leave firewall settings/rules
unchanged, keep LAN closed, preserve the dedicated window and match public
archive, core and bootstrap hashes.

An additional default-pinned updater run,
[35531329116](https://github.com/arizzi74/Smart-Stage/actions/runs/35531329116),
passed on both Macs and Windows ARM64 without a firewall-skip override. Its
Windows AMD64 runner hit GitHub's anonymous API rate limit before replacement;
the reported retry time was 19:24:31 UTC. The tagged release's Windows AMD64
update had already installed the same public bytes successfully. The additional
failure is retained and is not counted as a pass. Reports and provenance are in
[`verification/release-preview15`](verification/release-preview15/).

Historical preview 14 evidence follows.

[Preview 14](https://github.com/arizzi74/Smart-Stage/releases/tag/v0.1.0-preview.14)
is source `49c9bf7051c2a5a70196dabc7e585690e7dee515`.
[Candidate run 35525447112](https://github.com/arizzi74/Smart-Stage/actions/runs/35525447112)
passed shared race/vet and all four native/browser targets. The exact-source
[tagged run 35525752618](https://github.com/arizzi74/Smart-Stage/actions/runs/35525752618)
passed **all 20 jobs**, publishing 36 assets and verifying public downloads,
real browser controls, both Mac installers and actual automatic updates on all
four targets. All six independently downloaded ZIPs match their checksums,
architecture and clean source metadata; app cores match standalone cores.

Both Mac native scene probes observe actual AVPlayer volumes and overlapping
crossfades between background/foreground cues, between two cues, and on STOP.
They also check immediate starts from silence, fades to silence, background
looping, decoder retirement, music timelines through stage toggles, image layers,
transparent cursor configuration, natural completion and rapid PLAY/hard-STOP
cancellation. Both Mac browser tests use real native audio endpoints to preserve
selected music through images/stage toggles, stop music while keeping the image,
return to background, and persist hidden/background/fade/toggle preferences.
Both Windows probes verify looping video, image pixels, stable preparation,
independent stage/timeline behavior, cursor handling and hard stop. Windows CI
has no audio endpoint; audio fade and music integration fields explicitly remain
unverified there. These checks do not establish physical sound/pointer/projector
behavior or long-session acceptance of the new mixer.

The default installer changed to preview 14 at `f88525d` after public archive
verification. The public bootstrap fetched at 17:30:15 UTC on 20 September 2026
matches the repository and SHA-256
`6376448b035656b863237bda58c9f545352567d67d7ef76467d12e546d8308bb`.
Both Macs passed [default-install run 35526039651](https://github.com/arizzi74/Smart-Stage/actions/runs/35526039651)
without a version override, with matching installed core, archive and public
installer hashes.
Reports are retained in
[`verification/release-preview14`](verification/release-preview14/).

An additional default-pinned updater rerun,
[35526039627](https://github.com/arizzi74/Smart-Stage/actions/runs/35526039627),
passed on Intel Mac and both Windows architectures. Its Apple Silicon runner hit
GitHub's anonymous API rate limit before discovering the update; GitHub supplied
17:38:21 UTC as the retry time. The tagged release's Apple Silicon update had
already installed the same public bytes successfully. The failed rerun is retained
in [`additional-update-darwin-arm64-rate-limit.json`](verification/release-preview14/additional-update-darwin-arm64-rate-limit.json); it is not counted as a pass.

Historical preview 13 evidence follows.

[Preview 13](https://github.com/arizzi74/Smart-Stage/releases/tag/v0.1.0-preview.13)
is source `824698e90b360822f713840df60ae914767e21df`.
[Candidate run 35521516191](https://github.com/arizzi74/Smart-Stage/actions/runs/35521516191)
passed shared race/vet and all four native/browser targets. The exact-source
[tagged run 35521992281](https://github.com/arizzi74/Smart-Stage/actions/runs/35521992281)
passed **all 20 jobs**, publishing 36 assets and verifying public downloads,
real browser controls, both Mac installers and actual automatic updates on all
four targets. All six independently downloaded ZIPs match their checksums,
architecture and clean source metadata; Mac app cores match standalone cores.

Both Macs load the actual loopback Admin assets in the production WKWebView
bridge, establish the local session and CSRF token, connect authenticated
EventSource, send STOP, observe live state and use Admin Quit to stop the host.
Closing and reopening keeps the same window/WebKit object and unsaved interface
state. Native shutdown closes the window. Public Cocoa hit testing targets the
Admin web view; a native file-URL pasteboard reaches the bounded original-path
queue, preserving Unicode paths and file bytes. Mixed text cannot supply a file
path, and navigation outside Admin is rejected. Separate packaged-app checks
observe the real core PID, Dock icon and visible WindowServer ID across reopen,
with no external Admin browser dispatch or Terminal. LaunchServices imports
exercise Go validation, playlist persistence and unattended chooser/error Quit.
These are automated native/API observations, not physical Finder mouse gestures,
clipboard use by a human, speaker/projector output or clean-machine acceptance.

The default installer changed to preview 13 at
`d2a0926b` after the public archives were verified. The public bootstrap fetched
at 16:20:55 UTC on 20 September 2026 matches the repository and SHA-256
`c170764799c0e02591983cac09f88bb78deb77d844a56c24cbe245a8b0c57db6`.
Both Macs passed installation without a version override in
[default-install run 35522396312](https://github.com/arizzi74/Smart-Stage/actions/runs/35522396312),
including the dedicated window and same-window Dock reopen. Installer and
installed core hashes match the public bootstrap and archive evidence.
Reports are retained in
[`verification/release-preview13`](verification/release-preview13/).

Historical release evidence follows.

[Preview 12](https://github.com/arizzi74/Smart-Stage/releases/tag/v0.1.0-preview.12)
is source `ea9245c2b1a4d095d02f87faaa879cf434bdea77`.
[Candidate run 35519125516](https://github.com/arizzi74/Smart-Stage/actions/runs/35519125516)
passed shared race/vet and all four native/browser targets. The
[tagged run 35519418601](https://github.com/arizzi74/Smart-Stage/actions/runs/35519418601)
published 36 assets after all four native build, startup, import, icon and
platform-specific checks passed. All four published-download/startup jobs, all
four published-browser jobs and both Mac installers passed. The run finished
19 of 20 jobs successfully; the remaining Apple Silicon updater job hit GitHub's
anonymous rate limit before downloading an update. The separate four-target
updater run below installed the same release successfully.

Real browser checks quit during native video playback, observe exit code zero
and closed Admin/remote ports, then relaunch on the same ports with that Admin tab
still open. The tab reconnects, reloads its interface exactly once, and the host
suppresses another OS browser dispatch. The remote no longer renders its heading
block. Browser checks cover widths 320–1280, hidden success notices, visible
errors, native picker availability changing after initial page load, and quiet
closed-page reconnection. Shared tests verify Admin/Origin/CSRF enforcement,
flushing before exactly-once Quit, multi-tab presence expiry, launch coalescing,
and cancellation/deferment during shutdown and updates.

Both Macs exercise real native chooser creation, repeated request reuse, cancel,
reopen and cleanup on Quit. Bundled-app tests call the authenticated Admin chooser
API and quit with its panel unattended. Existing LaunchServices import tests
still preserve original paths and saved cues without copying or autoplay. These
checks do not claim a physical Finder gesture or file selection by a human.

All six public ZIPs independently pass checksum, architecture, clean tagged
source metadata and matching Mac bundle/core checks. The default updater
verification targets preview 12 at `5a7c7d70827d98367ecc4de724a0890e8c0ceeb7`;
[run 35519683976](https://github.com/arizzi74/Smart-Stage/actions/runs/35519683976)
passed actual public discovery/download/replacement/restart on all four targets.
The installed executable hashes match the independent public downloads. The
public unversioned installer fetched at 15:28:56 UTC on 20 September 2026 defaults
to preview 12 and matches SHA-256
`efd3f6fe1f7f51615d3a9e6ef52121e9f77439edcc7c6298e01fcd434c168153`.
Both Macs passed installation without a version override in
[default-install run 35519683947](https://github.com/arizzi74/Smart-Stage/actions/runs/35519683947).
Its observed installer hash and installed core hashes match the public bootstrap
and independent archive checks. The installer/verification-default commit also
passed shared checks and all four native/browser targets in
[run 35519684094](https://github.com/arizzi74/Smart-Stage/actions/runs/35519684094).
Reports are retained in
[`verification/release-preview12`](verification/release-preview12/).

An earlier standalone updater run against unchanged preview 11 hit GitHub's
anonymous rate limit on one hosted Apple Silicon runner. Its three other targets
passed; the preview 12 default updater run above passed all four. This was a
network-service limit, not evidence that preview 12 installed incorrectly.

[Preview 11](https://github.com/arizzi74/Smart-Stage/releases/tag/v0.1.0-preview.11)
is source `9e7919547038372adaa8b7a3b028817928df62a0`.
[Candidate native/browser run 35515364477](https://github.com/arizzi74/Smart-Stage/actions/runs/35515364477)
passed shared race/vet and all four native targets. The
[tagged release run 35515606727](https://github.com/arizzi74/Smart-Stage/actions/runs/35515606727)
passed **all 20 jobs**: shared checks, four native builds, publication of 36 assets,
four public downloads/startups, four real-browser checks, both Mac installers and
four actual automatic discovery/download/replacement/restart checks.

Both Macs received real LaunchServices file-open events while running and at
startup, retained original Unicode media paths and existing cue identities,
saved imports across restart, and quit with an unattended import error. No media
was copied and no playback started. Filesystem-identity regression checks cover
actual Unicode/case aliases and retain root/symlink restrictions. Test-only native
event drivers linked to the production bridges dispatched Cocoa/Win32 Escape
from both app and stage targets on all four architectures, observing disabled
stage state and hidden windows. These are native event tests, not physical Dock
mouse gestures, file-picker selections or keyboard delivery tests.

Browser checks against the published binaries verify hidden-file default/toggle,
color persistence and readable remote colors under production CSP, remote Stage
off during actual silent-video playback, Stage on and browser Escape. The real
non-loopback HTTP remote loads the embedded wake-lock code but correctly disables
the option and displays Needs HTTPS. Deterministic browser-API tests cover request,
denial, background release/reacquisition, logout and pending-request races; no
physical phone screen-sleep prevention on HTTP is claimed.

Independent checks of all six ZIPs confirm SHA-256, architecture, one executable
per standalone archive, exact tagged source, clean build metadata and matching
Mac app/standalone core bytes. The downloaded executable hashes agree with the
native icon, installer and actual self-update records. The updater checks retain
saved shows/media/ports, start stopped with stage disabled, and validate refreshed
Mac Dock/icon identity. Interactive update firewall authorization is explicitly
skipped in these CI checks; installer checks separately exercise scoped firewall
rules. Reports are retained in
[`verification/release-preview11`](verification/release-preview11/).

The installer default changed to preview 11 at
`b6556cd204a133b08d04841e1c40c798b9a3a9d3`. Both Macs passed without a version
override in [default-install run 35515883852](https://github.com/arizzi74/Smart-Stage/actions/runs/35515883852).
The public unversioned `main/install.sh` was fetched on 20 September 2026 at
14:15:48 UTC and exactly matched the tested installer SHA-256
`0feaccd7cce0c34dd67610b67985f80ec4ebfee95055e26c7f0457bffb8f6ed0`.
Updated standalone verification defaults also passed real updates on all four
targets in [run 35515883876](https://github.com/arizzi74/Smart-Stage/actions/runs/35515883876).
Local synthetic Chromium checks additionally cover actual mouse drag from Host
files, rejection/guidance for external Finder payloads, hidden-list response
races, compact row sizing and widths 320–1280; their report explicitly excludes
native/physical playback assertions.
The current installer/verification commit also passed shared checks and all four
native/browser targets in [run 35515883967](https://github.com/arizzi74/Smart-Stage/actions/runs/35515883967).
A separate [Mac memory profile run 35514949325](https://github.com/arizzi74/Smart-Stage/actions/runs/35514949325)
passed 1,200-second transition workloads and idle diagnostics on both Macs. That
profile used candidate source `9332bc586cd68278b5fc85159dc1413aacd0dff7`, with the
same native bridge as preview 11, before the shared filesystem-identity fix; it
does not replace exact-release or physical acceptance checks.

[Preview 10](https://github.com/arizzi74/Smart-Stage/releases/tag/v0.1.0-preview.10)
is source `3c8492f036840f64612cef12045bd718a96c9d81`.
[Native/browser run 35512154212](https://github.com/arizzi74/Smart-Stage/actions/runs/35512154212)
passed shared race/vet and all four native build, application, icon and browser
checks. [Tagged run 35512155124](https://github.com/arizzi74/Smart-Stage/actions/runs/35512155124)
passed 16 jobs covering those builds, publication of 36 assets, four fresh ZIP
downloads/startups, four real browser/native checks and both Mac installers.
Its four first automatic-update verification jobs failed: both Windows apps
updated correctly but Unicode/short-path cleanup in the test missed the new
process, Intel Mac updated correctly but the test compared Unicode path spelling
instead of filesystem identity, and Apple Silicon reported an early update
error whose message the original verifier did not retain. These original
results remain under `verification/release-preview10/initial-auto-update/`.

The corrected verifier at `c0ce51559ff7a711511589d15610916e72e7f8ac` compares
filesystem identity, reads Windows paths through the Unicode kernel API,
requires verified process cleanup and records complete initial update status.
All four targets passed
[automatic-update run 35512568301](https://github.com/arizzi74/Smart-Stage/actions/runs/35512568301)
against the unchanged published preview 10 binaries. Each fixture uses the
release's exact source with older version metadata, discovers GitHub releases
through production code and installs automatically without an install API call.
The new executable matches an independent public download, has a new PID and
instance, retains the show/media/ports and starts stopped with stage disabled.
Both Mac checks additionally verify the complete signature, regular activation
policy, matching registered icon and no Terminal launch. Interactive update
firewall authorization is explicitly skipped and recorded; the separate
installer checks still exercise real scoped firewall-rule changes.
The corrected verification source also passed all four native/browser targets
in [run 35512568540](https://github.com/arizzi74/Smart-Stage/actions/runs/35512568540).
All six independent ZIP downloads match their checksums, architectures and
clean source metadata. Reports and scope are retained in
[`verification/release-preview10/`](verification/release-preview10/).

The installer default changed to preview 10 at
`454324a0ceb2169c4be0463418a0ba0d7c4dee20`.
[Default-install run 35512624089](https://github.com/arizzi74/Smart-Stage/actions/runs/35512624089)
passed on Apple Silicon and Intel without a release override, including the
installed bytes, registered icon/Dock identity, Terminal-free launch, graceful
Quit, quarantine handling and real scoped firewall-rule checks. The unversioned
public `main/install.sh` URL was separately fetched without a cache-busting query
on 20 September 2026 at 13:08:40 UTC. It matched the checked installer, selected
preview 10 and had SHA-256
`20bd4a2023d8c7e76e9130db27e942ba0070fb181934331a2111ddae352b7cbc`.
The default installer and public-bootstrap reports retain these observations.

Preview 9, source `d0d69d93349f2ffbf4151b3d7dddea54ba0a330d`, passed its
native builds, application checks, public downloads, browser checks and Mac
installers, but all four real automatic-update checks failed before replacement
in [run 35511624437](https://github.com/arizzi74/Smart-Stage/actions/runs/35511624437).
Its validator incorrectly expected linker flags in Go build information, which
`-trimpath` intentionally omits. The downloaded executables themselves carried
the correct tagged module version and clean source metadata. The Mac installer
default remained preview 8. Failed reports and independently verified archive
metadata are retained in
[`verification/release-preview9/`](verification/release-preview9/).
Preview 10 validates the tagged module metadata and requires the new process to
confirm its runtime version before deleting the rollback copy. A real tagged
build regression and a wrong-runtime-version rollback subprocess test cover the
correction.

[Preview 8](https://github.com/arizzi74/Smart-Stage/releases/tag/v0.1.0-preview.8)
is source `00f0c659dc01fc94432b4394e703e6b68f2a53b0`.
[Native/browser run 35508597483](https://github.com/arizzi74/Smart-Stage/actions/runs/35508597483)
passed shared race/vet and all four native build, application, icon and browser
checks. Both Mac checks launched the app through Finder and independently
queried the core process through `NSRunningApplication`: the expected bundle
identity and regular activation policy were observed, and the registered icon
and bundled ICNS rendered to identical 128-pixel RGBA images (mean absolute
channel difference 0). The checks also observed no Terminal or controlling TTY,
stdout/stderr redirected to the app log, Admin reopen with the same core PID,
and standard Quit returning exit code 0 with the process and both HTTP listeners
closed.

This is evidence of Dock eligibility and matching registered artwork. It does
not capture the Dock's displayed pixels or synthesize a Dock-menu click or
Command-Q. The standard Quit AppleEvent exercises the same application
termination handler and its deferred native/Go cleanup. The user's report that
preview 7 now works, followed by the missing-icon complaint, remains a positive
operational observation rather than a completed physical acceptance matrix.
The [physical procedure](macos-physical-test.md) now targets preview 10.

[Tagged run 35508890135](https://github.com/arizzi74/Smart-Stage/actions/runs/35508890135)
completed all 16 jobs successfully: shared race/vet, four native builds and
application/icon checks, publication of 36 assets, four fresh ZIP downloads and
startup checks, four real browser/native control checks, and both published Mac
app installers. The installed Mac apps repeat the runtime icon, regular-policy,
reopen and graceful Quit checks, alongside quarantine, replacement and scoped
firewall-rule preservation checks. Independent downloads of all four portable
ZIPs and both Mac app ZIPs matched SHA-256 checksums, architecture and clean
source metadata. The app cores match the standalone executable bytes. Native
icon reports on both architectures observed the expected application name and
bundle identity, mean pixel difference 0 and standard Quit exit code 0. Evidence
and scope are retained in
[`verification/release-preview8/`](verification/release-preview8/).

The installer default changed to preview 8 at
`1c42f555ec6dd5a8de7a00fea7f228d05bb5e725`.
[Default-install run 35509117225](https://github.com/arizzi74/Smart-Stage/actions/runs/35509117225)
passed on Apple Silicon and Intel using that immutable installer source without
a release override. Separately, the unversioned public `main/install.sh` URL
was fetched without a cache-busting query on 20 September 2026 at 11:54:54 UTC;
its bytes matched the checked source, selected preview 8 and had SHA-256
`764bc7eaba66f7f2500528934cebcf84d8c33e997f48eacffd68bb73110d6ff4`.
The `installer-default-*.json` and `public-bootstrap-verification.json` records
separate native installation evidence from the mutable public entry-point check.

[Preview 7](https://github.com/arizzi74/Smart-Stage/releases/tag/v0.1.0-preview.7)
is source `7ca8c0685ed237849406fbf2371d44a9c500ce8d`.
[Tagged run 35503933513](https://github.com/arizzi74/Smart-Stage/actions/runs/35503933513)
completed all 16 jobs successfully: shared race/vet, four native builds and
application/icon checks, publication of 36 assets, four fresh ZIP downloads and
startup checks, four real browser/native control checks, and both published Mac
app installers. Each Mac installer report records 54 true checks, including
the no-Terminal lifecycle and real firewall-rule changes described below.
All six independently downloaded ZIPs match their checksums; their executable
architecture, Go 1.26.5 build metadata and clean source revision match the tag.
The Mac app cores match the standalone executable bytes and retain the source
icon. Reports and scope: [`verification/release-preview7/`](verification/release-preview7/).

The installer default was switched to preview 7 at `f6ae30f`.
[Default-install run 35504365951](https://github.com/arizzi74/Smart-Stage/actions/runs/35504365951)
passed on both Mac architectures at verification source `84337bd`. Native CI
fetches the immutable published installer source and checks its exact bytes;
the unversioned public `main/install.sh` entry point was checked separately and
matched SHA-256 `0970a14512627aab1cef624274ec41df9f57255edbf724dc3aac1f0c7079d409`.
This separates reproducible native execution from mutable-branch/CDN timing.
The `installer-default-*.json` and `public-bootstrap-verification.json` records
retain both observations.

The Mac menu bar implementation and all four native/browser targets passed
[run 35503710100](https://github.com/arizzi74/Smart-Stage/actions/runs/35503710100)
at source `c39a11e45a2eefe7f75ef2a19f6b24809a2b516a`. Both Mac checks launched
the app through Finder without starting Terminal, verified no controlling TTY,
observed stdout/stderr in the log file, reopened Admin with the same core PID,
and sent a standard Quit event that closed the process and both HTTP ports.

The firewall installer at source `7ca8c0685ed237849406fbf2371d44a9c500ce8d`
passed [run 35503879234](https://github.com/arizzi74/Smart-Stage/actions/runs/35503879234)
on both Mac architectures, using the published preview 6 app archives. Each
report records 45 true checks, including real blocked-to-permitted firewall
rule transitions for the core and bundle, unchanged global and unrelated app
rules, cancellation recovery, and skipping elevation for an identical already
allowed installation. The four global firewall settings were preserved; the
hosted runners started with the firewall disabled. AppleScript command building
and quoting ran normally, with only interactive elevation replaced by native
passwordless sudo on the CI runner. These checks establish rule changes, not
password-dialog interaction or physical packet filtering. Reports are retained
under [`verification/release-preview7/`](verification/release-preview7/).

The Mac app installer at source `24777c368067d026379693b1ea16fdd8ed41924b` passed
[run 35501797274](https://github.com/arizzi74/Smart-Stage/actions/runs/35501797274)
on both native Mac architectures (macOS 15.7.9). Published installer bytes and
preview 6 app archives matched their checksums. Tests injected real quarantine
attributes before the normal per-app cleanup and verified that unrelated files,
other attributes and saved configuration stayed intact. Corrupt downloads,
unrelated/symlink destinations and running-app replacement were refused. Failed
replacement and an interrupted backup rename restored the original app. The
installer opened the icon-bearing app through Finder/Terminal, and the exact
installed core served Admin only on loopback. Reports and scope:
[`verification/macos-app-install/`](verification/macos-app-install/).
The app binaries remain the already-published preview 6 builds; this adds an
installation path and does not provide Apple notarization.

[Preview 6](https://github.com/arizzi74/Smart-Stage/releases/tag/v0.1.0-preview.6)
is source `4aa3529fb9695d7b9da870dae548a5400acb7758`.
[Native/browser run 35500115900](https://github.com/arizzi74/Smart-Stage/actions/runs/35500115900)
passed on all four targets, including extracted single-file ZIP startup,
automatic system-browser dispatch, local Admin bootstrap, remote link/QR display,
numeric URL pairing, fragment removal, LAN-to-Admin rejection and real native
playback controls. Shared race/vet, native import/media/HTTP checks, Windows icon
extraction and both Finder launch paths also passed.

[Tagged run 35500115581](https://github.com/arizzi74/Smart-Stage/actions/runs/35500115581)
repeated the native checks for the release-version binaries and published 36
assets. Freshly downloaded ZIPs passed checksum/extraction/startup and automatic
browser dispatch on all four target runners. The same published binaries also
passed all four real Admin/Command browser jobs, including native PLAY/STOP and
natural completion over SSE. The entire tagged workflow completed successfully.
Independently downloaded primary
ZIPs each contained exactly one executable with 0755 permissions, the expected
native architecture, Go 1.26.5 and a clean matching source revision. Both optional
Mac app ZIPs contain the same core executable bytes and the expected icons.
Retained reports: [`verification/release-preview6/`](verification/release-preview6/).

The QR API's PNGs were decoded back to their exact fragment URLs in Go tests.
The separate synthetic Chromium check covered responsive layouts, LAN link
refresh, clipboard fallback, cookie resumption, invalid-link recovery, same-tab
token navigation and STOP/reconnect behavior. Camera scanning and physical
phone/Wi-Fi compatibility remain unverified. Playback code is unchanged from
preview 4; its two-hour/resource evidence below has not been rerun for preview 6.

[Preview 5](https://github.com/arizzi74/Smart-Stage/releases/tag/v0.1.0-preview.5)
is source `9e72b4cc614f675dd7e52fe863d7973f5fb3fb65`. Its packaging changes passed
[native run 35492226235](https://github.com/arizzi74/Smart-Stage/actions/runs/35492226235)
on all four targets, including shared race/vet, native imports/media/HTTP checks
and the new icon verification. Windows Shell extracted large and small icons;
all nine embedded image sizes matched the original ICO bytes. Both optional Mac
app archives passed native ICNS decoding and strict ad-hoc signature checks,
contained the exact standalone executable bytes and started through Finder in
Terminal from a directory containing spaces, an apostrophe and Unicode. The
test matched the HTTP listener to the bundled executable's kernel-reported path.

[Tagged run 35492382186](https://github.com/arizzi74/Smart-Stage/actions/runs/35492382186)
repeated those checks for the release-version binaries, published 36 assets and
passed version-selected curl/irm installation and actual Admin/Command browser
checks on all four targets. Independent downloads of all four executables and
both Mac app archives passed SHA-256 checks; binary architecture, Go version,
module release, clean VCS revision, bundled executable/icon identity and archive
executable permissions were also verified. The native icon records refer to
the same published file hashes. Retained reports:
[`verification/release-preview5/`](verification/release-preview5/).

After the installer defaults changed at `1fc541f`,
[installer run 35492612056](https://github.com/arizzi74/Smart-Stage/actions/runs/35492612056)
and [browser run 35492612023](https://github.com/arizzi74/Smart-Stage/actions/runs/35492612023)
passed on all four targets without a version override. Built-in Windows
PowerShell 5.1 additionally installed and replaced the preview 5 executable in
the default user directory on both architectures, served Command, retained the
published hash and left one user PATH entry. The browser and PowerShell records
are retained in the same verification directory. Public raw installer URLs
were checked to select `v0.1.0-preview.5`.

The icon and optional launcher are packaging changes. Playback code is unchanged
from preview 4; the longer memory/audio/display measurements below remain
evidence for their explicitly named preview 4 binaries, not a new preview 5 soak
or physical/clean-machine acceptance.

[Preview 4](https://github.com/arizzi74/Smart-Stage/releases/tag/v0.1.0-preview.4),
source `505a1e495075dcc01abe6d7367d3b57d3f8e60a9`, passed
[tag run 35479742963](https://github.com/arizzi74/Smart-Stage/actions/runs/35479742963):
shared Go race/vet checks, four native builds/import audits/media checks,
HTTP/native application checks and release publication, followed by curl/irm
installation and real browser checks on all four targets. The browser tests
use the same workflow and coverage described below, against the published
preview 4 executables. Runtime output reports `v0.1.0-preview.4 (505a1e495075)`.
Browser result JSONs are retained alongside the download verification below;
their full screenshots remain in this run's `browser-native-*` Actions artifacts.

Downloaded files in `dist/releases/v0.1.0-preview.4/` passed checksum, architecture,
Go/module-version and clean-source-commit checks. The exact sizes, hashes and
embedded build information are retained in
[`verification/release-preview4/download-verification.json`](verification/release-preview4/download-verification.json).
The one-command installers defaulted to preview 4 at that release. The source's completed
20-minute memory profile and completed four-target two-hour soak are described in
[resource results](soak-results.md); captured native virtual pixels and the
Windows ARM64 setup-screen limitation are in [display results](native-display-results.md).

After the default changed, [installer run 35480086360](https://github.com/arizzi74/Smart-Stage/actions/runs/35480086360)
and [browser run 35480086365](https://github.com/arizzi74/Smart-Stage/actions/runs/35480086365)
passed on all four targets at installer/test commit `a95a902`, without setting a
version override. Public raw installer URLs were also checked to serve preview 4.

[Installer run 35484067090](https://github.com/arizzi74/Smart-Stage/actions/runs/35484067090)
at test commit `214e15e` additionally passed the exact `irm | iex` command under
built-in **Windows PowerShell 5.1** on AMD64 and ARM64. Both reported Desktop
edition: `5.1.26100.33296` on AMD64 and `5.1.26100.9457` on ARM64.
No version or install-directory override was
set: the executable was installed at `%LOCALAPPDATA%\SmartStage\bin\smartstage.exe`,
reported preview 4/source `505a1e495075` in the correct native architecture and
served the Command page. A second installation exercised atomic replacement,
retained the expected published checksum, left exactly one user PATH entry and
removed temporary installer directories. PowerShell 7 and both Mac curl checks
also passed in that run. These hosted machines still contain development tools;
the result is installer compatibility evidence, not clean-machine acceptance.
Raw reports: [`verification/install-powershell51-214e15e/`](verification/install-powershell51-214e15e/).

[Windows audio run 35482038438](https://github.com/arizzi74/Smart-Stage/actions/runs/35482038438)
adds actual native audio-renderer event checks on both Windows architectures:
all four cues and 120 seconds of transitions passed against published preview 4,
using a signed virtual endpoint on disposable runners. A subsequent endpoint
meter check observed six signal/STOP/replay cycles per architecture but failed
its isolation assertion on the shared virtual cable. [Audio results](windows-audio-results.md)
retain the exact setup, raw samples, session diagnostics and pre-reboot limitation.
The stock-runner release/browser coverage below remains unchanged.

Earlier evidence follows, retained with its own source and release identity.

[Preview 3](https://github.com/arizzi74/Smart-Stage/releases/tag/v0.1.0-preview.3),
commit `d70b3e2`, passed [tag run 35471043955](https://github.com/arizzi74/Smart-Stage/actions/runs/35471043955):
Go race/vet checks, all four native builds/import audits/media checks, real
application HTTP/native checks including shutdown during validation, release
publication, then curl/irm installation and HTTP startup on all four platforms.
Downloaded executable checksums, architecture headers, module version and VCS
commit were independently verified in `dist/releases/v0.1.0-preview.3/`.
Runtime version/commit output is also recorded by the four installer jobs.
Runner output availability and
coverage match the table below.

[Default installer run 35471309014](https://github.com/arizzi74/Smart-Stage/actions/runs/35471309014)
passed the actual `curl | sh` / `irm | iex` installation, version and HTTP startup
checks on all four runners. Hosted runners contain development tools; these
checks are not clean-machine evidence.

| Target | Runner environment / output availability | Actual application smoke coverage |
| --- | --- | --- |
| macOS ARM64 | macOS 15.7.9 (24G830); Apple virtual and Null Audio endpoints; one 1024×768 virtual display | Four cues configured/reordered, all four natively played/stopped, restart restores show while stopped/stage-disabled |
| macOS AMD64 | macOS 15.7.9 (24G830); Null Audio endpoint; one 1920×1080 virtual display | Same four-cue playback/STOP and restart checks |
| Windows AMD64 | `windows-2025` hosted runner; **no audio endpoints**; 1024×768 Hyper-V display | Four cues configured/reordered; silent video natively played/stopped; restart checked. Audio-renderer playback was skipped, not verified. |
| Windows ARM64 | `windows-11-arm` hosted runner; **no audio endpoints**; 1024×768 Hyper-V display | Same silent-video application checks; audio-renderer playback skipped. |

All targets natively inspected WAV, MP3, 1080p H.264/AAC MP4 and silent H.264 MP4,
and rejected damaged media. Harness tests additionally rejected unavailable
audio/display IDs without reporting playing, checked native natural completion
and reported persistent stage-enabled state. Native status is not
evidence of physically black pixels or routed sound. Mac ARM64 selected the
non-default Null endpoint; this is native routing to a virtual device only.

[Windows capability run 35473613211](https://github.com/arizzi74/Smart-Stage/actions/runs/35473613211)
clarifies the audio limitation on both runner architectures. `Audiosrv` and
`AudioEndpointBuilder` were **Running**, startup mode **Auto**, exit code 0.
`Win32_SoundDevice` and the Media/AudioEndpoint PnP lists were empty. On the same
machines, the installed preview 3 executable reported zero audio endpoints and
passed silent-video playback/STOP plus restart checks. No service/device settings
were changed and no audio driver was installed. These runners lack an installed
sound device; this is not evidence of successful Windows audio-renderer playback.
Recorded OSes: Windows Server 2025 Datacenter build 26100 on AMD64 and Windows 11
Enterprise build 26200 on ARM64. The `windows-audio-amd64`/`windows-audio-arm64`
artifacts contain the capability and application reports; local copies are in
`dist/ci-evidence/windows-audio-502f759/`.

Toolchains: Go 1.26.5; Macs used Xcode 16.4 (16F6), Apple Clang 17.0.0
(clang-1700.0.13.5), SDK 15.5, deployment target 12.0. Windows used LLVM-MinGW
20260908 UCRT / Clang 23.1.1. Full toolchain/import, native harness and application
smoke records accompany the published preview 3 executables.

Local development: Ubuntu 22.04.5 ARM64; Windows cross-builds also succeed.
PE audits found only OS libraries (19 AMD64 imports, 18 ARM64 imports).
Mac `otool -L` audits likewise allow only OS frameworks/libraries. Compiler
support is linked into Windows executables. Audits cannot prove every eventual
OS/plugin load: clean-machine testing remains mandatory.

`go test -race ./...` and `go vet ./...` pass on Linux ARM64. Coverage includes
generation cancellation, epochs, duplicate/conflicting IDs, multiple controllers,
revision conflicts, active-source protection, persistence/corruption/backup/lock,
canonical roots, roles, CSRF/Origin/Host, malformed bodies, path redaction,
overload-independent STOP and authoritative SSE reconnects. New coverage checks
the TCP cap and shutdown, IPv4 link-local discovery, actual listing truncation,
rejection of oversized saves without losing the show/backup, invalid stored
labels and refusal to start an unsupported/no-cgo backend.

Chromium browser checks use a **synthetic HTTP fixture**, separate from native
tests. They passed at widths 320/390/768/844/1280: pairing, four wrapped labels,
escaping, visible STOP, STOP during pending PLAY, failed offline STOP,
server-authoritative highlighting, reconnect without replay, gap refresh and
Admin layout. Screenshots/results: `dist/browser-checks/`.

An additional [real browser run 35472402760](https://github.com/arizzi74/Smart-Stage/actions/runs/35472402760)
passed on all four targets against the **published preview 3 executables**.
Test source: `983cdef`; application source: `d70b3e2`. Playwright 1.63.0 and
Chromium 153.0.8010.12 opened separate Admin/Command sessions at an actual
non-loopback host IPv4 address over plain HTTP (`isSecureContext === false`).
Admin selected four real host files, saved four labels/reordered cue IDs and
selected native outputs. Command displayed the saved order, sent one PLAY per
tap, and received actual native playing/STOP/natural-completion state. The test
checked Command path redaction/access restrictions, no media transfer/elements,
and visible STOP at 390×844. Admin was exercised at 1280×900.

Both Macs natively played/stopped all four cues. Both Windows runners played
silent video, then requested audio and verified a visible missing-output error
with STOP recovery. Windows audio playback was not verified. Node versions were
22.23.2 on both Macs/Windows AMD64 and 24.21.0 on Windows ARM64; Node ran in each
runner's native architecture. The browser ran on the host, so this is not a
physical phone/Wi-Fi test. These screenshots show browser UI, not native output
pixels. Local evidence: `dist/ci-evidence/browser-983cdef/`; each job also exposes
its `browser-native-<os>-<arch>` artifact with result JSON and screenshots.

[Repeat/reporting run 35472755329](https://github.com/arizzi74/Smart-Stage/actions/runs/35472755329)
at test commit `ead78c6` passed Go race/vet and all four native jobs. Main CI now
includes ten seconds of repeated native PLAY/replacement/STOP before the saved
show restart check. Downloaded reports contain completed runs of 10.13–10.59
seconds, 19/20 cycles on Mac AMD64/ARM64 and 24/26 cycles on Windows AMD64/ARM64,
with initial/final resident-memory samples and Windows handle counts. This is
a short regression check, not long-running stability proof. The test script
now checkpoints incomplete reports and prints resource samples every minute;
it retains evidence if a future repeat loop fails or is interrupted. Neither
already-running two-hour job uses this later reporting change.

Native [captured-pixel observations](native-display-results.md) at renderer-fix
source `505a1e4` passed video/restart/STOP/end checks on both Mac virtual
desktops and Windows AMD64. A small Mac OS indicator remains visible. Windows
ARM64 captured Windows first-run setup and failed the visual assertions. These
results add virtual-pixel evidence without establishing physical outputs or latency.

## Reproduce

```sh
go test -race ./...
go vet ./...
bash scripts/build.sh darwin arm64
bash scripts/build.sh darwin amd64
bash scripts/build.sh windows amd64
bash scripts/build.sh windows arm64
python3 scripts/native-smoke.py dist/native-harness-darwin-arm64
python3 scripts/application-smoke.py dist/smartstage-darwin-arm64
NODE_PATH=/path/to/playwright/node_modules node scripts/browser-smoke.cjs
npm ci --prefix scripts/browser --ignore-scripts --no-audit --no-fund
node scripts/browser/node_modules/playwright/cli.js install chromium
node scripts/browser-native-smoke.cjs dist/smartstage-darwin-arm64
```

For repeated native transitions, add a duration in seconds to
`scripts/application-smoke.py`, for example
`python3 scripts/application-smoke.py dist/smartstage-darwin-arm64 7200`.
The adjacent `.http-smoke.json` records requested/elapsed time, completed cycles
and resource samples; current scripts mark the repeat loop `completed` only
after its full duration. Check the process/workflow result and subsequent
restart result too. A partial report or ten-second CI run cannot establish the
two-hour requirement.

The harness natural-end check now waits for the native `ended` event with a
30-second bound and records observed event times. In earlier
[run 35479026373](https://github.com/arizzi74/Smart-Stage/actions/runs/35479026373),
the Windows AMD64 harness exited at its fixed six-second process deadline while
the last playback position was only 2.60/3.00 seconds; that run failed its end
assertion. [Run 35479868178](https://github.com/arizzi74/Smart-Stage/actions/runs/35479868178)
at test source `4305547` passed the revised check on all four targets, with
observed end events 3.42–4.25 seconds after process start. It retains the required
end/stage-enabled assertions and does not change application code. Raw records
are in [`verification/native-smoke-4305547/`](verification/native-smoke-4305547/).
These process/event observations are not physical cue-start or STOP latency.

Use matching OS runners normally. Windows Linux cross-builds are additional
compile/import checks. Go/native SDKs/Python/Playwright are development-only.
`DEBUG=1` retains symbols; `VERSION=...` and Git commit identify the build.
`scripts/audit-dependencies.py` saves and checks PE imports/`otool -L` output.
SHA-256 files describe executable bytes after Mac ad-hoc signing.

## Remaining acceptance work

These are **unverified**, not assumed passed:

- Clean Windows 11 AMD64/ARM64 and both Mac architectures without Go, compilers,
  developer SDKs, players, extra runtimes or application libraries.
- Physical non-default audio output for audio and video soundtracks; second
  display; extended/mirrored layouts, negative coordinates, mixed DPI/rotation.
- Actual silence/black pixels during loading/STOP/end/error/replacement. No
  physical STOP latency (target 200 ms server-to-silence/black) or controller
  latency (target 500 ms tap-to-stopped) has been measured. Bluetooth buffering
  and hard real-time behavior are not guaranteed.
- Audio/display unplug/replug, system-default changes during playback, no
  fallback, no visible window relocation and explicit re-selection on return.
- Actual phones/tablets on a LAN; sleeping/reconnecting or slow controllers;
  validation/filesystem work under event conditions; inaccessible/protected
  folders and missing drives.
- Physical two-hour A/V stability, including drift and resource behavior on
  event hardware. [Preview 4 soak 35478342253](https://github.com/arizzi74/Smart-Stage/actions/runs/35478342253)
  completed all four native transition/STOP loops for at least 7,200 seconds,
  with 121 resource samples and successful restart checks at exact source
  `505a1e4`. [Resource analysis](soak-results.md) records substantially reduced
  Mac RSS trends (+0.34/+1.08 MiB/hour after minute 15 on AMD64/ARM64), together
  with the heap/idle evidence supporting the renderer fix. Windows memory and
  handle traces fluctuate with positive fitted trends; the stock runners had
  no audio endpoints. All earlier runs retain their own source identity and
  evidence. These observations do not establish physical A/V drift, routed
  sound, a two-hour Windows audio workload or leak-free behavior in every case.
- Production signing/notarization and the bare-executable permission workflow.

Windows 11 is the intended validation baseline. Mac deployment target 12.0 is
not a physically established minimum; CI ran macOS 15.7.9. Do not infer support
for every older OS revision. Mac binaries are ad-hoc signed, not Developer ID
signed/notarized. Windows binaries are not Authenticode signed. Standard OS
prompts can appear; use documented per-app approval, never disable OS security
globally. If protected-folder/local-network permissions need bundle metadata on
real machines, record that constraint before claiming bare-executable support.

## Event preparation

Rehearse exact files and outputs. Use extended desktop for independent stage
projection. Check physical blackout and a reachable STOP control. Verify trusted
LAN reachability, guest-network isolation, firewall and local-network permissions;
do not configure internet port forwarding. Prepare power, notifications, locks,
forced sleep and display arrangement. Native power assertions cannot suppress
OS dialogs/forced sleep. Keep a recovery procedure: application crash, OS failure,
disconnected projector or power loss can expose the desktop. Quit closes stage.
