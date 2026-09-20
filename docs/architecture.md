# Architecture

Smart Stage has one Go host process with a compiled-in native bridge. It embeds
HTML/CSS/JavaScript with `go:embed` and uses OS-native playback without a media
player, transcoder or separate playback service. The bundled Mac app displays
Admin in one retained AppKit window containing the system WKWebView. macOS may
start its own WebKit rendering/network helper processes; no browser engine or
additional runtime is distributed with Smart Stage.

After both HTTP listeners are ready, the Mac app shows its Admin window. It can
show startup-update progress while the service blocks playback and edits. Dock,
menu and native-import actions restore the same window, independent of external
browser presence. Closing the window hides Admin and keeps the host running;
Quit stops playback and exits. Window UI remains on the AppKit main queue.

Standalone Mac executables and Windows use the system browser. They give an
existing authenticated Admin page up to six seconds to reconnect before opening
another page. Active Admin SSE and authenticated heartbeats establish presence;
a new host instance reloads the existing page's assets. `--no-browser` suppresses
automatic Admin presentation for either window or browser, while explicit native
Open Admin actions remain available. Unsupported platforms/builds without cgo
fail initialization; there is no production fake backend.

The Mac launcher redirects output to
`~/Library/Logs/Smart Stage/smartstage.log` and replaces itself with the bundled
core executable. It does not start Terminal or leave an extra launcher process.
The app has its Dock icon and standard menus, including Quit and text editing.
The additional menu bar control opens Admin, reveals the log and requests Quit.

The dedicated window loads the actual loopback Admin URL, using the existing
session, Origin, CSRF and local-listener protections. Native navigation policy
keeps Admin in that local origin; intended external links use the system browser.
Finder drops are read from the specific native drag session's file-URL pasteboard
and queued through the same original-path import and root-validation flow as
Dock imports. Web-page scripts do not acquire arbitrary filesystem access.

## Automatic updates

`internal/update` checks the fixed GitHub repository on launch and every six
hours. Startup alone can install automatically: the coordinator reserves stopped
playback and a disabled stage before the first check, temporarily rejecting PLAY
and configuration changes. A ten-second metadata timeout releases the reservation
when offline. Subsequent checks never automatically stop/restart a running
session. Authenticated local Admin can explicitly request an idle update.

The updater selects a newer release for the running OS/architecture and package
type, verifies its SHA-256 checksum, and validates a bounded archive before
handoff. Staging is beside the installation so replacement stays on one volume.
A temporary copy of the existing executable runs in helper mode without the
native playback loop, waits for the original process to exit, and replaces the
app with a backup for rollback. The original process hands ownership to this
helper after closing Go services and storage, before final native shutdown:
AppKit can terminate directly while completing a pending Quit request. The
helper must observe the original process's actual exit before replacing files.
The replacement registers a private startup
receipt and confirms its expected version after native initialization, saved-show
loading and listener setup. Show configuration is never replaced by the updater.
Mac bundles restart through LaunchServices to retain their Dock identity; Windows
uses a detached helper to outlive the locked original executable. Update outcomes
are shown in Admin, and a failed target is not retried automatically in a loop.

The helper exists only during an update, with no extra downloaded runtime or
permanent service. `--no-auto-update` keeps checks but disables installation on
launch. Admin update routes retain loopback, session, Origin and CSRF restrictions.

## Threads and ownership

`internal/app.Service` owns authoritative show/playback state. Its mutex covers
in-memory transitions and short native enqueue calls. Filesystem operations,
inspection, persistence and HTTP streaming run outside it. A separate writer
mutex serializes saved edits. Cues being removed/replaced cannot be newly
activated while their edit is being written. STOP never acquires that mutex.

The initial OS thread is locked during package initialization and runs AppKit
or Win32. The Go application/server run in another goroutine. Windows UI,
loader, inspector and cleanup threads use the same COM MTA; no STA interface
crosses apartments. Bounded per-role source-loader mailboxes prepare Media Session topologies off
the UI thread. A separate worker shuts down retired sources and sessions. A
nonblocking native event poll advances playback. Each audio/video source uses
one session clock; its audio renderer endpoint ID is set before activation.
Per-stream channel volumes implement transitions without changing the shared
audio-session or endpoint mixer. WIC decodes images into bounded native rasters.

