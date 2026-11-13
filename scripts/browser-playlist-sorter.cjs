// Integrated Admin checks against the synthetic HTTP fixture. Mouse input is
// dispatched through Playwright, not through fabricated DOM drag events.
const assert = require('node:assert/strict');

module.exports = async function playlistSorterUI({ admin, page, state, config, broadcast, commands, requests, assetRequests, responseGate, setPlaylistWriteGate, setPlaylistWriteRace }) {
  const originalConfig = structuredClone(config), originalState = structuredClone(state);
  const language = await admin.locator('#language-mode').inputValue();
  const launcher = admin.locator('#sort-playlist'), dialog = admin.locator('#playlist-sorter');
  const cueHandle = id => dialog.locator(`.playlist-sorter-cue-handle[data-cue-id="${id}"]`);
  const sectionHandle = id => dialog.locator(`.playlist-sorter-section-handle[data-section-id="${id}"]`);
  const writes = () => requests.filter(item => item.path === '/api/playlist' && item.method === 'PUT');
  const assets = () => assetRequests.filter(item => /\/playlist-sorter\.(?:js|css)$/.test(item.path));
  const draft = () => dialog.locator('.playlist-sorter-section').evaluateAll(sections => sections.map(section => ({
    id: section.dataset.sectionId, cues: [...section.querySelectorAll('.playlist-sorter-cue')].map(cue => cue.dataset.cueId)
  })));
  const hostOrder = () => config.sections.map(section => ({ id: section.id, cues: config.cues.filter(cue => cue.sectionId === section.id).map(cue => cue.id) }));
  const identity = () => ({ activeCueId: state.activeCueId, generation: state.generation, transportRevision: state.transportRevision,
    stopEpoch: state.stopEpoch, elapsed: state.elapsed, paused: state.paused, stageEnabled: state.stageEnabled,
    sources: state.cues.filter(cue => cue.selected).map(({id, generation, transportRevision, paused, elapsed}) => ({id, generation, transportRevision, paused, elapsed})).sort((a, b) => a.id.localeCompare(b.id)) });
  const changeLanguage = async value => {
    await admin.locator('#language-mode').selectOption(value);
    await admin.waitForFunction(value => document.documentElement.lang === (value === 'system' ? 'en' : value) && !document.getElementById('language-mode').disabled, value);
  };
  const open = async () => { await launcher.click(); await dialog.waitFor({ state: 'visible' }); };
  const closed = () => admin.waitForFunction(() => !document.getElementById('playlist-sorter')?.open);
  let gate, testError;
  try {
    await admin.waitForFunction(() => !playlistBusy && !sectionCollapseSending && !pendingSectionCollapses.size);
    assert.equal(assets().length, 0, 'the sorter module and stylesheet are lazy before the first Admin open');
    const sections = [{ id: 'main', title: 'Main', collapsed: false }, { id: 'sort-second', title: 'Sort Playlist', collapsed: true }, { id: 'sort-empty', title: 'Empty act', collapsed: false }];
    config.sections = structuredClone(sections); state.sections = structuredClone(sections);
    for (let index = 0; index < config.cues.length; index++) {
      const cue = config.cues[index];
      Object.assign(cue, { sectionId: index < 3 ? 'main' : 'sort-second', color: cue.color || '', hidden: Boolean(cue.hidden), background: Boolean(cue.background), keepVideo: Boolean(cue.keepVideo), mixAudio: Boolean(cue.mixAudio), volume: cue.volume ?? .5 });
      if (index === 0) Object.assign(cue, { color: '#12abef', keepVideo: true, mixAudio: true, volume: .61 });
      if (index === 1) cue.background = true;
      if (index === 2) cue.hidden = true;
      const view = state.cues.find(item => item.id === cue.id);
      Object.assign(view, { ...cue, kind: cue.cache.media.kind, position: index + 1 }); delete view.path; delete view.cache;
    }
    const active = state.cues.find(cue => cue.id === 'cue-0');
    Object.assign(active, { selected: true, state: 'playing', paused: false, generation: 601, transportRevision: 12, elapsed: 1, seekPending: false });
    Object.assign(state, { activeCueId: active.id, state: 'playing', generation: 601, transportRevision: 12, elapsed: 1, duration: 3, paused: false });
    config.playlistRevision++; state.playlistRevision = config.playlistRevision; state.revision++; broadcast();
    await admin.reload(); await admin.waitForFunction(() => online && playlist?.sections?.length === 3 && !playlistBusy);
    assert.equal(await launcher.isEnabled(), true, 'the first loaded playlist enables Sort immediately, without waiting for a derived refresh or periodic tick');
    await changeLanguage('en');
    const initial = hostOrder(), playback = identity(), commandCount = commands.length, writeCount = writes().length;
    const metadata = new Map(config.cues.map(({sectionId, ...cue}) => [cue.id, structuredClone(cue)]));
    await open();
    assert.deepEqual(await draft(), initial);
    assert.equal(await dialog.locator('#playlist-sorter-save').isDisabled(), true, 'unchanged drafts cannot write');
    assert.equal(await dialog.locator('.playlist-sorter-empty').textContent(), 'Drop cues here');
    assert.equal(await dialog.locator('#playlist-sorter-heading').textContent(), 'Sort Playlist');
    assert.equal(await dialog.locator('img').count(), 0, 'cue names are rendered as text even when they contain HTML');
    assert.equal(assets().filter(item => item.path.endsWith('.js')).length, 1);
    assert.equal(assets().filter(item => item.path.endsWith('.css')).length, 1);
    assert(assets().every(item => item.listenerRole === 'admin'), 'only Admin fetches the sorter assets');
    assert(assets().some(item => item.path.endsWith('.js') && item.type === 'text/javascript'));
    assert(assets().some(item => item.path.endsWith('.css') && item.type === 'text/css'));

    let mouseDrags = 0, ghostSamples = 0;
    const drag = async (source, destination, fraction = .75) => {
      await source.scrollIntoViewIfNeeded();
      const start = await source.evaluate(element => {
        const box = element.getBoundingClientRect(), list = element.closest('.playlist-sorter-list').getBoundingClientRect();
        const top = Math.max(box.top, list.top), bottom = Math.min(box.bottom, list.bottom);
        const x = box.left + Math.min(60, box.width / 2), y = (top + bottom) / 2, hit = document.elementFromPoint(x, y);
        return {x, y, visible: bottom - top >= 8, hit: Boolean(hit && element.contains(hit))};
      });
      assert(start.visible && start.hit, `mouse drag starts on a visible sorting handle: ${JSON.stringify(start)}`);
      await admin.mouse.move(start.x, start.y); await admin.mouse.down();
      await admin.mouse.move(start.x + 16, start.y + 10, { steps: 3 });
      await dialog.locator('.playlist-sorter-ghost').waitFor();
      const first = await dialog.locator('.playlist-sorter-ghost').boundingBox();
      await admin.mouse.move(start.x + 39, start.y + 25, { steps: 3 });
      const second = await dialog.locator('.playlist-sorter-ghost').boundingBox();
      assert(Math.abs(second.x - first.x - 23) < 2 && Math.abs(second.y - first.y - 15) < 2,
        `the held ghost follows the actual mouse: ${JSON.stringify({first, second})}`);
      ghostSamples += 2;
      // Scroll the actual list while holding the pointer, then measure the
      // current drop geometry; no synthetic drop event chooses the result.
      await destination.scrollIntoViewIfNeeded();
      const box = await destination.boundingBox();
      const end = {x: box.x + Math.min(80, box.width / 2), y: box.y + box.height * fraction};
      await admin.mouse.move(end.x, end.y, { steps: 8 });
      assert.equal(await dialog.locator('.playlist-sorter-drop-marker').count(), 1);
      await admin.mouse.up(); mouseDrags++;
      await dialog.locator('.playlist-sorter-ghost').waitFor({ state: 'detached' });
      assert.equal(await dialog.locator('.playlist-sorter-drop-marker').count(), 0);
    };
    await drag(cueHandle('cue-0'), cueHandle('cue-1'));
    let expected = structuredClone(initial); expected[0].cues = ['cue-1', 'cue-0', 'cue-2'];
    assert.deepEqual(await draft(), expected, 'a held mouse drop reorders cues within a section');
    await drag(cueHandle('cue-0'), sectionHandle('sort-second'), .75);
    expected[0].cues = ['cue-1', 'cue-2']; expected[1].cues.unshift('cue-0');
    assert.deepEqual(await draft(), expected, 'a cue can move into another populated section');
    await drag(cueHandle('cue-2'), dialog.locator('.playlist-sorter-cues[data-section-id="sort-empty"]'), .5);
    expected[0].cues = ['cue-1']; expected[2].cues = ['cue-2'];
    assert.deepEqual(await draft(), expected, 'an empty section accepts an actual mouse drop');
    await drag(sectionHandle('sort-second'), sectionHandle('main'), .2);
    expected = [expected[1], expected[0], expected[2]];
    assert.deepEqual(await draft(), expected, 'whole-section dragging retains the cue groups');
    assert.deepEqual(hostOrder(), initial, 'draft dragging never writes to the host');
    assert.equal(writes().length, writeCount);
    await dialog.locator('#playlist-sorter-cancel').click(); await closed();
    assert.equal(await launcher.evaluate(node => document.activeElement === node), true, 'Cancel returns focus to the launcher');
    await open(); assert.deepEqual(await draft(), initial, 'Cancel discards every draft movement');

    await cueHandle('cue-0').focus(); await cueHandle('cue-0').press('Alt+ArrowDown');
    assert.deepEqual((await draft())[0].cues, ['cue-1', 'cue-0', 'cue-2']);
    assert.equal(await cueHandle('cue-0').evaluate(node => document.activeElement === node), true, 'keyboard reorder retains handle focus');
    await cueHandle('cue-0').press('Alt+ArrowRight');
    assert.equal((await draft())[1].cues.at(-1), 'cue-0', 'keyboard arrows can move a cue between sections');
    await admin.keyboard.press('Escape'); await closed();
    assert.equal(await launcher.evaluate(node => document.activeElement === node), true, 'Escape returns focus without sending emergency playback commands');
    await open(); assert.deepEqual(await draft(), initial, 'Escape discards keyboard draft changes');

    await cueHandle('cue-0').press('Alt+ArrowRight');
    await sectionHandle('sort-second').press('Alt+ArrowUp');
    expected = await draft();
    const beforeSave = writes().length;
    gate = responseGate(); setPlaylistWriteGate(gate);
    await dialog.locator('#playlist-sorter-save').click(); await gate.received;
    assert.equal(await dialog.locator('#playlist-sorter-save').isDisabled(), true);
    assert.equal(await dialog.locator('#playlist-sorter-cancel').isDisabled(), true);
    assert.equal(await dialog.getAttribute('aria-busy'), 'true');
    await admin.keyboard.press('Escape'); assert.equal(await dialog.isVisible(), true, 'a pending save cannot be dismissed or submitted again');
    assert.equal(writes().length, beforeSave + 1);
    gate.release(); gate = null; await closed();
    assert.deepEqual(hostOrder(), expected, 'Save persists section order and cue membership atomically');
    assert.equal(await launcher.evaluate(node => document.activeElement === node), true);
    assert.deepEqual(config.sections.map(({id, ...section}) => [id, section]), expected.map(group => {
      const {id, ...section} = sections.find(section => section.id === group.id); return [id, section];
    }), 'section title and collapsed metadata survive sorting');
    for (const {sectionId, ...cue} of config.cues) assert.deepEqual(cue, metadata.get(cue.id), `sorting preserves every metadata field for ${cue.id}`);
    assert.deepEqual(identity(), playback, 'sorting preserves the active source and timeline');
    await admin.reload(); await admin.waitForFunction(() => online && playlist?.sections?.[0]?.id === 'sort-second' && !playlistBusy);
    assert.equal(await launcher.isEnabled(), true, 'Sort is immediately available after reloading the saved order');
    assert.equal(await admin.locator('details.playlist-section[data-section-id="sort-second"]').evaluate(node => node.open), false, 'a collapsed section remains collapsed after saving and reloading');

    // A server-side race happens after the client has built its save request.
    await open(); await cueHandle('cue-0').press('Alt+ArrowLeft');
    // sort-second is first, so use an intra-section movement when at the edge.
    if (await dialog.locator('#playlist-sorter-save').isDisabled()) await cueHandle('cue-0').press('Alt+ArrowUp');
    const conflictDraft = await draft(), beforeConflict = writes().length;
    const changedID = config.cues.find(cue => cue.id !== 'cue-0').id;
    setPlaylistWriteRace(() => {
      config.cues.find(cue => cue.id === changedID).label = 'Host edit while sorting';
      state.cues.find(cue => cue.id === changedID).label = 'Host edit while sorting';
      config.playlistRevision++; state.playlistRevision = config.playlistRevision; state.revision++; broadcast();
    });
    await dialog.locator('#playlist-sorter-save').click();
    await dialog.locator('#playlist-sorter-error').waitFor({state: 'visible'});
    assert.match(await dialog.locator('#playlist-sorter-error').textContent(), /playlist changed|not saved|reload/i);
    assert.deepEqual(await draft(), conflictDraft, 'a rejected revision keeps the operator’s draft visible');
    assert.equal(writes().length, beforeConflict + 1);
    assert.equal(config.cues.find(cue => cue.id === changedID).label, 'Host edit while sorting', 'a stale modal cannot overwrite newer host metadata');
    assert.deepEqual(hostOrder(), expected, 'a rejected save does not partly reorder the host');
    await dialog.locator('#playlist-sorter-cancel').click(); await closed();

    await changeLanguage('it');
    assert.equal(await launcher.textContent(), 'Ordina playlist'); await open();
    assert.equal(await dialog.locator('#playlist-sorter-heading').textContent(), 'Ordina playlist');
    assert.equal(await dialog.locator('#playlist-sorter-save').textContent(), 'Salva ordine');
    assert.equal(await dialog.locator('#playlist-sorter-cancel').textContent(), 'Annulla');
    assert.equal(await dialog.locator('.playlist-sorter-empty').textContent(), 'Trascina qui gli elementi');
    assert.equal(await sectionHandle('sort-second').locator('.playlist-sorter-name').textContent(), 'Sort Playlist', 'user section names matching translation keys are not translated');
    assert.match(await sectionHandle('sort-second').getAttribute('aria-label'), /^Sposta la sezione Sort Playlist$/);
    await dialog.locator('#playlist-sorter-cancel').click(); await closed(); await changeLanguage('en');
    assert.equal(commands.length, commandCount, 'opening, dragging, save, conflict, Cancel, Escape and localization send no playback, stage or validation commands');
    assert.deepEqual(identity(), playback);
    console.log('Playlist sorter diagnostics:', JSON.stringify({mouseDrags, ghostSamples, atomicSaves: 1, rejectedRevisionSaves: 1, adminOnlyAssets: true}));
    return { playlistSorterImmediateAvailabilityAfterReload: true, playlistSorterLazyAdminOnlyModuleAndStyles: true, playlistSorterRealMouseWithinAcrossEmptyAndWholeSections: true,
      playlistSorterHeldGhostFollowsMouse: true, playlistSorterCancelEscapeAndKeyboardFocus: true,
      playlistSorterAtomicSavePreservesMetadataAndPlayback: true, playlistSorterPendingSaveGuard: true,
      playlistSorterStaleRevisionRetainsDraftAndHostEdit: true, playlistSorterEnglishItalianUserNamesUntranslated: true,
      playlistSorterFixtureOnlyNativeExecutionVerified: false };
  } catch (error) { testError = error; throw error;
  } finally {
    try {
      gate?.release(); setPlaylistWriteRace(null); setPlaylistWriteGate(null);
      const configRevision = config.playlistRevision + 1, stateRevision = state.revision + 1;
      for (const key of Object.keys(config)) delete config[key]; Object.assign(config, originalConfig, {playlistRevision: configRevision});
      for (const key of Object.keys(state)) delete state[key]; Object.assign(state, originalState, {playlistRevision: configRevision, revision: stateRevision});
      broadcast(); await admin.reload(); await admin.locator('.playlist-row').first().waitFor();
      if (await admin.locator('#language-mode').inputValue() !== language) await changeLanguage(language);
      await page.reload(); await page.locator('#connection.live').waitFor();
    } catch (cleanupError) { if (!testError) throw cleanupError; console.error('Sorter fixture cleanup failed:', cleanupError.message); }
  }
};
