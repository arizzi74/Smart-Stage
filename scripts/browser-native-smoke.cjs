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
  const mediaRoot = path.join(config, 'media');
  fs.cpSync(path.join(root, 'testdata', 'media'), mediaRoot, { recursive: true });
  fs.copyFileSync(path.join(mediaRoot, "Opening – café's tone.wav"), path.join(mediaRoot, '.hidden.wav'));
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
  const errors = [];
  let adminBase = '';
  let stderr = '', browser, admin, command;
  const application = spawn(exe, ['--port', '0', '--admin-port', '0', '--bind', '0.0.0.0', '--no-browser', '--config-dir', config,
    '--media-root', mediaRoot], { windowsHide: true, stdio: ['ignore', 'pipe', 'pipe'] });
  let exited = false, startError = null;
  application.on('error', error => { startError = error; });
  application.on('exit', () => { exited = true; });
  application.stderr.setEncoding('utf8');
  application.stderr.on('data', data => { stderr = (stderr + data).slice(-8192); });
  const lines = readline.createInterface({ input: application.stdout });
  lines.on('line', line => {
    const address = line.match(/^Admin:\s+(http:\/\/[^\s]+)\/admin$/);
    if (address) {
      const url = new URL(address[1]);
      assert.equal(url.hostname, '127.0.0.1', 'Admin must advertise only IPv4 localhost');
      adminBase = url.origin;
    }
  });
  try {
    await until(() => {
      if (startError) throw startError;
      if (exited) throw new Error(`Native application exited during startup: ${redact(stderr)}`);
      return adminBase;
    }, 'Native application did not print the local Admin URL');
    browser = await chromium.launch({ headless: true });
    record.browser = await browser.version();
    record.origin = 'Admin on 127.0.0.1; Command on an actual non-loopback host IPv4 address';
    const adminContext = await browser.newContext({ viewport: { width: 1280, height: 900 } });
    const commandContext = await browser.newContext({ viewport: { width: 390, height: 844 }, hasTouch: true });
    admin = await adminContext.newPage(); command = await commandContext.newPage();
    for (const page of [admin, command]) page.on('pageerror', error => errors.push(error.message));
    const apiPath = response => new URL(response.url()).pathname;
    const localSession = admin.waitForResponse(response => apiPath(response) === '/api/local-session');
    await admin.goto(adminBase + '/admin', { waitUntil: 'domcontentloaded' });
    assert.equal((await localSession).status(), 200);
    secrets.add((await (await localSession).json()).csrfToken);
    for (const cookie of await adminContext.cookies()) secrets.add(cookie.value);
    await admin.locator('#connection.live').waitFor();
    assert.equal(await admin.locator('#pairing').isVisible(), false);
    assert.equal(await admin.evaluate(() => window.isSecureContext), true);
    const remote = await (await admin.request.get(adminBase + '/api/remote-control')).json();
    assert.match(remote.token, /^\d{8}$/); secrets.add(remote.token);
    const link = remote.links.find(item => {
      const hostname = new URL(item.url).hostname;
      return net.isIPv4(hostname) && !hostname.startsWith('127.');
    });
    assert(link, 'A real non-loopback IPv4 remote link is required; do not substitute localhost');
    const remoteURL = new URL(link.url), commandBase = remoteURL.origin;
    assert.equal(remoteURL.hash, '#token=' + remote.token);
    assert.notEqual(remoteURL.port, new URL(adminBase).port);
    await admin.locator('#remote-ready').waitFor();
    if (remote.links.length > 1) await admin.locator('#remote-network').selectOption(link.url);
    assert.equal(await admin.locator('#remote-url').getAttribute('href'), link.url);
    await admin.waitForFunction(() => document.getElementById('remote-qr').naturalWidth > 100);
    assert.equal(await admin.locator('#remote-code').textContent(), remote.token);
    const lanAdminOpen = await new Promise(resolve => {
      const socket = net.connect({ host: remoteURL.hostname, port: Number(new URL(adminBase).port) });
      const done = result => { socket.destroy(); resolve(result); };
      socket.once('connect', () => done(true)); socket.once('error', () => done(false));
      socket.setTimeout(2000, () => done(false));
    });
    assert.equal(lanAdminOpen, false, 'Admin port must not accept a LAN connection');
    for (const denied of ['/admin', '/api/local-session']) {
      const response = await command.request.get(commandBase + denied);
      assert.equal(response.status(), 404, 'Remote listener must not expose Admin routes');
    }
    assert.equal((await command.request.get(commandBase + '/api/remote-control')).status(), 401);
    assert.equal((await command.request.get(commandBase + '/api/state')).status(), 401);
    record.checks.push('Local Admin opened without a pairing dialog, displayed remote URL/code/QR, and rejected LAN access');
    await admin.locator('#file-list .file-row').first().waitFor();
    assert.equal(await admin.locator('#admin-view').isVisible(), true);
    assert.equal(await admin.locator('#show-hidden').isChecked(), false, 'Hidden host files must default to off');
    assert.equal(await admin.getByRole('checkbox', { name: 'Select .hidden.wav', exact: true }).count(), 0);
    const hiddenListing = admin.waitForResponse(response => apiPath(response) === '/api/files' && new URL(response.url()).searchParams.get('showHidden') === 'true');
    await admin.locator('#show-hidden').check();
    assert.equal((await hiddenListing).status(), 200);
    assert((await (await hiddenListing).json()).entries.some(entry => entry.name === '.hidden.wav'), 'Real host listing must include the hidden fixture when requested');
    await admin.getByRole('checkbox', { name: 'Select .hidden.wav', exact: true }).waitFor();
    await admin.locator('#show-hidden').uncheck();
    await admin.getByRole('checkbox', { name: 'Select .hidden.wav', exact: true }).waitFor({ state: 'detached' });
    record.checks.push('Real host dotfile stayed hidden by default, appeared with Show hidden, and disappeared when disabled');
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
    const initial = await (await admin.request.get(adminBase + '/api/playlist')).json();
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
    const coloredCueID = saved.cues[0].id;
    await edit(() => admin.getByLabel('Color for cue 1', { exact: true }).evaluate(input => {
      input.value = '#fff000'; input.dispatchEvent(new Event('change', { bubbles: true }));
    }), saved.playlistRevision + 1);
    assert.equal(saved.cues.find(cue => cue.id === coloredCueID).color, '#fff000');
    const colorRoundtrip = await (await admin.request.get(adminBase + '/api/playlist')).json();
    assert.equal(colorRoundtrip.cues.find(cue => cue.id === coloredCueID).color, '#fff000', 'Saved color must survive a real playlist read');
    const labels = saved.cues.map(cue => cue.label);
    async function snapshot(page = admin) {
      const response = await page.request.get((page === admin ? adminBase : commandBase) + '/api/state');
      assert.equal(response.status(), 200);
      const result = await response.json(); secrets.add(result.csrfToken);
      return result.state;
    }
    await until(async () => (await snapshot()).cues.every(cue => cue.validation === 'ready'), 'Native cue validation did not finish', 90000);
    record.checks.push('Admin browser selected four real host files, saved four labels and reordered stable cue IDs');
    const devices = await (await admin.request.get(adminBase + '/api/devices')).json();
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
    const paired = command.waitForResponse(response => apiPath(response) === '/api/pair');
    await command.goto(link.url, { waitUntil: 'domcontentloaded' });
    const pairingResponse = await paired;
    assert.equal(pairingResponse.status(), 200);
    secrets.add((await pairingResponse.json()).csrfToken);
    for (const cookie of await commandContext.cookies()) secrets.add(cookie.value);
    await command.locator('#connection.live').waitFor();
    assert.equal(command.url(), commandBase + '/command', 'Pairing fragment must be removed from browser history');
    assert.equal(await command.evaluate(() => window.isSecureContext), false, 'Exercise plain LAN HTTP');
    assert.equal(await command.evaluate(() => typeof window.smartStageWakeLock?.setConnected), 'function', 'The real embedded wake-lock script must load');
    await command.waitForFunction(() => document.getElementById('keep-awake-status').textContent === 'Needs HTTPS');
    assert.equal(await command.locator('#keep-awake').isDisabled(), true, 'Plain LAN HTTP cannot offer a working screen wake lock');
    assert.match(await command.locator('#keep-awake-status').getAttribute('title'), /requires HTTPS/);
    record.checks.push('Embedded screen-wake-lock control truthfully reported Needs HTTPS and stayed disabled on real non-loopback HTTP');
    assert.equal(await command.locator('a[href$="/admin"]').count(), 0);
    assert.equal((await command.request.get(commandBase + '/api/remote-control')).status(), 403);
    await command.locator('.cue').nth(3).waitFor();
    assert.deepEqual(await command.locator('.cue-title').allTextContents(), labels);
    const controllerState = await snapshot(command);
    assert.equal(controllerState.cues.find(cue => cue.id === coloredCueID).color, '#fff000', 'Command state must carry the saved cue color');
    const coloredButton = command.locator('.cue').nth(saved.cues.findIndex(cue => cue.id === coloredCueID));
    await until(async () => await coloredButton.evaluate(node => getComputedStyle(node).backgroundColor === 'rgb(255, 240, 0)'), 'Remote cue did not apply its saved color under the actual CSP');
    assert.equal(await coloredButton.evaluate(node => getComputedStyle(node).color), 'rgb(0, 0, 0)', 'Bright custom cue colors need readable dark text');
    record.checks.push('Admin saved a cue color through the real API; playlist/command reads and remote CSSOM styling agreed under production CSP');
    const strings = value => typeof value === 'string' ? [value] : value && typeof value === 'object' ? Object.values(value).flatMap(strings) : [];
    for (const cue of saved.cues) assert(!strings(controllerState).includes(cue.path), 'Command state disclosed a host file path');
    assert.equal((await command.request.get(commandBase + '/api/files')).status(), 403);
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
    // Use the silent video so these real native stage controls do not depend
    // on an audio endpoint being installed on the CI machine.
    await command.locator('.cue').filter({ hasText: 'Finale' }).tap();
    await waitState('playing');
    await command.waitForFunction(() => document.getElementById('remote-stage').getAttribute('aria-pressed') === 'true');
    assert.equal(await command.locator('#remote-stage').isDisabled(), false, 'Stage off must remain available during playback');
    const stageOff = command.waitForResponse(response => apiPath(response) === '/api/stage-output' && response.request().method() === 'POST');
    await command.locator('#remote-stage').tap();
    assert.equal((await stageOff).status(), 202);
    assert.deepEqual((await stageOff).request().postDataJSON(), { enabled: false });
    await until(async () => { const state = await snapshot(); return state.state === 'stopped' && !state.stageEnabled; }, 'Remote Stage off did not stop playback and close the native stage');
    await command.waitForFunction(() => document.getElementById('remote-stage').getAttribute('aria-pressed') === 'false' && !document.getElementById('remote-stage').disabled);
    const stageOn = command.waitForResponse(response => apiPath(response) === '/api/stage-output' && response.request().method() === 'POST');
    await command.locator('#remote-stage').tap();
    assert.equal((await stageOn).status(), 202);
    assert.deepEqual((await stageOn).request().postDataJSON(), { enabled: true });
    await until(async () => (await snapshot()).stageEnabled, 'Remote Stage on did not open the native stage');
    const escapeOff = command.waitForResponse(response => apiPath(response) === '/api/stage-output' && response.request().method() === 'POST');
    await command.keyboard.press('Escape');
    assert.equal((await escapeOff).status(), 202);
    assert.deepEqual((await escapeOff).request().postDataJSON(), { enabled: false });
    await until(async () => { const state = await snapshot(); return state.state === 'stopped' && !state.stageEnabled; }, 'Browser Escape did not stop playback and close the native stage');
    record.checks.push('Remote Stage off stopped an actual silent video and closed the native stage; Stage on reopened it and browser Escape closed it again');
    await command.evaluate(() => scrollTo(0, document.body.scrollHeight));
    const bounds = await command.locator('#stop').boundingBox();
    assert(bounds && bounds.y >= 0 && bounds.y + bounds.height <= 844);
    assert(await command.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
    await command.screenshot({ path: path.join(output, 'command-phone.png'), fullPage: true });
    await admin.screenshot({ path: path.join(output, 'admin-desktop.png'), fullPage: true,
      mask: [admin.locator('#remote-url'), admin.locator('#remote-code'), admin.locator('#remote-qr')] });
    const allowed = new Set(['/command', '/assets/app.js', '/assets/style.css', '/assets/wake-lock.js', '/favicon.ico', '/api/pair', '/api/state', '/api/events', '/api/play', '/api/stop', '/api/stage-output']);
    for (const request of requests) {
      const url = new URL(request.url);
      assert.equal(url.origin, commandBase);
      assert.equal(url.hash, '', 'Pairing fragment must not be transmitted in HTTP requests');
      assert(allowed.has(url.pathname), `Unexpected browser resource: ${url.pathname}`);
      assert.notEqual(request.type, 'media');
    }
    assert.deepEqual(errors, []);
    record.checks.push('Command URL paired automatically on plain LAN HTTP, cleared its fragment, rendered the saved order and sent one PLAY per tap');
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
      if (page) try { await page.screenshot({ path: path.join(output, `${name}-failure.png`), fullPage: true,
        mask: name === 'admin' ? [page.locator('#remote-url'), page.locator('#remote-code'), page.locator('#remote-qr')] : [] }); } catch {}
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
