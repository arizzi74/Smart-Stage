# Implementation decisions

## Native APIs (19 September 2026)

The Windows bridge uses Media Foundation Media Session, a configured Streaming
Audio Renderer, and EVR in a native borderless window. Audio and video share one
session clock. The endpoint ID attribute is set before renderer activation;
the endpoint-role attribute is never set.

The preferred newer APIs were evaluated first. `MediaPlayer.AudioDevice` has
explicit routing, but the available Go-compatible GNU toolchain does not ship
the C++/WinRT projection required for a small maintainable integration. The
documented `IMFMediaEngineEx` surface provides an endpoint **role**, not an
arbitrary endpoint ID. The specification's allowed Media Session fallback
provides endpoint selection with OS DLLs and a compiled-in C++ bridge. This is
a toolchain decision, not a claim that MediaPlayer cannot work unpackaged.

References checked against official documentation:
- [MediaPlayer.AudioDevice](https://learn.microsoft.com/en-us/uwp/api/windows.media.playback.mediaplayer.audiodevice)
- [IMFMediaEngineEx methods](https://learn.microsoft.com/en-us/windows/win32/api/mfmediaengine/nn-mfmediaengine-imfmediaengineex)
- [Audio renderer endpoint attribute](https://learn.microsoft.com/en-us/windows/win32/medfound/mf-audio-renderer-attribute-endpoint-id-attribute)

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
