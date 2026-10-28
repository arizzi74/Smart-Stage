# Smart Stage v1.6.6 verification

Release source: `560265afc945ec519daa27000893a4c7f2eccafc`.

The [earlier candidate](earlier-candidate-workflow.json) and local checks use
`d7fc7a78042de9a781d6dafca94b68f6fdf51f28`.
[Source equivalence](source-equivalence.json) records byte-identical production
code; subsequent changes are Windows test preparation, a verifier report label
and the release-note version. New runtime gates verify the final source.

Update discovery reads the public website manifest at
`https://arizzi74.github.io/Smart-Stage/updates.json`. Each check makes one
website request. It never falls back to the GitHub API when the feed fails.
An installed version that is current does not contact GitHub. A newer eligible
version supplies exact official archive and checksum URLs, used only when
installation starts. Semantic version/channel, architecture, bounded downloads,
SHA-256 validation and rollback rules remain intact.

The website generator reads GitHub metadata using the CI job's temporary token.
It excludes drafts, untrusted or duplicate asset URLs, invalid sizes and incomplete
archive/checksum pairs. A release-specific deployment requires all six desktop
archives and checksums. The website deploys from protected main; the release
workflow dispatches that deployment and waits for anonymous public discovery of
the exact new release before running automatic-update verification.

[Local checks](local-checks.json) include Go race tests, vet, the source-package
vulnerability scan, 11 JavaScript checks, five manifest-generator checks and 22
website browser checks. Builds use Go 1.26.10; the previous Go 1.26.8 scan found
new standard-library advisories. Historical verification records retain their
original runtime versions. The [bootstrap website manifest](bootstrap-manifest.json)
was publicly readable and contained all current v1.6.4 desktop downloads before
the new release.

[Additional failed checks](additional-failures.json) remain separate. A run of
the original v1.6.4 updater hit GitHub's shared-IP limit on Mac ARM64. An optional
Windows ARM64 display observation used the older native harness from source
`505a1e495075dcc01abe6d7367d3b57d3f8e60a9` and did not observe the required fixture
colors; its cause is unresolved. A separate Windows ARM64 firewall probe failed
its first simple-filename case without diagnostic output, while its other cases
and current-candidate shared tests passed. Those results were not silently rerun,
relaxed or labelled successful. Production firewall and native playback code are
unchanged by this release.

The [Cloudflare assessment](cloudflare-assessment.json) is documentation only.
No Cloudflare helper is installed or executed by this change. Quick Tunnels can
provide account-free temporary HTTPS and WebSockets, but do not support the
browser's current SSE status stream. A future integration would need a separate
loopback remote-only listener, WebSocket status, public pairing protection,
verified per-user helper downloads and managed process cleanup. Quick Tunnels
have no uptime guarantee and change hostname on recreation. The current official
helper release has no Windows ARM64 asset; using Windows 11 x64 emulation would
need platform verification. Existing gateway/LAN behavior remains unchanged.

Physical speaker/projector, clean-machine, dedicated system WKWebView input and
affected UTM viewer acceptance remain incomplete. Native CI checks do not mark
these physical observations passed.

The immutable [v1.6.5 tag attempt](failed-tag-v1.6.5.json) failed before publication
on the first Windows ARM64 scoped-firewall test. The old subprocess supplied no
output and did not record its context deadline state. Its repeated 65–67 second
duration, including diagnostics, suggests cold provider startup consumed the
60-second rule-verification limit; the exact old cause is not asserted as proven.
The revised test explicitly imports native NetSecurity and performs read-only
provider queries within a separate two-minute setup bound. Each production rule
payload still executes once within its original one-minute limit, with all scoped
rule, profile and cleanup assertions preserved. No v1.6.5 release was published.

All seven [final-source candidate gates](candidate-workflow.json) passed.
The separate [Windows provider workflow](windows-provider-workflow.json) passed
on both architectures. Its [measured readbacks](windows-provider-verification.json)
retain all three strict cases per target, the original one-minute payload limit,
profile comparison and cleanup. The [Mac memory-profile workflow](memory-profile.json)
passed on both architectures using production code identical to the release.
A separate [old-updater recovery run](old-updater-recovery.json) passed all four
original v1.6.4 fixtures after the reset window; the earlier rate-limit failure
remains recorded and is not replaced by that later result.

The [published manifest](published-manifest.json) is anonymously readable and
contains all six v1.6.6 desktop archive/checksum pairs. The release-triggered
website deployment ran successfully from protected main, before the tagged
updater jobs started. The [public audit](public-assets.json) verified all 50
asset digests, eight checksum pairs and Go 1.26.10 metadata. Six desktop executable
variants embed the clean full release source; the reproducible gateway recipe
intentionally omits full embedded VCS metadata. Both public gateway binaries were
[reproduced byte for byte](gateway-reproducibility.json) from the immutable source.

All four [tagged automatic updates](tagged-auto-update-evidence.json) and the
[four current-default updates](supplemental-auto-update-evidence.json) passed
actual startup discovery, public-byte validation/replacement and restart.
Artifact digests, tagged fixture archives and executable hashes match independently
verified public downloads. Shows, original media and the Admin port are preserved;
restart does not autoplay. No manual update-install HTTP requests are sent.
Fixtures use the exact release source with older version metadata, rather than
every historical executable. [All six updater defaults](updater-defaults.json)
match the new release and exact tagged fixture source/run.

The four Chromium and two Mac WebKit [published-host browser checks](tagged-browser-evidence.json)
passed. They retain explicit limits for physical routing and dedicated system
WKWebView input. [All three public installer scripts](installer-defaults.json)
match their immutable default commit and select v1.6.6.

All nine publication/manifest gates and all 16 tagged post-publication jobs
passed in the [final tagged workflow](tag-workflow.json). All 12
[tagged/default native installer reports](native-installer-evidence.json) passed
in eight jobs, covering both desktop architectures and Windows PowerShell 5.1/7.
Exact bootstrap and installed hashes match the recorded sources and independent
public audit. All eight Windows comparisons retain complete firewall snapshots
with no exemptions; query ordering alone is normalized. All four Mac reports
retain global/unrelated-rule checks and allowed bundle/core readbacks. Raw rules,
logs, paths and machine identifiers are not published in this record.
