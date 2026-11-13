# Smart Stage v1.9.2 verification

Release source: `cf527c002bdc50300234fbdc7307bf97f80a7e23`.

Each preloaded remote section now owns its vertical scroll area. Switching from
the bottom of a long section to a short one shows the short section's buttons;
returning restores the long section's offset. Offsets last while the remote
page remains open. The ribbon and seek bar stay outside these areas, and smooth
horizontal gestures, retained button nodes and playback are preserved.

## Browser regression

[Local evidence](local-checks.json) binds the exact runtime and test bytes. The
old layout reproduced document scrolling instead of section scrolling. The
final Chromium suite passed 16 trusted touch gestures and 85 viewport samples,
including long-bottom to short-top navigation by swipe, tab and keyboard,
restored offsets, short-page boundary drags, rotation, inactive-page inertness,
stable nodes and absence of playback or document-navigation requests.

Focused Chromium and WebKit checks also cover immediate resizing, short pairing
dialogs and simulated software-keyboard geometry. An intermediate JavaScript
body-height approach failed existing immediate-resize assertions; the final
layout uses CSS viewport units and passes the unchanged assertions. The keyboard
checks are browser simulations, not physical-device acceptance. Very short
screens with two simultaneous long errors can leave a small button viewport.

## Published release

All 25 [tagged release checks](tag-workflow.json) passed on attempt 1, with
25 new executions and no retries or retained jobs. [Tagged receipts](tagged-verification.json)
verify sixteen artifact archives, four real startup updates, six installer shell
reports and six browser reports. Every browser report passes the new independent
section-scrolling scenario against its served native Go host, including WebKit
on both Macs. Four Chromium reports exercise the broader feature scenario;
the two focused WebKit reports retain their explicit limits. Physical touch and
dedicated native-window pointer forwarding are not asserted.

The anonymous [public audit](public-audit.json) verifies all 50 asset digests,
eight checksum pairs, six clean desktop variants, two static Linux gateways and
six website-manifest archive/checksum pairs. Ten installed executable hashes in
the tagged receipts match these audited public bytes. Physical output routing
and clean-machine acceptance remain open.

The three [public installer defaults](installer-defaults.json) match
`056b6f25fedaa210d1beae9bf7c859ff3b23a440` and select v1.9.2. The separate
[default-pin workflow snapshot](default-workflows.json) records four automatic
runs still queued or executing at its timestamp; it does not claim their
unfinished checks or uncollected report archives passed.

The task-created temporary remote release branch was removed using its exact
expected source as a lease. [Cleanup evidence](release-branch-cleanup.json)
confirms that main and the immutable annotated release tag were preserved.
Raw machine logs, media paths and runtime credentials remain private.
