// Section UI checks use the existing synthetic HTTP fixture. CDP sends real
// browser touch gestures; these checks do not claim native playback or hardware.
const assert = require('node:assert/strict');
const fs = require('node:fs');

module.exports = async function sectionsUI({ admin, page, state, config, broadcast, commands, requests, responseGate, setPlaylistWriteGate, exportedPlaylist }) {
  const originalConfig = structuredClone(config), originalState = structuredClone(state), viewport = page.viewportSize();
  const adminLanguage = await admin.locator('#language-mode').inputValue(), remoteLanguage = await page.locator('#language-mode').inputValue();
  const storage = await page.evaluate(() => ({ section: sessionStorage.getItem(`smartstage-section:${location.pathname}`), language: localStorage.getItem('smartstage.remote.language') }));
  const plays = () => commands.filter(item => item.path === '/api/play').length;
  const writes = () => requests.filter(item => item.path === '/api/playlist' && item.method === 'PUT');
  const settled = () => admin.waitForFunction(() => !playlistBusy && !sectionCollapseSending && !pendingSectionCollapses.size);
  const section = id => admin.locator(`details.playlist-section[data-section-id="${id}"]`);
  const tab = id => page.locator(`#remote-section-tabs [role=tab][data-section-id="${id}"]`);
  const selected = id => page.waitForFunction(id => document.querySelector(`#remote-section-tabs [data-section-id="${id}"]`)?.getAttribute('aria-selected') === 'true', id);
  const position = id => config.cues.findIndex(cue => cue.id === id) + 1;
  const rowSection = id => admin.getByRole('combobox', { name: `Section for cue ${position(id)}`, exact: true });
  const changeLanguage = async (target, value) => {
    await target.locator('#language-mode').selectOption(value);
    await target.waitForFunction(value => document.documentElement.lang === (value === 'system' ? 'en' : value) && !document.getElementById('language-mode').disabled, value);
  };
  const create = async title => {
    const before = config.sections.length;
    await admin.locator('#section-name').fill(title); await admin.locator('#add-section').click();
    await admin.waitForFunction(count => playlist.sections?.length === count && !playlistBusy, before + 1); await settled();
    return config.sections.find(item => item.title === title).id;
  };
  let gate, touch, testError;
  try {
    await settled();
    assert.equal(config.sections.length, 1); assert.equal(config.sections[0].id, 'main');
    assert.equal(await admin.locator('details.playlist-section').count(), 1);
    assert.equal(await admin.getByRole('button', { name: 'Remove section 1', exact: true }).isDisabled(), true, 'the final section cannot be deleted');
    const playCount = plays(), commandCount = commands.length;
    let second = await create('Act two'), empty = await create('Empty act');
    await admin.getByRole('textbox', { name: 'Name for section 2', exact: true }).fill('New section name');
    await admin.getByRole('textbox', { name: 'Name for section 2', exact: true }).press('Tab'); await settled();
    assert.equal(config.sections[1].title, 'New section name', 'section names are saved as user data');
    assert.equal(await section(empty).locator('.playlist-section-cues .playlist-row').count(), 0);
    assert.match(await section(empty).textContent(), /Move cues into this section/);

    for (const [name, key] of [['Do not stop video', 'keepVideo'], ['Mix audio', 'mixAudio']]) {
      await admin.getByRole('checkbox', { name: `${name} for cue 1`, exact: true }).check();
      await admin.waitForFunction(key => !playlistBusy && playlist.cues[0][key], key);
    }
    const level = admin.locator('#track-volume-cue-0');
    await level.fill('61'); await level.dispatchEvent('change');
    await admin.waitForFunction(() => playlist.cues.find(cue => cue.id === 'cue-0').volume === .61 && !playlistBusy);
    const playing = state.cues.find(cue => cue.id === 'cue-0');
    Object.assign(playing, { selected: true, state: 'playing', paused: false, generation: 501, transportRevision: 9, elapsed: 1, seekPending: false });
    Object.assign(state, { activeCueId: playing.id, state: 'playing', generation: 501, transportRevision: 9, elapsed: 1, duration: 3, paused: false });
    state.revision++; broadcast();
    const playbackIdentity = () => ({ activeCueId: state.activeCueId, generation: state.generation, transportRevision: state.transportRevision, stopEpoch: state.stopEpoch, elapsed: state.elapsed, paused: state.paused, stageEnabled: state.stageEnabled });
    const identity = playbackIdentity();
    await rowSection('cue-0').selectOption(second); await settled();
    const moved = config.cues.find(cue => cue.id === 'cue-0');
    assert.equal(moved.sectionId, second); assert.equal(moved.keepVideo, true); assert.equal(moved.mixAudio, true); assert.equal(moved.volume, .61);
    assert.equal(state.cues.find(cue => cue.id === 'cue-0').generation, 501, 'moving an active cue retains its source identity');
    assert.equal(await section(second).locator('.playlist-row').count(), 1);

    // A collapse save preserves the actual summary node and keyboard focus.
    const summary = section(second).locator('summary');
    await summary.focus(); const summaryNode = await summary.elementHandle(); await summary.press('Enter'); await admin.waitForFunction(id => playlist.sections.find(section => section.id === id)?.collapsed === true && !playlistBusy && !sectionCollapseSending && !pendingSectionCollapses.size, second);
    assert.equal(config.sections.find(item => item.id === second).collapsed, true);
    assert(await summaryNode.evaluate(node => node.isConnected && document.activeElement === node), 'collapse acknowledgement retains summary keyboard focus');
    await admin.reload(); await section(second).waitFor();
    assert.equal(await section(second).evaluate(node => node.open), false, 'saved collapse state survives reload');
    await section(second).locator('summary').click();
    await admin.waitForFunction(id => playlist.sections.find(section => section.id === id)?.collapsed === false && !playlistBusy && !sectionCollapseSending && !pendingSectionCollapses.size, second);
    const beforeQueue = writes().length;
    gate = responseGate(); setPlaylistWriteGate(gate);
    await section(second).locator('summary').click(); await gate.received;
    assert.equal(gate.snapshot.sections.find(section => section.id === second).collapsed, true, 'the held request is the first deliberate collapse target');
    await section(second).locator('summary').click();
    await admin.waitForFunction(id => playlistBusy && pendingSectionCollapses.get(id) === false, second);
    const draft = admin.getByRole('textbox', { name: `Label for cue ${position('cue-0')}`, exact: true });
    const draftNode = await draft.elementHandle(), savedLabel = moved.label;
    await draft.fill('Unsaved label while section collapse saves');
    gate.release(); gate = null; await settled();
    assert.equal(config.sections.find(item => item.id === second).collapsed, false, 'the last toggle queues behind the pending acknowledgement');
    assert.equal(writes().length, beforeQueue + 2, 'two distinct collapse targets save once each');
    assert.equal(await draft.inputValue(), 'Unsaved label while section collapse saves');
    assert(await draftNode.evaluate(node => node.isConnected && document.activeElement === node), 'queued collapse acknowledgements preserve a focused cue-label draft');
    assert.equal(config.cues.find(cue => cue.id === 'cue-0').label, savedLabel, 'collapse saving cannot silently commit an unrelated draft');
    await draft.fill(savedLabel); await draft.press('Tab'); await settled();

    // Omitted section fields are the older client's full-playlist edit form.
    const beforeWire = structuredClone(config.sections);
    await admin.evaluate(async () => {
      const cues = cueEdits().map(({ sectionId, ...cue }) => cue);
      acceptPlaylist(await api('PUT', '/api/playlist', { expectedRevision: playlist.playlistRevision, cues })); await refreshState();
    });
    assert.deepEqual(config.sections, beforeWire); assert.equal(config.cues.find(cue => cue.id === 'cue-0').sectionId, second);
    const beforeInvalid = config.playlistRevision;
    const invalid = await admin.evaluate(async () => {
      const cues = cueEdits(); cues[0].sectionId = 'missing-section';
      try { await api('PUT', '/api/playlist', { expectedRevision: playlist.playlistRevision, cues }); return ''; } catch (error) { return error.message; }
    });
    assert.match(invalid, /refer to a section/); assert.equal(config.playlistRevision, beforeInvalid, 'invalid references cannot partly save a section move');

    await page.waitForFunction(() => document.querySelectorAll('#remote-section-tabs [role=tab]').length === 3);
    await tab('main').tap(); await selected('main'); await page.evaluate(() => scrollTo(0, 0));
    const layout = await page.evaluate(() => {
      const tabs = document.getElementById('remote-section-tabs').getBoundingClientRect(), actions = document.querySelector('.transport-actions').getBoundingClientRect(), grid = document.getElementById('cue-grid').getBoundingClientRect();
      return { tabsBelowControls: tabs.top >= actions.bottom - 1, gridBelowTabs: grid.top >= tabs.bottom - 1 };
    });
    assert(layout.tabsBelowControls && layout.gridBelowTabs, 'remote tabs sit beneath transport controls and above cue buttons');
    assert.equal(await page.locator('[data-cue-id="cue-0"]').count(), 0, 'the active cue may remain selected on another tab');
    assert.equal(state.activeCueId, 'cue-0');
    await tab(second).tap(); await selected(second);
    assert.equal(await page.locator('[data-cue-id="cue-0"]').getAttribute('aria-pressed'), 'true');
    await tab(second).press('Home'); await selected('main');
    await tab('main').press('ArrowRight'); await selected(second);
    await tab(second).press('End'); await selected(empty);
    assert.equal(await page.locator('#cue-grid .cue').count(), 0); assert.equal(await page.locator('#empty-cues').isVisible(), true);
    await tab(empty).press('ArrowLeft'); await selected(second);

    touch = await page.context().newCDPSession(page);
    await page.evaluate(() => {
      window.__sectionTouches = [];
      window.__sectionTouchHandler = event => window.__sectionTouches.push({ pointerType: event.pointerType, trusted: event.isTrusted });
      document.getElementById('command-view').addEventListener('pointerdown', window.__sectionTouchHandler);
    });
    let pointerID = 70;
    const gestureStarts = [];
    const gesture = async ({ target, dx, dy = 0 }) => {
      await target.scrollIntoViewIfNeeded();
      // scrollIntoView can center a cue behind the sticky transport ribbon in
      // a short viewport. Place its top below that ribbon before hit testing.
      await target.evaluate(async element => {
        const ribbon = document.querySelector('header.transport').getBoundingClientRect();
        const box = element.getBoundingClientRect();
        if (innerHeight <= 400 || box.top < ribbon.bottom + 12) scrollBy(0, box.top - ribbon.bottom - 4);
        await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
      });
      const start = await target.evaluate((element, { dx }) => {
        const box = element.getBoundingClientRect(), area = document.getElementById('command-view');
        const bounds = area.getBoundingClientRect(), ribbon = document.querySelector('header.transport').getBoundingClientRect();
        const seek = document.getElementById('seek-bar'), seekTop = seek && !seek.hidden ? seek.getBoundingClientRect().top : innerHeight;
        const left = Math.max(8, box.left, bounds.left), right = Math.min(innerWidth - 8, box.right, bounds.right);
        const top = Math.max(8, box.top, bounds.top, ribbon.bottom + 8), bottom = Math.min(innerHeight - 20, box.bottom, bounds.bottom, seekTop - 4);
        const x = dx > 0 ? left + Math.min(30, (right - left) / 2) : right - Math.min(30, (right - left) / 2);
        const y = Math.max(top + 4, Math.min(bottom - 4, box.top + Math.min(box.height / 2, 80)));
        const hit = document.elementFromPoint(x, y);
        return { x, y, visible: right - left >= 16 && bottom - top >= 8,
          targetHit: Boolean(hit && element.contains(hit)), commandHit: Boolean(hit && area.contains(hit)),
          hitTag: hit?.tagName || '', hitID: hit?.id || '', headerBottom: ribbon.bottom, seekTop,
          targetTop: box.top, targetBottom: box.bottom, viewportHeight: innerHeight, scrollY };
      }, { dx });
      assert(start.visible && start.targetHit && start.commandHit && start.y > start.headerBottom,
        `Touch starts on its visible command target between the transport ribbon and seek bar: ${JSON.stringify(start)}`);
      const { x, y } = start, id = ++pointerID;
      gestureStarts.push({ id, dx, dy, ...start });
      await touch.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x, y, id }] });
      for (const part of [.25, .5, .75, 1]) {
        await touch.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [{ x: x + dx * part, y: y + dy * part, id }] });
        await page.waitForTimeout(25);
      }
      await touch.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
      await page.waitForTimeout(100);
      return start.scrollY;
    };
    const navPlays = plays();
    await gesture({ target: page.locator('[data-cue-id="cue-0"]'), dx: 115 }); await selected('main');
    await gesture({ target: page.locator('#cue-grid .cue').first(), dx: -115 }); await selected(second);
    await gesture({ target: page.locator('[data-cue-id="cue-0"]'), dx: -40 }); await selected(second);
    assert.equal(plays(), navPlays, 'a rejected short touch swipe over a selected cue cannot pause or restart it');
    await gesture({ target: page.locator('[data-cue-id="cue-0"]'), dx: -115 }); await selected(empty);
    await gesture({ target: page.locator('#command-view'), dx: 115 }); await selected(second);
    await tab('main').tap(); await selected('main');
    await page.setViewportSize({ width: 390, height: 360 }); await page.evaluate(() => scrollTo(0, 0));
    const scrollBefore = await gesture({ target: page.locator('#cue-grid .cue').first(), dx: 3, dy: -125 });
    await selected('main');
    assert(await page.evaluate(before => scrollY > before, scrollBefore), 'vertical touch drag scrolls the actual remote page');
    assert.equal(plays(), navPlays, 'tab, keyboard, horizontal touch and vertical scrolling send no PLAY request');
    const observedTouch = await page.evaluate(() => window.__sectionTouches);
    const touchDiagnostics = { expectedGestures: gestureStarts.length, observedCount: observedTouch.length,
      pointerTypes: [...new Set(observedTouch.map(event => event.pointerType))], trustedCount: observedTouch.filter(event => event.trusted).length, starts: gestureStarts };
    console.log('Section touch diagnostics:', JSON.stringify(touchDiagnostics));
    assert(observedTouch.length >= 6 && observedTouch.every(event => event.pointerType === 'touch' && event.trusted),
      `gestures exercise real browser touch pointer events: ${JSON.stringify(touchDiagnostics)}`);
    await page.setViewportSize(viewport); await page.evaluate(() => scrollTo(0, 0));
    assert.deepEqual(playbackIdentity(), identity, 'section navigation preserves the active transport and timeline');
    assert.equal(plays(), playCount, 'section CRUD, collapse, moves and navigation never start playback');
    assert.equal(commands.length, commandCount, 'section edits and navigation send no playback, stage or validation command');

    await changeLanguage(admin, 'it');
    assert.equal(await admin.getByRole('button', { name: 'Aggiungi sezione', exact: true }).isVisible(), true);
    assert.equal(await admin.getByRole('textbox', { name: 'Nome della sezione 2', exact: true }).inputValue(), 'New section name');
    assert.equal(await admin.getByRole('combobox', { name: `Sezione dell’elemento ${position('cue-0')}`, exact: true }).inputValue(), second);
    await changeLanguage(page, 'it');
    assert.equal(await tab(second).textContent(), 'New section name', 'user section titles matching translation keys are never translated');
    assert.equal(await tab('main').textContent(), 'Main', 'a saved Main title is user metadata');
    await changeLanguage(admin, 'en'); await changeLanguage(page, 'en');

    await section(empty).locator('summary').click();
    await admin.waitForFunction(id => playlist.sections.find(section => section.id === id)?.collapsed === true && !playlistBusy && !sectionCollapseSending, empty);
    assert.equal(await page.locator('#remote-section-tabs [role=tab]').count(), 3, 'desktop collapse does not remove a remote tab');
    // The portable file really passes through browser download/import UI with
    // multiple sections, so remapped references and metadata are both checked.
    const downloadEvent = admin.waitForEvent('download'); await admin.locator('#save-playlist-file').click();
    const download = await downloadEvent, document = JSON.parse(fs.readFileSync(await download.path(), 'utf8'));
    assert.deepEqual(document, exportedPlaylist()); assert.equal(document.sections.length, 3);
    await admin.locator('#stop').click(); await admin.waitForFunction(() => state.state === 'stopped');
    if (state.stageEnabled) { await page.locator('#remote-stage').click(); await page.waitForFunction(() => !state.stageEnabled); }
    await admin.locator('#load-playlist-file').click();
    const chooserEvent = admin.waitForEvent('filechooser'); await admin.locator('#playlist-load-continue').click();
    await (await chooserEvent).setFiles({ name: 'Sections.smartstage.json', mimeType: 'application/json', buffer: Buffer.from(JSON.stringify(document)) });
    await admin.waitForFunction(() => document.getElementById('playlist-file-message').textContent.startsWith('Playlist loaded.'));
    assert.deepEqual(config.sections.map(({ id, ...section }) => section), document.sections.map(({ id, ...section }) => section));
    const remapped = new Map(document.sections.map((section, index) => {
      assert.notEqual(config.sections[index].id, section.id, 'portable loading creates fresh section identities');
      return [section.id, config.sections[index].id];
    }));
    for (let i = 0; i < config.cues.length; i++) {
      assert.notEqual(config.cues[i].id, document.cues[i].id); assert.equal(config.cues[i].sectionId, remapped.get(document.cues[i].sectionId));
    }
    second = remapped.get(second); empty = remapped.get(empty);
    const importedCue = config.cues.find(cue => cue.sectionId === second), importedID = importedCue.id, first = config.sections[0].id;
    await admin.getByRole('button', { name: 'Remove section 2', exact: true }).click(); await settled();
    assert.equal(config.cues.find(cue => cue.id === importedID).sectionId, first, 'deleting a populated section moves its cues to the first remaining section');
    assert.equal(config.cues.find(cue => cue.id === importedID).keepVideo, true); assert.equal(config.cues.find(cue => cue.id === importedID).mixAudio, true); assert.equal(config.cues.find(cue => cue.id === importedID).volume, .61);
    assert.equal(config.sections.some(section => section.id === second), false);
    assert.equal(config.sections.find(section => section.id === empty).collapsed, true, 'portable load retains a true collapse flag');
    await section(empty).locator('summary').click();
    await admin.waitForFunction(id => playlist.sections.find(section => section.id === id)?.collapsed === false && !playlistBusy && !sectionCollapseSending, empty);
    await admin.getByRole('button', { name: 'Remove section 2', exact: true }).click(); await settled();
    assert.equal(config.sections.length, 1); assert.equal(await admin.getByRole('button', { name: 'Remove section 1', exact: true }).isDisabled(), true);

    // Truly legacy JSON has neither field; both clients provide a virtual Main
    // section without rewriting titles or inventing cue references in the UI.
    delete config.sections; delete state.sections;
    for (const cue of config.cues) delete cue.sectionId;
    for (const cue of state.cues) delete cue.sectionId;
    config.playlistRevision++; state.playlistRevision = config.playlistRevision; state.revision++; broadcast();
    await admin.reload(); await admin.locator('details.playlist-section[data-section-id="main"]').waitFor();
    await page.reload(); await page.locator('#connection.live').waitFor();
    assert.equal(await page.locator('#remote-section-tabs').isVisible(), false);
    assert.equal(await page.locator('#cue-grid .cue').count(), state.cues.filter(cue => !cue.hidden).length);

    return { sectionCreateRenameDeleteAndAtomicMoves: true, sectionMovesPreserveAudioFlagsVolumeAndSourceIdentity: true,
      sectionCollapsePersistedAndLatestQueuedToggle: true, sectionCollapsePreservesFocusAndCueDraft: true,
      sectionRemoteTabsKeyboardAndRealTouch: true, sectionEmptyTabAndOffTabActiveSelection: true,
      sectionPartialSwipeAndVerticalScrollNeverPlay: true, sectionNavigationPreservesPlayback: true,
      sectionEnglishItalianLeavesUserTitlesUntranslated: true, sectionPortableMetadataAndIdentityRemap: true,
      sectionLegacyMainFallback: true, sectionFixtureOnlyNativeExecutionVerified: false };
  } catch (error) {
    testError = error; throw error;
  } finally {
   try {
    gate?.release();
    if (touch) await touch.detach().catch(() => {});
    const configRevision = config.playlistRevision + 1, stateRevision = state.revision + 1;
    for (const key of Object.keys(config)) delete config[key]; Object.assign(config, originalConfig, { playlistRevision: configRevision });
    for (const key of Object.keys(state)) delete state[key]; Object.assign(state, originalState, { playlistRevision: configRevision, revision: stateRevision });
    broadcast();
    await page.setViewportSize(viewport);
    await admin.reload(); await admin.locator('.playlist-row').first().waitFor();
    if (await admin.locator('#language-mode').inputValue() !== adminLanguage) await changeLanguage(admin, adminLanguage);
    await page.reload(); await page.locator('#connection.live').waitFor();
    if (await page.locator('#language-mode').inputValue() !== remoteLanguage) await changeLanguage(page, remoteLanguage);
    await page.evaluate(storage => {
      const key = `smartstage-section:${location.pathname}`;
      if (storage.section === null) sessionStorage.removeItem(key); else sessionStorage.setItem(key, storage.section);
      if (storage.language === null) localStorage.removeItem('smartstage.remote.language'); else localStorage.setItem('smartstage.remote.language', storage.language);
    }, storage);
   } catch (cleanupError) {
    if (!testError) throw cleanupError;
    console.error('Section fixture cleanup failed:', cleanupError.message);
   }
  }
};
