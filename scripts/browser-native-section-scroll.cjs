// Authenticated served-host layout checks, shared by Chromium and Mac WebKit.
// Wheel input is real browser input; native-window forwarding and touch are not claimed.
const assert = require('node:assert/strict');
const {isDeepStrictEqual} = require('node:util');

module.exports = async function sectionScroll({admin, command, record}) {
  const report = record.sectionScrollIntegration = {status: 'running', browserEngine: record.browserEngine,
    method: 'Authenticated Admin API fixtures and actual browser wheel/tab input against the served native Go host',
    dedicatedWindowInputVerified: false, physicalTouchVerified: false};
  const original = await admin.evaluate(() => api('GET', '/api/playlist'));
  const viewport = command.viewportSize();
  const cueEdits = cues => cues.map(({id, label, path, sectionId, color, hidden, background, keepVideo, mixAudio, volume}) =>
    ({id, label, path, sectionId, color: color || '', hidden: Boolean(hidden), background: Boolean(background),
      keepVideo: Boolean(keepVideo), mixAudio: Boolean(mixAudio), ...(volume === undefined ? {} : {volume})}));
  const replace = async config => {
    const saved = await admin.evaluate(async edit => {
      const current = await api('GET', '/api/playlist');
      const saved = await api('PUT', '/api/playlist', {...edit, expectedRevision: current.playlistRevision});
      await loadPlaylist(); await refreshState(); return saved;
    }, {cues: cueEdits(config.cues), sections: config.sections});
    await command.waitForFunction(revision => state?.playlistRevision === revision, saved.playlistRevision);
    return saved;
  };
  const source = original.cues.find(cue => cue.cache?.media?.kind === 'audio');
  assert(source && original.sections.length <= 48 && original.cues.length <= 489, 'Scroll fixture needs a validated audio path and spare cue/section slots');
  const suffix = Date.now().toString(36), longID = `scroll-long-${suffix}`, shortID = `scroll-short-${suffix}`;
  const previous = await command.evaluate(() => ({selected: document.querySelector('.remote-section-page.is-current')?.dataset.sectionId,
    offsets: [...document.querySelectorAll('.remote-section-page')].map(page => [page.dataset.sectionId, page.scrollTop])}));
  const select = async id => {
    const tab = command.locator(`#remote-section-tabs [data-section-id="${id}"]`);
    if (await tab.getAttribute('aria-selected') !== 'true') await tab.click();
    await command.waitForFunction(id => document.querySelector('.remote-section-page.is-current')?.dataset.sectionId === id &&
      !document.querySelector('.remote-section-pager.is-settling'), id);
  };
  const traffic = [];
  const observe = request => {
    const pathname = new URL(request.url()).pathname;
    if (request.isNavigationRequest() && request.method() === 'GET') traffic.push('document-navigation');
    if (request.method() === 'POST' && ['/api/play', '/api/stop', '/api/emergency-stop', '/api/seek', '/api/stage-output', '/api/volume'].includes(pathname)) traffic.push(pathname);
  };
  for (const page of [admin, command]) page.on('request', observe);
  let stable, restored, failure;
  try {
    await command.setViewportSize({width: 390, height: 844});
    await replace({sections: [...original.sections, {id: longID, title: `Scroll long ${suffix}`, collapsed: false},
      {id: shortID, title: `Scroll short ${suffix}`, collapsed: false}], cues: [...original.cues,
      ...Array.from({length: 11}, (_, index) => ({id: '', path: source.path, label: `Scroll fixture cue ${index + 1}`,
        sectionId: index < 10 ? longID : shortID, volume: source.volume}))]});
    await select(longID);
    const long = command.locator(`.remote-section-page[data-section-id="${longID}"]`);
    stable = await long.evaluateHandle(page => ({page, grid: page.querySelector('.cue-grid'), cue: page.querySelector('.cue')}));
    const wheel = async () => {
      const box = await command.locator('.remote-section-page.is-current').boundingBox();
      assert(box && box.height > 30, 'The selected section must retain a usable viewport');
      await command.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
      await command.mouse.wheel(0, 10000);
    };
    await wheel();
    await command.waitForFunction(id => {const page = document.querySelector(`.remote-section-page[data-section-id="${id}"]`);
      return page.scrollTop > 100 && Math.abs(page.scrollHeight - page.clientHeight - page.scrollTop) < .5;}, longID);
    const offset = await long.evaluate(page => page.scrollTop);
    assert.equal(await command.evaluate(() => scrollY), 0, 'Only the section scrolls, never the document');
    await select(shortID);
    assert(await command.locator('.remote-section-page.is-current').evaluate(page => page.scrollTop === 0 && page.scrollHeight <= page.clientHeight + 1 &&
      page.querySelector('.cue').getBoundingClientRect().top >= page.getBoundingClientRect().top), 'A short section starts at its own top with visible content');
    await wheel(); await command.waitForTimeout(250);
    assert.equal(await command.evaluate(() => scrollY), 0, 'Wheel input on a short section cannot chain into the document');
    assert.equal(await long.evaluate(page => page.scrollTop), offset, 'An inactive section retains its independent scroll position');
    assert.equal(await command.locator('.remote-section-page.is-current').evaluate(page => page.scrollTop), 0);
    await select(longID);
    assert.equal(await long.evaluate(page => page.scrollTop), offset, 'Returning to a section restores the same offset');
    await command.setViewportSize({width: 844, height: 390});
    await command.waitForFunction(() => Math.abs(document.body.getBoundingClientRect().height - innerHeight) < 2);
    await wheel();
    await command.waitForFunction(() => {const page = document.querySelector('.remote-section-page.is-current');
      return Math.abs(page.scrollHeight - page.clientHeight - page.scrollTop) < .5;});
    const frame = await command.evaluate(() => {
      const page = document.querySelector('.remote-section-page.is-current'), box = page.getBoundingClientRect();
      const stop = document.getElementById('stop'), stopBox = stop.getBoundingClientRect(), seek = document.getElementById('seek-bar');
      const seekVisible = !seek.hidden, seekBox = seek.getBoundingClientRect(), last = page.querySelector('.cue:last-child').getBoundingClientRect();
      return {documentTop: scrollY, stopVisible: stopBox.top >= 0 && stopBox.bottom <= innerHeight &&
        stop.contains(document.elementFromPoint(stopBox.x + stopBox.width / 2, stopBox.y + stopBox.height / 2)),
        pageBelowStop: box.top >= stopBox.bottom, bottomCueVisible: last.bottom <= box.bottom + 1 && last.bottom > box.top,
        pageWithinFrame: box.bottom <= innerHeight && (!seekVisible || box.bottom <= seekBox.top + 1), seekVisible};
    });
    assert(frame.documentTop === 0 && frame.stopVisible && frame.pageBelowStop && frame.bottomCueVisible && frame.pageWithinFrame,
      `Rotated section and transport geometry: ${JSON.stringify(frame)}`);
    assert(await stable.evaluate(({page, grid, cue}) => page.isConnected && grid.isConnected && cue.isConnected &&
      page.querySelector('.cue-grid') === grid && grid.contains(cue)), 'Scrolling and tab navigation preserve actual cue/grid nodes');
    assert.deepEqual(traffic, [], 'Scrolling and section changes send no document navigation or playback commands');
    Object.assign(report, {wheelScrollsOnlySelectedSection: true, shortSectionStartsAtTopWithoutScrollChaining: true,
      returningSectionRetainsOffsetAndNodes: true, rotatedBottomCueAndStopRemainUnoccluded: true,
      navigationAndPlaybackRequestsAbsent: true, seekBarVisibleDuringCheck: frame.seekVisible});
  } catch (error) {failure = error; report.status = 'failed'; throw error;
  } finally {
    try {
      restored = await replace(original);
      assert(isDeepStrictEqual(cueEdits(restored.cues), cueEdits(original.cues)) && isDeepStrictEqual(restored.sections, original.sections) &&
        isDeepStrictEqual(restored.stage, original.stage) && isDeepStrictEqual(restored.outputs, original.outputs), 'Scroll checks must restore all original cue/section metadata and preserve stage/output settings');
      await command.setViewportSize(viewport);
      if (previous.selected) await select(previous.selected);
      await command.evaluate(offsets => {for (const [id, top] of offsets) {const page = document.querySelector(`.remote-section-page[data-section-id="${id}"]`); if (page) page.scrollTop = top;}}, previous.offsets);
      report.originalPlaylistViewportAndSectionRestored = true;
    } catch (error) {report.status = 'failed'; if (!failure) throw error;
    } finally {for (const page of [admin, command]) page.off('request', observe); await stable?.dispose();}
  }
  report.status = 'passed'; return restored;
};
