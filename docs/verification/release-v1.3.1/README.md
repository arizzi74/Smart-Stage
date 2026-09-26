# Smart Stage v1.3.1 verification

This patch changes the Admin layout only. Main and tagged workflows ran in
parallel; the candidate record does not imply completion before tagging.
Publication is gated by shared, native, gateway and binary-security jobs.

- `layout-checks.json` records measured QR/caption bounds using synthetic local
  fixtures in Chromium. `admin-compact-it.png` shows the 1095 × 758 Italian view.
  Small phone Admin windows still scroll; the phone remote layout is unchanged.
- `browser-checks.json` and `change-scope.md` describe local verification.
- `candidate-workflow.json` and `tag-workflow.json` identify exact source and runs.
- `assets.json`, `security-scans.json` and `govulncheck-*.txt` record independent
  public downloads, provenance/checksums and Go package scans of six executables.
- `installer-defaults.json` compares committed defaults with public script bytes.
- `postpublication-status.json`, `auto-update-evidence.json` and
  `default-installer-workflows.json` record actual follow-ups at collection time.

Screenshots and layout fixtures contain only synthetic example.com links.
Native macOS WebKit was not used for local pixel measurements. Existing native
CI exercises the app, but physical display/speaker acceptance remains separate.
No production gateway, nginx or firewall changes were made.
