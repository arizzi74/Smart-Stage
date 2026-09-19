# Smart Stage — Codex implementation goal prompt

**Version:** 1.0  
**Prepared:** 19 September 2026  
**Deliverable:** A working application, source code, native backends, tests, build scripts, and operating documentation.

## 1. Your assignment

Build **Smart Stage**, a self-contained desktop-hosted application for manually triggering music and video cues during live events.

Implement the product described below. Do not stop after producing a design, a scaffold, screenshots, or a browser-only demonstration. Native playback on the host computer is the central requirement. Inspect the repository first, preserve unrelated work, and then implement in testable milestones.

Use Go for shared application logic. Compile thin platform-specific native bridges into the application where required. Make reasonable decisions for unspecified details and record them in `docs/decisions.md`; do not introduce additional services or expand the product into a general-purpose media editor.

Distinguish implemented, compiled, automatically tested, and physically verified capabilities. Never describe an untested native backend or a mock as production-ready.

## 2. Product goal and example

One application runs on a Windows PC or Mac connected to the event's audio equipment and screen/projector. It exposes two browser interfaces over the local network:

- `/admin`: configure the show, browse the **host computer's** filesystem, assemble an ordered playlist, label items, and select audio and video outputs.
- `/command`: a responsive remote control containing one large button per playlist item and a prominent **STOP** button.

Example playlist:

| Position | Host file | Operator-defined label |
| --- | --- | --- |
| 1 | `opening.mp3` | Opening music |
| 2 | `intro.mp4` | Welcome video |
| 3 | `interlude.wav` | Interlude |
| 4 | `finale.mp4` | Finale |

The phone displays four cue buttons bearing those labels. Pressing **Welcome video** starts `intro.mp4` on the **host's selected monitor**, with its soundtrack on the **host's selected audio output**. Nothing plays on the phone. Pressing **STOP** silences playback and leaves the selected stage display black.

The playlist is an ordered **manual cue list**, not an automatically advancing queue.

## 3. Non-negotiable architecture and packaging

### One application, one process

Run the web server, application state, and native playback integration in one application process. Native OS services used internally by media frameworks are not separate Smart Stage components.

Run in the signed-in user's interactive desktop session. The application may be launched from a terminal and remain running, but do not implement the playback application as a Windows service, a privileged daemon, or a headless agent.

### Download-and-run contract

Required release architectures:

- Windows: `windows/amd64`.
- macOS Apple Silicon: `darwin/arm64`.
- macOS Intel: `darwin/amd64`.

Deliver one executable for each target. A universal macOS executable may additionally combine the two Mac architectures. Document and test minimum OS versions; use Windows 11 as the initial Windows validation baseline. Select the macOS deployment target against the chosen Go toolchain, SDK, required APIs, and real test environments rather than claiming every macOS version works.

The user must not need to install Go, Node.js, Python, a compiler, a database, a media player, or an additional application runtime. Include web assets in the executable using `go:embed`. [R1]

“Self-contained” means application code, web assets, and application-specific bridges are included. Dynamic imports of libraries/frameworks supplied by a supported OS are allowed. Do **not** attempt to statically link macOS system frameworks; Apple's published guidance distinguishes static libraries from fully static executables. [R2]

Do not introduce VLC, libVLC, FFmpeg, ffprobe, mpv, GStreamer, Electron, Qt, a browser engine, an external playback executable, or a companion DLL/dylib containing Smart Stage application logic. Do not unpack and launch hidden helpers at runtime. Do not depend on a separately installed Windows App SDK or WebView2 runtime.

Build-time native toolchains are allowed. `cgo` provides the bridge to C-compatible native interfaces; its use requires an appropriate native compiler when building. This does not make that compiler an end-user requirement. [R3]

Use stripped release builds where supported, with version and commit metadata. Do not assume `CGO_ENABLED=0` or a blanket `-static` linker flag satisfies this project.

### Local and offline

After downloading the application and supplying local media files, the show must work without internet access. Embed all HTML, CSS, JavaScript, icons, and fonts that are actually needed. Prefer system fonts, vanilla JavaScript, and Go's standard library. Small compiled-in Go dependencies are acceptable when justified and pinned.

No cloud login, external CDN, remote telemetry, runtime package downloads, or online licensing check.

## 4. Native media backends

