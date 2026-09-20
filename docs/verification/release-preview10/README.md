# Preview 10 update verification

Published source: `3c8492f036840f64612cef12045bd718a96c9d81`.
[Native/browser checks](https://github.com/arizzi74/Smart-Stage/actions/runs/35512154212)
passed on all four targets. The
[tagged run](https://github.com/arizzi74/Smart-Stage/actions/runs/35512155124)
passed 16 normal build/publication/download/browser/installer jobs, but its four
initial updater verifications failed; those original reports are retained in
`initial-auto-update/`. Windows failures concerned test-process cleanup after
successful updates, Intel Mac concerned equivalent Unicode bundle paths, and
the first Apple Silicon check reported an early error without recording its
message. No success is inferred from those failed checks.

The corrected verifier at `c0ce51559ff7a711511589d15610916e72e7f8ac` passed
[all four automatic updates](https://github.com/arizzi74/Smart-Stage/actions/runs/35512568301)
against the unchanged published release. The top-level `auto-update-*.json`
reports record those successful runs. Both Mac architectures then passed
[default installation](https://github.com/arizzi74/Smart-Stage/actions/runs/35512624089)
at installer source `454324a0ceb2169c4be0463418a0ba0d7c4dee20`. The public
unversioned installer bytes were checked separately; its SHA-256 and observation
time are in `public-bootstrap-verification.json`.

Preview 10 introduces automatic application updates at startup. It reserves
playback before checking for a newer release, verifies the architecture-specific
archive, shuts down gracefully, replaces the installation and restarts with the
saved show. Checks during an existing session never automatically restart it.

The update package tests cover release channels and numeric version ordering,
bounded HTTPS downloads, exact asset names/sizes/checksums, rejected archive
paths/types, installation locking, startup receipts and rollback. The subprocess
tests run the production helper with test executables acting as the original and
candidate applications. These establish process replacement and failed-startup
recovery, rather than native media playback.

Preview 9's published updater rejected Go's optimized build metadata before
replacement; its [failed results](../release-preview9/) are retained separately.
The correction reads the exact tagged main module version and requires clean
Git/native build metadata. A regression creates and compiles a real tagged Git
fixture using the release's `-trimpath` options, accepts its correct version and
rejects different versions and dirty source. The subprocess tests also cover
rollback when the new process rejects its runtime version before registration.

The published-release checks use the release's exact source compiled with older
`v0.0.0-preview.1` metadata as a CI fixture. That application discovers the actual
release using the production GitHub API, downloads the published architecture's
archive, replaces itself and starts the published executable. No alternate feed
or synthetic acknowledgement is accepted by production code, and the test sends
no install request. Its report compares the final executable with an independent
download, observes new process/instance identities, preserved ports and show
data, and confirms stopped playback and disabled stage output after restart.

Mac update checks also observe the actual running app's regular activation
policy and matching registered icon through `NSRunningApplication`, verify the
complete bundle signature and confirm that no Terminal was opened. This proves
Dock eligibility and registered artwork, not a screenshot or physical Dock click.

The automatic-update CI explicitly sets `SMARTSTAGE_SKIP_FIREWALL=1`: it does
not exercise the interactive administrator prompt or claim a firewall allowance.
The skip is recorded in the update outcome. Mac installer checks separately
exercise real blocked-to-permitted rules with test-only elevation substitution.
Neither establishes physical LAN traffic through an enabled firewall.

The synthetic browser suite verifies update status, startup reservation controls,
safe rendering, retry and reconnect/QR refresh using simulated responses. Actual
replacement is established by the native checks above. Hosted results remain
distinct from clean-machine permissions, physical speaker/projector routing and
phone/camera/Wi-Fi acceptance.
