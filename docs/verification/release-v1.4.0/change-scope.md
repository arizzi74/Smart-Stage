# Audio/video seeking and pause/resume

Admin and remote share a fixed bottom audio/video position slider. Touch, mouse,
keyboard and assistive range input preview time locally in a centered overlay;
release sends one seek with the active cue, instance, stop epoch, generation and
transport revision. Cancellation, lost connection or changed playback invalidates
the gesture. The overlay fades unless reduced motion is requested. Unknown-duration
and loading media cannot seek. Pending native acknowledgement preserves keyboard
focus, and an old pending request does not block a newly selected source.

A second selected audio/video press pauses the same native source; another resumes.
Paused video retains its frame, independent images remain selected, and a paused
seek stays paused. STOP clears foreground media/image and returns to background or
black; image buttons and Stage visibility retain their existing semantics. The
legacy toggleAudio setting remains in version-1 files for compatibility but no
longer controls selected audio/video button behavior.

Mac AVPlayer and Windows Media Foundation commands/callbacks carry applied
transport revisions. Windows serializes asynchronous native Start/Pause operations;
Mac guards asynchronous seek completion and current timeline end notifications.
Newer STOP/seek/pause intent wins over stale native events. Windows paused seek at
the exact duration uses the last 100 ms to let the native Pause acknowledge before
completion. Playback remains native on the host; no media is uploaded to browsers.

Local Go/race/vet, package vulnerability checks, JavaScript tests and browser
regressions passed. Browser evidence uses a synthetic host and does not prove
native audio/video playback. The real HTTPS relay test uses actual HTTP, TLS,
tunnel, cookies, CSRF and SSE, with an explicitly simulated native boundary.
Mac/Windows native CI and public-download verification are recorded separately.
Hosted Windows runners may have no audio endpoint; this is reported explicitly,
not counted as successful physical audio verification. Physical device acceptance
remains open. Screenshots contain fixture labels and masked connection data.

Transport extends the existing /api/play route and existing state snapshots; no
new gateway route is needed. The production gateway, nginx and firewall were not
changed. Public gateway remains the default connection mode.

The first candidate passed both Mac jobs and Windows AMD64, but its Windows ARM64
scene probe incorrectly observed GetCursor's thread-local cached handle after a
synthetic cursor message. The corrected probe checks released cursor ownership,
requires successful global GetCursorInfo observation, and rejects Smart Stage's
transparent global cursor after Stage off. No production cursor code or transport
assertion was changed. Final candidate results refer only to the corrected source.