Create a small, documented Go-facing backend boundary. It should cover initialization, device/display enumeration, media inspection, output configuration, asynchronous playback, stop/blackout, status events, and shutdown. Keep HTTP handlers and playlist logic independent of OS-specific types.

Keep audio and video from the same media file on one synchronized native playback timeline. Do not launch separate unsynchronized audio and video players.

### macOS

Use AVFoundation for media playback, with `AVPlayer` and `AVPlayerLayer` as the expected starting point. Use AppKit for the native output window and screen selection, and Core Audio for audio-device enumeration. Investigate and verify `AVPlayer.audioOutputDeviceUniqueID` against the actual target SDK for per-player output routing. Do not silently substitute changing the system-wide default audio device. [R4, R5]

Use an Objective-C bridge with a C-compatible interface, compiled into the executable. Keep window ownership and Cocoa work on the proper main thread, with a live AppKit event loop. Go documents the need to respect OS-thread affinity when using graphical frameworks. [R6]

Do not block the main event loop with HTTP serving, directory scans, or media inspection. Correctly manage Objective-C ownership, observers, callback lifetimes, and autorelease pools.

### Windows

Use Windows-native media playback and native window/display APIs. Evaluate `Windows.Media.Playback.MediaPlayer` or `IMFMediaEngine` first: Microsoft recommends these for new video playback code. `MediaPlayer.AudioDevice` is a documented per-player audio-routing facility. Prove that the selected integration works in the required unpackaged executable without additional runtimes. [R7, R8]

If necessary to satisfy explicit endpoint selection and single-executable packaging, a Media Foundation Media Session topology with a configured audio renderer and an EVR video renderer is an allowed fallback. Document why that legacy renderer was selected. Microsoft documents Media Session playback and the audio-renderer endpoint attribute `MF_AUDIO_RENDERER_ATTRIBUTE_ENDPOINT_ID`. Do not set conflicting endpoint-ID and endpoint-role attributes. [R9, R10]

Use either native Go bindings or a compiled-in C/C++ bridge. Enumerate actual render endpoints, not recording devices. Route both standalone audio and video soundtracks to the chosen endpoint without changing global Windows settings.

Create a native borderless output window, with correct monitor coordinates and DPI handling. Manage COM initialization, thread ownership, callbacks, and native resources explicitly. Never pass a COM interface between threads without a valid threading/marshaling strategy.

### Mandatory early feasibility milestone

Before polishing the web interfaces, implement a minimal real playback harness for each backend that demonstrates:

1. Listing real audio devices and monitors.
2. Playing local audio on a selected non-default output.
3. Playing a local video on a selected monitor, with its soundtrack on that same chosen audio output.
4. Stopping into silence and a persistent black window.

Use this to settle native API and toolchain decisions early. Verify APIs against current official documentation and installed SDK headers; do not invent methods or assume an API demonstrated for one renderer works for another.

## 5. Launching and LAN access

The normal command is `smartstage` or `smartstage.exe`.

Provide these flags, with documented defaults:

| Flag | Behaviour |
| --- | --- |
| `--port` | HTTP port; default `8787`. |
| `--bind` | Listen address; default `0.0.0.0`, with an explicit-interface option. |
| `--advertise-ip` | Select one valid local address to emphasize in printed URLs. |
| `--config-dir` | Override the normal per-user data/configuration directory. |
| `--media-root` | Repeatable optional restriction on accessible host directories. |
| `--log-level` | Logging verbosity; never include authentication secrets in request logs. |
| `--version` | Print build and platform information and exit. |

On successful startup, print usable LAN addresses, for example:

```text
Smart Stage
Admin:   http://192.168.1.42:8787/admin
Command: http://192.168.1.42:8787/command

Admin pairing key:   <generated secret>
Command pairing key: <different generated secret>
```

The addresses above are examples, not hard-coded values.

Enumerate active non-loopback addresses. When multiple network interfaces are present, list candidate URLs with interface names instead of claiming to know which network the phone uses. Do not print `0.0.0.0` as a destination. A loopback URL may be printed additionally, but cannot be the only address when a LAN address exists.

If there is no LAN address, say so and permit local configuration. Report address changes. Fail clearly on a port conflict rather than silently changing the port.

