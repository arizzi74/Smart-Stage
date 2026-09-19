# Release verification

Status: **preview; physical/clean-machine acceptance incomplete**. CI executes
real native APIs but cannot verify what a human sees or hears on event hardware.

## Recorded evidence

[Preview 3](https://github.com/arizzi74/Smart-Stage/releases/tag/v0.1.0-preview.3),
commit `d70b3e2`, passed [tag run 35471043955](https://github.com/arizzi74/Smart-Stage/actions/runs/35471043955):
Go race/vet checks, all four native builds/import audits/media checks, real
application HTTP/native checks including shutdown during validation, release
publication, then curl/irm installation and HTTP startup on all four platforms.
Downloaded executable checksums, architecture headers, module version and VCS
commit were independently verified in `dist/releases/v0.1.0-preview.3/`.
Runtime version/commit output is also recorded by the four installer jobs.
Runner output availability and
coverage match the table below.

[Default installer run 35471309014](https://github.com/arizzi74/Smart-Stage/actions/runs/35471309014)
passed the actual `curl | sh` / `irm | iex` installation, version and HTTP startup
checks on all four runners. Hosted runners contain development tools; these
checks are not clean-machine evidence.

| Target | Runner environment / output availability | Actual application smoke coverage |
| --- | --- | --- |
| macOS ARM64 | macOS 15.7.9 (24G830); Apple virtual and Null Audio endpoints; one 1024×768 virtual display | Four cues configured/reordered, all four natively played/stopped, restart restores show while stopped/stage-disabled |
| macOS AMD64 | macOS 15.7.9 (24G830); Null Audio endpoint; one 1920×1080 virtual display | Same four-cue playback/STOP and restart checks |
| Windows AMD64 | `windows-2025` hosted runner; **no audio endpoints**; 1024×768 Hyper-V display | Four cues configured/reordered; silent video natively played/stopped; restart checked. Audio-renderer playback was skipped, not verified. |
| Windows ARM64 | `windows-11-arm` hosted runner; **no audio endpoints**; 1024×768 Hyper-V display | Same silent-video application checks; audio-renderer playback skipped. |

All targets natively inspected WAV, MP3, 1080p H.264/AAC MP4 and silent H.264 MP4,
and rejected damaged media. Harness tests additionally rejected unavailable
audio/display IDs without reporting playing, checked native natural completion
and reported persistent stage-enabled state. Native status is not
evidence of physically black pixels or routed sound. Mac ARM64 selected the
non-default Null endpoint; this is native routing to a virtual device only.

[Windows capability run 35473613211](https://github.com/arizzi74/Smart-Stage/actions/runs/35473613211)
clarifies the audio limitation on both runner architectures. `Audiosrv` and
`AudioEndpointBuilder` were **Running**, startup mode **Auto**, exit code 0.
`Win32_SoundDevice` and the Media/AudioEndpoint PnP lists were empty. On the same
machines, the installed preview 3 executable reported zero audio endpoints and
passed silent-video playback/STOP plus restart checks. No service/device settings
were changed and no audio driver was installed. These runners lack an installed
sound device; this is not evidence of successful Windows audio-renderer playback.
Recorded OSes: Windows Server 2025 Datacenter build 26100 on AMD64 and Windows 11
Enterprise build 26200 on ARM64. The `windows-audio-amd64`/`windows-audio-arm64`
artifacts contain the capability and application reports; local copies are in
`dist/ci-evidence/windows-audio-502f759/`.

Toolchains: Go 1.26.5; Macs used Xcode 16.4 (16F6), Apple Clang 17.0.0
(clang-1700.0.13.5), SDK 15.5, deployment target 12.0. Windows used LLVM-MinGW
20260908 UCRT / Clang 23.1.1. Full toolchain/import, native harness and application
smoke records accompany the published preview 3 executables.

Local development: Ubuntu 22.04.5 ARM64; Windows cross-builds also succeed.
PE audits found only OS libraries (19 AMD64 imports, 18 ARM64 imports).
Mac `otool -L` audits likewise allow only OS frameworks/libraries. Compiler
support is linked into Windows executables. Audits cannot prove every eventual
OS/plugin load: clean-machine testing remains mandatory.

`go test -race ./...` and `go vet ./...` pass on Linux ARM64. Coverage includes
generation cancellation, epochs, duplicate/conflicting IDs, multiple controllers,
revision conflicts, active-source protection, persistence/corruption/backup/lock,
canonical roots, roles, CSRF/Origin/Host, malformed bodies, path redaction,
overload-independent STOP and authoritative SSE reconnects. New coverage checks
the TCP cap and shutdown, IPv4 link-local discovery, actual listing truncation,
rejection of oversized saves without losing the show/backup, invalid stored
labels and refusal to start an unsupported/no-cgo backend.

Chromium browser checks use a **synthetic HTTP fixture**, separate from native
tests. They passed at widths 320/390/768/844/1280: pairing, four wrapped labels,
escaping, visible STOP, STOP during pending PLAY, failed offline STOP,
server-authoritative highlighting, reconnect without replay, gap refresh and
Admin layout. Screenshots/results: `dist/browser-checks/`.

An additional [real browser run 35472402760](https://github.com/arizzi74/Smart-Stage/actions/runs/35472402760)
passed on all four targets against the **published preview 3 executables**.
Test source: `983cdef`; application source: `d70b3e2`. Playwright 1.63.0 and
Chromium 153.0.8010.12 opened separate Admin/Command sessions at an actual
non-loopback host IPv4 address over plain HTTP (`isSecureContext === false`).
Admin selected four real host files, saved four labels/reordered cue IDs and
selected native outputs. Command displayed the saved order, sent one PLAY per
tap, and received actual native playing/STOP/natural-completion state. The test
checked Command path redaction/access restrictions, no media transfer/elements,
and visible STOP at 390×844. Admin was exercised at 1280×900.

Both Macs natively played/stopped all four cues. Both Windows runners played
silent video, then requested audio and verified a visible missing-output error
with STOP recovery. Windows audio playback was not verified. Node versions were
22.23.2 on both Macs/Windows AMD64 and 24.21.0 on Windows ARM64; Node ran in each
runner's native architecture. The browser ran on the host, so this is not a
physical phone/Wi-Fi test. These screenshots show browser UI, not native output
pixels. Local evidence: `dist/ci-evidence/browser-983cdef/`; each job also exposes
its `browser-native-<os>-<arch>` artifact with result JSON and screenshots.

[Repeat/reporting run 35472755329](https://github.com/arizzi74/Smart-Stage/actions/runs/35472755329)
at test commit `ead78c6` passed Go race/vet and all four native jobs. Main CI now
includes ten seconds of repeated native PLAY/replacement/STOP before the saved
show restart check. Downloaded reports contain completed runs of 10.13–10.59
seconds, 19/20 cycles on Mac AMD64/ARM64 and 24/26 cycles on Windows AMD64/ARM64,
with initial/final resident-memory samples and Windows handle counts. This is
a short regression check, not long-running stability proof. The test script
now checkpoints incomplete reports and prints resource samples every minute;
it retains evidence if a future repeat loop fails or is interrupted. Neither
already-running two-hour job uses this later reporting change.

## Reproduce

```sh
go test -race ./...
go vet ./...
bash scripts/build.sh darwin arm64
bash scripts/build.sh darwin amd64
bash scripts/build.sh windows amd64
bash scripts/build.sh windows arm64
python3 scripts/native-smoke.py dist/native-harness-darwin-arm64
python3 scripts/application-smoke.py dist/smartstage-darwin-arm64
NODE_PATH=/path/to/playwright/node_modules node scripts/browser-smoke.cjs
npm ci --prefix scripts/browser --ignore-scripts --no-audit --no-fund
node scripts/browser/node_modules/playwright/cli.js install chromium
node scripts/browser-native-smoke.cjs dist/smartstage-darwin-arm64
```

For repeated native transitions, add a duration in seconds to
`scripts/application-smoke.py`, for example
`python3 scripts/application-smoke.py dist/smartstage-darwin-arm64 7200`.
The adjacent `.http-smoke.json` records requested/elapsed time, completed cycles
and resource samples; current scripts mark the repeat loop `completed` only
after its full duration. Check the process/workflow result and subsequent
restart result too. A partial report or ten-second CI run cannot establish the
two-hour requirement.

Use matching OS runners normally. Windows Linux cross-builds are additional
compile/import checks. Go/native SDKs/Python/Playwright are development-only.
`DEBUG=1` retains symbols; `VERSION=...` and Git commit identify the build.
`scripts/audit-dependencies.py` saves and checks PE imports/`otool -L` output.
SHA-256 files describe executable bytes after Mac ad-hoc signing.

## Remaining acceptance work

These are **unverified**, not assumed passed:

- Clean Windows 11 AMD64/ARM64 and both Mac architectures without Go, compilers,
  developer SDKs, players, extra runtimes or application libraries.
- Physical non-default audio output for audio and video soundtracks; second
  display; extended/mirrored layouts, negative coordinates, mixed DPI/rotation.
- Actual silence/black pixels during loading/STOP/end/error/replacement. No
  physical STOP latency (target 200 ms server-to-silence/black) or controller
  latency (target 500 ms tap-to-stopped) has been measured. Bluetooth buffering
  and hard real-time behavior are not guaranteed.
- Audio/display unplug/replug, system-default changes during playback, no
  fallback, no visible window relocation and explicit re-selection on return.
- Actual phones/tablets on a LAN; sleeping/reconnecting or slow controllers;
  validation/filesystem work under event conditions; inaccessible/protected
  folders and missing drives.
- A two-hour native soak measuring resources, callback/handle growth, frozen
  windows and audiovisual drift. [Preview 3 soak 35471045100](https://github.com/arizzi74/Smart-Stage/actions/runs/35471045100)
  is running a two-hour native transition/STOP/resource test at release commit
  `d70b3e2`. The earlier [run 35468888430](https://github.com/arizzi74/Smart-Stage/actions/runs/35468888430)
  at `20bcf35` is also running; its results apply to that earlier source only.
  Results and resource analysis are pending. Even a passing CI
  result cannot establish physical A/V drift or routed sound.
- Production signing/notarization and the bare-executable permission workflow.

Windows 11 is the intended validation baseline. Mac deployment target 12.0 is
not a physically established minimum; CI ran macOS 15.7.9. Do not infer support
for every older OS revision. Mac binaries are ad-hoc signed, not Developer ID
signed/notarized. Windows binaries are not Authenticode signed. Standard OS
prompts can appear; use documented per-app approval, never disable OS security
globally. If protected-folder/local-network permissions need bundle metadata on
real machines, record that constraint before claiming bare-executable support.

## Event preparation

Rehearse exact files and outputs. Use extended desktop for independent stage
projection. Check physical blackout and a reachable STOP control. Verify trusted
LAN reachability, guest-network isolation, firewall and local-network permissions;
do not configure internet port forwarding. Prepare power, notifications, locks,
forced sleep and display arrangement. Native power assertions cannot suppress
OS dialogs/forced sleep. Keep a recovery procedure: application crash, OS failure,
disconnected projector or power loss can expose the desktop. Quit closes stage.
