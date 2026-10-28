Smart Stage 1.6.5 checks the Smart Stage website for updates, avoiding GitHub API limits on shared company networks.

- Routine checks read a small public website manifest, without a GitHub account or token. GitHub is contacted for the archive and checksum only when an update is installed.
- Official download URLs, architecture selection, semantic version/channel selection, SHA-256 verification and rollback protection remain in place. A website failure never falls back to the GitHub API.
- The release workflow publishes and verifies the website manifest before testing automatic updates. Company filtering refusals are no longer automatically labelled as rate limiting.
- Startup installation remains automatic before playback; later checks never interrupt a running show.
- Builds use Go 1.26.10, including the latest certificate-validation and HTTP security fixes for the existing Go release line.

Quit Smart Stage fully and reopen it to receive the desktop update before playback starts. Saved shows, language and connection settings are preserved.

The first upgrade still uses your installed version's updater. If that older updater is rate-limited, wait for GitHub's reset time or use the installer below once. Subsequent checks use the website manifest.

This feature works with the existing gateway daemon; no gateway upgrade or restart is required.

Install on Mac:

```sh
curl -fsSL https://raw.githubusercontent.com/arizzi74/Smart-Stage/main/install.sh | sh
```

Install on Windows from a normal PowerShell window:

```powershell
irm https://raw.githubusercontent.com/arizzi74/Smart-Stage/main/install.ps1 | iex
```

[Download ZIPs](https://github.com/arizzi74/Smart-Stage/releases/latest) · [Website](https://arizzi74.github.io/Smart-Stage/) · [Gateway guide](https://github.com/arizzi74/Smart-Stage/blob/main/docs/gateway-install.md).

Use a dedicated gateway hostname and share its registration token only with mutually trusted Smart Stage hosts; endpoint paths on one gateway still share a browser origin. Mac builds remain ad-hoc signed and unnotarized; Windows binaries are not Authenticode signed. Windows uses Microsoft WebView2, supplied by the installer if missing. [Verification and known limits](https://github.com/arizzi74/Smart-Stage/blob/main/docs/release-verification.md).
