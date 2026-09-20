# Public gateway deployment

Smart Stage Gateway is a separate, static Go executable for Linux amd64 or arm64. The server runs continuously under systemd. Smart Stage on your Mac or Windows computer opens an outbound encrypted connection to it; your phone opens the HTTPS remote URL shown in Admin. Playback and media stay on the Smart Stage computer.

Install on the public Linux server from an interactive SSH terminal:

```sh
curl -fsSL https://github.com/arizzi74/Smart-Stage/releases/download/v0.1.0-preview.15/install-gateway.sh | sh
```

The bootstrap downloads the matching architecture, checks its published SHA-256 checksum, and runs the Go installer with `sudo`. It reads choices from the terminal even when the script is piped into `sh`. You can also download the raw executable and its `.sha256` file from the release, verify them, and run `sudo ./smartstage-gateway-linux-amd64 install` (or `arm64`).

## Existing nginx HTTPS host

The installer reads the effective configuration with `nginx -T` and lists suitable HTTPS virtual hosts with their source files. Choose the number corresponding to the domain you want. It adds only the managed `/smartstage` locations inside that existing server block; other locations and the current TLS certificate remain in place.

Eligible hosts have an explicit DNS name and a TLS listener on port 443. Wildcards, regex names, duplicate host definitions, loopback listeners, server-wide redirects, conflicting `/smartstage` locations, and ambiguous includes are excluded. Includes, comments, quoted values, and multiple server blocks in one file are parsed before choosing the insertion point. A managed location is recognized on repeated installation.

List hosts without changing files:

```sh
sudo smartstage-gateway install --list-hosts
```

You can preselect a listed hostname with `--host stage.example.com`. The installer still shows the concrete configuration and asks before applying it.

The proxy preserves the `/smartstage` prefix, supports WebSocket connections and streaming responses, disables buffering and access logs for the gateway location, and uses 75-second proxy timeouts. Existing source ownership and permissions are preserved. Configuration validation runs before reload. If validation or reload fails, modified files are restored from their originals; timestamped hidden `.smartstage-backup-*` files are also retained alongside changed files, outside ordinary nginx include globs.

## Caddy when nginx is absent

When nginx is absent, the installer offers Caddy and asks for a dedicated public domain such as `stage.example.com`. It can install Caddy from its official repository on Debian/Ubuntu (`apt-get`) and Fedora/RHEL (`dnf`). On other distributions, install Caddy first and rerun the gateway installer.

The domain must resolve to this server, with inbound TCP ports 80 and 443 available. Caddy obtains and renews its HTTPS certificate automatically. You can prefill the domain with `--caddy-domain stage.example.com`.

An existing Caddyfile is preserved. The installer asks before appending a new managed site, validates the result, then reloads Caddy. If the chosen domain already appears in the Caddyfile, the installer stops rather than guessing how to modify that site. A modified managed block also requires manual review. Existing Caddy sites, custom imports, or another process already using ports 80/443 can require administrator configuration.

## Connect Smart Stage

At the end of installation, copy the displayed gateway URL and authentication token into **Smart Stage Admin → Remote control → Public gateway**. The URL has this form:

```text
https://stage.example.com/smartstage
```

The token is a random 256-bit secret authorizing Smart Stage computers to register with this gateway. Keep it private. Admin shows a separate remote-control URL and QR code for phones and tablets; you do not share the gateway registration token with remote users.

The service listens only on `127.0.0.1:8790`; nginx or Caddy is the public entry point. No inbound LAN connection to the Smart Stage computer is needed in gateway mode. The gateway cannot play your files itself, and the Smart Stage application must stay running and connected.

## Service and updates

Installed paths:

| Item | Location |
| --- | --- |
| Executable | `/usr/local/bin/smartstage-gateway` |
| Configuration and token | `/etc/smartstage-gateway/config.json` |
| systemd unit | `/etc/systemd/system/smartstage-gateway.service` |

The daemon runs as the unprivileged `smartstage-gateway` account with a read-only system filesystem and no capabilities. The config directory is `0750`; the config is `0640`, owned by root and the service group. The token is read from the config, not passed in process arguments.

```sh
sudo systemctl status smartstage-gateway
sudo journalctl -u smartstage-gateway --since '10 minutes ago'
sudo systemctl restart smartstage-gateway
sudo systemctl stop smartstage-gateway
```

Rerun the release installer to update the server binary. The existing token is preserved; changing its public URL requires an explicit manual migration. The gateway does not install server updates by itself. Caddy/nginx and operating-system updates remain managed through your server's normal package maintenance.

For custom deployments, build both static binaries with `sh scripts/build-gateway.sh`, configure the JSON fields `listen`, `publicURL`, and `token`, and run `smartstage-gateway serve --config /path/to/config.json`. The build script uses `CGO_ENABLED=0` and checks that neither ELF has a dynamic loader or linked shared libraries.

The proxy settings follow the [nginx proxy module documentation](https://nginx.org/en/docs/http/ngx_http_proxy_module.html). Package installation follows the [official Caddy installation instructions](https://caddyserver.com/docs/install).
