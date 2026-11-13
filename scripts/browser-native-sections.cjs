// Real Admin controls and remote navigation against the native application.
// No mocked routes, synthetic backend, stored tokens or physical output claim.
const assert = require('node:assert/strict');
const {isDeepStrictEqual} = require('node:util');

module.exports = async function nativeSections({admin, command, adminBase, snapshot, until,
  apiPath, record, audio, musicCue}) {
  const report = record.sectionsIntegration = {
    status: 'running', audioContinuityVerified: false, physicalRoutingVerified: false,
    method: 'Real Admin section edits, authenticated portable export, remote tab controls and browser mouse sorting through the native host; local-only module delivery and native source/clock continuity checked within their stated scope.'
  };
  if (!audio) report.audioUnavailableReason = 'No enumerated native audio endpoint';
  const initial = await snapshot();
  const readPlaylist = async () => {
    const response = await admin.request.get(adminBase + '/api/playlist');
    assert.equal(response.status(), 200, 'Real host playlist must remain available');
    return response.json();
  };
  const original = await readPlaylist();
  assert(original.sections?.length > 0, 'Native host must publish saved playlist sections');
  assert(original.sections.length < 50, 'Section scenario requires one free section slot');
  const editCues = cues => cues.map(({id, label, path, sectionId, color, hidden, background, keepVideo, mixAudio, volume}) =>
    ({id, label, path, sectionId, color: color || '', hidden: Boolean(hidden), background: Boolean(background),
      keepVideo: Boolean(keepVideo), mixAudio: Boolean(mixAudio), ...(volume === undefined ? {} : {volume})}));
  const originalMusic = original.cues.find(cue => cue.id === musicCue.id);
  assert(originalMusic && !originalMusic.hidden && !originalMusic.background, 'Expected a visible ordinary music fixture');
  const cueState = current => {
    const cue = current.cues.find(cue => cue.id === musicCue.id);
    assert(cue, 'Music fixture must remain in authoritative host state');
    return cue;
  };
  const stopAll = async () => {
    const [response] = await Promise.all([
      command.waitForResponse(item => apiPath(item) === '/api/stop' && item.request().method() === 'POST'),
      command.locator('#stop').tap()
    ]);
    assert.equal(response.status(), 202, 'Remote STOP must reach the real host');
    return until(async () => {
      const current = await snapshot();
      return current.state === 'stopped' && current.cues.every(cue => !cue.selected) && current;
    }, 'Section checks could not clear the native playback selections');
  };
  const edit = async action => {
    const [response] = await Promise.all([
      admin.waitForResponse(item => apiPath(item) === '/api/playlist' && item.request().method() === 'PUT'),
      Promise.resolve().then(action)
    ]);
    assert.equal(response.status(), 200, 'Section changes must save through actual Admin controls');
    const saved = await response.json();
    await admin.waitForFunction(revision => !playlistBusy && !sectionCollapseSending &&
      playlist?.playlistRevision === revision, saved.playlistRevision);
    return saved;
  };
  const playbackRequests = [];
  const observeRequest = request => {
    if (request.method() !== 'POST') return;
    const pathname = new URL(request.url()).pathname;
    if (['/api/play', '/api/stop', '/api/emergency-stop', '/api/seek', '/api/stage-output'].includes(pathname)) {
      // Retain only fixed endpoint names, never payloads, tokens or host URLs.
      playbackRequests.push(pathname);
    }
  };
  for (const page of [admin, command]) page.on('request', observeRequest);
  let restored, scenarioFailure;
  try {
    await stopAll();
    await admin.locator('#section-name').fill('Native section check');
    let configured = await edit(() => admin.locator('#add-section').click());
    const created = configured.sections.find(section => !original.sections.some(item => item.id === section.id));
    assert(created, 'Create section must return a new persisted section identity');
    assert.equal(configured.sections.length, original.sections.length + 1);
    const position = configured.sections.findIndex(section => section.id === created.id) + 1;
    const title = 'Native section — second act';
    const name = admin.getByRole('textbox', {name: `Name for section ${position}`, exact: true});
    await name.fill(title);
    configured = await edit(() => name.press('Tab'));
    assert.equal(configured.sections.find(section => section.id === created.id).title, title);
    report.adminCreatedAndRenamedSection = true;

    // The long fixture gives real playback enough time for metadata requests
    // and an Admin reload without depending on short media reaching EOF.
    const mainTab = command.locator(`#remote-section-tabs [data-section-id="${originalMusic.sectionId}"]`);
    await mainTab.waitFor();
    await mainTab.tap();
    let playing;
    if (audio) {
      const [response] = await Promise.all([
        command.waitForResponse(item => apiPath(item) === '/api/play' && item.request().postDataJSON()?.cueId === musicCue.id),
        command.locator(`[data-cue-id="${musicCue.id}"]`).tap()
      ]);
      assert.equal(response.status(), 202, 'Music must start through the real remote button');
      playing = await until(async () => {
        const cue = cueState(await snapshot());
        return cue.selected && cue.state === 'playing' && !cue.paused && cue.generation > 0 && cue.transportRevision > 0 && cue;
      }, 'Native music did not acknowledge playback before section edits');
    }
    const beforeMetadata = await snapshot();
    const requestBaseline = playbackRequests.length;
    const assertPlaybackUnchanged = async () => {
      const current = await snapshot();
      assert.equal(current.stopEpoch, beforeMetadata.stopEpoch, 'Section edits must not send or apply STOP');
      assert.equal(current.stageEnabled, beforeMetadata.stageEnabled, 'Section metadata must not change Stage visibility');
      assert.equal(current.backgroundCueId, beforeMetadata.backgroundCueId, 'Section metadata must preserve the saved stage background');
      assert.deepEqual(playbackRequests.slice(requestBaseline), [], 'Section edits and tab navigation must not emit playback commands');
      if (audio) {
        const cue = cueState(current);
        assert.equal(cue.selected, true, 'Moving a playing cue must preserve its native selection');
        assert.equal(cue.state, 'playing'); assert.equal(cue.paused, false);
        assert.equal(cue.generation, playing.generation, 'Section metadata must retain the exact native source');
        assert.equal(cue.transportRevision, playing.transportRevision, 'Section metadata must not restart, pause or seek native playback');
      } else {
        assert.equal(current.state, 'stopped');
        assert.equal(current.cues.some(cue => cue.selected), false, 'Endpoint-free navigation must remain idle');
      }
      return current;
    };

    const cuePosition = configured.cues.findIndex(cue => cue.id === musicCue.id) + 1;
    configured = await edit(() => admin.getByRole('combobox', {name: `Section for cue ${cuePosition}`, exact: true}).selectOption(created.id));
    assert.equal(configured.cues.find(cue => cue.id === musicCue.id).sectionId, created.id);
    await assertPlaybackUnchanged();
    report.adminMovedExistingCueWithoutPlaybackCommand = true;

    const details = admin.locator(`details.playlist-section[data-section-id="${created.id}"]`);
    const summary = details.locator(':scope > summary');
    assert.equal(await details.evaluate(node => node.open), true);
    await summary.focus();
    configured = await edit(() => summary.press('Enter'));
    assert.equal(configured.sections.find(section => section.id === created.id).collapsed, true);
    assert.equal(await details.evaluate(node => node.open), false);
    assert.equal(await summary.evaluate(node => document.activeElement === node), true,
      'Saving collapsed metadata must preserve the focused summary DOM node');
    await assertPlaybackUnchanged();
    report.collapseSavePreservesSummaryFocus = true;

    await admin.reload({waitUntil: 'domcontentloaded'});
    await admin.locator('#connection.live').waitFor();
    await admin.waitForFunction(({id, revision}) => playlist?.playlistRevision === revision &&
      playlist.sections.some(section => section.id === id && section.collapsed) && !playlistBusy,
    {id: created.id, revision: configured.playlistRevision});
    assert.equal(await details.evaluate(node => node.open), false, 'Collapsed section must persist after Admin reload');
    const reloaded = await readPlaylist();
    assert.deepEqual(reloaded.sections, configured.sections);
    assert.equal(reloaded.cues.find(cue => cue.id === musicCue.id).sectionId, created.id);
    report.sectionNameMembershipAndCollapseSurviveAdminReload = true;
    await assertPlaybackUnchanged();

    const exportResponse = await admin.request.get(adminBase + '/api/playlist/export');
    assert.equal(exportResponse.status(), 200, 'The real host must export a portable playlist');
    const portable = await exportResponse.json();
    assert.equal(portable.format, 'smartstage-playlist');
    assert.deepEqual(portable.sections, configured.sections, 'Portable show must include section order, names and collapsed state');
    assert(isDeepStrictEqual(editCues(portable.cues), editCues(configured.cues)),
      'Portable show must include cue membership and all original cue properties');
    assert.deepEqual(portable.stage, original.stage);
    report.portableExportIncludesSectionsCollapseAndCueMembership = true;

    const selectedTab = command.getByRole('tab', {name: title, exact: true});
    await selectedTab.waitFor();
    await selectedTab.tap();
    await command.waitForFunction(id => document.querySelector(`#remote-section-tabs [data-section-id="${id}"]`)?.getAttribute('aria-selected') === 'true', created.id);
    assert.equal(await command.locator(`#cue-grid [data-cue-id="${musicCue.id}"]`).isVisible(), true,
      'Admin collapse must not hide the section on the remote');
    assert.deepEqual(await command.locator('#cue-grid .cue').evaluateAll(nodes => nodes.map(node => node.dataset.cueId)), [musicCue.id]);
    await selectedTab.focus();
    await selectedTab.press('Home');
    await command.waitForFunction(id => document.querySelector(`#remote-section-tabs [data-section-id="${id}"]`)?.getAttribute('aria-selected') === 'true', configured.sections[0].id);
    assert.equal(await command.locator(`#cue-grid [data-cue-id="${musicCue.id}"]`).count(), 0, 'Remote must show only the selected section');
    await command.locator('#remote-section-tabs [aria-selected="true"]').press('End');
    await command.waitForFunction(id => document.querySelector(`#remote-section-tabs [data-section-id="${id}"]`)?.getAttribute('aria-selected') === 'true', created.id);
    assert.equal(await selectedTab.evaluate(node => document.activeElement === node), true, 'Keyboard section navigation must retain focused tab');
    report.remoteTapAndKeyboardSelectOnlyRequestedSection = true;
    await assertPlaybackUnchanged();

    const sorterAssets = new Map();
    const observeSorterAsset = response => {
      const pathname = new URL(response.url()).pathname;
      if (['/assets/playlist-sorter.js', '/assets/playlist-sorter.css'].includes(pathname)) {
        sorterAssets.set(pathname, {status: response.status(), type: response.headers()['content-type'] || ''});
      }
    };
    admin.on('response', observeSorterAsset);
    try {
      await admin.locator('#sort-playlist').click();
      const sorter = admin.locator('#playlist-sorter');
      await sorter.waitFor({state: 'visible'});
      for (const [pathname, type] of [['/assets/playlist-sorter.js', 'text/javascript'], ['/assets/playlist-sorter.css', 'text/css']]) {
        assert.equal(sorterAssets.get(pathname)?.status, 200, 'Real Admin must serve its lazy sorter asset');
        assert(sorterAssets.get(pathname).type.startsWith(type), 'Sorter assets must have browser-compatible MIME types');
        const remoteResponse = await command.request.get(new URL(pathname, command.url()).href);
        assert.equal(remoteResponse.status(), 404, 'The command listener must not serve Admin sorting assets');
      }
      assert.equal(await sorter.locator('.playlist-sorter-cue').count(), configured.cues.length,
        'The compact sorter must include hidden and collapsed-section cues');
      const beforeSort = editCues(configured.cues);
      const firstSection = configured.sections[0].id;
      const handle = sorter.locator(`.playlist-sorter-cue-handle[data-cue-id="${musicCue.id}"]`);
      const destination = sorter.locator(`.playlist-sorter-cues[data-section-id="${firstSection}"]`);
      const from = await handle.boundingBox(), to = await destination.boundingBox();
      assert(from && to, 'Real sorter source and destination must be visible');
      await admin.mouse.move(from.x + from.width / 2, from.y + from.height / 2);
      await admin.mouse.down();
      await admin.mouse.move(to.x + 20, to.y + 6, {steps: 12});
      await admin.mouse.up();
      await admin.waitForFunction(({cue, section}) => document.querySelector(`.playlist-sorter-cue[data-cue-id="${cue}"]`)?.dataset.sectionId === section,
        {cue: musicCue.id, section: firstSection});
      configured = await edit(() => admin.locator('#playlist-sorter-save').click());
      await sorter.waitFor({state: 'hidden'});
      assert.equal(configured.cues.find(cue => cue.id === musicCue.id).sectionId, firstSection);
      for (const cue of editCues(configured.cues)) {
        const expected = beforeSort.find(item => item.id === cue.id);
        assert(isDeepStrictEqual({...cue, sectionId: expected.sectionId}, expected),
          'Sorting must retain every cue property except its requested section membership');
      }
      assert.deepEqual(configured.sections, reloaded.sections, 'Sorting must retain section names and collapsed metadata');
      await assertPlaybackUnchanged();
      report.adminSorterLazyAssetsAndNativeHostPointerSave = true;
      report.sorterRetainsCueMetadataWithoutPlaybackCommands = true;
    } finally {
      admin.off('response', observeSorterAsset);
      if (await admin.locator('#playlist-sorter').isVisible()) await admin.locator('#playlist-sorter-cancel').click();
    }
    if (audio) {
      await until(async () => cueState(await assertPlaybackUnchanged()).elapsed > playing.elapsed + .25,
        'Native music clock did not advance through section moves, collapse, reload and tab navigation');
      report.audioContinuityVerified = true;
      report.nativeSourceGenerationTransportAndAdvancingClockPreserved = true;
    }
    report.sectionEditsAndNavigationSendNoPlaybackCommands = true;
  } catch (error) {
    scenarioFailure = error; report.status = 'failed'; throw error;
  } finally {
    try {
      await stopAll();
      restored = await admin.evaluate(async original => {
        const current = await api('GET', '/api/playlist');
        const saved = await api('PUT', '/api/playlist', {expectedRevision: current.playlistRevision,
          cues: original.cues, sections: original.sections});
        await loadPlaylist(); await refreshState();
        return saved;
      }, {cues: editCues(original.cues), sections: original.sections});
      await admin.waitForFunction(revision => !playlistBusy && playlist?.playlistRevision === revision, restored.playlistRevision);
      assert.deepEqual(restored.sections, original.sections, 'Section checks must restore all original section metadata in the same transaction');
      // Compare private host paths in memory without putting them in a failed
      // assertion's object diff or the public verification report.
      assert(isDeepStrictEqual(editCues(restored.cues), editCues(original.cues)),
        'Section checks must restore original cue order, membership, flags, paths and titles');
      assert.deepEqual(restored.stage, original.stage, 'Section checks must preserve saved stage/background settings');
      assert(isDeepStrictEqual(restored.outputs, original.outputs), 'Section checks must preserve output devices');
      await until(async () => {
        const current = await snapshot();
        return !current.validationJob.running && current.cues.every(cue => cue.validation === 'ready');
      }, 'Restored section fixture did not finish native validation', 90000);
      if ((await snapshot()).stageEnabled !== initial.stageEnabled) {
        await command.locator('#remote-stage').tap();
        await until(async () => (await snapshot()).stageEnabled === initial.stageEnabled, 'Section checks did not restore Stage visibility');
      }
      await command.waitForFunction(revision => state?.playlistRevision === revision, restored.playlistRevision);
      report.originalCueAndSectionSettingsRestoredTogether = true;
    } catch (error) {
      report.cleanupFailed = true;
      if (!scenarioFailure) { report.status = 'failed'; throw error; }
    } finally {
      for (const page of [admin, command]) page.off('request', observeRequest);
    }
  }
  report.status = 'passed';
  record.checks.push('Real Admin section metadata survived reload/export; remote tabs and lazy Admin-only sorter mouse saves sent no playback commands; source continuity checked when audio available; original show restored');
  return restored;
};
