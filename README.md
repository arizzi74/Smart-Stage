# Smart Stage

Trigger local music/video cues on a Windows PC or Mac from a phone, tablet or
desktop browser. Native playback stays on the host. One executable includes the
browser interfaces and native bridge. No Go, Node, Python, database, player or
extra application runtime is needed on the event computer.

**Preview:** built and exercised through real OS APIs on all four target runners.
Physical routing, visible projector blackout, hotplug, latency, clean-machine
acceptance and physical A/V stability remain unverified. All four targets
completed a two-hour native CI workload; Mac memory growth was substantially
reduced. Stock Windows CI lacks audio endpoints, while a separate signed virtual
driver evaluation observed audio signal/STOP/replay; its meter isolation check
failed on shared driver meters. Mac CI uses virtual/null audio. Read [verification](docs/release-verification.md)
and [implementation status](IMPLEMENTATION_STATUS.md). This is not an accepted
production release.

## Install on Mac

Open Terminal, paste this command, and press Return:

```sh
curl -fsSL https://raw.githubusercontent.com/arizzi74/Smart-Stage/main/install.sh | sh
```

The installer detects Apple Silicon or Intel, downloads the matching preview 8
app ZIP, verifies its SHA-256 checksum, and installs **Smart Stage.app** with its
icon into `~/Applications`. It removes `com.apple.quarantine` only from that app
and its contents, then launches it. Copying the app needs no administrator
password, and no extra runtime is required. To allow phone/tablet connections, the installer
also configures Smart Stage's incoming-connection rule in the macOS firewall.
macOS may request administrator authorization for that rule. The firewall stays
enabled and other applications' rules are preserved. Saved shows are retained.

Admin opens automatically in your system browser. You can close Terminal after
installation: the app runs independently, without a Terminal window. Its icon
appears in the Dock. Right-click the Dock icon and choose **Quit**, or use
**Smart Stage → Quit Smart Stage** when the app is active. Clicking the Dock icon
reopens Admin. Its additional menu bar control provides Admin, logs and Quit. Later, open
**Smart Stage.app** from the Applications folder inside your home folder. Quit
the app before running the installer again to update it.

This deliberately removes this app's quarantine check; it does not provide Apple
notarization or disable Gatekeeper system-wide. Other macOS permissions, such as
local-network access, may still need approval.

## Download ZIPs

Download the ZIP for your computer, extract it, and open the executable inside.
Each standalone ZIP below contains just the single Smart Stage
executable, including both browser interfaces and native playback.

