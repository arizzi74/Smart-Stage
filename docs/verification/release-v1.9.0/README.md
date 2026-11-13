# Smart Stage v1.9.0 verification

Release source: `2eba7f4815db98430bad940aea227fef484e88df`.

Remote sections use stable cue nodes inside horizontally translated pages.
During a touch drag the cue area follows the finger and reveals its neighbor;
release settles to a section or back to the current one. Edge resistance,
vertical scrolling, cancellation, interrupted transitions and reduced motion
are explicit. Inactive pages are inert, hidden from accessibility navigation,
and excluded from tab order, including a fallback for older WebKit engines.
Navigation never issues playback commands.

Admin's **Sort Playlist** opens a compact section/cue-name dialog. Mouse dragging
moves sections as groups or cues within/across sections, including empty ones.
The popup keeps a private draft until **Save order**; Cancel discards it. A saved
transaction changes order/membership only, retaining current paths, labels,
levels, flags, backgrounds, settings and collapsed metadata. Optimistic revision
checks reject a stale draft while preserving the newer host configuration.

The sorter is an ES module and stylesheet loaded only on Admin's first use.
Remote pages never request them, existing gateway allowlists are unchanged, and
the command listener returns 404 for both assets. Names are rendered as text;
existing authentication, CSRF and CSP protections remain in effect.

## Earlier candidate

The [first candidate](earlier-candidate-workflow.json) passed five of seven gates.
Its Windows ARM64 installer fixture did not observe a matching current-user
Windows Start ActiveStore firewall rule within the existing 120-second readiness
budget. This happened before the firewall baseline or installer ran; the new
sorter browser scenario and downstream binary scan were skipped on that path.
The log does not establish whether Start launched or why its rule was absent.
This is retained as a prerequisite timeout with cause unproven.

The following candidate also includes an independent UI readiness correction:
Sort availability refreshes as collapse-save pending flags clear, and a pending
playlist read does not block opening a version-guarded draft. Immediate browser
assertions cover both collapse saves and reloads. Windows verifier behavior,
readiness assertion and deadline remain unchanged; the UI correction is not
presented as a repair for the Windows fixture.

## Tagged runner-capacity recovery

The [first tagged attempt](earlier-tag-workflow.json) completed five jobs
successfully, but GitHub could not allocate its Apple Silicon runner. That job
was cancelled with zero steps and no runner; exact annotations reported hosted
acquisition failure and macOS ARM capacity constraints. Security/publication and
post-publication jobs were skipped, and no release existed at that point.