macOS uses AVPlayer/AVPlayerItem/AVPlayerLayer, asynchronous asset loading and
Core Audio endpoint UIDs. `audioOutputDeviceUniqueID` pins each cue's route.
AppKit, KVO handling and player transitions run on the main queue. Preflight
uses AVAssetReader on a background worker. ARC owns objects; teardown removes
KVO, notification and time observers and detaches each player's own layer.
ImageIO decodes still images on a bounded worker queue; validation also decodes
an image without creating a visible renderer.

`internal/playback/backend.go` defines the typed boundary. `bridge.h` defines
the C ABI: UTF-8 input strings are copied; returned native malloc strings are
freed with `ss_free`. No Go pointer/callback, COM interface or Objective-C
object crosses the boundary. Native events use a bounded JSON queue polled by
Go. Inspection admits one native operation at a time. Cancellation releases
the caller, but an OS call may continue until the decoder/filesystem returns;
it cannot hold the coordinator mutex or trap STOP behind it.

## Scenes, commands and stage presentation

`SceneBackend.ApplyScene` supplies a complete desired scene: a foreground
source/identity, independent image overlay, background image/video and audio
opt-in, output IDs, stage visibility, fade duration and hard-stop flag. Native
code copies every string before returning. Scene revision orders a latest-only
command mailbox; foreground identity remains stable across image, background
and visibility changes. A repeated intentional foreground PLAY uses a new
identity. Legacy `Start/Stop/Stage` remain for the native diagnostic harness.

PLAY requires the current process instance and stop epoch. Accepted foreground
changes advance a generation; replacement preparation keeps outgoing sound
available until the incoming decoder starts. STOP advances the epoch, cancels
pending foreground/image work and clears both selections. Native foreground
`playing/progress/ended` events identify that foreground generation;
`stage/background-error` events identify the applied scene revision. This lets
music progress remain valid across a stage toggle while stale visual state is
ignored. Async decoder completion is tied to source identity, and newer STOP
or hard-stop commands prevent a stale decoder from starting or revealing.

The last 4,096 accepted request IDs retain their payload hash and acknowledgement
(FIFO eviction). Identical retries are idempotent; different payloads conflict.
Deliberate repeated audio/video presses restart, unless optional audio-toggle
mode makes a second press stop the current audio cue. Image cues preserve
foreground sound; background buttons replace only the session's background.
Hidden buttons remain saved cues and are omitted from the remote UI, not from
role-appropriate state. Command responses contain no source paths or raw errors.

Visual priority is image overlay, foreground video, background image/video, then
opaque black. Native renderers preserve aspect ratio. Background video loops
while the stage is enabled and remains muted behind foreground sound, retaining
its loop position when music ends. Its soundtrack is opt-in. Stage off hides all
visuals and silences background sound while preserving the foreground timeline;
stage on restores the selected visual. Pointer hiding applies only over the
visible native stage. It does not hide the pointer over Admin or other apps.

When fading is enabled, the configured duration (initially one second; 0.1–30
seconds) controls audio replacement and STOP. Incoming sound starts immediately
from silence; otherwise outgoing and incoming streams overlap with volume ramps. Native code bounds active sources and retiring audio tails, and a
new command retargets the current gains. STOP returns an enabled stage to the
background, crossfading back to its optional soundtrack or fading to silence.
Natural foreground completion also returns to background. A stopped foreground
may therefore coexist with an audible background and an enabled stage.

Escape, authenticated emergency stop, Quit and update shutdown immediately
silence all streams, cancel fades and hide the stage. Connected browser pages
send `/api/emergency-stop`; stage on/off is a separate operation. Native Escape
remains effective when a concurrent PLAY makes its generation stale, without
global keyboard permission. Foreground errors and device loss also disarm the
stage; background errors leave foreground music intact. Explicit output
re-selection/save is required after device loss. The system-default audio
preference resolves to a concrete endpoint without changing global OS routing.