| Computer | Download |
| --- | --- |
| Mac — Apple Silicon (ARM64) | [smartstage-darwin-arm64.zip](https://github.com/arizzi74/Smart-Stage/releases/download/v0.1.0-preview.8/smartstage-darwin-arm64.zip) |
| Mac — Intel (AMD64) | [smartstage-darwin-amd64.zip](https://github.com/arizzi74/Smart-Stage/releases/download/v0.1.0-preview.8/smartstage-darwin-amd64.zip) |
| Windows — ARM64 | [smartstage-windows-arm64.zip](https://github.com/arizzi74/Smart-Stage/releases/download/v0.1.0-preview.8/smartstage-windows-arm64.zip) |
| Windows — Intel / AMD (AMD64) | [smartstage-windows-amd64.zip](https://github.com/arizzi74/Smart-Stage/releases/download/v0.1.0-preview.8/smartstage-windows-amd64.zip) |

[Release notes and checksums](https://github.com/arizzi74/Smart-Stage/releases/tag/v0.1.0-preview.8).
Windows executables include the Smart Stage icon. Mac app bundles with the icon
are also available directly: [Apple Silicon app ZIP](https://github.com/arizzi74/Smart-Stage/releases/download/v0.1.0-preview.8/smartstage-darwin-arm64.app.zip)
and [Intel app ZIP](https://github.com/arizzi74/Smart-Stage/releases/download/v0.1.0-preview.8/smartstage-darwin-amd64.app.zip).
Each app bundle includes the same core executable plus its Finder launcher and
icon. Opening `Smart Stage.app` runs it without Terminal and opens Admin. The
standalone ZIPs above contain only `smartstage` (Mac) or `smartstage.exe` (Windows).

Mac files downloaded through your browser are still unnotarized and may require
**System Settings → Privacy & Security → Open Anyway** after the first launch
attempt. The Mac installation command above handles the app-specific quarantine
removal automatically.

Windows 11 is the intended baseline. Mac builds target macOS 12.0 and have been
exercised on macOS 15.7.9 CI, not every intervening OS version. Mac binaries are
ad-hoc signed, not Developer ID signed/notarized; Windows binaries are not
Authenticode signed. Legitimate security/local-network/protected-folder prompts
may require per-app approval. Clean-machine permission behavior remains part of
acceptance testing.

## First show

1. On Mac, use the installation command above or open the extracted app. For a
   standalone ZIP, open `smartstage` (`smartstage.exe` on Windows) in the
   signed-in interactive desktop session. Keep the application running. It
   automatically opens **Admin** in your system browser at
   `http://127.0.0.1:8787/admin`. Admin is available only on the host computer.
2. In Host files, browse the **host computer**, inspect/select files and add them.
   Set labels/order in Playlist. Wait for native validation; missing/unsupported
   files remain visible. Adding a cue does not upload or copy its file.
3. Select audio and stage outputs, then Save outputs. Explicitly acknowledge
   primary/only-display coverage. Audio works without a stage display. Use an
   extended desktop for independent projection; mirrored displays are identified.
4. In Admin, find **Remote control**. Scan its QR code with a phone/tablet camera
   on the same network, or copy its URL. That URL includes a random numeric token
   and opens the remote controls without typing a separate pairing key. Use the
   address for the network your phone can reach.
5. Tap a cue to start/restart it. STOP silences and retains an enabled black stage.
   Natural completion does the same, without auto-advance. Enable/Disable stage
   output are separate actions.

Admin listens only on `127.0.0.1`. Remote controls use port `8788` by default;
Admin pages and administration APIs are unavailable on that listener. The remote
URL and token change on each application launch; scan the new QR code after a
restart. Anyone with the remote URL can control playback, so keep it within your
show team. HTTP is **not encrypted**: use a trusted LAN without internet port
forwarding. The token does not protect against a network eavesdropper.

Multiple interfaces are listed with their names, including IPv4 link-local
addresses on networks without DHCP; select the reachable network. IPv6 URLs
require a global or unique-local address; scoped IPv6 link-local URLs are not
advertised to remote browsers. Guest Wi-Fi isolation can prevent access;
firewall/local-network permissions may need approval.

If a remote URL shows `ERR_EMPTY_RESPONSE`, first check the two listeners on the
Mac while Smart Stage is running:

```sh
lsof -nP -iTCP:8787 -iTCP:8788 -sTCP:LISTEN
curl -q --noproxy '*' --connect-timeout 3 --max-time 5 -v -o /dev/null http://127.0.0.1:8787/admin
curl -q --noproxy '*' --connect-timeout 3 --max-time 5 -v -o /dev/null http://127.0.0.1:8788/command
```

For the complete read-only Mac diagnostic, including firewall state and address
checks, run:

```sh
curl -fsSL https://raw.githubusercontent.com/arizzi74/Smart-Stage/main/scripts/diagnose-macos.sh | sh -s -- 192.168.1.100
```

Replace `192.168.1.100` with the address shown in your own Admin page.

Repeat the second `curl` above with the LAN address shown in Admin instead of
`127.0.0.1`. All three should return `HTTP/1.1 200 OK`. No token is needed to
load the remote page; pairing happens afterward. A failed connection therefore
needs diagnosis before changing the token. For an app launch, use **View Log**
in the menu bar control or inspect `~/Library/Logs/Smart Stage/smartstage.log`.

Escape while the stage window has focus stops without exposing the desktop.
Quit Smart Stage (or Ctrl+C for a Terminal launch) closes the stage window.
Losing browser connections or a sleeping phone does not stop playback.
Unacknowledged STOP is shown as unconfirmed;
PLAY is never queued or replayed on reconnect.

## Flags and saved data

| Flag | Default / purpose |
| --- | --- |
| `--admin-port` | `8787`; Admin always binds to `127.0.0.1` |
| `--port` | `8788`; remote-control port; conflicts fail clearly |
| `--bind` | `0.0.0.0`; choose a local IP for remote controls |
| `--no-browser` | Suppress automatically opening the local Admin page |
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
stopped, silent and stage-disabled. Remote-control tokens and browser sessions
rotate at each launch.

Advanced examples, run from the folder containing the extracted executable:

```sh
./smartstage --media-root '/Users/operator/Show'
```

```powershell
.\smartstage.exe --media-root 'C:\Show'
```

## Build and test

Development prerequisites: Go 1.26.5, Python 3 for audits, Xcode command-line
tools/SDK on Mac or LLVM-MinGW **20260908 UCRT** on Windows. Use Git Bash on
Windows and put the matching compiler `bin` on PATH. Go dependencies, including
the QR encoder, are compiled into the executable. These tools are not end-user
requirements.

Third-party license notices are embedded in the executable and available from
the local Admin server at `http://127.0.0.1:8787/licenses.txt` (use your selected
Admin port if changed). They are also retained with the vendored source.

The Mac installer uses built-in macOS tools. Advanced overrides are
`SMARTSTAGE_VERSION` (release tag), `SMARTSTAGE_INSTALL_DIR` (parent folder for
`Smart Stage.app`), `SMARTSTAGE_NO_LAUNCH=1` (install without opening it), and
`SMARTSTAGE_SKIP_FIREWALL=1` (leave firewall configuration to you or your IT team).
If firewall authorization is cancelled, the installed app remains available;
allow its incoming connections in **System Settings → Network → Firewall →
Options** before using phone/tablet controls. Managed network filters can require
an additional policy exception from your IT team.
Its native CI check exercises real ZIP downloads, targeted quarantine removal,
replacement/refusal behavior and Finder-to-Admin startup on both Macs.

```sh
go test -race ./...
go vet ./...
bash scripts/build.sh darwin arm64
bash scripts/build.sh darwin amd64
bash scripts/build.sh windows amd64
bash scripts/build.sh windows arm64
```

Build on matching OS runners normally. `DEBUG=1` retains symbols;
`VERSION=v0.1.0-preview.8` sets metadata. Output:
`dist/smartstage-<os>-<arch>.zip`, the raw build executable and engineering
harness `dist/native-harness-<os>-<arch>[.exe]`, with adjacent checksums, import
audits and toolchain records. Published application downloads are ZIPs. Apple system frameworks remain dynamic; Windows compiler
support is linked in. `CGO_ENABLED=0` cannot make a playback release.

Windows builds embed the Smart Stage icon in the executable. Mac builds also
produce `dist/smartstage-darwin-<arch>.app.zip`, an optional Finder app with the
same icon. Extract it and open `Smart Stage.app` to start the bundled executable
without Terminal, with the Smart Stage icon in the Dock and a standard Quit
command. Its menu bar control also provides Admin, logs and Quit. Both
launch paths open the local Admin page automatically. See the
[icon source and regeneration instructions](assets/icon/README.md).

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
Use the [Mac physical test](docs/macos-physical-test.md) for an isolated test show,
speaker/display observations and a result template.
