Smart Stage preview 16 simplifies Admin and makes the remote connection settings
less intrusive.

- Removes the **ON THE HOST COMPUTER / Host files** browser and its navigation
  entry. In the Mac app, add original files with Finder drag-and-drop,
  **Choose Media…**, or the Dock icon. Files stay in place.
- Collapses **Remote control → Connection settings** by default. Expand it to
  change Public gateway / Local network, the URL or registration token. The
  current remote link, QR code and connection status remain visible.
- Keeps **Public gateway** as the default. Installing or automatically updating
  in gateway mode does not ask for administrator authorization to configure the
  firewall, and does not open the LAN listener.
- Saving **Local network** explains the incoming-connection requirement and
  restarts Smart Stage for the existing scoped firewall setup. Admin keeps
  firewall instructions visible after restart.
- Browser-only hosts without a native chooser retain a small **Add files by
  path** form in Playlist. It is hidden in the dedicated Mac app.

Install the gateway on a Linux server with systemd:

```sh
curl -fsSL https://github.com/arizzi74/Smart-Stage/releases/download/v0.1.0-preview.16/install-gateway.sh | sh
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

The public gateway and all stage/image/background/audio-fade features are retained.
Gateway and browser integration checks cover authenticated registration, scoped
pairing/cookies, CSRF, streaming status, STOP capacity, private API exclusion,
reconnection and closed LAN access. Native builds, extracted downloads,
installers and actual automatic updates are checked on all four desktop targets.
Physical speaker/projector routing and phone power-management behavior still
require testing on real equipment. This remains a preview release.

[Downloads and first-show setup](https://github.com/arizzi74/Smart-Stage#download-zips) ·
[Verification](https://github.com/arizzi74/Smart-Stage/blob/main/docs/release-verification.md)
