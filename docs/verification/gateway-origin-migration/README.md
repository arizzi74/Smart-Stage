# Dedicated gateway origin migration

On 22 September 2026, the running v1.1.2 Linux gateway was moved from the existing
website's HTTPS virtual host to a dedicated HTTPS hostname. This was an
operational configuration change; the released executable was not changed.
Operational hostnames are replaced with role-based examples in these reports.

The managed `/smartstage` reverse-proxy block was moved to the new virtual host,
and only `publicURL` changed in the private gateway configuration. The token,
loopback listener, configuration ownership/permissions, service unit and binary
were retained. The new virtual host no longer serves the shared website document
root; unrelated paths return 404. Old gateway paths return 410 rather than
forwarding authenticated registrations through a redirect. nginx configuration
validation succeeded before reload, the gateway restarted successfully, and
private rollback backups were retained outside nginx include directories.

## Verified on the live deployment

- [Deployment checks](deployment.json): new public HTTPS and loopback health
  return 200; nginx and the gateway are active; old gateway paths return 410;
  the original website root remains available and its protected area still
  requires authentication. Existing TLS certificates were preserved.
- [Gateway smoke](gateway-smoke.json): a disposable endpoint registered through
  the new hostname, paired after invalid guesses, enforced session/CSRF rules,
  accepted paired STOP/emergency STOP, revoked logout and cleaned up. No commands
  targeted a user's show.
- [Routing checks](origin-routing.json): non-health requests bearing the old
  Host are rejected with 403, the old Origin cannot issue a command through the
  new host, and Admin is not exposed.
- [Browser origin checks](browser-origin.json): a fresh Chromium context at the
  new HTTPS origin could not read the old website using a credentials-included
  fetch. Browser diagnostics explicitly reported a missing CORS allow-origin
  header. CSP was bypassed for this test so the result demonstrates origin/CORS
  enforcement rather than a CSP block. TLS certificate checking remained on;
  no actual user credentials or account data were used.

Each desktop must save the new gateway URL in **Admin → Remote control →
Connection settings** and explicitly re-enter the existing 64-character gateway
registration token. Leaving it blank works only when the URL is unchanged;
changing the hostname requires explicit token entry. A rejected save preserves
the old settings and token. Phones then use the current QR code. Registered
clients intentionally do not follow registration redirects.

## Finding 1 scope

This deployment no longer serves registered-desktop active content under the
unrelated website's origin, addressing the demonstrated co-hosted-site exposure.
This does not make one gateway safe for mutually untrusted registered computers:
their endpoint paths still share one gateway origin. Give the registration token
only to mutually trusted hosts until separate endpoint origins or trusted
gateway-owned active content are implemented.

Sibling subdomains are different origins but remain the same site. Parent-domain
cookies, cross-origin writes/CSRF and application-specific credentialed CORS
policies require their own controls. The browser check did not audit every
authenticated application route or prove those separate properties. Other
installations still need their own dedicated-host configuration.
