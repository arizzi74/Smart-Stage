# Preview 9 verification: updater failed closed

Preview 9 is source `d0d69d93349f2ffbf4151b3d7dddea54ba0a330d`.
The [tagged workflow](https://github.com/arizzi74/Smart-Stage/actions/runs/35511624437)
published it after shared and four native build/application checks passed.
Independent public downloads of all six ZIPs matched checksums, architecture,
clean source metadata and bundle/core identity; `archive-verification.json`
records those static observations.

The real automatic-update checks failed before replacement. Go builds using
`-trimpath` omit the linker flags from build information, but the updater
incorrectly required that field to validate the candidate version. The published
binaries contain the correct release version in their main module metadata.
The failed `auto-update-*.json` results retain the exact error and startup
reservation observations. These records do not establish successful updates.

The default Mac installer remained on preview 8. Preview 9 is superseded by
preview 10, whose correction and complete update checks are recorded separately
in [`../release-preview10/`](../release-preview10/).
