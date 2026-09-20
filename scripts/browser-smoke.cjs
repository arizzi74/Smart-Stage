// Browser-only checks against two explicitly synthetic HTTP listeners. Native
// playback and real QR decoding are verified by the separate native/Go checks.
const { chromium } = require('playwright');
const http = require('node:http');
const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');

(async () => {
  const assets = path.resolve(__dirname, '../internal/web/assets');
  const output = path.resolve(__dirname, '../dist/browser-checks'); fs.mkdirSync(output, { recursive: true });
  const clients = new Set(), commands = [], errors = [], requests = [], sessions = new Map();
  let token = '12345678';
  // Image-loading fixture only: this one-pixel PNG is deliberately not a QR code.
  const imageFixture = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII=', 'base64');
  let stateGets = 0, holdPlay = false, remoteGets = 0, links = [], sessionCounter = 0;
  let updateGets = 0, updateGetDelay = 0, failUpdateCheck = false, restarting = false;
  let update = { currentVersion: 'v0.1.0-preview.8', latestVersion: 'v0.1.0-preview.9', phase: 'available', available: true, canInstall: true, message: 'A new version is available.', releaseURL: 'https://github.com/arizzi74/Smart-Stage/releases/tag/v0.1.0-preview.9', checkedAt: new Date().toISOString() };
  const labels = ['Opening music', 'Welcome video with a deliberately long label that must wrap clearly', "Café's interlude", '<img src=x onerror="window.__xss=true">'];
  const state = { instanceId: 'browser-fixture', revision: 1, playlistRevision: 1, state: 'stopped', activeCueId: '', activePosition: 0, elapsed: 0, duration: 0, lastError: '', outputs: { audioId: 'default', displayId: 'screen', allowPrimary: true }, resolvedAudioId: '', stageEnabled: false, outputFault: false, generation: 1, stopEpoch: 1, validationJob: { running: false, completed: 4, total: 4 }, cues: labels.map((label, i) => ({ id: `cue-${i}`, label, position: i + 1, kind: i % 2 ? 'video' : 'audio', duration: 3, validation: 'ready' })) };
  const config = { schema: 1, playlistRevision: 1, outputs: state.outputs, cues: state.cues.map(c => ({ id: c.id, label: c.label, path: `/Host/Show/${c.id}.mp4`, cache: { status: 'ready', media: { kind: c.kind, duration: 3 } } })) };
  const broadcast = () => { for (const c of clients) c.write(`event: state\ndata: ${JSON.stringify(state)}\n\n`); };
  const createServer = listenerRole => http.createServer(async (req, res) => {
    const url = new URL(req.url, 'http://localhost');
    const reply = (value, status = 200) => { res.writeHead(status, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(value)); };
    if (!url.pathname.startsWith('/api/')) {
      const name = url.pathname.startsWith('/assets/') ? path.basename(url.pathname) : 'index.html';
      const type = name.endsWith('.js') ? 'text/javascript' : name.endsWith('.css') ? 'text/css' : 'text/html';
      res.writeHead(200, { 'Content-Type': type }); res.end(fs.readFileSync(path.join(assets, name))); return;
    }
    let raw = ''; for await (const chunk of req) raw += chunk;
    const body = raw ? JSON.parse(raw) : {};
    requests.push({ listenerRole, path: url.pathname, body, origin: req.headers.origin, csrf: req.headers['x-csrf-token'] });
    if (restarting) { reply({ error: { message: 'Host restarting' } }, 503); return; }
    const cookieName = `smartstage_${listenerRole}_session`;
    const sessionID = (req.headers.cookie || '').split(';').map(part => part.trim()).find(part => part.startsWith(cookieName + '='))?.slice(cookieName.length + 1);
    const role = sessions.get(sessionID);
    const pair = () => {
      const id = `fixture-${++sessionCounter}`; sessions.set(id, listenerRole);
      res.setHeader('Set-Cookie', `${cookieName}=${id}; Path=/; HttpOnly; SameSite=Strict`);
      reply({ role: listenerRole, csrfToken: 'test-csrf' });
    };
    if (url.pathname === '/api/local-session' && listenerRole === 'admin') { pair(); return; }
    if (url.pathname === '/api/pair' && listenerRole === 'command') {
      if (body.key !== token) { reply({ error: { message: 'Invalid connection code' } }, 401); return; }
      pair(); return;
    }
    if (role !== listenerRole) { reply({ error: { message: 'Connect this browser first' } }, 401); return; }
    if (url.pathname === '/api/logout') { sessions.delete(sessionID); res.setHeader('Set-Cookie', `${cookieName}=; Path=/; Max-Age=0`); reply({}); return; }
    if (url.pathname === '/api/state') { stateGets++; reply({ role, csrfToken: 'test-csrf', state }); return; }
    if (url.pathname === '/api/events') { res.writeHead(200, { 'Content-Type': 'text/event-stream' }); clients.add(res); res.on('close', () => clients.delete(res)); broadcast(); return; }
    if (url.pathname.startsWith('/api/update')) {
      if (role !== 'admin') { reply({ error: { message: 'Admin only' } }, 403); return; }
      if (url.pathname === '/api/update') {
        updateGets++;
        if (updateGetDelay) await new Promise(resolve => setTimeout(resolve, updateGetDelay));
        reply(update); return;
      }
      assert.equal(req.headers['x-csrf-token'], 'test-csrf', 'update mutation requires the active CSRF token');
      if (url.pathname === '/api/update/check') {
        update = { ...update, phase: 'checking', message: 'Checking for updates…' };
        reply(update, 202);
        setTimeout(() => { update = { ...update, phase: failUpdateCheck ? 'error' : 'available', available: !failUpdateCheck, message: failUpdateCheck ? 'GitHub is unavailable. Try again later.' : 'A new version is available.' }; }, 100);
        return;
      }
      if (url.pathname === '/api/update/install') {
        assert.equal(state.state, 'stopped'); assert.equal(state.stageEnabled, false);
        state.updatePending = true; state.revision++; broadcast();
        update = { ...update, phase: 'downloading', message: 'Downloading and verifying the update…' };
        reply(update, 202); return;
      }
    }
    if (['/api/remote-control', '/api/remote-control/qr', '/api/playlist', '/api/devices', '/api/files'].includes(url.pathname) && role !== 'admin') { reply({ error: { message: 'Admin only' } }, 403); return; }
    if (url.pathname === '/api/remote-control') { remoteGets++; reply({ links, token }); return; }
    if (url.pathname === '/api/remote-control/qr') { res.writeHead(200, { 'Content-Type': 'image/png', 'Cache-Control': 'no-store' }); res.end(imageFixture); return; }
    if (url.pathname === '/api/playlist') { reply(config); return; }
    if (url.pathname === '/api/devices') { reply({ audio: [{ id: 'speaker', name: 'USB Audio', default: true }], displays: [{ id: 'screen', name: 'Stage display', width: 1920, height: 1080, primary: true, mirrored: false }] }); return; }
    if (url.pathname === '/api/files') { reply({ path: '/Host/Show', roots: ['/Host/Show'], breadcrumbs: [{ name: 'Show', path: '/Host/Show' }], entries: [{ name: "Café's opening.mp4", path: "/Host/Show/Café's opening.mp4", directory: false, size: 1000000, modified: Date.now() * 1e6 }], truncated: false }); return; }
    commands.push({ path: url.pathname, body });
    if (url.pathname === '/api/play' && holdPlay) { await new Promise(resolve => setTimeout(resolve, 600)); }
    if (url.pathname === '/api/stop') { state.revision++; state.stopEpoch++; state.activeCueId = ''; state.state = 'stopped'; broadcast(); }
    reply({ accepted: true }, 202);
  });
  const adminServer = createServer('admin'), commandServer = createServer('command');
  await Promise.all([adminServer, commandServer].map(server => new Promise(resolve => server.listen(0, '127.0.0.1', resolve))));
  const adminBase = `http://127.0.0.1:${adminServer.address().port}`, commandBase = `http://127.0.0.1:${commandServer.address().port}`;
  const fixtureLink = (label, base, index) => ({ label, url: `${base}/command#token=${token}`, qrURL: `/api/remote-control/qr?index=${index}` });
  links = [fixtureLink('Wi-Fi', commandBase, 0), fixtureLink('Ethernet <img src=x onerror="window.__xss=true">', 'http://192.0.2.15:8788', 1)];
  const browser = await chromium.launch({ headless: true });
  try {
    const context = await browser.newContext({ viewport: { width: 390, height: 844 }, hasTouch: true });
    const page = await context.newPage(); page.on('pageerror', e => errors.push(e.message));
    await page.addInitScript(() => {
      const nativeFetch = window.fetch;
      window.fetch = (...args) => { if (args[0] === '/api/pair') window.__pairingHash = location.hash; return nativeFetch(...args); };
    });
    await page.goto(commandBase + '/command#token=' + token);
    await page.locator('#connection.live').waitFor();
    assert.equal(await page.evaluate(() => location.hash), '', 'shared token must be removed from the address bar');
    assert.equal(await page.evaluate(() => window.__pairingHash), '', 'token fragment must be consumed before the pairing request');
    assert.equal(await page.evaluate(() => localStorage.length + sessionStorage.length), 0, 'token must not be retained in browser storage');
    assert.equal(await page.locator('#pairing').isVisible(), false, 'valid shared link connects without manual pairing');
    assert.equal(await page.locator('a[href="/admin"]').count(), 0, 'remote UI must not offer Admin');
    assert.equal(await page.locator('.cue').count(), 4);
    assert.equal(await page.evaluate(() => window.__xss), undefined);
    assert.equal(await page.locator('#cue-grid img').count(), 0);
    const beforeReloadPairs = requests.filter(r => r.path === '/api/pair').length;
    await page.reload(); await page.locator('#connection.live').waitFor();
    assert.equal(requests.filter(r => r.path === '/api/pair').length, beforeReloadPairs, 'valid cookie resumes without the numeric token');
    holdPlay = true;
    await page.locator('.cue').nth(0).tap();
    await page.locator('#stop').tap();
    await page.waitForTimeout(750);
    assert.equal(commands.filter(c => c.path === '/api/play').length, 1, 'one tap must send one PLAY');
    assert.equal(commands.filter(c => c.path === '/api/stop').length, 1, 'STOP must remain active during a pending PLAY');
    assert.equal(await page.locator('.cue.active').count(), 0, 'HTTP acceptance must not impersonate native playing');
    state.state = 'playing'; state.activeCueId = 'cue-1'; state.revision++; broadcast();
    await page.locator('.cue.active').waitFor();
    assert.equal(await page.locator('.cue.active .cue-title').textContent(), labels[1]);
    for (const size of [{ width: 320, height: 568 }, { width: 390, height: 844 }, { width: 844, height: 390 }, { width: 768, height: 1024 }, { width: 1280, height: 800 }]) {
      await page.setViewportSize(size); await page.evaluate(() => scrollTo(0, document.body.scrollHeight));
      const bounds = await page.locator('#stop').boundingBox();
      assert(bounds && bounds.y >= 0 && bounds.y + bounds.height <= size.height, 'STOP must remain in the viewport');
      assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `horizontal overflow at ${size.width}`);
    }
    await page.setViewportSize({ width: 390, height: 844 }); await page.evaluate(() => scrollTo(0, 0));
    await page.screenshot({ path: path.join(output, 'command-phone.png'), fullPage: true });
    const beforeGap = stateGets; state.revision += 5; broadcast(); await page.waitForTimeout(150);
    assert(stateGets > beforeGap, 'revision gap must fetch authoritative state');
    await context.setOffline(true); await page.waitForTimeout(200);
    await page.locator('#stop').tap();
    await page.waitForFunction(() => document.getElementById('notice').textContent.includes('unconfirmed'));
    assert(await page.locator('.cue').first().isDisabled(), 'disconnected cue activation must be disabled');
    const playCount = commands.filter(c => c.path === '/api/play').length;
    await context.setOffline(false); await page.waitForTimeout(1000);
    assert.equal(commands.filter(c => c.path === '/api/play').length, playCount, 'reconnect must not replay PLAY');
    await page.locator('#logout').click(); await page.locator('#pairing').waitFor();
    assert.equal(requests.filter(r => r.path === '/api/pair').length, beforeReloadPairs, 'logout cannot silently reuse a URL token');
    await page.locator('#pair-key').fill(token); await page.getByRole('button', { name: 'Connect', exact: true }).click();
    await page.locator('#connection.live').waitFor();
    assert.equal(await page.locator('#pair-key').inputValue(), '', 'manual code is cleared after submission');

    const admin = await context.newPage(); admin.on('pageerror', e => errors.push(e.message));
    await admin.setViewportSize({ width: 1280, height: 900 }); await admin.goto(adminBase + '/admin');
    await admin.locator('.playlist-row').first().waitFor(); await admin.locator('#remote-ready').waitFor();
    assert.equal(await admin.locator('#pairing').isVisible(), false, 'local Admin opens automatically');
    assert.equal(await admin.locator('#logout').isVisible(), false, 'local Admin has no remote pairing logout');
    assert.equal(requests.find(r => r.path === '/api/local-session').origin, adminBase, 'Admin session request includes same-origin Origin');
    assert.equal(await admin.locator('.playlist-row').count(), 4);
    assert.equal(await admin.locator('#playlist img').count(), 0);
    assert.equal(await admin.locator('#audio-output option').count(), 2);
    assert.equal(await admin.locator('#remote-url').textContent(), links[0].url);
    assert.equal(await admin.locator('#open-remote-url').getAttribute('href'), links[0].url);
    assert.equal(await admin.locator('#remote-code').textContent(), token);
    await admin.waitForFunction(() => document.getElementById('remote-qr').naturalWidth > 0);
    await admin.locator('#remote-network').selectOption(links[1].url);
    const selectedLink = links[1].url;
    links = [fixtureLink('Ethernet', 'http://192.0.2.15:8788', 0), fixtureLink('Wi-Fi', commandBase, 1)];
    const beforeRefresh = remoteGets;
    await admin.waitForFunction(expected => document.getElementById('remote-qr').getAttribute('src') === expected, links[0].qrURL, { timeout: 15000 });
    assert(remoteGets > beforeRefresh, 'Admin periodically refreshes LAN links');
    assert.equal(await admin.locator('#remote-network').inputValue(), selectedLink, 'network choice survives address reordering');
    await admin.evaluate(() => {
      Object.defineProperty(navigator, 'clipboard', { value: undefined, configurable: true });
      document.execCommand = action => { window.__copiedLink = document.querySelector('.clipboard-copy')?.value; return action === 'copy'; };
    });
    await admin.locator('#copy-remote-url').click();
    assert.equal(await admin.evaluate(() => window.__copiedLink), selectedLink, 'HTTP clipboard fallback copies the complete token link');
    assert.equal(await admin.locator('.clipboard-copy').count(), 0, 'temporary clipboard field is removed');
    links = []; await admin.evaluate(() => loadRemoteControl());
    assert.equal(await admin.locator('#remote-ready').isVisible(), false, 'removed LAN addresses hide stale links and QR');
    assert.match(await admin.locator('#remote-unavailable').textContent(), /No network address/);
    links = [fixtureLink('Wi-Fi', commandBase, 0)]; await admin.evaluate(() => loadRemoteControl());
    assert.equal(await admin.locator('#remote-ready').isVisible(), true, 'remote panel recovers when a network returns');
    assert.equal(await admin.locator('#remote-network-choice').isVisible(), false, 'one available address needs no selector');
    await admin.screenshot({ path: path.join(output, 'admin-desktop.png'), fullPage: true });
    for (const width of [320, 390, 768, 1280]) {
      await admin.setViewportSize({ width, height: 900 });
      assert(await admin.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `admin layout overflows at ${width}`);
    }
    assert.equal(await page.locator('#connection.live').isVisible(), true, 'opening Admin does not replace the remote session');
    assert.equal(requests.some(r => r.listenerRole === 'command' && r.path.startsWith('/api/remote-control')), false, 'remote UI never requests the private link or QR');

    await admin.locator('#update-current').filter({ hasText: 'v0.1.0-preview.8' }).waitFor();
    assert.equal(await page.locator('#updates-section').isVisible(), false, 'updates are local Admin only');
    assert.equal(requests.some(r => r.listenerRole === 'command' && r.path.startsWith('/api/update')), false, 'remote UI never requests update status');
    assert.equal(await admin.locator('#install-update').isDisabled(), true, 'update installation is unavailable during playback');
    assert.match(await admin.locator('#update-requirements').textContent(), /Stop playback and disable stage/);
    assert.equal(requests.some(r => r.path === '/api/update/install'), false, 'the browser must not trigger automatic installation during a session');
    assert.match(await admin.locator('#updates-section').textContent(), /Updates install automatically when Smart Stage starts/);
    state.state = 'stopped'; state.activeCueId = ''; state.updatePending = true; state.revision++; broadcast();
    update = { ...update, phase: 'checking', message: 'Checking for updates before starting…' };
    await admin.evaluate(() => loadUpdateStatus());
    await admin.waitForFunction(() => document.getElementById('update-requirements').textContent.includes('before starting'));
    assert.equal(await admin.locator('.playlist-row input').first().isDisabled(), true, 'automatic startup check reserves show editing');
    assert.equal(await page.locator('.cue').first().isDisabled(), true, 'automatic startup check reserves remote playback');
    assert.equal(await admin.locator('#stop').isDisabled(), false, 'STOP remains available during automatic startup checking');
    state.updatePending = false;
    update = { ...update, phase: 'available', message: 'A new version is available.' };
    await admin.evaluate(() => loadUpdateStatus());
    state.state = 'stopped'; state.activeCueId = ''; state.stageEnabled = true; state.revision++; broadcast();
    await admin.waitForFunction(() => document.getElementById('stage-state').textContent.startsWith('Stage output enabled'));
    assert.equal(await admin.locator('#install-update').isDisabled(), true, 'a stopped but enabled black stage still blocks installation');
    state.stageEnabled = false; state.revision++; broadcast();
    await admin.waitForFunction(() => !document.getElementById('install-update').disabled);
    updateGetDelay = 150;
    const beforeUpdateGets = updateGets;
    await admin.evaluate(() => Promise.all([loadUpdateStatus(), loadUpdateStatus(), loadUpdateStatus()]));
    assert.equal(updateGets, beforeUpdateGets + 1, 'concurrent refresh attempts share one in-flight request');
    updateGetDelay = 0;
    update.releaseURL = 'https://github.com.evil.invalid/arizzi74/Smart-Stage/releases/tag/v1';
    update.latestVersion = '<img src=x onerror="window.__xss=true">';
    await admin.evaluate(() => loadUpdateStatus());
    assert.equal(await admin.locator('#update-release').isVisible(), false, 'untrusted release URLs are not linked');
    assert.equal(await admin.locator('#update-latest img').count(), 0, 'version labels are rendered as text');
    assert.equal(await admin.evaluate(() => window.__xss), undefined);
    update.lastUpdate = { version: 'v0.1.0-preview.8', status: 'updated', message: 'Firewall approval was cancelled. Allow Smart Stage in Firewall Options. <img src=x>' };
    await admin.evaluate(() => loadUpdateStatus());
    assert.equal(await admin.locator('#update-outcome').isVisible(), true, 'last update warnings remain visible alongside the current check status');
    assert.match(await admin.locator('#update-outcome').textContent(), /Firewall approval was cancelled/);
    assert.equal(await admin.locator('#update-outcome img').count(), 0, 'update outcome is rendered as text');
    update.releaseURL = 'https://github.com/arizzi74/Smart-Stage/releases/tag/v0.1.0-preview.9';
    update.latestVersion = 'v0.1.0-preview.9';
    failUpdateCheck = true;
    await admin.locator('#check-update').click();
    await admin.waitForFunction(() => document.getElementById('check-update').textContent === 'Checking…');
    assert.equal(await admin.locator('#check-update').isDisabled(), true);
    await admin.waitForFunction(() => document.getElementById('update-message').textContent.includes('GitHub is unavailable'), { timeout: 10000 });
    assert.equal(await admin.locator('#check-update').isDisabled(), false, 'failed checks can be retried');
    assert.equal(await admin.locator('#install-update').isDisabled(), true);
    failUpdateCheck = false;
    await admin.locator('#check-update').click();
    await admin.waitForFunction(() => !document.getElementById('install-update').disabled, { timeout: 10000 });
    assert.equal(await admin.locator('#update-release').getAttribute('href'), update.releaseURL);
    await admin.locator('#file-list input[type=checkbox]').check();
    await admin.locator('#install-update').click();
    await admin.waitForFunction(() => document.getElementById('install-update').textContent === 'Downloading…');
    assert.equal(requests.filter(r => r.path === '/api/update/install').length, 1, 'one click starts exactly one install');
    assert.equal(await admin.locator('.playlist-row input').first().isDisabled(), true, 'playlist editing is disabled during preparation');
    assert.equal(await admin.locator('.cue-tools button').first().isDisabled(), true, 'Admin PLAY is disabled during preparation');
    assert.equal(await page.locator('.cue').first().isDisabled(), true, 'remote PLAY is disabled by authoritative updatePending state');
    assert.equal(await admin.locator('#save-outputs').isDisabled(), true);
    assert.equal(await admin.locator('#add-files').isDisabled(), true);
    assert.equal(await admin.locator('#stop').isDisabled(), false, 'STOP remains available during update preparation');
    update = { ...update, phase: 'restarting', message: 'Restarting Smart Stage. Admin will reconnect automatically.' };
    await admin.evaluate(() => loadUpdateStatus());
    const beforeRestartSessions = requests.filter(r => r.path === '/api/local-session').length;
    const oldRemoteURL = await admin.locator('#remote-url').textContent();
    restarting = true;
    for (const client of clients) client.end();
    await admin.waitForFunction(() => document.getElementById('connection').textContent.includes('Restarting'));
    assert.equal(await admin.locator('#notice.error').count(), 0, 'expected restart is not presented as a connection failure');
    sessions.clear(); token = '87654321';
    state.instanceId = 'browser-fixture-after-update'; state.revision = 1; state.updatePending = false;
    update = { ...update, currentVersion: 'v0.1.0-preview.9', phase: 'idle', available: false, message: 'Smart Stage is up to date.' };
    links = [fixtureLink('Wi-Fi', commandBase, 0)]; restarting = false;
    await admin.waitForFunction(() => document.getElementById('remote-code').textContent === '87654321', { timeout: 15000 });
    await admin.locator('#connection.live').waitFor();
    assert(requests.filter(r => r.path === '/api/local-session').length > beforeRestartSessions, 'Admin renews its session after restart');
    assert.notEqual(await admin.locator('#remote-url').textContent(), oldRemoteURL, 'Admin replaces the obsolete phone link');
    assert.equal(await admin.locator('#remote-url').textContent(), links[0].url);
    await admin.waitForFunction(() => document.getElementById('update-current').textContent === 'v0.1.0-preview.9');
    assert.equal(await admin.locator('.playlist-row input').first().isDisabled(), false, 'edits become available after the new host starts');
    assert.equal(await admin.locator('#install-update').isDisabled(), true, 'installed version is no longer offered');
    assert.equal(requests.filter(r => r.path === '/api/update/install').length, 1, 'reconnection does not repeat installation');

    const invalid = await browser.newPage(); invalid.on('pageerror', e => errors.push(e.message));
    await invalid.goto(commandBase + '/command#token=00000000'); await invalid.locator('#pairing').waitFor();
    await invalid.waitForFunction(() => document.getElementById('pair-error').textContent.includes('current link'));
    assert.equal(await invalid.evaluate(() => location.hash), '', 'invalid token is also removed');
    await invalid.goto(commandBase + '/command#token=abc'); await invalid.locator('#pairing').waitFor();
    await invalid.waitForFunction(() => document.getElementById('pair-error').textContent.includes('This link has an invalid connection code'));
    await invalid.goto(commandBase + '/command#token=' + token); await invalid.locator('#connection.live').waitFor();
    assert.equal(await invalid.locator('#pairing').isVisible(), false, 'a current shared link reconnects the same tab after an invalid link');
    assert.equal(await invalid.evaluate(() => location.hash), '', 'same-document token navigation is also consumed');
    await invalid.context().clearCookies();
    await invalid.goto(commandBase + '/command'); await invalid.locator('#pairing').waitFor();
    assert.match(await invalid.locator('#pair-form').textContent(), /Scan the QR code/);
    assert.deepEqual(errors, []);
    fs.writeFileSync(path.join(output, 'result.json'), JSON.stringify({ passed: true, browser: await browser.version(), viewportWidths: [320, 390, 768, 844, 1280], adminAutomaticSession: true, remoteFragmentPairing: true, cookieResume: true, manualReconnect: true, remoteLinkRefresh: true, clipboardHTTPFallback: true, updatesAdminOnly: true, updateCheckRetry: true, automaticUpdateStartupReservation: true, updateStageAndPlaybackGuard: true, updateMutationGuard: true, updateRestartSessionAndQRRefresh: true, updateExecutionVerified: false, physicalPlaybackVerified: false, qrContentVerified: false }, null, 2));
    console.log('Browser checks passed: automatic Admin, token links/session reconnect, QR panel/LAN refresh/clipboard fallback, cues/escaping/STOP/responsive layouts, and update status/retry/startup reservation/install guards/restart session and QR refresh.');
    await context.close();
  } finally { await browser.close(); for (const c of clients) c.end(); await Promise.all([adminServer, commandServer].map(server => new Promise(resolve => server.close(resolve)))); }
})().catch(e => { console.error(e); process.exit(1); });