Document that the controller and host need network reachability, that guest-network isolation can prevent access, and that firewall/local-network permissions may require user approval. Do not automatically disable firewalls or expose the application through internet port forwarding.

## 6. Admin interface

Provide a usable desktop/tablet interface with these sections: **Playlist**, **Host files**, **Outputs**, and **Status**.

### Browse host files

The file browser must browse files on the PC/Mac running Smart Stage. A browser upload dialog or `<input type="file">` on the phone is not a substitute.

Start in the host user's home directory. Show navigable directories and accessible Windows drives or macOS mounted volumes. Support breadcrumbs, parent navigation, file names, and useful media metadata. Files remain in place; adding a cue does not upload, copy, transcode, or embed the media.

By default, an authenticated administrator may browse locations readable by the host user. When `--media-root` restrictions are supplied, enforce them server-side for browsing, inspection, cue creation, and playback. Account for symlinks, Windows junctions/reparse points, path normalization, and drive boundaries. Do not use a naive string-prefix check.

Support Unicode, spaces, apostrophes, and duplicate filenames in different directories. Fail gracefully for missing drives and denied access. Do not follow arbitrary HTTP URLs or run commands derived from paths.

Filesystem access is read-only. Do not implement deletion, rename, command execution, or an unrestricted HTTP download endpoint.

### Playlist editor

Support adding one or several selected files, editing labels, removing cue entries, and reordering cues. Provide accessible move-up/move-down controls as well as optional drag-and-drop. Removing a cue must never delete its file.

Give each cue a stable unique ID independent of its position, label, or filename. The same file may appear more than once under different cue IDs. Default the label to the filename without its extension; require a non-empty label. Duplicate labels are allowed but should show a warning or position number.

Display detected media type, duration when available, and validation status. Keep missing/unsupported entries visible with an explanation rather than silently dropping them.

Persist successful edits automatically and update connected control pages. Prevent lost updates between multiple admin tabs using an expected playlist revision or equivalent optimistic concurrency check.

Allow label/order changes during playback. Reject removal or source replacement of the active/loading cue until stopped; show a clear message rather than unexpectedly changing the playing media.

### Output configuration

Provide separate selectors for:

- The global **audio output**, used for all audio cues and every video soundtrack.
- The **stage display**, used for all video cues.

Include a system-default audio option and actual named devices. Persist stable identities, not list positions. Resolve a configured system-default choice to a concrete endpoint for each new cue, and avoid silent route changes while that cue is running.

List display names, dimensions, and primary-display status. Never assume monitor number 2 exists. Treat mirrored displays honestly; do not promise independent projection in a mirrored desktop configuration.

Changing outputs is allowed only while stopped. Missing configured devices require explicit operator selection; do not silently choose another speaker or monitor.

Provide **Enable stage output** and **Disable stage output** controls. Warn before covering the only display or primary display. These controls are distinct from STOP.

On startup, restore configuration but do not play media or automatically cover a display. Once a display is configured, an explicit Enable action or a valid video cue may activate it. Audio-only operation must work without selecting or enabling a stage display.

## 7. Command interface

Design `/command` for iPhone, Android phones, iPad, other tablets, and desktop browsers. It must work in portrait and landscape, including narrow phone widths.

Show the playlist as a responsive grid of large touch-friendly buttons, in playlist order, using the operator's labels. Do not truncate labels into ambiguity; wrap them cleanly. Show cue position/type as secondary information.

Keep a large, high-contrast red **STOP** button visible without requiring scrolling. Do not require confirmation to stop. STOP must remain usable while loading, while another command is pending, and after a recoverable error.

Show connected/disconnected status, current cue, loading/playing/stopped/error state, and elapsed/duration information when available. Highlight actual server-reported playback, not merely the last tapped button. Status must not rely on colour alone.

One deliberate tap sends one command. Do not register duplicate touch and click actions. Use semantic buttons, accessible labels, visible focus, and at least approximately 48 CSS-pixel touch targets.

Do not render local audio/video elements as the playback engine. The controller sends commands only and never receives a media stream.

Reconnect automatically after connection loss and fetch authoritative state. Do not queue or replay PLAY commands after reconnection. Mark status as stale, disable cue activation while disconnected, and never claim a STOP succeeded without acknowledgement. Permit a best-effort authenticated STOP request when its HTTP path remains reachable, clearly showing failure when it is not.

