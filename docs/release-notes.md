Smart Stage 1.1.0 adds Italian and English throughout the desktop app and phone/tablet remote.

- Admin starts in the system’s preferred supported language. Choose System, English or Italiano from the top bar; the choice survives restarts and updates.
- Native Mac and Windows menus, media chooser, app messages and Windows quit confirmation follow the Admin language.
- Changing language preserves playlist edits and playback. Cue labels, original file paths and device names remain unchanged.
- The remote follows its own browser language and remembers a separate manual choice. Existing public gateways work without an upgrade.
- The bilingual website includes three short animated tutorials for adding media, choosing outputs and connecting the remote, with stop controls and reduced-motion support.

Quit Smart Stage fully and reopen it to receive this stable update automatically before playback starts. Saved shows and connection settings are preserved.

Install on Mac:

```sh
curl -fsSL https://raw.githubusercontent.com/arizzi74/Smart-Stage/main/install.sh | sh
```

Install on Windows from a normal PowerShell window:

```powershell
irm https://raw.githubusercontent.com/arizzi74/Smart-Stage/main/install.ps1 | iex
```

[Download ZIPs](https://github.com/arizzi74/Smart-Stage/releases/latest) · [Website](https://arizzi74.github.io/Smart-Stage/) · [User guide](https://github.com/arizzi74/Smart-Stage/blob/main/docs/user-guide.md).

Downloads include Mac app bundles and Windows executable ZIPs for ARM64 and AMD64, static Linux gateway binaries and checksums. Mac builds remain ad-hoc signed and unnotarized; Windows binaries are not Authenticode signed. Windows uses Microsoft WebView2, supplied by the installer if missing. Physical speakers/projectors and the previously reported UTM cursor behavior remain outside the automated test scope. [Verification and known limits](https://github.com/arizzi74/Smart-Stage/blob/main/docs/release-verification.md).
