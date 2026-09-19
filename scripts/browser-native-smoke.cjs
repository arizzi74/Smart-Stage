// Real browser -> published application -> native OS playback. No mock routes,
// media elements, fake backend, stored credentials, HAR or trace recordings.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const net = require('node:net');
const readline = require('node:readline');
const { spawn, spawnSync } = require('node:child_process');
const { chromium } = require('./browser/node_modules/playwright');

const secrets = new Set();
const redact = value => {
  let text = String(value);
  for (const secret of secrets) if (secret) text = text.split(secret).join('[redacted]');
  return text;
};
const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));
async function until(check, message, timeout = 30000) {
  const end = Date.now() + timeout;
  while (Date.now() < end) {
    const result = await check();
    if (result) return result;
    await sleep(50);
  }
  throw new Error(message);
}

(async () => {
  const exe = path.resolve(process.argv[2] || '');
  assert(fs.statSync(exe).isFile(), 'Pass a real supported-OS Smart Stage executable');
  const root = path.resolve(__dirname, '..');
  const config = fs.mkdtempSync(path.join(os.tmpdir(), 'smartstage-browser-'));
  const probe = net.createServer();
  await new Promise((resolve, reject) => { probe.once('error', reject); probe.listen(0, '0.0.0.0', resolve); });
  const port = probe.address().port;
  await new Promise(resolve => probe.close(resolve));
  const version = spawnSync(exe, ['--version'], { encoding: 'utf8' });
  assert.equal(version.status, 0, 'Published executable must report its version');
  const target = version.stdout.match(/, (darwin|windows)\/(arm64|amd64)/);
  assert(target, 'Expected native application platform/version metadata');
  if (process.env.SMARTSTAGE_VERSION) assert(version.stdout.includes(`Smart Stage ${process.env.SMARTSTAGE_VERSION} (`));
  const output = path.join(root, 'dist', 'browser-native', `${target[1]}-${target[2]}`);
  fs.mkdirSync(output, { recursive: true });
  const record = { passed: false, application: version.stdout.trim(), platform: target[1],
    architecture: target[2], node: process.version, nodeArchitecture: process.arch,
    physicalRoutingVerified: false, checks: [] };
  const keys = {}, origins = [], errors = [];
  let stderr = '', browser, admin, command;
  const application = spawn(exe, ['--port', String(port), '--bind', '0.0.0.0', '--config-dir', config,
    '--media-root', path.join(root, 'testdata', 'media')], { windowsHide: true, stdio: ['ignore', 'pipe', 'pipe'] });
  let exited = false, startError = null;
  application.on('error', error => { startError = error; });
  application.on('exit', () => { exited = true; });
  application.stderr.setEncoding('utf8');
  application.stderr.on('data', data => { stderr = (stderr + data).slice(-8192); });
  const lines = readline.createInterface({ input: application.stdout });
  lines.on('line', line => {
    const key = line.match(/^(Admin|Command) pairing key:\s+(\S+)/);
    if (key) { keys[key[1]] = key[2]; secrets.add(key[2]); }
    const address = line.match(/^Admin:\s+(http:\/\/[^\s]+)\/admin$/);
    if (address) {
      const url = new URL(address[1]);
      if (net.isIPv4(url.hostname) && !url.hostname.startsWith('127.')) origins.push(url.origin);
    }
  });
  try {
    await until(() => {
      if (startError) throw startError;
      if (exited) throw new Error(`Native application exited during startup: ${redact(stderr)}`);
      return keys.Admin && keys.Command;
    }, 'Native application did not print pairing keys');
    assert.notEqual(keys.Admin, keys.Command);
    assert(origins.length > 0, 'A real non-loopback IPv4 address is required; do not substitute localhost');
    const base = origins[0];
    browser = await chromium.launch({ headless: true });
    record.browser = await browser.version();
    record.origin = 'HTTP on an actual non-loopback host IPv4 address';
    const adminContext = await browser.newContext({ viewport: { width: 1280, height: 900 } });
    const commandContext = await browser.newContext({ viewport: { width: 390, height: 844 }, hasTouch: true });
    admin = await adminContext.newPage(); command = await commandContext.newPage();
    for (const page of [admin, command]) page.on('pageerror', error => errors.push(error.message));
    const apiPath = response => new URL(response.url()).pathname;
    async function pair(page, context, route, key) {
      await page.goto(base + route, { waitUntil: 'domcontentloaded' });
      await page.locator('#pair-key').fill(key);
      const paired = page.waitForResponse(response => apiPath(response) === '/api/pair');
      await page.getByRole('button', { name: 'Pair browser' }).click();
      const response = await paired;
      assert.equal(response.status(), 200);
      secrets.add((await response.json()).csrfToken);
      for (const cookie of await context.cookies()) secrets.add(cookie.value);
      await page.locator('#connection.live').waitFor();
      assert.equal(await page.evaluate(() => window.isSecureContext), false, 'Exercise plain LAN HTTP, not a trusted localhost origin');
    }
    await pair(admin, adminContext, '/admin', keys.Admin);
    await admin.locator('#file-list .file-row').first().waitFor();
    assert.equal(await admin.locator('#admin-view').isVisible(), true);
    const filenames = ["Opening – café's tone.wav", 'silent-1080p.mp4', 'tone.mp3', 'video-aac-1080p.mp4'];
    for (const filename of filenames) await admin.getByRole('checkbox', { name: `Select ${filename}`, exact: true }).check();
    let saved;
    async function edit(action, expectedRevision) {
      const changed = admin.waitForResponse(response => apiPath(response) === '/api/playlist' && response.request().method() === 'PUT');
      const refreshed = admin.waitForResponse(async response => {
        if (apiPath(response) !== '/api/state' || response.status() !== 200) return false;
        return (await response.json()).state.playlistRevision >= expectedRevision;
      });
      await action();
      const response = await changed;
      assert.equal(response.status(), 200, 'UI playlist edit must save successfully');
      saved = await response.json();
      assert.equal(saved.playlistRevision, expectedRevision);
      await refreshed;
      await admin.waitForFunction(revision => document.getElementById('playlist-revision').textContent.endsWith(`revision ${revision}`), expectedRevision);
    }
    const initial = await (await admin.request.get(base + '/api/playlist')).json();
    await edit(() => admin.locator('#add-files').click(), initial.playlistRevision + 1);
    assert.equal(saved.cues.length, 4);
    const names = new Map([[filenames[0], 'Opening music'], [filenames[1], 'Finale'],
      [filenames[2], 'Interlude'], [filenames[3], 'Welcome video']]);
    for (let i = 0; i < saved.cues.length; ++i) {
      const label = names.get(path.basename(saved.cues[i].path));
      assert(label);
      await edit(async () => {
        const field = admin.getByRole('textbox', { name: `Label for cue ${i + 1}`, exact: true });
        await field.fill(label); await field.press('Tab');
      }, saved.playlistRevision + 1);
    }
    const before = saved.cues.map(cue => cue.id);
    await edit(() => admin.getByRole('button', { name: 'Move cue 4 up', exact: true }).click(), saved.playlistRevision + 1);
    assert.deepEqual(saved.cues.map(cue => cue.id), [before[0], before[1], before[3], before[2]]);
    const labels = saved.cues.map(cue => cue.label);
    async function snapshot(page = admin) {
      const response = await page.request.get(base + '/api/state');
      assert.equal(response.status(), 200);
      const result = await response.json(); secrets.add(result.csrfToken);
      return result.state;
    }
    await until(async () => (await snapshot()).cues.every(cue => cue.validation === 'ready'), 'Native cue validation did not finish', 90000);
    record.checks.push('Admin browser selected four real host files, saved four labels and reordered stable cue IDs');
    const devices = await (await admin.request.get(base + '/api/devices')).json();
    const audio = devices.audio.find(device => !device.default) || devices.audio[0];
    const display = devices.displays.find(device => !device.primary) || devices.displays[0];
    assert(display, 'This native browser scenario requires an actual enumerated display');
    await admin.locator('#audio-output').selectOption(audio ? audio.id : 'default');
    await admin.locator('#display-output').selectOption(display.id);
    await admin.locator('#allow-primary').check();
    const outputs = admin.waitForResponse(response => apiPath(response) === '/api/outputs' && response.request().method() === 'PUT');
    await admin.getByRole('button', { name: 'Save outputs', exact: true }).click();
    assert.equal((await outputs).status(), 200);
    await until(async () => (await snapshot()).state === 'stopped', 'Output configuration did not stop/disarm stage');
    assert.equal((await snapshot()).stageEnabled, false);

    const requests = [];
    command.on('request', request => requests.push({ url: request.url(), type: request.resourceType() }));
    await pair(command, commandContext, '/command', keys.Command);
    await command.locator('.cue').nth(3).waitFor();
    assert.deepEqual(await command.locator('.cue-title').allTextContents(), labels);
    const controllerState = await snapshot(command);
    const strings = value => typeof value === 'string' ? [value] : value && typeof value === 'object' ? Object.values(value).flatMap(strings) : [];
    for (const cue of saved.cues) assert(!strings(controllerState).includes(cue.path), 'Command state disclosed a host file path');
    assert.equal((await command.request.get(base + '/api/files')).status(), 403);
    assert.equal(await command.locator('audio,video').count(), 0);
    const waitState = value => command.waitForFunction(expected => document.getElementById('play-state').textContent === expected, value, { polling: 20, timeout: 30000 });
    let played = 0;
    for (const cue of saved.cues) {
      const silent = path.basename(cue.path) === 'silent-1080p.mp4';
      if (!silent && !audio) continue;
      const button = command.locator('.cue').filter({ has: command.locator('.cue-title', { hasText: cue.label }) });
      const previous = requests.filter(request => new URL(request.url).pathname === '/api/play').length;
      const previouslyEnabled = (await snapshot()).stageEnabled;
      await button.tap(); await waitState('playing');
      assert.equal(await button.getAttribute('aria-pressed'), 'true');
      assert.equal(requests.filter(request => new URL(request.url).pathname === '/api/play').length, previous + 1);
      await command.locator('#stop').tap(); await waitState('stopped');
      const stopped = await snapshot();
      assert.equal(stopped.activeCueId, '');
      assert.equal(stopped.stageEnabled, previouslyEnabled || cue.path.endsWith('.mp4'));
      played++;
    }
    if (!audio) {
      await command.locator('.cue').filter({ hasText: 'Opening music' }).tap();
      await waitState('error');
      assert.equal(await command.locator('#playback-error').isVisible(), true);
      await command.locator('#stop').tap(); await waitState('stopped');
      record.checks.push('Missing native audio output produced a visible recoverable error; STOP remained usable');
    }
    // Native completion must be visible through the real SSE connection and
    // must leave the video stage enabled without advancing to another cue.
    await command.locator('.cue').filter({ hasText: 'Finale' }).tap();
    await waitState('playing'); await waitState('stopped');
    const ended = await snapshot();
    assert.equal(ended.activeCueId, ''); assert.equal(ended.stageEnabled, true);
    await command.evaluate(() => scrollTo(0, document.body.scrollHeight));
    const bounds = await command.locator('#stop').boundingBox();
    assert(bounds && bounds.y >= 0 && bounds.y + bounds.height <= 844);
    assert(await command.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
    await command.screenshot({ path: path.join(output, 'command-phone.png'), fullPage: true });
    await admin.screenshot({ path: path.join(output, 'admin-desktop.png'), fullPage: true });
    const allowed = new Set(['/command', '/assets/app.js', '/assets/style.css', '/favicon.ico', '/api/pair', '/api/state', '/api/events', '/api/play', '/api/stop']);
    for (const request of requests) {
      const url = new URL(request.url);
      assert.equal(url.origin, base);
      assert(allowed.has(url.pathname), `Unexpected browser resource: ${url.pathname}`);
      assert.notEqual(request.type, 'media');
    }
    assert.deepEqual(errors, []);
    record.checks.push('Command paired separately on plain HTTP, rendered the saved order, and sent one PLAY per tap');
    record.checks.push('Real native playing/STOP/natural completion reached the browser over SSE; no media transferred to browser');
    record.cuesNativelyPlayedAndStopped = played;
    record.audioEndpoints = devices.audio.length; record.displays = devices.displays.length;
    record.audioSkipped = !audio;
    // Deliberately release stage output before test cleanup. Windows child
    // termination below is not evidence of graceful application shutdown.
    await admin.getByRole('button', { name: 'Disable stage output', exact: true }).click();
    await until(async () => !(await snapshot()).stageEnabled, 'Stage disable did not complete');
    record.passed = true;
    console.log(`Real browser/native checks passed: ${played} playable cues; audio endpoints=${devices.audio.length}. Physical routing remains unverified.`);
  } catch (error) {
    record.error = redact(error.stack || error);
    for (const [name, page] of [['admin', admin], ['command', command]]) {
      if (page) try { await page.screenshot({ path: path.join(output, `${name}-failure.png`), fullPage: true }); } catch {}
    }
    throw error;
  } finally {
    if (browser) await browser.close();
    if (!exited) application.kill(process.platform === 'win32' ? 'SIGKILL' : 'SIGINT');
    await until(() => exited || startError, 'Application did not exit after browser test', 10000).catch(() => application.kill('SIGKILL'));
    lines.close();
    fs.writeFileSync(path.join(output, 'result.json'), JSON.stringify(record, null, 2));
    fs.rmSync(config, { recursive: true, force: true });
  }
})().catch(error => { console.error(redact(error.stack || error)); process.exitCode = 1; });