Losing all browser connections must not stop currently running host playback. A phone going to sleep must not make the music stop.

## 8. Exact playback semantics

Implement these behaviours consistently across operating systems:

| Action/event | Required result |
| --- | --- |
| Press an audio cue | Start it from the beginning on the selected audio device; keep an enabled stage window black. |
| Press a video cue | Start from the beginning, fullscreen on the selected display; soundtrack follows the selected audio device. |
| Press another cue | Stop the previous cue, then start the new one. No overlap or automatic crossfade. |
| Press the currently playing cue | Restart that cue from the beginning. |
| Press STOP | Silence audio, cancel loading/pending starts, reset the active cue, and black out an enabled stage window. |
| Cue ends naturally | Return to stopped/silent; keep an enabled stage window black; do not advance. |
| Accepted cue fails to load/play | Stop/silence, black out, and report a useful error. Do not resume the previous cue. |
| Invalid/unauthorized/stale request | Reject it without changing the current playback. |

Retain the output window after STOP and natural completion. Do not close it and expose the desktop. Do not leave the final video frame visible.

Use a persistent black backing surface or overlay that does not depend on a paused frame becoming black. Keep it black during loading, teardown, cue replacement, and errors. Reveal video only for the current valid playback generation.

Use borderless fullscreen on the selected display, not a browser fullscreen page. Preserve aspect ratio with black letterboxing/pillarboxing; do not stretch, crop, or change screen resolution by default. Keep player controls, labels, notifications generated by Smart Stage, and window chrome off the stage output. Hide the cursor over that window while presenting without affecting other displays.

Disabling stage output must first stop playback and then deliberately close/hide the stage window. Normal application exit may also close it. Do not claim blackout survives an application crash, OS failure, or disconnected projector.

## 9. State, command ordering, and STOP safety

Use one authoritative playback coordinator. Keep native loading asynchronous so STOP is never trapped behind a slow file read, preflight operation, or synchronous decoder initialization.

Track at least:

- Process-instance ID, playback state revision, and playlist revision.
- State: `stopped`, `loading`, `playing`, `stopping`, or `error`.
- Active cue ID, position, duration, and last relevant error.
- Selected and resolved output identities; whether stage output is enabled.
- Playback generation and a stop epoch used to invalidate stale requests.

Assign an increasing generation to accepted playback changes. Native callbacks must carry or be associated with their generation. Ignore late readiness, progress, completion, and failure callbacks from replaced/cancelled generations. A delayed load completion must never start playback after STOP.

Use unique request IDs and bounded idempotency tracking. An identical request retry must not restart a cue twice. Reuse of a request ID with different content must be rejected. Two deliberate presses with different IDs remain two commands.

For PLAY, require the current process-instance ID and stop epoch from the latest state. Each accepted STOP increments the stop epoch, invalidates pending starts, and cancels older native work. Reject PLAY requests carrying an earlier epoch, even when network delays make them arrive after STOP. New intentional playback after STOP requires refreshed state and the new epoch.

STOP itself must not be rejected because a client's playback revision/stop epoch is old. It requires valid authorization, but not a matching expected playback state. Duplicate delivery of the same STOP request remains idempotent.

Serialize accepted commands from multiple controllers. The latest accepted valid PLAY supersedes previous loading/playing work; never maintain an implicit queue of cues to play later. Give STOP priority over pending PLAY work and keep its path available when ordinary queues are full. Use bounded queues and explicit overload errors.

An HTTP acknowledgement means a command was accepted, not that media is already playing. Report native playback transitions through server state/events. Preserve a separate visible error if a command was accepted but native execution failed.

## 10. Device loss and local recovery

Refresh device/display availability and react to native hot-plug notifications where supported.

If the active audio endpoint disappears, stop the cue, black out any enabled stage display, and report the loss. Prevent silent fallback to another output. In particular, do not unexpectedly send stage audio through laptop speakers.

If the selected display disappears, stop the cue and invalidate/disarm stage output. Do not let window-manager relocation expose a running video on a different display; hide the lost-display window. Require explicit operator re-selection/re-enabling after reconnection. Keep configured identities available for diagnosis rather than selecting an arbitrary device.

Do not resume media automatically when a device returns. Re-enumeration and preflight must be possible without restarting the application.

