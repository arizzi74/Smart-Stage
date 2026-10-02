# Smart Stage v1.6.4 verification

Release source: `040774dc80900899787df31ab2ff7a13f7938cf3`.

Track volume uses the browser’s native range dragging with an arrow cursor.
Holding a mouse or touch gesture does not save, even after more than 200ms
without movement. Releasing saves one changed value; hovering does not move
the slider. Keyboard adjustments save when the adjustment keys are released.
Cancellation clears native thumb tracking, and an aborted newer drag preserves
an earlier released adjustment waiting in the save queue. The master volume’s
200ms debounce and saved normalized track levels remain unchanged.

## Source and regression checks

[Local checks](local-checks.json), [full Chromium fixtures](browser-checks.json),
and actual-mouse [Chromium](chromium-track-drag.json) and
[WebKit](webkit-track-drag.json) checks ran at
`94908416ecbb28dd87d74abf95edb39db2e7e772`. Production code and browser fixtures
are unchanged in the final release; [source equivalence](ui-source-equivalence.json)
records the exact UI hashes and the three files changed afterward. Those changes
are two native verifiers and the release-note version. Local tests were not
repeated for those verifier-only changes.

Frozen v1.6.2 [Chromium](chromium-track-drag-baseline.json) and
[WebKit](webkit-track-drag-baseline.json) records reproduce the older behavior
and are expected failures.

The final-source [candidate workflow](candidate-workflow.json) passed all seven
gates. Its [native and browser evidence](native-volume-evidence.json) includes
four full Chromium checks and two additional Mac WebKit checks against actual
native hosts. They verify zero writes and revisions while held, one after
release, stable hover, cleared capture, ordinary cursor and focus, and Mac
cancellation/reload. Mac native gain/fade checks passed. The Windows runners
have no audio endpoint, so audible playback and native audio gain remain
unavailable there.

## Published bytes and automatic updates

The [public audit](assets.json) verified all 50 release assets, eight checksum
pairs, architecture/source metadata and Go 1.26.8. All
[six exact executable vulnerability scans](security-scans.json) passed.
The published Mac hosts passed both full Chromium and focused WebKit
[regression checks](tagged-browser-evidence.json); the Windows hosts passed the
full Chromium checks.

All 16 [tagged post-publication jobs](postpublication-status.json) passed:
four public downloads, four browser/native-host jobs (six engine reports), four
automatic updates and four native installer jobs covering both architectures.

All four [tagged automatic updates](tagged-auto-update-evidence.json) passed
actual release discovery, verified public-byte replacement and restart. Starting
fixtures use the release source with older version metadata; these checks do not
exercise every historical executable. Saved shows and original media remain
preserved, and restart begins without playback.

A [separate current-default updater run](supplemental-auto-update-evidence.json)
at `254597e2bb3c40a05ef357de49d5949092d99957` also passed all four targets using
the original tagged fixtures. All six dispatch/active version, fixture-run and
fixture-source defaults match v1.6.4. Tagged attempts remain recorded separately.
Fixture cores do not embed the full VCS SHA; their full source is established
by digest-verified artifact manifests and the exact-tag detached-worktree build
recipe, plus the executed source prefix. Public and installed executables have
clean full source metadata and match the audited public executable hashes.

All three [public installer scripts](installer-defaults.json) match
`2085137b0315ba93a30ab1b73273acc7337e4248` byte for byte and default to v1.6.4.
The public gateway installer asset also matches the current default script.

All four [default-installer jobs](default-installer-workflows.json) passed.
All 12 [tagged/default native installer reports](native-installer-evidence.json)
passed, including both Windows architectures under PowerShell 5.1 and 7.
The [installer integrity audit](installer-integrity.json) matches exact
bootstrap and installed executable hashes to the recorded sources and public
assets. All eight Windows firewall comparisons use complete snapshots with no
exemptions. Mac checks preserve default/global/unrelated rules and verify fixture
cleanup. Raw firewall snapshots and machine identifiers remain private.

## Earlier attempts

The [candidate history](candidate-history.json) keeps previous attempts distinct.
The [earlier Mac candidate failure](earlier-candidate-mac-failure.json) preceded
the explicit range focus correction. The [earlier passed candidate](earlier-passed-candidate-workflow.json)
and its [native evidence](earlier-passed-native-volume-evidence.json) use the
same production code as this release.

The immutable v1.6.3 tag then [failed publication](earlier-tag-failure.json) on
a Windows menu-label assertion and a Mac Intel initially paused-video assertion.
No v1.6.3 release was published. Source review found asynchronous readiness gaps
in those verifiers. The failed artifacts did not retain exact menu/frame values,
so these gaps are not claimed as measured causes of the old failures.
The Windows verifier now waits for all three exact translated labels within its
unchanged eight-second bound. The Mac verifier waits for completed pause
preparation while continuously rejecting playback, then observes 250ms of
stationary state. Its 0.03-second clock tolerance and 85-second global deadline
are unchanged. Both final-source native checks passed.

A preliminary Windows ARM64 startup check also encountered an unresolved
connection reset on remote POST `/api/local-session`. Its record remains in the
history. Later startup checks passed without a production or verifier workaround
for that route.

## Scope

Playwright WebKit uses actual native hosts on both Mac architectures, separately
from the dedicated window’s system WKWebView. Physical Mac mouse input and
system WKWebView event forwarding remain unverified. Native window lifecycle
checks are separate. Physical speaker, projector, clean-machine and affected
UTM viewer acceptance remain incomplete; automated passes do not mark them passed.