A one-off helper on an isolated temporary branch verified the immutable tag,
source, run/job identity, complete original job set and exact cancellation
annotations before requesting only that job and its dependent jobs again.
[GitHub's job rerun endpoint](https://docs.github.com/en/rest/actions/workflow-runs#re-run-a-job-from-a-workflow-run)
provides this operation using repository Actions write permission. The helper
performed no checkout and changed no application, assertion, deadline or tag.
[Recovery evidence](runner-retry-verification.json) records the source-specific
request and attempt accounting; the [helper definition](runner-retry-workflow.yml)
is retained for review.

The [second attempt](tag-attempt2-workflow.json) published the release and passed
23 of 25 effective checks. GitHub again cancelled two Apple Silicon jobs without
assigning a runner or executing any steps: public download verification and the
Mac installer. Their exact annotations again reported runner acquisition failure
and capacity constraints. The [strict collection failure](tagged-collection-failure-attempt2.json)
preserves that incomplete result.

A second [guarded helper](runner-retry-postchecks-workflow.yml) verified the
complete 25-job set, the two exact zero-step cancellations, all 23 successes and
the identity/digests of the already-published 50 assets. It requested failed-job
recovery on that immutable run; [its receipt](runner-retry-postchecks-verification.json)
is separate from the first recovery. No release assets were republished. The
temporary branch and clean worktree were removed after recovery;
[cleanup evidence](runner-retry-cleanup.json) records exact-ref checks and both
archived helper definitions.

Final job results explicitly distinguish retained successful checks from new
execution by comparing complete step arrays and exact start/end timestamps with
every prior attempt. Earlier cancellation is never relabelled as successful
execution. The final 25 successes comprise five physical executions from attempt
1, eighteen from attempt 2 and two from attempt 3.

## Current-default Windows observation

The [original default build failure](default-build-failure.json) is separate from
the successful tagged release: its Windows ARM64 check did not observe the
shipped process's Italian Quit confirmation within the existing eight-second
budget after cancelling the native media chooser. Earlier WebView, chooser,
original-file drop and helper-process close checks passed; later shipped-process
checks were not reached. The original report did not record native chooser-idle
state, so the cause remains unproven.

A verifier-only commit adds bounded, sanitized window-category and elapsed-time
observations around the same sequence. It changes no production file, readiness
predicate, message sequence, assertion or timeout. The original failure remains
recorded independently of the [fresh build result](default-build-recovery.json).
Both Windows targets passed the complete fresh job, including the previously
failing native close sequence and the new sorter browser integration. The
diagnostic build remains incomplete overall: five required gates passed,
GitHub cancelled Apple Silicon before assigning a runner or executing any step
with explicit capacity annotations, and dependent binary security was skipped.
The workflow's failure conclusion is retained. The immutable released source
already passed all its native and binary-security checks; no further diagnostic
capacity rerun was requested.

## Checks and scope

[Local checks](local-checks.json) retain the earlier local full Go race/vet run,
the Admin asset-boundary check, twelve localization/wake-lock tests and the final
Chromium fixture suite. The readiness correction changed embedded JavaScript;
the final-source candidate and tagged CI reran full Go race/vet. Real touch input
checks nine gestures, held intermediate translations,
adjacent visibility, intermediate released-snap motion, cancellation, both edges,
reduced motion, live-state node identity and actual vertical scrolling. Real
mouse sorting checks within-section, cross-section, whole-section and empty
section drops, ghost movement, Save/Cancel, keyboard alternatives, metadata,
English/Italian labels and an actual 409 revision conflict. Those fixture checks
do not establish native playback.

The final sorter module also passed a focused Playwright WebKit check on Linux
for mouse dragging, saving, rejection, focus, cancellation and narrow layout.
This is not a physical macOS/dedicated WKWebView input claim. Capture ownership
is checked so a descendant's implicit capture loss does not cancel its parent's
drag; actual touch input exercised the transfer in both controls.

All seven [candidate gates](candidate-workflow.json) and all 25 effective
[tagged release job results](tag-workflow.json) passed after capacity recovery. The real-host browser section
scenario additionally opens the lazy sorter through the Go server, checks asset
MIME types and command-listener rejection, and saves an actual mouse move while
preserving cue metadata and native source continuity when an audio endpoint is
available. [Tagged receipts](tagged-verification.json) verify source/target,
artifact size/digest and reported scope. The four Chromium reports include the
new native-host sorter checks. The two WebKit reports retain their existing
track-drag-only scope and mark these integrations not exercised. Windows hosted
runners without audio endpoints cannot establish audio continuity; no physical
speaker/projector or dedicated native-window behavior is asserted.

The [independent public audit](public-audit.json) verifies all 50 asset digests,
eight checksum pairs, six desktop variants with clean full VCS metadata, two
static Linux gateways and anonymous website update discovery. Sixteen tagged
artifact archives, four actual startup updates, six installer shell reports and
six browser reports passed; installed executables match audited public bytes.

The three [public installer scripts](installer-defaults.json) match default
commit `bf5d036f6df660161a4433ff2e3d8936dd8542c6` and select v1.9.0. [Current-default receipts](default-verification.json)
verify six installer shell reports and four actual updates from all three
successful distribution workflows. Bootstrap source and installed bytes are
checked independently. The receipt explicitly records the diagnostic build's
five passed gates, one capacity cancellation and security skip, with
`mainBuildSevenGatesPassed: false` and overall `status: incomplete`; ten passed
distribution reports do not replace the missing build gates. Windows firewall
comparisons retain every rule without exemptions. Raw logs, host media paths,
pairing data and firewall rule lists stay private. The default commit changes
release pins and verification references; application code remains the tagged
source. The separate build-verification source differs from the default-pin
source only in `scripts/windows_close_checks.py`; the full changed-path list is
checked before collecting current-default results.