Offer a local emergency STOP through the admin UI and Escape when the stage window has keyboard focus. Escape stops and blackens; it does not exit fullscreen. Document Ctrl+C or the normal quit action as application exit, distinct from STOP.

While stage output is enabled, use an appropriate OS power-management assertion to discourage idle sleep/display sleep, and release it on disable/exit. Do not imply the app can prevent a forced sleep, screen lock, system dialog, notification, or OS failure. Include an event-preparation checklist for these risks.

## 11. Media compatibility and preflight

Use native OS decoders. Define a tested baseline rather than promising every file extension will play. Media Foundation documents containers and codecs separately; extension alone is not sufficient validation. [R11]

Initial cross-platform acceptance targets are:

- MP3 audio.
- PCM WAV audio.
- MP4 containing H.264 video and AAC audio.
- MP4 containing H.264 video without an audio track.

These are required test targets, not claims that all encoding profiles or arbitrary damaged files are supported. Specify the tested profiles, sample rates, and limits in `docs/media-compatibility.md`; include a representative 1080p video test. Treat other native-playable formats as best effort unless covered by actual tests. DRM, streaming URLs, transcoding, unusual multichannel routing, and optional codec-pack formats are outside v1.

On cue addition, inspect the real file through the native backend without audible or visible playback. Recheck at startup and when metadata such as size/modification time changes. Provide **Validate all cues** in Admin as a bounded background job that does not obstruct STOP or active playback.

Record validation as `unchecked`, `checking`, `ready`, `missing`, `unsupported`, or `error`, with a useful reason. Determine audio/video from native track metadata, not just the extension. Show unknown duration honestly.

Preflight must attempt native opening/decoder preparation, not only filesystem existence. Do not claim this proves every frame in a long file is intact; document the validation depth and permit an operator rehearsal. Never emit preflight audio or show preflight video on a live output.

A valid PLAY may perform additional preparation when cached information is stale. Retain generation checks throughout. Prefer asynchronous, cancellable preparation to loading whole files into memory. No mandatory cue-preloading/cache subsystem is required for v1.

## 12. Persistence and application data

Store versioned configuration and the playlist in the normal per-user application configuration directory, with `--config-dir` as an override. Do not require administrator access or a writable executable directory. A local JSON store is sufficient; no database server is needed.

Persist cue IDs, labels, canonical file references, order, output preferences, and configuration schema version. Keep derived media inspection data clearly marked as a cache.

Write atomically using a temporary file in the same directory and a platform-correct replacement strategy. Serialize writers. Keep a last-known-good backup and report write failures without pretending edits were saved. If configuration is corrupt, retain the original for recovery and show an actionable error; do not silently erase the show.

After restart, restore the cue list and output choices but remain stopped, silent, and stage-output-disabled. Do not restore a previous playback position or start a cue automatically.

Prevent two processes using the same configuration directory from corrupting state. A native per-user instance lock is sufficient. Release it on normal exit and recover safely from a crashed process.

## 13. HTTP API, real-time updates, and security

Use JSON request/response contracts with documented schemas and error codes. Keep media commands separate from filesystem administration.

A suitable minimum API is:

| Endpoint | Purpose | Role |
| --- | --- | --- |
| `POST /api/pair` | Exchange pairing key for a browser session. | Unpaired, rate-limited |
| `POST /api/logout` | Invalidate the browser session. | Any session |
| `GET /api/state` | Current state and role-appropriate cue list. | Command/Admin |
| `GET /api/events` | Server-sent state updates and heartbeats. | Command/Admin |
| `POST /api/play` | Trigger a cue by ID, with request ID, instance ID, and stop epoch. | Command/Admin |
| `POST /api/stop` | High-priority stop/blackout. | Command/Admin |
| `GET /api/files` | Browse a host directory. | Admin |
| `GET /api/playlist` | Full editable playlist. | Admin |
| `PUT /api/playlist` | Atomically validate/save an edit with expected revision. | Admin |
| `POST /api/validate` | Start a validation job; updates arrive through state/events. | Admin |
| `GET /api/devices` | Audio endpoints and displays. | Admin |
| `PUT /api/outputs` | Configure outputs while stopped. | Admin |
| `POST /api/stage-output` | Explicitly enable/disable the stage window. | Admin |

