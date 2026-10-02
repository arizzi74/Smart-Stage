'use strict';
const $ = id => document.getElementById(id);
const adminPage = location.pathname === '/admin';
// Only the gateway's endpoint route may scope remote requests. Admin always
// uses the loopback root; arbitrary paths cannot redirect privileged requests.
const gatewayRoute = /^\/smartstage\/e\/[A-Za-z0-9_-]{16,128}\/command$/.test(location.pathname);
const endpointPrefix = gatewayRoute ? location.pathname.slice(0, -'/command'.length) : '';
const endpointPath = path => endpointPrefix + path;
// The native user-agent token changes guidance only. It grants no API access
// and never lets JavaScript read or submit a native file's original path.
const desktopAdmin = adminPage && /(?:^|\s)SmartStageDesktop(?:\s|$)/.test(navigator.userAgent);
// Platform detection selects copy only; host capabilities still control every
// native action, and all requests use the authenticated local Admin session.
const windowsPlatform = /(?:^|\s)SmartStageWindowsDesktop(?:\s|$)|Windows NT/.test(navigator.userAgent) || /^Win/.test(navigator.platform);
const fileManagerName = windowsPlatform ? 'File Explorer' : 'Finder';
let state = null, role = '', csrf = '', online = false, source = null, lastSeen = 0;
let playlist = null, devices = null;
let playlistBusy = false, refreshing = false, renderedOrder = '', playlistRefresh = false;
let stageSettingsDirty = false, stageSettingsRevision = 0, stageSettingsBase = '', stageSettingsConflict = '';
let stageBackgroundDraft = null, playlistRefreshPending = false;
let controlSequence = 0;
let seekGesture = null, seekSending = false, seekTarget = null, pendingTransport = null;
let masterVolumeTarget = null, masterVolumeSending = false, masterVolumeGesture = false, masterVolumeTimer = null;
let masterVolumeRequest = null, masterVolumeConfirmation = null, masterVolumePointerId = null, masterVolumeSequence = 0;
let masterVolumeStopPending = 0;
const masterVolumeKeys = ['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown', 'PageUp', 'PageDown', 'Home', 'End'];
const masterVolumeHeldKeys = new Set();
let trackVolumeSending = false, trackVolumeTimer = null;
const pendingTrackVolumes = new Map();
let trackVolumeRequest = null, trackVolumeSequence = 0, trackPlaylistRenderPending = false;
const trackVolumeGestures = new Map();
let validationSignature = '', validationRefresh = false, validationRefreshPending = false;
let localSessionBusy = false, localSessionRetry = null;
let presenceBusy = false, quitBusy = false, appClosed = false, reloadingAdmin = false;
let adminCapabilities = {}, chooseFilesBusy = false;
let playlistFileBusy = false, playlistFileRevision = 0;
let remoteLinks = [], selectedRemoteURL = '', remoteRefresh = false;
let gateway = null, gatewayDirty = false, gatewayBusy = false, gatewayRefresh = false, gatewaySequence = 0;
let updateStatus = null, updateBusy = false, updatePreparing = false, updatePollAt = 0;
let updateRestartInstance = '', updateRestartComplete = false, updateRestartStarted = 0;
const cueButtons = new Map(), playlistRows = new Map();
const i18n = window.smartStageI18n;
const t = (key, values) => i18n.t(key, values);
const localizedText = (node, read) => i18n.text(node, read);
const localizedAttribute = (node, name, read) => i18n.attribute(node, name, read);
function localizedNode(read) { const node = document.createTextNode(''); localizedText(node, read); return node; }
function errorText(error) { return i18n.diagnostic(error?.message || ''); }
let languageMode = 'system', hostLanguageLoaded = false, languageBusy = false;
const languageControls = [$('language-mode'), $('pair-language-mode')];
function renderLanguageControls() {
  for (const control of languageControls) {
    control.value = languageMode;
    control.disabled = languageBusy || (adminPage && (!online || role !== 'admin' || quitBusy || appClosed));
  }
}
function applyHostLanguage(value, force = false) {
  if (!adminPage || !value || !['system', 'en', 'it'].includes(value.mode) || !['en', 'it'].includes(value.effective) || (languageBusy && !force)) return;
  hostLanguageLoaded = true; languageMode = value.mode;
  i18n.setLanguage(value.effective); renderLanguageControls();
}
async function loadLanguage() {
  if (!adminPage || role !== 'admin' || hostLanguageLoaded) return;
  try { applyHostLanguage(await api('GET', '/api/language')); }
  catch { /* Older hosts may not expose preferences; keep the browser-language fallback. */ }
}
function remoteLanguageMode() {
  try { const saved = localStorage.getItem('smartstage.remote.language'); return ['en', 'it'].includes(saved) ? saved : 'system'; }
  catch { return 'system'; }
}
function applyRemoteLanguage() {
  if (adminPage) return;
  i18n.setLanguage(languageMode === 'system' ? i18n.systemLanguage() : languageMode);
  renderLanguageControls();
}
for (const control of languageControls) control.addEventListener('change', async () => {
  const mode = control.value;
  if (!['system', 'en', 'it'].includes(mode) || languageBusy) { renderLanguageControls(); return; }
  if (!adminPage) {
    languageMode = mode;
    try {
      if (mode === 'system') localStorage.removeItem('smartstage.remote.language');
      else localStorage.setItem('smartstage.remote.language', mode);
    } catch { /* The selection still works for this page when storage is unavailable. */ }
    applyRemoteLanguage(); return;
  }
  if (!online || role !== 'admin') { renderLanguageControls(); return; }
  languageBusy = true; renderLanguageControls();
  try { applyHostLanguage(await api('PUT', '/api/language', {mode}), true); }
  catch (error) { notify(() => t('Could not save the language. {0}', {0: errorText(error)}), true); }
  finally { languageBusy = false; renderLanguageControls(); }
});
window.addEventListener('languagechange', () => {
  if (!adminPage && languageMode === 'system') applyRemoteLanguage();
});
window.addEventListener('storage', event => {
  if (!adminPage && event.key === 'smartstage.remote.language') { languageMode = remoteLanguageMode(); applyRemoteLanguage(); }
});
if (!adminPage) { languageMode = remoteLanguageMode(); applyRemoteLanguage(); }
renderLanguageControls();
document.body.classList.toggle('remote-page', !adminPage);
document.body.classList.toggle('admin-page', adminPage);
$('page-title').hidden = !adminPage;
$('master-volume-control').hidden = adminPage;
if (gatewayRoute) {
  localizedText($('pair-guidance'), () => t("Scan the current QR code or open the remote control link shown in Admin on the host computer. You can also paste the access key from that link below."));
  localizedText($('pair-key-label'), () => t("Access key"));
  $('pair-key').inputMode = 'text'; $('pair-key').maxLength = 64; $('pair-key').minLength = 64;
  $('pair-key').pattern = '[0-9a-fA-F]{64}';
  localizedText($('pair-network-hint'), () => t("Keep Smart Stage running and connected to the public gateway."));
}

function element(tag, text, className) {
  const node = document.createElement(tag);
  if (text !== undefined && text !== '') node.append(localizedNode(text));
  if (className) node.className = className;
  return node;
}
function button(text, action, className) {
  const node = element('button', text, className); node.type = 'button';
  node.addEventListener('click', action); return node;
}
function requestID() {
  return Array.from(crypto.getRandomValues(new Uint8Array(24)), b => b.toString(16).padStart(2, '0')).join('');
}
function volumePercent(value, fallback = 0.5) {
  return Math.round(100 * (Number.isFinite(value) ? Math.max(0, Math.min(1, value)) : fallback));
}
function trackVolumeText(range) {
  const relative = 2 * (Number(range.value) - 50);
  return `${relative > 0 ? '+' : ''}${relative}%`;
}
function renderTrackVolume(range, output) {
  const text = trackVolumeText(range);
  output.value = text; range.setAttribute('aria-valuetext', text);
}
function masterVolumeAvailable() {
  return Boolean(state && online && csrf && !masterVolumeStopPending &&
    !quitBusy && !appClosed && !reloadingAdmin && !updatePending());
}
function currentMasterVolumeTarget(target) {
  return Boolean(target && state && target.sequence === masterVolumeSequence &&
    target.instanceId === state.instanceId && target.stopEpoch === state.stopEpoch);
}
function releaseMasterVolumePointer() {
  const pointerId = masterVolumePointerId; masterVolumePointerId = null;
  const range = $('master-volume');
  if (pointerId !== null && range.hasPointerCapture?.(pointerId)) range.releasePointerCapture(pointerId);
}
function cancelMasterVolume(preserveConfirmation = false) {
  ++masterVolumeSequence;
  clearTimeout(masterVolumeTimer); masterVolumeTimer = null;
  masterVolumeTarget = null; masterVolumeGesture = false;
  if (!preserveConfirmation) masterVolumeConfirmation = null;
  masterVolumeHeldKeys.clear();
  releaseMasterVolumePointer();
}
function renderMasterVolume() {
  const range = $('master-volume');
  const available = masterVolumeAvailable();
  const preserveConfirmation = Boolean(masterVolumeStopPending && state && online && csrf &&
    !quitBusy && !appClosed && !reloadingAdmin && !updatePending());
  if (!available && (masterVolumeTarget || !preserveConfirmation && masterVolumeConfirmation || masterVolumeGesture || masterVolumeHeldKeys.size || currentMasterVolumeTarget(masterVolumeRequest))) cancelMasterVolume(preserveConfirmation);
  range.disabled = !available;
  if (!masterVolumeGesture && !masterVolumeHeldKeys.size && !masterVolumeTarget && !currentMasterVolumeTarget(masterVolumeRequest)) {
    range.value = volumePercent(masterVolumeConfirmation?.volume ?? state?.masterVolume, .75);
  }
  $('master-volume-value').value = `${range.value}%`;
  range.setAttribute('aria-valuetext', `${range.value}%`);
}
function scheduleMasterVolume() {
  clearTimeout(masterVolumeTimer); masterVolumeTimer = null;
  if (!masterVolumeTarget || masterVolumeSending) return;
  const delay = masterVolumeTarget.readyAt - Date.now();
  if (delay <= 0) { void sendMasterVolume(); return; }
  masterVolumeTimer = setTimeout(() => { void sendMasterVolume(); }, delay);
}
async function sendMasterVolume() {
  clearTimeout(masterVolumeTimer); masterVolumeTimer = null;
  if (masterVolumeSending || !masterVolumeTarget) return;
  if (!masterVolumeAvailable() || !currentMasterVolumeTarget(masterVolumeTarget)) { cancelMasterVolume(); renderMasterVolume(); return; }
  if (masterVolumeTarget.readyAt > Date.now()) { scheduleMasterVolume(); return; }
  const confirmedVolume = masterVolumeConfirmation?.volume ?? state.masterVolume;
  if (volumePercent(masterVolumeTarget.volume) === volumePercent(confirmedVolume, .75)) {
    masterVolumeTarget = null; renderMasterVolume(); return;
  }
  const target = masterVolumeTarget; masterVolumeTarget = null; masterVolumeSending = true;
  masterVolumeRequest = target;
  try {
    const ack = await api('POST', '/api/play', { requestId: requestID(), instanceId: target.instanceId, stopEpoch: target.stopEpoch, action: 'volume', volume: target.volume });
    if (!currentMasterVolumeTarget(target) || !masterVolumeAvailable()) return;
    if (ack.accepted !== true || ack.instanceId !== target.instanceId || ack.stopEpoch !== target.stopEpoch || !Number.isSafeInteger(ack.revision)) {
      throw new Error('The host did not acknowledge the volume change.');
    }
    // The acknowledgement confirms only volume. Keep the full state revision
    // intact so delayed SSE still supplies transport, STOP, and update fields.
    masterVolumeConfirmation = state.revision < ack.revision ? { ...target, revision: ack.revision } : null;
  } catch (error) {
    if (currentMasterVolumeTarget(target)) {
      if (['stale_instance', 'stale_epoch', 'updating'].includes(error.code)) cancelMasterVolume();
      else masterVolumeConfirmation = null;
      notify(() => t("Volume change is unconfirmed. {0}", {0: errorText(error)}), true);
    }
    await refreshState();
  } finally {
    if (masterVolumeRequest === target) masterVolumeRequest = null;
    masterVolumeSending = false; renderMasterVolume();
    scheduleMasterVolume();
  }
}
$('master-volume').addEventListener('pointerdown', event => {
  if (!masterVolumeAvailable()) return;
  masterVolumeGesture = true; masterVolumePointerId = event.pointerId;
  try { $('master-volume').setPointerCapture?.(event.pointerId); } catch { /* Document release handlers cover native range tracking. */ }
});
function endMasterVolumeGesture() {
  masterVolumeGesture = false; masterVolumeHeldKeys.clear(); releaseMasterVolumePointer();
  if (masterVolumeTarget) masterVolumeTarget.readyAt = 0;
  scheduleMasterVolume(); renderMasterVolume();
}
for (const event of ['pointerup', 'pointercancel', 'blur']) $('master-volume').addEventListener(event, endMasterVolumeGesture);
$('master-volume').addEventListener('change', () => {
  // Native ranges can emit change on every keydown, before the key is released.
  if (!masterVolumeHeldKeys.size) endMasterVolumeGesture();
});
$('master-volume').addEventListener('lostpointercapture', () => { if (masterVolumeGesture) endMasterVolumeGesture(); });
for (const event of ['pointerup', 'pointercancel']) document.addEventListener(event, pointer => {
  if (masterVolumeGesture && pointer.pointerId === masterVolumePointerId) endMasterVolumeGesture();
});
$('master-volume').addEventListener('keydown', event => {
  if (event.key === 'Escape') { cancelMasterVolume(true); renderMasterVolume(); return; }
  if (masterVolumeKeys.includes(event.key) && masterVolumeAvailable()) masterVolumeHeldKeys.add(event.key);
});
$('master-volume').addEventListener('keyup', event => {
  if (!masterVolumeKeys.includes(event.key)) return;
  masterVolumeHeldKeys.delete(event.key);
  if (!masterVolumeHeldKeys.size) endMasterVolumeGesture();
});
document.addEventListener('keyup', event => {
  if (!masterVolumeHeldKeys.has(event.key)) return;
  masterVolumeHeldKeys.delete(event.key);
  if (!masterVolumeHeldKeys.size) endMasterVolumeGesture();
});
window.addEventListener('blur', () => { if (masterVolumeGesture || masterVolumeHeldKeys.size || masterVolumeTarget) endMasterVolumeGesture(); });
$('master-volume').addEventListener('input', () => {
  if (!masterVolumeAvailable()) { renderMasterVolume(); return; }
  const volume = Number($('master-volume').value) / 100;
  if (masterVolumeTarget && volumePercent(masterVolumeTarget.volume) === volumePercent(volume)) return;
  const pendingRequest = currentMasterVolumeTarget(masterVolumeRequest) ? masterVolumeRequest : null;
  const confirmedVolume = masterVolumeConfirmation?.volume ?? state.masterVolume;
  // Keep the latest intent until an in-flight acknowledgement arrives: another
  // controller may have changed that level since our earlier request applied.
  masterVolumeTarget = !pendingRequest && volumePercent(confirmedVolume, .75) === volumePercent(volume) ? null : {
    instanceId: state.instanceId, stopEpoch: state.stopEpoch, sequence: masterVolumeSequence,
    volume, readyAt: Date.now() + 200
  };
  renderMasterVolume(); scheduleMasterVolume();
});

