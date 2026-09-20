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
  let hiddenListingDelay = 0;
  let adminDocumentLoads = 0, adminCapabilities = { chooseFiles: false };
  let update = { currentVersion: 'v0.1.0-preview.8', latestVersion: 'v0.1.0-preview.9', phase: 'available', available: true, canInstall: true, message: 'A new version is available.', releaseURL: 'https://github.com/arizzi74/Smart-Stage/releases/tag/v0.1.0-preview.9', checkedAt: new Date().toISOString() };
  const labels = ['Opening music', 'Welcome video with a deliberately long label that must wrap clearly', "Café's interlude", '<img src=x onerror="window.__xss=true">'];
  const stageDefaults = { backgroundCueId: '', backgroundAudio: false, fadeEnabled: false, fadeSeconds: 1, toggleAudio: false };
  const state = { stage: { ...stageDefaults }, backgroundCueId: '', imageCueId: '', instanceId: 'browser-fixture', revision: 1, playlistRevision: 1, state: 'stopped', activeCueId: '', activePosition: 0, elapsed: 0, duration: 0, lastError: '', outputs: { audioId: 'default', displayId: 'screen', allowPrimary: true }, resolvedAudioId: '', stageEnabled: false, outputFault: false, generation: 1, stopEpoch: 1, validationJob: { running: false, completed: 4, total: 4 }, cues: labels.map((label, i) => ({ id: `cue-${i}`, label, position: i + 1, kind: i % 2 ? 'video' : 'audio', duration: 3, validation: 'ready' })) };
  const config = { stage: { ...stageDefaults }, schema: 1, playlistRevision: 1, outputs: state.outputs, cues: state.cues.map(c => ({ id: c.id, label: c.label, path: `/Host/Show/${c.id}.mp4`, cache: { status: 'ready', media: { kind: c.kind, duration: 3 } } })) };
  const broadcast = () => { for (const c of clients) c.write(`event: state\ndata: ${JSON.stringify(state)}\n\n`); };
  const createServer = listenerRole => http.createServer(async (req, res) => {
    const url = new URL(req.url, 'http://localhost');
    const reply = (value, status = 200) => { res.writeHead(status, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(value)); };
    if (!url.pathname.startsWith('/api/')) {
      if (listenerRole === 'admin' && url.pathname === '/admin') adminDocumentLoads++;
      const name = url.pathname.startsWith('/assets/') ? path.basename(url.pathname) : 'index.html';
      const type = name.endsWith('.js') ? 'text/javascript' : name.endsWith('.css') ? 'text/css' : 'text/html';
      res.writeHead(200, { 'Content-Type': type, 'Content-Security-Policy': "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'" }); res.end(fs.readFileSync(path.join(assets, name))); return;
    }
    let raw = ''; for await (const chunk of req) raw += chunk;
    const body = raw ? JSON.parse(raw) : {};
    requests.push({ listenerRole, path: url.pathname, query: url.search, body, origin: req.headers.origin, csrf: req.headers['x-csrf-token'] });
    if (restarting) { reply({ error: { message: 'Host restarting' } }, 503); return; }
    const cookieName = `smartstage_${listenerRole}_session`;
    const sessionID = (req.headers.cookie || '').split(';').map(part => part.trim()).find(part => part.startsWith(cookieName + '='))?.slice(cookieName.length + 1);
    const role = sessions.get(sessionID);
    const pair = () => {
      const id = `fixture-${++sessionCounter}`; sessions.set(id, listenerRole);
      res.setHeader('Set-Cookie', `${cookieName}=${id}; Path=/; HttpOnly; SameSite=Strict`);
      reply({ role: listenerRole, csrfToken: 'test-csrf', ...(listenerRole === 'admin' ? { capabilities: adminCapabilities } : {}) });
    };
    if (url.pathname === '/api/local-session' && listenerRole === 'admin') { pair(); return; }
    if (url.pathname === '/api/pair' && listenerRole === 'command') {
      if (body.key !== token) { reply({ error: { message: 'Invalid connection code' } }, 401); return; }
      pair(); return;
    }
    if (role !== listenerRole) { reply({ error: { message: 'Connect this browser first' } }, 401); return; }
    if (url.pathname === '/api/logout') { sessions.delete(sessionID); res.setHeader('Set-Cookie', `${cookieName}=; Path=/; Max-Age=0`); reply({}); return; }
    if (url.pathname === '/api/state') { stateGets++; reply({ role, csrfToken: 'test-csrf', state, ...(role === 'admin' ? { capabilities: adminCapabilities } : {}) }); return; }
    if (['/api/admin-presence', '/api/quit', '/api/choose-files'].includes(url.pathname)) {
      if (role !== 'admin') { reply({ error: { message: 'Admin only' } }, 403); return; }
      assert.equal(req.headers['x-csrf-token'], 'test-csrf');
      assert.equal(req.headers.origin, `http://127.0.0.1:${req.socket.localPort}`);
      assert.deepEqual(body, {});
      if (url.pathname === '/api/admin-presence') { reply({ present: true, capabilities: adminCapabilities }); return; }
      if (url.pathname === '/api/choose-files') { assert(adminCapabilities.chooseFiles); reply({ choosing: true }, 202); return; }
      reply({ quitting: true }, 202);
      setTimeout(() => { restarting = true; for (const client of clients) client.end(); }, 25);
      return;
    }
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
    if (['/api/remote-control', '/api/remote-control/qr', '/api/playlist', '/api/stage-settings', '/api/devices', '/api/files'].includes(url.pathname) && role !== 'admin') { reply({ error: { message: 'Admin only' } }, 403); return; }
    if (url.pathname === '/api/remote-control') { remoteGets++; reply({ links, token }); return; }
    if (url.pathname === '/api/remote-control/qr') { res.writeHead(200, { 'Content-Type': 'image/png', 'Cache-Control': 'no-store' }); res.end(imageFixture); return; }
    if (url.pathname === '/api/playlist') {
      if (req.method === 'PUT') {
        assert.equal(req.headers['x-csrf-token'], 'test-csrf'); assert.equal(body.expectedRevision, config.playlistRevision);
        config.cues = body.cues.map((cue, index) => {
          const previous = config.cues.find(item => item.id === cue.id);
          return { ...cue, id: cue.id || `added-${config.playlistRevision}-${index}`, label: cue.label || path.basename(cue.path), color: cue.color ?? previous?.color ?? '', hidden: cue.hidden ?? previous?.hidden ?? false, background: cue.background ?? previous?.background ?? false, cache: previous?.cache || { status: 'ready', media: { kind: 'audio', duration: 3 } } };
        });
        config.playlistRevision++; state.playlistRevision = config.playlistRevision;
        state.cues = config.cues.map((cue, index) => ({ id: cue.id, label: cue.label, color: cue.color, hidden: cue.hidden, background: cue.background, position: index + 1, kind: cue.cache.media.kind, duration: 3, validation: 'ready' }));
        state.revision++; broadcast();
      }
      reply(config); return;
    }
    if (url.pathname === '/api/stage-settings') {
      assert.equal(req.method, 'PUT'); assert.equal(req.headers['x-csrf-token'], 'test-csrf');
      if (body.expectedRevision !== config.playlistRevision) { reply({ error: { message: 'The playlist changed; reload before editing' } }, 409); return; }
      assert.equal(Boolean(state.updatePending), false);
      assert(body.settings.fadeSeconds >= .1 && body.settings.fadeSeconds <= 30);
      config.stage = { ...body.settings }; state.stage = { ...config.stage }; state.backgroundCueId = config.stage.backgroundCueId;
      config.playlistRevision++; state.playlistRevision = config.playlistRevision; state.revision++; broadcast(); reply(config); return;
    }
    if (url.pathname === '/api/devices') { reply({ audio: [{ id: 'speaker', name: 'USB Audio', default: true }], displays: [{ id: 'screen', name: 'Stage display', width: 1920, height: 1080, primary: true, mirrored: false }] }); return; }
    if (url.pathname === '/api/files') {
      const folder = url.searchParams.get('path') || '/Host/Show';
      const entry = (name, directory = false) => ({ name, path: `${folder}/${name}`, directory, size: 1000000, modified: Date.now() * 1e6 });
      const entries = folder === '/Host/Show' ? [{ ...entry('Audio rehearsals with a long folder name that must stay on one line', true), path: '/Host/Show/Audio' }, entry("Café's opening.mp4")] : [entry('Interlude.wav')];
      const breadcrumbs = [{ name: 'Show', path: '/Host/Show' }];
      if (folder !== '/Host/Show') breadcrumbs.push({ name: 'Audio', path: folder });
      if (url.searchParams.get('showHidden') === 'true') {
        entries.push(entry('.DS_Store'), entry('.backstage', true));
        if (hiddenListingDelay) await new Promise(resolve => setTimeout(resolve, hiddenListingDelay));
      }
      reply({ path: folder, parent: folder === '/Host/Show' ? '' : '/Host/Show', roots: ['/Host/Show'], breadcrumbs, entries, truncated: false }); return;
    }
    if (url.pathname === '/api/inspect') { reply({ status: 'ready', media: { kind: 'video', duration: 180 }, reason: '' }); return; }
    commands.push({ path: url.pathname, body });
    if (url.pathname === '/api/stage-output') {
      assert.equal(req.headers['x-csrf-token'], 'test-csrf');
      assert.equal(req.headers.origin, `http://127.0.0.1:${req.socket.localPort}`);
      assert.equal(typeof body.enabled, 'boolean');
      if (body.enabled) assert.equal(Boolean(state.updatePending), false);
      state.stageEnabled = body.enabled; state.revision++; broadcast();
    }
    if (url.pathname === '/api/emergency-stop') { assert.equal(req.headers['x-csrf-token'], 'test-csrf'); state.state = 'stopped'; state.activeCueId = ''; state.imageCueId = ''; state.stageEnabled = false; state.stopEpoch++; state.revision++; broadcast(); }
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
    assert.equal(await page.locator('#page-title').isVisible(), false, 'remote control omits the Admin title and eyebrow block');
    assert.equal(await page.locator('#quit-app').isVisible(), false, 'Quit is only offered in local Admin');
    assert.equal(await page.locator('.transport #logout').count(), 1, 'Disconnect belongs in the fixed transport');
    assert((await page.locator('#logout').boundingBox()).height <= 32, 'Disconnect remains a small secondary control');
    await page.locator('#remote-stage').click();
    await page.waitForFunction(() => document.getElementById('remote-stage').getAttribute('aria-pressed') === 'true');
    assert.deepEqual(requests.filter(r => r.path === '/api/stage-output').at(-1).body, { enabled: true });
    assert.equal(requests.filter(r => r.path === '/api/stage-output').at(-1).listenerRole, 'command', 'remote Stage uses the authenticated command listener');
    state.state = 'playing'; state.activeCueId = 'cue-0'; state.revision++; broadcast();
    await page.locator('.cue.active').waitFor();
    assert.equal(await page.locator('#remote-stage').isDisabled(), false, 'an enabled stage can be closed during playback');
    await page.locator('#remote-stage').click();
    await page.waitForFunction(() => document.getElementById('remote-stage').getAttribute('aria-pressed') === 'false');
    assert.deepEqual(requests.filter(r => r.path === '/api/stage-output').at(-1).body, { enabled: false });
    assert.equal(state.state, 'playing', 'closing the stage does not stop foreground music');
    assert.equal(state.activeCueId, 'cue-0');
    await page.locator('#remote-stage').click();
    await page.waitForFunction(() => document.getElementById('remote-stage').getAttribute('aria-pressed') === 'true');
    assert.equal(state.activeCueId, 'cue-0', 'stage can reopen while music continues');
    state.state = 'stopped'; state.activeCueId = ''; state.revision++; broadcast();
    state.stageEnabled = true; state.state = 'playing'; state.revision++; broadcast();
    await page.waitForFunction(() => document.getElementById('remote-stage').getAttribute('aria-pressed') === 'true');
    await page.keyboard.press('Escape');
    await page.waitForFunction(() => document.getElementById('remote-stage').getAttribute('aria-pressed') === 'false');
    assert.equal(state.state, 'stopped', 'Escape in remote control stops playback and closes the stage');
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
      for (const selector of ['#remote-stage', '#logout']) {
        const control = await page.locator(selector).boundingBox();
        assert(control && control.y >= 0 && control.y + control.height <= size.height, `${selector} must remain in the fixed top bar`);
      }
      assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `horizontal overflow at ${size.width}`);
      await page.evaluate(() => scrollTo(0, 0));
      const gap = await page.evaluate(() => document.getElementById('cue-grid').getBoundingClientRect().top - document.querySelector('.transport').getBoundingClientRect().bottom);
      assert(gap >= 12 && gap <= 20, `remote cues must sit directly below the header at ${size.width}px (gap ${gap})`);
    }
    await page.setViewportSize({ width: 390, height: 844 }); await page.evaluate(() => scrollTo(0, 0));
    await page.screenshot({ path: path.join(output, 'command-phone.png'), fullPage: true });
    const beforeGap = stateGets; state.revision += 5; broadcast(); await page.waitForTimeout(150);
    assert(stateGets > beforeGap, 'revision gap must fetch authoritative state');
    await context.setOffline(true); await page.waitForTimeout(200);
    await page.locator('#stop').tap();
    await page.waitForFunction(() => document.getElementById('notice').textContent.includes('unconfirmed'));
    assert.equal(await page.locator('#notice.error').isVisible(), true, 'critical remote errors remain visible');
    assert.equal(await page.locator('#notice').getAttribute('aria-live'), 'polite');
    assert(await page.locator('.cue').first().isDisabled(), 'disconnected cue activation must be disabled');
    const playCount = commands.filter(c => c.path === '/api/play').length;
    await context.setOffline(false); await page.waitForTimeout(1000);
    assert.equal(commands.filter(c => c.path === '/api/play').length, playCount, 'reconnect must not replay PLAY');
    await page.locator('#logout').click(); await page.locator('#pairing').waitFor();
    assert.equal(requests.filter(r => r.path === '/api/pair').length, beforeReloadPairs, 'logout cannot silently reuse a URL token');
    await page.locator('#pair-key').fill(token); await page.getByRole('button', { name: 'Connect', exact: true }).click();
    await page.locator('#connection.live').waitFor();
    assert.equal(await page.locator('#pair-key').inputValue(), '', 'manual code is cleared after submission');

    const admin = await browser.newPage({ viewport: { width: 1280, height: 900 }, hasTouch: false }); admin.on('pageerror', e => errors.push(e.message));
    await admin.setViewportSize({ width: 1280, height: 900 }); await admin.goto(adminBase + '/admin');
    await admin.locator('.playlist-row').first().waitFor(); await admin.locator('#remote-ready').waitFor();
    assert.equal(await admin.locator('#pairing').isVisible(), false, 'local Admin opens automatically');
    assert.equal(await admin.locator('#logout').isVisible(), false, 'local Admin has no remote pairing logout');
    assert.equal(requests.find(r => r.path === '/api/local-session').origin, adminBase, 'Admin session request includes same-origin Origin');
    assert.equal(await admin.locator('.playlist-row').count(), 4);
    assert.equal(await admin.locator('#playlist img').count(), 0);
    assert.equal(await admin.locator('#audio-output option').count(), 2);
    await untilPresence();
    async function untilPresence() { await admin.waitForFunction(() => !document.getElementById('quit-app').disabled); assert(requests.some(r => r.path === '/api/admin-presence' && r.listenerRole === 'admin'), 'Admin establishes authenticated presence'); }
    assert.equal(await admin.locator('#choose-files').isVisible(), false, 'the native picker stays hidden until the desktop host is ready');
    const documentsBeforeChooserReady = adminDocumentLoads;
    adminCapabilities = { chooseFiles: true };
    await admin.evaluate(() => sendAdminPresence(true));
    await admin.locator('#choose-files').waitFor({ state: 'visible' });
    assert.equal(adminDocumentLoads, documentsBeforeChooserReady, 'a presence response refreshes native picker readiness without reloading Admin');
    await admin.locator('#choose-files').click();
    await admin.waitForFunction(() => document.getElementById('file-drop-message').textContent.includes('Mac dialog'));
    assert.equal(requests.filter(r => r.path === '/api/choose-files').length, 1);
    adminCapabilities = { chooseFiles: false }; await admin.evaluate(() => refreshState());
    assert.equal(await admin.locator('#choose-files').isVisible(), false, 'unsupported hosts retain Host files without a nonworking native-picker button');
    adminCapabilities = { chooseFiles: true }; await admin.evaluate(() => refreshState());
    await admin.locator('#file-list .file-row').first().waitFor();
    assert.equal(await admin.locator('#show-hidden').isChecked(), false, 'host files hide dotfiles by default');
    assert.equal(requests.filter(r => r.path === '/api/files')[0].query.includes('showHidden'), false, 'default browsing uses the server hidden-file filter');
    assert.equal(await admin.locator('#file-list .file-row').count(), 2);
    assert.equal(await admin.locator('#file-list').getByText('.DS_Store', { exact: true }).count(), 0);
    assert(await admin.locator('#file-list .file-row').evaluateAll(rows => rows.every(row => row.getBoundingClientRect().height >= 32 && row.getBoundingClientRect().height <= 36)), 'desktop file rows stay compact and on one line');
    await admin.locator('#file-list label.file-name').click();
    assert.equal(await admin.locator('#add-files').textContent(), 'Add selected (1)', 'clicking a file name selects its checkbox');
    await admin.locator('#file-list').getByRole('button', { name: "Inspect Café's opening.mp4", exact: true }).click();
    await admin.waitForFunction(() => document.getElementById('file-message').textContent.includes('video · 3:00 · ready'));
    assert(await admin.locator('#file-list .file-row').evaluateAll(rows => rows.every(row => row.getBoundingClientRect().height <= 36)), 'inspection results do not expand desktop rows');
    await admin.locator('#show-hidden').check();
    await admin.locator('#file-list').getByText('.DS_Store', { exact: true }).waitFor();
    assert.equal(await admin.locator('#file-list .file-row').count(), 4);
    assert.equal(await admin.locator('#add-files').isDisabled(), true, 'refreshing the listing clears stale selections');
    await admin.locator('#file-list').getByRole('button', { name: 'Open folder Audio rehearsals with a long folder name that must stay on one line', exact: true }).click();
    await admin.waitForFunction(() => document.getElementById('host-path').value === '/Host/Show/Audio');
    assert.equal(await admin.locator('#file-list').getByText('.DS_Store', { exact: true }).count(), 1, 'Show hidden applies while navigating folders');
    await admin.locator('#breadcrumbs').getByRole('button', { name: '↑ Parent', exact: true }).click();
    await admin.waitForFunction(() => document.getElementById('host-path').value === '/Host/Show');
    await admin.locator('#file-list').getByRole('checkbox', { name: 'Select .DS_Store', exact: true }).check();
    await admin.locator('#show-hidden').uncheck();
    await admin.waitForFunction(() => document.querySelectorAll('#file-list .file-row').length === 2);
    assert.equal(await admin.locator('#add-files').isDisabled(), true, 'hidden selections are removed when dotfiles are hidden again');
    hiddenListingDelay = 250;
    const delayedHiddenResponse = admin.waitForResponse(response => response.url().includes('/api/files?') && response.url().includes('showHidden=true'));
    await admin.locator('#show-hidden').check();
    await admin.locator('#show-hidden').uncheck();
    await delayedHiddenResponse;
    await admin.waitForTimeout(100);
    hiddenListingDelay = 0;
    assert.equal(await admin.locator('#file-list .file-row').count(), 2, 'an older delayed Show hidden response cannot reveal files after hiding them');
    assert.equal(await admin.locator('#show-hidden').isChecked(), false);
    assert.equal(requests.some(r => r.listenerRole === 'command' && r.path === '/api/files'), false, 'remote control never requests host files');
    const playlistWritesBefore = requests.filter(r => r.path === '/api/playlist' && r.body.cues).length;
    await admin.evaluate(() => {
      const files = new DataTransfer(); files.items.add(new File(['example'], 'Finder opening.wav', { type: 'audio/wav' }));
      const drop = new DragEvent('drop', { dataTransfer: files, bubbles: true, cancelable: true });
      document.getElementById('playlist-drop').dispatchEvent(drop); window.__finderDropPrevented = drop.defaultPrevented;
    });
    assert.equal(await admin.evaluate(() => window.__finderDropPrevented), true, 'external file drops cannot navigate Admin away');
    assert.match(await admin.locator('#file-drop-message').textContent(), /Dock icon/);
    assert.equal(requests.filter(r => r.path === '/api/playlist' && r.body.cues).length, playlistWritesBefore, 'Finder drops are not uploaded or converted to invented local paths');
    await admin.evaluate(() => {
      const payload = new DataTransfer(); payload.setData('application/x-smartstage-host-files', '/untrusted/injected.mp4');
      document.getElementById('playlist-drop').dispatchEvent(new DragEvent('drop', { dataTransfer: payload, bubbles: true, cancelable: true }));
    });
    assert.equal(requests.filter(r => r.path === '/api/playlist' && r.body.cues).length, playlistWritesBefore, 'a custom drag payload from another page cannot inject a path');
    const initialCueCount = config.cues.length;
    const dragSource = admin.locator('#file-list label.file-name');
    await dragSource.scrollIntoViewIfNeeded();
    const sourceBounds = await dragSource.boundingBox();
    await admin.mouse.move(sourceBounds.x + 30, sourceBounds.y + sourceBounds.height / 2);
    await admin.mouse.down();
    await admin.mouse.move(sourceBounds.x + 50, sourceBounds.y + sourceBounds.height / 2, { steps: 5 });
    await admin.locator('#playlist-drop').scrollIntoViewIfNeeded();
    const dropBounds = await admin.locator('#playlist-drop').boundingBox();
    await admin.mouse.move(dropBounds.x + 50, dropBounds.y + dropBounds.height / 2, { steps: 10 });
    await admin.mouse.move(dropBounds.x + 51, dropBounds.y + dropBounds.height / 2);
    await admin.mouse.up();
    await admin.waitForFunction(expected => document.querySelectorAll('.playlist-row').length === expected, initialCueCount + 1);
    assert.equal(config.cues.at(-1).path, "/Host/Show/Café's opening.mp4", 'Host file drops keep the original server path');
    await admin.waitForFunction(() => document.getElementById('file-drop-message').textContent.includes('Files stay in place'));
    assert.match(await admin.locator('#file-drop-message').textContent(), /Files stay in place/);
    assert.equal(requests.some(r => r.path === '/api/playlist/import'), false, 'adding in-place files never sends an upload');
    const firstColor = admin.getByRole('textbox', { name: 'Label for cue 1', exact: true });
    await admin.locator('.cue-color-controls input[type=color]').first().evaluate(input => { input.value = '#ffff00'; input.dispatchEvent(new Event('change', { bubbles: true })); });
    await page.waitForFunction(() => getComputedStyle(document.querySelector('.cue')).backgroundColor === 'rgb(255, 255, 0)');
    assert.equal(config.cues[0].color, '#ffff00', 'Admin color changes are saved in the playlist');
    assert.equal(await page.locator('.cue').first().evaluate(node => getComputedStyle(node).color), 'rgb(0, 0, 0)', 'bright cue backgrounds use dark text under the real content security policy');
    await firstColor.fill('Opening music in yellow'); await firstColor.press('Tab');
    await admin.waitForFunction(() => document.getElementById('playlist-revision').textContent.includes('saved revision 4'));
    assert.equal(config.cues[0].color, '#ffff00', 'unrelated label saves preserve cue colors');
    state.cues[0].color = '#111111'; state.revision++; broadcast();
    await page.waitForFunction(() => getComputedStyle(document.querySelector('.cue')).backgroundColor === 'rgb(17, 17, 17)');
    assert.equal(await page.locator('.cue').first().evaluate(node => getComputedStyle(node).color), 'rgb(255, 255, 255)', 'dark cue backgrounds use light text after a state event');
    state.cues[1].color = '#ffff00'; state.revision++; broadcast();
    await page.waitForFunction(() => document.querySelector('.cue.active')?.classList.contains('custom-color'));
    assert.equal(await page.locator('.cue.active').evaluate(node => getComputedStyle(node).outlineStyle), 'solid', 'active colored cues retain a visible outline');
    await page.evaluate(() => scrollTo(0, 0));
    await page.screenshot({ path: path.join(output, 'command-colors-phone.png'), fullPage: true });
    await admin.getByRole('button', { name: 'Use default color for cue 1', exact: true }).click();
    await page.waitForFunction(() => !document.querySelector('.cue').classList.contains('custom-color'));
    assert.equal(config.cues[0].color, '', 'Default explicitly resets the saved color');
    await admin.waitForFunction(() => document.querySelector('button[aria-label="Use default color for cue 1"]').disabled);
    // UI checks use explicit authoritative fixture events; they do not emulate
    // native decoders, soundtrack arbitration, image rendering, or fades.
    assert.equal(await admin.locator('#fade-enabled').isChecked(), false);
    assert.equal(await admin.locator('#fade-seconds').inputValue(), '1');
    assert.equal(await admin.locator('#fade-seconds').isDisabled(), true);
    await admin.getByRole('checkbox', { name: 'Hide remote button for cue 1', exact: true }).check();
    await page.locator('[data-cue-id="cue-0"]').waitFor({ state: 'detached' });
    assert.equal(config.cues[0].hidden, true);
    assert.equal(await admin.locator('.playlist-row').count(), config.cues.length, 'hidden buttons remain editable in Admin');
    await admin.getByRole('textbox', { name: 'Label for cue 1', exact: true }).fill('Hidden opening music');
    await admin.getByRole('textbox', { name: 'Label for cue 1', exact: true }).press('Tab');
    await admin.waitForFunction(() => document.getElementById('notice').textContent === 'Playlist saved.');
    assert.equal(config.cues[0].hidden, true, 'unrelated cue edits preserve hidden state');
    await admin.getByRole('checkbox', { name: 'Hide remote button for cue 1', exact: true }).uncheck();
    await page.locator('[data-cue-id="cue-0"]').waitFor();
    await admin.getByRole('checkbox', { name: 'Use cue 2 as a background button', exact: true }).check();
    await page.waitForFunction(() => document.querySelector('[data-cue-id="cue-1"] .cue-meta').textContent.includes('background video'));
    await admin.locator('#background-cue').selectOption('cue-1');
    await admin.locator('#background-audio').check();
    await admin.locator('#fade-enabled').check();
    await admin.locator('#fade-seconds').fill('1.6');
    await admin.locator('#toggle-audio').check();
    await admin.locator('#save-stage-settings').click();
    await admin.waitForFunction(() => document.getElementById('stage-settings-message').textContent === 'Stage and sound settings saved.');
    assert.deepEqual(config.stage, { backgroundCueId: 'cue-1', backgroundAudio: true, fadeEnabled: true, fadeSeconds: 1.6, toggleAudio: true });
    assert.equal(config.cues[1].background, true, 'stage settings preserve background button flags');
    await admin.reload(); await admin.locator('.playlist-row').first().waitFor();
    assert.equal(await admin.locator('#background-cue').inputValue(), 'cue-1');
    assert.equal(await admin.locator('#fade-seconds').inputValue(), '1.6', 'saved settings survive an Admin reload');
    assert.equal(await admin.locator('#background-audio').isChecked(), true);
    assert.equal(await admin.locator('#toggle-audio').isChecked(), true);
    config.cues.push({ id: 'image-fixture', label: 'Sponsor image', path: '/Host/Show/sponsor.png', hidden: false, background: false, cache: { status: 'ready', media: { kind: 'image', duration: 0 } } });
    config.playlistRevision++; state.playlistRevision = config.playlistRevision;
    state.cues.push({ id: 'image-fixture', label: 'Sponsor image', position: config.cues.length, kind: 'image', duration: 0, validation: 'ready' });
    state.activeCueId = 'cue-0'; state.state = 'playing'; state.stageEnabled = true; state.revision++; broadcast();
    await admin.waitForFunction(() => document.querySelector('#background-cue option[value="image-fixture"]'));
    await page.locator('[data-cue-id="image-fixture"]').click();
    assert.equal(requests.filter(r => r.path === '/api/play').at(-1).body.cueId, 'image-fixture');
    state.imageCueId = 'image-fixture'; state.revision++; broadcast();
    await page.waitForFunction(() => document.querySelector('[data-cue-id="image-fixture"]').getAttribute('aria-pressed') === 'true');
    assert.equal(await page.locator('[data-cue-id="cue-0"]').getAttribute('aria-pressed'), 'true', 'the authoritative image selection keeps the music button selected');
    assert.match(await page.locator('[data-cue-id="cue-0"] .cue-meta').textContent(), /Press again to stop/);
    await page.locator('[data-cue-id="cue-0"]').click();
    assert.equal(requests.filter(r => r.path === '/api/play').at(-1).body.cueId, 'cue-0', 'a selected music button sends the same cue; host decides the configured toggle');
    state.activeCueId = ''; state.state = 'stopped'; state.revision++; broadcast();
    await page.waitForFunction(() => document.querySelector('[data-cue-id="cue-0"]').getAttribute('aria-pressed') === 'false');
    assert.equal(await page.locator('[data-cue-id="image-fixture"]').getAttribute('aria-pressed'), 'true', 'stopping music alone preserves the selected image');
    await page.locator('[data-cue-id="cue-1"]').click();
    assert.equal(requests.filter(r => r.path === '/api/play').at(-1).body.cueId, 'cue-1');
    state.imageCueId = ''; state.backgroundCueId = 'cue-1'; state.revision++; broadcast();
    await page.waitForFunction(() => document.querySelector('[data-cue-id="cue-1"] .cue-meta').textContent.includes('Background selected'));
    await admin.locator('#background-cue').selectOption('image-fixture');
    await admin.locator('#save-stage-settings').click();
    await admin.waitForFunction(() => document.getElementById('stage-settings-message').textContent === 'Stage and sound settings saved.');
    assert.equal(config.stage.backgroundCueId, 'image-fixture', 'images can also be configured as the default background');
    await admin.locator('#fade-seconds').fill('2.5');
    const staleRevision = config.playlistRevision;
    config.playlistRevision++; state.playlistRevision = config.playlistRevision; state.revision++; broadcast();
    await admin.locator('#save-stage-settings').click();
    await admin.waitForFunction(() => document.getElementById('stage-settings-message').textContent.includes('Settings were not saved'));
    assert.equal(requests.filter(r => r.path === '/api/stage-settings').at(-1).body.expectedRevision, staleRevision, 'an unsaved settings draft keeps its original revision across incoming state');
    assert.equal(config.stage.fadeSeconds, 1.6, 'stale settings cannot overwrite a newer saved show');
    await admin.locator('#reload-playlist').click();
    await admin.waitForFunction(() => document.getElementById('fade-seconds').value === '1.6');
    state.activeCueId = 'cue-0'; state.state = 'playing'; state.revision++; broadcast();
    await admin.waitForFunction(() => !document.getElementById('enable-stage').disabled);
    await admin.locator('#disable-stage').click();
    await admin.waitForFunction(() => document.getElementById('stage-state').textContent.startsWith('Stage output disabled'));
    assert.equal(state.activeCueId, 'cue-0', 'Admin Stage off leaves foreground music selected');
    await admin.locator('#enable-stage').click();
    await admin.waitForFunction(() => document.getElementById('stage-state').textContent.startsWith('Stage output enabled'));
    assert.equal(state.activeCueId, 'cue-0', 'Admin Stage on works during foreground music');
    await admin.locator('#stage-section').screenshot({ path: path.join(output, 'admin-stage-sound.png') });
    for (const width of [390, 768, 1280]) {
      await admin.setViewportSize({ width, height: 900 });
      assert(await admin.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `stage controls overflow at ${width}`);
    }
    await admin.setViewportSize({ width: 1280, height: 900 });
    const stageCommandsBeforeEscape = requests.filter(r => r.path === '/api/emergency-stop').length;
    await admin.keyboard.press('Escape');
    await admin.waitForFunction(() => document.getElementById('play-state').textContent === 'stopped');
    assert.equal(requests.filter(r => r.path === '/api/emergency-stop').length, stageCommandsBeforeEscape + 1, 'Escape sends one atomic emergency stop from Admin');
    assert.equal(typeof requests.filter(r => r.path === '/api/emergency-stop').at(-1).body.requestId, 'string');
    state.state = 'playing'; state.activeCueId = 'cue-1'; state.revision++; broadcast();
    await page.locator('.cue.active').waitFor();
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
    await admin.locator('#files-section').screenshot({ path: path.join(output, 'host-files-compact.png') });
    await admin.evaluate(() => scrollTo(0, 0));
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
    assert.equal(await admin.locator('.cue-color-controls input[type=color]').first().isDisabled(), true, 'automatic updates reserve color edits too');
    assert.equal(await admin.locator('#save-stage-settings').isDisabled(), true, 'automatic updates reserve stage settings');
    assert.equal(await admin.getByRole('checkbox', { name: 'Hide remote button for cue 1', exact: true }).isDisabled(), true, 'automatic updates reserve visibility edits');
    assert.equal(await page.locator('.cue').first().isDisabled(), true, 'automatic startup check reserves remote playback');
    assert.equal(await admin.locator('#stop').isDisabled(), false, 'STOP remains available during automatic startup checking');
    state.stageEnabled = true; state.revision++; broadcast();
    await page.waitForFunction(() => document.getElementById('remote-stage').getAttribute('aria-pressed') === 'true');
    assert.equal(await page.locator('#remote-stage').isDisabled(), false, 'Stage off remains available while an update reserves playback');
    await page.locator('#remote-stage').click();
    await page.waitForFunction(() => document.getElementById('remote-stage').getAttribute('aria-pressed') === 'false');
    assert.equal(await page.locator('#remote-stage').isDisabled(), true, 'Stage on remains blocked during update preparation');
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
    const documentsBeforeUpdateRestart = adminDocumentLoads;
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
    assert.equal(adminDocumentLoads, documentsBeforeUpdateRestart + 1, 'a new host instance reloads Admin assets exactly once');

    // A synthetic user-agent exercises presentation only. Native pasteboard
    // handling is verified separately on macOS; this marker grants no access.
    const desktop = await browser.newPage({ viewport: { width: 1280, height: 900 }, userAgent: 'BrowserFixture SmartStageDesktop' });
    desktop.on('pageerror', e => errors.push(e.message));
    const sessionsBeforeDesktopRemote = requests.filter(r => r.path === '/api/local-session').length;
    await desktop.goto(commandBase + '/command'); await desktop.locator('#pairing').waitFor();
    assert.equal(await desktop.locator('#admin-view').isVisible(), false, 'a desktop user-agent does not expose Admin on the remote page');
    assert.equal(requests.filter(r => r.path === '/api/local-session').length, sessionsBeforeDesktopRemote, 'the desktop marker never bypasses the normal remote pairing flow');
    await desktop.goto(adminBase + '/admin'); await desktop.locator('.playlist-row').first().waitFor();
    assert.equal(await desktop.locator('#playlist-drop-title').textContent(), 'Drop Finder files here');
    assert.match(await desktop.locator('#playlist-drop-hint').textContent(), /Originals stay in place.*never uploaded or copied/);
    assert.equal(await admin.locator('#playlist-drop-title').textContent(), 'Drag files here from Host files below', 'external browsers retain their original-path guidance');
    const writesBeforeDesktopDrop = requests.filter(r => r.path === '/api/playlist' && r.body.cues).length;
    await desktop.evaluate(() => {
      const files = new DataTransfer(); files.items.add(new File(['example'], 'spoofed Finder.wav', { type: 'audio/wav' }));
      const drop = new DragEvent('drop', { dataTransfer: files, bubbles: true, cancelable: true });
      document.getElementById('playlist-drop').dispatchEvent(drop); window.__desktopDropPrevented = drop.defaultPrevented;
      const paths = new DataTransfer(); paths.setData('application/x-smartstage-host-files', '/untrusted/native-spoof.wav');
      document.getElementById('playlist-drop').dispatchEvent(new DragEvent('drop', { dataTransfer: paths, bubbles: true, cancelable: true }));
    });
    assert.equal(await desktop.evaluate(() => window.__desktopDropPrevented), true);
    assert.equal(requests.filter(r => r.path === '/api/playlist' && r.body.cues).length, writesBeforeDesktopDrop, 'a spoofed desktop marker cannot turn File objects or forged drag paths into privileged imports');
    assert.equal(requests.some(r => r.path === '/api/playlist/import'), false);
    const beforeDesktopAddition = config.cues.length;
    config.cues.push({ id: 'native-added-fixture', label: 'Native added fixture', path: '/Host/Show/native-added.wav', cache: { status: 'ready', media: { kind: 'audio', duration: 3 } } });
    config.playlistRevision++; state.playlistRevision = config.playlistRevision; state.revision++;
    state.cues.push({ id: 'native-added-fixture', label: 'Native added fixture', position: beforeDesktopAddition + 1, kind: 'audio', duration: 3, validation: 'ready' });
    broadcast();
    await desktop.waitForFunction(expected => document.querySelectorAll('.playlist-row').length === expected, beforeDesktopAddition + 1);
    assert.equal(await desktop.locator('#file-drop-message').textContent(), 'Added 1 file to the playlist. Originals stay in place.', 'native playlist changes are announced after the authoritative state event');
    await desktop.locator('#playlist-drop').scrollIntoViewIfNeeded();
    await desktop.screenshot({ path: path.join(output, 'admin-desktop-drop.png') });
    await desktop.close();

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
    assert.equal(requests.some(r => r.listenerRole === 'command' && ['/api/admin-presence', '/api/quit', '/api/choose-files'].includes(r.path)), false, 'remote pages never call local lifecycle endpoints');
    const beforeQuitDocuments = adminDocumentLoads;
    await admin.locator('#quit-app').click();
    await admin.locator('#app-closed').waitFor();
    assert.equal(requests.filter(r => r.path === '/api/quit').length, 1, 'one Admin click sends one authenticated quit request');
    assert.equal(await admin.locator('#stop').isDisabled(), true);
    assert.equal(await admin.locator('#admin-view').isVisible(), false);
    await admin.waitForTimeout(2500);
    assert.equal(await admin.locator('#notice.error').count(), 0, 'intentional shutdown stays quiet while the closed page probes for a relaunch');
    assert.equal(await admin.locator('#connection').textContent(), 'Smart Stage is closed');
    await admin.screenshot({ path: path.join(output, 'admin-closed.png'), fullPage: true });
    sessions.clear(); token = '11223344';
    state.instanceId = 'browser-fixture-after-quit'; state.revision = 1;
    links = [fixtureLink('Wi-Fi', commandBase, 0)]; restarting = false;
    await admin.waitForFunction(() => document.getElementById('remote-code').textContent === '11223344', { timeout: 10000 });
    await admin.locator('#connection.live').waitFor();
    assert.equal(await admin.locator('#app-closed').isVisible(), false);
    assert.equal(await admin.locator('#quit-app').isDisabled(), false);
    assert.equal(await admin.locator('#stop').isDisabled(), false);
    assert.equal(adminDocumentLoads, beforeQuitDocuments + 1, 'relaunch refreshes the existing Admin tab once rather than opening a new page');
    assert.equal(requests.filter(r => r.path === '/api/quit').length, 1, 'reconnection never repeats Quit');
    assert.deepEqual(errors, []);
    fs.writeFileSync(path.join(output, 'result.json'), JSON.stringify({ passed: true, browser: await browser.version(), viewportWidths: [320, 390, 768, 844, 1280], remoteCuesDirectlyBelowHeader: true, remoteErrorsRemainVisible: true, adminQuitClosedState: true, adminAuthenticatedPresence: true, existingAdminTabReloadsOnceAfterRelaunch: true, nativeFilePickerCapabilityAndRequest: true, nativeFilePickerExecutionVerified: false, desktopFinderDropGuidance: true, desktopMarkerGrantsNoFileAccess: true, desktopPlaylistAdditionFeedback: true, nativeWindowExecutionVerified: false, hostFilesHiddenByDefault: true, hiddenFileToggleAndNavigation: true, hiddenFileResponseRace: true, compactDesktopFileRows: true, hostFileDragUsesOriginalPaths: true, externalFileDropGuidance: true, remoteStageOutputControl: true, stageIndependentOfMusic: true, imageCueRetainsSelectedMusic: true, backgroundButtonsAndSelection: true, hiddenRemoteButtonsEditableInAdmin: true, stageSettingsSaveReloadAndRevisionConflict: true, fadeAndMusicToggleSettings: true, emergencyEscapeCommand: true, nativeBackgroundAndFadeExecutionVerified: false, compactHeaderDisconnect: true, cueColorSaveResetAndStateUpdates: true, cueColorContrastUnderCSP: true, adminAutomaticSession: true, remoteFragmentPairing: true, cookieResume: true, manualReconnect: true, remoteLinkRefresh: true, clipboardHTTPFallback: true, updatesAdminOnly: true, updateCheckRetry: true, automaticUpdateStartupReservation: true, updateStageAndPlaybackGuard: true, updateMutationGuard: true, updateRestartSessionAndQRRefresh: true, updateExecutionVerified: false, physicalPlaybackVerified: false, qrContentVerified: false }, null, 2));
    console.log('Browser checks passed: compact remote cues/errors, authenticated Admin presence/Quit/relaunch reload, native file-picker capability/request, host-file selection/dragging, cue colors, stage controls, and update/authentication regressions.');
    await context.close();
  } finally { await browser.close(); for (const c of clients) c.end(); await Promise.all([adminServer, commandServer].map(server => new Promise(resolve => server.close(resolve)))); }
})().catch(e => { console.error(e); process.exit(1); });
