# Smart Stage

Smart Stage is being implemented from [the product specification](SMART_STAGE_CODEX_PROMPT.md).
It will run native host audio/video playback with paired LAN Admin and Command
interfaces. It does not play media in the remote browser.

**Development status:** native feasibility implementation; not a validated
release. See [implementation status](IMPLEMENTATION_STATUS.md). Physical audio
routing, persistent blackout, native device loss and clean-machine acceptance
remain unverified. GitHub builds are not physical verification.

Targets: macOS ARM64 and AMD64; Windows AMD64 and ARM64 (added by request).
Windows 11 is the validation baseline. macOS 12 is the provisional deployment
target; validated minimum OS versions have not yet been established.

## Native harness

Build on the matching OS with Go 1.26.5 and Xcode command-line tools (macOS), or
LLVM-MinGW 20260908 UCRT (Windows; its `bin` directory must be on PATH). Python 3
is used only for the build dependency audit. End users need none of these tools.

```sh
bash scripts/build.sh darwin arm64
bash scripts/build.sh darwin amd64
bash scripts/build.sh windows amd64
bash scripts/build.sh windows arm64
```

Each build writes executables, SHA-256 checksums, dependency audits and toolchain
records to `dist/`. `DEBUG=1` retains native/debug symbols.

```sh
./dist/native-harness-darwin-arm64 --list
./dist/native-harness-darwin-arm64 --inspect '/Users/operator/Show/opening.wav'
./dist/native-harness-darwin-arm64 --file '/Users/operator/Show/intro.mp4' \
  --audio 'device UID from --list' --display 'display UUID from --list'
```

Use the corresponding `.exe` on Windows. Pass endpoint/display IDs exactly as
printed by `--list`. The harness is an engineering tool; selecting a display
explicitly covers it. Type `stop`, `play` (restart), `enable`, `disable`, or
`quit`. Escape in the stage window stops; Ctrl+C quits. STOP retains black;
disable/quit deliberately expose the desktop. `--stop-after 2s` and
`--exit-after 5s` support automated smoke runs.

The full application, one-command installers and GitHub release publication
are being implemented. No install command is advertised before its release
assets exist.
