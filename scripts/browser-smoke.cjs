// Browser-only tests with an explicitly synthetic HTTP fixture. Native playback
// is verified separately by native-smoke.py and application-smoke.py.
const { chromium } = require('playwright');
const http = require('node:http');
const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');

(async () => {
  const assets = path.resolve(__dirname, '../internal/web/assets');
  const output = path.resolve(__dirname, '../dist/browser-checks'); fs.mkdirSync(output, { recursive: true });
  const clients = new Set(), commands = [], errors = [];
  let role = '', stateGets = 0, holdPlay = false;
  const labels = ['Opening music', 'Welcome video with a deliberately long label that must wrap clearly', "Café's interlude", '<img src=x onerror="window.__xss=true">'];
  const state = { instanceId: 'browser-fixture', revision: 1, playlistRevision: 1, state: 'stopped', activeCueId: '', activePosition: 0, elapsed: 0, duration: 0, lastError: '', outputs: { audioId: 'default', displayId: 'screen', allowPrimary: true }, resolvedAudioId: '', stageEnabled: false, outputFault: false, generation: 1, stopEpoch: 1, validationJob: { running: false, completed: 4, total: 4 }, cues: labels.map((label, i) => ({ id: `cue-${i}`, label, position: i + 1, kind: i % 2 ? 'video' : 'audio', duration: 3, validation: 'ready' })) };
  const config = { schema: 1, playlistRevision: 1, outputs: state.outputs, cues: state.cues.map(c => ({ id: c.id, label: c.label, path: `/Host/Show/${c.id}.mp4`, cache: { status: 'ready', media: { kind: c.kind, duration: 3 } } })) };
  const broadcast = () => { for (const c of clients) c.write(`event: state\ndata: ${JSON.stringify(state)}\n\n`); };
  const server = http.createServer(async (req, res) => {
    const url = new URL(req.url, 'http://localhost');
    const reply = (value, status = 200) => { res.writeHead(status, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(value)); };
    if (!url.pathname.startsWith('/api/')) {
      const name = url.pathname.startsWith('/assets/') ? path.basename(url.pathname) : 'index.html';
      const type = name.endsWith('.js') ? 'text/javascript' : name.endsWith('.css') ? 'text/css' : 'text/html';
      res.writeHead(200, { 'Content-Type': type }); res.end(fs.readFileSync(path.join(assets, name))); return;
    }
    if (url.pathname === '/api/pair') { role = 'command'; reply({ role, csrfToken: 'test-csrf' }); return; }
    if (!role) { reply({ error: { message: 'Pair this browser first' } }, 401); return; }
    if (url.pathname === '/api/state') { stateGets++; reply({ role, csrfToken: 'test-csrf', state }); return; }
    if (url.pathname === '/api/events') { res.writeHead(200, { 'Content-Type': 'text/event-stream' }); clients.add(res); res.on('close', () => clients.delete(res)); broadcast(); return; }
    if (url.pathname === '/api/playlist') { reply(config); return; }
    if (url.pathname === '/api/devices') { reply({ audio: [{ id: 'speaker', name: 'USB Audio', default: true }], displays: [{ id: 'screen', name: 'Stage display', width: 1920, height: 1080, primary: true, mirrored: false }] }); return; }
    if (url.pathname === '/api/files') { reply({ path: '/Host/Show', roots: ['/Host/Show'], breadcrumbs: [{ name: 'Show', path: '/Host/Show' }], entries: [{ name: "Café's opening.mp4", path: "/Host/Show/Café's opening.mp4", directory: false, size: 1000000, modified: Date.now() * 1e6 }], truncated: false }); return; }
    let body = ''; for await (const chunk of req) body += chunk;
    commands.push({ path: url.pathname, body: body ? JSON.parse(body) : {} });
    if (url.pathname === '/api/play' && holdPlay) { await new Promise(resolve => setTimeout(resolve, 600)); }
    if (url.pathname === '/api/stop') { state.revision++; state.stopEpoch++; state.activeCueId = ''; state.state = 'stopped'; broadcast(); }
    reply({ accepted: true }, 202);
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const base = `http://127.0.0.1:${server.address().port}`;
  const browser = await chromium.launch({ headless: true });
  try {
    const context = await browser.newContext({ viewport: { width: 390, height: 844 }, hasTouch: true });
    const page = await context.newPage(); page.on('pageerror', e => errors.push(e.message));
    await page.goto(base + '/command');
    await page.locator('#pair-key').fill('test-command'); await page.getByRole('button', { name: 'Pair browser' }).click();
    await page.locator('#connection.live').waitFor();
    assert.equal(await page.locator('.cue').count(), 4);
    assert.equal(await page.evaluate(() => window.__xss), undefined);
    assert.equal(await page.locator('#cue-grid img').count(), 0);
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
    role = 'admin'; await page.setViewportSize({ width: 1280, height: 900 }); await page.goto(base + '/admin');
    await page.locator('.playlist-row').first().waitFor();
    assert.equal(await page.locator('.playlist-row').count(), 4);
    assert.equal(await page.locator('#playlist img').count(), 0);
    assert.equal(await page.locator('#audio-output option').count(), 2);
    await page.screenshot({ path: path.join(output, 'admin-desktop.png'), fullPage: true });
    await page.setViewportSize({ width: 390, height: 844 });
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'admin phone layout overflows');
    assert.deepEqual(errors, []);
    fs.writeFileSync(path.join(output, 'result.json'), JSON.stringify({ passed: true, browser: await browser.version(), viewportWidths: [320, 390, 768, 844, 1280], physicalPlaybackVerified: false }, null, 2));
    console.log('Browser checks passed: pairing, four cues, escaping, STOP visibility/pending/offline behavior, reconnect, and Admin layout.');
    await context.close();
  } finally { await browser.close(); for (const c of clients) c.end(); await new Promise(resolve => server.close(resolve)); }
})().catch(e => { console.error(e); process.exit(1); });
