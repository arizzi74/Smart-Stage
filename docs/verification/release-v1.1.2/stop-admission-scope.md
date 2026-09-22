# Finding 2 remediation and verification scope

Candidate source: `6c93a24d4b7c3c1eac96bf400fd8cec8520f5425` (v1.1.2 candidate; publication is checked separately).

## Change

The public gateway rejects STOP and emergency-stop requests whose command-session cookie was not issued by that endpoint's successful desktop pairing response. The rejection happens before reading the body or occupying a STOP slot. It returns HTTP 401 with the existing `unpaired` JSON error so the remote can reopen pairing. HTTP/1 incomplete requests close instead of being drained before responding.

Admission stores only SHA-256 session identifiers, independently for each tunnel, with at most 128 entries and at most 24 hours of lifetime (shorter cookie expiry is respected). Failed pairings and invalid CSRF do not evict live sessions. Successful logout and authoritative session-protected 401 responses invalidate admission; tunnel disconnect discards the entire cache. Full caches do not evict existing sessions to accommodate new unrelated sessions.

Bodies have an independent bounded read pool (32 ordinary, 32 streams, 4 STOP), a 1 MiB byte limit and a 5-second read deadline. Relay command capacity is acquired after the complete bounded read. The existing desktop session, Origin and CSRF checks remain authoritative. No wire-protocol changes are required for existing desktop pairing responses.

## Evidence checked

- `internal/httpapi/gateway_security_test.go` uses a real TLS listener, WebSocket tunnel and production command API with `auth.Manager`; native playback alone uses a test backend.
- Both `/api/stop` and `/api/emergency-stop` remain HTTP 202 and increment the host stop epoch while four incomplete requests supply either no session cookie or forged random cookies. Attack requests receive JSON 401 before completing their bodies.
- 32 anonymous incomplete pairing bodies fill the ordinary read pool; the 33rd pairing receives 429, while previously paired STOP and emergency-stop still succeed and increment the host stop epoch.
- Four incomplete requests with a legitimately issued cookie fill the distinct STOP read pool. They time out after the gateway's 5-second deadline, before the fixture's 15-second server timeout. Both commands succeed again after those readers are released.
- Failed re-pair with an existing cookie returns 401 but leaves that original cookie and CSRF usable for both commands.
- `internal/gateway/admission_test.go` verifies bounded cookie parsing, endpoint separation, successful-pair-only learning, expiry without sliding extension, successful logout, failed-re-pair/CSRF retention, cache pressure without live-session eviction, concurrent access, exact read-byte bounds, deadline reset rules and slot release on successful/failed reads.
- Existing gateway tests still prove ordinary relay saturation does not starve STOP, narrow route/header forwarding, stream cancellation and disconnect cleanup.
- `internal/gatewayinstall/proxy_linux_test.go` now pairs through the actual nginx and Caddy reverse proxies using real session/CSRF validation, then verifies STOP, immediate SSE delivery, stream cancellation and connection cleanup. Both real proxy subtests passed locally; neither was skipped.
- Local race-enabled gateway tests passed (`go test -race ./internal/gateway`, 2.233s); the real TLS security integration suite passed (7.4s, separate agent); the full gateway-installer suite with real nginx/Caddy available passed (`go test -race ./internal/gatewayinstall`, 2.040s). Final source/native/security publication gates are recorded separately by the release audit.

## Limits

This resolves the four-unauthenticated-request starvation demonstrated in finding 2. It is not a guarantee against general network/CPU/connection-exhaustion attacks. A party possessing a legitimate session cookie can still temporarily occupy the bounded authenticated body pool; timeouts limit each hold, and desktop authorization still governs execution. Anonymous ordinary-read saturation can still delay new pairing, without consuming paired STOP capacity.

The tests establish host command acceptance and stop-epoch changes; they do not establish physical speaker silence or projector behavior. Finding 1’s same-origin isolation remains deliberately outside this remediation. The gateway must be updated for the admission fix to take effect; restarting it rotates endpoint URLs and requires phones to reconnect using the current Admin link/QR.
