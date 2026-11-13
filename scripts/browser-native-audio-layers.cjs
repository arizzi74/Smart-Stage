// Real browser controls -> authenticated host -> native per-source events.
// No route mocking or test endpoints. Native renderer/gain inspection lives
// in the separate OS probes; this scenario checks the complete public path.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

module.exports = async function nativeAudioLayers({admin, command, adminBase, snapshot, until, sleep,
  apiPath, record, audio, display, mediaRoot, musicCue, videoCue}) {
  const report = record.audioLayersIntegration = {
    status: 'unavailable', nativeAudioGainsMeasuredHere: false, physicalRoutingVerified: false,
    method: 'Saved Admin checkbox flags and remote buttons against the real application; authoritative native per-cue acknowledgments and clocks.'
  };
  if (!audio || !display) {
    report.reason = !audio ? 'No enumerated native audio endpoint' : 'No enumerated native display';
    return null;
  }
  report.status = 'running';
  const initial = await snapshot();
  assert.equal(initial.imageCueId, '');
  assert.equal(initial.backgroundOverrideCueId, '');
  const readPlaylist = async () => {
    const response = await admin.request.get(adminBase + '/api/playlist');
    assert.equal(response.status(), 200);
    return response.json();
  };
  const original = await readPlaylist();
  const editCues = cues => cues.map(({id, label, path, color, hidden, background, keepVideo, mixAudio, volume}) =>
    ({id, label, path, color: color || '', hidden: Boolean(hidden), background: Boolean(background),
      keepVideo: Boolean(keepVideo), mixAudio: Boolean(mixAudio), ...(volume === undefined ? {} : {volume})}));
  const replaceCues = async cues => {
    const saved = await admin.evaluate(async cues => {
      const current = await api('GET', '/api/playlist');
      const saved = await api('PUT', '/api/playlist', {expectedRevision: current.playlistRevision, cues});
      await loadPlaylist(); await refreshState();
      return saved;
    }, cues);
    await admin.waitForFunction(revision => !playlistBusy && playlist.playlistRevision === revision, saved.playlistRevision);
    return saved;
  };
  const stopAll = async () => {
    const [response] = await Promise.all([
      command.waitForResponse(item => apiPath(item) === '/api/stop' && item.request().method() === 'POST'),
      command.locator('#stop').tap()
    ]);
    assert.equal(response.status(), 202);
    return until(async () => {
      const current = await snapshot();
      return current.state === 'stopped' && current.cues.every(cue => !cue.selected) && current;
    }, 'Global STOP did not clear every native audio/video source');
  };
  const cueState = (current, id) => {
    const cue = current.cues.find(cue => cue.id === id);
    assert(cue, 'Expected fixture cue in authoritative state');
    return cue;
  };
  const waitCue = async (id, phase, generation) => {
    const cue = await until(async () => {
      const current = cueState(await snapshot(), id);
      return current.selected && current.state === phase && current.paused === (phase === 'paused') &&
        (!generation || current.generation === generation) && current;
    }, `Native cue did not acknowledge ${phase}`);
    assert(cue.generation > 0 && cue.transportRevision > 0);
    await command.waitForFunction(({id, phase, generation, transportRevision}) => {
      const cue = state?.cues.find(cue => cue.id === id);
      return cue?.selected && cue.state === phase && cue.generation === generation &&
        cue.transportRevision === transportRevision && !pendingTransport &&
        document.querySelector(`[data-cue-id="${id}"]`)?.getAttribute('aria-pressed') === 'true';
    }, {id, phase, generation: cue.generation, transportRevision: cue.transportRevision});
    return cue;
  };
  const tapCue = async (id, action) => {
    const before = cueState(await snapshot(), id);
    if (action !== 'play') await waitCue(id, action === 'pause' ? 'playing' : 'paused', before.generation);
    const [response] = await Promise.all([
      command.waitForResponse(item => apiPath(item) === '/api/play' && item.request().postDataJSON()?.cueId === id),
      command.locator(`[data-cue-id="${id}"]`).tap()
    ]);
    assert.equal(response.status(), 202, 'Remote cue command must reach the real host');
    const body = response.request().postDataJSON();
    assert.equal(body.action || 'play', action);
    if (action !== 'play') {
      assert.equal(body.generation, before.generation, 'Each remote button must address its own native source');
      assert.equal(body.transportRevision, before.transportRevision, 'Each remote button must use its own current transport revision');
    }
    return waitCue(id, action === 'pause' ? 'paused' : 'playing', action === 'play' ? 0 : before.generation);
  };
  const setFlag = async (id, name, value) => {
    const current = await readPlaylist(), position = current.cues.findIndex(cue => cue.id === id) + 1;
    assert(position > 0);
    const flag = admin.getByRole('checkbox', {name: `${name} for cue ${position}`, exact: true});
    if (await flag.isChecked() === value) return;
    const [response] = await Promise.all([
      admin.waitForResponse(item => apiPath(item) === '/api/playlist' && item.request().method() === 'PUT'),
      flag.setChecked(value)
    ]);
    assert.equal(response.status(), 200, 'Audio flags must save through the actual Admin controls');
    const saved = await response.json();
    await admin.waitForFunction(revision => !playlistBusy && playlist.playlistRevision === revision, saved.playlistRevision);
  };
  const extraPath = path.join(mediaRoot, 'Scene mixed layer 60s.wav');
  let restored, scenarioFailure;
  try {
    await stopAll();
    fs.copyFileSync(musicCue.path, extraPath);
    const added = await replaceCues([...editCues(original.cues), {id: '', label: 'Mixed audio layer', path: extraPath}]);
    const extra = added.cues.find(cue => !original.cues.some(previous => previous.id === cue.id));
    assert(extra && extra.id !== musicCue.id);
    await until(async () => {
      const current = await snapshot();
      return !current.validationJob.running && current.cues.every(cue => cue.validation === 'ready');
    }, 'Native layer fixture validation did not finish', 90000);
    // The existing AAC video is a hidden background button in the surrounding
    // scenario. Temporarily expose it as an ordinary foreground cue.
    const videoPosition = added.cues.findIndex(cue => cue.id === videoCue.id) + 1;
    for (const name of [`Use cue ${videoPosition} as a background button`, `Hide remote button for cue ${videoPosition}`]) {
      const flag = admin.getByRole('checkbox', {name, exact: true});
      if (await flag.isChecked()) {
        const [response] = await Promise.all([
          admin.waitForResponse(item => apiPath(item) === '/api/playlist' && item.request().method() === 'PUT'), flag.uncheck()
        ]);
        assert.equal(response.status(), 200);
        const saved = await response.json();
        await admin.waitForFunction(revision => !playlistBusy && playlist.playlistRevision === revision, saved.playlistRevision);
      }
    }
    await setFlag(musicCue.id, 'Do not stop video', true);
    await setFlag(musicCue.id, 'Mix audio', false);
    await setFlag(extra.id, 'Mix audio', true);
    await admin.reload({waitUntil: 'domcontentloaded'}); await admin.locator('#connection.live').waitFor();
    const configured = await readPlaylist();
    const firstConfig = configured.cues.find(cue => cue.id === musicCue.id), secondConfig = configured.cues.find(cue => cue.id === extra.id);
    assert.equal(firstConfig.keepVideo, true); assert.equal(Boolean(firstConfig.mixAudio), false); assert.equal(secondConfig.mixAudio, true);
    for (const [id, name] of [[musicCue.id, 'Do not stop video'], [extra.id, 'Mix audio']]) {
      const position = configured.cues.findIndex(cue => cue.id === id) + 1;
      assert.equal(await admin.getByRole('checkbox', {name: `${name} for cue ${position}`, exact: true}).isChecked(), true);
    }
    await until(async () => {
      const current = await snapshot();
      return !current.validationJob.running && current.cues.every(cue => cue.validation === 'ready');
    }, 'Flag-save native validation did not finish', 90000);
    report.adminFlagsSavedAndReloaded = true;
    await command.locator(`[data-cue-id="${videoCue.id}"]`).waitFor();
    const videoPlaying = await tapCue(videoCue.id, 'play');
    // Pause the short video before starting the long audio fixtures. Later we
    // resume its actual clock while both mixed sources are playing.
    const videoPaused = await tapCue(videoCue.id, 'pause');
    assert.equal(videoPaused.generation, videoPlaying.generation);
    const first = await tapCue(musicCue.id, 'play');
    const second = await tapCue(extra.id, 'play');
    assert.notEqual(first.generation, second.generation);
    let current = await snapshot();
    for (const [id, source] of [[musicCue.id, first], [extra.id, second]]) {
      const actual = cueState(current, id);
      assert.equal(actual.generation, source.generation); assert.equal(actual.state, 'playing'); assert.equal(actual.selected, true);
    }
    assert.equal(cueState(current, videoCue.id).generation, videoPlaying.generation);
    assert.equal(cueState(current, videoCue.id).state, 'paused');
    report.twoIndependentNativeAudioSourcesAcknowledged = true;
    report.keepVideoPreservedNativeSourceIdentity = true;
    const resumedVideo = await tapCue(videoCue.id, 'resume');
    await until(async () => {
      const current = await snapshot(), video = cueState(current, videoCue.id);
      assert.equal(cueState(current, musicCue.id).generation, first.generation);
      assert.equal(cueState(current, extra.id).generation, second.generation);
      return video.generation === resumedVideo.generation && video.state === 'playing' && video.elapsed > resumedVideo.elapsed + .1 &&
        cueState(current, musicCue.id).state === 'playing' && cueState(current, extra.id).state === 'playing';
    }, 'Retained video clock did not advance alongside both native audio sources');
    await tapCue(videoCue.id, 'pause');
    report.retainedVideoClockAdvancedWithBothAudioLayers = true;
    for (const [id, other, generation, otherGeneration] of [[musicCue.id, extra.id, first.generation, second.generation], [extra.id, musicCue.id, second.generation, first.generation]]) {
      const paused = await tapCue(id, 'pause');
      const otherBefore = cueState(await snapshot(), other);
      await sleep(350);
      current = await snapshot();
      const held = cueState(current, id), continuing = cueState(current, other);
      assert.equal(held.generation, generation); assert.equal(held.state, 'paused');
      assert(Math.abs(held.elapsed - paused.elapsed) < .08, 'Only the selected native audio clock must freeze');
      assert.equal(continuing.generation, otherGeneration); assert.equal(continuing.state, 'playing');
      await until(async () => cueState(await snapshot(), other).elapsed > otherBefore.elapsed + .1, 'The other native audio source stopped advancing');
      const resumed = await tapCue(id, 'resume');
      assert.equal(resumed.generation, generation);
      assert(resumed.transportRevision > paused.transportRevision);
      await until(async () => cueState(await snapshot(), id).elapsed > paused.elapsed + .1, 'Resumed native layer did not advance from its paused clock');
    }
    report.remotePauseResumeUsesEachSourceIdentityAndTransport = true;
    report.pausingEitherLayerPreservesTheOtherNativeClock = true;
    await stopAll();
    report.globalStopClearsAllNativeSelections = true;
  } catch (error) {
    scenarioFailure = error; report.status = 'failed'; throw error;
  } finally {
    try {
      await stopAll();
      restored = await replaceCues(editCues(original.cues));
      assert.deepEqual(restored.stage, original.stage, 'Layer checks must preserve saved stage settings');
      assert.deepEqual(editCues(restored.cues), editCues(original.cues), 'Layer checks must restore every original cue setting');
      await until(async () => {
        const current = await snapshot();
        return !current.validationJob.running && current.cues.every(cue => cue.validation === 'ready');
      }, 'Restored playlist did not complete native validation', 90000);
      if ((await snapshot()).stageEnabled !== initial.stageEnabled) {
        await command.locator('#remote-stage').tap();
        await until(async () => (await snapshot()).stageEnabled === initial.stageEnabled, 'Layer checks did not restore stage visibility');
      }
      report.playlistFlagsAndSettingsRestored = true;
      // The surrounding scenario removes its entire temporary media directory
      // after the application exits, allowing native file handles to unwind.
    } catch (error) {
      report.cleanupFailed = true;
      if (!scenarioFailure) { report.status = 'failed'; throw error; }
    }
  }
  report.status = 'passed';
  record.checks.push('Saved KeepVideo/MixAudio flags reached the real native host; two audio sources and retained video kept distinct identities, remote pause/resume froze only its own clock, and STOP cleared every selection');
  return restored;
};
