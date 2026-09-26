# Visual cue toggles and transitions

Selected foreground videos and visible/preparing images toggle off on a second
intentional PLAY. Retried request IDs remain idempotent. Video stop clears any
covering image; image stop preserves independent music but also stops a covered
foreground video. Stage remains enabled and returns to the current background
or black. Selecting a retained image while Stage is off shows it again.
Background selectors and the optional audio toggle retain their prior behavior.

The existing optional fade duration applies to sound and visuals; the config,
playlist file format and native bridge ABI have no new fields. First playback
from silence/black stays immediate. Native code retains bounded outgoing visuals
through replacements and returns, cancels immediately for Stage off/Escape/hard
STOP, and disposes old sources after their audio and visual use ends.

Mac blends native image/player layers. Windows blends decoded images and live
EVR readbacks at up to 30 Hz in a bounded 1920×1080 temporary overlay, then
returns to native video rendering. Unsupported video readback has a bounded
fallback to the new visual with a diagnostic; it cannot leave the overlay frozen.
Rapid replacement bounds visual owners and prevents live EVRs sharing a target
window. Physical display smoothness is not established by hosted CI.

Backend regressions cover black/image/video backgrounds, repeat request IDs,
late callbacks and loading cancellation, independent music identity/timeline,
covered videos, stale epochs, Stage off/reselection and unchanged background
and optional audio behavior. Browser regressions cover English/Italian labels,
Admin/remote selected controls and the actual repeat-cue request. Native probes
extend prior audio checks with image/video replacements, moving video sources,
returns to background/black, disposal, interruption and immediate cancellation.

Local Go race tests, vet, source package vulnerability scan, all 11 JavaScript
unit tests and the browser smoke suite passed. Windows AMD64/ARM64 production
bridge and native scene probe cross-links passed. Platform runtime outcomes are
recorded separately in the exact candidate/tag workflow and scene evidence.

This feature needs no gateway/proxy/firewall changes. Media paths, show data,
playlist files, credentials and installer privileges are unchanged.

The first candidate's Windows ARM browser run exposed a reproducible Admin
validation race: a checking-state playlist response could arrive after the final
ready event and leave video/background controls unavailable without a later
event. The fix serializes validation-detail refreshes and drains pending state
changes, discarding superseded snapshots. A deterministic browser regression
holds that response across the final event and checks recovery without another
event while preserving focused label and unsaved Stage settings. The native
browser helper also handles concurrent promise rejections immediately so future
failures preserve diagnostics rather than terminating before cleanup.

A second candidate passed that checkbox flow but failed its ARM Stage-settings
save when unconditional background reinspection returned an error. The generic
message hid its cause; no native decoder-lifecycle defect was established.
ConfigureStage now freshly resolves the allowed host file and reuses only ready
validation whose size and modification time still match, as foreground playback
already does. Changed, checking or unchecked files are re-inspected; missing,
out-of-root, nonregular and audio-only backgrounds reject. Inspection failures
include the original reason in local Admin. Tests cover successful reuse despite
a deliberately failing extra decoder, changed-file success/failure, persistence
protection and responsive STOP during blocked inspection. The native browser
scenario waits for validation scheduled by its flag edits before asserting
settings persistence, and records the actual HTTP error body on failure.
