'use strict';
const $ = id => document.getElementById(id);
const adminPage = location.pathname === '/admin';
let state = null, role = '', csrf = '', online = false, source = null, lastSeen = 0;
let playlist = null, devices = null, fileSelection = new Set(), fileEntries = [];
let playlistBusy = false, refreshing = false, renderedOrder = '', playlistRefresh = false;
let controlSequence = 0;
let validationSignature = '', validationRefresh = false;
const cueButtons = new Map(), playlistRows = new Map();

function element(tag, text, className) {
  const node = document.createElement(tag);
  if (text !== undefined) node.textContent = text;
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
function clock(seconds) {
  if (!Number.isFinite(seconds) || seconds < 0) return '—';
  const whole = Math.floor(seconds); return `${Math.floor(whole / 60)}:${String(whole % 60).padStart(2, '0')}`;
}
function notify(message, error = false) {
  $('notice').textContent = message; $('notice').classList.toggle('error', error);
}
function connection(connected) {
  online = connected;
  $('connection').textContent = connected ? 'Connected to host' : 'Disconnected · status may be stale';
  $('connection').className = connected ? 'live' : 'stale';
  for (const [id, node] of cueButtons) {
    const cue = state?.cues.find(c => c.id === id);
    node.disabled = !connected || !cue || ['missing', 'unsupported', 'error'].includes(cue.validation);
  }
}
function showPair() {
  connection(false); if (source) { source.close(); source = null; }
  if (!$('pairing').open) $('pairing').showModal();
}
async function api(method, path, body) {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), path === '/api/stop' ? 5000 : 30000);
  try {
    const response = await fetch(path, {
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
  if (refreshing) return;
  refreshing = true;
  try {
    const result = await api('GET', '/api/state'); role = result.role; csrf = result.csrfToken;
    applyState(result.state); return true;
  } catch (error) { connection(false); if (!$('pairing').open) notify(error.message, true); return false; }
  finally { refreshing = false; }
}
function connectEvents() {
  if (source) source.close();
  source = new EventSource('/api/events');
  source.addEventListener('state', event => {
    try {
      const next = JSON.parse(event.data);
      const gap = state && (next.instanceId !== state.instanceId || next.revision > state.revision + 1);
      lastSeen = Date.now(); applyState(next); connection(true);
      if (gap) void refreshState();
    } catch { connection(false); }
  });
  source.addEventListener('heartbeat', () => { lastSeen = Date.now(); });
  source.addEventListener('open', () => { void refreshState(); });
  source.addEventListener('error', () => { connection(false); void refreshState(); });
}
function applyState(next) {
  if (state?.instanceId === next.instanceId && next.revision < state.revision) return;
  state = next;
  const current = next.cues.find(c => c.id === next.activeCueId);
  $('play-state').textContent = next.state;
  $('current-cue').textContent = current ? `${current.position}. ${current.label}` : next.state === 'error' ? 'Operator attention needed' : 'Ready when you are';
  $('time').textContent = `${clock(next.elapsed)}${next.duration > 0 ? ` / ${clock(next.duration)}` : ''}`;
  $('playback-error').hidden = !next.lastError; $('playback-error').textContent = next.lastError;
  renderCues();
  if (adminPage && role === 'admin') {
    $('stage-state').textContent = next.stageEnabled ? 'Stage output enabled · STOP retains black' : 'Stage output disabled';
    $('save-outputs').disabled = !['stopped', 'error'].includes(next.state);
    $('enable-stage').disabled = !['stopped', 'error'].includes(next.state) || next.outputFault;
    const job = next.validationJob;
    $('validate').textContent = job.running ? `Validating ${job.completed}/${job.total}…` : 'Validate all cues';
    $('validate').disabled = job.running;
    for (const c of next.cues) {
      const row = playlistRows.get(c.id);
      if (row) {
        const reason = playlist?.cues.find(item => item.id === c.id)?.cache.reason;
        row.validation.textContent = `${c.kind || 'Unknown type'} · ${c.duration ? clock(c.duration) : 'Duration unknown'} · ${c.validation}${reason ? ` · ${reason}` : ''}`;
        row.remove.disabled = c.id === next.activeCueId;
      }
    }
    const signature = next.cues.map(c => `${c.id}:${c.validation}`).join('|');
    if (playlist && signature !== validationSignature && !validationRefresh) {
      validationSignature = signature; void refreshValidationDetails();
    }
    const details = $('status-details'); details.replaceChildren();
    for (const [label, value] of [
      ['Playback', next.state], ['Current cue', current?.label || 'None'],
      ['Stage', next.stageEnabled ? 'Enabled' : 'Disabled'], ['Output selection', next.outputFault ? 'Re-select outputs required' : 'Configured'],
      ['Audio route', next.resolvedAudioId ? (devices?.audio.find(d => d.id === next.resolvedAudioId)?.name || next.resolvedAudioId) : 'No active route'],
      ['Playlist revision', next.playlistRevision], ['Validation', job.running ? `${job.completed} of ${job.total}` : 'Idle']
    ]) { details.append(element('dt', label), element('dd', String(value))); }
    if (playlist && playlist.playlistRevision !== next.playlistRevision && !playlistBusy && !playlistRefresh) {
      if ($('playlist').contains(document.activeElement)) notify('The playlist changed in another tab. Finish or discard your edit, then Reload.', true);
      else void loadPlaylist();
    }
  }
}
function renderCues() {
  const order = state.cues.map(c => c.id).join('|');
  for (const [id, node] of cueButtons) if (!state.cues.some(c => c.id === id)) { node.remove(); cueButtons.delete(id); }
  for (const cue of state.cues) {
    let node = cueButtons.get(cue.id);
    if (!node) {
      node = button('', () => trigger(cue.id), 'cue');
      node.append(element('span', '', 'cue-title'), element('span', '', 'cue-meta'));
      cueButtons.set(cue.id, node);
    }
    node.firstChild.textContent = cue.label;
    const active = state.activeCueId === cue.id && ['loading', 'playing'].includes(state.state);
    node.lastChild.replaceChildren(element('span', `${String(cue.position).padStart(2, '0')} · ${cue.kind || 'unchecked'}`), element('span', active ? state.state : cue.validation === 'ready' ? 'Start cue ↗' : cue.validation));
    node.classList.toggle('active', active); node.setAttribute('aria-pressed', String(active));
    node.disabled = !online || ['missing', 'unsupported', 'error'].includes(cue.validation);
    if (renderedOrder !== order) $('cue-grid').append(node);
  }
  renderedOrder = order; $('empty-cues').hidden = state.cues.length > 0;
}
async function trigger(cueId) {
  if (!online || Date.now() - lastSeen > 18000 || !state) { connection(false); notify('Disconnected: PLAY was not sent. Reconnect before triggering a cue.', true); return; }
  const request = { requestId: requestID(), instanceId: state.instanceId, stopEpoch: state.stopEpoch, cueId };
  const sequence = ++controlSequence;
  try { await api('POST', '/api/play', request); if (sequence === controlSequence) notify('Cue accepted. Check host playback status.'); await refreshState(); }
  catch (error) { if (sequence === controlSequence) notify(error.message, true); await refreshState(); }
}
$('stop').addEventListener('click', async () => {
  if (!csrf) { showPair(); return; }
  const sequence = ++controlSequence;
  notify('Sending STOP…');
  try { await api('POST', '/api/stop', { requestId: requestID() }); if (sequence === controlSequence) notify('STOP accepted by host. Check the playback status for native completion.'); await refreshState(); }
  catch (error) { if (sequence === controlSequence) notify(`STOP is unconfirmed. ${error.message}`, true); }
});
$('pairing').addEventListener('cancel', event => event.preventDefault());
$('pair-form').addEventListener('submit', async event => {
  event.preventDefault(); $('pair-error').textContent = '';
  try {
    const result = await api('POST', '/api/pair', { key: $('pair-key').value.trim() });
    $('pair-key').value = ''; role = result.role; csrf = result.csrfToken; $('pairing').close();
    await initializeSession();
  } catch (error) { $('pair-error').textContent = error.message; }
});
$('logout').addEventListener('click', async () => {
  try { await api('POST', '/api/logout', {}); csrf = ''; showPair(); } catch (error) { notify(error.message, true); }
});

async function loadPlaylist() {
  if (playlistRefresh) return;
  playlistRefresh = true;
  try { playlist = await api('GET', '/api/playlist'); renderPlaylist(); }
  catch (error) { notify(error.message, true); }
  finally { playlistRefresh = false; }
}
async function refreshValidationDetails() {
  validationRefresh = true;
  try {
    const current = await api('GET', '/api/playlist');
    if (playlist && current.playlistRevision === playlist.playlistRevision) {
      for (const cue of current.cues) {
        const existing = playlist.cues.find(c => c.id === cue.id);
        if (existing) existing.cache = cue.cache;
        const row = playlistRows.get(cue.id);
        if (row) row.validation.textContent = `${cue.cache.media.kind || 'Unknown type'} · ${cue.cache.media.duration ? clock(cue.cache.media.duration) : 'Duration unknown'} · ${cue.cache.status}${cue.cache.reason ? ` · ${cue.cache.reason}` : ''}`;
      }
    }
  } catch (error) { notify(error.message, true); }
  finally { validationRefresh = false; }
}
function cueEdits() { return playlist.cues.map(({ id, label, path }) => ({ id, label, path })); }
async function savePlaylist(cues) {
  if (playlistBusy) { notify('An edit is being saved. Wait before making another edit.', true); return false; }
  playlistBusy = true;
  try {
    playlist = await api('PUT', '/api/playlist', { expectedRevision: playlist.playlistRevision, cues });
    renderPlaylist(); notify('Playlist saved.'); await refreshState(); return true;
  } catch (error) { notify(`${error.message} Your edit was not saved. Reload to use the host version.`, true); return false; }
  finally { playlistBusy = false; }
}
function renderPlaylist() {
  $('playlist').replaceChildren(); playlistRows.clear();
  $('playlist-revision').textContent = `${playlist.cues.length} cues · saved revision ${playlist.playlistRevision}`;
  if (!playlist.cues.length) $('playlist').append(element('p', 'Build your show by adding files below.', 'empty'));
  const counts = new Map(); for (const c of playlist.cues) counts.set(c.label, (counts.get(c.label) || 0) + 1);
  playlist.cues.forEach((cue, index) => {
    const row = element('div', undefined, 'playlist-row'), info = element('div', undefined, 'playlist-info');
    const input = element('input'); input.value = cue.label; input.maxLength = 512; input.setAttribute('aria-label', `Label for cue ${index + 1}`);
    input.addEventListener('change', () => { const edited = cueEdits(); edited[index].label = input.value; void savePlaylist(edited); });
    const validation = element('div', `${cue.cache.media.kind || 'Unknown type'} · ${cue.cache.status}${cue.cache.reason ? ` · ${cue.cache.reason}` : ''}`, 'validation');
    info.append(input, element('p', cue.path, 'source-path'), validation);
    if (counts.get(cue.label) > 1) info.append(element('p', 'Duplicate label — use cue position to distinguish.', 'hint'));
    const tools = element('div', undefined, 'cue-tools');
    const move = delta => { const edited = cueEdits(); [edited[index], edited[index + delta]] = [edited[index + delta], edited[index]]; void savePlaylist(edited); };
    const up = button('↑ Up', () => move(-1)); up.disabled = index === 0; up.setAttribute('aria-label', `Move cue ${index + 1} up`);
    const down = button('↓ Down', () => move(1)); down.disabled = index === playlist.cues.length - 1; down.setAttribute('aria-label', `Move cue ${index + 1} down`);
    const remove = button('Remove', () => { const edited = cueEdits(); edited.splice(index, 1); void savePlaylist(edited); });
    remove.disabled = state?.activeCueId === cue.id; remove.setAttribute('aria-label', `Remove cue ${index + 1} from playlist`);
    tools.append(button('Play', () => trigger(cue.id)), up, down, remove);
    row.append(element('span', String(index + 1).padStart(2, '0'), 'position'), info, tools);
    $('playlist').append(row); playlistRows.set(cue.id, { validation, remove });
  });
}
async function browse(path = '') {
  $('file-message').textContent = 'Reading the host folder…';
  try {
    const listing = await api('GET', `/api/files?path=${encodeURIComponent(path)}`);
    $('host-path').value = listing.path; fileEntries = listing.entries; fileSelection.clear(); updateSelected();
    $('roots').replaceChildren(...listing.roots.map(root => { const option = element('option', root); option.value = root; return option; }));
    $('breadcrumbs').replaceChildren();
    if (listing.parent) $('breadcrumbs').append(button('↑ Parent', () => browse(listing.parent)));
    for (const crumb of listing.breadcrumbs) $('breadcrumbs').append(button(crumb.name, () => browse(crumb.path)));
    $('file-list').replaceChildren();
    for (const entry of listing.entries) {
      const row = element('div', undefined, 'file-row');
      if (entry.directory) {
        row.append(element('span', '▸'), button(entry.name, () => browse(entry.path), 'file-name'));
      } else {
        const check = element('input'); check.type = 'checkbox'; check.setAttribute('aria-label', `Select ${entry.name}`);
        check.addEventListener('change', () => { if (check.checked) fileSelection.add(entry.path); else fileSelection.delete(entry.path); updateSelected(); });
        const info = element('div', undefined, 'file-name');
        info.append(element('span', entry.name), element('div', `${(entry.size / 1024 / 1024).toFixed(2)} MB · ${new Date(entry.modified / 1e6).toLocaleDateString()}`, 'file-details'));
        const inspect = button('Inspect', async () => {
          inspect.disabled = true;
          try {
            const result = await api('POST', '/api/inspect', { path: entry.path });
            info.lastChild.textContent = `${result.media.kind || 'Unknown type'} · ${result.media.duration ? clock(result.media.duration) : 'Duration unknown'} · ${result.status}${result.reason ? ` · ${result.reason}` : ''}`;
          } catch (error) { info.lastChild.textContent = error.message; }
          finally { inspect.disabled = false; }
        });
        row.append(check, info, inspect);
      }
      $('file-list').append(row);
    }
    $('file-message').textContent = `${listing.entries.length} visible entries${listing.truncated ? ' · Listing limited to 1,000 entries; use a smaller folder.' : ''}`;
  } catch (error) { $('file-message').textContent = error.message; }
}
function updateSelected() { $('add-files').textContent = `Add selected (${fileSelection.size})`; $('add-files').disabled = fileSelection.size === 0; }
$('browse-form').addEventListener('submit', event => { event.preventDefault(); void browse($('host-path').value); });
$('roots').addEventListener('change', () => browse($('roots').value));
$('add-files').addEventListener('click', async () => {
  if (!playlist || !fileSelection.size) return;
  const additions = fileEntries.filter(entry => fileSelection.has(entry.path)).map(entry => ({ id: '', label: '', path: entry.path }));
  if (await savePlaylist([...cueEdits(), ...additions])) { fileSelection.clear(); $('file-list').querySelectorAll('input[type=checkbox]').forEach(node => { node.checked = false; }); updateSelected(); }
});
$('reload-playlist').addEventListener('click', () => loadPlaylist());
$('validate').addEventListener('click', async () => {
  try { await api('POST', '/api/validate', {}); notify('Native validation started. STOP remains available.'); }
  catch (error) { notify(error.message, true); }
});
function option(value, label) { const node = element('option', label); node.value = value; return node; }
async function loadDevices() {
  try {
    devices = await api('GET', '/api/devices');
    const audio = $('audio-output'), display = $('display-output');
    audio.replaceChildren(option('default', 'System default (resolved for each cue)'), ...devices.audio.map(d => option(d.id, `${d.name}${d.default ? ' · current default' : ''}`)));
    display.replaceChildren(option('', 'No stage display — audio only'), ...devices.displays.map(d => option(d.id, `${d.name} · ${d.width} × ${d.height}${d.primary ? ' · Primary' : ''}${d.mirrored ? ' · Mirrored' : ''}`)));
    const selected = state.outputs;
    if (selected.audioId !== 'default' && !devices.audio.some(d => d.id === selected.audioId)) audio.append(option(selected.audioId, `Unavailable: ${selected.audioId}`));
    if (selected.displayId && !devices.displays.some(d => d.id === selected.displayId)) display.append(option(selected.displayId, `Unavailable: ${selected.displayId}`));
    audio.value = selected.audioId; display.value = selected.displayId; $('allow-primary').checked = selected.allowPrimary;
    displayWarning();
  } catch (error) { notify(error.message, true); }
}
function displayWarning() {
  const d = devices?.displays.find(item => item.id === $('display-output').value);
  $('display-warning').textContent = !d ? 'Choose a display to use video cues.' : d.mirrored ? 'This display is mirrored. The desktop cannot present an independent stage image.' : d.primary || devices.displays.length === 1 ? 'Warning: enabling stage output or a video cue covers the primary/only display.' : 'Video fills only the selected display, with black bars to preserve its aspect ratio.';
}
$('display-output').addEventListener('change', () => { $('allow-primary').checked = false; displayWarning(); });
$('refresh-devices').addEventListener('click', () => loadDevices());
$('save-outputs').addEventListener('click', async () => {
  try {
    await api('PUT', '/api/outputs', { audioId: $('audio-output').value, displayId: $('display-output').value, allowPrimary: $('allow-primary').checked });
    notify('Outputs saved. Stage output is disabled until you enable it or trigger video.'); await refreshState();
  } catch (error) { notify(error.message, true); }
});
for (const [id, enabled] of [['enable-stage', true], ['disable-stage', false]]) $(id).addEventListener('click', async () => {
  try { await api('POST', '/api/stage-output', { enabled }); notify(enabled ? 'Stage enable accepted.' : 'Stop and stage disable accepted.'); await refreshState(); }
  catch (error) { notify(error.message, true); }
});
async function initializeSession() {
  if (!await refreshState()) return;
  if (adminPage && role !== 'admin') { location.replace('/command'); return; }
  $('admin-view').hidden = !adminPage; $('command-view').hidden = adminPage;
  $('page-heading').textContent = adminPage ? 'Set the stage' : 'Show control';
  $('other-page').href = adminPage ? '/command' : '/admin'; $('other-page').textContent = adminPage ? 'Open Command' : 'Admin';
  connectEvents();
  if (adminPage) { await Promise.all([loadPlaylist(), loadDevices(), browse()]); }
}
setInterval(() => { if (Date.now() - lastSeen > 18000) connection(false); }, 2000);
document.addEventListener('visibilitychange', () => {
  if (!document.hidden && csrf) { connection(false); void refreshState(); connectEvents(); }
});
window.addEventListener('offline', () => connection(false));
window.addEventListener('online', () => { if (csrf) { void refreshState(); connectEvents(); } });
void initializeSession();
