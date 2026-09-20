Smart Stage preview 15 adds a public HTTPS gateway and makes it the default
remote-control mode on Mac and Windows.

- Deploy the new static Linux gateway binary on AMD64 or ARM64 with one command.
  The installer lists existing nginx HTTPS virtual hosts and adds `/smartstage`
  to the selected site. Without nginx it offers Caddy and automatic HTTPS.
- In Admin → Remote control, enter the gateway URL and its registration token.
  Smart Stage connects outward and displays a separate public link/QR for phones
  and tablets. Admin, files and media stay on the event computer.
- Direct LAN access is disabled in gateway mode, including while disconnected.
  Automatic reconnection creates a new endpoint and phone pairing secret.
- Selecting Local LAN explicitly restarts Smart Stage and configures the app's
  incoming firewall allowance. Gateway-mode installation and subsequent updates
  leave firewall rules unchanged.
- The HTTPS remote supports Keep awake on compatible browsers while visible.
  Power saving, switching apps or manually locking can release the wake lock.

Install the gateway on a Linux server with systemd:

```sh
curl -fsSL https://github.com/arizzi74/Smart-Stage/releases/download/v0.1.0-preview.15/install-gateway.sh | sh
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
and removes quarantine only from Smart Stage. Standalone desktop ZIPs still
contain one executable. Mac app bundles and all four desktop architectures are
available below, alongside the two Linux gateway binaries and checksums.

Automatic updates install on launch before playback. An update from preview 14
or earlier uses that older updater and may still show its firewall approval
once; later gateway-mode updates skip firewall setup. Mac releases remain ad-hoc
signed, not Developer ID signed/notarized; Windows executables are not
Authenticode signed.

All stage/image/background/audio-fade features from preview 14 are retained.
Gateway and browser integration checks cover authenticated registration, scoped
pairing/cookies, CSRF, streaming status, STOP capacity, private API exclusion,
reconnection and closed LAN access. Native builds, extracted downloads,
installers and actual automatic updates are checked on all four desktop targets.
Physical speaker/projector routing and phone power-management behavior still
require testing on real equipment. This remains a preview release.

[Downloads and first-show setup](https://github.com/arizzi74/Smart-Stage#download-zips) ·
[Verification](https://github.com/arizzi74/Smart-Stage/blob/main/docs/release-verification.md)
