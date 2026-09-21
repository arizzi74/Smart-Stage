Smart Stage preview 20 asks for confirmation when you close Windows Admin.

- Clicking **X** or pressing **Alt+F4** opens **Quit Smart Stage?**
  Choose **OK** to stop playback, close the stage and quit the application.
  **Cancel** is selected by default and keeps the window and playback running.
- Minimize Windows Admin to keep playback running in the background.
  Existing **Quit Smart Stage** menu/Admin controls still exit directly.
- Repeated close requests do not stack confirmations. Application shutdown and
  update restarts can dismiss an open confirmation without getting stuck.
- Retains preview 19's Windows stage cursor fix for stationary and moving
  pointers, with restoration outside the stage or when it turns off.
- Retains preview 18's file-detection fix for supported MP4 audio or WAV content
  named `.mp3`; no renaming or conversion is needed for those supported files.
- Retains the dedicated Mac and Windows Admin windows, original-file drops,
  **Choose Media…**, icons, Quit controls and automatic updates before playback.
  Public gateway remains the default and needs no incoming firewall rule.

The stage cursor change uses Windows' native cursor APIs. The affected UTM VM
still needs a visual check to confirm what its host display shows.

Install on Windows from a normal PowerShell window:

```powershell
irm https://raw.githubusercontent.com/arizzi74/Smart-Stage/main/install.ps1 | iex
```

Install the gateway on a Linux server with systemd:

```sh
curl -fsSL https://github.com/arizzi74/Smart-Stage/releases/download/v0.1.0-preview.20/install-gateway.sh | sh
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
