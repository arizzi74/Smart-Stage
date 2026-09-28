# Smart Stage v1.5.1 verification

Release source: `beccb506dbf3eaecebfc5123a76de9b69e13929b`. Installer-default source: `5fd57cad948353daccfba849a9f3edd12630356b`.

All seven candidate gates passed before tagging. All tagged build/security and
publication gates passed. The public audit verified 50 assets, eight checksum
pairs and six exact executable vulnerability scans. Fifteen of 16 tagged
post-publication jobs passed. The [original Mac ARM64 updater attempt](https://github.com/arizzi74/Smart-Stage/actions/runs/36386628208/job/108816936437)
failed during GitHub release discovery because of an anonymous API rate limit
(retry time: 2026-09-28T06:48:17Z); replacement and restart did not
complete in that attempt. Its [failed report](original-tagged-updater-failure.json)
is retained alongside the [original four reports](auto-update-evidence.json).

A [separate four-target updater workflow](https://github.com/arizzi74/Smart-Stage/actions/runs/36388305308) at installer-default
source `5fd57cad948353daccfba849a9f3edd12630356b` subsequently passed all four jobs and actual update reports.
Its [evidence](supplemental-auto-update-evidence.json) verifies the original
tagged fixture artifact digests and hashes, exact release source with older
version metadata, and installed binaries matching the audited public release.
This supplemental result does not change the original tagged failure. All four
latest selected default-installer jobs passed. No product binaries were changed.

An [earlier duplicate Windows ARM64 installer attempt](older-duplicate-installer-failure.json)
failed the final strict global firewall snapshot comparison: one
`Microsoft.StartExperiencesApp` rule appeared (480 rules before, 481 after).
No rule was removed and no Smart Stage-named rule changed; the snapshots do not
identify the creator. The latest selected run at the same installer source and
using the same public binary passed both Windows targets with the strict check
retained. Both results are recorded; not every installer attempt passed.

Real browser/native-host checks on all four targets saved a default background
after hiding its playlist cue, retained an existing stage draft across flag
edits, toggled backgrounds to the saved default or black, and deselected an
override equal to the default while preserving that default. Music continuity
is verified only where a native audio endpoint was available. Targets without
an audio endpoint: windows/amd64, windows/arm64.

Go tests cover pending-load cancellation, request idempotence, default/override
reconciliation, independent foreground state, API state and persistence. Browser
fixtures cover revision races, conflict recovery, localization and the visible
update notice. The update-notice screenshot uses synthetic fixture data; it is
not a recording of a real download. Supplemental updater evidence separately
verifies public release discovery, replacement and restart on all four targets.

Physical speakers, projection, perceived smoothness and this change on a physical
Windows 10 laptop were not verified here. Native records are sanitized and retain
artifact/member hashes without host paths, device IDs, credentials or raw logs.
No production gateway, nginx or firewall changes were made.
