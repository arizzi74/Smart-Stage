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
adding another service. No drag-and-drop is necessary because accessible Up/Down
controls meet the ordering requirement.

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
