# Smart Stage v1.7.0 verification

Release source: `18108546989e247d817f7093825684892d3bcbc7`.
Installer/default verification source: `941e3634494b86af661ee776af6c31e40d8b1838`.
The second commit changes installer version pins, updater verification defaults
and the implementation-status heading; production application code is identical.

Cloudflare Quick Tunnels are the default on supported hosts without a configured
gateway. Existing configured gateway and explicit LAN selections are preserved.
Switching modes retains gateway URL/token. Windows ARM64 has no official native
helper in the pinned distribution, so Cloudflare is disabled there, with no
emulated helper or automatic LAN fallback. Admin remains on loopback; the tunnel
uses a separate remote-only loopback origin and authenticated WebSocket status.
Existing gateway/LAN SSE behavior remains covered by regression tests.

## Release and public downloads

[Workflow receipts](workflow-verification.json) record seven successful final
candidate jobs, all 25 successful tagged jobs, and the release-triggered website
deployment. The tagged jobs comprise nine build/publication/manifest jobs and
16 public-download, installer, browser and updater jobs.

The [public audit](public-audit.json) verifies all 50 asset digests, eight archive
and checksum pairs, six desktop executable variants with clean full source
metadata, and the anonymously readable v1.7.0 update manifest. The two static
Linux gateway binaries intentionally use `buildvcs=false`; their embedded version
prefix and digest-verified static reports are distinct from desktop full VCS
metadata.

[Tagged verification receipts](tagged-verification.json) retain the 16 verified
artifact archive digests, four real startup updater reports, six installer reports
(two Macs and two PowerShell versions per Windows architecture), and six native
browser reports. All ten installed executable hashes match independently audited
public bytes. Updater fixtures use the exact tagged source with older version
metadata; no manual update HTTP request substitutes for automatic startup discovery.

[Default verification receipts](default-verification.json) cover the same four
startup updater targets and six installer shell reports using the new defaults,
plus the complete main build. [Public installer checks](installer-defaults.json)
confirm all three raw-main bootstrap scripts match the default commit and select
v1.7.0. Windows installer firewall comparisons retain every rule without exemptions.
Raw machine paths, rule lists and logs remain outside these committed summaries.

## Cloudflare and local checks

[Local/native checks](local-and-native-checks.json) record actual HTTPS pairing,
WebSocket status, authenticated HTTP STOP, private-route rejection and helper/origin
shutdown through a real public Quick Tunnel. This used Linux ARM64, synthetic
playback and the default system resolver; the hostname became resolvable after
60.7 seconds. New links can take about a minute to resolve and change on tunnel
recreation. No production resolver bypass is used.

The [official helper receipt](cloudflared-pins-2026.10.5.json) records actual
download hashes for all eight supported platform assets. Current Darwin release
asset digests supersede stale hashes in the upstream release description.
Native CI exercises the official helper CLI on both Mac architectures and Windows
AMD64. Guardian tests cover cancellation, startup failure and abrupt parent death
on all four desktop targets, including runtime-directory cleanup. Helper
availability remains disabled for Windows ARM64.

Go race tests, vet, package and exact executable vulnerability scans, authenticated
TLS browser tests, bilingual UI checks, and native startup/lifecycle checks pass.
Builds use Go 1.26.10. Physical speaker/projector, clean-machine, dedicated system
WKWebView mouse input and the affected UTM viewer remain separate open checks.
Quick Tunnels provide temporary URLs without an uptime guarantee.

## Earlier checks and corrections

The [first candidate](https://github.com/arizzi74/Smart-Stage/actions/runs/40700085647)
passed shared/gateway jobs but failed native jobs. Windows guardian working
directories held runtime folders open; the guardian now uses the cache parent,
while the helper uses its private runtime directory. The Mac probe expected a
gateway draft while Cloudflare was selected by default; it now explicitly selects
Gateway and preserves its existing draft/window assertions.

The [next candidate](https://github.com/arizzi74/Smart-Stage/actions/runs/40701926889)
failed Windows AMD64, while Mac ARM64 was cancelled; the other four build/test
jobs passed and binary-security was skipped. The Windows smoke verifier
force-terminated the host and immediately deleted temporary files before guardian cleanup completed. The final
verifier requests authenticated graceful Quit, retains a bounded legacy fallback,
and requires strict temporary-file removal within 15 seconds. Focused verifier
tests and the final candidate pass; cleanup assertions were not removed.

An [earlier Mac installer check](https://github.com/arizzi74/Smart-Stage/actions/runs/40700085377)
also failed an exact informational-message assertion after the new mode wording.
The corrected assertion passed in the
[follow-up run](https://github.com/arizzi74/Smart-Stage/actions/runs/40700316996).
These earlier failures remain distinct from the successful immutable v1.7.0
release, which needed no reruns.
