// Invoked by TestCloudflareBrowser against the real TLS command API.
// Socket loss and session revocation come from a separate Go test controller;
// browser HTTP, cookies, WebSocket traffic, assets, and state are never mocked.
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
  const errors = [], requests = [], mutations = [], socketURLs = [];
  try {
    const context = await browser.newContext({ ignoreHTTPSErrors: true, viewport: { width: 390, height: 844 }, hasTouch: true });
    const page = await context.newPage();
    page.on('pageerror', error => errors.push(error.message));
    page.on('request', request => {
      const url = new URL(request.url());
      requests.push(url.pathname);
      if (request.method() === 'POST') mutations.push(request);
    });
    page.on('websocket', socket => socketURLs.push(socket.url()));
    await page.addInitScript(() => {
      window.__cloudflareStates = [];
      window.__cloudflareHeartbeats = 0;
      window.__cloudflareSocketCount = 0;
      window.__cloudflareAuthFrames = 0;
      window.__cloudflareSSECount = 0;
      window.__cloudflareCSP = [];
      document.addEventListener('securitypolicyviolation', event => window.__cloudflareCSP.push(event.violatedDirective));
      const NativeSocket = window.WebSocket;
      window.WebSocket = class extends NativeSocket {
        constructor(...args) {
          super(...args);
          window.__cloudflareSocketCount++;
          this.addEventListener('message', event => {
            const frame = JSON.parse(event.data);
            if (frame.type === 'state') {
              window.__cloudflareStates.push(frame.state);
              if (window.__cloudflareStates.length > 100) window.__cloudflareStates.shift();
            } else if (frame.type === 'heartbeat') window.__cloudflareHeartbeats++;
          });
        }
        send(data) {
          const frame = JSON.parse(data);
          if (frame.type === 'authenticate' && typeof frame.csrfToken === 'string' && frame.csrfToken) window.__cloudflareAuthFrames++;
          super.send(data);
        }
      };
      const NativeEvents = window.EventSource;
      window.EventSource = class extends NativeEvents {
        constructor(...args) { super(...args); window.__cloudflareSSECount++; }
      };
      const nativeFetch = window.fetch;
      window.fetch = (...args) => {
        if (String(args[0]).endsWith('/api/pair')) window.__cloudflareHashAtPairing = location.hash;
        return nativeFetch(...args);
      };
    });
    const response = await page.goto(config.publicURL + '/command#token=' + config.token);
    assert.equal(response.status(), 200);
    assert.match(response.headers()['content-security-policy'], /connect-src 'self' wss:/);
    await page.locator('#connection.live').waitFor();
    await page.waitForFunction(() => window.__cloudflareStates.length > 0);
    assert.equal(await page.evaluate(() => isSecureContext), true);
    assert.equal(await page.evaluate(() => location.hash), '');
    assert.equal(await page.evaluate(() => window.__cloudflareHashAtPairing), '', 'the pairing fragment is consumed before the first request');
    assert.equal(await page.evaluate(() => localStorage.length + sessionStorage.length), 0);
    assert.equal(await page.locator('#admin-view').isVisible(), false);
    assert.equal(await page.locator('#quit-app').isVisible(), false);
    const sessionCookie = async () => (await context.cookies()).find(cookie => cookie.name === 'smartstage_command_session');
    const firstCookie = await sessionCookie();
    assert(firstCookie, 'pairing must issue its own session cookie');
    assert.equal(firstCookie.secure, true);
    assert.equal(firstCookie.httpOnly, true);
    assert.equal(firstCookie.sameSite, 'Strict');
    assert.equal(firstCookie.path, '/');
    assert.equal(await page.evaluate(() => JSON.stringify(window.__cloudflareStates).includes('PRIVATE_CLOUDFLARE_BROWSER')), false);

    const cue = page.locator('[data-cue-id="cloudflare-browser-cue"]');
    const command = async (route, click) => {
      const acknowledgement = page.waitForResponse(reply => new URL(reply.url()).pathname === route && reply.request().method() === 'POST');
      await click();
      assert.equal((await acknowledgement).status(), 202, 'HTTP command must be acknowledged by the real service');
    };
    await command('/api/play', () => cue.click());
    await page.waitForFunction(() => window.__cloudflareStates.at(-1).state === 'playing');
    await page.locator('[data-cue-id="cloudflare-browser-cue"].active').waitFor();
    await command('/api/play', () => cue.click());
    await page.waitForFunction(() => window.__cloudflareStates.at(-1).state === 'paused');
    await command('/api/play', () => cue.click());
    await page.waitForFunction(() => window.__cloudflareStates.at(-1).state === 'playing');
    const initialEpoch = await page.evaluate(() => window.__cloudflareStates.at(-1).stopEpoch);
    await command('/api/stop', () => page.locator('#stop').click());
    await page.waitForFunction(epoch => {
      const state = window.__cloudflareStates.at(-1);
      return state.state === 'stopped' && state.stopEpoch > epoch;
    }, initialEpoch);

    const previousSockets = await page.evaluate(() => window.__cloudflareSocketCount);
    const previousStates = await page.evaluate(() => window.__cloudflareStates.length);
    const previousStateGets = requests.filter(route => route === '/api/state').length;
    const previousPairs = mutations.filter(request => new URL(request.url()).pathname === '/api/pair').length;
    const previousMutations = mutations.length;
    const disconnected = await fetch(config.controlURL + '/disconnect', { method: 'POST' });
    assert.equal(disconnected.status, 200);
    await page.waitForFunction(count => window.__cloudflareSocketCount > count, previousSockets);
    await page.waitForFunction(count => window.__cloudflareStates.length > count, previousStates);
    await page.locator('#connection.live').waitFor();
    assert(requests.filter(route => route === '/api/state').length > previousStateGets, 'reconnect revalidates the HTTP session');
    assert.equal(mutations.length, previousMutations, 'transport reconnect never replays a playback command');
    assert.equal(mutations.filter(request => new URL(request.url()).pathname === '/api/pair').length, previousPairs, 'an intact cookie reconnects without pairing again');
    await command('/api/play', () => cue.click());
    await page.waitForFunction(() => window.__cloudflareStates.at(-1).state === 'playing');
    await command('/api/stop', () => page.locator('#stop').click());
    await page.waitForFunction(() => window.__cloudflareStates.at(-1).state === 'stopped');
    await page.waitForFunction(() => window.__cloudflareHeartbeats > 0, undefined, { timeout: 15000 });
    await page.screenshot({ path: path.join(config.outputDir, 'cloudflare-remote-phone.png') });

    const logout = page.waitForResponse(reply => new URL(reply.url()).pathname === '/api/logout');
    await page.locator('#logout').click();
    assert.equal((await logout).status(), 200);
    await page.locator('#pairing[open]').waitFor();
    assert.equal(await sessionCookie(), undefined, 'logout clears the public session cookie');
    assert.equal(await cue.isDisabled(), true);
    const revoked = await context.request.get(config.publicURL + '/api/state', { headers: { Cookie: firstCookie.name + '=' + firstCookie.value } });
    assert.equal(revoked.status(), 401, 'logout revokes the server-side cookie session');
    await page.locator('#pair-key').fill(config.token);
    await page.locator('#pair-form button[type=submit]').click();
    await page.locator('#connection.live').waitFor();
    assert.equal(await page.locator('#pairing').evaluate(node => node.open), false);
    const nextCookie = await sessionCookie();
    assert(nextCookie && nextCookie.value !== firstCookie.value, 'pairing again creates a new session');
    const revokedExternally = await fetch(config.controlURL + '/revoke', {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ session: nextCookie.value })
    });
    assert.equal(revokedExternally.status, 200);
    await page.locator('#pairing[open]').waitFor();
    assert.equal(await cue.isDisabled(), true);
    assert.equal(await page.locator('#pair-key').inputValue(), '');
    assert.equal(await page.evaluate(() => localStorage.length + sessionStorage.length), 0);

    assert.equal(await page.evaluate(() => window.__cloudflareSSECount), 0, 'Cloudflare never opens an EventSource fallback');
    assert.equal(requests.includes('/api/events'), false);
    assert(requests.includes('/assets/app.js') && requests.includes('/assets/style.css'));
    assert.equal(await page.evaluate(() => window.__cloudflareAuthFrames === window.__cloudflareSocketCount), true, 'each live socket proves session CSRF');
    assert(socketURLs.length >= 3);
    for (const socketURL of socketURLs) {
      const url = new URL(socketURL);
      assert.equal(url.protocol, 'wss:');
      assert.equal(url.host, new URL(config.publicURL).host);
      assert.equal(url.pathname, '/api/ws');
      assert.equal(url.search + url.hash, '', 'WebSocket credentials never enter the URL');
    }
    for (const mutation of mutations) {
      const route = new URL(mutation.url()).pathname;
      const headers = await mutation.allHeaders();
      assert.equal(headers.origin, config.publicURL);
      if (route !== '/api/pair') assert(headers['x-csrf-token'], 'HTTP commands retain CSRF protection');
    }
    assert.equal(await page.evaluate(() => window.__cloudflareCSP.length), 0, 'CSP must permit the real secure WebSocket');
    assert.deepEqual(errors, []);
    console.log(JSON.stringify({ passed: true, transport: 'real TLS WebSocket', sseRequests: 0, reconnect: true, logoutAndRevocation: true, httpPlaybackCommands: true }));
  } finally {
    await browser.close();
  }
})().catch(error => {
  // Failure output and artifacts must never disclose pairing/session secrets
  // or the public endpoint. Screenshots contain only the consumed-link UI.
  const message = String(error.stack || error).replace(/[a-f0-9]{32,}/gi, '[redacted]').replace(/https?:\/\/[^\s)'"<>]+/gi, '[redacted URL]');
  console.error(message);
  process.exit(1);
});
