// Real browser -> published application -> native OS playback. No mock routes,
// media elements, fake backend, stored credentials, HAR or trace recordings.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const net = require('node:net');
const readline = require('node:readline');
const zlib = require('node:zlib');
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

function dedicatedWindowsAdmin(platform, release) {
  if (platform !== 'windows') return false; // This scenario starts the portable Mac executable.
  if (release === 'dev') return true;
  const version = release.match(/^v?(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$/);
  assert(version, `Unrecognized Windows release: ${release}`);
  const core = version.slice(1, 4).map(Number), baseline = [0, 1, 0];
  for (let index = 0; index < core.length; index++) {
    if (core[index] !== baseline[index]) return core[index] > baseline[index];
  }
  const preview = (version[4] || '').match(/^preview\.(\d+)(?:\.[0-9A-Za-z.-]+)?$/);
  return preview ? Number(preview[1]) >= 17 : !version[4] || version[4].split('.')[0] > 'preview';
}

(async () => {
  const exe = path.resolve(process.argv[2] || '');
  assert(fs.statSync(exe).isFile(), 'Pass a real supported-OS Smart Stage executable');
  const root = path.resolve(__dirname, '..');
  const config = fs.mkdtempSync(path.join(os.tmpdir(), 'smartstage-browser-'));
  // Native LAN coverage explicitly opts in; new installations default to gateway.
  fs.writeFileSync(path.join(config, 'gateway.json'), JSON.stringify({ mode: 'lan' }), { mode: 0o600 });
  const mediaRoot = path.join(config, 'media');
  fs.cpSync(path.join(root, 'testdata', 'media'), mediaRoot, { recursive: true });

  // Long native sound keeps the new scene checks independent of UI/decoder
  // startup timing. The PNG is a real bounded image with valid chunk CRCs.
  const sceneMusicFilename = 'Scene music 60s.wav', sceneImageFilename = 'Scene image.png';
  const samples = 16000 * 60, wave = Buffer.alloc(44 + samples * 2);
  wave.write('RIFF', 0); wave.writeUInt32LE(wave.length - 8, 4); wave.write('WAVEfmt ', 8);
  wave.writeUInt32LE(16, 16); wave.writeUInt16LE(1, 20); wave.writeUInt16LE(1, 22);
  wave.writeUInt32LE(16000, 24); wave.writeUInt32LE(32000, 28);
  wave.writeUInt16LE(2, 32); wave.writeUInt16LE(16, 34); wave.write('data', 36); wave.writeUInt32LE(samples * 2, 40);
  for (let i = 0; i < samples; ++i) wave.writeInt16LE(Math.round(3000 * Math.sin(2 * Math.PI * 440 * i / 16000)), 44 + i * 2);
  fs.writeFileSync(path.join(mediaRoot, sceneMusicFilename), wave);
  function pngChunk(kind, data) {
    const type = Buffer.from(kind), body = Buffer.concat([type, data]);
    let crc = 0xffffffff;
    for (const byte of body) { crc ^= byte; for (let bit = 0; bit < 8; ++bit) crc = (crc >>> 1) ^ ((crc & 1) ? 0xedb88320 : 0); }
    const size = Buffer.alloc(4), checksum = Buffer.alloc(4);
    size.writeUInt32BE(data.length); checksum.writeUInt32BE((crc ^ 0xffffffff) >>> 0);
    return Buffer.concat([size, body, checksum]);
  }
  const imageHeader = Buffer.alloc(13); imageHeader.writeUInt32BE(64, 0); imageHeader.writeUInt32BE(64, 4);
  imageHeader[8] = 8; imageHeader[9] = 2;
  const imagePixels = Buffer.alloc((64 * 3 + 1) * 64);
  for (let y = 0; y < 64; ++y) for (let x = 0; x < 64; ++x) imagePixels[y * 193 + 1 + x * 3] = 255;
  fs.writeFileSync(path.join(mediaRoot, sceneImageFilename), Buffer.concat([
    Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]), pngChunk('IHDR', imageHeader),
    pngChunk('IDAT', zlib.deflateSync(imagePixels)), pngChunk('IEND', Buffer.alloc(0))]));

  const version = spawnSync(exe, ['--version'], { encoding: 'utf8' });
  assert.equal(version.status, 0, 'Published executable must report its version');
  const target = version.stdout.match(/, (darwin|windows)\/(arm64|amd64)/);
  assert(target, 'Expected native application platform/version metadata');
  const release = version.stdout.match(/^Smart Stage (\S+) \(/);
  assert(release, 'Expected Smart Stage release metadata');
  const nativeAdmin = dedicatedWindowsAdmin(target[1], release[1]);
  if (process.env.SMARTSTAGE_VERSION) assert(version.stdout.includes(`Smart Stage ${process.env.SMARTSTAGE_VERSION} (`));
  const output = path.join(root, 'dist', 'browser-native', `${target[1]}-${target[2]}`);
  fs.mkdirSync(output, { recursive: true });
  const record = { passed: false, application: version.stdout.trim(), platform: target[1],
    architecture: target[2], node: process.version, nodeArchitecture: process.arch,
    physicalRoutingVerified: false, checks: [] };
  const errors = [];
  let adminBase = '';
  let stderr = '', browser, admin, command;
  let application, lines, exited = false, startError = null, exitCode = null, launchLog = '';
  function startApplication(ports = ['--port', '0', '--admin-port', '0', '--no-browser']) {
    exited = false; startError = null; exitCode = null; launchLog = ''; stderr = '';
    application = spawn(exe, [...ports, '--bind', '0.0.0.0', '--no-auto-update', '--config-dir', config,
      '--media-root', mediaRoot], { windowsHide: true, stdio: ['ignore', 'pipe', 'pipe'] });
    application.on('error', error => { startError = error; });
    application.on('exit', code => { exited = true; exitCode = code; });
    application.stderr.setEncoding('utf8');
    application.stderr.on('data', data => {
      stderr = (stderr + data).slice(-8192);
      launchLog = (launchLog + data).slice(-16384);
    });
    lines = readline.createInterface({ input: application.stdout });
    lines.on('line', line => {
      launchLog = (launchLog + line + '\n').slice(-16384);
      const address = line.match(/^Admin:\s+(http:\/\/[^\s]+)\/admin$/);
      if (address) {
        const url = new URL(address[1]);
        assert.equal(url.hostname, '127.0.0.1', 'Admin must advertise only IPv4 localhost');
        adminBase = url.origin;
      }
    });
  }
  startApplication();
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
    const adminRequests = [];
    admin.on('request', request => adminRequests.push(new URL(request.url()).pathname));
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
    assert.equal(await admin.locator('#admin-view').isVisible(), true);
    assert.equal(await admin.locator('#files-section, a[href="#files-section"]').count(), 0);
    assert.equal(await admin.locator('#remote-connection-settings').evaluate(node => node.open), false);
    await admin.evaluate(() => loadGateway());
    assert.equal(await admin.locator('#remote-connection-settings').evaluate(node => node.open), false, 'status polling keeps settings collapsed');
    assert.equal(await admin.locator('#remote-url').isVisible(), true);
    assert.equal(await admin.locator('#remote-qr').isVisible(), true);
    assert.equal(await admin.locator('#lan-firewall-guidance').isVisible(), true, 'saved LAN firewall guidance remains visible outside settings');
    assert.equal(adminRequests.some(p => ['/api/files', '/api/inspect'].includes(p)), false);
    record.checks.push('Admin has no Host files browser; connection settings stay collapsed while LAN link, QR and firewall guidance remain accessible');
    const filenames = ["Opening – café's tone.wav", 'silent-1080p.mp4', 'tone.mp3', 'video-aac-1080p.mp4'];
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
    const importMethods = new Set();
    async function importMedia(filenames, expectedRevision) {
      const paths = filenames.map(filename => path.join(mediaRoot, filename));
      if (await admin.locator('#media-path-settings').isVisible()) {
        if (!await admin.locator('#media-path-settings').evaluate(node => node.open)) await admin.locator('#media-path-settings > summary').click();
        await admin.locator('#media-paths').fill(paths.map(value => '"' + value + '"').join('\n'));
        await edit(() => admin.locator('#add-media-paths').click(), expectedRevision);
        assert.equal(await admin.locator('#media-paths').inputValue(), '');
        await admin.locator('#media-path-settings > summary').click();
        importMethods.add('Original-path fallback UI');
      } else {
        // The desktop app exposes a native chooser, whose OS dialog cannot be
        // selected by Playwright. Seed this playback scenario through the
        // authenticated API; separate native Admin probes exercise OS file drops.
        assert.equal(await admin.locator('#choose-files').isVisible(), true);
        assert.equal(await admin.locator('#choose-files').textContent(), 'Choose Media…');
        await edit(() => admin.evaluate(async paths => {
          const current = await api('GET', '/api/playlist');
          await api('PUT', '/api/playlist', { expectedRevision: current.playlistRevision,
            cues: [...current.cues.map(({ id, label, path, color, hidden, background }) => ({ id, label, path, color, hidden, background })), ...paths.map(path => ({ id: '', label: '', path }))] });
          await loadPlaylist(); await refreshState();
        }, paths), expectedRevision);
        importMethods.add('Authenticated API fixture setup; native chooser is separate');
      }
      for (const original of paths) {
        const source = fs.statSync(original, { bigint: true });
        assert(saved.cues.some(cue => { const imported = fs.statSync(cue.path, { bigint: true }); return imported.dev === source.dev && imported.ino === source.ino; }), 'Imported fixture must reference the original file, including canonical Windows path aliases');
      }
    }
    const initial = await (await admin.request.get(adminBase + '/api/playlist')).json();
    await importMedia(filenames, initial.playlistRevision + 1);
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
    record.checks.push('Four real media fixtures were imported without copying; Admin saved four labels and reordered stable cue IDs');
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
    assert.equal(await command.locator('#page-title').isVisible(), false, 'Remote must not show the title block above cue buttons');
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
      if (cue.path.endsWith('.mp4')) {
        assert.match(await button.locator('.cue-meta').textContent(), /Press again to stop/);
        await button.tap();
      } else await command.locator('#stop').tap();
      await waitState('stopped');
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
    await until(async () => { const state = await snapshot(); return state.state === 'playing' && !state.stageEnabled; }, 'Remote Stage off interrupted playback or failed to close the native stage');
    await command.waitForFunction(() => document.getElementById('remote-stage').getAttribute('aria-pressed') === 'false' && !document.getElementById('remote-stage').disabled);
    const stageOn = command.waitForResponse(response => apiPath(response) === '/api/stage-output' && response.request().method() === 'POST');
    await command.locator('#remote-stage').tap();
    assert.equal((await stageOn).status(), 202);
    assert.deepEqual((await stageOn).request().postDataJSON(), { enabled: true });
    await until(async () => (await snapshot()).stageEnabled, 'Remote Stage on did not open the native stage');
    const escapeOff = command.waitForResponse(response => apiPath(response) === '/api/emergency-stop' && response.request().method() === 'POST');
    await command.keyboard.press('Escape');
    assert.equal((await escapeOff).status(), 202);
    assert.equal(typeof (await escapeOff).request().postDataJSON().requestId, 'string');
    await until(async () => { const state = await snapshot(); return state.state === 'stopped' && !state.stageEnabled; }, 'Browser Escape did not stop playback and close the native stage');
    record.checks.push('Remote Stage off preserved native video playback while hiding the stage; Stage on reopened it and emergency Escape stopped and hid it');

    // Dedicated scene integration: browser controls, persisted settings, real
    // media inspection and authoritative native playback events. Native probes
    // separately measure renderer gains; this page test never claims speakers.
    await importMedia([sceneMusicFilename, sceneImageFilename], saved.playlistRevision + 1);
    const musicCue = saved.cues.find(cue => path.basename(cue.path) === sceneMusicFilename);
    const imageCue = saved.cues.find(cue => path.basename(cue.path) === sceneImageFilename);
    const backgroundCue = saved.cues.find(cue => path.basename(cue.path) === 'video-aac-1080p.mp4');
    assert(musicCue && imageCue && backgroundCue);
    await until(async () => {
      const current = await snapshot();
      return current.cues.find(cue => cue.id === imageCue.id)?.kind === 'image' &&
        current.cues.filter(cue => [musicCue.id, imageCue.id].includes(cue.id)).every(cue => cue.validation === 'ready');
    }, 'Native audio/image inspection did not validate scene fixtures', 90000);
    const backgroundIndex = saved.cues.findIndex(cue => cue.id === backgroundCue.id) + 1;
    const imageIndex = saved.cues.findIndex(cue => cue.id === imageCue.id) + 1;
    await edit(() => admin.getByRole('checkbox', { name: `Use cue ${backgroundIndex} as a background button`, exact: true }).check(), saved.playlistRevision + 1);
    await edit(() => admin.getByRole('checkbox', { name: `Hide remote button for cue ${backgroundIndex}`, exact: true }).check(), saved.playlistRevision + 1);
    await command.locator(`[data-cue-id="${backgroundCue.id}"]`).waitFor({ state: 'detached' });
    assert.equal(await admin.locator('.playlist-row').count(), saved.cues.length, 'Hidden remote cues must remain in Admin');
    assert.equal(saved.cues.find(cue => cue.id === backgroundCue.id).background, true);
    assert.equal(saved.cues.find(cue => cue.id === backgroundCue.id).hidden, true);
    assert.equal(await admin.locator('#fade-seconds').inputValue(), '1', 'Default fade duration must be one second');
    async function saveStageSettings() {
      const response = admin.waitForResponse(item => apiPath(item) === '/api/stage-settings' && item.request().method() === 'PUT');
      await admin.locator('#save-stage-settings').click();
      const result = await response;
      assert.equal(result.status(), 200, 'Stage settings must save through the actual Admin API');
      saved = await result.json();
      await admin.waitForFunction(revision => document.getElementById('playlist-revision').textContent.endsWith(`revision ${revision}`), saved.playlistRevision);
      await until(async () => (await snapshot()).playlistRevision === saved.playlistRevision, 'Saved scene settings did not reach live state');
    }
    await admin.locator('#background-cue').selectOption(backgroundCue.id);
    await admin.locator('#background-audio').setChecked(Boolean(audio));
    await admin.locator('#fade-enabled').check();
    await admin.locator('#fade-seconds').fill('1.2');
    await admin.locator('#toggle-audio').check();
    await saveStageSettings();
    assert.deepEqual(saved.stage, { backgroundCueId: backgroundCue.id, backgroundAudio: Boolean(audio), fadeEnabled: true, fadeSeconds: 1.2, toggleAudio: true });
    await admin.reload({ waitUntil: 'domcontentloaded' });
    await admin.locator('#connection.live').waitFor();
    await admin.locator('.playlist-row').last().waitFor();
    assert.equal(await admin.locator('#background-cue').inputValue(), backgroundCue.id);
    assert.equal(await admin.locator('#background-audio').isChecked(), Boolean(audio));
    assert.equal(await admin.locator('#fade-seconds').inputValue(), '1.2');
    assert.equal(await admin.locator('#toggle-audio').isChecked(), true);
    assert.equal(await admin.getByRole('checkbox', { name: `Hide remote button for cue ${backgroundIndex}`, exact: true }).isChecked(), true);
    await command.locator('#remote-stage').tap();
    await until(async () => {
      const state = await snapshot(); return state.stageEnabled && state.backgroundCueId === backgroundCue.id && !state.backgroundError;
    }, 'Stage did not enable the configured video background');
    const videoButton = command.locator('.cue').filter({ hasText: 'Finale' });
    await videoButton.tap(); await waitState('playing');
    await videoButton.tap(); await waitState('stopped');
    const videoToggledOff = await snapshot();
    assert.equal(videoToggledOff.activeCueId, '');
    assert.equal(videoToggledOff.stageEnabled, true);
    assert.equal(videoToggledOff.backgroundCueId, backgroundCue.id);
    record.checks.push('A second press stopped the selected native video with Stage still on, returning to black or the configured background without changing the background selection');
    const musicButton = command.locator(`[data-cue-id="${musicCue.id}"]`);
    const imageButton = command.locator(`[data-cue-id="${imageCue.id}"]`);
    async function assertIndependentStage(expected, source) {
      await command.waitForFunction(enabled => document.getElementById('remote-stage').getAttribute('aria-pressed') === String(!enabled) && !document.getElementById('remote-stage').disabled, expected);
      await command.locator('#remote-stage').tap();
      await until(async () => {
        const state = await snapshot();
        if (source && (state.activeCueId !== source.activeCueId || state.generation !== source.generation || state.state !== 'playing'))
          throw new Error('Stage toggle interrupted or restarted the selected music');
        return state.stageEnabled === expected;
      }, `Stage did not become ${expected ? 'enabled' : 'disabled'} independently`);
    }
    if (audio) {
      await musicButton.tap(); await waitState('playing');
      const musicPlaying = await snapshot();
      assert.equal(musicPlaying.activeCueId, musicCue.id);
      await imageButton.tap();
      await until(async () => {
        const state = await snapshot();
        return state.imageCueId === imageCue.id && state.activeCueId === musicCue.id && state.generation === musicPlaying.generation && state.state === 'playing';
      }, 'Image cue did not preserve the selected music and playback generation');
      await command.waitForFunction(ids => ids.every(id => document.querySelector(`[data-cue-id="${id}"]`)?.getAttribute('aria-pressed') === 'true'), [imageCue.id, musicCue.id]);
      await imageButton.tap();
      await until(async () => {
        const state = await snapshot();
        return state.imageCueId === '' && state.stageEnabled && state.activeCueId === musicCue.id && state.generation === musicPlaying.generation && state.state === 'playing';
      }, 'A second image press did not clear only the image while preserving music');
      await imageButton.tap();
      await until(async () => (await snapshot()).imageCueId === imageCue.id, 'Image did not reappear after being toggled off');
      record.checks.push('A second image press cleared its overlay while independent native music kept its identity and generation');
      const beforeStageToggle = await snapshot();
      await assertIndependentStage(false, beforeStageToggle);
      await until(async () => (await snapshot()).elapsed > beforeStageToggle.elapsed + .2, 'Native music timeline stopped while stage was off');
      await assertIndependentStage(true, beforeStageToggle);
      assert.equal((await snapshot()).imageCueId, imageCue.id, 'Stage reopening must preserve the selected image');
      await musicButton.tap(); await waitState('stopped');
      const toggledOff = await snapshot();
      assert.equal(toggledOff.activeCueId, ''); assert.equal(toggledOff.imageCueId, imageCue.id);
      assert.equal(toggledOff.stageEnabled, true); assert.equal(toggledOff.backgroundCueId, backgroundCue.id);
      await command.waitForFunction(id => document.querySelector(`[data-cue-id="${id}"]`)?.getAttribute('aria-pressed') === 'true', imageCue.id);
      await musicButton.tap(); await waitState('playing');
      await until(async () => (await snapshot()).imageCueId === '', 'Starting a music button did not return visual output to the background');
      await imageButton.tap();
      await until(async () => (await snapshot()).imageCueId === imageCue.id, 'Image did not become active again');
      await command.locator('#stop').tap(); await waitState('stopped');
      const stoppedScene = await snapshot();
      assert.equal(stoppedScene.imageCueId, ''); assert.equal(stoppedScene.activeCueId, '');
      assert.equal(stoppedScene.backgroundCueId, backgroundCue.id); assert.equal(stoppedScene.stageEnabled, true);
      record.checks.push('Real native music kept its cue ID, generation and advancing timeline through image selection and Stage off/on; pressing the music button again stopped only music and kept the image; STOP cleared the image and returned to background');
    } else {
      await imageButton.tap();
      await until(async () => (await snapshot()).imageCueId === imageCue.id, 'Native image cue did not become active');
      await imageButton.tap();
      await until(async () => { const state = await snapshot(); return state.imageCueId === '' && state.stageEnabled; }, 'A second image press did not return to background with Stage on');
      await imageButton.tap();
      await until(async () => (await snapshot()).imageCueId === imageCue.id, 'Native image did not reappear after toggling off');
      await assertIndependentStage(false); await assertIndependentStage(true);
      assert.equal((await snapshot()).imageCueId, imageCue.id);
      await command.locator('#stop').tap(); await waitState('stopped');
      assert.equal((await snapshot()).imageCueId, '');
      record.checks.push('Real native image selection, independent stage hiding/restoration, and image-only STOP passed; music/background soundtrack integration was unavailable because the runner has no audio endpoint');
    }
    await edit(() => admin.getByRole('checkbox', { name: `Use cue ${imageIndex} as a background button`, exact: true }).check(), saved.playlistRevision + 1);
    let backgroundMusic;
    if (audio) { await musicButton.tap(); await waitState('playing'); backgroundMusic = await snapshot(); }
    await imageButton.tap();
    await until(async () => (await snapshot()).backgroundCueId === imageCue.id, 'Image background button did not select the current background');
    const backgroundSwitched = await snapshot();
    assert.equal(backgroundSwitched.imageCueId, '');
    assert.equal(backgroundSwitched.stage.backgroundCueId, backgroundCue.id, 'A session background button must not overwrite the saved default');
    if (audio) {
      assert.equal(backgroundSwitched.activeCueId, musicCue.id); assert.equal(backgroundSwitched.generation, backgroundMusic.generation);
      assert.equal(backgroundSwitched.state, 'playing');
    }
    await command.locator('#stop').tap(); await waitState('stopped');
    await admin.locator('#background-cue').selectOption(imageCue.id);
    await admin.locator('#background-audio').uncheck();
    await saveStageSettings();
    assert.equal(saved.stage.backgroundCueId, imageCue.id);
    assert.equal(saved.cues.find(cue => cue.id === backgroundCue.id).hidden, true);
    assert.equal(saved.cues.find(cue => cue.id === imageCue.id).background, true);
    record.sceneIntegration = { nativeImageInspected: true, savedVideoAndImageBackgrounds: true,
      hiddenBackgroundCueEditableInAdmin: true, persistedConfigReloaded: true, configuredFadeSeconds: 1.2,
      musicImageAndIndependentStage: Boolean(audio), backgroundSoundtrackEnabled: Boolean(audio),
      selectedMusicToggleKeepsImage: Boolean(audio), nativeAudioGainsMeasuredHere: false,
      audioUnavailableReason: audio ? '' : 'No enumerated native audio endpoint' };
    record.checks.push('Admin saved/reloaded fade, background soundtrack and music-toggle settings; hidden background video stayed editable; image background button changed only the session background and kept music when an audio endpoint was available');

    await command.evaluate(() => scrollTo(0, document.body.scrollHeight));
    const bounds = await command.locator('#stop').boundingBox();
    assert(bounds && bounds.y >= 0 && bounds.y + bounds.height <= 844);
    assert(await command.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
    await command.screenshot({ path: path.join(output, 'command-phone.png'), fullPage: true });
    await admin.screenshot({ path: path.join(output, 'admin-desktop.png'), fullPage: true,
      mask: [admin.locator('#remote-url'), admin.locator('#remote-code'), admin.locator('#remote-qr')] });
    const allowed = new Set(['/command', '/assets/app.js', '/assets/style.css', '/assets/wake-lock.js', '/favicon.ico', '/api/pair', '/api/state', '/api/events', '/api/play', '/api/stop', '/api/stage-output', '/api/emergency-stop']);
    for (const request of requests) {
      const url = new URL(request.url);
      assert.equal(url.origin, commandBase);
      assert.equal(url.hash, '', 'Pairing fragment must not be transmitted in HTTP requests');
      assert(allowed.has(url.pathname), `Unexpected browser resource: ${url.pathname}`);
      assert.notEqual(request.type, 'media');
    }
    assert.equal(adminRequests.some(p => ['/api/files', '/api/inspect'].includes(p)), false, 'Admin never starts hidden folder browsing');
    assert.equal(await admin.locator('#remote-connection-settings').evaluate(node => node.open), false);
    record.fixtureImportMethods = [...importMethods];
    record.nativeChooserDialogExecutionVerified = false;
    assert.deepEqual(errors, []);
    record.checks.push('Command URL paired automatically on plain LAN HTTP, cleared its fragment, rendered the saved order and sent one PLAY per tap');
    record.checks.push('Real native playing/STOP/natural completion reached the browser over SSE; no media transferred to browser');
    record.cuesNativelyPlayedAndStopped = played;
    record.audioEndpoints = devices.audio.length; record.displays = devices.displays.length;
    record.audioSkipped = !audio;
    // Quit through the real Admin page while native video is playing, then keep
    // that same tab alive across a normal relaunch on the same addresses.
    await command.locator('.cue').filter({ hasText: 'Finale' }).tap();
    await waitState('playing');
    const oldInstance = (await snapshot()).instanceId;
    const originalURL = admin.url();
    const originalAdminPort = new URL(adminBase).port;
    async function quitFromAdmin() {
      const accepted = admin.waitForResponse(response => apiPath(response) === '/api/quit');
      await admin.getByRole('button', { name: 'Quit Smart Stage', exact: true }).click();
      assert.equal((await accepted).status(), 202);
      assert.deepEqual(await (await accepted).json(), { quitting: true });
      await admin.locator('#app-closed').waitFor();
      await until(() => exited, 'Admin Quit did not shut down the native host', 15000);
      assert.equal(exitCode, 0, 'Admin Quit must exit cleanly');
      for (const port of [originalAdminPort, remoteURL.port]) {
        const listening = await new Promise(resolve => {
          const socket = net.connect({ host: '127.0.0.1', port: Number(port) });
          const done = result => { socket.destroy(); resolve(result); };
          socket.once('connect', () => done(true)); socket.once('error', () => done(false));
          socket.setTimeout(2000, () => done(false));
        });
        assert.equal(listening, false, 'Quit must release both listeners');
      }
      lines.close();
    }
    await quitFromAdmin();
    record.checks.push('Admin Quit during real native video playback exited successfully and closed both HTTP listeners');
    let reloads = 0;
    admin.on('framenavigated', frame => { if (frame === admin.mainFrame()) reloads++; });
    startApplication(['--port', remoteURL.port, '--admin-port', originalAdminPort]);
    await until(() => {
      if (startError) throw startError;
      if (exited) throw new Error(`Relaunched host exited: ${redact(stderr)}`);
      return nativeAdmin
        ? launchLog.includes('Requested the dedicated Admin window') && launchLog.includes('Loaded native Admin page')
        : launchLog.includes('Reusing the existing Admin browser page');
    }, nativeAdmin ? 'Relaunch did not load the dedicated Windows Admin window' : 'Relaunch did not detect and reuse the existing Admin tab', 30000);
    await admin.locator('#connection.live').waitFor();
    await admin.locator('#quit-app:not([disabled])').waitFor();
    await until(async () => (await snapshot()).instanceId !== oldInstance, 'Old Admin tab did not acquire the new host instance');
    await sleep(6500);
    assert.equal(reloads, 1, 'An existing Admin tab must reload new assets exactly once');
    assert.equal(admin.url(), originalURL, 'Reused Admin tab must retain its URL');
    assert.equal(adminContext.pages().length, 1, 'Admin context must retain one page');
    assert(!/(?:Opened|Reopened) Admin in the system browser/.test(launchLog), 'Relaunch must not dispatch another system browser page');
    assert.equal((await snapshot()).state, 'stopped', 'Relaunch must never resume playback');
    assert.equal((await snapshot()).stageEnabled, false, 'Relaunch must leave the native stage closed');
    assert.deepEqual(errors, []);
    record.relaunchAdminMode = nativeAdmin ? 'Dedicated Windows app window' : 'Reused system-browser Admin page';
    record.existingBrowserTabReconnectedAfterRelaunch = true;
    record.externalBrowserDispatchedOnRelaunch = false;
    if (nativeAdmin) {
      record.dedicatedAdminPageLoadedOnRelaunch = true;
      record.checks.push('Windows relaunch loaded its dedicated Admin window; the existing external Admin tab also reconnected, loaded new assets exactly once, retained its URL and stayed the only page without an OS browser dispatch');
    } else {
      record.checks.push('Same Admin tab reconnected after relaunch, loaded new assets once, and suppressed automatic OS browser dispatch');
    }
    await quitFromAdmin();
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
