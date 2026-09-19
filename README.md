# Smart Stage

Trigger local music/video cues on a Windows PC or Mac from a phone, tablet or
desktop browser. Native playback stays on the host. One executable includes the
browser interfaces and native bridge. No Go, Node, Python, database, player or
extra application runtime is needed on the event computer.

**Preview:** built and exercised through real OS APIs on all four target runners.
Physical routing, visible projector blackout, hotplug, latency, clean-machine
acceptance and the two-hour soak remain unverified. Windows CI has no audio
endpoints; Mac CI uses virtual/null audio. Read [verification](docs/release-verification.md)
and [implementation status](IMPLEMENTATION_STATUS.md). This is not an accepted
production release.

## Install or download

[Executables and checksums](https://github.com/arizzi74/Smart-Stage/releases/tag/v0.1.0-preview.3)
are provided for macOS ARM64 (Apple Silicon), macOS AMD64 (Intel), Windows ARM64
and Windows AMD64. Windows 11 is the intended baseline. Mac builds target 12.0
and have been exercised on macOS 15.7.9 CI, not every intervening OS version.

macOS Terminal:

```sh
curl -fsSL https://raw.githubusercontent.com/arizzi74/Smart-Stage/main/install.sh | sh
```

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/arizzi74/Smart-Stage/main/install.ps1 | iex
```

Installers choose native architecture, verify SHA-256, and install per user
without elevation. Mac: `~/.local/bin/smartstage`; Windows:
`%LOCALAPPDATA%\SmartStage\bin\smartstage.exe`. Mac login profiles/Windows user
PATH are updated. On Mac, use the printed full path immediately or open a new
login terminal. `SMARTSTAGE_INSTALL_DIR` and `SMARTSTAGE_VERSION` override the
destination and version. Source is available for inspection. Installers do not
disable security or firewall settings.

Mac binaries are ad-hoc signed, not Developer ID signed/notarized; Windows
binaries are not Authenticode signed. Legitimate security/local-network/protected
folder prompts may require per-app approval. Clean-machine permission behavior
remains part of acceptance testing.

## First show

1. Run `smartstage` (`smartstage.exe` on Windows) in the signed-in interactive
   desktop session. Keep the host terminal/application running.
2. Open the actual **Admin URL printed by your host**, for example
   `http://192.168.1.42:8787/admin`, and enter its Admin pairing key.
3. In Host files, browse the **host computer**, inspect/select files and add them.
   Set labels/order in Playlist. Wait for native validation; missing/unsupported
   files remain visible. Adding a cue does not upload or copy its file.
4. Select audio and stage outputs, then Save outputs. Explicitly acknowledge
   primary/only-display coverage. Audio works without a stage display. Use an
   extended desktop for independent projection; mirrored displays are identified.
5. Open the printed **Command URL**, for example
   `http://192.168.1.42:8787/command`, on your phone/tablet and use the different
   Command pairing key. Each large button starts/restarts that cue.
6. STOP silences and retains an enabled black stage. Natural completion does the
   same, without auto-advance. Enable/Disable stage output are separate actions.

Example addresses are not hard-coded. Multiple interfaces are listed with their
names, including IPv4 link-local addresses on networks without DHCP; select the
reachable network. IPv6 URLs require a global or unique-local address; scoped
IPv6 link-local URLs are not advertised to remote browsers.
Guest Wi-Fi isolation can prevent access;
firewall/local-network permissions may need approval. HTTP is **not encrypted**:
use a trusted LAN, without internet port forwarding. Pairing/CSRF do not protect
against a network eavesdropper.

Escape while the stage window has focus stops without exposing the desktop.
Ctrl+C/normal exit closes the stage window. Losing browser connections or a
sleeping phone does not stop playback. Unacknowledged STOP is shown as unconfirmed;
PLAY is never queued or replayed on reconnect.

## Flags and saved data

| Flag | Default / purpose |
| --- | --- |
| `--port` | `8787`; port conflicts fail clearly |
| `--bind` | `0.0.0.0`; choose a local IP to restrict the interface |
| `--advertise-ip` | Empty; emphasize one valid reachable local address |
| `--config-dir` | Per-user `SmartStage` under `os.UserConfigDir()` |
| `--media-root` | All host-readable locations; repeat to restrict roots |
| `--log-level` | `info`; also `debug`, `warn`, `error` |
| `--version` | Print version/commit/Go/platform and exit |

Typical data locations: `~/Library/Application Support/SmartStage` on macOS,
`%APPDATA%\SmartStage` on Windows. Successful edits save `state.json` with a
`state.json.bak` last-known-good snapshot. Corruption stops startup and retains
the original; inspect/save it before restoring a backup. One process may use a
configuration directory. Restart restores cues/preferences, but is always
stopped, silent and stage-disabled. Pairing keys rotate at each launch.

```sh
smartstage --media-root '/Users/operator/Show' --port 8787
```

```powershell
smartstage.exe --media-root 'C:\Show' --port 8787
```

## Build and test

Development prerequisites: Go 1.26.5, Python 3 for audits, Xcode command-line
tools/SDK on Mac or LLVM-MinGW **20260908 UCRT** on Windows. Use Git Bash on
Windows and put the matching compiler `bin` on PATH. The app has no external Go
module dependencies. These tools are not end-user requirements.

```sh
go test -race ./...
go vet ./...
bash scripts/build.sh darwin arm64
bash scripts/build.sh darwin amd64
bash scripts/build.sh windows amd64
bash scripts/build.sh windows arm64
```

Build on matching OS runners normally. `DEBUG=1` retains symbols;
`VERSION=v0.1.0-preview.3` sets metadata. Output:
`dist/smartstage-<os>-<arch>[.exe]` and engineering harness
`dist/native-harness-<os>-<arch>[.exe]`, with adjacent checksums, import audits
and toolchain records. Apple system frameworks remain dynamic; Windows compiler
support is linked in. `CGO_ENABLED=0` cannot make a playback release.

```sh
./dist/native-harness-darwin-arm64 --list
./dist/native-harness-darwin-arm64 --inspect '/Users/operator/Show/opening.wav'
./dist/native-harness-darwin-arm64 --file '/Users/operator/Show/intro.mp4' \
  --audio 'device UID from --list' --display 'display UUID from --list'
python3 scripts/native-smoke.py dist/native-harness-darwin-arm64
python3 scripts/application-smoke.py dist/smartstage-darwin-arm64
```

Use the matching Windows `.exe`/paths there. Harness commands: `stop`, `play`,
`enable`, `disable`, `quit`. `--stop-after`, `--stop-playing-after` and
`--exit-after` accept Go durations. The harness always uses real native APIs.

Browser tests use development-only Playwright 1.63.0/Chromium. On a supported
native host, exercise the actual executable through Admin and Command:

```sh
npm ci --prefix scripts/browser --ignore-scripts --no-audit --no-fund
node scripts/browser/node_modules/playwright/cli.js install chromium
node scripts/browser-native-smoke.cjs dist/smartstage-darwin-arm64
```

Use the matching executable path/architecture on Windows or Intel Mac. This
uses a real non-loopback host address and native playback; it saves browser
screenshots and a result under `dist/browser-native/`. It does not establish
physical routing or phone/LAN compatibility. The separate synthetic browser
fixture covers timing/reconnect/layout cases:
`NODE_PATH=/path/to/node_modules node scripts/browser-smoke.cjs`.

Read [architecture](docs/architecture.md), [decisions](docs/decisions.md),
[API](docs/api.md), [media compatibility](docs/media-compatibility.md), and
[release verification/event checklist](docs/release-verification.md).
The [acceptance audit](docs/acceptance-audit.md) maps specification requirements
to actual evidence and identifies the open physical checks.
