# Gateway local verification

Verified on 2026-09-20 at 18:36 UTC in the Linux arm64 development workspace with Go 1.26.5. These checks used the working tree before the preview 15 release commit, based on `4120b18100b1`; they are development evidence, not a claim about a published release.

## Real reverse proxies

Both `nginx -t` and Caddy's `adapt --validate` accepted the generated configuration. The production gateway and outbound host client also ran through each real proxy with isolated loopback listeners and a temporary TLS certificate trusted only by the test client.

| Proxy | Version / source | Result |
| --- | --- | --- |
| nginx | Existing `/usr/sbin/nginx`, 1.18.0 (Ubuntu) | Passed |
| Caddy | Official GitHub `v2.11.4`, Linux arm64 archive, extracted under `/tmp/smartstage-caddy-validator` | Passed |

The Caddy archive was verified against its official SHA-512 checksum file before extraction. Archive SHA-512: `d5a7c423853c24a799765e0e8210d5c7c22a8f56ed37a3cae2fb9f58be138853c02b4efd6b59d576e6d8c7c0d30b9c1592deeaa6a536ff69bcca23b8c1ea709c`. Caddy reported `v2.11.4 h1:XKxkMTgNSizEvKG6QHue6cAsFOteU2qA61w2tKkCWi0=`.

`TestRealReverseProxiesRelayWebSocketAndSSE` passed for both proxies:

- Outbound authenticated WebSocket registration using the production client and relay.
- HTTPS remote page retrieval and a forwarded STOP request.
- Rejection of Admin, file-browser, and Quit routes with HTTP 404.
- Delivery of the first SSE event before the stream ended, verifying buffering was disabled.
- Propagation of SSE cancellation back to the host and clean connection shutdown.

The tests used the installer-generated proxy blocks, substituting temporary upstream/listener ports and a test certificate. No system configuration, public listeners, service units, or firewall rules were installed or changed.

## Installer and build checks

```sh
PATH="$PATH:/usr/sbin:/tmp/smartstage-caddy-validator" \
  go test -race -v ./internal/gatewayinstall
PATH="$PATH:/usr/sbin:/tmp/smartstage-caddy-validator" \
  go test -race ./internal/gatewayinstall ./internal/gateway ./cmd/smartstage-gateway ./internal/qrcode
go vet ./internal/gatewayinstall ./internal/gateway ./cmd/smartstage-gateway
VERSION=dev sh scripts/build-gateway.sh
sh -n install-gateway.sh scripts/build-gateway.sh
```

Installer tests also invoke the real shell bootstrap with isolated download fixtures: valid bytes execute with forwarded arguments, while a corrupt checksum fails before execution. Other tests cover quote/comment/brace parsing, included configuration, suitable-host filtering, conflicting routes, recursive or unresolved includes, repeat installation, existing-token preservation, explicit URL migration, refusal of concurrent source edits and symlinks, rollback of changed/new files, and preservation of existing Caddy sites. Hidden backups avoid duplicate nginx sites under ordinary `sites-enabled/*` includes.

Both architecture builds passed ELF checks for absence of `INTERP` and `NEEDED`. Their metadata reports `CGO_ENABLED=0`, `GOOS=linux`, and the matching architecture. The arm64 executable ran locally and reported `dev (4120b18100b1)`; its embedded ISC notice was read with `--licenses`.

| Development binary | SHA-256 |
| --- | --- |
| Linux amd64 | `1b3e6109f8618dd55cc6d1d54b5d52f897b2f2e0611002bc840bbf70c6e9620a` |
| Linux arm64 | `6f230c7748459257ef86369c7aeffe782ef42e48a20e4477c781dcdc4a6a3917` |

Python syntax checks passed for the adjusted application, download, icon, macOS installer, and automatic-update verifiers. Those native verifiers still require their matching operating-system runners. Real public DNS, ACME issuance, privileged package installation, systemd installation, and an end-user public server were not exercised here.

## Ubuntu Caddy compatibility follow-up

At 18:45 UTC on 2026-09-20, candidate CI revealed that Caddy 2.6.2 retained the upstream SSE request after the phone disconnected. This was reproduced locally with Ubuntu's `caddy_2.6.2-6ubuntu0.24.04.3_arm64.deb`, extracted under `/tmp/smartstage-caddy-ubuntu` without installation. The downloaded package SHA-256 was verified against the official Ubuntu `noble-updates/universe/binary-arm64/Packages.xz` metadata: `83c00e66c175a8f86c5085a0a67c802cc72f7b43bb4f55eae024041a3e6ac686`.

The generated Caddyfile now omits `flush_interval -1`. SSE responses already flush automatically; the negative override disables upstream cancellation in that older Caddy release. The unchanged three-second cancellation check then passed with both Ubuntu Caddy 2.6.2 and official Caddy 2.11.4, as did WebSocket registration, first-event delivery, STOP forwarding, private-route rejection, and shutdown. No test timeout was increased.

```sh
PATH="/tmp/smartstage-caddy-ubuntu/extracted/usr/bin:$PATH:/usr/sbin" \
  go test -race -count=1 -v ./internal/gatewayinstall
PATH="/tmp/smartstage-caddy-validator:$PATH:/usr/sbin" \
  go test -race -count=1 \
  -run 'TestGeneratedCaddyConfigWithRealValidator|TestRealReverseProxiesRelayWebSocketAndSSE' \
  -v ./internal/gatewayinstall
```

Both commands passed on the working tree based on candidate `66c8cfb88c22`. The earlier development-binary hashes above predate this compatibility fix.