Minor endpoint refinements are allowed if documented. Never accept an arbitrary media path through the command-role PLAY endpoint: accept a configured cue ID and resolve its file on the server.

Use server-sent events for updates plus ordinary POST commands unless there is a documented reason to choose WebSockets. Send an initial authoritative snapshot, periodic heartbeats, and subsequent changes. On reconnect or revision gaps, fetch a fresh snapshot. Use bounded per-client buffers so a slow or disconnected browser cannot block playback. Do not put command requests in an offline service-worker queue.

Implement separate admin and command pairing keys generated with at least 128 bits of cryptographic randomness. Print them locally at launch, never in URL query strings or persistent request logs. Each browser pairs once per session. Admin sessions can control playback; command sessions cannot browse files, modify playlists, configure outputs, or reveal host paths. Use separate response models rather than accidentally serializing admin objects to controllers.

Issue server-managed, unguessable session cookies with `HttpOnly` and `SameSite=Strict`; use `Secure` when served over HTTPS. Rotate launch keys on process restart, invalidate previous sessions, and provide logout. Sessions should last through a normal event, with a documented lifetime such as 24 hours.

Validate Host and Origin, enforce same-origin requests, and protect mutations with a session-bound CSRF token. Do not enable wildcard CORS. Rate-limit pairing attempts; do not put STOP behind an inappropriate ordinary-command rate limiter.

Validate and bound JSON bodies, string lengths, cue counts, file listings, and connection counts. Render labels/paths as text, not injected HTML. Set suitable Content Security Policy, no-referrer, and frame-embedding restrictions. Never allow labels or filenames to become shell commands.

Plain HTTP on a trusted LAN is the v1 baseline. Pairing and CSRF protection do not encrypt traffic or defend against a network eavesdropper; say this clearly in the UI/documentation. Do not market the HTTP mode as internet-safe or add external TLS infrastructure. Native optional TLS support is acceptable only after the core product works.

## 14. Suggested repository structure

Adapt to an existing repository where necessary. Otherwise use a structure similar to:

```text
cmd/smartstage/
internal/app/
internal/playback/           # coordinator, states, generations, backend contract
internal/platform/darwin/    # Go + compiled-in Objective-C bridge
internal/platform/windows/   # Go bindings or compiled-in native bridge
internal/httpapi/
internal/auth/
internal/files/
internal/store/
internal/web/                # embedded admin and command assets
internal/testbackend/        # test-only fake; never a production fallback
scripts/
docs/
testdata/
```

Keep the Go/native boundary narrow. Specify buffer ownership, string encoding, callback lifetime, and shutdown order. Do not retain Go pointers in native objects in ways prohibited by cgo. [R3]

Keep filesystem scanning, validation, HTTP work, and slow-client processing off the native UI loop and playback control path. Use cancellation and explicit resource release; do not accumulate players, observers, COM objects, windows, or event subscriptions per cue.

A fake backend is useful for unit tests on any OS. It must be explicit and test-only: an unsupported platform or missing real backend must never fall back to simulated successful playback in a release build.

## 15. Build, release, and verification requirements

Provide reproducible build scripts for each target and record exact toolchain versions. Build native integrations on matching OS runners by default; do not assume ordinary Go cross-compilation is sufficient when native SDKs and cgo are involved. [R3]

For macOS, document the required development SDK/compiler and deployment target. For Windows, choose a toolchain actually compatible with the bridge and Go linking strategy. Statically include any necessary non-OS compiler support libraries so release executables do not require a C++ redistributable or a MinGW DLL alongside the program.

Audit release dependencies: use tools such as `otool -L` on macOS and PE import inspection on Windows, and examine transitive non-OS imports where applicable. Save audit output with release verification records. Check that no library is being found only because a developer's machine happens to have it installed.

Keep native debug builds for development; stripped release binaries should include enough application logging and version metadata for diagnosis. Generate checksums. Signing keys and certificates must never be embedded in source.

Document legitimate OS security prompts, code signing, macOS notarization, local-network permissions, and access to protected folders. A native app bundle may be offered as optional macOS packaging for user experience/signing, while retaining the core executable. If real testing reveals that a particular permission workflow needs bundle metadata, report that constraint explicitly rather than making an unsupported promise about a bare executable. Do not instruct users to disable OS security globally.

