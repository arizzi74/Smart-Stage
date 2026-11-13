# Smart Stage v1.8.0 verification

Release source: `59d329fa5066b2d1b82ca0efddd2f1fbbb6913eb`.

Named playlist sections retain their order, cue membership and collapsed state
in the saved show and exported playlist files. Admin can create, rename,
reorder and remove sections and move cues between them. The remote has section
tabs and horizontal touch navigation; browsing sections never sends playback
commands. Metadata edits preserve existing native sources and their clocks.

Audio cues can retain a video's decoder while replacing its soundtrack, or mix
with existing video/music sources. Replacement leaves that video silent until
it ends or is restarted. Mixing retains existing audible sound; an already
silenced video stays silent. Selected sources pause/resume independently; STOP clears them all. Track/master levels
and independent audio fades apply to the native sources.

## Candidate failures and corrections

The [first candidate](earlier-candidate-workflow.json) failed an existing Mac
background-audio fade assertion. A preparing background player was excluded
before its asynchronous audio metadata arrived. The Mac mixer now uses the
coordinator's final background-audio eligibility while native track metadata is still loading,
retaining outgoing sound until readiness. The original
fade assertion and duration were retained. The [second candidate](second-candidate-workflow.json)
passed all seven gates, but preceded the final section implementation and native
terminal-error/readiness changes; it is supporting evidence only.

The [third candidate](third-candidate-workflow.json) failed a compound Mac layer
resume assertion without individual predicate diagnostics. The verifier now
checks decoder identity and unrelated playback continuously, and waits for the
original >0.07-second media-clock advance plus exact transport acknowledgement
under its unchanged 55-second total deadline. Final-source Intel
observations directly recorded acknowledgement before sufficient clock motion,
then the required advance with both decoder identities intact. This does not
identify the precise predicate that failed in the older run.

The third candidate also encountered the previously documented Windows ARM64
`MF_E_SHUTDOWN` while rewinding a multi-track inspection reader. The first
supported audio and video streams each have their own first-sample decoder probe and fresh source/reader, removing the rewind
and cross-track lifecycle transition. Every required sample must still decode;
failures are returned without retry. Both Windows architectures passed eight
source-opening cases and 16 consecutive mixed-video/audio inspection pairs. The
original internal Media Foundation shutdown trigger remains unproven.

The third browser failure lacked touch coordinates/count diagnostics. Explicit
hit testing later exposed fixture contact on the fixed seek slider locally.
The [fourth candidate](fourth-candidate-workflow.json) then failed a stricter
visibility precondition: its short viewport exposed six pixels where eight
were required, although contact hit the cue. Aligning the fixture four pixels
below the ribbon leaves the required clear strip. Header/footer clipping, real
hit tests, six trusted touch gestures, partial/vertical gestures and the
no-playback-command assertions remain required. No production gesture behavior
was changed for these fixture corrections.

## Installer-default diagnostic recovery

The first default build at `948e0ba6b0a2345d4860407a51424fa2b2a4101a`
failed its Mac Intel WebKit `errors === []` assertion after both drags,
cancellation and persisted-reload checks passed. The
[sanitized failure record](default-build-failure.json) retains two API
access-control page errors and the verified original artifact. Their timing and
source-document identity were not recorded; the cause remains unresolved.
A controlled Linux WebKit probe reproduced navigation cancellation, not the
original Mac page error, and is not treated as an explanation.

Commit `78ef44417afab750e360c0ebae50a23f1d28b7b1` adds bounded phase,
allowlisted-path, document-epoch and request-status diagnostics only. No request
is changed or error suppressed; every original slider/error assertion and timing
bound remains intact. [Source equivalence](source-equivalence.json) verifies that
application code and installer bootstraps did not change. The
[recovery build](default-build-recovery.json) passed all seven required gates. It
is recorded separately from the installer/updater workflows at the default-pin
commit; the earlier API error was not reproduced and remains unexplained.

The installer workflow at that default-pin commit also lost its Apple Silicon
job before any step ran: GitHub reported hosted-runner acquisition failure and ARM runner
capacity constraints. [The cancellation receipt](default-macos-arm-cancellation.json)
records zero steps and no assigned runner; this is not counted as a test pass.
Commit `cc301094068dd23d9e07333788efef584f5d6a9b` changes one workflow comment
to retry the unchanged Mac default checks. Bootstrap bytes and workflow behavior
are identical. [The Mac retry](default-macos-recovery.json) passed both
architectures, including default version selection and installed-byte verification. It is source-specific;
[the original four workflows](failed-default-workflows.json) remain recorded
with their failed/cancelled results.

## Final evidence

[Local checks](local-checks.json) record Go race/vet, package vulnerability scanning,
strict Windows cross-links and the full synthetic Chromium suite. Its section
coverage includes queued collapse saves, focus/draft preservation, moves,
validation, portable-file identity remapping, English/Italian controls and real
browser touch input. Fixture checks are explicitly distinguished from native
playback. The toolchain is Go 1.26.11, replacing vulnerable Go 1.26.10.

All seven [final candidate gates](candidate-workflow.json) and all 25
[tagged release jobs](tag-workflow.json) passed. [Native feature evidence](feature-evidence.json)
checks artifact digest/size/source/job identity on all four desktop targets.
Both Macs passed 20 native audio-layer observations plus real host/browser audio
and section flows, including advancing-clock continuity across section edits.
Both Windows targets passed section UI/API flows and the strict inspection
regression. Their hosted runners had **no audio endpoint**: Windows native mixing
and section audio continuity are explicitly unavailable, rather than passed.
Real Windows audio-device acceptance remains open.

Both Mac memory profiles passed at source `368c7ad42d37130d0da2f5ad033d5664818c2e3b`;
[source equivalence](source-equivalence.json) records unchanged Mac/shared
application inputs in the release. Later production changes affect Windows
inspection only. Supporting [second](second-candidate-memory-workflow.json) and
[third](third-candidate-memory-workflow.json) memory receipts are retained. The
[first profile run](earlier-candidate-memory-workflow.json) was cancelled and is
not counted as a pass.

The [public audit](public-audit.json) verifies 50 asset digests, eight checksum
pairs, six desktop executable variants with clean full source metadata, two
static Linux gateways and anonymous website update discovery. [Tagged receipts](tagged-verification.json)
verify 16 post-publication artifact archives, four actual automatic startup
updates, six installer shell reports and six native browser reports. Installed
executables match the independently audited public bytes. The four Chromium
reports exercise the new feature integrations; the two WebKit reports cover
track dragging only and explicitly mark the new integrations as not exercised.

All three [public installer scripts](installer-defaults.json) match default
commit `948e0ba6b0a2345d4860407a51424fa2b2a4101a` and select v1.8.0. The
[current-default receipts](default-verification.json) verify all four workflows,
with Windows installer/updater evidence from the default-pin commit, Mac installer
evidence from the comment-only retry, and seven build gates from the diagnostic
recovery. All ten installed binaries match audited public bytes. The earlier
failed/cancelled workflows remain explicit in the receipt. Windows firewall
comparisons retain all rules without exemptions. Raw machine paths, pairing
secrets, firewall rule lists and logs remain private.

Native clock/gain/decoder evidence does not verify physical speakers,
projectors, clean-machine permissions, all dedicated WebView input behavior or
the affected UTM viewer. Those acceptance checks remain separate.
