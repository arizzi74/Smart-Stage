# Media compatibility and preflight

Playback uses the host OS decoders. Extension alone does not establish type or
compatibility. DRM, HTTP streams, transcoding, codec packs and unusual
multichannel routing are outside v1. Other native-playable formats are best
effort unless explicitly covered by tests.

Self-authored fixture provenance, parameters and hashes are in `testdata/media/`.
The development generation script uses an installed encoder; Smart Stage ships
and invokes no encoder/player. The inspected baseline is:

| File | Encoding | Native inspection |
| --- | --- | --- |
| Unicode/apostrophe/space WAV | PCM s16le, stereo 48 kHz, 3 s, ~−24 dBFS | Passed on all four target runners |
| MP3 | MPEG-1 Layer III, 128 kbit/s, stereo 48 kHz, 3 s | Passed on all four |
| Silent MP4 | H.264 High level 4.0, YUV420P, 1920×1080, 30 fps, 3 s | Passed on all four |
| MP4 with soundtrack | Same video plus AAC-LC stereo 48 kHz, 128 kbit/s | Passed on all four |
| Damaged MP4 | Deliberate non-media bytes | Correctly rejected on all four |

These are sample profiles, not guarantees for all MP3/WAV/MP4 files. Stock
Windows runners have no audio render endpoints. A separate
[signed virtual-driver evaluation](windows-audio-results.md) passed actual
native playback/STOP events for all four cues on both Windows architectures,
and observed signal/STOP/replay for WAV, MP3 and H.264/AAC. Its strict isolation
test failed on the two driver-provided meters of the same virtual cable.
Mac runner outputs are virtual/null devices.
Physical speaker/projector results and full-file integrity remain unverified.

Windows preflight uses Source Reader PCM/RGB32 output and requires a decoded
sample from each first audio/video track, rewinding between tracks. It creates
no audio/video renderer. macOS loads native asset metadata asynchronously and
decodes a sample per media type using AVAssetReader, with no AVPlayer/window.
Type/duration come from native metadata; zero duration is shown as unknown.

Validation runs on startup, cue edits, Validate all cues, and PLAY preparation
when cached readiness or size/modification time changed. Missing/invalid cues
remain visible. An accepted changed/inaccessible source fails into silence and
blackout. Validation never blocks the STOP mutex/path. Application inspection
callers have a 35-second timeout; the Mac asset-property load has a 30-second
timeout. An OS filesystem/decoder call can continue until the OS returns.

Preflight does not decode every frame. Rehearse every exact show file to its end
on the intended event machine/output. Protected folders, lost mounts and OS
editions without multimedia components can fail; no optional codec/runtime is
downloaded. Native Windows scope is described in Microsoft's
[format documentation](https://learn.microsoft.com/en-us/windows/win32/medfound/supported-media-formats-in-media-foundation).
