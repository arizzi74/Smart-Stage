# Acceptance audit

Audit date: 19 September 2026. **Full acceptance is not achieved.** This maps
the specification to current implementation and evidence. An automated result
is credited only for what it observes; native events do not prove physical
sound, black pixels, routing or timing.

Native/application evidence: [preview 3 tag run 35471043955](https://github.com/arizzi74/Smart-Stage/actions/runs/35471043955),
commit `d70b3e2`, passed all four OS/architecture jobs. Linux Go race/vet also
passed. Publication/installation evidence for the currently published preview
is in [release verification](release-verification.md). That document records
the exact runner OS, compiler, SDK and available outputs.

[Real browser run 35472402760](https://github.com/arizzi74/Smart-Stage/actions/runs/35472402760)
also passed against all four published preview 3 executables. Test source is
`983cdef`; browser interactions reach the actual app/native backend over a
non-loopback host HTTP address, without a synthetic server. The browser runs
on each hosted machine, so this does not establish phone/Wi-Fi compatibility.

[Windows capability run 35473613211](https://github.com/arizzi74/Smart-Stage/actions/runs/35473613211)
confirmed both Windows audio services running but no installed sound devices or
native audio endpoints on either runner architecture. The published preview's
silent-video/restart checks passed in those same environments; Windows audio
playback still needs machines with real render endpoints.

## Product and implementation requirements

| Specification | Implementation and inspected evidence | Remaining proof |
| --- | --- | --- |
| §§2–3: one host process, native playback, embedded offline web interfaces, no external runtime/player | `cmd/smartstage`, `internal/web/embed.go`, both compiled-in bridges; four executable import audits allow OS libraries only | Clean machines without developer tools or extra media runtimes; physical host playback |
| §3: release architectures, metadata, stripped builds, checksums | `scripts/build.sh`, four native CI jobs and published Mac ARM64/AMD64 + Windows ARM64/AMD64 assets; downloaded headers/hashes checked | Physically established minimum OS versions; signing/notarization workflow |
| §4: AVFoundation/AppKit/Core Audio on main thread, per-player routing | `bridge_darwin.m`, initial-thread lock in `platform_native.go`; four fixture cues natively play/stop on both Mac runners, including one non-default virtual endpoint | Real non-default speaker and video soundtrack routing; second screen |
| §4: Windows native renderer, explicit endpoint, one timeline and compiled-in COM bridge | `bridge_windows.cpp`; documented Media Session/EVR fallback and common MTA; real format inspection and silent-video playback on both Windows runners. Source/API review corrected session-wide STOP mute to per-stream channel volume control | Windows runners expose no audio endpoints, so actual audio-renderer playback, successive-cue audibility, per-stream STOP and routing remain unverified |
| §4: bounded bridge, ownership and orderly shutdown | C ABI ownership in `bridge.h`; capped native queues, latest command/load slots, Go inspection drain; shutdown-during-validation native smoke | Native heap/handle stability over the full soak and difficult OS/driver shutdown conditions |
| §5: flags, launch keys, local URLs, interface refresh, port errors | `cmd/smartstage/main.go`, `internal/lan`; loopback native application launch, actual distinct keys; LAN unit checks include IPv4 link-local and IPv6 global URLs | Actual phone reachability and changing physical interfaces. IPv6 link-local scope URLs are deliberately not advertised |
| §6: host browsing, roots, symlinks, Unicode, volumes, read-only files | `internal/files`; real target-OS path tests, native Unicode fixture inspection, root/symlink containment and 1,000-entry listing checks | Real disconnected drives, protected folders, Windows reparse configurations and OS permission prompts |
| §6: cue IDs, labels, duplicate sources, order, revisions, automatic persistence, active-cue protection | `internal/app/edit.go`, store and app tests; native application creates/reorders four cues and restores them after restart; browser escaping/layout checks | Full operator workflow on event machines |
| §6: output selectors, stable IDs, primary warning, explicit stage enable/disable, stopped-only changes | Admin UI and coordinator checks; both bridges enumerate native outputs and pin concrete identities; invalid output IDs cause native error without playing | Primary/secondary and mirrored layouts, default-device changes, physical stage transitions |
| §7: responsive Command, sticky STOP, text labels, authoritative status, single-tap commands, reconnect | Playwright checks at 320/390/768/844/1280 widths cover pending PLAY/STOP, escaping, stale status, gap fetch and no replay; images inspected | iPhone/Android/iPad browsers, sleep/reconnect and landscape on actual devices |
| §8: manual cue/restart/replacement, no overlap/advance, STOP/end/error retain black stage | Coordinator generations and both native stop paths; tests cover replacements, native playing/stopped/ended/error events, persistent enabled state | Actual silence, black pixels, no overlap, aspect ratio, cursor and loading/teardown appearance |
| §9: instance/revisions, epochs, idempotency, latest PLAY, STOP priority, cancellation | Go race tests cover PLAY A→PLAY B→STOP, late callbacks, old epochs, request retry/conflict, concurrent controllers, blocked save and slow subscribers | End-to-end behavior under real network/storage/driver load; physical STOP latency |
| §10: output loss, disarm, no fallback/resume, Escape, power assertions | Native notifications, explicit identities and fault state; local Escape race test; unavailable-ID native rejection on all four targets | Unplug/replug/default changes during playback, window relocation, physical Escape focus and idle-sleep behavior |
| §11: MP3/PCM WAV/H.264+AAC/silent H.264, native preflight, metadata, revalidation | All four real native decoders inspect the documented fixtures and reject damaged media; no preflight renderers; cache and bounded validation paths inspected | Additional real show files, long-file integrity and physical rehearsal; only the documented fixture profiles are proven |
| §12: schema, per-user storage, atomic replacement, backup, corruption, instance lock, silent restart | Store tests run on all four target OS jobs; corrupt originals retained; native app restart restores labels/order and remains stopped/stage-disabled. The 4 MiB limit is enforced before saving so successful edits stay readable on restart | Sudden power-loss/storage failure and clean-user permission scenarios |
| §13: documented API, roles, random keys/cookies, CSRF/Host/Origin, logout/expiry, safe text | `internal/httpapi`, `internal/auth`, tests and [API contracts](api.md); controller path redaction and browser escaping checked | Real deployment/LAN security review; HTTP intentionally provides no confidentiality |
| §13: bounded requests, connections and SSE; reconnect and overload-independent STOP | 256 TCP connections, 64 SSE streams, 16 ordinary operations; connection-cap shutdown test; STOP bypass, malformed-body and authoritative reconnect tests | Pathological network floods cannot guarantee remote STOP; local emergency control is still required |
| §§14–15: no production fake, builds, debug artifacts, toolchains, dependency records | Unsupported-build test refuses to start; all four native builds and import audits; build/debug scripts and metadata | Missing native framework/driver initialization on affected real OS installations; transitive runtime behavior on clean machines |
| §§16–18: native harness, fixtures/provenance, source and required documents | Real harness, CC0 fixtures/encoding/hashes, all named documents, Git history and preview assets exist | Physical feasibility and acceptance gates remain open despite the implemented later milestones |
| §19: manual single-cue scope | Source/UI contain manual PLAY/restart/STOP and separate stage enablement; no automatic playlist advancement, streaming, uploads or parallel outputs | Confirm final behavior in the physical scenario below |

## Required end-to-end scenario

These are the specification's eleven steps, not a substitute scenario.

| Step | Current evidence / status |
| --- | --- |
| 1. Run on clean supported machines | **Pending**. Hosted runner installs pass but runners contain development tools. |
| 2. Actual LAN URLs and separate keys | Real browser pairs using separate launch keys at a printed non-loopback host IPv4 URL on all four runners; LAN discovery has unit coverage. Physical remote LAN reachability pending. |
| 3. Pair Admin and browse host filesystem | Real browser interacts with each published native application and selects actual host files. Physical remote browser-to-host check pending. |
| 4. Add two audio/two video cues, labels and order | Real Admin browser and separate actual application HTTP checks pass on all four runners. |
| 5. Non-default audio and second display | **Pending physical hardware**. Only Mac ARM64 has a tested non-default virtual endpoint. |
| 6. Pair a phone/tablet on the same LAN | **Pending**. Desktop Chromium and HTTP sessions are insufficient proof. |
| 7. Four ordered labelled buttons and continuously visible STOP | Real Command browser checks pass at 390×844 on all four native hosts; synthetic layout checks cover the other listed sizes. Physical mobile browsers pending. |
| 8. Each cue on chosen physical speakers/display | Mac virtual playback passes all four cues; Windows silent-video playback only. **Physical routing pending on both OSes**. |
| 9. STOP during audio/video/loading, no restart, silence/blackout | State/race/native events pass available checks. Physical output and latency pending. |
| 10. Natural end without advance, silent/black | Native completion/stage-enabled events pass. Physical sound/pixels pending. |
| 11. Restart restores configuration while silent/stage-disabled | Actual application checks pass on all four runners; physical observation pending. |

## Outstanding measurements and long-running evidence

The two-hour [preview 3 soak 35471045100](https://github.com/arizzi74/Smart-Stage/actions/runs/35471045100)
is running at exact release commit `d70b3e2`. It repeats native transitions/STOP
and collects resident memory plus Windows handle counts. Completion and resource
trend analysis are pending. The earlier [soak 35468888430](https://github.com/arizzi74/Smart-Stage/actions/runs/35468888430)
is also still running at `20bcf35`; its evidence must remain tied to that earlier
source and cannot establish the correctness of later changes.

No measured claim is made for server-received STOP to physical silence/black
(target 200 ms), tap to visible controller stopped state (target 500 ms), cue
start latency or audiovisual drift. Physical measurements need a documented
wired-audio/LAN setup and observation of the real outputs. HTTP/native event
timestamps alone would not prove these targets.

No goal-complete or production-ready claim is justified until those physical,
clean-machine and long-running requirements have supporting evidence.
