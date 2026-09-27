# Smart Stage v1.4.0 verification

Release source: `e38d6155f56f7f3757363988353f0475fed26f5a`. Installer-default source: `e47bf366cdbc2c16ed1c1d35534b95286a38ad17`.

All seven candidate gates passed before tagging. All seven tagged build/security
checks and publication passed. The independent asset audit verified 50 assets,
eight checksum pairs and six exact public executable vulnerability scans.
The tagged run finished with 15 of 16 post-publication jobs passed and one failed
Windows ARM64 installer job. Its final strict global firewall snapshot comparison
failed after installation and app lifecycle checks. The original harness did not
retain the snapshots, so the changed field and cause remain unknown. The
[original failure](windows-arm64-installer-failure.json) and failed workflow
status are preserved. Four actual updater reports and all four separate default
installer architecture jobs passed. The separate Windows ARM64 job passed the
same strict firewall check in both built-in Windows PowerShell and PowerShell 7
against the same published binaries; this does not explain the original failure.
The [sanitized confirmation](default-arm64-confirmation.json) verifies the exact
public executable and installer hashes. Diagnostic-only harness commit
`134d6f003709b456cef6208e7d56b5eff55c10ea` adds retained snapshots and a
difference while preserving the strict assertion; release binaries are unchanged.

Native scene probes and browser controls passed on all four targets. Mac runners
had audio endpoints and verified audio/video pause and seeking. Windows runners
had no audio endpoints: video and transport checks passed, and audio verification
is explicitly unavailable. Physical speakers, projectors, routing and cursor
visibility on a physical/UTM display remain unverified by hosted CI.

The browser fixture and HTTPS relay reports state their simulated boundaries.
Screenshots use fixture labels and mask connection data. Native evidence is a
sanitized subset with original artifact/member hashes; it excludes device IDs,
host paths, cookies, raw logs and session credentials. The failed first candidate
is described in change-scope.md; these gate/native records are the final source.
