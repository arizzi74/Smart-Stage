// UI contract checks inside the existing synthetic browser fixture. Native
// playback and mixing are exercised separately by the Go and OS probes.
const assert = require('node:assert/strict');

module.exports = async function audioLayersUI({ admin, page, state, config, broadcast }) {
  const flag = (name, position = 1) => admin.getByRole('checkbox', { name: `${name} for cue ${position}`, exact: true });
  const savedFlag = async (name, key, value) => {
    await flag(name).setChecked(value);
    await admin.waitForFunction(({ key, value }) => !playlistBusy && playlist.cues[0][key] === value, { key, value });
    assert.equal(config.cues[0][key], value);
  };
  assert.equal(await flag('Do not stop video').isVisible(), true);
  assert.equal(await flag('Mix audio').isVisible(), true);
  assert.equal(await flag('Do not stop video', 2).isVisible(), false, 'video cues do not expose audio-only flags');
  assert.equal(await flag('Mix audio', 2).isVisible(), false);
  await savedFlag('Do not stop video', 'keepVideo', true);
  await savedFlag('Mix audio', 'mixAudio', true);
  // Saving an unrelated property must keep both flags in the full cue edit.
  await admin.getByRole('checkbox', { name: 'Hide remote button for cue 1', exact: true }).check();
  await admin.waitForFunction(() => !playlistBusy && playlist.cues[0].hidden);
  assert.equal(config.cues[0].keepVideo, true); assert.equal(config.cues[0].mixAudio, true);
  await admin.getByRole('checkbox', { name: 'Hide remote button for cue 1', exact: true }).uncheck();
  await admin.waitForFunction(() => !playlistBusy && !playlist.cues[0].hidden);
  await admin.reload();
  await flag('Mix audio').waitFor();
  assert.equal(await flag('Do not stop video').isChecked(), true);
  assert.equal(await flag('Mix audio').isChecked(), true);
  await admin.locator('#language-mode').selectOption('it');
  await admin.waitForFunction(() => document.documentElement.lang === 'it' && !document.getElementById('language-mode').disabled);
  assert.equal(await admin.getByRole('checkbox', { name: 'Non interrompere il video per l’elemento 1', exact: true }).isChecked(), true);
  assert.equal(await admin.getByRole('checkbox', { name: 'Mixa l’audio dell’elemento 1', exact: true }).isChecked(), true);
  await admin.locator('#language-mode').selectOption('en');
  await admin.waitForFunction(() => document.documentElement.lang === 'en' && !document.getElementById('language-mode').disabled);

  const original = structuredClone(state);
  const identities = new Map([['cue-1', 101], ['cue-0', 102], ['cue-2', 103]]);
  for (const cue of state.cues) if (identities.has(cue.id)) Object.assign(cue, {
    selected: true, state: 'playing', paused: false, generation: identities.get(cue.id), transportRevision: 7, elapsed: 1, seekPending: false,
  });
  Object.assign(state, { activeCueId: 'cue-2', state: 'playing', generation: 103, transportRevision: 7, elapsed: 1, duration: 3, paused: false });
  state.revision++; broadcast();
  for (const id of identities.keys()) {
    await page.waitForFunction(id => document.querySelector(`[data-cue-id="${id}"]`)?.getAttribute('aria-pressed') === 'true', id);
  }
  const controls = [];
  const intercept = async route => {
    const body = route.request().postDataJSON(), cue = state.cues.find(item => item.id === body.cueId);
    assert(identities.has(body.cueId)); assert.equal(body.generation, identities.get(body.cueId));
    assert.equal(body.transportRevision, cue.transportRevision);
    assert.equal(body.instanceId, state.instanceId); assert.equal(body.stopEpoch, state.stopEpoch);
    assert(['pause', 'resume'].includes(body.action), 'selected layer buttons send a transport action rather than start another source');
    controls.push(body);
    cue.paused = body.action === 'pause'; cue.state = cue.paused ? 'paused' : 'playing'; cue.transportRevision++;
    Object.assign(state, { activeCueId: cue.id, generation: cue.generation, transportRevision: cue.transportRevision, paused: cue.paused, state: cue.state, elapsed: cue.elapsed });
    state.revision++; broadcast();
    await route.fulfill({ status: 202, contentType: 'application/json', body: JSON.stringify({ accepted: true }) });
  };
  await page.route('**/api/play', intercept);
  try {
    await page.locator('[data-cue-id="cue-0"]').click();
    await page.waitForFunction(() => document.querySelector('[data-cue-id="cue-0"]').classList.contains('paused') && !pendingTransport);
    assert.equal(state.cues.find(cue => cue.id === 'cue-1').paused, false, 'pausing mixed audio leaves video running');
    assert.equal(state.cues.find(cue => cue.id === 'cue-2').paused, false, 'pausing one layer leaves the other audio playing');
    assert.equal(state.activeCueId, 'cue-0', 'the transport panel follows the cue the operator just selected');
    await page.locator('[data-cue-id="cue-0"]').click();
    await page.waitForFunction(() => !document.querySelector('[data-cue-id="cue-0"]').classList.contains('paused') && !pendingTransport);
    assert.deepEqual(controls.map(value => value.action), ['pause', 'resume']);
    assert.deepEqual(controls.map(value => value.transportRevision), [7, 8]);
    const videoPosition = config.cues.findIndex(cue => cue.id === 'cue-1') + 1;
    await admin.waitForFunction(position => document.querySelectorAll('.playlist-row')[position - 1]?.querySelector('.cue-tools button')?.textContent === 'Pause', videoPosition);
  } finally {
    await page.unroute('**/api/play', intercept);
    const revision = state.revision + 1;
    for (const key of Object.keys(state)) delete state[key];
    Object.assign(state, original, { revision }); broadcast();
  }
  await savedFlag('Mix audio', 'mixAudio', false);
  await savedFlag('Do not stop video', 'keepVideo', false);
};
