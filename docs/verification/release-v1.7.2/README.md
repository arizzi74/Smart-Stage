# Smart Stage v1.7.2 verification

Release source: `8d2680a6ba788f17020510259c5f627ea8ab8b27`.
Implementation source: `9d2aa59cd417a8fc531d95235b823171f08747ba`.
[Source equivalence](source-equivalence.json) records identical application code;
later changes affect release notes and verifier diagnostics/synchronization.

The pinned helper retains `--protocol auto` with `--retries 2` and
`--max-edge-addr-retries 2`. Bounding both counters permits HTTP/2 selection after
three failed QUIC dials, allowing fallback during the existing 90-second startup
budget in the verified timeout scenario. DNS and other setup delays can still
consume that budget. Retries cap
backoff rather than limiting the helper lifetime to two attempts. Admin preserves
a fixed EN/IT timeout notice during automatic retries, then clears it on success
or explicit reconnect/mode change. Raw helper errors remain private.

[Local checks](local-checks.json) include Go race/vet, workflow lint, diagnostic
browser coverage, and a real public tunnel with HTTPS pairing, WebSocket status,
authenticated STOP, private-route rejection and helper/origin shutdown. The live
test used Linux ARM64 and synthetic playback. The [native fallback proof](local-fallback-verification.json)
uses the real verified helper with loopback provisioning/origin/edge fixtures:
three failed UDP dials are followed by HTTP/2 selection and actual TCP acceptance.
It proves transport selection, not completed TLS or registration. Supported
native desktop CI runs the same proof; the Cloudflare helper remains unavailable
on Windows ARM64.

## Earlier failures and verifier recovery

The [first candidate](earlier-candidate-workflow.json) failed its Windows ARM64
LAN browser check: `video-aac-1080p.mp4` revalidation returned `MF_E_SHUTDOWN` while
rewinding a native inspection track. The verifier now reports terminal failures
immediately with sanitized diagnostics, retaining the 90-second deadline and
all-cues-ready requirement. No native playback repair or automatic validation
retry was added; the original Media Foundation failure's cause remains unresolved.

The [follow-up candidate](candidate-workflow.json) passed all five jobs that ran,
including that Windows ARM64 browser scenario. GitHub cancelled Apple Silicon
before any step ran because a hosted runner was unavailable; the binary scan
was therefore skipped. Neither candidate is labelled entirely successful.

The [v1.7.1 tag attempt](failed-tag-v1.7.1.json) failed before publication in two
existing verifiers. The gateway browser fixture could reset a track after an
HTTP response while its structural playlist reload was still pending. A
[controlled probe](gateway-reload-boundary.json) confirmed that response completion
can leave old or differently ordered rows. The exact original −46% assertion
failure was not reproduced, so this does not establish its precise cause.
The verifier now waits for old-row detachment, exact intended order and idle
playlist state, preserving all visible/accessibility/saved-volume assertions.

The Mac Intel scene probe's combined video-to-image assertion failed without
identifying its individual predicate. Its setup checked readiness/start flags
without first observing movement. The [precondition review](mac-fixture-precondition-review.json)
records the added same-player/live-playing/clock-advance precondition and numeric
failure diagnostics. The 0.5-second fades, global 85-second bound and all four
result predicates are unchanged; no production renderer was modified. This does
not assert which predicate failed in the earlier run. The immutable v1.7.1 tag
is retained and no v1.7.1 release or installer default was published.

## Release evidence and limits

The [public audit](public-audit.json) verifies all 50 asset digests, eight archive
and checksum pairs, six desktop executable variants with clean full source
metadata, and the anonymously readable v1.7.2 update manifest. Both Linux gateways
are static; they intentionally omit full VCS metadata and retain the release
version prefix and digest-verified static reports.

All 25 [tagged release jobs](tag-workflow.json) passed on the first attempt: seven
build/security gates, publication and website discovery, plus 16 public download,
browser, installer and automatic-update jobs. The pinned native helper actually
switched from failed QUIC to HTTP/2 in both Mac architectures and Windows AMD64
CI; the fixture confirms TCP acceptance, not a completed Cloudflare edge handshake.

[Tagged artifact receipts](tagged-verification.json) verify all 16 artifact
archives and their reported sizes/digests, four actual startup updates, six
installer shell reports and six native browser reports. All ten installed
executables match the independently audited release bytes. Updater fixtures
retain the tagged source with older version metadata; automatic startup discovery
is exercised without a manual update request. Windows firewall comparisons retain
all rules without exemptions. Raw machine paths, rule lists and logs remain private.

All three [public installer scripts](installer-defaults.json) match default commit
`3ebe4780989e3b4794b0aafa788c042da74139e3` and select v1.7.2. The
[current-default receipts](default-verification.json) verify all four workflows,
including the seven main build gates, plus four actual updater and six installer
shell reports. All ten installed hashes match independently audited public bytes;
installer bootstraps match the default commit. The default commit changes only
installer pins, updater verification defaults and implementation status, leaving
production application code identical to the tag.

Physical speaker/projector, clean-machine, dedicated system WKWebView input and
affected UTM viewer checks remain separate open items. The user's successful
HTTP/2 registration confirms a working transport on that Mac; earlier TLS EOFs
do not identify whether Zscaler, another filter or a remote endpoint closed them.