OS editions/configurations without the required multimedia components are not automatically supported. Detect missing capabilities where possible, report actionable diagnostics, and document prerequisites accurately. Do not download codec packs or application runtimes on the user's behalf.

Test the finished releases on clean supported Windows and macOS machines without Go, compilers, developer SDKs, third-party players, or application-specific libraries installed. The absence of development tools is a release test, not an assumption.

## 16. Implementation milestones

Work in this order, maintaining a short `IMPLEMENTATION_STATUS.md` as you go:

1. **Native feasibility:** real output enumeration, routed audio, routed fullscreen video, stop/blackout, and a proven packaging/toolchain approach for each OS.
2. **Core:** typed backend contract, playback coordinator, command ordering, generation cancellation, persistence, and unit tests.
3. **Web:** LAN startup discovery, pairing/roles, host file browser, playlist editing, output selection, and responsive cue buttons.
4. **Reliability:** real-time updates, reconnect handling, preflight, device loss, races, failure handling, and long-running resource checks.
5. **Release:** build scripts, dependency audits, platform tests, clean-machine verification, and operating documentation.

Deliver real executable behaviour early; do not postpone both native backends until after extensive frontend work. A feasible implementation on one OS does not establish that the other OS is complete.

When the current environment cannot compile or physically test a target, implement the relevant code/build automation as far as possible, run the available tests, and name exactly what remains unverified. Never fabricate successful compilation, hardware routing, performance measurements, signing, or clean-machine test results.

## 17. Acceptance tests

### Required end-to-end scenario, on each supported OS

1. Launch the release executable on a clean supported machine; do not install application runtimes or media tools.
2. Verify it prints the actual LAN Admin and Command URLs and separate pairing keys.
3. Open Admin from a browser, pair, and browse the **host** filesystem.
4. Add two audio files and two video files, assign four custom labels, and reorder them.
5. Select a real non-default audio output and a second display when available.
6. Open Command on a phone/tablet on the same LAN; pair using only the command key.
7. Verify exactly four cue buttons appear in the saved order with the correct labels, plus a continuously visible STOP control.
8. Trigger each cue. Confirm sound physically comes from the chosen output. Confirm video fills only the selected display and its soundtrack uses the chosen audio output rather than an unintended HDMI/default route.
9. Press STOP during audio, video, and loading. Confirm silence, no later restart, and a black stage display rather than the desktop or the last video frame.
10. Allow cues to finish naturally. Confirm no auto-advance and the same silent/black result.
11. Restart Smart Stage. Verify saved configuration/labels/order return, but playback and stage output do not start automatically.

Single-monitor and audio-only machines must also work under their documented limitations. Do not mark second-display or non-default-device routing tested on a machine that lacks those outputs.

### Automated coverage

Include tests for playlist CRUD/revisions, persistence/recovery, role authorization, CSRF/Origin/Host enforcement, path restrictions, label escaping, malformed requests, and duplicate request IDs.

Exercise race sequences including `PLAY A → PLAY B → STOP`, `PLAY A → STOP → late A-ready`, `STOP → delayed old-epoch PLAY`, a retry of an already accepted PLAY, and two different controllers issuing commands. Assert no stale callback revives media and no duplicate HTTP delivery causes a second restart.

Test state/event reconnect behaviour, slow subscribers, failed native initialization, failed loads, missing files, corrupted configuration, and process-instance changes. Run Go race detection where supported and isolate native integration tests appropriately; passing a fake-backend suite is not native playback verification.

Provide small self-authored or clearly licensed media fixtures, with provenance and documented encoding parameters. Do not make tests depend on random public download URLs. Generating fixtures is a development task, not a runtime requirement.

### Physical/manual coverage

Record actual results for Unicode/space-containing paths, audio-only and silent-video files, different monitor arrangements/DPI, cue replacement, active cue removal protection, output unplug/replug, system-default changes, and inaccessible files.

Verify that browsing/validation and a disconnected or slow phone cannot block STOP. Test multiple controllers and a phone sleeping/reconnecting. Confirm no media is transferred to or played by the controller.

Run at least a two-hour soak with repeated cue transitions and STOP operations. Watch for resource growth, duplicate callbacks, leaked handles, frozen windows, and audio/video drift.

