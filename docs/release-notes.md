Smart Stage preview 18 fixes a Windows media-opening mismatch between import and playback.

- When Windows rejects a file's byte-stream type (`0xc00d36c4`), Smart Stage
  retries using Windows' content detection. This allows supported media with
  a misleading extension, such as MP4/AAC or WAV content named `.mp3`, to open without
  renaming or converting the file.
- Import and playback now use the same source-opening policy. Import still
  decodes a real sample before marking a cue ready; damaged files remain errors.
- Retains the dedicated Mac and Windows Admin windows, original-file drops,
  **Choose Media…**, icons, Quit controls and automatic updates before playback.
  Public gateway remains the default and needs no incoming firewall rule.

This change addresses the source-opening failure before audio output starts.
It does not add codecs or establish physical audio/VM compatibility for every
file. Retesting the originally reported file on the affected Windows VM is
still required.

Install on Windows from a normal PowerShell window:

```powershell
irm https://raw.githubusercontent.com/arizzi74/Smart-Stage/main/install.ps1 | iex
```

Install the gateway on a Linux server with systemd:

```sh
curl -fsSL https://github.com/arizzi74/Smart-Stage/releases/download/v0.1.0-preview.18/install-gateway.sh | sh
```

[Gateway setup, prerequisites and recovery](https://github.com/arizzi74/Smart-Stage/blob/main/docs/gateway-install.md).
Caddy needs a domain pointing at the server and reachable ports 80/443. A public
server is required; Smart Stage does not provision hosting. Existing shows are
preserved. Installations without saved remote-mode settings now start in public
gateway mode: configure a gateway or explicitly select Local LAN to reconnect
phones. Local Admin still opens at `http://127.0.0.1:8787/admin`.

Mac installation with the app icon and dedicated window:

```sh
curl -fsSL https://raw.githubusercontent.com/arizzi74/Smart-Stage/main/install.sh | sh
```

The Mac installer verifies the matching archive, installs in `~/Applications`,
and removes quarantine only from Smart Stage. The Windows installer uses
`%LOCALAPPDATA%\Programs\SmartStage`; no administrator prompt is needed in
default gateway mode. Standalone desktop ZIPs still
contain one executable. Mac app bundles and all four desktop architectures are
available below. Windows ZIP users need Microsoft Evergreen WebView2 installed;
the one-command installer checks this automatically. Also available are the two Linux gateway binaries and checksums.

Automatic updates install on launch before playback. An update from preview 14
or earlier uses that older updater and may still show its firewall approval
once; later gateway-mode updates skip firewall setup. Mac releases remain ad-hoc
signed, not Developer ID signed/notarized; Windows executables are not
Authenticode signed.

The public gateway and all stage/image/background/audio-fade features are retained.
Gateway and browser integration checks cover authenticated registration, scoped
pairing/cookies, CSRF, streaming status, STOP capacity, private API exclusion,
reconnection and closed LAN access. Native builds, extracted downloads,
installers and actual automatic updates are checked on all four desktop targets.
Physical speaker/projector routing and phone power-management behavior still
require testing on real equipment. This remains a preview release.

[Downloads and first-show setup](https://github.com/arizzi74/Smart-Stage#download-zips) ·
[Verification](https://github.com/arizzi74/Smart-Stage/blob/main/docs/release-verification.md)
