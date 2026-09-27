# Smart Stage v1.5.0 verification

Release source: `529b0e901e128480abaec167b0fe4acd195230a2`. Installer-default source: `7ea6f395a6a086e469a4d3b1e1d050172f03394d`.

All seven candidate gates passed before tagging. All tagged build/security and
publication gates passed. The public audit verified 50 assets, eight checksum
pairs and six exact executable vulnerability scans. All 16 post-publication
jobs, four actual automatic updates and four separate default-installer jobs passed.

Native visual fade checks and browser settings tests passed on all four targets.
Audio settings include video soundtracks and are separate from picture fades.
Native probes cover disabled groups, differing completion orders and retained
source lifetimes. Audio checks are unavailable on targets with no audio endpoint:
windows/amd64, windows/arm64. Their visual checks passed; unavailable audio is not counted as passed.
Physical speakers, routing, projection and perceived smoothness need hardware testing.

Legacy settings/file migration, independent disabled states and fractional
durations are covered by Go and browser tests. Browser-only fixtures explicitly
do not claim native rendering. The screenshot uses fixture content. Native
records are sanitized and include artifact/member hashes; no host paths, device
IDs, credentials or raw logs are published here.
