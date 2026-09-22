# Security maintenance: findings 2, 3 and 4

Source: `6c93a24d4b7c3c1eac96bf400fd8cec8520f5425`.

Finding 2 is fixed in the Linux gateway. It learns command sessions only from the corresponding desktop's successful pairing response and rejects unpaired STOP requests before reading bodies or reserving STOP capacity. Separate bounded body-reading pools and deadlines prevent ordinary anonymous slow requests from occupying the paired command reserve. Desktop session, Origin and CSRF checks remain authoritative.

Finding 3 is addressed by rebuilding all six executables with Go 1.26.8. The build workflow scans imported Go packages and the actual platform executables with govulncheck v1.8.0; both source and binary checks gate publication. These scans cover the Go runtime and dependencies, not native operating-system libraries or unknown vulnerabilities.

Finding 4 is fixed in the desktop authentication manager. The correct 256-bit public-gateway key can create a session independently of failed-guess budgets. Invalid public keys still consume those budgets, session capacity/expiry remain bounded, and the shorter eight-digit LAN code retains its original guessing limits. This preserves compatibility with existing gateways and clients behind shared addresses without accepting untrusted forwarded-IP headers.

## Regression evidence

- Real TLS/WebSocket integration reproduces anonymous STOP starvation and valid-public-key lockout against the original code using temporary Go overlays. The same regressions pass with the fixes.
- Slow anonymous bodies cannot block paired STOP or emergency STOP; successful acceptance also advances the host stop epoch. Anonymous pairing saturation cannot consume the STOP pool. Authenticated incomplete bodies are released by their five-second deadline.
- Failed re-pair and incorrect CSRF do not evict a valid admission; logout and expired sessions revoke it. Admission remains scoped to a single endpoint, bounded in size, and discarded on disconnect.
- Public pairing works after peer-wide and global invalid-guess budgets are exhausted. Wrong tokens stay rejected, and session capacity and expiry still apply.
- Both actual nginx and Caddy fixtures pair successfully, relay authenticated STOP and stream SSE. The fixture was updated to pair before issuing protected commands; protection was not relaxed to accommodate the old test.
- Local Go race tests, vet, 11 JavaScript tests and the actual HTTPS browser fixture passed. Candidate, tagged release and public-asset evidence are recorded separately.

## Scope retained

Finding 1 remains open at the user's request until a separate-domain migration is arranged. A dedicated domain protects unrelated co-hosted applications; mutually untrusted endpoints on one origin still need an isolation design. Release publisher signing and notarization were outside this remediation.

The fixes do not guarantee availability against generic network/CPU exhaustion or malicious clients that already possess a valid session. Physical playback and projector behavior are not established by command-acceptance tests. A separate optional Windows ARM visual workflow still used historical binaries and was obstructed by the hosted runner's Windows privacy setup screen; it is not a security-candidate result.

The first candidate also encountered an intermittent Windows Media Foundation `MF_E_SHUTDOWN` error during repeated video-background inspection. Earlier inspection and native scene checks in that job succeeded. Those native paths are unchanged from v1.1.1, and the cause remains unproven; later successful runs do not establish that the intermittent condition is fixed. No assertion was disabled or timeout extended.
