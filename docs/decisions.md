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
`arizzi74/Smart-Stage`, GitHub binary publication and curl/irm installers. Those
extend packaging only; playback semantics remain unchanged. Preview publication
is explicitly labelled incomplete acceptance until physical checks pass.

All successful cue edits auto-save; derived metadata is a cache and is refreshed
on startup rather than trusted across processes. Output changes disarm the stage
and require an enable action or new video cue. Primary/only-display permission
is explicitly acknowledged in Outputs and stored with that selection. A device
loss requires re-selection/save, even after the endpoint returns.

Limits: 500 cues, 1,000 listed entries per folder, 256 accepted TCP connections,
64 SSE clients, 128 sessions, 16 concurrent ordinary HTTP operations and 4,096
accepted idempotency records.
STOP bypasses ordinary-operation admission. These bound memory/work without
adding another service. No drag-and-drop is necessary because accessible Up/Down
controls meet the ordering requirement.

The installers choose native OS architecture, verify release checksums and
install per user without elevation. Their default is the explicitly named
preview version; `SMARTSTAGE_VERSION` can select a different release. They do
not disable Gatekeeper/SmartScreen, firewalls or OS permissions.
