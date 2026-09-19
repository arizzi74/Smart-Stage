# Architecture

Smart Stage is one Go process with a compiled-in native bridge. It embeds the
HTML/CSS/JavaScript with `go:embed` and never starts a media player, browser
engine, helper process, transcoder or separate service. The native harness is a
development artifact, not an application dependency. Unsupported platforms or
builds without cgo fail initialization; there is no production fake backend.

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

Pairing/session/CSRF secrets have 192 random bits. Sessions expire after 24
hours, with HttpOnly and SameSite=Strict cookies. Host and Origin are validated.
HTTP on the trusted LAN does not encrypt traffic. At most 128 sessions, 256
accepted TCP connections, 64 SSE streams and 16 ordinary concurrent operations
are admitted; STOP bypasses the ordinary-operation limit. The TCP cap includes
idle keep-alive connections and requests still sending their headers. Excess
connections wait in the OS listen backlog. Header/read/idle timeouts release
stalled connections; a network or connection-exhaustion attack can still prevent
remote commands, so local Escape remains the emergency control. JSON, paths,
labels, cue counts and listings are bounded.

SSE supplies full snapshots and ten-second heartbeats. One notification slot
per subscriber and stream write deadlines isolate slow clients. The controller
disables cues when stale/disconnected, fetches authoritative state on reconnect
or revision gaps, and never replays PLAY. STOP remains a best-effort authenticated
HTTP request and is explicitly unconfirmed when its acknowledgement fails.