As engineering targets on a documented wired-audio, healthy-LAN test setup, aim for server-received STOP to native silence/blackout within 200 ms, and a visible stopped state on the controller within 500 ms of the tap. Measure and report results and audio buffering/network conditions; these are validation targets, not hard-real-time guarantees or promises for Bluetooth outputs. Record cue-start latency rather than inventing a universal start-time guarantee.

## 18. Required repository deliverables

Deliver source code for the Go application and both real native backends, embedded responsive interfaces, automated tests, native test harnesses, media fixtures/provenance, reproducible build scripts, and runnable release artifacts for targets actually built.

Include:

- `README.md`: download/build/run, architecture selection, two URLs, pairing, and first-show setup.
- `docs/architecture.md` and `docs/decisions.md`: native backend choices, threading, state/generation semantics, and trade-offs.
- `docs/api.md`: routes, schemas, authorization, error codes, command acknowledgements, and events.
- `docs/media-compatibility.md`: tested formats/parameters, preflight limits, and known unsupported cases.
- `docs/release-verification.md`: clean-machine/dependency checks, tested OS versions/hardware, measurements, and manual-test results.
- `IMPLEMENTATION_STATUS.md`: what is implemented, built, tested, and still unverified for each target.

Provide a concise final implementation report with exact build/run commands, locations of produced artifacts, test results, and remaining limitations. Do not label the project complete until the required native functionality and acceptance tests are satisfied; a useful partial implementation must be reported as partial.

## 19. Scope boundaries and final completion rule

Do not add automatic playlist advancement, overlapping cues, crossfades, looping, pause/seek controls, scheduling, MIDI/OSC/DMX, multiple independent stage outputs, media editing, streaming services, file uploads, cloud sync, mobile apps, or a plugin system in v1. Do not add these instead of finishing native playback and reliable STOP.

The defaults deliberately chosen for v1 are: manually triggered cues; one active cue at a time; restart on a repeated intentional cue press; one global audio output; one stage display; persistent black after STOP/end; and paired LAN access. Implement those defaults without converting the product into a different workflow.

**Completion means that an operator can download Smart Stage, run it on a supported PC or Mac, configure four labelled host-media cues in Admin, and reliably trigger or stop them from a phone with correct physical audio/display routing and no extra application runtime to install.**

---

## Technical references for implementation

These primary references explain platform mechanisms, not proof that an implementation has passed testing. Check current SDK availability and behaviour during development. Product defaults, APIs, security rules, and acceptance targets above are implementation requirements rather than claims quoted from these sources.

- **[R1] Go — `embed` package:** `https://pkg.go.dev/embed`
- **[R2] Apple — Technical Q&A QA1118, static linking terminology and support (archived guidance):** `https://developer.apple.com/library/archive/qa/qa1118/_index.html`
- **[R3] Go — cgo documentation, native compilation and pointer rules:** `https://go.dev/src/cmd/cgo/doc.go`
- **[R4] Apple — AVPlayer:** `https://developer.apple.com/documentation/avfoundation/avplayer`
- **[R5] Apple — AVPlayer audioOutputDeviceUniqueID:** `https://developer.apple.com/documentation/avfoundation/avplayer/audiooutputdeviceuniqueid`
- **[R6] Go — OS-thread affinity guidance:** `https://go.dev/wiki/LockOSThread`
- **[R7] Microsoft — EVR guidance and recommendation to use MediaPlayer/IMFMediaEngine for new playback code:** `https://learn.microsoft.com/en-us/windows/win32/medfound/enhanced-video-renderer`
- **[R8] Microsoft — MediaPlayer.AudioDevice:** `https://learn.microsoft.com/en-us/uwp/api/windows.media.playback.mediaplayer.audiodevice`
- **[R9] Microsoft — Media Session playback tutorial:** `https://learn.microsoft.com/en-us/windows/win32/medfound/how-to-play-unprotected-media-files`
- **[R10] Microsoft — MF_AUDIO_RENDERER_ATTRIBUTE_ENDPOINT_ID:** `https://learn.microsoft.com/en-us/windows/win32/medfound/mf-audio-renderer-attribute-endpoint-id-attribute`
- **[R11] Microsoft — native Media Foundation containers and codecs:** `https://learn.microsoft.com/en-us/windows/win32/medfound/supported-media-formats-in-media-foundation`
