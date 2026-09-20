# Architecture

Smart Stage is one Go process with a compiled-in native bridge. It embeds the
HTML/CSS/JavaScript with `go:embed` and uses OS-native playback without a media
player, transcoder or separate service. After both HTTP listeners are ready, it
asks the operating system to open local Admin in the default system browser;
`--no-browser` disables that launch. A browser engine is not bundled. The native
harness is a development artifact, not an application dependency. Unsupported platforms or
builds without cgo fail initialization; there is no production fake backend.

The optional Mac app launcher redirects output to
`~/Library/Logs/Smart Stage/smartstage.log` and replaces itself with the bundled
core executable. It does not start Terminal or leave a separate background
helper. Finder launches use regular AppKit activation with the bundled icon in
the Dock and a standard application menu. Dock Quit and the application menu's
Quit command request graceful shutdown. The additional menu bar control reopens
local Admin, reveals the log, and requests normal shutdown. Closing the browser
leaves the host running.

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
crosses apartments. A latest-only source-loader mailbox prepares Media Session
topologies off the UI thread. A separate worker shuts down retired sources and
sessions. A nonblocking native event poll advances playback. Audio and video
use one session clock; audio renderer endpoint ID is set before activation.
STOP zeros the retiring renderer's per-stream channel volumes, then stops its
session. It does not change the shared audio-session or endpoint mixer state.

macOS uses AVPlayer/AVPlayerItem/AVPlayerLayer, asynchronous asset loading and
Core Audio endpoint UIDs. `audioOutputDeviceUniqueID` pins each cue's route.
AppKit, KVO handling and player transitions run on the main queue. Preflight
uses AVAssetReader on a background worker. ARC owns objects; teardown removes
KVO, notification and time observers and detaches the player layer.

`internal/playback/backend.go` defines the typed boundary. `bridge.h` defines
the C ABI: UTF-8 input strings are copied; returned native malloc strings are
freed with `ss_free`. No Go pointer/callback, COM interface or Objective-C
object crosses the boundary. Native events use a bounded JSON queue polled by
Go. Inspection admits one native operation at a time. Cancellation releases
the caller, but an OS call may continue until the decoder/filesystem returns;
it cannot hold the coordinator mutex or trap STOP behind it.

## Commands, generation and blackout

PLAY requires the current process instance and stop epoch. Accepted playback
changes advance a generation. STOP advances the epoch, clears the active cue,
cancels pending preparation and invalidates old native work before enqueueing
blackout. Late callbacks are ignored. A latest-only mailbox replaces pending
PLAY; there is no advancing cue queue. Native errors stop/blacken and remain
visible. HTTP acknowledgement means accepted, not physically playing/silent.

The last 4,096 accepted request IDs are tracked with their payload hash and
acknowledgement (FIFO eviction). Identical retries are idempotent; different
payloads conflict. Deliberate repeated presses use different IDs and restart.
Command-role responses use cue views without source paths or raw native errors.

The stage window persists through STOP/end/error. Windows hides the EVR child
surface over a black parent; macOS uses an independent black overlay and hides
the video layer. Current-generation video alone can be revealed. Native
renderers preserve aspect ratio. Disable/exit deliberately hide/close the stage.
Escape requests STOP. Power assertions discourage idle/display sleep.

Native endpoint/display notifications trigger checks. Device loss stops media;
display loss also hides/disarms output. Explicit re-selection/save is required
after an output fault. The system-default audio preference is resolved anew
for each cue and never changes global OS routing. Physical hotplug/relocation
behavior still requires validation.

## Files, persistence and network

Configuration is per-user schema-versioned JSON, with a native exclusive
process lock released by the OS after a crash. Saves sync a same-directory
temporary file, then replace atomically (`rename` plus directory sync on Unix;
`ReplaceFileW`/`MoveFileExW` on Windows). The previous valid snapshot becomes
`state.json.bak`. Corruption stops startup, retaining the original for recovery.
Restart restores cues/preferences but never playback or stage enablement.

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
