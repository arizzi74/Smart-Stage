# Gateway URL-only saves

The desktop accepts an authenticated Admin save of a new gateway URL with an empty or omitted token. It retains the existing registration token, persists the new URL and reconnects to that selected HTTPS endpoint. An explicitly entered token replaces the saved value. First-time setup still requires a valid token.

The blank-field rule applies after preserving the existing gateway settings for a Local LAN switch with both fields blank. HTTPS URL validation, token format checks, private persistence, Admin session/CSRF checks and refusal to follow registration redirects remain in place. The local status response exposes only `hasToken`, never the stored secret. The Admin form explains in English and Italian that saving this URL uses the stored token.

## Verification

- Full local Go race suite, including real nginx/Caddy proxy checks, passed.
- Go vet and source package vulnerability scan passed.
- The complete browser fixture and all 11 language/wake-lock tests passed.
- Settings tests cover canonical URLs, changed hostname, whitespace-only token, explicit replacement, first-time missing token, invalid URL/token, save I/O failure, and blank-field LAN preservation.
- A real TLS/WebSocket test registers with one hostname, saves a second hostname without a token, verifies the new endpoint responds, observes the old endpoint gone, reloads the saved URL/token, then checks LAN switching retains those settings.
- Browser tests verify initial token required, saved token optional after URL change, URL-only API payload omits the token, a page reload retains the new URL without disclosing the token, explicit replacement, and translated Italian guidance.

The gateway daemon does not implement desktop settings. This patch does not require restarting or upgrading the existing v1.1.2 production gateway. Its dedicated HTTPS hostname remains unchanged. Native platform and publication results are recorded separately after the candidate/tag workflows and public download audit.
