# Smart Stage v1.9.1 verification

Release source: `6ff2f2efa74cf81266f1a325bfe076795f3ae637`.

Remote section pages already retained their buttons, but absolute positioning
made the selected page alone determine the clipping height. A shorter selected
page could cut off the incoming page until selection changed. The CSS fix puts
every preloaded page in one intrinsic grid row, reserving the tallest height
throughout the swipe. Existing horizontal offsets, nodes, inertness and playback
controls are unchanged. Short sections intentionally retain blank space below
their buttons; this patch adds no per-section scroll restoration.

## Browser regression

[Local evidence](local-checks.json) binds the exact CSS and test bytes. The old
CSS reproduced a 240px clipping area with an incoming 570px section. The fixed
full Chromium suite passed eleven trusted touch gestures and 65 height samples,
covering both directions, held and settling frames, empty sections, cancellation,
reduced motion, live off-tab label growth and rotation. Nodes stay connected,
navigation sends no document request and playback identity is preserved.

A separate Linux WebKit probe passed with the actual stylesheet and a
representative remote DOM, including the old failure and fixed transition
geometry at 320px, 390px and 844px. This is a layout check, not a physical iPhone
or dedicated Mac-window touch claim. The synthetic label-update test explicitly
reloads Admin's acknowledged playlist revision before later unrelated writes;
its earlier fixture-sync failure remains private, without relaxing assertions.

## Published release

All 25 effective [tagged release checks](tag-workflow.json) passed on attempt 3.
Complete step arrays and exact job start/end timestamps identify five physical
executions from attempt 1, nineteen from attempt 2 and one from attempt 3.
Retained successful execution is distinguished from fresh execution; neither
failed attempts nor unstarted cancellations are relabelled as passes.

The [public audit](public-audit.json) verifies all 50 asset digests, eight checksum
pairs, six clean desktop variants, two static Linux gateways and six anonymous
website-manifest archive/checksum pairs. [Tagged receipts](tagged-verification.json)
verify sixteen artifact archives, four actual startup updates, six installer
shell reports and six browser reports against the audited public executables.
The native browser reports retain their stated scope; the new height cases are
in the full Chromium fixture suite. Physical output acceptance remains open.

The three [public installer defaults](installer-defaults.json) match
`9bdcb5413899df633ad8bc28cfa358787a2b3da7` and select v1.9.1. Only release pins and verification references changed. The separate
[automatic default-pin workflow snapshot](default-workflows.json) records live
source-bound outcomes at its timestamp; unfinished jobs and uncollected default
report archives are not claimed as additional passed checks.

## Preserved failures and retry

The [first tagged attempt](earlier-tag-workflow.json) passed five gates, then
failed the unchanged Windows ARM64 [Start-component prerequisite](tag-attempt1-windows-start-failure.json)
before the firewall baseline and Smart Stage installation. Its configured
120-second wait and current-user package/SID rule matching were retained. Cause
remains unproven. The independent main run passed that Windows job at the same
source; it is not substituted for the failed tagged execution.

A [fixed helper](runner-retry-workflow.yml) on an isolated temporary branch
requested only that Windows job and its dependent checks again. Its
[receipt](runner-retry-verification.json) binds the immutable tag, source,
complete original job set, reviewed failure and original artifact identity.
Two earlier helpers failed without an accepted retry; the
[first](retry-helper-first-failure.json) recorded a generic request error, and
the [second](retry-helper-second-failure.json) identified a failed GET of the old
CI log. The first error does not establish which request failed. The final
helper uses the separately reviewed failure proof and live artifact metadata;
it does not claim to parse the old log in CI. No application, test assertion,
timeout or publication requirement was bypassed.

The [second tagged attempt](tag-attempt2-workflow.json) passed 24 checks, but
GitHub cancelled the sole remaining [Mac download check](tag-attempt2-capacity-failure.json)
without assigning a runner or executing a step. Exact annotations reported
hosted acquisition failure and Mac ARM64 capacity constraints. A separate
[guarded helper](runner-retry-postchecks-workflow.yml) requested only that job
again; its [receipt](runner-retry-postchecks-verification.json) binds the
unchanged 50-asset fingerprint and complete earlier outcomes. Publication and
other successful jobs were retained. The temporary remote/local branch and clean worktree were removed;
[cleanup evidence](runner-retry-cleanup.json) records exact-ref checks and all four
archived helper definitions. No helper was merged into main.

The [separate main build](main-workflow.json) passed five gates but failed Mac
ARM64 video inspection with `AppleM2ScalerParavirtDriver` / `Cannot Decode`, and
skipped dependent security. Its [failure proof](main-mac-arm64-failure.json)
preserves the exact source, artifact digest and unproven cause. That same Mac
target passed in the tagged workflow. Failed jobs remain failed; no overall
main-build pass is claimed. Raw logs, paths and runtime credentials stay private.