HTTP acknowledgement means accepted, not physically playing or silent. Power
assertions discourage idle/display sleep on the host while stage output is on;
remote browser Screen Wake Lock is separate and requires a secure context.
Physical routing, cursor visibility, hotplug and projector behavior still require
hardware validation.

## Files, persistence and network

Configuration is per-user schema-versioned JSON, with a native exclusive
process lock released by the OS after a crash. Saves sync a same-directory
temporary file, then replace atomically (`rename` plus directory sync on Unix;
`ReplaceFileW`/`MoveFileExW` on Windows). The previous valid snapshot becomes
`state.json.bak`. Corruption stops startup, retaining the original for recovery.
Restart restores cues/preferences, including the saved background, fade settings
and hidden/background button flags, but never playback or stage enablement.
Selecting a background button overrides the current session's background; the
saved default remains the next-launch selection.

Root restrictions resolve symlinks/junctions and compare relative paths and
volumes, during browsing, source edits, inspection and each play preparation.
Windows device namespaces and alternate streams are rejected. There is no
filesystem mutation/download endpoint. The signed-in host user and local
filesystem are trusted: the native path-based media APIs are not an isolation
boundary against a hostile local process racing replacement of ancestor paths.

Two listeners share the service while keeping separate HTTP roles. Admin binds
only `127.0.0.1:8787` by default; its handler additionally checks the actual TCP
peer is loopback and Host is exactly `127.0.0.1` with its configured port. An
explicit same-origin `POST /api/local-session {}` creates/reuses an Admin session.
Remote control binds `0.0.0.0:8788` by default and exposes only Command routes;
changing `--bind` cannot make Admin listen on the network. Forwarded-IP headers
never grant local access. The signed-in host user and local processes are
trusted. Host/Origin and fetch-metadata checks prevent a foreign browser origin
from using local auto-login.

Each process generates an eight-digit code using `crypto/rand`. Local Admin
shows discovered remote URLs and a QR code. Links use `/command#token=…`; the
controller removes the fragment before posting the code to `/api/pair`. A
successful exchange issues a Command session, regardless of other credentials
presented. Pairing has per-IP (ten/minute) and global (100/minute) attempt budgets.
`smartstage_admin_session` and `smartstage_command_session` are separate
HttpOnly, SameSite=Strict cookies, with a mandatory matching role check on each
listener. Session/CSRF secrets retain 192 random bits and expire after 24 hours.
Codes and sessions rotate at process restart. Only authenticated local Admin
can retrieve remote links, the code and QR images; Command state/events keep
source paths and raw native errors redacted. This design implements the user's
later localhost-Admin/camera-pairing request, as recorded in
[decisions](decisions.md#local-admin-and-camera-pairing-20-september-2026).

QR rendering uses the pinned, vendored, pure-Go `github.com/piglig/go-qr` v1.1.0
encoder. It generates PNGs in memory with no network service or runtime package.
Its full MIT copyright/license notice is embedded and served at `/licenses.txt`
on either listener, preserving the one-executable distribution.

HTTP on the trusted LAN does not encrypt traffic. At most 128 sessions and 64
SSE streams are shared across the process. Each listener admits 256 accepted TCP
connections and 16 ordinary concurrent operations; STOP bypasses ordinary and
pairing limits. The TCP cap includes idle keep-alive connections and requests
still sending headers. Excess connections wait in the OS listen backlog.
Header/read/idle timeouts release stalled connections; network or connection
exhaustion can still prevent remote commands, so local Escape remains the
emergency control. JSON, paths, labels, cue counts, listings and QR payloads are
bounded.

SSE supplies full snapshots and ten-second heartbeats. One notification slot
per subscriber and stream write deadlines isolate slow clients. The controller
disables cues when stale/disconnected, fetches authoritative state on reconnect
or revision gaps, and never replays PLAY. STOP remains a best-effort authenticated
HTTP request and is explicitly unconfirmed when its acknowledgement fails.
