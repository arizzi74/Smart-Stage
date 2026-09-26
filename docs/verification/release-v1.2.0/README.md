# Smart Stage v1.2.0 verification

Release source: `2ad6960398e09ee988ecdc1c475846c1da17d18f`.
[Candidate run](https://github.com/arizzi74/Smart-Stage/actions/runs/36246297321)
and [tagged release run](https://github.com/arizzi74/Smart-Stage/actions/runs/36247167331)
passed all seven build/security gates; publication succeeded. All 50 public
assets and eight checksum pairs were independently checked, and all six public
executables passed package-level Go vulnerability scans.

`change-scope.md` describes the playlist feature and local checks.
`native-playlist-checks.json` and `native-playlist-release-checks.json` retain
sanitized candidate/release dialog evidence. Mac Save completion and Load
cancellation were observed; successful Mac Load selection is outside that
native probe's coverage. Both Windows architectures exercised Save and Load.
Backend/browser tests separately cover complete file restoration.

`auto-update-evidence.json` records four successful actual public-download
replacement/restart checks. Starting fixtures use this release's source with
older version metadata, rather than historical release executables.
`postpublication-status.json` is a timestamped snapshot: 15 of 16 follow-up jobs
passed, with Windows ARM64 installer verification still running.
`installer-defaults.json` verifies public script bytes at installer commit
`83ef94868813e542adef9f61baa1acc70b3a1326`;
`default-installer-workflows.json` records three passed architecture jobs and
one running Windows ARM64 job. No pending check is represented as passed.

These are hosted automated checks. They do not establish physical speaker or
projector acceptance. The production gateway and firewall were not changed.
