# Preview 10 update verification

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
