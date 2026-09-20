# Implementation decisions

## Native APIs (19 September 2026)

The Windows bridge uses Media Foundation Media Session, a configured Streaming
Audio Renderer, and EVR in a native borderless window. Audio and video share one
session clock. The endpoint ID attribute is set before renderer activation;
the endpoint-role attribute is never set.

The preferred newer APIs were evaluated first. `MediaPlayer.AudioDevice` has
explicit routing, but the selected Go-compatible LLVM-MinGW toolchain does not ship
the C++/WinRT projection required for a small maintainable integration. The
documented `IMFMediaEngineEx` surface provides an endpoint **role**, not an
arbitrary endpoint ID. The specification's allowed Media Session fallback
provides endpoint selection with OS DLLs and a compiled-in C++ bridge. This is
a toolchain decision, not a claim that MediaPlayer cannot work unpackaged.

References checked against official documentation:
- [MediaPlayer.AudioDevice](https://learn.microsoft.com/en-us/uwp/api/windows.media.playback.mediaplayer.audiodevice)
- [IMFMediaEngineEx methods](https://learn.microsoft.com/en-us/windows/win32/api/mfmediaengine/nn-mfmediaengine-imfmediaengineex)
- [Audio renderer endpoint attribute](https://learn.microsoft.com/en-us/windows/win32/medfound/mf-audio-renderer-attribute-endpoint-id-attribute)

Windows STOP uses `IMFAudioStreamVolume` through `MR_STREAM_VOLUME_SERVICE`.
The default SAR audio session is shared by the process, so using session-wide
mute risks carrying STOP's mute state into later cues. Per-stream zero channel
volumes silence only the retiring renderer and preserve the operator's mixer
settings. Channel counts and volume control are checked before playback, with
storage prepared in advance. This correction follows source/API review; physical
Windows audio verification is still pending.

- [SAR audio sessions and volume scope](https://learn.microsoft.com/en-us/windows/win32/medfound/streaming-audio-renderer)
- [IMFAudioStreamVolume](https://learn.microsoft.com/en-us/windows/win32/api/mfidl/nn-mfidl-imfaudiostreamvolume)

macOS uses AVPlayer/AVPlayerLayer, AppKit and Core Audio. Each cue uses an
explicit audio-device UID, including cues whose preference is system default.
The main process thread runs AppKit; background work never pumps a substitute
event loop. The proposed deployment target is macOS 12.0, pending verification
with the matching Go toolchain, actual SDK and clean machines. No older version
is claimed as supported.

- [AVPlayer.audioOutputDeviceUniqueID](https://developer.apple.com/documentation/avfoundation/avplayer/audiooutputdeviceuniqueid)
- [Go OS-thread affinity](https://go.dev/wiki/LockOSThread)

Linux is a development/test host, not a playback target. Unsupported builds
fail explicitly; no fake backend is linked as a production fallback.

## Product details

The user additionally requested Windows ARM64 builds, pushing commits to
`arizzi74/Smart-Stage` and GitHub binary publication. The later download request
initially replaced curl/irm installation with direct architecture-specific ZIP
links. The subsequent Mac launch request adds a Mac-only curl installer for the
existing app bundle with its Finder icon. It verifies the published archive and
removes quarantine only from that installed app, as explicitly requested by the
user. ZIP downloads remain available, and Windows continues to use ZIPs. Each
standalone ZIP contains one executable; browser assets, native playback and QR
rendering are compiled in. The Mac app bundle adds a launcher and icon around
the same executable; it is one Finder application, not one physical file.
The later request to run without Terminal changes the Mac app launcher to
redirect logs and execute the core directly. A native menu bar control provides
Open Admin, log access and Quit; quitting uses the existing graceful shutdown.
The user subsequently reported that the menu-bar-only app left no visible icon
and made Quit difficult to find. Finder launches therefore use regular AppKit
activation, an explicit application icon, and standard application/Dock Quit.
Terminal launches retain their existing native activation policy.
After the user's Mac firewall blocked remote access, the user explicitly
requested an installer step to allow Smart Stage's incoming connections. This
uses per-application firewall rules with macOS administrator authorization; it
does not disable the firewall or modify other applications' rules. Managed
network filters may still require an IT policy change.
Preview publication remains explicitly incomplete acceptance until physical
checks pass.

All successful cue edits auto-save; derived metadata is a cache and is refreshed
on startup rather than trusted across processes. Output changes disarm the stage
and require an enable action or new video cue. Primary/only-display permission
is explicitly acknowledged in Outputs and stored with that selection. A device
loss requires re-selection/save, even after the endpoint returns.

Limits: 500 cues, 1,000 listed entries per folder, 64 SSE clients, 128 sessions
and 4,096 accepted idempotency records per process. Each HTTP listener admits
256 TCP connections and 16 concurrent ordinary operations.
STOP bypasses ordinary-operation admission. These bound memory/work without
adding another service. Accessible Up/Down controls remain available for ordering.

## Original files and compact remote controls (20 September 2026)

The user requested compact browsing with dot-prefixed names hidden by default,
dragging media into the playlist without copying it, per-cue button colors, small
remote Stage/Disconnect controls, and Escape to turn the stage off. These requests
extend the original UI requirements. Hiding files affects presentation, not
media-root authorization. Cue colors are optional persisted RGB values and are
included in path-redacted remote state.

Host-file rows can be dragged within Admin using paths already returned by its
authenticated file browser. A web browser does not disclose full filesystem
paths for Finder drops. The Mac app therefore accepts native Finder/Dock file-open
events and offers Choose Media, also available through Admin’s native chooser
button; its native queue passes original paths to the
same root validation and atomic playlist save. No files are uploaded or copied,
and importing does not trigger playback. During an update reservation imports
are rejected with an instruction to retry afterward. The app registers audio and
movie document types as a non-default Viewer.

The authenticated remote may now POST stage on/off using the normal Origin and
CSRF checks. It cannot change output selection or other Admin settings. Escape
in the native app/stage or a connected browser page stops and closes the stage;
this deliberately changes the earlier Escape-retains-black behavior. Ordinary
STOP and natural completion continue to retain an enabled black stage.

Keep awake uses the standard Screen Wake Lock API with explicit opt-in and
actual lock status. HTTPS and browser support are required; HTTP LAN pages show
Needs HTTPS and explain the device Auto-Lock/Screen timeout alternative. No
hidden media playback, automatic certificate installation, or OS lock override
is introduced. The lock is released on disconnect/backgrounding and requested
again when the opted-in connected page becomes visible.

## Automatic application updates (20 September 2026)

The user explicitly chose automatic installation. Smart Stage therefore checks
official GitHub releases at startup and installs a newer compatible release
before enabling playback. Startup reserves the same coordinator used by remote
PLAY, so a phone cannot start a cue during update preparation. Saved edits finish
before reservation. Failed/offline checks release controls. Checks later in the
session advertise updates for the next launch; they never interrupt a show.
Admin also offers an immediate update action while stopped with stage disabled.

The update code is embedded in the existing executable. A temporary copy of that
executable performs replacement after graceful shutdown and actual parent exit;
there is no installed updater service or additional runtime. Mac updates replace
the complete signed app bundle and relaunch through LaunchServices. Standalone
Mac and Windows downloads replace their executable in place. The previous
installation remains recoverable until the new instance confirms startup.

Release discovery uses the release list because GitHub's latest endpoint omits
prereleases. Numeric semantic-version ordering and stable/preview channel rules
prevent downgrade or lexicographic mistakes. Downloads use the fixed repository,
HTTPS and published SHA-256 checksums. Only local authenticated Admin exposes
update actions; it accepts no caller-supplied feed URL, destination or version.
Restart preserves show files and rotates the phone pairing code as usual.

## Local Admin and camera pairing (20 September 2026)

The user's latest instruction is authoritative over the original prompt: launch
opens Admin automatically in the system browser; Admin listens only on
`127.0.0.1`; its page shows a phone/tablet URL and QR code; the remote URL contains
a short random numeric token. This explicitly replaces the original LAN-accessible
Admin/key-pairing design and the blanket prohibition on credentials in URLs.
The original specification remains unchanged as historical input.

Admin and Command run on separate listeners (defaults `127.0.0.1:8787` and
`0.0.0.0:8788`). Actual loopback peer, fixed loopback Host, Origin and fetch
metadata checks protect Admin's automatic local session endpoint. The remote
listener cannot serve Admin or issue/use Admin sessions, including requests
from localhost or carrying forged forwarding headers. Distinct cookie names
(`smartstage_admin_session`, `smartstage_command_session`) and mandatory
role matching isolate privileges despite browser cookies not being port scoped.

The token has eight decimal digits sampled uniformly with `crypto/rand`, retains
leading zeroes and regenerates at each process launch. URL fragments carry it
as `/command#token=12345678`; the controller removes the fragment before making
requests and exchanges it for a full-entropy session. Fragments satisfy the
requested scan/open flow while keeping the code out of HTTP request URLs and
referrers. Only local Admin reveals the URL/code/QR. The short code is protected
by ten pairing attempts per peer IP per minute and 100 per minute globally.
Session and CSRF tokens retain 192 random bits; STOP bypasses pairing limits.
Possession of the URL grants playback access. Trusted-LAN HTTP does not protect
against eavesdropping.

QR images are rendered locally from the exact link with the vendored, pinned
[`github.com/piglig/go-qr` v1.1.0](https://github.com/piglig/go-qr/releases/tag/v1.1.0)
(`832517b8dd4c5f48188211c0c6691c6ac38b0363`). It has no third-party runtime
dependencies. Native Go decoder tests recover the exact URL including the token
from generated PNGs. The upstream MIT copyright/license notice is embedded in
the executable and available at `/licenses.txt` on either listener. Packaging
therefore remains one executable per primary ZIP.


## Admin lifecycle and remote layout (20 September 2026)

The user requested removal of the remote heading/ordinary acknowledgement block,
a visible Admin Quit action, and reuse of an open Admin page after relaunch.
Command success remains available to assistive technology; errors remain visible
without reserving an empty block above cues. Quit requires the loopback Admin
session, Origin and CSRF, acknowledges before graceful cleanup, and does not
require browser-window closure. The page quietly probes after Quit.

A fresh authenticated Admin heartbeat or a live Admin SSE proves page presence.
Startup allows six seconds for reconnect, then opens the URL only if no page was
seen; diagnostics that fetch state alone do not count. Mac native menu, Dock and
successful import requests share the Go browser-launch policy. A new host
instance makes the existing Admin page reload current assets. Detection is best
effort for background/discarded tabs; no browser-specific Automation permission
is added and exact-tab selection is not promised.

External Finder drops remain unable to supply native paths to a normal system
browser. Admin can now open the existing asynchronous native chooser and asks the
operator to select those files, keeping original references. Reading a global
native drag pasteboard after a browser request would not reliably bind a path to
the user's actual drop, so no such workaround is used.


## Dedicated Mac Admin window (20 September 2026)

The user approved displaying the existing Admin interface inside a dedicated
macOS application window instead of an external browser. The Mac app therefore
uses AppKit plus the system WKWebView, with one retained Admin window. Dock and
menu requests show that same window; browser-tab detection is unnecessary for
this route. Closing Admin hides it without interrupting playback. Explicit Quit
retains the existing graceful shutdown. Windows and standalone Mac CLI launches
continue to use the system browser, and phone/tablet remote control remains web
based. The existing `--no-browser` flag also suppresses automatic window display.

Native Finder drops target the app-owned window. Their original local file URLs
come from that specific drag session and enter the bounded native import queue.
Go retains media-root validation and atomic playlist persistence; no media is
uploaded, copied, or played merely because it was dropped. Internal Host files
dragging still uses the existing authenticated web listing. A harmless desktop
user-agent marker changes help text only and never grants authorization.

The web view loads the real loopback Admin page and uses its normal HTTP/session
security. It does not receive a privileged JavaScript filesystem bridge. Native
navigation policy prevents replacing Admin with an arbitrary web/file page;
intended external links can open in the system browser. The framework is supplied
by macOS rather than bundled as an additional app runtime.


## Simplified Admin media and connection setup (20 September 2026)

At the user's request, preview 16 removes the Host files browser and its internal
row-dragging interface. Native Finder drops, Dock imports and Choose Media remain
the Mac app's media entry points, preserving original paths. The local filesystem
APIs remain private and available for compatibility; Admin no longer requests a
directory listing. Browser-only hosts without a native chooser get a small,
collapsed absolute-path import form so they can still build a playlist.

Connection mode, gateway URL/token and save/reconnect actions sit inside a native
collapsed details element. Polling and reconnection do not open it. The current
remote link, QR code, status and explicit LAN firewall guidance remain visible.
The installer/updater keep the existing gateway-first policy: only saved explicit
LAN mode (or an intentional advanced installer override) enables firewall setup.
An already-running updater from preview 14 or earlier cannot be changed by a
newly downloaded executable; using the current Mac installer avoids that older
helper's unconditional firewall step.