function trackVolumeAvailable() {
  return Boolean(adminPage && role === 'admin' && online && csrf && playlist &&
    !quitBusy && !appClosed && !reloadingAdmin && !updatePending() && !playlistFileBusy);
}
function trackVolumeCue(id, path) {
  return playlist?.cues.find(cue => cue.id === id && cue.path === path && ['audio', 'video'].includes(cue.cache?.media?.kind));
}
function currentTrackVolumeControl(gesture) {
  const row = playlistRows.get(gesture.id);
  return Boolean(row?.volume === gesture.range && row.volumePath === gesture.path && trackVolumeCue(gesture.id, gesture.path));
}
function currentTrackVolumeRequest(id, path) {
  return Boolean(trackVolumeRequest?.sequence === trackVolumeSequence && trackVolumeRequest.id === id &&
    trackVolumeRequest.path === path && trackVolumeCue(id, path));
}
function startTrackVolumeGesture(gesture) {
  if (trackVolumeGestures.get(gesture.id) === gesture) return;
  const target = pendingTrackVolumes.get(gesture.id);
  gesture.previousTarget = target?.path === gesture.path && target.sequence === trackVolumeSequence && Number.isFinite(target.readyAt) ? { ...target } : null;
  trackVolumeGestures.set(gesture.id, gesture);
}
function cancelTrackVolumeGesture(gesture, pointerReleased = false) {
  const active = trackVolumeGestures.get(gesture.id) === gesture;
  if (active) trackVolumeGestures.delete(gesture.id);
  const previous = gesture.previousTarget; gesture.previousTarget = null;
  gesture.heldKeys.clear();
  const pointerId = gesture.pointerId; gesture.pointerId = null; gesture.pointerType = '';
  try {
    if (pointerId !== null && gesture.range.hasPointerCapture?.(pointerId)) gesture.range.releasePointerCapture(pointerId);
  } catch { /* A disabled or detached native range may already have released capture. */ }
  const target = pendingTrackVolumes.get(gesture.id);
  if (active && !pointerReleased) {
    if (target?.path === gesture.path && target.readyAt === null) pendingTrackVolumes.delete(gesture.id);
    if (previous?.sequence === trackVolumeSequence && previous.path === gesture.path && currentTrackVolumeControl(gesture)) pendingTrackVolumes.set(gesture.id, previous);
    const retained = pendingTrackVolumes.get(gesture.id);
    const cue = trackVolumeCue(gesture.id, gesture.path);
    gesture.range.value = volumePercent(retained?.path === gesture.path ? retained.volume : currentTrackVolumeRequest(gesture.id, gesture.path) ? trackVolumeRequest.volume : cue?.volume);
    renderTrackVolume(gesture.range, gesture.output);
  }
  if (pointerId !== null && !pointerReleased) {
    // The native thumb has its own drag state. Disabling stops that tracking
    // when cancellation prevents its normal mouseup; availability rendering
    // restores only controls that still belong to the current editable cue.
    gesture.canceling = true;
    gesture.range.disabled = true;
    gesture.canceling = false;
  }
}
function cancelTrackVolumes() {
  ++trackVolumeSequence;
  clearTimeout(trackVolumeTimer); trackVolumeTimer = null;
  pendingTrackVolumes.clear();
  for (const gesture of [...trackVolumeGestures.values()]) cancelTrackVolumeGesture(gesture);
}
function pruneTrackVolumes() {
  for (const [id, target] of pendingTrackVolumes) {
    if (target.sequence !== trackVolumeSequence || !trackVolumeCue(id, target.path)) pendingTrackVolumes.delete(id);
  }
  for (const gesture of [...trackVolumeGestures.values()]) {
    if (!currentTrackVolumeControl(gesture)) cancelTrackVolumeGesture(gesture);
  }
}
function scheduleTrackVolumes() {
  clearTimeout(trackVolumeTimer); trackVolumeTimer = null;
  if (!pendingTrackVolumes.size || trackVolumeSending) return;
  if (!trackVolumeAvailable()) { cancelTrackVolumes(); return; }
  pruneTrackVolumes();
  const targets = [...pendingTrackVolumes].filter(([id, target]) => target.readyAt !== null && !trackVolumeGestures.has(id));
  if (!targets.length) return;
  const delay = Math.max(0, Math.min(...targets.map(([, target]) => target.readyAt)) - Date.now());
  if (playlistBusy || delay > 0) {
    trackVolumeTimer = setTimeout(() => { void saveTrackVolume(); }, playlistBusy ? Math.max(120, delay) : delay);
  } else void saveTrackVolume();
}
async function saveTrackVolume() {
  clearTimeout(trackVolumeTimer); trackVolumeTimer = null;
  if (!pendingTrackVolumes.size || trackVolumeSending) return;
  if (!trackVolumeAvailable()) { cancelTrackVolumes(); renderEditAvailability(); return; }
  pruneTrackVolumes();
  const ready = [...pendingTrackVolumes].filter(([id, target]) => target.readyAt !== null && !trackVolumeGestures.has(id) && target.readyAt <= Date.now()).sort((a, b) => a[1].readyAt - b[1].readyAt);
  if (playlistBusy || !ready.length) { scheduleTrackVolumes(); return; }
  const [id, target] = ready[0]; pendingTrackVolumes.delete(id);
  const cue = trackVolumeCue(id, target.path);
  if (!cue || volumePercent(cue.volume) === volumePercent(target.volume)) { renderEditAvailability(); scheduleTrackVolumes(); return; }
  const request = { ...target, id };
  trackVolumeRequest = request; trackVolumeSending = true; playlistBusy = true; renderEditAvailability();
  try {
    const confirmed = await api('PUT', '/api/playlist/volume', { expectedRevision: playlist.playlistRevision, cueId: id, volume: target.volume });
    if (request.sequence !== trackVolumeSequence) return;
    if (!trackVolumeAvailable()) { cancelTrackVolumes(); return; }
    acceptPlaylist(confirmed);
    localizedText($('playlist-revision'), () => t(playlist.cues.length === 1 ? '{0} cue · saved revision {1}' : '{0} cues · saved revision {1}', {0: playlist.cues.length, 1: playlist.playlistRevision}));
    // The PUT already confirms the complete playlist. State/SSE refresh may
    // continue independently while the next ready cue uses that saved revision.
    renderStageSettings(); void refreshState();
  } catch (error) {
    if (request.sequence !== trackVolumeSequence || !trackVolumeAvailable()) return;
    const discarded = new Map(pendingTrackVolumes);
    for (const gesture of trackVolumeGestures.values()) discarded.set(gesture.id, gesture);
    discarded.set(id, target); cancelTrackVolumes();
    const recoverySequence = trackVolumeSequence;
    notify(() => t("Track volume was not saved. {0}", {0: errorText(error)}), true);
    try {
      const current = await api('GET', '/api/playlist');
      if (recoverySequence === trackVolumeSequence && trackVolumeAvailable()) acceptPlaylist(current);
    } catch { /* Retain the last confirmed level. */ }
    if (recoverySequence === trackVolumeSequence && trackVolumeAvailable()) {
      for (const [cueId, previous] of discarded) {
        const row = playlistRows.get(cueId), saved = trackVolumeCue(cueId, previous.path);
        if (row?.volumePath === previous.path && saved && !pendingTrackVolumes.has(cueId) && !trackVolumeGestures.has(cueId)) {
          row.volume.value = volumePercent(saved.volume); renderTrackVolume(row.volume, row.volumeValue);
        }
      }
    }
  } finally {
    if (trackVolumeRequest === request) trackVolumeRequest = null;
    trackVolumeSending = false; playlistBusy = false; renderEditAvailability(); scheduleTrackVolumes();
  }
}
function flushTrackPlaylistRender() {
  if (trackPlaylistRenderPending && !trackVolumeGestures.size && !$('playlist').contains(document.activeElement)) renderPlaylist();
}
function endTrackVolumeGesture(gesture) {
  const current = currentTrackVolumeControl(gesture);
  const target = pendingTrackVolumes.get(gesture.id);
  if (current && !gesture.range.disabled && target?.path === gesture.path) target.readyAt = 0;
  cancelTrackVolumeGesture(gesture, true);
  scheduleTrackVolumes(); renderEditAvailability();
  setTimeout(flushTrackPlaylistRender, 0);
}
function bindTrackVolumeControl(id, path, range, output) {
  const gesture = { id, path, range, output, pointerId: null, pointerType: '', heldKeys: new Set(), previousTarget: null, canceling: false };
  range.addEventListener('pointerdown', event => {
    if (!event.isPrimary || event.button !== 0 || range.disabled || !trackVolumeAvailable() || playlistBusy && !trackVolumeSending || !currentTrackVolumeControl(gesture)) return;
    gesture.pointerId = event.pointerId; gesture.pointerType = event.pointerType;
    startTrackVolumeGesture(gesture);
    // WebKit's native range owns thumb tracking. Explicit pointer capture can
    // interrupt that drag and leave it following the mouse after release.
    scheduleTrackVolumes();
  });
  range.addEventListener('blur', () => {
    if (gesture.canceling || !currentTrackVolumeControl(gesture)) return;
    if (range.disabled || gesture.pointerId !== null) { cancelTrackVolumeGesture(gesture); scheduleTrackVolumes(); renderEditAvailability(); }
    else endTrackVolumeGesture(gesture);
  });
  range.addEventListener('lostpointercapture', event => {
    if (trackVolumeGestures.get(id) !== gesture || event.pointerId !== gesture.pointerId) return;
    cancelTrackVolumeGesture(gesture); scheduleTrackVolumes(); renderEditAvailability();
  });
  range.addEventListener('keydown', event => {
    if (!masterVolumeKeys.includes(event.key) || !trackVolumeAvailable() || playlistBusy && !trackVolumeSending || !currentTrackVolumeControl(gesture)) return;
    gesture.heldKeys.add(event.key); startTrackVolumeGesture(gesture);
  });
  range.addEventListener('keyup', event => {
    if (!masterVolumeKeys.includes(event.key) || !currentTrackVolumeControl(gesture)) return;
    gesture.heldKeys.delete(event.key);
    if (gesture.pointerId === null && !gesture.heldKeys.size) endTrackVolumeGesture(gesture);
  });
  range.addEventListener('change', () => { if (gesture.pointerId === null && !gesture.heldKeys.size && currentTrackVolumeControl(gesture)) endTrackVolumeGesture(gesture); });
  range.addEventListener('input', () => {
    if (!currentTrackVolumeControl(gesture)) return;
    const cue = trackVolumeCue(id, path);
    if (!trackVolumeAvailable() || playlistBusy && !trackVolumeSending) { range.value = volumePercent(cue.volume); renderTrackVolume(range, output); return; }
    renderTrackVolume(range, output);
    const volume = Number(range.value) / 100, previous = pendingTrackVolumes.get(id);
    if (previous && volumePercent(previous.volume) === volumePercent(volume)) return;
    if (!trackVolumeSending && volumePercent(cue.volume) === volumePercent(volume)) pendingTrackVolumes.delete(id);
    else pendingTrackVolumes.set(id, { path, volume, sequence: trackVolumeSequence, readyAt: trackVolumeGestures.get(id) === gesture ? null : Date.now() + 200 });
    scheduleTrackVolumes();
  });
  return gesture;
}
for (const event of ['pointerup', 'pointercancel']) document.addEventListener(event, pointer => {
  for (const gesture of [...trackVolumeGestures.values()]) {
    if (gesture.pointerId !== pointer.pointerId) continue;
    if (event === 'pointerup') endTrackVolumeGesture(gesture);
    else { cancelTrackVolumeGesture(gesture); scheduleTrackVolumes(); renderEditAvailability(); }
  }
}, true);
// Also observe native mouse release in case range tracking omits pointerup,
// before target handlers can replace or disable rows.
document.addEventListener('mouseup', event => {
  if (event.button !== 0) return;
  for (const gesture of [...trackVolumeGestures.values()]) {
    if (gesture.pointerId !== null && gesture.pointerType === 'mouse') endTrackVolumeGesture(gesture);
  }
}, true);
document.addEventListener('keyup', event => {
  for (const gesture of [...trackVolumeGestures.values()]) {
    if (!gesture.heldKeys.delete(event.key)) continue;
    if (gesture.pointerId === null && !gesture.heldKeys.size) endTrackVolumeGesture(gesture);
  }
});
window.addEventListener('blur', () => {
  for (const gesture of [...trackVolumeGestures.values()]) {
    if (gesture.pointerId !== null) cancelTrackVolumeGesture(gesture);
    else endTrackVolumeGesture(gesture);
  }
  for (const row of playlistRows.values()) if (row.volume === document.activeElement) endTrackVolumeGesture(row.volumeGesture);
  scheduleTrackVolumes(); renderEditAvailability();
});
$('playlist').addEventListener('focusout', () => { setTimeout(flushTrackPlaylistRender, 0); });
function clock(seconds) {
  if (!Number.isFinite(seconds) || seconds < 0) return '—';
  const whole = Math.floor(seconds); return `${Math.floor(whole / 60)}:${String(whole % 60).padStart(2, '0')}`;
}
function notify(message, error = false) {
  if (appClosed || reloadingAdmin) return;
  localizedText($('notice'), () => message); $('notice').classList.toggle('error', error);
}
function playbackPaused() { return Boolean(state?.paused ?? (state?.state === 'paused')); }
function activeMediaCue() {
  if (!state || !['loading', 'playing', 'paused'].includes(state.state)) return null;
  return state.cues.find(cue => cue.id === state.activeCueId && ['audio', 'video'].includes(cue.kind)) || null;
}
function transportIdentity() {
  return { instanceId: state.instanceId, stopEpoch: state.stopEpoch, cueId: state.activeCueId,
    generation: state.generation, transportRevision: state.transportRevision };
}
function sameTransport(value) {
  return state && value.instanceId === state.instanceId && value.stopEpoch === state.stopEpoch &&
    value.cueId === state.activeCueId && value.generation === state.generation &&
    value.transportRevision === state.transportRevision;
}
function sameForeground(value) {
  return value && state && value.instanceId === state.instanceId && value.stopEpoch === state.stopEpoch &&
    value.cueId === state.activeCueId && value.generation === state.generation;
}
function seekRequestPending() { return seekSending && sameForeground(seekTarget); }
function seekAvailable() {
  return Boolean(activeMediaCue() && ['playing', 'paused'].includes(state.state) &&
    Number.isFinite(state.duration) && state.duration > 0 && Number.isSafeInteger(state.transportRevision) &&
    online && Date.now() - lastSeen <= 18000 && csrf && !updatePending() &&
    !quitBusy && !appClosed && !reloadingAdmin);
}
function canSeek() {
  return seekAvailable() && !seekRequestPending() && !state.seekPending && !sameForeground(pendingTransport);
}
function cancelSeekGesture() {
  const previous = seekGesture;
  seekGesture = null;
  $('seek-preview').classList.remove('visible');
  if (previous?.pointerId !== undefined && $('seek-range').hasPointerCapture?.(previous.pointerId)) {
    $('seek-range').releasePointerCapture(previous.pointerId);
  }
}
function renderSeek() {
  const cue = activeMediaCue();
  const visible = Boolean(cue) && !appClosed && !quitBusy && !reloadingAdmin;
  if (seekGesture && (!canSeek() || !sameTransport(seekGesture))) cancelSeekGesture();
  $('seek-bar').hidden = !visible;
  document.body.classList.toggle('has-seek-bar', visible);
  // Keep keyboard focus during native acknowledgement, while all handlers guard input.
  $('seek-range').disabled = !visible || !seekAvailable();
  $('seek-range').setAttribute('aria-disabled', String(!canSeek()));
  if (!visible) { cancelSeekGesture(); return; }
  const duration = Number.isFinite(state.duration) && state.duration > 0 ? state.duration : 0;
  const pendingTarget = seekRequestPending();
  const position = seekGesture ? seekGesture.position : pendingTarget ? seekTarget.position : state.elapsed;
  if (!seekGesture) {
    $('seek-range').max = String(duration || 1);
    $('seek-range').value = String(Math.max(0, Math.min(duration, position || 0)));
  }
  localizedText($('seek-cue'), () => cue.label);
  localizedText($('seek-elapsed'), () => clock(position));
  localizedText($('seek-duration'), () => duration ? clock(duration) : '—');
  localizedAttribute($('seek-range'), 'aria-valuetext', () => t('{0} of {1}', {0: clock(position), 1: duration ? clock(duration) : '—'}));
  localizedText($('seek-status'), () => !online ? t('Disconnected') : state.state === 'loading' ? t('Loading…') :
    seekRequestPending() || state.seekPending ? t('Seeking…') : !duration ? t('Seeking unavailable') : playbackPaused() ? t('Paused') : '');
}
function beginSeek(kind, pointerId) {
  if (seekGesture || !canSeek()) return false;
  seekGesture = { ...transportIdentity(), kind, pointerId, position: Number($('seek-range').value), changed: false };
  localizedText($('seek-preview-time'), () => clock(seekGesture?.position ?? state?.elapsed));
  $('seek-preview').classList.add('visible');
  return true;
}
function previewSeek() {
  if (!seekGesture) return;
  if (!canSeek() || !sameTransport(seekGesture)) { cancelSeekGesture(); renderSeek(); return; }
  const value = Number($('seek-range').value);
  if (!Number.isFinite(value)) return;
  seekGesture.changed ||= Math.abs(value - seekGesture.position) > .01;
  seekGesture.position = Math.max(0, Math.min(state.duration, value));
  localizedText($('seek-preview-time'), () => clock(seekGesture?.position ?? value));
  renderSeek();
}
async function finishSeek(commit) {
  const gesture = seekGesture;
  const send = commit && gesture?.changed && canSeek() && sameTransport(gesture);
  cancelSeekGesture();
  if (!send) { renderSeek(); return; }
  const request = { requestId: requestID(), action: 'seek', instanceId: gesture.instanceId,
    stopEpoch: gesture.stopEpoch, cueId: gesture.cueId, generation: gesture.generation,
    transportRevision: gesture.transportRevision, position: Math.min(state.duration, gesture.position) };
  const sequence = ++controlSequence;
  seekSending = true; seekTarget = request; renderSeek();
  try { await api('POST', '/api/play', request); await refreshState(); }
  catch (error) { if (sequence === controlSequence) notify(() => errorText(error), true); await refreshState(); }
  finally { if (seekTarget === request) { seekSending = false; seekTarget = null; } renderSeek(); }
}
$('seek-range').addEventListener('pointerdown', event => {
  if (!canSeek()) { event.preventDefault(); return; }
  if (!event.isPrimary || event.button !== 0 || !beginSeek('pointer', event.pointerId)) return;
  try { event.currentTarget.setPointerCapture(event.pointerId); } catch { /* Native range tracking remains available. */ }
});
$('seek-range').addEventListener('input', () => {
  if (!seekGesture) {
    if (!beginSeek('assistive')) { renderSeek(); return; }
    seekGesture.changed = true;
  }
  previewSeek();
});
$('seek-range').addEventListener('change', () => {
  if (seekGesture?.kind === 'assistive') void finishSeek(true);
});
document.addEventListener('pointerup', event => {
  if (seekGesture?.kind === 'pointer' && event.pointerId === seekGesture.pointerId) void finishSeek(true);
});
document.addEventListener('pointercancel', event => {
  if (seekGesture?.pointerId === event.pointerId) { cancelSeekGesture(); renderSeek(); }
});
$('seek-range').addEventListener('lostpointercapture', event => {
  if (seekGesture?.pointerId === event.pointerId) { cancelSeekGesture(); renderSeek(); }
});
const seekKeys = ['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown', 'Home', 'End', 'PageUp', 'PageDown'];
$('seek-range').addEventListener('keydown', event => {
  if (event.key === 'Escape') { cancelSeekGesture(); renderSeek(); return; }
  if (!seekKeys.includes(event.key)) return;
  event.preventDefault();
  if (!canSeek()) return;
  if (seekGesture && seekGesture.kind !== 'keyboard' || !seekGesture && !beginSeek('keyboard')) return;
  const delta = ['ArrowLeft', 'ArrowDown'].includes(event.key) ? -1 :
    ['ArrowRight', 'ArrowUp'].includes(event.key) ? 1 : event.key === 'PageDown' ? -10 : 10;
  const position = event.key === 'Home' ? 0 : event.key === 'End' ? state.duration : seekGesture.position + delta;
  $('seek-range').value = String(Math.max(0, Math.min(state.duration, position)));
  previewSeek();
});
$('seek-range').addEventListener('keyup', event => {
  if (seekGesture?.kind === 'keyboard' && seekKeys.includes(event.key)) void finishSeek(true);
});
$('seek-range').addEventListener('blur', () => { cancelSeekGesture(); renderSeek(); });
window.addEventListener('blur', () => { cancelSeekGesture(); renderSeek(); });
document.addEventListener('visibilitychange', () => { if (document.hidden) { cancelSeekGesture(); renderSeek(); } });
function connection(connected) {
  if (appClosed || quitBusy || reloadingAdmin) return;
  online = connected;
  localizedText($('connection'), () => connected ? t("Connected to host") : expectingUpdateRestart() ? t("Restarting Smart Stage…") : t("Disconnected · status may be stale"));
  $('connection').className = connected ? 'live' : 'stale';
  renderRemoteStage();
  for (const [id, node] of cueButtons) {
    const cue = state?.cues.find(c => c.id === id);
    const selected = cue?.background ? state?.backgroundOverrideCueId === id : [state?.activeCueId, state?.imageCueId].includes(id);
    node.disabled = !connected || updatePending() || !cue || sameForeground(pendingTransport) && pendingTransport.cueId === id || !selected && ['missing', 'unsupported', 'error'].includes(cue.validation);
  }
  if (adminPage) { renderUpdateStatus(); renderEditAvailability(); }
  renderLanguageControls(); renderSeek(); renderMasterVolume();
}
function showPair() {
  if (appClosed || quitBusy || reloadingAdmin) return;
  if (!adminPage) window.smartStageWakeLock?.setConnected(false);
  connection(false); if (source) { source.close(); source = null; }
  if (adminPage) { void connectLocalAdmin(); return; }
  if (!$('pairing').open) $('pairing').showModal();
}
function pairError(message = '') {
  localizedText($('pair-error'), () => message); $('pair-error').hidden = !i18n.resolve(message);
}
async function connectLocalAdmin() {
  if (localSessionBusy || quitBusy || appClosed || reloadingAdmin) return;
  localSessionBusy = true; clearTimeout(localSessionRetry);
  try {
    const session = await api('POST', '/api/local-session', {});
    role = session.role; csrf = session.csrfToken;
    adminCapabilities = session.capabilities || {}; applyHostLanguage(session.language);
    if (role !== 'admin') throw new Error('Open Admin on the host computer.');
    void sendAdminPresence(true);
    if (!await initializeSession()) throw new Error('Could not load the host status.');
    notify(updateRestartComplete ? () => t("Smart Stage restarted. Use the new remote control link or QR code to reconnect phones and tablets.") : '');
    updateRestartComplete = false;
  } catch (error) {
    if (quitBusy || appClosed || reloadingAdmin) return;
    connection(false);
    if (expectingUpdateRestart()) notify(() => t("Smart Stage is restarting. Admin will reconnect automatically."));
    else notify(() => t("Cannot connect to Admin. {0} Retrying…", {0: errorText(error)}), true);
    localSessionRetry = setTimeout(() => { void connectLocalAdmin(); }, 5000);
  } finally { localSessionBusy = false; }
}
async function sendAdminPresence(force = false) {
  if (!adminPage || role !== 'admin' || !csrf || presenceBusy || quitBusy || reloadingAdmin || (!online && !force)) return;
  presenceBusy = true;
  try {
    const result = await api('POST', '/api/admin-presence', {});
    if (!appClosed && !quitBusy && !reloadingAdmin) applyHostLanguage(result.language);
    if (result.capabilities && !appClosed && !quitBusy && !reloadingAdmin) {
      adminCapabilities = result.capabilities;
      renderEditAvailability();
    }
  }
  catch { /* Presence is a best-effort hint, not a playback command. */ }
  finally { presenceBusy = false; }
}
function reloadAdmin() {
  if (reloadingAdmin) return;
  reloadingAdmin = true;
  clearTimeout(localSessionRetry);
  if (source) { source.close(); source = null; }
  location.reload();
}
async function probeClosedAdmin() {
  if (!appClosed || localSessionBusy || reloadingAdmin) return;
  clearTimeout(localSessionRetry);
  localSessionBusy = true;
  try {
    const session = await api('POST', '/api/local-session', {});
    if (session.role !== 'admin') return;
    role = session.role; csrf = session.csrfToken;
    void sendAdminPresence(true);
    const result = await api('GET', '/api/state');
    if (result.role === 'admin' && result.state.instanceId !== state?.instanceId) reloadAdmin();
  } catch { /* A deliberately closed app should not produce connection errors. */ }
  finally {
    localSessionBusy = false;
    if (appClosed && !reloadingAdmin) localSessionRetry = setTimeout(() => { void probeClosedAdmin(); }, 2000);
  }
}
function showAppClosed() {
  appClosed = true; quitBusy = false; online = false; role = ''; csrf = '';
  clearTimeout(localSessionRetry);
  if (source) { source.close(); source = null; }
  $('page-title').hidden = true; $('admin-view').hidden = true; $('playback-error').hidden = true;
  localizedText($('notice'), () => ''); $('notice').classList.remove('error');
  $('app-closed').hidden = false; $('stop').disabled = true; $('quit-app').disabled = true;
  localizedText($('connection'), () => t("Smart Stage is closed")); $('connection').className = '';
  localizedText($('play-state'), () => t("Closed")); localizedText($('current-cue'), () => t("No playback")); localizedText($('time'), () => '0:00');
  cancelSeekGesture(); renderSeek(); renderMasterVolume();
  localSessionRetry = setTimeout(() => { void probeClosedAdmin(); }, 2000);
}
$('quit-app').addEventListener('click', async () => {
  if (!adminPage || role !== 'admin' || !online || quitBusy || appClosed) return;
  quitBusy = true; cancelSeekGesture(); renderSeek(); $('quit-app').disabled = true; localizedText($('quit-app'), () => t("Closing…"));
  $('admin-view').inert = true;
  localizedText($('connection'), () => t("Closing Smart Stage…")); notify(() => t("Closing Smart Stage…"));
  try {
    const result = await api('POST', '/api/quit', {});
    if (result.quitting !== true) throw new Error('The host did not acknowledge the quit request.');
    showAppClosed();
  } catch (error) {
    quitBusy = false; localizedText($('quit-app'), () => t("Quit Smart Stage")); $('admin-view').inert = false;
    connection(online); notify(() => t("Quit is unconfirmed. {0}", {0: errorText(error)}), true); void refreshState();
  }
});
async function api(method, path, body) {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), appClosed || path === '/api/admin-presence' ? 2000 : path === '/api/stop' ? 5000 : 30000);
  try {
    const response = await fetch(endpointPath(path), {
      method, credentials: 'same-origin', cache: 'no-store', signal: controller.signal,
      headers: body === undefined ? {} : { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
      body: body === undefined ? undefined : JSON.stringify(body)
    });
    const result = await response.json();
    if (!response.ok) {
      if (response.status === 401) showPair();
      const error = new Error(result.error?.message || `Request failed (${response.status})`);
      error.code = result.error?.code; throw error;
    }
    return result;
  } catch (error) {
    if (error.name === 'AbortError') throw new Error('No acknowledgement from the host. Check connection and host status.');
    throw error;
  } finally { clearTimeout(timeout); }
}
async function refreshState() {
  if (refreshing || quitBusy || appClosed || reloadingAdmin) return;
  refreshing = true;
  try {
    const result = await api('GET', '/api/state');
    if (quitBusy || appClosed || reloadingAdmin) return false;
    role = result.role; csrf = result.csrfToken;
    if (adminPage) { adminCapabilities = result.capabilities || {}; applyHostLanguage(result.language); }
    applyState(result.state); return !reloadingAdmin;
  } catch (error) {
    if (quitBusy || appClosed || reloadingAdmin) return false;
    connection(false);
    if (expectingUpdateRestart()) notify(() => t("Smart Stage is restarting. Admin will reconnect automatically."));
    else if (!$('pairing').open) notify(() => errorText(error), true);
    return false;
  }
  finally { refreshing = false; }
}
function connectEvents() {
  if (quitBusy || appClosed || reloadingAdmin) return;
  if (source) source.close();
  source = new EventSource(endpointPath('/api/events'));
  source.addEventListener('state', event => {
    try {
      const next = JSON.parse(event.data);
      const gap = state && (next.instanceId !== state.instanceId || next.revision > state.revision + 1);
      lastSeen = Date.now(); applyState(next); connection(true);
      if (gap) void refreshState();
    } catch { connection(false); }
  });
  source.addEventListener('heartbeat', () => { lastSeen = Date.now(); });
  source.addEventListener('open', () => { void refreshState(); void sendAdminPresence(true); });
  source.addEventListener('error', () => { connection(false); void refreshState(); });
}
function applyState(next) {
  if (quitBusy || appClosed || reloadingAdmin) return;
  if (adminPage && state && next.instanceId !== state.instanceId) { reloadAdmin(); return; }
  if (state?.instanceId === next.instanceId && next.revision < state.revision) return;
  if (updateRestartInstance && next.instanceId !== updateRestartInstance) {
    updatePreparing = false; updateRestartInstance = ''; updateRestartStarted = 0; updateRestartComplete = true;
  }
  const updateReservationChanged = Boolean(state?.updatePending) !== Boolean(next.updatePending);
  if (state && (state.instanceId !== next.instanceId || state.stopEpoch !== next.stopEpoch)) cancelMasterVolume();
  if (masterVolumeConfirmation && next.revision >= masterVolumeConfirmation.revision) masterVolumeConfirmation = null;
  state = next;
  renderRemoteStage(); renderSeek(); renderMasterVolume();
  const current = next.cues.find(c => c.id === next.activeCueId);
  localizedText($('play-state'), () => t(next.state));
  localizedText($('current-cue'), () => current ? `${current.position}. ${current.label}` : next.state === 'error' ? t("Operator attention needed") : t("Ready when you are"));
  localizedText($('time'), () => `${clock(next.elapsed)}${next.duration > 0 ? ` / ${clock(next.duration)}` : ''}`);
  $('playback-error').hidden = !next.lastError; localizedText($('playback-error'), () => t(next.lastError));
  renderCues();
  if (adminPage && role === 'admin') {
    localizedText($('stage-state'), () => next.stageEnabled ? t("Stage output enabled · STOP returns to background") : t("Stage output disabled · music is independent"));
    renderBackgroundStatus();
    const job = next.validationJob;
    localizedText($('validate'), () => job.running ? t("Validating {0}/{1}…", {0: job.completed, 1: job.total}) : t("Validate all cues"));
    for (const c of next.cues) {
      const row = playlistRows.get(c.id);
      if (row) {
        const reason = playlist?.cues.find(item => item.id === c.id)?.cache.reason;
        localizedText(row.validation, () => `${t(c.kind) || t("Unknown type")} · ${c.duration ? clock(c.duration) : t("Duration unknown")} · ${t(c.validation)}${reason ? ` · ${t(reason)}` : ''}`);
      }
    }
    const signature = next.cues.map(c => `${c.id}:${c.validation}:${c.kind}:${c.duration}`).join('|');
    if (playlist && signature !== validationSignature) {
      validationSignature = signature; void refreshValidationDetails();
    }
    renderStatusDetails(next);
    renderEditAvailability(); renderUpdateStatus();
    if (updateReservationChanged) void loadUpdateStatus();
  }
}
function renderStatusDetails(next) {
  const current = next.cues.find(c => c.id === next.activeCueId), job = next.validationJob;
    const details = $('status-details'); details.replaceChildren();
    for (const [label, value] of [
      [t("Playback"), t(next.state)], [t("Current cue"), current?.label || t("None")],
      [t("Stage"), next.stageEnabled ? t("Enabled") : t("Disabled")], [t("Output selection"), next.outputFault ? t("Re-select outputs required") : t("Configured")],
      [t("Audio route"), next.resolvedAudioId ? (devices?.audio.find(d => d.id === next.resolvedAudioId)?.name || next.resolvedAudioId) : t("No active route")],
      [t("Background"), next.cues.find(c => c.id === next.backgroundCueId)?.label || t("None — black")],
      [t("Stage image"), next.cues.find(c => c.id === next.imageCueId)?.label || t("None")],
      [t("Background error"), t(next.backgroundError) || t("None")],
      [t("Playlist revision"), next.playlistRevision], [t("Validation"), job.running ? t("{0} of {1}", {0: job.completed, 1: job.total}) : t("Idle")]
    ]) { details.append(element('dt', label), element('dd', String(value))); }
}
function renderCues() {
  const visible = state.cues.filter(cue => !cue.hidden);
  const order = visible.map(c => c.id).join('|');
  for (const [id, node] of cueButtons) if (!visible.some(c => c.id === id)) { node.remove(); cueButtons.delete(id); }
  for (const cue of visible) {
    let node = cueButtons.get(cue.id);
    if (!node) {
      node = button('', () => trigger(cue.id), 'cue'); node.dataset.cueId = cue.id;
      node.append(element('span', '', 'cue-title'), element('span', '', 'cue-meta'));
      cueButtons.set(cue.id, node);
    }
    localizedText(node.firstChild, () => cue.label);
    const color = validCueColor(cue.color);
    node.classList.toggle('custom-color', Boolean(color));
    if (color) { node.style.setProperty('--cue-fill', color); node.style.setProperty('--cue-ink', cueTextColor(color)); }
    else { node.style.removeProperty('--cue-fill'); node.style.removeProperty('--cue-ink'); }
    const foreground = state.activeCueId === cue.id && ['loading', 'playing', 'paused'].includes(state.state);
    const image = state.stageEnabled && state.imageCueId === cue.id;
    const background = Boolean(cue.background && state.backgroundOverrideCueId === cue.id);
    const active = cue.background ? background : foreground || image;
    let action = cue.background ? t("Set background ↗") : cue.kind === 'image' ? t("Show image ↗") : t("Start cue ↗");
    if (background) action = t(state.stage?.backgroundCueId ? "Restore default" : "Deselect background");
    if (!cue.background && image) action = t("Press again to stop");
    if (!cue.background && foreground) action = ['video', 'audio'].includes(cue.kind) ? t(playbackPaused() ? "Press again to resume" : "Press again to pause") : t(state.state);
    if (cue.validation !== 'ready' && !active) action = t(cue.validation);
    node.lastChild.replaceChildren(element('span', () => `${String(cue.position).padStart(2, '0')} · ${cue.background ? t("background ") : ''}${t(cue.kind || 'unchecked')}`), element('span', action));
    node.classList.toggle('active', active); node.classList.toggle('paused', !cue.background && foreground && Boolean(state.paused)); node.setAttribute('aria-pressed', String(active));
    node.disabled = !online || updatePending() || sameForeground(pendingTransport) && pendingTransport.cueId === cue.id || !active && ['missing', 'unsupported', 'error'].includes(cue.validation);
    if (renderedOrder !== order) $('cue-grid').append(node);
  }
  renderedOrder = order; $('empty-cues').hidden = visible.length > 0;
  localizedText($('empty-cues'), () => state.cues.length ? t("No visible buttons. Show cue buttons in Admin on the host computer.") : t("Your show is empty. Add cues on the host computer to get started."));
}
function validCueColor(value) { return /^#[0-9a-f]{6}$/i.test(value || '') ? value : ''; }
function cueTextColor(color) {
  const channels = [1, 3, 5].map(offset => parseInt(color.slice(offset, offset + 2), 16) / 255).map(value => value <= .04045 ? value / 12.92 : ((value + .055) / 1.055) ** 2.4);
  const luminance = channels[0] * .2126 + channels[1] * .7152 + channels[2] * .0722;
  return luminance > .179 ? '#000000' : '#ffffff';
}
function renderRemoteStage() {
  const enabled = Boolean(state?.stageEnabled), control = $('remote-stage');
  localizedText(control, () => enabled ? t("Stage on") : t("Stage off"));
  control.setAttribute('aria-pressed', String(enabled));
  localizedAttribute(control, 'aria-label', () => enabled ? t("Disable stage output") : t("Enable stage output"));
  control.disabled = !online || !state || (!enabled && (state.outputFault || updatePending()));
  localizedAttribute(control, 'title', () => enabled ? t("Close the stage display; music keeps playing") : t("Open the stage display; music keeps playing"));
}
async function trigger(cueId) {
  if (updatePending()) { notify(() => t("Smart Stage is preparing an update. Playback is unavailable until it finishes.")); return; }
  if (!online || Date.now() - lastSeen > 18000 || !state) { connection(false); notify(() => t("Disconnected: PLAY was not sent. Reconnect before triggering a cue."), true); return; }
  const request = { requestId: requestID(), instanceId: state.instanceId, stopEpoch: state.stopEpoch, cueId };
  const cue = state.cues.find(item => item.id === cueId);
  const foreground = activeMediaCue();
  if (!cue?.background && foreground?.id === cueId && Number.isSafeInteger(state.transportRevision)) {
    if (sameForeground(pendingTransport) && pendingTransport.cueId === cueId) return;
    Object.assign(request, transportIdentity(), { action: playbackPaused() ? 'resume' : 'pause' });
    pendingTransport = request;
  }
  cancelSeekGesture(); renderSeek(); renderCues(); renderEditAvailability();
  const sequence = ++controlSequence;
  try { await api('POST', '/api/play', request); if (sequence === controlSequence) notify(() => t("Cue accepted. Check host playback status.")); await refreshState(); }
  catch (error) { if (sequence === controlSequence) notify(() => errorText(error), true); await refreshState(); }
  finally {
    if (pendingTransport === request) pendingTransport = null;
    renderSeek(); renderCues(); renderEditAvailability();
  }
}
$('stop').addEventListener('click', async () => {
  cancelSeekGesture(); renderSeek(); cancelMasterVolume(true); renderMasterVolume();
  if (!csrf) { showPair(); return; }
  ++masterVolumeStopPending; renderMasterVolume();
  const sequence = ++controlSequence;
  notify(() => t("Sending STOP…"));
  try { await api('POST', '/api/stop', { requestId: requestID() }); if (sequence === controlSequence) notify(() => t("STOP accepted by host. Check the playback status for native completion.")); await refreshState(); }
  catch (error) { if (sequence === controlSequence) notify(() => t("STOP is unconfirmed. {0}", {0: errorText(error)}), true); }
  finally { --masterVolumeStopPending; renderMasterVolume(); }
});
$('pairing').addEventListener('cancel', event => event.preventDefault());
$('pair-form').addEventListener('submit', async event => {
  event.preventDefault(); pairError();
  const submit = $('pair-form').querySelector('button[type=submit]'); submit.disabled = true;
  let token = $('pair-key').value.trim(); $('pair-key').value = '';
  try {
    const result = await api('POST', '/api/pair', { key: token });
    role = result.role; csrf = result.csrfToken; $('pairing').close();
    await initializeSession();
  } catch (error) { pairError(() => errorText(error)); }
  finally { token = ''; submit.disabled = false; }
});
$('logout').addEventListener('click', async () => {
  try { await api('POST', '/api/logout', {}); csrf = ''; role = ''; pairError(); showPair(); } catch (error) { notify(() => errorText(error), true); }
});

function renderRemoteLink() {
  const index = remoteLinks.findIndex(link => link.url === $('remote-network').value);
  const link = remoteLinks[index];
  if (!link) return;
  const changed = selectedRemoteURL !== link.url;
  selectedRemoteURL = link.url;
  localizedText($('remote-url'), () => link.url); $('remote-url').href = link.url;
  $('open-remote-url').href = link.url;
  // The QR is served by the host, without sending the link to another service.
  if (changed || $('remote-qr').getAttribute('src') !== link.qrURL || ($('remote-qr').complete && !$('remote-qr').naturalWidth)) {
    $('remote-qr').src = link.qrURL;
    localizedText($('remote-message'), () => '');
  }
}
function clearRemoteLinks() {
  remoteLinks = []; selectedRemoteURL = '';
  $('remote-ready').hidden = true; $('remote-unavailable').hidden = false;
  $('remote-section').classList.remove('has-remote');
  $('remote-network').replaceChildren();
  $('remote-url').removeAttribute('href'); localizedText($('remote-url'), () => '');
  $('open-remote-url').removeAttribute('href'); $('remote-qr').removeAttribute('src');
  localizedText($('remote-code'), () => ''); localizedText($('remote-message'), () => '');
}
async function loadRemoteControl() {
  if (!adminPage || role !== 'admin' || remoteRefresh || quitBusy || appClosed || reloadingAdmin) return;
  remoteRefresh = true;
  const sequence = gatewaySequence;
  try {
    const result = await api('GET', '/api/remote-control');
    if (sequence !== gatewaySequence || quitBusy || appClosed || reloadingAdmin) return;
    remoteLinks = result.links || [];
    const publicMode = result.mode === 'gateway';
    localizedText($('remote-code'), () => result.token);
    $('remote-code-row').hidden = publicMode;
    localizedText($('remote-guidance'), () => publicMode ? t("Scan this QR code or open the link from any phone or tablet with an internet connection. Keep Smart Stage running.") : t("Connect your phone or tablet to the same network, then scan this QR code with its camera or open the link."));
    $('remote-ready').hidden = !remoteLinks.length;
    $('remote-section').classList.toggle('has-remote', remoteLinks.length > 0);
    $('remote-unavailable').hidden = remoteLinks.length > 0;
    if (!remoteLinks.length) {
      clearRemoteLinks();
      localizedText($('remote-unavailable'), () => publicMode ? t("The public gateway is not connected. The remote link and QR code appear after Smart Stage connects.") : t("No network address is available. Connect this computer to Wi-Fi or Ethernet to use remote control."));
      localizedText($('remote-message'), () => ''); return;
    }
    $('remote-network').replaceChildren(...remoteLinks.map(link => option(link.url, link.label)));
    $('remote-network-choice').hidden = remoteLinks.length < 2;
    $('remote-network').value = remoteLinks.some(link => link.url === selectedRemoteURL) ? selectedRemoteURL : remoteLinks[0].url;
    renderRemoteLink();
  } catch (error) {
    localizedText($('remote-message'), () => t("Could not refresh the remote control link. {0}", {0: errorText(error)}));
  } finally { remoteRefresh = false; }
}
$('remote-network').addEventListener('change', renderRemoteLink);
$('remote-qr').addEventListener('error', () => {
  if (selectedRemoteURL) localizedText($('remote-message'), () => t("The QR code could not load. Use the remote control link above."));
});
async function copyRemoteURL() {
  if (!selectedRemoteURL) return;
  try {
    if (!navigator.clipboard?.writeText) throw new Error('Clipboard unavailable');
    await navigator.clipboard.writeText(selectedRemoteURL);
  } catch {
    // Clipboard API is absent on ordinary HTTP LAN pages in many browsers.
    const previousFocus = document.activeElement;
    const copy = element('textarea'); copy.value = selectedRemoteURL; copy.readOnly = true;
    copy.className = 'clipboard-copy'; localizedAttribute(copy, 'aria-label', () => t("Remote control link"));
    document.body.append(copy); copy.select(); copy.setSelectionRange(0, copy.value.length);
    let copied = false;
    try { copied = document.execCommand('copy'); } catch { /* Show the manual fallback below. */ }
    finally { copy.remove(); previousFocus?.focus(); }
    if (!copied) { localizedText($('remote-message'), () => t("Select and copy the link above to share it.")); return; }
  }
  localizedText($('remote-message'), () => t("Remote control link copied."));
}
$('copy-remote-url').addEventListener('click', () => { void copyRemoteURL(); });

function renderGateway() {
  if (!adminPage) return;
  const publicDraft = $('gateway-mode').value === 'gateway';
  const stored = Boolean(gateway?.hasToken);
  const unavailable = gatewayBusy || !online || role !== 'admin' || updatePending();
  $('gateway-fields').hidden = !publicDraft;
  $('gateway-url').required = publicDraft;
  $('gateway-token').required = publicDraft && !stored;
  localizedAttribute($('gateway-token'), 'placeholder', () => stored ? t("Leave blank to keep saved token") : t("Token from the gateway installer"));
  localizedText($('gateway-token-hint'), () => stored ? t("Leave blank to use the saved token with this URL, or enter a replacement.") : t("Enter the token printed by the gateway installer."));
  for (const id of ['gateway-mode', 'gateway-url', 'gateway-token', 'save-gateway']) $(id).disabled = unavailable;
  $('reconnect-gateway').hidden = gateway?.mode !== 'gateway';
  $('reconnect-gateway').disabled = unavailable || gatewayDirty;
  const status = gateway?.status;
  const description = () => gateway?.mode === 'gateway' ? {
    unconfigured: t("Configure your gateway URL and token to connect. Local network remote access is disabled."),
    connecting: t("Connecting to public gateway… Local network remote access is disabled."),
    connected: t("Connected to public gateway. Local network remote access is disabled."),
    error: t("Public gateway is unavailable. Smart Stage will retry automatically. Local network remote access is disabled."),
    disabled: gateway?.url ? t("Public gateway is not connected. Local network remote access is disabled.") : t("Configure your gateway URL and token to connect. Local network remote access is disabled.")
  }[status] || t("Waiting for the public gateway connection…") : gateway ? t("Local network remote control is enabled.") : t("Loading connection settings…");
  localizedText($('gateway-status'), () => {
    const message = gateway?.message;
    const repeatsConnectedStatus = status === 'connected' && message === 'Public gateway connected. Local LAN access is disabled.';
    return description() + (message && !repeatsConnectedStatus ? ` ${t(message)}` : '');
  });
  $('gateway-status').classList.toggle('error', status === 'error');
  $('lan-firewall-guidance').hidden = gateway?.mode !== 'lan';
  localizedText($('network-note-title'), () => gateway?.mode === 'gateway' ? t("Public gateway over HTTPS") : t("Trusted LAN only"));
  localizedText($('network-note-text'), () => gateway?.mode === 'gateway' ? t("Remote commands travel through your gateway over HTTPS. Anyone with the remote link can control playback. Admin stays on this computer. HTTPS also allows supported phones and tablets to use Keep awake.") : t("HTTP traffic is not encrypted. Pairing protects control access, but cannot protect against someone listening on the network. Keep the host and controllers on a trusted network."));
}
function applyGateway(value) {
  if (value.mode === 'gateway' && (value.status !== 'connected' || (gateway?.remoteURL && gateway.remoteURL !== value.remoteURL))) {
    clearRemoteLinks();
    localizedText($('remote-unavailable'), () => t("The public gateway is not connected. The remote link and QR code appear after Smart Stage connects."));
  }
  if (value.status === 'connected' && gateway?.status !== 'connected' && !$('gateway-message').classList.contains('error')) localizedText($('gateway-message'), () => '');
  gateway = value;
  if (!gatewayDirty) { $('gateway-mode').value = value.mode; $('gateway-url').value = value.url || ''; }
  renderGateway();
}
async function loadGateway() {
  if (!adminPage || role !== 'admin' || gatewayRefresh || gatewayBusy || quitBusy || appClosed || reloadingAdmin) return;
  gatewayRefresh = true;
  const sequence = gatewaySequence;
  try {
    const value = await api('GET', '/api/gateway');
    if (sequence !== gatewaySequence || quitBusy || appClosed || reloadingAdmin) return;
    applyGateway(value);
    await loadRemoteControl();
  } catch (error) {
    if (sequence === gatewaySequence) localizedText($('gateway-status'), () => t("Could not refresh gateway status. {0}", {0: errorText(error)}));
  } finally { gatewayRefresh = false; }
}
for (const id of ['gateway-mode', 'gateway-url', 'gateway-token']) $(id).addEventListener('input', () => {
  gatewayDirty = true; localizedText($('gateway-message'), () => ''); renderGateway();
});
$('gateway-form').addEventListener('submit', async event => {
  event.preventDefault();
  if (gatewayBusy || !online || role !== 'admin' || updatePending()) return;
  const body = { mode: $('gateway-mode').value, url: $('gateway-url').value.trim() };
  if ($('gateway-token').value.trim()) body.token = $('gateway-token').value.trim();
  // Clear secrets before the request. They are never echoed by the status API
  // or retained in browser storage, even when saving fails.
  $('gateway-token').value = '';
  gatewayBusy = true; gatewaySequence++; renderGateway();
  localizedText($('gateway-message'), () => t("Saving connection…")); $('gateway-message').classList.remove('error');
  try {
    const result = await api('PUT', '/api/gateway', body);
    gatewayDirty = false; applyGateway(result);
    localizedText($('gateway-message'), () => result.restart ? t("Saved. Smart Stage is restarting to enable local network remote control. Approve its firewall setup and allow Smart Stage incoming connections when prompted.") : result.mode === 'gateway' ? t("Saved. The public link and QR code appear once connected.") : t("Saved. Local network remote control is enabled. Allow Smart Stage incoming connections if your firewall asks."));
    await loadRemoteControl();
  } catch (error) {
    localizedText($('gateway-message'), () => t("Could not save the connection. {0}", {0: errorText(error)}));
    $('gateway-message').classList.add('error');
  } finally { delete body.token; gatewayBusy = false; renderGateway(); }
});
$('reconnect-gateway').addEventListener('click', async () => {
  if (gatewayBusy || gatewayDirty || !online || role !== 'admin' || updatePending()) return;
  gatewayBusy = true; gatewaySequence++; renderGateway();
  localizedText($('gateway-message'), () => t("Reconnecting…")); $('gateway-message').classList.remove('error');
  try {
    applyGateway(await api('POST', '/api/gateway/reconnect', {}));
    await loadRemoteControl();
    localizedText($('gateway-message'), () => t("Reconnecting. Use the current link or QR code after the gateway connects."));
  } catch (error) { localizedText($('gateway-message'), () => errorText(error)); $('gateway-message').classList.add('error'); }
  finally { gatewayBusy = false; renderGateway(); }
});

function updatePending() { return Boolean(state?.updatePending || updatePreparing); }
function expectingUpdateRestart() {
  return Boolean(updateRestartInstance && updateRestartStarted && Date.now() - updateRestartStarted < 180000);
}
function renderEditAvailability() {
  if (!adminPage || !state) return;
  const trackAvailable = trackVolumeAvailable();
  if (!trackAvailable && (pendingTrackVolumes.size || trackVolumeGestures.size || trackVolumeRequest?.sequence === trackVolumeSequence)) cancelTrackVolumes();
  pruneTrackVolumes();
  renderGateway();
  $('quit-app').disabled = !online || role !== 'admin' || quitBusy || appClosed || reloadingAdmin;
  $('choose-files').hidden = adminCapabilities.chooseFiles !== true;
  $('choose-files').disabled = !online || role !== 'admin' || chooseFilesBusy || quitBusy || appClosed || updatePending() || playlistBusy || playlistFileBusy;
  const fileUnavailable = !online || role !== 'admin' || !playlist || playlistBusy || playlistFileBusy || chooseFilesBusy || quitBusy || appClosed || reloadingAdmin || updatePending();
  $('save-playlist-file').disabled = fileUnavailable || stageSettingsDirty;
  $('load-playlist-file').disabled = fileUnavailable || !['stopped', 'error'].includes(state.state) || state.stageEnabled;
  $('playlist-load-continue').disabled = $('load-playlist-file').disabled;
  $('reload-playlist').disabled = playlistBusy || playlistFileBusy;
  localizedText($('playlist-file-guidance'), () => stageSettingsDirty ? t("Save or reload your stage settings before saving a playlist file.") : !['stopped', 'error'].includes(state.state) || state.stageEnabled ? t("Stop playback and turn Stage off before loading a playlist.") : '');
  localizedText($('playlist-drop-title'), () => desktopAdmin ? t("Drop {0} files here", {0: t(fileManagerName)}) : adminCapabilities.chooseFiles === true ? windowsPlatform ? t("Add media from this PC") : t("Add media from this Mac") : t("Add media from this computer"));
  localizedText($('playlist-drop-hint'), () => desktopAdmin ? t("Use Choose Media, or drag files from {0} into this window. Originals stay in place; nothing is uploaded or copied.", {0: t(fileManagerName)}) : adminCapabilities.chooseFiles === true ? windowsPlatform ? t("Use Choose Media to select files on this PC. Browser drops cannot provide their original paths. You can also drag File Explorer files into the Smart Stage app window.") : t("Use Choose Media to select files on this Mac. Browser drops cannot provide their original paths. You can also drop files onto Smart Stage’s Dock icon.") : t("Browsers cannot read original file paths from a drop. Use Add files by path below, or open the Smart Stage app on Mac or Windows to choose files or drop them into its window."));
  $('media-path-settings').hidden = desktopAdmin || adminCapabilities.chooseFiles === true;
  for (const id of ['media-paths', 'add-media-paths']) $(id).disabled = !online || role !== 'admin' || !playlist || playlistBusy || playlistFileBusy || quitBusy || appClosed || updatePending();
  const pending = updatePending() || playlistFileBusy;
  $('save-outputs').disabled = pending || !['stopped', 'error'].includes(state.state);
  $('enable-stage').disabled = !online || pending || state.outputFault;
  $('disable-stage').disabled = !online;
  const editingStage = !online || pending || playlistBusy || !playlist;
  for (const id of ['background-cue', 'background-audio', 'audio-fade-enabled', 'visual-fade-enabled', 'save-stage-settings']) $(id).disabled = editingStage;
  $('reload-stage-settings').disabled = editingStage;
  $('reload-stage-settings').hidden = !stageSettingsDirty;
  for (const kind of ['audio', 'visual']) $(`${kind}-fade-seconds`).disabled = editingStage || !$(`${kind}-fade-enabled`).checked;
  $('validate').disabled = pending || state.validationJob.running;
  for (const id of ['audio-output', 'display-output', 'allow-primary']) $(id).disabled = pending;
  for (const [id, row] of playlistRows) {
    row.input.disabled = pending;
    row.play.disabled = pending || !online || sameForeground(pendingTransport) && pendingTransport.cueId === id;
    row.up.disabled = pending || row.first;
    row.down.disabled = pending || row.last;
    row.remove.disabled = pending || id === state.activeCueId;
    row.color.disabled = pending;
    row.resetColor.disabled = pending || !row.customColor;
    row.hidden.disabled = pending || playlistBusy;
    const cue = playlist?.cues.find(c => c.id === id);
    row.volumeControls.hidden = !['audio', 'video'].includes(cue?.cache.media.kind);
    const volumeDisabled = !trackAvailable || !trackVolumeCue(id, row.volumePath) || playlistBusy && !trackVolumeSending;
    if (volumeDisabled && trackVolumeGestures.get(id) === row.volumeGesture) cancelTrackVolumeGesture(row.volumeGesture);
    row.volume.disabled = volumeDisabled;
    if (!trackAvailable || document.activeElement !== row.volume && !trackVolumeGestures.has(id) && !pendingTrackVolumes.has(id) && !currentTrackVolumeRequest(id, row.volumePath)) {
      row.volume.value = volumePercent(cue?.volume);
    }
    renderTrackVolume(row.volume, row.volumeValue);
    const foreground = state.activeCueId === id && ['loading', 'playing', 'paused'].includes(state.state);
    const image = state.stageEnabled && state.imageCueId === id;
    const background = Boolean(cue?.background && state.backgroundOverrideCueId === id);
    const selected = cue?.background ? background : foreground || image;
    row.play.setAttribute('aria-pressed', String(Boolean(selected)));
    localizedText(row.play, () => cue?.background ? t(background ? state.stage?.backgroundCueId ? "Restore default" : "Deselect background" : "Set background") : cue?.cache.media.kind === 'image' ? t(image ? "Hide image" : "Show image") : foreground && ['video', 'audio'].includes(cue?.cache.media.kind) ? t(playbackPaused() ? "Resume" : "Pause") : t("Play"));
    row.background.disabled = pending || playlistBusy || !['image', 'video'].includes(cue?.cache.media.kind);
    row.backgroundLabel.hidden = !['image', 'video'].includes(cue?.cache.media.kind) && !cue?.background;
  }
  // A newer host revision can arrive during our PUT. Recheck when its busy
  // flag clears as well as on state events, even if no further event follows.
  if (online && playlist && state.playlistRevision > playlist.playlistRevision && !playlistBusy && !playlistFileBusy && !playlistRefresh) {
    if ($('playlist').contains(document.activeElement) || trackVolumeGestures.size || pendingTrackVolumes.size) notify(() => t("The playlist changed in another tab. Finish or discard your edit, then Reload."), true);
    else void loadPlaylist();
  }
  if (!trackVolumeSending && !playlistBusy && pendingTrackVolumes.size) scheduleTrackVolumes();
}
function releaseLink(value) {
  try {
    const url = new URL(value);
    if (url.origin === 'https://github.com' && !url.username && !url.password && !url.search && !url.hash &&
        /^\/arizzi74\/Smart-Stage\/releases\/tag\/v[0-9][A-Za-z0-9._-]*$/.test(url.pathname)) return url.href;
  } catch { /* Only this project's release pages may be opened. */ }
  return '';
}
function renderUpdateBanner() {
  const phase = updateStatus?.phase || 'idle';
  const outcome = updateStatus?.lastUpdate;
  const installing = ['downloading', 'restarting'].includes(phase) || updatePreparing || state?.updatePending && phase === 'available';
  const failed = phase === 'error' || !installing && phase !== 'checking' && ['error', 'rolled_back'].includes(outcome?.status);
  const checking = (phase === 'checking' || Boolean(state?.updatePending)) && !installing && !failed;
  const banner = $('update-banner');
  banner.hidden = !adminPage || !(failed || installing || checking);
  banner.classList.toggle('update-failed', failed);
  if (banner.hidden) return;
  const version = updateStatus?.latestVersion;
  localizedText($('update-banner-title'), () => failed ? t("Automatic update needs attention")
    : checking ? t(state?.updatePending ? "Checking for automatic updates" : "Checking for updates…")
    : phase === 'restarting' ? t("Restarting Smart Stage")
    : version ? t("Smart Stage is updating to {0}", {0: version}) : t("Smart Stage is updating"));
  localizedText($('update-banner-detail'), () => failed
    ? `${i18n.diagnostic(phase === 'error' ? updateStatus?.message : outcome?.message) || t("The update could not be completed.")} ${t("Check the Updates section below and try again when connected.")}`
    : checking ? t(state?.updatePending ? "Checking before playback starts. STOP remains available." : "Checking for a newer release.")
    : phase === 'restarting' || !online && expectingUpdateRestart() ? t("The app is restarting. This page will reconnect automatically; keep it open.")
    : phase === 'downloading' ? t("Downloading and verifying the update. The app will restart automatically. STOP remains available.")
    : t("Preparing the automatic update. The app will restart automatically. STOP remains available."));
}
function renderUpdateStatus() {
  if (!adminPage) return;
  renderUpdateBanner();
  const phase = updateStatus?.phase || 'idle';
  const busy = updateBusy || updatePending() || ['checking', 'downloading', 'restarting'].includes(phase);
  localizedText($('update-current'), () => updateStatus?.currentVersion || t("Loading…"));
  localizedText($('update-latest'), () => updateStatus?.latestVersion || t("Not checked yet"));
  const defaults = {
    idle: t("No newer version is available."), checking: t("Checking for updates…"),
    available: t("A new version is available."), downloading: t("Downloading and verifying the update…"),
    restarting: t("Restarting Smart Stage. Admin will reconnect automatically."),
    error: t("Could not check or install the update. Try again when connected."),
    unsupported: t("Automatic updates are not available for this installation.")
  };
  localizedText($('update-message'), () => i18n.diagnostic(updateStatus?.message) || (updateStatus ? defaults[phase] || t("Update status unavailable.") : t("Loading update status…")));
  $('update-message').classList.toggle('error', phase === 'error');
  const outcome = updateStatus?.lastUpdate;
  $('update-outcome').hidden = !outcome?.message;
  localizedText($('update-outcome'), () => i18n.diagnostic(outcome?.message) || '');
  $('update-outcome').classList.toggle('error', ['error', 'rolled_back'].includes(outcome?.status));
  const safeToInstall = state?.state === 'stopped' && !state.stageEnabled && !updatePending();
  localizedText($('update-requirements'), () => updatePending() ? (phase === 'checking' ? t("Checking for an update before starting. Playback and edits are temporarily unavailable; STOP remains available.") : t("Preparing the update. Playback and edits are temporarily unavailable; STOP remains available.")) :
    safeToInstall ? t("Ready to update when a new version is available.") : t("Stop playback and disable stage output before updating."));
  $('check-update').disabled = !online || busy || phase === 'unsupported';
  localizedText($('check-update'), () => phase === 'checking' ? t("Checking…") : t("Check for updates"));
  $('install-update').disabled = !online || busy || !safeToInstall || !updateStatus?.available || !updateStatus?.canInstall;
  localizedText($('install-update'), () => phase === 'downloading' ? t("Downloading…") : phase === 'restarting' ? t("Restarting…") : t("Update and restart"));
  const href = releaseLink(updateStatus?.releaseURL);
  $('update-release').hidden = !href;
  if (href) $('update-release').href = href; else $('update-release').removeAttribute('href');
}
function applyUpdateStatus(result) {
  if (!result || typeof result.phase !== 'string') return;
  updateStatus = result;
  if (['downloading', 'restarting'].includes(result.phase) && state && !updateRestartInstance) {
    updatePreparing = true; updateRestartInstance = state.instanceId; updateRestartStarted = Date.now();
  }
  if (result.phase === 'restarting' && updateRestartInstance && !updateRestartStarted) updateRestartStarted = Date.now();
  if (['idle', 'available', 'error', 'unsupported'].includes(result.phase)) {
    updatePreparing = false; updateRestartInstance = ''; updateRestartStarted = 0;
  }
}
async function loadUpdateStatus() {
  if (!adminPage || role !== 'admin' || updateBusy || quitBusy || appClosed || reloadingAdmin) return;
  updateBusy = true; updatePollAt = Date.now();
  try { applyUpdateStatus(await api('GET', '/api/update')); }
  catch (error) {
    if (!expectingUpdateRestart()) applyUpdateStatus({ ...updateStatus, phase: 'error', message: () => t("Update status unavailable. {0}", {0: errorText(error)}) });
  } finally { updateBusy = false; renderUpdateStatus(); renderEditAvailability(); }
}
async function updateAction(install) {
  if (!adminPage || role !== 'admin' || updateBusy || $(install ? 'install-update' : 'check-update').disabled) return;
  updateBusy = true; renderUpdateStatus();
  try {
    const result = await api('POST', `/api/update/${install ? 'install' : 'check'}`, {});
    if (install) {
      updatePreparing = true; updateRestartInstance = state.instanceId; updateRestartStarted = Date.now();
      notify(() => t("Preparing the update. Smart Stage will restart and Admin will reconnect automatically."));
    }
    applyUpdateStatus(result);
  } catch (error) {
    updateStatus = { ...updateStatus, phase: 'error', message: () => errorText(error) };
    updatePreparing = false; updateRestartInstance = ''; updateRestartStarted = 0;
  } finally { updateBusy = false; renderUpdateStatus(); renderEditAvailability(); }
}
$('check-update').addEventListener('click', () => { void updateAction(false); });
$('install-update').addEventListener('click', () => { void updateAction(true); });

function savedStageSignature(settings) {
  return JSON.stringify(Object.keys(settings || {}).sort().map(key => [key, settings[key]]));
}
function captureStageBackground() {
  const id = $('background-cue').value;
  stageBackgroundDraft = id ? { id, path: playlist?.cues.find(cue => cue.id === id)?.path } : null;
}
function beginStageDraft() {
  if (stageSettingsDirty) return;
  stageSettingsDirty = true;
  stageSettingsRevision = playlist?.playlistRevision || 0;
  stageSettingsBase = savedStageSignature(playlist?.stage);
  captureStageBackground();
}
function stageSettingsMessage(message, error = false) {
  localizedText($('stage-settings-message'), message);
  $('stage-settings-message').classList.toggle('error', error);
}
function showStageConflict() {
  stageSettingsMessage(() => t(stageSettingsConflict === 'background'
    ? "The draft background was removed or replaced. Choose it again, or discard the draft and reload saved settings."
    : "Saved stage settings changed in another session. Your draft is kept. Discard the draft and reload saved settings before editing again."), true);
}
function reconcileStageDraft(next) {
  if (!stageSettingsDirty) return;
  const previousConflict = stageSettingsConflict;
  const selected = stageBackgroundDraft && next.cues.find(cue => cue.id === stageBackgroundDraft.id);
  stageSettingsConflict = savedStageSignature(next.stage) !== stageSettingsBase ? 'settings'
    : stageBackgroundDraft && (!selected || selected.path !== stageBackgroundDraft.path) ? 'background' : '';
  if (!stageSettingsConflict) {
    // Cue labels, visibility, order and new files do not change the saved
    // Stage settings or the draft's selected source. Retain the draft while
    // advancing its optimistic revision to this verified snapshot.
    stageSettingsRevision = next.playlistRevision;
    if (previousConflict) stageSettingsMessage(() => t("Unsaved changes."));
  } else showStageConflict();
}
function acceptPlaylist(next) {
  // A GET begun before a successful PUT may arrive afterwards. Revisions
  // are monotonic within this host instance; instance changes reload Admin.
  if (quitBusy || appClosed || reloadingAdmin || playlist && next.playlistRevision < playlist.playlistRevision) return false;
  reconcileStageDraft(next);
  playlist = next;
  pruneTrackVolumes();
  return true;
}
async function loadPlaylist(announceAdditions = true) {
  if (playlistRefresh) { playlistRefreshPending = true; return; }
  playlistRefresh = true;
  try {
    do {
      playlistRefreshPending = false;
      const previous = playlist;
      const accepted = acceptPlaylist(await api('GET', '/api/playlist'));
      if (accepted) renderPlaylist();
      // Validation may have finished while this full playlist snapshot loaded.
      // Refresh derived details after committing it, without rebuilding drafts.
      void refreshValidationDetails();
      if (accepted && announceAdditions && desktopAdmin && previous && previous.playlistRevision !== playlist.playlistRevision) {
        const previousIDs = new Set(previous.cues.map(cue => cue.id));
        const added = playlist.cues.filter(cue => !previousIDs.has(cue.id)).length;
        if (added) fileDropMessage(() => t(added === 1 ? 'Added {0} file to the playlist. Originals stay in place.' : 'Added {0} files to the playlist. Originals stay in place.', {0: added}));
      }
    } while (!quitBusy && !appClosed && !reloadingAdmin && (playlistRefreshPending || state?.playlistRevision > playlist?.playlistRevision));
  }
  catch (error) { notify(() => errorText(error), true); }
  finally { playlistRefresh = false; }
}
async function refreshValidationDetails() {
  // Final validation can arrive while an older playlist snapshot is in flight.
  // Remember that change and drain one current request after it, even if no
  // further state events arrive. Never apply the already superseded snapshot.
  if (validationRefresh) { validationRefreshPending = true; return; }
  validationRefresh = true;
  try {
    do {
      validationRefreshPending = false;
      try {
        const current = await api('GET', '/api/playlist');
        if (!validationRefreshPending && playlist && current.playlistRevision === playlist.playlistRevision) {
          for (const cue of current.cues) {
            const existing = playlist.cues.find(c => c.id === cue.id);
            if (existing) existing.cache = cue.cache;
            const row = playlistRows.get(cue.id);
            if (row) localizedText(row.validation, () => `${t(cue.cache.media.kind) || t("Unknown type")} · ${cue.cache.media.duration ? clock(cue.cache.media.duration) : t("Duration unknown")} · ${t(cue.cache.status)}${cue.cache.reason ? ` · ${t(cue.cache.reason)}` : ''}`);
          }
        }
      } catch (error) {
        validationSignature = '';
        notify(() => errorText(error), true);
      }
    } while (validationRefreshPending && !quitBusy && !appClosed && !reloadingAdmin);
  } finally { validationRefresh = false; renderStageSettings(); renderEditAvailability(); }
}
function cueEdits() { return playlist.cues.map(({ id, label, path, color, hidden, background, volume }) => ({ id, label, path, color: color || '', hidden: Boolean(hidden), background: Boolean(background), volume: Number.isFinite(volume) ? volume : .5 })); }
async function savePlaylist(cues) {
  if (updatePending()) { notify(() => t("Smart Stage is preparing an update. Wait before editing the show.")); return false; }
  if (playlistBusy || playlistFileBusy) { notify(() => t("An edit is being saved. Wait before making another edit."), true); return false; }
  playlistBusy = true; renderEditAvailability();
  try {
    acceptPlaylist(await api('PUT', '/api/playlist', { expectedRevision: playlist.playlistRevision, cues }));
    renderPlaylist(); notify(() => t("Playlist saved.")); await refreshState(); return true;
  } catch (error) { notify(() => t("{0} Your edit was not saved. Reload to use the host version.", {0: errorText(error)}), true); return false; }
  finally { playlistBusy = false; renderEditAvailability(); }
}
function fileDropMessage(message, error = false) {
  localizedText($('file-drop-message'), () => message);
  $('file-drop-message').classList.toggle('error', error);
}
function playlistFileMessage(message, error = false) {
  localizedText($('playlist-file-message'), () => message);
  $('playlist-file-message').classList.toggle('error', error);
}
function playlistLoaded() {
  stageSettingsDirty = false; stageSettingsConflict = ''; stageSettingsBase = ''; stageBackgroundDraft = null;
  localizedText($('stage-settings-message'), () => '');
  $('stage-settings-message').classList.remove('error');
}
async function nativePlaylistFile(operation, expectedRevision) {
  playlistFileBusy = true; renderEditAvailability();
  playlistFileMessage(() => t("Choose a playlist file in the app dialog."));
  try {
    let job = await api('POST', '/api/playlist/file', { operation, expectedRevision });
    const id = job.id;
    if (!id || job.operation !== operation) throw new Error('The playlist file request could not be confirmed.');
    while (['choosing', 'saving', 'loading'].includes(job.phase)) {
      await new Promise(resolve => setTimeout(resolve, 500));
      if (appClosed || quitBusy || reloadingAdmin) return;
      job = await api('GET', '/api/playlist/file');
      if (job.id !== id || job.operation !== operation) throw new Error('The playlist file request could not be confirmed.');
    }
    if (job.phase === 'cancelled') { playlistFileMessage(() => t("Cancelled. The current playlist is unchanged.")); return; }
    if (job.phase !== 'complete') throw new Error(job.message || 'The playlist file request could not be confirmed.');
    if (operation === 'load') {
      playlistLoaded(); await loadPlaylist(false); await refreshState();
      playlistFileMessage(() => t("Playlist loaded. Media files stay at their original paths."));
    } else playlistFileMessage(() => t("Playlist file saved."));
  } catch (error) {
    playlistFileMessage(() => t(operation === 'load' ? "Could not load the playlist. {0}" : "Could not save the playlist. {0}", {0: errorText(error)}), true);
  } finally { playlistFileBusy = false; renderEditAvailability(); }
}
$('save-playlist-file').addEventListener('click', async () => {
  if (!adminPage || $('save-playlist-file').disabled) return;
  if (adminCapabilities.playlistFiles === true) { void nativePlaylistFile('save', playlist.playlistRevision); return; }
  playlistFileBusy = true; renderEditAvailability();
  try {
    const file = await api('GET', '/api/playlist/export');
    const url = URL.createObjectURL(new Blob([JSON.stringify(file, null, 2) + '\n'], {type: 'application/json'}));
    const link = document.createElement('a'); link.href = url; link.download = 'Playlist.smartstage.json';
    document.body.append(link); link.click(); link.remove();
    // Keep the Blob alive until the browser has accepted its download.
    setTimeout(() => URL.revokeObjectURL(url), 30000);
    playlistFileMessage(() => t("Playlist download started. Check your browser’s downloads."));
  } catch (error) { playlistFileMessage(() => t("Could not save the playlist. {0}", {0: errorText(error)}), true); }
  finally { playlistFileBusy = false; renderEditAvailability(); }
});
$('load-playlist-file').addEventListener('click', () => {
  if (adminPage && !$('load-playlist-file').disabled && !$('playlist-load-confirm').open) $('playlist-load-confirm').showModal();
});
$('playlist-load-cancel').addEventListener('click', () => $('playlist-load-confirm').close());
$('playlist-load-continue').addEventListener('click', () => {
  if (!adminPage || $('load-playlist-file').disabled) return;
  $('playlist-load-confirm').close();
  playlistFileRevision = playlist.playlistRevision;
  if (adminCapabilities.playlistFiles === true) { void nativePlaylistFile('load', playlistFileRevision); return; }
  $('playlist-file-input').value = ''; $('playlist-file-input').click();
});
$('playlist-file-input').addEventListener('cancel', () => playlistFileMessage(() => t("Cancelled. The current playlist is unchanged.")));
$('playlist-file-input').addEventListener('change', async () => {
  const file = $('playlist-file-input').files?.[0]; $('playlist-file-input').value = '';
  if (!file) return;
  if (!adminPage || $('load-playlist-file').disabled) {
    playlistFileMessage(() => t("Stop playback and turn Stage off before loading a playlist."), true); return;
  }
  if (file.size > 4 * 1024 * 1024) { playlistFileMessage(() => t("Choose a playlist file no larger than 4 MiB."), true); return; }
  playlistFileBusy = true; renderEditAvailability();
  try {
    let fileDocument;
    try { fileDocument = JSON.parse(await file.text()); }
    catch { throw new Error('This file is not valid playlist JSON.'); }
    const loaded = await api('POST', '/api/playlist/import', { expectedRevision: playlistFileRevision, playlist: fileDocument });
    playlistLoaded(); acceptPlaylist(loaded); renderPlaylist(); await refreshState();
    playlistFileMessage(() => t("Playlist loaded. Media files stay at their original paths."));
  } catch (error) { playlistFileMessage(() => t("Could not load the playlist. {0}", {0: errorText(error)}), true); }
  finally { playlistFileBusy = false; renderEditAvailability(); }
});
$('choose-files').addEventListener('click', async () => {
  if (!adminPage || adminCapabilities.chooseFiles !== true || $('choose-files').disabled) return;
  chooseFilesBusy = true; renderEditAvailability();
  try {
    const result = await api('POST', '/api/choose-files', {});
    if (result.choosing !== true) throw new Error('The host did not open the file chooser.');
    fileDropMessage(() => t("Choose files in the {0} dialog. Originals stay in place.", {0: windowsPlatform ? 'Windows' : 'Mac'}));
  } catch (error) { fileDropMessage(() => errorText(error), true); }
  finally { chooseFilesBusy = false; renderEditAvailability(); }
});
$('media-path-form').addEventListener('submit', async event => {
  event.preventDefault();
  if ($('media-path-settings').hidden || !online || role !== 'admin' || !playlist || playlistBusy || playlistFileBusy || updatePending()) return;
  const paths = [...new Set($('media-paths').value.split(/\r?\n/).map(value => {
    const path = value.trim();
    return path.length > 1 && ['"', "'"].includes(path[0]) && path.at(-1) === path[0] ? path.slice(1, -1) : path;
  }).filter(Boolean))];
  if (!paths.length) { fileDropMessage(() => t("Enter one original file path per line."), true); return; }
  if (paths.some(path => !/^(?:\/|[A-Za-z]:[\\/]|\\\\)/.test(path))) {
    fileDropMessage(() => t("Use absolute file paths on the computer running Smart Stage, one per line."), true); return;
  }
  if (playlist.cues.length + paths.length > 500) { fileDropMessage(() => t("A playlist can contain up to 500 cues. Add fewer files."), true); return; }
  if (await savePlaylist([...cueEdits(), ...paths.map(path => ({ id: '', label: '', path }))])) {
    $('media-paths').value = '';
    fileDropMessage(() => t(paths.length === 1 ? 'Added {0} file to the playlist. Originals stay in place.' : 'Added {0} files to the playlist. Originals stay in place.', {0: paths.length}));
  } else fileDropMessage(() => t("The files were not added. Check the message above and try again."), true);
});
function dragHasType(event, type) { return Array.from(event.dataTransfer?.types || []).includes(type); }
if (adminPage) {
  document.addEventListener('dragover', event => {
    if (!dragHasType(event, 'Files')) return;
    event.preventDefault();
    event.dataTransfer.dropEffect = 'none';
  });
  document.addEventListener('drop', event => {
    if (!dragHasType(event, 'Files')) return;
    event.preventDefault();
    const message = () => desktopAdmin ? t("Drop files directly from {0} onto this Smart Stage window, or use Choose Media. Originals stay in place.", {0: t(fileManagerName)}) : adminCapabilities.chooseFiles === true ? windowsPlatform ? t("Your browser cannot read File Explorer file paths. Use Choose Media, or drop files into the Smart Stage app window.") : t("Your browser cannot read Finder file paths. Use Choose Media, or drop files onto the Smart Stage Dock icon.") : t("Your browser cannot read original file paths from a drop. Use Add files by path in the Playlist, or drop files into the Smart Stage app on Mac or Windows.");
    fileDropMessage(() => message); notify(() => message);
  });
}
function renderPlaylist() {
  pruneTrackVolumes();
  const rows = [...playlistRows];
  const sameLayout = rows.length === playlist.cues.length && playlist.cues.every((cue, index) => rows[index][0] === cue.id && rows[index][1].volumePath === cue.path);
  if (sameLayout && (trackVolumeGestures.size || rows.some(([, row]) => row.volume === document.activeElement && currentTrackVolumeControl(row.volumeGesture)))) {
    trackPlaylistRenderPending = true; renderStageSettings(); renderEditAvailability(); return;
  }
  trackPlaylistRenderPending = false;
  for (const gesture of [...trackVolumeGestures.values()]) cancelTrackVolumeGesture(gesture);
  playlistRows.clear();
  for (const [, row] of rows) row.volume.disabled = true;
  $('playlist').replaceChildren();
  localizedText($('playlist-revision'), () => t(playlist.cues.length === 1 ? '{0} cue · saved revision {1}' : '{0} cues · saved revision {1}', {0: playlist.cues.length, 1: playlist.playlistRevision}));
  if (!playlist.cues.length) $('playlist').append(element('p', () => t("Add audio, videos, or images to build your show."), 'empty'));
  const counts = new Map(); for (const c of playlist.cues) counts.set(c.label, (counts.get(c.label) || 0) + 1);
  playlist.cues.forEach((cue, index) => {
    const row = element('div', undefined, 'playlist-row'), info = element('div', undefined, 'playlist-info');
    const input = element('input'); input.value = cue.label; input.maxLength = 512; localizedAttribute(input, 'aria-label', () => t("Label for cue {0}", {0: index + 1}));
    input.addEventListener('change', () => { const edited = cueEdits(); edited[index].label = input.value; void savePlaylist(edited); });
    const validation = element('div', () => `${t(cue.cache.media.kind) || t("Unknown type")} · ${t(cue.cache.status)}${cue.cache.reason ? ` · ${t(cue.cache.reason)}` : ''}`, 'validation');
    info.append(input, element('p', cue.path, 'source-path'), validation);
    const colors = element('div', undefined, 'cue-color-controls'), colorLabel = element('label', () => t("Color"));
    const color = element('input'); color.type = 'color'; color.value = validCueColor(cue.color) || '#1b2227';
    localizedAttribute(color, 'aria-label', () => t("Color for cue {0}", {0: index + 1}));
    color.addEventListener('change', () => { const edited = cueEdits(); edited[index].color = color.value; void savePlaylist(edited); });
    colorLabel.append(color);
    const resetColor = button(() => t("Default"), () => { const edited = cueEdits(); edited[index].color = ''; void savePlaylist(edited); });
    localizedAttribute(resetColor, 'aria-label', () => t("Use default color for cue {0}", {0: index + 1}));
    colors.append(colorLabel, resetColor); info.append(colors);
    const volumeControls = element('div', undefined, 'track-volume-control');
    const volumeLabel = element('label', () => t("Track volume")), volume = element('input'), volumeValue = element('output');
    const pendingVolume = pendingTrackVolumes.get(cue.id);
    const draftVolume = pendingVolume?.path === cue.path ? pendingVolume.volume : currentTrackVolumeRequest(cue.id, cue.path) ? trackVolumeRequest.volume : cue.volume;
    volume.type = 'range'; volume.min = '0'; volume.max = '100'; volume.step = '1'; volume.value = volumePercent(draftVolume);
    // Explicit tabindex keeps Mac WebKit's native mouse press focused on the
    // range, rather than blurring it and canceling the thumb's new drag.
    volume.tabIndex = 0;
    volume.id = `track-volume-${cue.id}`; volumeLabel.htmlFor = volume.id; volumeValue.htmlFor = volume.id; renderTrackVolume(volume, volumeValue);
    localizedAttribute(volume, 'aria-label', () => t("Volume for cue {0}", {0: index + 1}));
    localizedAttribute(volume, 'aria-valuetext', () => trackVolumeText(volume));
    const volumeGesture = bindTrackVolumeControl(cue.id, cue.path, volume, volumeValue);
    const minus = element('span', '−', 'volume-direction'), plus = element('span', '+', 'volume-direction');
    minus.setAttribute('aria-hidden', 'true'); plus.setAttribute('aria-hidden', 'true');
    volumeControls.append(volumeLabel, minus, volume, plus, volumeValue); info.append(volumeControls);
    const options = element('div', undefined, 'cue-options');
    const hiddenLabel = element('label', undefined, 'check'), hidden = element('input'); hidden.type = 'checkbox'; hidden.checked = Boolean(cue.hidden);
    localizedAttribute(hidden, 'aria-label', () => t("Hide remote button for cue {0}", {0: index + 1}));
    hidden.addEventListener('change', () => { const edited = cueEdits(); edited[index].hidden = hidden.checked; void savePlaylist(edited); });
    hiddenLabel.append(hidden, localizedNode(() => t("Hide remote button")));
    const backgroundLabel = element('label', undefined, 'check'), background = element('input'); background.type = 'checkbox'; background.checked = Boolean(cue.background);
    localizedAttribute(background, 'aria-label', () => t("Use cue {0} as a background button", {0: index + 1}));
    background.addEventListener('change', () => { const edited = cueEdits(); edited[index].background = background.checked; void savePlaylist(edited); });
    backgroundLabel.append(background, localizedNode(() => t("Background button")));
    localizedAttribute(backgroundLabel, 'title', () => t("Pressing this button changes the stage background and keeps music playing. Press it again to restore the saved default, or black."));
    options.append(hiddenLabel, backgroundLabel); info.append(options);
    if (counts.get(cue.label) > 1) info.append(element('p', () => t("Duplicate label — use cue position to distinguish."), 'hint'));
    const tools = element('div', undefined, 'cue-tools');
    const move = delta => { const edited = cueEdits(); [edited[index], edited[index + delta]] = [edited[index + delta], edited[index]]; void savePlaylist(edited); };
    const up = button(() => t("↑ Up"), () => move(-1)); up.disabled = index === 0; localizedAttribute(up, 'aria-label', () => t("Move cue {0} up", {0: index + 1}));
    const down = button(() => t("↓ Down"), () => move(1)); down.disabled = index === playlist.cues.length - 1; localizedAttribute(down, 'aria-label', () => t("Move cue {0} down", {0: index + 1}));
    const remove = button(() => t("Remove"), () => { const edited = cueEdits(); edited.splice(index, 1); void savePlaylist(edited); });
    remove.disabled = state?.activeCueId === cue.id; localizedAttribute(remove, 'aria-label', () => t("Remove cue {0} from playlist", {0: index + 1}));
    const play = button(() => t("Play"), () => trigger(cue.id));
    tools.append(play, up, down, remove);
    row.append(element('span', String(index + 1).padStart(2, '0'), 'position'), info, tools);
    $('playlist').append(row); playlistRows.set(cue.id, { validation, remove, input, color, resetColor, hidden, background, backgroundLabel, volume, volumeValue, volumeControls, volumeGesture, volumePath: cue.path, customColor: Boolean(validCueColor(cue.color)), play, up, down, first: index === 0, last: index === playlist.cues.length - 1 });
  });
  renderStageSettings(); renderEditAvailability();
}
function renderBackgroundStatus() {
  if (!adminPage || !state) return;
  const current = state.cues.find(cue => cue.id === state.backgroundCueId);
  localizedText($('current-background'), () => t("Current background: {0}{1}{2}", {0: current?.label || t("None — black"), 1: state.stageEnabled ? '' : t(" · stage is off"), 2: state.backgroundError ? ` · ${t(state.backgroundError)}` : ''}));
}
function renderStageSettings() {
  if (!playlist || !adminPage) return;
  const settings = playlist.stage || {};
  const selected = stageSettingsDirty ? $('background-cue').value : settings.backgroundCueId || '';
  const options = [option('', () => t("None — black"))];
  for (const cue of playlist.cues) {
    if (['image', 'video'].includes(cue.cache.media.kind) && cue.cache.status === 'ready') options.push(option(cue.id, `${cue.label} · ${t(cue.cache.media.kind)}`));
  }
  if (selected && !options.some(item => item.value === selected)) {
    const cue = playlist.cues.find(item => item.id === selected);
    options.push(option(selected, () => `${cue?.label || t("Previous background")} · ${t('unavailable')}`));
  }
  $('background-cue').replaceChildren(...options); $('background-cue').value = selected;
  if (!stageSettingsDirty) {
    stageSettingsRevision = playlist.playlistRevision;
    $('background-audio').checked = Boolean(settings.backgroundAudio);
    for (const kind of ['audio', 'visual']) {
      $(`${kind}-fade-enabled`).checked = Boolean(settings[`${kind}FadeEnabled`]);
      $(`${kind}-fade-seconds`).value = settings[`${kind}FadeSeconds`] > 0 ? settings[`${kind}FadeSeconds`] : 1;
    }
  }
  renderBackgroundStatus();
}
for (const id of ['background-cue', 'background-audio', 'audio-fade-enabled', 'audio-fade-seconds', 'visual-fade-enabled', 'visual-fade-seconds']) $(id).addEventListener('input', () => {
  beginStageDraft();
  if (id === 'background-cue') captureStageBackground();
  reconcileStageDraft(playlist);
  if (!stageSettingsConflict) stageSettingsMessage(() => t("Unsaved changes."));
  renderEditAvailability();
});
$('stage-settings-form').addEventListener('submit', async event => {
  event.preventDefault();
  if (!online || !playlist || playlistBusy || playlistFileBusy || updatePending()) return;
  beginStageDraft(); reconcileStageDraft(playlist);
  if (stageSettingsConflict) { showStageConflict(); renderEditAvailability(); return; }
  const audioFadeSeconds = Number($('audio-fade-seconds').value);
  const visualFadeSeconds = Number($('visual-fade-seconds').value);
  for (const [kind, seconds] of [['audio', audioFadeSeconds], ['visual', visualFadeSeconds]]) {
    if (!Number.isFinite(seconds) || seconds < .1 || seconds > 30) {
      localizedText($('stage-settings-message'), () => kind === 'audio'
        ? t("Choose an audio fade duration from 0.1 to 30 seconds.")
        : t("Choose a video/image fade duration from 0.1 to 30 seconds."));
      $('stage-settings-message').classList.add('error'); return;
    }
  }
  const settings = {
    backgroundCueId: $('background-cue').value, backgroundAudio: $('background-audio').checked,
    audioFadeEnabled: $('audio-fade-enabled').checked, audioFadeSeconds,
    visualFadeEnabled: $('visual-fade-enabled').checked, visualFadeSeconds,
    toggleAudio: Boolean(playlist.stage?.toggleAudio)
  };
  playlistBusy = true; renderEditAvailability();
  try {
    const saved = await api('PUT', '/api/stage-settings', { expectedRevision: stageSettingsRevision || playlist.playlistRevision, settings });
    playlistLoaded(); acceptPlaylist(saved); renderPlaylist();
    localizedText($('stage-settings-message'), () => t("Stage and sound settings saved."));
    $('stage-settings-message').classList.remove('error'); await refreshState();
  } catch (error) {
    stageSettingsMessage(() => t("{0} Settings were not saved. Your draft is kept; retry or discard it and reload saved settings.", {0: errorText(error)}), true);
    if (error.code === 'revision_conflict') {
      try {
        acceptPlaylist(await api('GET', '/api/playlist')); renderPlaylist();
        if (stageSettingsConflict) showStageConflict();
        else stageSettingsMessage(() => t("The playlist was refreshed. Your stage changes are kept. Save again to apply them."));
      } catch { /* A failed refresh must retain the draft and its old revision. */ }
    }
  } finally { playlistBusy = false; renderEditAvailability(); }
});
async function reloadSavedPlaylist() {
  if (!online || playlistBusy || playlistFileBusy || updatePending()) return;
  playlistBusy = true; renderEditAvailability();
  try {
    const current = await api('GET', '/api/playlist');
    if (quitBusy || appClosed || reloadingAdmin) return;
    // Discard only after a successful read. A concurrent newer accepted
    // snapshot remains authoritative if this response is older.
    playlistLoaded(); acceptPlaylist(current); renderPlaylist();
    stageSettingsMessage(() => t("Saved stage and sound settings reloaded."));
  } catch (error) {
    stageSettingsMessage(() => t("Could not reload saved settings. Your draft is kept. {0}", {0: errorText(error)}), true);
  } finally { playlistBusy = false; renderEditAvailability(); }
}
$('reload-playlist').addEventListener('click', () => { void reloadSavedPlaylist(); });
$('reload-stage-settings').addEventListener('click', () => { void reloadSavedPlaylist(); });
$('validate').addEventListener('click', async () => {
  try { await api('POST', '/api/validate', {}); notify(() => t("Native validation started. STOP remains available.")); }
  catch (error) { notify(() => errorText(error), true); }
});
function option(value, label) { const node = element('option', label); node.value = value; return node; }
async function loadDevices() {
  try {
    devices = await api('GET', '/api/devices');
    const audio = $('audio-output'), display = $('display-output');
    audio.replaceChildren(option('default', () => t("System default (resolved for each cue)")), ...devices.audio.map(d => option(d.id, () => `${d.name}${d.default ? t(" · current default") : ''}`)));
    display.replaceChildren(option('', () => t("No stage display — audio only")), ...devices.displays.map(d => option(d.id, () => `${d.name} · ${d.width} × ${d.height}${d.primary ? t(" · Primary") : ''}${d.mirrored ? t(" · Mirrored") : ''}`)));
    const selected = state.outputs;
    if (selected.audioId !== 'default' && !devices.audio.some(d => d.id === selected.audioId)) audio.append(option(selected.audioId, () => t("Unavailable: {0}", {0: selected.audioId})));
    if (selected.displayId && !devices.displays.some(d => d.id === selected.displayId)) display.append(option(selected.displayId, () => t("Unavailable: {0}", {0: selected.displayId})));
    audio.value = selected.audioId; display.value = selected.displayId; $('allow-primary').checked = selected.allowPrimary;
    displayWarning();
  } catch (error) { notify(() => errorText(error), true); }
}
function displayWarning() {
  const d = devices?.displays.find(item => item.id === $('display-output').value);
  localizedText($('display-warning'), () => !d ? t("Choose a display to show images, videos, and backgrounds.") : d.mirrored ? t("This display is mirrored. The desktop cannot present an independent stage image.") : d.primary || devices.displays.length === 1 ? t("Warning: enabling stage output or a video cue covers the primary/only display.") : t("Images and videos fill the selected display while preserving their aspect ratio."));
}
$('display-output').addEventListener('change', () => { $('allow-primary').checked = false; displayWarning(); });
$('refresh-devices').addEventListener('click', () => loadDevices());
$('save-outputs').addEventListener('click', async () => {
  try {
    await api('PUT', '/api/outputs', { audioId: $('audio-output').value, displayId: $('display-output').value, allowPrimary: $('allow-primary').checked });
    notify(() => t("Outputs saved. Stage output is disabled until you enable it or trigger video.")); await refreshState();
  } catch (error) { notify(() => errorText(error), true); }
});
async function setStageOutput(enabled) {
  if (!csrf) { showPair(); return; }
  try { await api('POST', '/api/stage-output', { enabled }); notify(() => enabled ? t("Stage enable accepted. Music keeps playing.") : t("Stage disable accepted. Music keeps playing.")); await refreshState(); }
  catch (error) { notify(() => t("{0} is unconfirmed. {1}", {0: enabled ? t("Stage enable") : t("Stage disable"), 1: errorText(error)}), true); }
}
for (const [id, enabled] of [['enable-stage', true], ['disable-stage', false]]) $(id).addEventListener('click', () => { void setStageOutput(enabled); });
$('remote-stage').addEventListener('click', () => { void setStageOutput(!state?.stageEnabled); });
document.addEventListener('keydown', event => {
  if (event.key !== 'Escape' || event.repeat || !csrf || !online || $('pairing').open || $('playlist-load-confirm').open) return;
  event.preventDefault(); cancelSeekGesture(); renderSeek(); cancelMasterVolume(true);
  ++masterVolumeStopPending; renderMasterVolume();
  const sequence = ++controlSequence;
  void api('POST', '/api/emergency-stop', { requestId: requestID() }).then(async () => {
    if (sequence === controlSequence) notify(() => t("Emergency stop accepted. All sound stops and the stage closes."));
    await refreshState();
  }).catch(error => { if (sequence === controlSequence) notify(() => t("Emergency stop is unconfirmed. {0}", {0: errorText(error)}), true); })
    .finally(() => { --masterVolumeStopPending; renderMasterVolume(); });
});
async function initializeSession() {
  if (!await refreshState()) return false;
  if (adminPage && role !== 'admin') { notify(() => t("Open Admin on the host computer."), true); return false; }
  $('admin-view').hidden = !adminPage; $('command-view').hidden = adminPage;
  $('logout').hidden = adminPage;
  $('transport-tools').hidden = adminPage;
  if (!adminPage) window.smartStageWakeLock?.setConnected(role === 'command');
  connectEvents();
  if (adminPage) { await Promise.all([loadLanguage(), loadGateway(), loadRemoteControl(), loadPlaylist(), loadDevices(), loadUpdateStatus()]); }
  return true;
}
setInterval(() => { if (Date.now() - lastSeen > 18000) connection(false); }, 2000);
setInterval(() => { if (!document.hidden) void loadRemoteControl(); }, 10000);
setInterval(() => { if (!document.hidden) void loadGateway(); }, 3000);
setInterval(() => {
  const active = updatePending() || ['downloading', 'restarting'].includes(updateStatus?.phase);
  if ((!document.hidden || active) && Date.now() - updatePollAt >= (active ? 1000 : 5000)) void loadUpdateStatus();
}, 1000);
setInterval(() => { if (online && !appClosed) void sendAdminPresence(); }, 5000);
document.addEventListener('visibilitychange', () => {
  if (!document.hidden && appClosed) { void probeClosedAdmin(); return; }
  if (!document.hidden && csrf) { connection(false); void refreshState(); connectEvents(); void loadRemoteControl(); void loadGateway(); void sendAdminPresence(true); }
});
window.addEventListener('offline', () => connection(false));
window.addEventListener('online', () => {
  if (appClosed) { void probeClosedAdmin(); return; }
  if (csrf) { void refreshState(); connectEvents(); void loadRemoteControl(); void loadGateway(); }
  else if (adminPage) void connectLocalAdmin();
});
window.addEventListener('hashchange', () => { if (!adminPage && location.hash) void start(); });
async function start() {
  if (adminPage) { await connectLocalAdmin(); return; }
  let token = new URLSearchParams(location.hash.slice(1)).get('token');
  // Consume a shared link before making requests; never retain its token in browser storage.
  if (location.hash) history.replaceState(null, '', location.pathname + location.search);
  if (token === null) { await initializeSession(); return; }
  try {
    if (!(gatewayRoute ? /^[0-9a-fA-F]{64}$/ : /^[0-9]{8}$/).test(token)) throw new Error('This link has an invalid connection code. Scan the current QR code in Admin.');
    const session = await api('POST', '/api/pair', { key: token });
    role = session.role; csrf = session.csrfToken;
    if ($('pairing').open) $('pairing').close();
    pairError();
    await initializeSession();
  } catch (error) {
    showPair(); pairError(() => t("{0} Use the current link or QR code from Admin on the host computer.", {0: errorText(error)}));
  } finally { token = ''; }
}
window.addEventListener('smartstage-languagechange', () => {
  // Re-render only display-only summaries with cached translated values. Form
  // nodes and their drafts stay intact; changing language sends no commands.
  if (state && !appClosed) { renderCues(); if (adminPage) renderStatusDetails(state); }
  if (adminPage && !appClosed) { renderGateway(); renderUpdateStatus(); renderEditAvailability(); }
});
void start();

if (typeof ResizeObserver === 'function') {
  new ResizeObserver(entries => { document.documentElement.style.setProperty('--seek-height', entries[0].target.getBoundingClientRect().height + 'px'); }).observe($('seek-bar'));
  new ResizeObserver(entries => { document.documentElement.style.setProperty('--transport-height', `${entries[0].target.getBoundingClientRect().height}px`); }).observe(document.querySelector('.transport'));
}
