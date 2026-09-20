// Invoked by TestGatewayBrowser, which owns real Admin + TLS gateway servers.
// No API route, cookie, SSE stream, asset, or pairing response is mocked here.
const { chromium } = require('playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

(async () => {
  const chunks = [];
  for await (const chunk of process.stdin) chunks.push(chunk);
  const config = JSON.parse(Buffer.concat(chunks).toString('utf8'));
  fs.mkdirSync(config.outputDir, { recursive: true });
  const browser = await chromium.launch({ headless: true });
  const errors = [], publicRequests = [], mutations = [];
  try {
    const adminContext = await browser.newContext({ ignoreHTTPSErrors: true });
    const admin = await adminContext.newPage();
    admin.on('pageerror', error => errors.push(error.message));
    await admin.goto(config.adminURL + '/admin');
    await admin.locator('#connection.live').waitFor();
    await admin.waitForFunction(() => document.getElementById('gateway-mode').value === 'gateway' && !document.getElementById('gateway-fields').hidden);
    assert.equal(await admin.locator('#remote-ready').isVisible(), false, 'unconfigured default gateway must not advertise a LAN link');
    await admin.locator('#gateway-url').fill(config.gatewayURL);
    await admin.locator('#gateway-token').fill(config.registrationToken);
    const save = admin.waitForResponse(response => response.url().endsWith('/api/gateway') && response.request().method() === 'PUT');
    await admin.locator('#save-gateway').click();
    assert.equal((await save).status(), 200);
    await admin.locator('#remote-ready').waitFor({ state: 'visible' });
    const remoteURL = await admin.locator('#remote-url').getAttribute('href');
    const parsed = new URL(remoteURL), prefix = parsed.pathname.slice(0, -'/command'.length);
    assert.match(parsed.pathname, /^\/smartstage\/e\/[a-f0-9]{32}\/command$/);
    assert.match(parsed.hash, /^#token=[a-f0-9]{64}$/);
    assert.notEqual(parsed.hash.slice(7), config.registrationToken);
    assert.equal(await admin.locator('#gateway-token').inputValue(), '', 'saved gateway secret stays out of the form');
    await admin.waitForFunction(() => document.getElementById('remote-qr').naturalWidth > 0);
    await admin.screenshot({ path: path.join(config.outputDir, 'gateway-admin.png'), fullPage: false });

    // Follow the real target=_blank link, which starts as a cross-site
    // navigation from loopback HTTP Admin to the HTTPS public gateway.
    const popupReady = admin.waitForEvent('popup');
    const popupDocument = adminContext.waitForEvent('response', response => response.request().isNavigationRequest() && response.url() === remoteURL.split('#')[0]);
    await admin.locator('#open-remote-url').click();
    const popup = await popupReady;
    assert.equal((await popupDocument).status(), 200);
    await popup.locator('#connection.live').waitFor();
    await popup.close();

    const context = await browser.newContext({ ignoreHTTPSErrors: true, viewport: { width: 390, height: 844 }, hasTouch: true });
    const page = await context.newPage();
    page.on('pageerror', error => errors.push(error.message));
    page.on('request', request => {
      const u = new URL(request.url());
      if (u.origin === parsed.origin) {
        publicRequests.push(u.pathname);
        if (request.method() === 'POST') mutations.push({ path: u.pathname, request });
      }
    });
    await page.addInitScript(() => {
      window.__gatewaySSEStates = [];
      window.__gatewayCSP = [];
      document.addEventListener('securitypolicyviolation', event => window.__gatewayCSP.push(event.violatedDirective));
      const RealEvents = window.EventSource;
      window.EventSource = class extends RealEvents {
        constructor(...args) {
          super(...args);
          this.addEventListener('state', event => {
            window.__gatewaySSEStates.push(JSON.parse(event.data));
            if (window.__gatewaySSEStates.length > 100) window.__gatewaySSEStates.shift();
          });
        }
      };
      const realFetch = window.fetch;
      window.fetch = (...args) => {
        if (String(args[0]).endsWith('/api/pair')) window.__pairingHash = location.hash;
        return realFetch(...args);
      };
      window.__wakeRequests = 0;
      window.__wakeReleases = 0;
      Object.defineProperty(navigator, 'wakeLock', { configurable: true, value: {
        async request(kind) {
          if (kind !== 'screen') throw new Error('Wrong wake-lock kind');
          window.__wakeRequests++;
          const sentinel = new EventTarget();
          sentinel.released = false;
          sentinel.release = async () => { sentinel.released = true; window.__wakeReleases++; sentinel.dispatchEvent(new Event('release')); };
          return sentinel;
        }
      }});
    });
    const documentResponse = await page.goto(remoteURL);
    assert.equal(documentResponse.status(), 200);
    assert.match(documentResponse.headers()['content-security-policy'], /script-src 'self'/);
    await page.locator('#connection.live').waitFor();
    await page.waitForFunction(() => window.__gatewaySSEStates.length > 0);
    assert.equal(await page.evaluate(() => window.isSecureContext), true);
    assert.equal(await page.evaluate(() => location.hash), '');
    assert.equal(await page.evaluate(() => window.__pairingHash), '');
    assert.equal(await page.evaluate(() => localStorage.length + sessionStorage.length), 0);
    const cookies = (await context.cookies()).filter(cookie => cookie.name === 'smartstage_command_session');
    assert.equal(cookies.length, 1);
    assert.equal(cookies[0].secure, true);
    assert.equal(cookies[0].httpOnly, true);
    assert.equal(cookies[0].sameSite, 'Strict');
    assert.equal(cookies[0].path, prefix + '/');
    assert.equal(await page.locator('#admin-view').isVisible(), false);
    assert.equal(await page.locator('#quit-app').isVisible(), false);

    const play = page.waitForResponse(response => response.url().endsWith('/api/play'));
    await page.locator('[data-cue-id="gateway-browser-cue"]').click();
    assert.equal((await play).status(), 202);
    await page.waitForFunction(() => window.__gatewaySSEStates.some(state => state.state === 'playing' && state.activeCueId === 'gateway-browser-cue'));
    await page.locator('[data-cue-id="gateway-browser-cue"].active').waitFor();
    await page.locator('#keep-awake').click();
    await page.waitForFunction(() => document.getElementById('keep-awake-status').textContent === 'On');
    assert.equal(await page.evaluate(() => window.__wakeRequests), 1);
    const beforeStop = await page.evaluate(() => window.__gatewaySSEStates.at(-1).stopEpoch);
    const stop = page.waitForResponse(response => response.url().endsWith('/api/stop'));
    await page.locator('#stop').click();
    assert.equal((await stop).status(), 202);
    await page.waitForFunction(epoch => window.__gatewaySSEStates.some(state => state.stopEpoch > epoch && state.state === 'stopped' && !state.activeCueId), beforeStop);
    await page.screenshot({ path: path.join(config.outputDir, 'gateway-remote-phone.png') });
    assert.equal(await page.locator('#keep-awake-status').textContent(), 'On');
    assert(publicRequests.includes(prefix + '/assets/app.js'));
    assert(publicRequests.includes(prefix + '/assets/style.css'));
    assert(publicRequests.includes(prefix + '/assets/wake-lock.js'));
    assert(publicRequests.includes(prefix + '/api/events'));
    assert(publicRequests.every(p => p.startsWith(prefix + '/') || p === '/favicon.ico'), 'remote requests escaped endpoint path');
    for (const m of mutations.filter(m => !m.path.endsWith('/api/pair'))) {
      const headers = await m.request.allHeaders();
      assert.equal(headers.origin, parsed.origin);
      assert(headers['x-csrf-token'], 'real mutating request omitted CSRF');
    }
    assert.deepEqual(await page.evaluate(() => window.__gatewayCSP), []);
    assert.deepEqual(errors, []);

    const logout = page.waitForResponse(response => response.url().endsWith('/api/logout'));
    await page.locator('#logout').click();
    assert.equal((await logout).status(), 200);
    await page.locator('#pairing').waitFor({ state: 'visible' });
    assert.equal((await context.cookies()).filter(c => c.name === 'smartstage_command_session').length, 0);
    assert.equal(await page.evaluate(() => window.__wakeReleases), 1);
    const report = {
      actualTLSGateway: true, actualHostTunnel: true, actualAdminConfiguration: true,
      actualAdminOpenRemoteLink: true,
      actualPairingAndScopedSecureCookie: true, actualCSRFMutations: true,
      actualSSEPlaybackTransitions: true, endpointRelativeAssetsAndRequests: true,
      publicPairingFragmentRemovedBeforeRequest: true, noPairingSecretInBrowserStorage: true,
      CSPWithoutViolations: true, logoutRevokesCookieAndWakeLock: true,
      HTTPSWakeLockRequested: true, nativePlaybackSimulated: true, deviceWakeLockGrantSimulated: true,
      publicRequestCount: publicRequests.length, browserErrors: errors
    };
    fs.writeFileSync(path.join(config.outputDir, 'gateway-browser.json'), JSON.stringify(report, null, 2) + '\n');
    process.stdout.write(JSON.stringify(report) + '\n');
    await context.close(); await adminContext.close();
  } finally { await browser.close(); }
})().catch(error => { console.error(error.stack || error); process.exitCode = 1; });
