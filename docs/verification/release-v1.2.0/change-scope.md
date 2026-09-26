# Playlist file save and load

Admin can save and load version-1 `.smartstage.json` files. Mac/Windows desktop hosts use native Save/Open dialogs; browser-only hosts download and upload bounded JSON files. Media remains at original absolute host paths. Files include cue order, labels, colors, hidden/background flags and saved stage/sound settings. Output devices, credentials, language preferences and validation caches are excluded.

Loading confirms replacement, requires stopped playback (or an idle error state) and Stage off, checks the expected revision, validates every media path, then persists cues and stage settings in one transaction. Failed saves, invalid files, missing media and conflicting edits leave the current playlist unchanged. Fresh cue IDs and remapped background references isolate stale requests and validation callbacks. Loading never starts playback; media is revalidated.

Native dialogs return a path only to the Go coordinator. Web requests cannot provide native read/write paths. One native operation is active at a time, native result IDs are checked once, saving captures the committed playlist before opening the dialog, and loading rechecks its original revision after selection. File writes use a same-directory temporary file followed by atomic replacement. Admin session, loopback peer, Host/Origin and CSRF checks remain enforced; remote controllers cannot export/import files or open dialogs.

## Local validation

- Full Go race suite passed, including real nginx/Caddy checks.
- Go vet and source package vulnerability scan passed.
- All 11 JavaScript language/wake-lock tests passed.
- Complete browser smoke passed: downloaded file round trip, malformed/oversized files, cancellation, conflicting revisions, unsaved stage drafts, playback/update guards, English/Italian controls and native status handling.
- HTTP/controller tests exercised native save snapshots, cancellation, stale/duplicate native results, load revision conflicts, file restoration and local Admin authorization. File tests covered replacement, invalid targets, symlink refusal and cleanup.
- Windows production and native harness sources passed compilation checks for both architectures.

The initial candidate's round-trip assertion was corrected to compare canonical media paths, and the Windows test driver uses the expanded temporary-folder path. Mac screenshots showed that post-presentation proxy property changes did not update the system-hosted Save dialog. The final driver leaves the production default name intact, waits for the panel to settle, and confirms Save with a real Return key. It asserts the actual result, expected filename and no file written by the dialog. Earlier failed automation attempts were not promoted to passes; production behavior was unchanged throughout these test-driver corrections. Final platform/runtime, publication and public-byte verification are recorded separately.

The existing deployed v1.1.2 gateway is compatible. No production gateway service/configuration or firewall rules were changed for this feature. Hosted native tests do not establish physical speaker/projector behavior; playlist files do not relocate media across computers. A hung network filesystem can delay completion/shutdown while an accepted file operation finishes.
