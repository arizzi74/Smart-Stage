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
  let language = {mode: 'system', effective: 'en', system: 'en'}, languageSaveFail = false;
  // Image-loading fixture only: this one-pixel PNG is deliberately not a QR code.
  const imageFixture = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII=', 'base64');
  let stateGets = 0, holdPlay = false, remoteGets = 0, links = [], sessionCounter = 0;
  let updateGets = 0, updateGetDelay = 0, failUpdateCheck = false, restarting = false;
  let nativeChooserImports = [], playlistFailSave = false;
  let nativePlaylistStatus = { id: 0, phase: 'idle' }, nativePlaylistResult = { phase: 'complete' }, nativePlaylistMismatch = false;
  let adminDocumentLoads = 0, adminCapabilities = { chooseFiles: false };
  let gateway = { mode: 'lan', url: '', hasToken: false, status: 'disabled', message: '', remoteURL: '' };
  const gatewayRemoteToken = 'a1'.repeat(32), gatewayRegistrationToken = 'b2'.repeat(32);
  const publicPrefix = '/smartstage/e/' + '0123456789abcdef'.repeat(2), publicRequests = [];
  let gatewayDelay = 0, gatewayFailSave = false;
  let update = { currentVersion: 'v0.1.0-preview.8', latestVersion: 'v0.1.0-preview.9', phase: 'available', available: true, canInstall: true, message: 'A new version is available.', releaseURL: 'https://github.com/arizzi74/Smart-Stage/releases/tag/v0.1.0-preview.9', checkedAt: new Date().toISOString() };
  const labels = ['Opening music', 'Welcome video with a deliberately long label that must wrap clearly', "Café's interlude", '<img src=x onerror="window.__xss=true">'];
  const stageDefaults = { backgroundCueId: '', backgroundAudio: false, fadeEnabled: false, fadeSeconds: 1, toggleAudio: false };
  const state = { stage: { ...stageDefaults }, backgroundCueId: '', imageCueId: '', instanceId: 'browser-fixture', revision: 1, playlistRevision: 1, state: 'stopped', activeCueId: '', activePosition: 0, elapsed: 0, duration: 0, lastError: '', outputs: { audioId: 'default', displayId: 'screen', allowPrimary: true }, resolvedAudioId: '', stageEnabled: false, outputFault: false, generation: 1, stopEpoch: 1, validationJob: { running: false, completed: 4, total: 4 }, cues: labels.map((label, i) => ({ id: `cue-${i}`, label, position: i + 1, kind: i % 2 ? 'video' : 'audio', duration: 3, validation: 'ready' })) };
  const config = { stage: { ...stageDefaults }, schema: 1, playlistRevision: 1, outputs: state.outputs, cues: state.cues.map(c => ({ id: c.id, label: c.label, path: `/Host/Show/${c.id}.mp4`, cache: { status: 'ready', media: { kind: c.kind, duration: 3 } } })) };
  const broadcast = () => { for (const c of clients) c.write(`event: state\ndata: ${JSON.stringify(state)}\n\n`); };
  const exportedPlaylist = () => ({ format: 'smartstage-playlist', version: 1,
    cues: config.cues.map(({ id, label, path, color, hidden, background }) => ({ id, label, path, color: color || '', hidden: Boolean(hidden), background: Boolean(background) })), stage: { ...config.stage } });
  const importPlaylist = document => {
    const oldCues = config.cues, ids = new Map(document.cues.map((cue, index) => [cue.id, `loaded-${config.playlistRevision}-${index}`]));
    config.cues = document.cues.map(cue => ({ ...cue, id: ids.get(cue.id), cache: oldCues.find(old => old.path === cue.path)?.cache || { status: 'ready', media: { kind: 'audio', duration: 3 } } }));
    config.stage = { ...document.stage, backgroundCueId: ids.get(document.stage.backgroundCueId) || '' }; state.stage = { ...config.stage }; state.backgroundCueId = config.stage.backgroundCueId;
    config.playlistRevision++; state.playlistRevision = config.playlistRevision;
    state.cues = config.cues.map((cue, index) => ({ id: cue.id, label: cue.label, color: cue.color, hidden: cue.hidden, background: cue.background, position: index + 1, kind: cue.cache.media.kind, duration: 3, validation: 'ready' }));
    state.revision++; broadcast();
  };
  const createServer = (listenerRole, prefix = '') => http.createServer(async (req, res) => {
    const url = new URL(req.url, 'http://localhost');
    const reply = (value, status = 200) => { res.writeHead(status, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(value)); };
    if (prefix) {
      publicRequests.push(req.url);
      if (!url.pathname.startsWith(prefix + '/')) { reply({ error: { message: 'Outside public endpoint' } }, 404); return; }
      url.pathname = url.pathname.slice(prefix.length);
    }
    if (!url.pathname.startsWith('/api/')) {
      if (listenerRole === 'admin' && url.pathname === '/admin') adminDocumentLoads++;
      const name = url.pathname.startsWith('/assets/') ? path.basename(url.pathname) : 'index.html';
      const type = name.endsWith('.js') ? 'text/javascript' : name.endsWith('.css') ? 'text/css' : 'text/html';
      res.writeHead(200, { 'Content-Type': type, 'Content-Security-Policy': "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'" }); res.end(name === 'app.js' ? fs.readFileSync(path.join(assets, 'i18n.js'), 'utf8') + '\n' + fs.readFileSync(path.join(assets, name), 'utf8') : fs.readFileSync(path.join(assets, name))); return;
    }
    let raw = ''; for await (const chunk of req) raw += chunk;
    const body = raw ? JSON.parse(raw) : {};
    requests.push({ listenerRole, path: url.pathname, requestPath: req.url, query: url.search, body, origin: req.headers.origin, csrf: req.headers['x-csrf-token'] });
    if (restarting) { reply({ error: { message: 'Host restarting' } }, 503); return; }
    const cookieName = `smartstage_${listenerRole}_session`;
    const sessionID = (req.headers.cookie || '').split(';').map(part => part.trim()).find(part => part.startsWith(cookieName + '='))?.slice(cookieName.length + 1);
    const role = sessions.get(sessionID);
    const pair = () => {
      const id = `fixture-${++sessionCounter}`; sessions.set(id, listenerRole);
      res.setHeader('Set-Cookie', `${cookieName}=${id}; Path=${prefix || '/'}; HttpOnly; SameSite=Strict`);
      reply({ role: listenerRole, csrfToken: 'test-csrf', ...(listenerRole === 'admin' ? { capabilities: adminCapabilities, language } : {}) });
    };
    if (url.pathname === '/api/local-session' && listenerRole === 'admin') { pair(); return; }
    if (url.pathname === '/api/pair' && listenerRole === 'command') {
      if (body.key !== (prefix ? gatewayRemoteToken : token)) { reply({ error: { message: 'Invalid connection code' } }, 401); return; }
      pair(); return;
    }
    if (role !== listenerRole) { reply({ error: { message: 'Connect this browser first' } }, 401); return; }
    if (url.pathname === '/api/logout') { sessions.delete(sessionID); res.setHeader('Set-Cookie', `${cookieName}=; Path=${prefix || '/'}; Max-Age=0`); reply({}); return; }
    if (url.pathname === '/api/language') {
      if (role !== 'admin') { reply({error: {message: 'Admin only'}}, 403); return; }
      if (req.method === 'PUT') {
        assert.equal(req.headers['x-csrf-token'], 'test-csrf');
        assert.equal(req.headers.origin, `http://127.0.0.1:${req.socket.localPort}`);
        assert(['system', 'en', 'it'].includes(body.mode));
        if (languageSaveFail) { reply({error: {code: 'language_save_failed', message: 'Could not save the language preference'}}, 503); return; }
        language = {...language, mode: body.mode, effective: body.mode === 'system' ? language.system : body.mode};
      }
      reply(language); return;
    }
    if (url.pathname === '/api/state') { stateGets++; reply({ role, csrfToken: 'test-csrf', state, ...(role === 'admin' ? { capabilities: adminCapabilities, language } : {}) }); return; }
    if (['/api/admin-presence', '/api/quit', '/api/choose-files'].includes(url.pathname)) {
      if (role !== 'admin') { reply({ error: { message: 'Admin only' } }, 403); return; }
      assert.equal(req.headers['x-csrf-token'], 'test-csrf');
      assert.equal(req.headers.origin, `http://127.0.0.1:${req.socket.localPort}`);
      assert.deepEqual(body, {});
      if (url.pathname === '/api/admin-presence') { reply({ present: true, capabilities: adminCapabilities, language }); return; }
      if (url.pathname === '/api/choose-files') {
        assert(adminCapabilities.chooseFiles); reply({ choosing: true }, 202);
        const imported = nativeChooserImports; nativeChooserImports = [];
        if (imported.length) setTimeout(() => {
          for (const item of imported) config.cues.push({ id: `chooser-${config.cues.length}`, label: path.basename(item.path), path: item.path, cache: { status: 'ready', media: { kind: item.kind, duration: 3 } } });
          config.playlistRevision++; state.playlistRevision = config.playlistRevision;
          state.cues = config.cues.map((cue, index) => ({ id: cue.id, label: cue.label, color: cue.color, hidden: cue.hidden, background: cue.background, position: index + 1, kind: cue.cache.media.kind, duration: 3, validation: 'ready' }));
          state.revision++; broadcast();
        }, 50);
        return;
      }
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
    if (['/api/remote-control', '/api/remote-control/qr', '/api/playlist', '/api/playlist/export', '/api/playlist/import', '/api/playlist/file', '/api/stage-settings', '/api/devices', '/api/files', '/api/gateway', '/api/gateway/reconnect'].includes(url.pathname) && role !== 'admin') { reply({ error: { message: 'Admin only' } }, 403); return; }
    if (url.pathname === '/api/playlist/export') { assert.equal(req.method, 'GET'); reply(exportedPlaylist()); return; }
    if (url.pathname === '/api/playlist/import') {
      assert.equal(req.method, 'POST'); assert.equal(req.headers['x-csrf-token'], 'test-csrf');
      assert.equal(req.headers.origin, `http://127.0.0.1:${req.socket.localPort}`);
      if (body.expectedRevision !== config.playlistRevision) { reply({ error: { message: 'The playlist changed; reload it before loading a playlist file' } }, 409); return; }
      assert(['stopped', 'error'].includes(state.state)); assert.equal(state.stageEnabled, false); assert.equal(Boolean(state.updatePending), false);
      if (body.playlist?.format !== 'smartstage-playlist' || body.playlist.version !== 1 || !Array.isArray(body.playlist.cues) || !body.playlist.stage) {
        reply({ error: { message: 'This is not a supported Smart Stage playlist file' } }, 400); return;
      }
      importPlaylist(body.playlist); reply(config); return;
    }
    if (url.pathname === '/api/playlist/file') {
      assert.equal(adminCapabilities.playlistFiles, true);
      if (req.method === 'GET') { reply(nativePlaylistMismatch ? { ...nativePlaylistStatus, id: nativePlaylistStatus.id + 1 } : nativePlaylistStatus); return; }
      assert.equal(req.method, 'POST'); assert.equal(req.headers['x-csrf-token'], 'test-csrf'); assert.equal(body.expectedRevision, config.playlistRevision);
      assert.deepEqual(Object.keys(body).sort(), ['expectedRevision', 'operation']); assert(['save', 'load'].includes(body.operation));
      if (body.operation === 'load') { assert(['stopped', 'error'].includes(state.state)); assert.equal(state.stageEnabled, false); }
      nativePlaylistStatus = { id: nativePlaylistStatus.id + 1, operation: body.operation, phase: 'choosing' };
      const result = nativePlaylistResult; nativePlaylistResult = { phase: 'complete' };
      reply(nativePlaylistStatus, 202);
      setTimeout(() => {
        if (body.operation === 'load' && result.phase === 'complete') importPlaylist(result.playlist || exportedPlaylist());
        nativePlaylistStatus = { ...nativePlaylistStatus, phase: result.phase, message: result.message || '', filename: 'Show.smartstage.json' };
      }, 100);
      return;
    }
    if (url.pathname === '/api/gateway') {
      if (req.method === 'GET') { const snapshot = { ...gateway }; if (gatewayDelay) await new Promise(resolve => setTimeout(resolve, gatewayDelay)); reply(snapshot); return; }
      assert.equal(req.method, 'PUT'); assert.equal(req.headers['x-csrf-token'], 'test-csrf');
      assert.equal(req.headers.origin, `http://127.0.0.1:${req.socket.localPort}`);
      if (gatewayFailSave) { reply({ error: { message: 'Gateway settings could not be saved.' } }, 500); return; }
      assert(['lan', 'gateway'].includes(body.mode));
      if (body.mode === 'gateway' && !gateway.hasToken) assert.equal(body.token, gatewayRegistrationToken);
      if (body.token !== undefined) assert.match(body.token, /^[0-9a-f]{64}$/);
      gateway = { mode: body.mode, url: body.url, hasToken: Boolean(body.token || gateway.hasToken), status: body.mode === 'gateway' ? 'connecting' : 'disabled', message: '', remoteURL: '', restart: body.mode === 'lan' && gateway.mode !== 'lan' };
      reply(gateway); return;
    }
    if (url.pathname === '/api/gateway/reconnect') {
      assert.equal(req.method, 'POST'); assert.equal(req.headers['x-csrf-token'], 'test-csrf'); assert.deepEqual(body, {});
      gateway = { ...gateway, status: 'connecting', remoteURL: '' }; reply(gateway); return;
    }
    if (url.pathname === '/api/remote-control') {
      remoteGets++;
      reply(gateway.mode === 'gateway' ? { mode: 'gateway', token: gateway.status === 'connected' ? gatewayRemoteToken : '', links: gateway.status === 'connected' ? [{ label: 'Public gateway', url: gateway.remoteURL, qrURL: '/api/remote-control/qr?index=0' }] : [] } : { mode: 'lan', links, token }); return;
    }
    if (url.pathname === '/api/remote-control/qr') { res.writeHead(200, { 'Content-Type': 'image/png', 'Cache-Control': 'no-store' }); res.end(imageFixture); return; }
    if (url.pathname === '/api/playlist') {
      if (req.method === 'PUT') {
        if (playlistFailSave) { reply({ error: { message: 'The original path is unavailable on the host.' } }, 400); return; }
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
  const adminServer = createServer('admin'), commandServer = createServer('command'), publicServer = createServer('command', publicPrefix);
  await Promise.all([adminServer, commandServer, publicServer].map(server => new Promise(resolve => server.listen(0, '127.0.0.1', resolve))));
  const adminBase = `http://127.0.0.1:${adminServer.address().port}`, commandBase = `http://127.0.0.1:${commandServer.address().port}`;
  const publicBase = `http://127.0.0.1:${publicServer.address().port}`;
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

    gateway = { mode: 'gateway', url: '', hasToken: false, status: 'unconfigured', message: '', remoteURL: '' };
    const freshAdmin = await browser.newPage(); freshAdmin.on('pageerror', e => errors.push(e.message));
    await freshAdmin.goto(adminBase + '/admin');
    await freshAdmin.waitForFunction(() => document.getElementById('gateway-status').textContent.includes('Configure your gateway URL'));
    assert.equal(await freshAdmin.locator('#gateway-mode').inputValue(), 'gateway', 'new installations start with public gateway mode');
    assert.equal(await freshAdmin.locator('#remote-ready').isVisible(), false, 'unconfigured gateway mode does not advertise a LAN fallback');
    assert.equal(await freshAdmin.locator('#remote-connection-settings').evaluate(node => node.open), false, 'connection settings start collapsed');
    assert.equal(await freshAdmin.locator('#gateway-fields').isVisible(), false);
    assert.equal(await freshAdmin.locator('#gateway-status').isVisible(), true, 'connection status remains visible outside settings');
    assert.equal(await freshAdmin.locator('#lan-firewall-guidance').isVisible(), false);
    assert.equal(requests.some(r => r.path === '/api/gateway' && r.body.mode), false, 'loading Admin never opts into LAN automatically');
    await freshAdmin.locator('#remote-connection-settings > summary').click();
    await freshAdmin.locator('#gateway-mode').selectOption('lan');
    assert.equal(await freshAdmin.locator('#lan-firewall-guidance').isVisible(), false, 'an unsaved LAN draft does not advertise an active LAN firewall flow');
    await freshAdmin.locator('#save-gateway').click();
    await freshAdmin.waitForFunction(() => document.getElementById('gateway-message').textContent.includes('restarting to enable local network'));
    assert.deepEqual(requests.filter(r => r.path === '/api/gateway' && r.body.mode).at(-1).body, { mode: 'lan', url: '' });
    await freshAdmin.close(); gateway.restart = false;

    const admin = await browser.newPage({ viewport: { width: 1280, height: 900 }, hasTouch: false }); admin.on('pageerror', e => errors.push(e.message));
    await admin.setViewportSize({ width: 1280, height: 900 }); await admin.goto(adminBase + '/admin');
    await admin.locator('.playlist-row').first().waitFor(); await admin.locator('#remote-ready').waitFor();
    assert.equal(await admin.locator('#files-section, a[href="#files-section"]').count(), 0, 'Host files section and navigation are absent');
    assert.equal(await admin.locator('#remote-connection-settings').evaluate(node => node.open), false);
    const closedGatewayReads = requests.filter(r => r.path === '/api/gateway').length;
    await admin.evaluate(() => loadGateway());
    assert(requests.filter(r => r.path === '/api/gateway').length > closedGatewayReads, 'status still refreshes with settings collapsed');
    assert.equal(await admin.locator('#remote-connection-settings').evaluate(node => node.open), false, 'status refresh cannot open connection settings');
    assert.equal(await admin.locator('#remote-url').isVisible(), true, 'LAN link remains accessible while settings are collapsed');
    assert.equal(await admin.locator('#remote-qr').isVisible(), true);
    assert.equal(await admin.locator('#lan-firewall-guidance').isVisible(), true, 'saved LAN firewall instructions survive reload outside collapsed settings');
    assert.match(await admin.locator('#lan-firewall-guidance').textContent(), /Firewall.*Allow incoming connections/);
    assert.equal(await admin.locator('#pairing').isVisible(), false, 'local Admin opens automatically');
    assert.equal(await admin.locator('#logout').isVisible(), false, 'local Admin has no remote pairing logout');
    assert.equal(requests.find(r => r.path === '/api/local-session').origin, adminBase, 'Admin session request includes same-origin Origin');
    assert.equal(await admin.locator('.playlist-row').count(), 4);
    assert.equal(await admin.locator('#playlist img').count(), 0);
    assert.equal(await admin.locator('#audio-output option').count(), 2);
    await admin.waitForFunction(() => document.getElementById('gateway-status').textContent.includes('Local network'));
    assert.equal(await admin.locator('#gateway-mode').inputValue(), 'lan');
    assert.equal(await admin.locator('#gateway-fields').isVisible(), false);
    await admin.locator('#remote-connection-settings > summary').click();
    await admin.locator('#gateway-mode').selectOption('gateway');
    await admin.locator('#gateway-url').fill('https://stage.example.com/smartstage');
    assert.equal(await admin.locator('#gateway-token').getAttribute('required'), '', 'first gateway setup still requires a token');
    await admin.locator('#gateway-token').fill(gatewayRegistrationToken);
    await admin.evaluate(() => loadGateway());
    assert.equal(await admin.locator('#remote-connection-settings').evaluate(node => node.open), true, 'status refresh preserves an expanded settings form');
    assert.equal(await admin.locator('#gateway-mode').inputValue(), 'gateway', 'status polling preserves the unsaved connection mode');
    assert.equal(await admin.locator('#gateway-url').inputValue(), 'https://stage.example.com/smartstage');
    assert.equal(await admin.locator('#gateway-token').inputValue(), gatewayRegistrationToken, 'polling cannot overwrite a newly entered secret');
    gatewayFailSave = true;
    await admin.locator('#save-gateway').click();
    await admin.waitForFunction(() => document.getElementById('gateway-message').textContent.includes('Could not save'));
    assert.equal(await admin.locator('#gateway-token').inputValue(), '', 'a failed save clears the submitted gateway secret');
    assert.equal(await admin.evaluate(() => localStorage.length + sessionStorage.length), 0);
    gatewayFailSave = false;
    await admin.locator('#gateway-token').fill(gatewayRegistrationToken);
    gatewayDelay = 600;
    const staleGateway = admin.evaluate(() => loadGateway());
    await admin.waitForTimeout(50);
    await admin.locator('#save-gateway').click();
    await admin.waitForFunction(() => document.getElementById('gateway-message').textContent.startsWith('Saved.'));
    await staleGateway; gatewayDelay = 0;
    assert.equal(await admin.locator('#gateway-mode').inputValue(), 'gateway', 'an older LAN status response cannot revert a just-saved gateway');
    assert.equal(await admin.locator('#gateway-token').inputValue(), '');
    assert.match(await admin.locator('#gateway-token-hint').textContent(), /use the saved token with this URL/);
    assert.equal(await admin.locator('#remote-ready').isVisible(), false, 'connecting does not retain a stale LAN link or QR code');
    assert.match(await admin.locator('#remote-unavailable').textContent(), /gateway is not connected/);
    gateway = { ...gateway, status: 'connected', remoteURL: `https://stage.example.com${publicPrefix}/command#token=${gatewayRemoteToken}` };
    await admin.waitForFunction(expected => document.getElementById('remote-url').textContent === expected, gateway.remoteURL, { timeout: 7000 });
    assert.equal(await admin.locator('#remote-code-row').isVisible(), false, 'public links do not present a long key as an eight-digit LAN code');
    assert.equal(await admin.locator('#remote-qr').getAttribute('src'), '/api/remote-control/qr?index=0', 'QR generation remains local to Admin');
    assert.match(await admin.locator('#gateway-status').textContent(), /Local network remote access is disabled/);
    assert.equal(await admin.locator('#lan-firewall-guidance').isVisible(), false);
    assert.match(await admin.locator('#network-note-text').textContent(), /Keep awake/);
    assert.equal(await admin.locator('#gateway-token').inputValue(), '', 'status API never repopulates the registration secret');
    await admin.locator('#remote-connection-settings > summary').click();
    await admin.evaluate(async () => { await loadGateway(); await loadRemoteControl(); });
    assert.equal(await admin.locator('#remote-connection-settings').evaluate(node => node.open), false);
    assert.equal(await admin.locator('#remote-url').isVisible(), true, 'public URL stays visible outside collapsed settings');
    assert.equal(await admin.locator('#remote-qr').isVisible(), true, 'public QR stays visible outside collapsed settings');
    assert.equal(await admin.locator('#gateway-status').isVisible(), true);
    await admin.locator('#remote-connection-settings > summary').click();
    await admin.locator('#save-gateway').click();
    await admin.waitForFunction(() => !document.getElementById('save-gateway').disabled);
    assert.equal(requests.filter(r => r.path === '/api/gateway' && r.body.mode).at(-1).body.token, undefined, 'blank token keeps the stored token without echoing it');
    gateway = { ...gateway, status: 'error', message: 'Connection interrupted.', remoteURL: '' };
    await admin.evaluate(() => loadGateway());
    assert.equal(await admin.locator('#remote-ready').isVisible(), false);
    assert.equal(await admin.locator('#remote-url').getAttribute('href'), null, 'a disconnected gateway removes its obsolete public link');
    assert.equal(await admin.locator('#remote-qr').getAttribute('src'), null);
    await admin.locator('#reconnect-gateway').click();
    await admin.waitForFunction(() => document.getElementById('gateway-message').textContent.startsWith('Reconnecting.'));
    assert.equal(requests.filter(r => r.path === '/api/gateway/reconnect').length, 1);
    gateway = { ...gateway, status: 'connected', remoteURL: `https://stage.example.com/smartstage/e/${'23'.repeat(16)}/command#token=${gatewayRemoteToken}`, message: '' };
    await admin.evaluate(() => loadGateway());
    assert.equal(await admin.locator('#remote-url').textContent(), gateway.remoteURL, 'reconnection replaces the endpoint URL');
    await admin.locator('#remote-connection-settings > summary').click();
    await admin.locator('#remote-section').scrollIntoViewIfNeeded();
    await admin.screenshot({ path: path.join(output, 'admin-public-gateway.png') });
    await admin.locator('#remote-connection-settings > summary').click();
    await admin.locator('#gateway-url').fill('https://another.example.com/smartstage');
    assert.equal(await admin.locator('#gateway-token').getAttribute('required'), null, 'a stored token remains optional when the URL changes');
    assert.equal(await admin.locator('#gateway-token').inputValue(), '');
    await admin.locator('#save-gateway').click();
    await admin.waitForFunction(() => !document.getElementById('save-gateway').disabled);
    const urlOnlySave = requests.filter(r => r.path === '/api/gateway' && r.body.mode).at(-1).body;
    assert.equal(urlOnlySave.url, 'https://another.example.com/smartstage');
    assert.equal(urlOnlySave.token, undefined, 'URL-only save never reads or resends the stored secret through the browser');
    assert.equal(gateway.hasToken, true);
    await admin.reload();
    await admin.locator('#connection.live').waitFor();
    await admin.locator('#remote-connection-settings > summary').click();
    await admin.waitForFunction(() => document.getElementById('gateway-url').value === 'https://another.example.com/smartstage');
    assert.equal(await admin.locator('#gateway-token').inputValue(), '', 'reload does not expose the preserved token');
    assert.equal(await admin.locator('#gateway-token').getAttribute('required'), null);
    const replacementToken = 'c3'.repeat(32);
    await admin.locator('#gateway-token').fill(replacementToken);
    await admin.locator('#save-gateway').click();
    await admin.waitForFunction(() => !document.getElementById('save-gateway').disabled);
    assert.equal(requests.filter(r => r.path === '/api/gateway' && r.body.mode).at(-1).body.token, replacementToken);
    assert.equal(await admin.locator('#gateway-token').inputValue(), '', 'replacement token is cleared after save');
    await admin.locator('#gateway-mode').selectOption('lan');
    assert.equal(await admin.locator('#lan-firewall-guidance').isVisible(), false, 'firewall instructions follow saved mode, not an unsaved selection');
    await admin.locator('#save-gateway').click();
    await admin.waitForFunction(() => document.getElementById('gateway-status').textContent.includes('Local network remote control is enabled'));
    await admin.locator('#remote-ready').waitFor();
    assert.equal(await admin.locator('#remote-url').textContent(), links[0].url);
    await admin.locator('#remote-connection-settings > summary').click();
    assert.equal(await admin.locator('#lan-firewall-guidance').isVisible(), true);
    await untilPresence();
    async function untilPresence() { await admin.waitForFunction(() => !document.getElementById('quit-app').disabled); assert(requests.some(r => r.path === '/api/admin-presence' && r.listenerRole === 'admin'), 'Admin establishes authenticated presence'); }
    assert.equal(await admin.locator('#choose-files').isVisible(), false, 'the native picker stays hidden until the desktop host is ready');
    const documentsBeforeChooserReady = adminDocumentLoads;
    adminCapabilities = { chooseFiles: true };
    await admin.evaluate(() => sendAdminPresence(true));
    await admin.locator('#choose-files').waitFor({ state: 'visible' });
    assert.equal(adminDocumentLoads, documentsBeforeChooserReady, 'a presence response refreshes native picker readiness without reloading Admin');
    assert.equal(await admin.locator('#choose-files').textContent(), 'Choose Media…');
    const beforeChooserImport = config.cues.length;
    nativeChooserImports = [{ path: "/Host/Show/Café's opening.mp4", kind: 'video' }];
    await admin.locator('#choose-files').click();
    await admin.waitForFunction(() => document.getElementById('file-drop-message').textContent.includes('Mac dialog'));
    assert.equal(requests.filter(r => r.path === '/api/choose-files').length, 1);
    await admin.waitForFunction(expected => document.querySelectorAll('.playlist-row').length === expected, beforeChooserImport + 1);
    assert.equal(config.cues.at(-1).path, "/Host/Show/Café's opening.mp4", 'native chooser import keeps the original host path in the authoritative playlist');
    adminCapabilities = { chooseFiles: false }; await admin.evaluate(() => refreshState());
    assert.equal(await admin.locator('#choose-files').isVisible(), false, 'unsupported hosts never show a nonworking native-picker button');
    assert.equal(await admin.locator('#media-path-settings').isVisible(), true, 'standalone browsers retain original-path import');
    assert.equal(await admin.locator('#media-path-settings').evaluate(node => node.open), false, 'path import is compact by default');
    await admin.locator('#media-path-settings > summary').click();
    const beforePathWrites = requests.filter(r => r.path === '/api/playlist' && r.body.cues).length;
    await admin.locator('#media-paths').fill('relative.wav');
    await admin.locator('#add-media-paths').click();
    assert.match(await admin.locator('#file-drop-message').textContent(), /absolute file paths/);
    assert.equal(requests.filter(r => r.path === '/api/playlist' && r.body.cues).length, beforePathWrites, 'relative paths are not invented or submitted');
    const pathDraft = '"/Host/Show/path-added.wav"\n"C:\\Show\\quoted media.wav"\n"/Host/Show/path-added.wav"';
    await admin.locator('#media-paths').fill(pathDraft);
    playlistFailSave = true;
    await admin.locator('#add-media-paths').click();
    await admin.waitForFunction(() => document.getElementById('file-drop-message').textContent.startsWith('The files were not added.'));
    assert.equal(await admin.locator('#media-paths').inputValue(), pathDraft, 'failed host-path import retains the draft for correction');
    playlistFailSave = false;
    const beforePathImport = config.cues.length;
    await admin.locator('#add-media-paths').click();
    await admin.waitForFunction(expected => document.querySelectorAll('.playlist-row').length === expected, beforePathImport + 2);
    assert.deepEqual(config.cues.slice(-2).map(cue => cue.path), ['/Host/Show/path-added.wav', 'C:\\Show\\quoted media.wav'], 'quoted original paths are preserved and duplicate lines add only one cue');
    await admin.waitForFunction(() => document.getElementById('media-paths').value === '');
    assert.equal(await admin.locator('#media-paths').inputValue(), '');
    assert.match(await admin.locator('#file-drop-message').textContent(), /Added 2 files.*Originals stay in place/);
    adminCapabilities = { chooseFiles: true }; await admin.evaluate(() => refreshState());
    assert.equal(await admin.locator('#media-path-settings').isVisible(), false, 'native chooser availability hides the fallback');
    assert.equal(requests.some(r => ['/api/files', '/api/inspect'].includes(r.path)), false, 'Admin does not browse host folders in the background');
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
    assert.equal(requests.some(r => r.path === '/api/playlist/import'), false, 'native media selection never uploads file bytes');
    const firstColor = admin.getByRole('textbox', { name: 'Label for cue 1', exact: true });
    await admin.locator('.cue-color-controls input[type=color]').first().evaluate(input => { input.value = '#ffff00'; input.dispatchEvent(new Event('change', { bubbles: true })); });
    await page.waitForFunction(() => getComputedStyle(document.querySelector('.cue')).backgroundColor === 'rgb(255, 255, 0)');
    assert.equal(config.cues[0].color, '#ffff00', 'Admin color changes are saved in the playlist');
    assert.equal(await page.locator('.cue').first().evaluate(node => getComputedStyle(node).color), 'rgb(0, 0, 0)', 'bright cue backgrounds use dark text under the real content security policy');
    const expectedLabelRevision = config.playlistRevision + 1;
    await firstColor.fill('Opening music in yellow'); await firstColor.press('Tab');
    await admin.waitForFunction(revision => document.getElementById('playlist-revision').textContent.endsWith(`saved revision ${revision}`), expectedLabelRevision);
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
    assert.equal(await admin.getByRole('checkbox', { name: 'Fade audio, video and images', exact: true }).count(), 1);
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
    assert.match(await page.locator('[data-cue-id="image-fixture"] .cue-meta').textContent(), /Press again to stop/);
    await admin.waitForFunction(() => [...document.querySelectorAll('.playlist-row button')].some(button => button.textContent === 'Hide image'));
    await page.locator('[data-cue-id="image-fixture"]').click();
    assert.equal(requests.filter(r => r.path === '/api/play').at(-1).body.cueId, 'image-fixture', 'an active image sends the same cue for the host to toggle');
    state.imageCueId = ''; state.revision++; broadcast();
    await page.waitForFunction(() => document.querySelector('[data-cue-id="image-fixture"]').getAttribute('aria-pressed') === 'false');
    assert.equal(await page.locator('[data-cue-id="cue-0"]').getAttribute('aria-pressed'), 'true', 'toggling an image off keeps independent music selected');
    state.imageCueId = 'image-fixture'; state.revision++; broadcast();
    await page.waitForFunction(() => document.querySelector('[data-cue-id="image-fixture"]').getAttribute('aria-pressed') === 'true');
    assert.match(await page.locator('[data-cue-id="cue-0"] .cue-meta').textContent(), /Press again to stop/);
    await page.locator('[data-cue-id="cue-0"]').click();
    assert.equal(requests.filter(r => r.path === '/api/play').at(-1).body.cueId, 'cue-0', 'a selected music button sends the same cue; host decides the configured toggle');
    state.activeCueId = ''; state.state = 'stopped'; state.revision++; broadcast();
    await page.waitForFunction(() => document.querySelector('[data-cue-id="cue-0"]').getAttribute('aria-pressed') === 'false');
    assert.equal(await page.locator('[data-cue-id="image-fixture"]').getAttribute('aria-pressed'), 'true', 'stopping music alone preserves the selected image');
    // Video toggles do not depend on the optional music setting. The fixture
    // only supplies authoritative state; native fade execution is tested elsewhere.
    state.stage.toggleAudio = false; state.activeCueId = 'cue-3'; state.imageCueId = ''; state.state = 'playing'; state.revision++; broadcast();
    await page.waitForFunction(() => document.querySelector('[data-cue-id="cue-3"] .cue-meta').textContent.includes('Press again to stop'));
    await admin.waitForFunction(() => [...document.querySelectorAll('.playlist-row button')].some(button => button.textContent === 'Stop video'));
    await page.locator('[data-cue-id="cue-3"]').click();
    assert.equal(requests.filter(r => r.path === '/api/play').at(-1).body.cueId, 'cue-3');
    state.stage.toggleAudio = true; state.activeCueId = ''; state.state = 'stopped'; state.revision++; broadcast();
    await page.waitForFunction(() => document.querySelector('[data-cue-id="cue-3"]').getAttribute('aria-pressed') === 'false');
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
    await admin.locator('#install-update').click();
    await admin.waitForFunction(() => document.getElementById('install-update').textContent === 'Downloading…');
    assert.equal(requests.filter(r => r.path === '/api/update/install').length, 1, 'one click starts exactly one install');
    assert.equal(await admin.locator('.playlist-row input').first().isDisabled(), true, 'playlist editing is disabled during preparation');
    assert.equal(await admin.locator('.cue-tools button').first().isDisabled(), true, 'Admin PLAY is disabled during preparation');
    assert.equal(await page.locator('.cue').first().isDisabled(), true, 'remote PLAY is disabled by authoritative updatePending state');
    assert.equal(await admin.locator('#save-outputs').isDisabled(), true);
    assert.equal(await admin.locator('#choose-files').isDisabled(), true, 'native media import is disabled during update preparation');
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

    // User-agent fixtures exercise presentation only. Native file pickers and
    // Finder/Explorer drop handling are verified separately on each OS.
    for (const platform of [
      { name: 'mac', marker: 'SmartStageDesktop', manager: 'Finder', picker: 'Mac', original: '/Host/Show/native-added-mac.wav', screenshot: 'admin-desktop-drop.png' },
      { name: 'windows', marker: 'SmartStageDesktop SmartStageWindowsDesktop', manager: 'File Explorer', picker: 'Windows', original: "C:\\Show\\Café's native-added-windows.wav", screenshot: 'admin-windows-desktop-drop.png' }
    ]) {
      const desktop = await browser.newPage({ viewport: { width: 1280, height: 900 }, userAgent: `BrowserFixture ${platform.marker}` });
      desktop.on('pageerror', e => errors.push(e.message));
      const sessionsBeforeDesktopRemote = requests.filter(r => r.path === '/api/local-session').length;
      await desktop.goto(commandBase + '/command'); await desktop.locator('#pairing').waitFor();
      assert.equal(await desktop.locator('#admin-view').isVisible(), false, 'desktop markers never expose Admin on the remote page');
      assert.equal(requests.filter(r => r.path === '/api/local-session').length, sessionsBeforeDesktopRemote, 'desktop markers never bypass remote pairing');
      adminCapabilities = { chooseFiles: false };
      await desktop.goto(adminBase + '/admin'); await desktop.locator('.playlist-row').first().waitFor();
      assert.equal(await desktop.locator('#playlist-drop-title').textContent(), `Drop ${platform.manager} files here`);
      assert.match(await desktop.locator('#playlist-drop-hint').textContent(), /Originals stay in place.*nothing is uploaded or copied/);
      assert.equal(await desktop.locator('#choose-files').isVisible(), false, 'a native user-agent cannot grant picker capability');
      assert.equal(await desktop.locator('#media-path-settings').isVisible(), false, 'native windows never show the path-entry fallback while initializing');
      assert.equal(await desktop.locator('#remote-connection-settings').evaluate(node => node.open), false);
      assert.equal(await desktop.locator('#remote-qr').isVisible(), true);
      assert.equal(await desktop.locator('#lan-firewall-guidance').isVisible(), true);
      await desktop.evaluate(async () => { await loadGateway(); await loadRemoteControl(); });
      assert.equal(await desktop.locator('#remote-connection-settings').evaluate(node => node.open), false, 'native polling preserves collapsed settings');
      assert.equal(await desktop.locator('#files-section').count(), 0);
      const writesBeforeDesktopDrop = requests.filter(r => r.path === '/api/playlist' && r.body.cues).length;
      await desktop.evaluate(() => {
        const files = new DataTransfer(); files.items.add(new File(['example'], 'spoofed native file.wav', { type: 'audio/wav' }));
        const drop = new DragEvent('drop', { dataTransfer: files, bubbles: true, cancelable: true });
        document.getElementById('playlist-drop').dispatchEvent(drop); window.__desktopDropPrevented = drop.defaultPrevented;
        const paths = new DataTransfer(); paths.setData('application/x-smartstage-host-files', '/untrusted/native-spoof.wav');
        document.getElementById('playlist-drop').dispatchEvent(new DragEvent('drop', { dataTransfer: paths, bubbles: true, cancelable: true }));
      });
      assert.equal(await desktop.evaluate(() => window.__desktopDropPrevented), true);
      assert.match(await desktop.locator('#file-drop-message').textContent(), new RegExp(platform.manager));
      assert.equal(requests.filter(r => r.path === '/api/playlist' && r.body.cues).length, writesBeforeDesktopDrop, 'a spoofed desktop marker cannot turn File objects or forged drag paths into imports');
      assert.equal(requests.some(r => r.path === '/api/playlist/import'), false);
      adminCapabilities = { chooseFiles: true };
      await desktop.evaluate(() => sendAdminPresence(true));
      await desktop.locator('#choose-files').waitFor({ state: 'visible' });
      assert.equal(await desktop.locator('#choose-files').textContent(), 'Choose Media…');
      assert.equal(await desktop.locator('#media-path-settings').isVisible(), false);
      const beforeDesktopAddition = config.cues.length;
      const pickerRequestsBefore = requests.filter(r => r.path === '/api/choose-files').length;
      nativeChooserImports = [{ path: platform.original, kind: 'audio' }];
      await desktop.locator('#choose-files').click();
      await desktop.waitForFunction(picker => document.getElementById('file-drop-message').textContent.includes(`${picker} dialog`), platform.picker);
      assert.equal(requests.filter(r => r.path === '/api/choose-files').length, pickerRequestsBefore + 1);
      await desktop.waitForFunction(expected => document.querySelectorAll('.playlist-row').length === expected, beforeDesktopAddition + 1);
      assert.equal(config.cues.at(-1).path, platform.original, 'native import feedback uses the authoritative original host path');
      assert.equal(await desktop.locator('#file-drop-message').textContent(), 'Added 1 file to the playlist. Originals stay in place.');
      await desktop.locator('#playlist-drop').scrollIntoViewIfNeeded();
      await desktop.screenshot({ path: path.join(output, platform.screenshot) });
      await desktop.close();
    }

    const windowsBrowser = await browser.newPage({ userAgent: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) BrowserFixture' });
    windowsBrowser.on('pageerror', e => errors.push(e.message));
    await windowsBrowser.goto(adminBase + '/admin'); await windowsBrowser.locator('#choose-files').waitFor({ state: 'visible' });
    assert.equal(await windowsBrowser.locator('#playlist-drop-title').textContent(), 'Add media from this PC');
    assert.match(await windowsBrowser.locator('#playlist-drop-hint').textContent(), /File Explorer.*Smart Stage app window/);
    assert.doesNotMatch(await windowsBrowser.locator('#playlist-drop-hint').textContent(), /Finder|Dock|Mac/);
    assert.equal(await windowsBrowser.locator('#media-path-settings').isVisible(), false);
    adminCapabilities = { chooseFiles: false }; await windowsBrowser.evaluate(() => refreshState());
    assert.equal(await windowsBrowser.locator('#choose-files').isVisible(), false);
    assert.equal(await windowsBrowser.locator('#media-path-settings').isVisible(), true, 'external Windows browsers retain path fallback without native chooser capability');
    await windowsBrowser.close();
    adminCapabilities = { chooseFiles: true };

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
    const publicContext = await browser.newContext({ viewport: { width: 390, height: 844 }, hasTouch: true });
    const publicPage = await publicContext.newPage(); publicPage.on('pageerror', e => errors.push(e.message));
    await publicPage.addInitScript(() => {
      const nativeFetch = window.fetch;
      window.fetch = (...args) => { if (String(args[0]).endsWith('/api/pair')) window.__pairingHash = location.hash; return nativeFetch(...args); };
    });
    await publicPage.goto(`${publicBase}${publicPrefix}/command#token=${gatewayRemoteToken}`);
    await publicPage.locator('#connection.live').waitFor();
    assert.equal(await publicPage.evaluate(() => location.hash), '');
    assert.equal(await publicPage.evaluate(() => window.__pairingHash), '', 'public fragment is consumed before sending its key');
    assert.equal(await publicPage.evaluate(() => localStorage.length + sessionStorage.length), 0);
    assert.equal(await publicPage.locator('#admin-view').isVisible(), false);
    assert.equal(await publicPage.locator('#pair-key').getAttribute('maxlength'), '64');
    assert(publicRequests.includes(publicPrefix + '/assets/app.js'));
    assert(publicRequests.includes(publicPrefix + '/assets/wake-lock.js'));
    assert(publicRequests.includes(publicPrefix + '/assets/style.css'));
    assert(publicRequests.includes(publicPrefix + '/api/events'), 'public EventSource stays inside its endpoint prefix');
    assert.equal(await publicPage.locator('.cue').count(), state.cues.filter(c => !c.hidden).length);
    const publicCommandsBefore = requests.length;
    await publicPage.locator('.cue').first().tap();
    await publicPage.locator('#remote-stage').tap();
    await publicPage.locator('#stop').tap();
    await publicPage.keyboard.press('Escape');
    await publicPage.waitForTimeout(250);
    for (const command of ['/api/play', '/api/stage-output', '/api/stop', '/api/emergency-stop']) {
      const request = requests.slice(publicCommandsBefore).find(r => r.requestPath === publicPrefix + command);
      assert(request, `public ${command} stays inside the endpoint`);
      assert.equal(request.origin, publicBase);
      assert.equal(request.csrf, 'test-csrf');
    }
    const publicPairs = requests.filter(r => r.requestPath === publicPrefix + '/api/pair').length;
    await publicPage.reload(); await publicPage.locator('#connection.live').waitFor();
    assert.equal(requests.filter(r => r.requestPath === publicPrefix + '/api/pair').length, publicPairs, 'scoped public cookie resumes on reload');
    const publicCookie = (await publicContext.cookies()).find(cookie => cookie.name === 'smartstage_command_session');
    assert.equal(publicCookie.path, publicPrefix); assert.equal(publicCookie.httpOnly, true);
    await publicPage.locator('#logout').click(); await publicPage.locator('#pairing').waitFor();
    assert.match(await publicPage.locator('#pair-guidance').textContent(), /current QR code/);
    assert.match(await publicPage.locator('#pair-network-hint').textContent(), /public gateway/);
    assert(requests.some(r => r.requestPath === publicPrefix + '/api/logout'));
    await publicPage.locator('#pair-key').fill(gatewayRemoteToken); await publicPage.getByRole('button', { name: 'Connect', exact: true }).click();
    await publicPage.locator('#connection.live').waitFor();
    assert.equal(await publicPage.locator('#pair-key').inputValue(), '');
    await publicPage.screenshot({ path: path.join(output, 'command-public-gateway.png'), fullPage: true });
    assert(publicRequests.every(url => url.startsWith(publicPrefix + '/')), 'no public asset, API, or event request escapes its endpoint');
    assert.equal(publicRequests.some(url => /local-session|admin-presence|gateway|playlist|files|quit/.test(url)), false, 'public remote does not request any local Admin API');
    await publicContext.close();

    // Playlist files describe the show; media bytes, outputs and gateway
    // credentials never belong in a browser download or import request.
    adminCapabilities = { chooseFiles: true, playlistFiles: false };
    await admin.evaluate(async () => { await refreshState(); await loadPlaylist(); });
    const savedShow = exportedPlaylist(), outputsBeforeFile = JSON.stringify(config.outputs), gatewayBeforeFile = JSON.stringify(gateway);
    const downloadEvent = admin.waitForEvent('download'); await admin.locator('#save-playlist-file').click();
    const playlistDownload = await downloadEvent;
    assert.equal(playlistDownload.suggestedFilename(), 'Playlist.smartstage.json');
    const savedDocument = JSON.parse(fs.readFileSync(await playlistDownload.path(), 'utf8'));
    assert.deepEqual(savedDocument, savedShow);
    assert.deepEqual(Object.keys(savedDocument).sort(), ['cues', 'format', 'stage', 'version']);
    assert(savedDocument.cues.every(cue => !('cache' in cue)), 'validation cache is absent from the downloaded playlist');
    const importsBeforeFile = () => requests.filter(r => r.path === '/api/playlist/import').length;
    const beginBrowserPlaylistLoad = async () => {
      await admin.locator('#load-playlist-file').click(); await admin.locator('#playlist-load-confirm').waitFor();
      const chooserEvent = admin.waitForEvent('filechooser'); await admin.locator('#playlist-load-continue').click(); return chooserEvent;
    };
    const filePayload = document => ({name: 'Show.smartstage.json', mimeType: 'application/json', buffer: Buffer.from(JSON.stringify(document))});
    const beforeCancelImports = importsBeforeFile(), beforeCancelCommands = commands.length;
    await admin.locator('#load-playlist-file').click(); await admin.locator('#playlist-load-cancel').click();
    assert.equal(await admin.locator('#playlist-load-confirm').isVisible(), false);
    await admin.locator('#load-playlist-file').click(); await admin.keyboard.press('Escape');
    assert.equal(await admin.locator('#playlist-load-confirm').isVisible(), false);
    assert.equal(importsBeforeFile(), beforeCancelImports, 'confirmation cancellation cannot start a replacement');
    assert.equal(commands.length, beforeCancelCommands, 'Escape closes the file confirmation without a playback command');

    await admin.locator('#fade-enabled').check(); await admin.locator('#fade-seconds').fill('2.7');
    assert.equal(await admin.locator('#save-playlist-file').isDisabled(), true, 'unsaved stage drafts cannot silently be excluded from Save');
    assert.match(await admin.locator('#playlist-file-guidance').textContent(), /Save or reload/);
    let chooser = await beginBrowserPlaylistLoad();
    await chooser.setFiles({name: 'Broken.smartstage.json', mimeType: 'application/json', buffer: Buffer.from('{ broken')});
    await admin.waitForFunction(() => document.getElementById('playlist-file-message').textContent.includes('not valid playlist JSON'));
    assert.equal(importsBeforeFile(), beforeCancelImports, 'invalid JSON is rejected without an import request');
    assert.equal(await admin.locator('#fade-seconds').inputValue(), '2.7', 'a failed file load preserves the unsaved stage draft');
    assert.equal(await admin.locator('#save-playlist-file').isDisabled(), true);
    chooser = await beginBrowserPlaylistLoad(); await chooser.setFiles(filePayload({schema: 1, cues: []}));
    await admin.waitForFunction(() => document.getElementById('playlist-file-message').textContent.includes('not a supported'));
    assert.deepEqual(exportedPlaylist(), savedShow, 'a rejected file leaves the saved show unchanged');
    const beforeOversize = importsBeforeFile();
    chooser = await beginBrowserPlaylistLoad();
    await chooser.setFiles({name: 'Large.smartstage.json', mimeType: 'application/json', buffer: Buffer.alloc(4 * 1024 * 1024 + 1, 32)});
    await admin.waitForFunction(() => document.getElementById('playlist-file-message').textContent.includes('no larger than 4 MiB'));
    assert.equal(importsBeforeFile(), beforeOversize, 'oversized files never enter the JSON import request');
    chooser = await beginBrowserPlaylistLoad();
    config.playlistRevision++; state.playlistRevision = config.playlistRevision; state.revision++; broadcast();
    await chooser.setFiles(filePayload(savedDocument));
    await admin.waitForFunction(() => document.getElementById('playlist-file-message').textContent.includes('The playlist changed'));
    assert.equal(await admin.locator('#fade-seconds').inputValue(), '2.7', 'a revision conflict preserves the stage draft');
    assert.deepEqual(exportedPlaylist(), savedShow);
    await admin.locator('#reload-playlist').click();
    await admin.waitForFunction(() => !document.getElementById('save-playlist-file').disabled);
    config.cues[0].label = 'Changed after saving'; config.stage.fadeSeconds = 3.5;
    config.playlistRevision++; state.playlistRevision = config.playlistRevision; state.revision++; broadcast();
    await admin.evaluate(() => loadPlaylist());
    chooser = await beginBrowserPlaylistLoad(); await chooser.setFiles(filePayload(savedDocument));
    await admin.waitForFunction(() => document.getElementById('playlist-file-message').textContent.startsWith('Playlist loaded.'));
    assert.deepEqual(config.cues.map(({id, cache, ...cue}) => cue), savedDocument.cues.map(({id, ...cue}) => cue), 'file roundtrip restores labels, original paths, order, colors, hidden flags and background buttons');
    const savedBackgroundIndex = savedDocument.cues.findIndex(cue => cue.id === savedDocument.stage.backgroundCueId);
    assert.deepEqual(config.stage, {...savedDocument.stage, backgroundCueId: savedBackgroundIndex >= 0 ? config.cues[savedBackgroundIndex].id : ''}, 'file roundtrip restores stage settings with fresh cue identities');
    assert.equal(await admin.locator('#save-playlist-file').isDisabled(), false, 'a successful replacement clears discarded stage drafts');
    assert.equal(JSON.stringify(config.outputs), outputsBeforeFile); assert.equal(JSON.stringify(gateway), gatewayBeforeFile);
    for (const guardedState of [{state: 'playing', stageEnabled: false}, {state: 'stopped', stageEnabled: true}]) {
      Object.assign(state, guardedState); state.revision++; broadcast(); await admin.evaluate(() => refreshState());
      assert.equal(await admin.locator('#load-playlist-file').isDisabled(), true);
      assert.equal(await admin.locator('#save-playlist-file').isDisabled(), false, 'a saved show can be exported without interrupting playback');
    }
    state.state = 'stopped'; state.stageEnabled = false; state.updatePending = true; state.revision++; broadcast(); await admin.evaluate(() => refreshState());
    assert.equal(await admin.locator('#save-playlist-file').isDisabled(), true); assert.equal(await admin.locator('#load-playlist-file').isDisabled(), true);
    state.updatePending = false; state.revision++; broadcast(); await admin.evaluate(() => refreshState());

    state.state = 'error'; state.revision++; broadcast(); await admin.evaluate(() => refreshState());
    assert.equal(await admin.locator('#load-playlist-file').isDisabled(), false, 'an idle playback error can recover by loading another show');
    state.state = 'stopped'; state.revision++; broadcast(); await admin.evaluate(() => refreshState());

    adminCapabilities = { chooseFiles: true, playlistFiles: true }; await admin.evaluate(() => refreshState());
    const nativeExportsBefore = requests.filter(r => r.path === '/api/playlist/export').length;
    await admin.locator('#save-playlist-file').click();
    assert.equal(await admin.locator('#load-playlist-file').isDisabled(), true, 'native dialog completion keeps playlist editing reserved');
    assert.equal(await admin.locator('#choose-files').isDisabled(), true);
    await admin.waitForFunction(() => document.getElementById('playlist-file-message').textContent === 'Playlist file saved.');
    assert.equal(requests.filter(r => r.path === '/api/playlist/export').length, nativeExportsBefore, 'native file saving does not start a WebView download');
    assert.equal(requests.filter(r => r.path === '/api/playlist/file' && r.body.operation === 'save').length, 1);
    await admin.locator('#fade-enabled').check(); await admin.locator('#fade-seconds').fill('2.2');
    const beforeNativeCancel = JSON.stringify(config);
    nativePlaylistResult = { phase: 'cancelled' };
    await admin.locator('#load-playlist-file').click(); await admin.locator('#playlist-load-continue').click();
    await admin.waitForFunction(() => document.getElementById('playlist-file-message').textContent.startsWith('Cancelled.'));
    assert.equal(JSON.stringify(config), beforeNativeCancel); assert.equal(await admin.locator('#fade-seconds').inputValue(), '2.2');
    nativePlaylistResult = { phase: 'error', message: 'The selected playlist file changed; choose it again' };
    await admin.locator('#load-playlist-file').click(); await admin.locator('#playlist-load-continue').click();
    await admin.waitForFunction(() => document.getElementById('playlist-file-message').textContent.includes('selected playlist file changed'));
    assert.equal(JSON.stringify(config), beforeNativeCancel); assert.equal(await admin.locator('#fade-seconds').inputValue(), '2.2');
    nativePlaylistResult = { phase: 'complete', playlist: exportedPlaylist() };
    await admin.locator('#load-playlist-file').click(); await admin.locator('#playlist-load-continue').click();
    await admin.waitForFunction(() => document.getElementById('playlist-file-message').textContent.startsWith('Playlist loaded.'));
    assert.equal(await admin.locator('#save-playlist-file').isDisabled(), false);
    assert.equal(await admin.locator('#fade-seconds').inputValue(), String(config.stage.fadeSeconds));
    nativePlaylistMismatch = true;
    await admin.locator('#save-playlist-file').click();
    await admin.waitForFunction(() => document.getElementById('playlist-file-message').textContent.includes('request could not be confirmed'));
    nativePlaylistMismatch = false;
    assert.equal(JSON.stringify(config.outputs), outputsBeforeFile); assert.equal(JSON.stringify(gateway), gatewayBeforeFile);

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
    assert.equal(await admin.locator('#remote-connection-settings').evaluate(node => node.open), false);
    assert.equal(await admin.locator('#lan-firewall-guidance').isVisible(), true, 'saved LAN guidance remains visible after restart');
    assert.equal(requests.some(r => ['/api/files', '/api/inspect'].includes(r.path)), false, 'no Admin page or reconnect starts folder browsing');
    assert.deepEqual(errors, []);
    // Locale is a display preference. The native host owns Admin persistence;
    // remote devices use their own browser preference and never call Admin APIs.
    language = {mode: 'system', effective: 'it', system: 'it'};
    const localeContext = await browser.newContext({locale: 'en-US', viewport: {width: 1280, height: 900}});
    const localeAdmin = await localeContext.newPage(); localeAdmin.on('pageerror', e => errors.push(e.message));
    await localeAdmin.goto(adminBase + '/admin');
    await localeAdmin.locator('.playlist-row').first().waitFor();
    await localeAdmin.waitForFunction(() => document.documentElement.lang === 'it');
    assert.equal(await localeAdmin.locator('#quit-app').textContent(), 'Esci da Smart Stage');
    assert.equal(await localeAdmin.locator('#save-playlist-file').textContent(), 'Salva playlist…');
    assert.equal(await localeAdmin.locator('#load-playlist-file').textContent(), 'Carica playlist…');
    assert.equal(await localeAdmin.getByRole('checkbox', { name: 'Dissolvenza per audio, video e immagini', exact: true }).count(), 1);
    await localeAdmin.locator('#load-playlist-file').click();
    assert.equal(await localeAdmin.locator('#playlist-load-heading').textContent(), 'Caricare una playlist?');
    assert.match(await localeAdmin.locator('#playlist-load-description').textContent(), /File multimediali, uscite e impostazioni di connessione/);
    await localeAdmin.locator('#playlist-load-cancel').click();
    assert.equal(await localeAdmin.locator('#language-mode').inputValue(), 'system');
    assert.match(await localeAdmin.locator('#gateway-token-hint').textContent(), /token salvato con questo URL/, 'stored-token guidance is translated into Italian');
    assert.equal(await localeAdmin.evaluate(() => navigator.language), 'en-US', 'Admin follows host system language rather than browser language');
    const rawLabel = await localeAdmin.locator('.playlist-row input').first().inputValue();
    const rawPath = await localeAdmin.locator('.source-path').first().textContent();
    assert((await localeAdmin.locator('#display-output option[value=screen]').textContent()).startsWith('Stage display ·'), 'a device name matching a UI translation key stays unchanged');
    await localeAdmin.setViewportSize({width: 640, height: 900});
    assert(await localeAdmin.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'Italian Admin fits the native 640px minimum width');
    await localeAdmin.setViewportSize({width: 1280, height: 900});
    const beforeLocaleMutations = requests.filter(r => ['/api/play', '/api/stop', '/api/emergency-stop', '/api/playlist', '/api/stage-settings', '/api/outputs', '/api/gateway'].includes(r.path) && Object.keys(r.body).length).length;
    await localeAdmin.evaluate(() => {
      const input = document.querySelector('.playlist-row input');
      input.value = 'Color <originale> 🟢'; input.focus(); input.setSelectionRange(3, 7); window.__localeDraft = input;
      const details = document.getElementById('remote-connection-settings'); details.open = true;
      const mode = document.getElementById('gateway-mode'); mode.value = 'gateway'; mode.dispatchEvent(new Event('input', {bubbles: true}));
      for (const [id, value] of [['gateway-url', 'https://draft.example/smartstage'], ['gateway-token', 'secret-draft-not-translated'], ['fade-seconds', '2.7']]) {
        const node = document.getElementById(id); node.value = value; node.dispatchEvent(new Event('input', {bubbles: true}));
      }
      document.getElementById('audio-output').selectedIndex = 1;
    });
    const audioDraft = await localeAdmin.locator('#audio-output').inputValue();
    await localeAdmin.locator('#language-mode').selectOption('en');
    await localeAdmin.waitForFunction(() => document.documentElement.lang === 'en' && !document.getElementById('language-mode').disabled);
    assert.equal(await localeAdmin.locator('#quit-app').textContent(), 'Quit Smart Stage');
    assert.equal(await localeAdmin.locator('#save-playlist-file').textContent(), 'Save playlist…');
    assert.equal(await localeAdmin.locator('.playlist-row input').first().inputValue(), 'Color <originale> 🟢');
    assert.equal(await localeAdmin.locator('.source-path').first().textContent(), rawPath);
    assert.equal(await localeAdmin.locator('#gateway-token').inputValue(), 'secret-draft-not-translated');
    assert.equal(await localeAdmin.locator('#gateway-url').inputValue(), 'https://draft.example/smartstage');
    assert.equal(await localeAdmin.locator('#fade-seconds').inputValue(), '2.7');
    assert.equal(await localeAdmin.locator('#audio-output').inputValue(), audioDraft);
    assert.equal(await localeAdmin.locator('#remote-connection-settings').evaluate(node => node.open), true);
    assert.equal(await localeAdmin.evaluate(() => window.__localeDraft === document.querySelector('.playlist-row input') && document.activeElement === window.__localeDraft && window.__localeDraft.selectionStart === 3 && window.__localeDraft.selectionEnd === 7), true, 'locale change retains the focused input node and selection');
    assert.equal(requests.filter(r => ['/api/play', '/api/stop', '/api/emergency-stop', '/api/playlist', '/api/stage-settings', '/api/outputs', '/api/gateway'].includes(r.path) && Object.keys(r.body).length).length, beforeLocaleMutations, 'changing language sends no playback or show-edit mutation');
    assert.equal(await localeAdmin.evaluate(() => localStorage.length + sessionStorage.length), 0, 'Admin preference is persisted by the host, not WebView storage');
    await localeAdmin.reload(); await localeAdmin.locator('.playlist-row').first().waitFor();
    await localeAdmin.waitForFunction(() => document.documentElement.lang === 'en');
    assert.equal(await localeAdmin.locator('#language-mode').inputValue(), 'en');
    assert.equal(await localeAdmin.locator('.playlist-row input').first().inputValue(), rawLabel);
    await localeAdmin.locator('#language-mode').selectOption('it');
    await localeAdmin.waitForFunction(() => document.documentElement.lang === 'it' && !document.getElementById('language-mode').disabled);
    languageSaveFail = true;
    await localeAdmin.locator('#language-mode').selectOption('en');
    await localeAdmin.waitForFunction(() => !document.getElementById('language-mode').disabled && document.getElementById('notice').textContent.includes('Impossibile salvare'));
    assert.equal(await localeAdmin.locator('#language-mode').inputValue(), 'it', 'failed preference write restores the saved selection');
    assert.equal(await localeAdmin.locator('html').getAttribute('lang'), 'it');
    languageSaveFail = false;
    const beforeAnchorSessions = requests.filter(r => r.path === '/api/local-session').length;
    await localeAdmin.locator('a[href="#stage-section"]').click(); await localeAdmin.waitForTimeout(100);
    assert.equal(requests.filter(r => r.path === '/api/local-session').length, beforeAnchorSessions, 'Admin section navigation cannot reconnect or reload');
    const sectionClear = await localeAdmin.locator('#stage-section').evaluate(node => node.getBoundingClientRect().top >= document.querySelector('.transport').getBoundingClientRect().bottom);
    assert(sectionClear, 'anchor target clears the actual translated transport height');
    language.system = 'en';
    await localeAdmin.locator('#language-mode').selectOption('system');
    await localeAdmin.waitForFunction(() => document.documentElement.lang === 'en' && !document.getElementById('language-mode').disabled);
    await localeContext.close();

    const italianRemoteContext = await browser.newContext({locale: 'it-IT', viewport: {width: 390, height: 844}});
    const italianRemote = await italianRemoteContext.newPage(); italianRemote.on('pageerror', e => errors.push(e.message));
    await italianRemote.goto(commandBase + '/command#token=' + token); await italianRemote.locator('#connection.live').waitFor();
    assert.equal(await italianRemote.locator('html').getAttribute('lang'), 'it');
    assert.equal(await italianRemote.locator('#connection').textContent(), 'Collegato al computer');
    assert.equal(await italianRemote.locator('#keep-awake-status').textContent(), await italianRemote.evaluate(() => window.isSecureContext && typeof navigator.wakeLock?.request === 'function') ? 'Disattivo' : 'Richiede HTTPS');
    const remoteLabel = await italianRemote.locator('.cue-title').first().textContent();
    const remoteButton = await italianRemote.locator('.cue').first().elementHandle();
    await italianRemote.locator('#language-mode').selectOption('en');
    assert.equal(await italianRemote.locator('#connection').textContent(), 'Connected to host');
    assert.equal(await italianRemote.locator('.cue-title').first().textContent(), remoteLabel);
    assert(await remoteButton.evaluate(node => node === document.querySelector('.cue')), 'language changes retain cue button nodes');
    assert.equal(await italianRemote.evaluate(() => localStorage.getItem('smartstage.remote.language')), 'en');
    await italianRemote.reload(); await italianRemote.locator('#connection.live').waitFor();
    assert.equal(await italianRemote.locator('html').getAttribute('lang'), 'en');
    await italianRemote.locator('#language-mode').selectOption('system');
    assert.equal(await italianRemote.locator('html').getAttribute('lang'), 'it');
    assert.equal(await italianRemote.evaluate(() => localStorage.length), 0);
    for (const width of [320, 390, 768]) {
      await italianRemote.setViewportSize({width, height: 844});
      assert(await italianRemote.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `Italian remote horizontal overflow at ${width}`);
    }
    const bindingBaseline = await italianRemote.evaluate(() => window.smartStageI18n.bindingCount);
    for (let batch = 0; batch < 15; batch++) {
      await italianRemote.evaluate(() => { for (let i = 0; i < 20; i++) { renderCues(); } });
      await italianRemote.waitForTimeout(0);
    }
    assert(await italianRemote.evaluate(before => window.smartStageI18n.bindingCount <= before + 4, bindingBaseline), 'repeated state rendering cannot retain removed localized nodes indefinitely');
    assert.equal(requests.some(r => r.listenerRole === 'command' && r.path === '/api/language'), false, 'Remote never accesses the privileged language API');
    await italianRemoteContext.close();
    assert.deepEqual(errors, []);
    fs.writeFileSync(path.join(output, 'result.json'), JSON.stringify({ passed: true, visualCueToggleControls: true, audioAndVisualFadeSettings: true, playlistFilesBrowserRoundtrip: true, playlistFilesNativeCapabilityAndStatus: true, playlistFilesCancelInvalidAndRevisionProtection: true, playlistFilesDraftAndPlaybackGuards: true, playlistFilesEnglishItalian: true, playlistFilesNativeDialogExecutionVerified: false, adminHostSystemLanguage: true, englishItalianInPlacePreference: true, localizationPreservesUnsavedDraftsAndFocus: true, localizationNeverTranslatesMediaData: true, localeSelectionSendsNoPlaybackOrShowEdits: true, adminPreferencePersistedByHost: true, remoteLocaleIndependentAndLocalOnly: true, translatedWakeLockStatus: true, localizedBindingsBoundedAcrossStateUpdates: true, browser: await browser.version(), viewportWidths: [320, 390, 768, 844, 1280], remoteCuesDirectlyBelowHeader: true, remoteErrorsRemainVisible: true, adminQuitClosedState: true, adminAuthenticatedPresence: true, existingAdminTabReloadsOnceAfterRelaunch: true, nativeFilePickerCapabilityAndRequest: true, nativeFilePickerExecutionVerified: false, desktopFinderDropGuidance: true, desktopExplorerDropGuidance: true, windowsDesktopChooserAndImportFeedback: true, desktopCapabilitiesGateNativePicker: true, windowsBrowserChooserAndPathFallback: true, desktopSettingsCollapseAndQRPreserved: true, desktopMarkerGrantsNoFileAccess: true, desktopPlaylistAdditionFeedback: true, nativeWindowExecutionVerified: false, hostFilesPanelAndBackgroundBrowsingRemoved: true, nativeChooserImportUpdatesPlaylist: true, fallbackOriginalPathImportAndRetry: true, connectionSettingsCollapsedByDefault: true, connectionSettingsDisclosurePreservedDuringPolling: true, connectionQRAvailableWhileCollapsed: true, savedLANFirewallGuidanceVisibleWhileCollapsed: true, externalFileDropGuidance: true, remoteStageOutputControl: true, stageIndependentOfMusic: true, imageCueRetainsSelectedMusic: true, backgroundButtonsAndSelection: true, hiddenRemoteButtonsEditableInAdmin: true, stageSettingsSaveReloadAndRevisionConflict: true, fadeAndMusicToggleSettings: true, emergencyEscapeCommand: true, nativeBackgroundAndFadeExecutionVerified: false, compactHeaderDisconnect: true, cueColorSaveResetAndStateUpdates: true, cueColorContrastUnderCSP: true, adminAutomaticSession: true, remoteFragmentPairing: true, cookieResume: true, manualReconnect: true, remoteLinkRefresh: true, publicEndpointRelativeAssetsAndAPIs: true, publicFragmentPairingAndScopedCookieResume: true, publicRemoteAdminIsolation: true, gatewayDefaultAndLANRestartAcknowledgement: true, gatewaySettingsSecretClearingAndDraftPreservation: true, gatewayURLOnlySaveWithStoredToken: true, gatewayTokenReplacement: true, gatewayFirstSetupRequiresToken: true, gatewayPollingReconnectAndQRRefresh: true, gatewayNativeConnectionVerified: false, clipboardHTTPFallback: true, updatesAdminOnly: true, updateCheckRetry: true, automaticUpdateStartupReservation: true, updateStageAndPlaybackGuard: true, updateMutationGuard: true, updateRestartSessionAndQRRefresh: true, updateExecutionVerified: false, physicalPlaybackVerified: false, qrContentVerified: false }, null, 2));
    console.log('Browser checks passed: compact remote cues/errors, authenticated Admin presence/Quit/relaunch reload, Choose Media capability/import feedback, collapsed connection settings and visible QR, cue colors, stage controls, and update/authentication regressions.');
    await context.close();
  } finally { await browser.close(); for (const c of clients) c.end(); await Promise.all([adminServer, commandServer, publicServer].map(server => new Promise(resolve => server.close(resolve)))); }
})().catch(e => { console.error(e); process.exit(1); });
