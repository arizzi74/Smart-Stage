# Release verification

Status: **preview; physical/clean-machine acceptance incomplete**. CI executes
real native APIs but cannot verify what a human sees or hears on event hardware.

## Recorded evidence

[Preview 5](https://github.com/arizzi74/Smart-Stage/releases/tag/v0.1.0-preview.5)
is source `9e72b4cc614f675dd7e52fe863d7973f5fb3fb65`. Its packaging changes passed
[native run 35492226235](https://github.com/arizzi74/Smart-Stage/actions/runs/35492226235)
on all four targets, including shared race/vet, native imports/media/HTTP checks
and the new icon verification. Windows Shell extracted large and small icons;
all nine embedded image sizes matched the original ICO bytes. Both optional Mac
app archives passed native ICNS decoding and strict ad-hoc signature checks,
contained the exact standalone executable bytes and started through Finder in
Terminal from a directory containing spaces, an apostrophe and Unicode. The
test matched the HTTP listener to the bundled executable's kernel-reported path.

[Tagged run 35492382186](https://github.com/arizzi74/Smart-Stage/actions/runs/35492382186)
repeated those checks for the release-version binaries, published 36 assets and
passed version-selected curl/irm installation and actual Admin/Command browser
checks on all four targets. Independent downloads of all four executables and
both Mac app archives passed SHA-256 checks; binary architecture, Go version,
module release, clean VCS revision, bundled executable/icon identity and archive
executable permissions were also verified. The native icon records refer to
the same published file hashes. Retained reports:
[`verification/release-preview5/`](verification/release-preview5/).

The icon and optional launcher are packaging changes. Playback code is unchanged
from preview 4; the longer memory/audio/display measurements below remain
evidence for their explicitly named preview 4 binaries, not a new preview 5 soak
or physical/clean-machine acceptance.

[Preview 4](https://github.com/arizzi74/Smart-Stage/releases/tag/v0.1.0-preview.4),
source `505a1e495075dcc01abe6d7367d3b57d3f8e60a9`, passed
[tag run 35479742963](https://github.com/arizzi74/Smart-Stage/actions/runs/35479742963):
shared Go race/vet checks, four native builds/import audits/media checks,
HTTP/native application checks and release publication, followed by curl/irm
installation and real browser checks on all four targets. The browser tests
use the same workflow and coverage described below, against the published
preview 4 executables. Runtime output reports `v0.1.0-preview.4 (505a1e495075)`.
Browser result JSONs are retained alongside the download verification below;
their full screenshots remain in this run's `browser-native-*` Actions artifacts.

Downloaded files in `dist/releases/v0.1.0-preview.4/` passed checksum, architecture,
Go/module-version and clean-source-commit checks. The exact sizes, hashes and
embedded build information are retained in
[`verification/release-preview4/download-verification.json`](verification/release-preview4/download-verification.json).
The one-command installers defaulted to preview 4 at that release. The source's completed
20-minute memory profile and completed four-target two-hour soak are described in
[resource results](soak-results.md); captured native virtual pixels and the
Windows ARM64 setup-screen limitation are in [display results](native-display-results.md).

After the default changed, [installer run 35480086360](https://github.com/arizzi74/Smart-Stage/actions/runs/35480086360)
and [browser run 35480086365](https://github.com/arizzi74/Smart-Stage/actions/runs/35480086365)
passed on all four targets at installer/test commit `a95a902`, without setting a
version override. Public raw installer URLs were also checked to serve preview 4.

[Installer run 35484067090](https://github.com/arizzi74/Smart-Stage/actions/runs/35484067090)
at test commit `214e15e` additionally passed the exact `irm | iex` command under
built-in **Windows PowerShell 5.1** on AMD64 and ARM64. Both reported Desktop
edition: `5.1.26100.33296` on AMD64 and `5.1.26100.9457` on ARM64.
No version or install-directory override was
set: the executable was installed at `%LOCALAPPDATA%\SmartStage\bin\smartstage.exe`,
reported preview 4/source `505a1e495075` in the correct native architecture and
served the Command page. A second installation exercised atomic replacement,
retained the expected published checksum, left exactly one user PATH entry and
removed temporary installer directories. PowerShell 7 and both Mac curl checks
also passed in that run. These hosted machines still contain development tools;
the result is installer compatibility evidence, not clean-machine acceptance.
Raw reports: [`verification/install-powershell51-214e15e/`](verification/install-powershell51-214e15e/).

[Windows audio run 35482038438](https://github.com/arizzi74/Smart-Stage/actions/runs/35482038438)
adds actual native audio-renderer event checks on both Windows architectures:
all four cues and 120 seconds of transitions passed against published preview 4,
using a signed virtual endpoint on disposable runners. A subsequent endpoint
meter check observed six signal/STOP/replay cycles per architecture but failed
its isolation assertion on the shared virtual cable. [Audio results](windows-audio-results.md)
retain the exact setup, raw samples, session diagnostics and pre-reboot limitation.
The stock-runner release/browser coverage below remains unchanged.

Earlier evidence follows, retained with its own source and release identity.

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

Native [captured-pixel observations](native-display-results.md) at renderer-fix
source `505a1e4` passed video/restart/STOP/end checks on both Mac virtual
desktops and Windows AMD64. A small Mac OS indicator remains visible. Windows
ARM64 captured Windows first-run setup and failed the visual assertions. These
results add virtual-pixel evidence without establishing physical outputs or latency.

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

The harness natural-end check now waits for the native `ended` event with a
30-second bound and records observed event times. In earlier
[run 35479026373](https://github.com/arizzi74/Smart-Stage/actions/runs/35479026373),
the Windows AMD64 harness exited at its fixed six-second process deadline while
the last playback position was only 2.60/3.00 seconds; that run failed its end
assertion. [Run 35479868178](https://github.com/arizzi74/Smart-Stage/actions/runs/35479868178)
at test source `4305547` passed the revised check on all four targets, with
observed end events 3.42–4.25 seconds after process start. It retains the required
end/stage-enabled assertions and does not change application code. Raw records
are in [`verification/native-smoke-4305547/`](verification/native-smoke-4305547/).
These process/event observations are not physical cue-start or STOP latency.

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
- Physical two-hour A/V stability, including drift and resource behavior on
  event hardware. [Preview 4 soak 35478342253](https://github.com/arizzi74/Smart-Stage/actions/runs/35478342253)
  completed all four native transition/STOP loops for at least 7,200 seconds,
  with 121 resource samples and successful restart checks at exact source
  `505a1e4`. [Resource analysis](soak-results.md) records substantially reduced
  Mac RSS trends (+0.34/+1.08 MiB/hour after minute 15 on AMD64/ARM64), together
  with the heap/idle evidence supporting the renderer fix. Windows memory and
  handle traces fluctuate with positive fitted trends; the stock runners had
  no audio endpoints. All earlier runs retain their own source identity and
  evidence. These observations do not establish physical A/V drift, routed
  sound, a two-hour Windows audio workload or leak-free behavior in every case.
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
