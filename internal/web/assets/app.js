'use strict';
const $ = id => document.getElementById(id);
const adminPage = location.pathname === '/admin';
// Only the gateway's endpoint route may scope remote requests. Admin always
// uses the loopback root; arbitrary paths cannot redirect privileged requests.
const gatewayRoute = /^\/smartstage\/e\/[A-Za-z0-9_-]{16,128}\/command$/.test(location.pathname);
const endpointPrefix = gatewayRoute ? location.pathname.slice(0, -'/command'.length) : '';
const endpointPath = path => endpointPrefix + path;
// The native user-agent token changes guidance only. It grants no API access
// and never lets JavaScript read or submit a Finder file's original path.
const desktopAdmin = adminPage && /(?:^|\s)SmartStageDesktop(?:\s|$)/.test(navigator.userAgent);
let state = null, role = '', csrf = '', online = false, source = null, lastSeen = 0;
let playlist = null, devices = null;
let playlistBusy = false, refreshing = false, renderedOrder = '', playlistRefresh = false;
let stageSettingsDirty = false, stageSettingsRevision = 0;
let controlSequence = 0;
let validationSignature = '', validationRefresh = false;
let localSessionBusy = false, localSessionRetry = null;
let presenceBusy = false, quitBusy = false, appClosed = false, reloadingAdmin = false;
let adminCapabilities = {}, chooseFilesBusy = false;
let remoteLinks = [], selectedRemoteURL = '', remoteRefresh = false;
let gateway = null, gatewayDirty = false, gatewayBusy = false, gatewayRefresh = false, gatewaySequence = 0;
let updateStatus = null, updateBusy = false, updatePreparing = false;
let updateRestartInstance = '', updateRestartComplete = false, updateRestartStarted = 0;
const cueButtons = new Map(), playlistRows = new Map();
document.body.classList.toggle('remote-page', !adminPage);
$('page-title').hidden = !adminPage;
if (gatewayRoute) {
  $('pair-guidance').textContent = 'Scan the current QR code or open the remote control link shown in Admin on the host computer. You can also paste the access key from that link below.';
  $('pair-key-label').textContent = 'Access key';
  $('pair-key').inputMode = 'text'; $('pair-key').maxLength = 64; $('pair-key').minLength = 64;
  $('pair-key').pattern = '[0-9a-fA-F]{64}';
  $('pair-network-hint').textContent = 'Keep Smart Stage running and connected to the public gateway.';
}

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
  if (appClosed || reloadingAdmin) return;
  $('notice').textContent = message; $('notice').classList.toggle('error', error);
}
function connection(connected) {
  if (appClosed || quitBusy || reloadingAdmin) return;
  online = connected;
  $('connection').textContent = connected ? 'Connected to host' : expectingUpdateRestart() ? 'Restarting Smart Stage…' : 'Disconnected · status may be stale';
  $('connection').className = connected ? 'live' : 'stale';
  renderRemoteStage();
  for (const [id, node] of cueButtons) {
    const cue = state?.cues.find(c => c.id === id);
    node.disabled = !connected || updatePending() || !cue || ['missing', 'unsupported', 'error'].includes(cue.validation);
  }
  if (adminPage) { renderUpdateStatus(); renderEditAvailability(); }
}
function showPair() {
  if (appClosed || quitBusy || reloadingAdmin) return;
  if (!adminPage) window.smartStageWakeLock?.setConnected(false);
  connection(false); if (source) { source.close(); source = null; }
  if (adminPage) { void connectLocalAdmin(); return; }
  if (!$('pairing').open) $('pairing').showModal();
}
function pairError(message = '') {
  $('pair-error').textContent = message; $('pair-error').hidden = !message;
}
async function connectLocalAdmin() {
  if (localSessionBusy || quitBusy || appClosed || reloadingAdmin) return;
  localSessionBusy = true; clearTimeout(localSessionRetry);
  try {
    const session = await api('POST', '/api/local-session', {});
    role = session.role; csrf = session.csrfToken;
    adminCapabilities = session.capabilities || {};
    if (role !== 'admin') throw new Error('Open Admin on the host computer.');
    void sendAdminPresence(true);
    if (!await initializeSession()) throw new Error('Could not load the host status.');
    notify(updateRestartComplete ? 'Smart Stage restarted. Use the new remote control link or QR code to reconnect phones and tablets.' : '');
    updateRestartComplete = false;
  } catch (error) {
    if (quitBusy || appClosed || reloadingAdmin) return;
    connection(false);
    if (expectingUpdateRestart()) notify('Smart Stage is restarting. Admin will reconnect automatically.');
    else notify(`Cannot connect to Admin. ${error.message} Retrying…`, true);
    localSessionRetry = setTimeout(() => { void connectLocalAdmin(); }, 5000);
  } finally { localSessionBusy = false; }
}
async function sendAdminPresence(force = false) {
  if (!adminPage || role !== 'admin' || !csrf || presenceBusy || quitBusy || reloadingAdmin || (!online && !force)) return;
  presenceBusy = true;
  try {
    const result = await api('POST', '/api/admin-presence', {});
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
  $('notice').textContent = ''; $('notice').classList.remove('error');
  $('app-closed').hidden = false; $('stop').disabled = true; $('quit-app').disabled = true;
  $('connection').textContent = 'Smart Stage is closed'; $('connection').className = '';
  $('play-state').textContent = 'Closed'; $('current-cue').textContent = 'No playback'; $('time').textContent = '0:00';
  localSessionRetry = setTimeout(() => { void probeClosedAdmin(); }, 2000);
}
$('quit-app').addEventListener('click', async () => {
  if (!adminPage || role !== 'admin' || !online || quitBusy || appClosed) return;
  quitBusy = true; $('quit-app').disabled = true; $('quit-app').textContent = 'Closing…';
  $('admin-view').inert = true;
  $('connection').textContent = 'Closing Smart Stage…'; notify('Closing Smart Stage…');
  try {
    const result = await api('POST', '/api/quit', {});
    if (result.quitting !== true) throw new Error('The host did not acknowledge the quit request.');
    showAppClosed();
  } catch (error) {
    quitBusy = false; $('quit-app').textContent = 'Quit Smart Stage'; $('admin-view').inert = false;
    connection(online); notify(`Quit is unconfirmed. ${error.message}`, true); void refreshState();
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
    if (adminPage) adminCapabilities = result.capabilities || {};
    applyState(result.state); return !reloadingAdmin;
  } catch (error) {
    if (quitBusy || appClosed || reloadingAdmin) return false;
    connection(false);
    if (expectingUpdateRestart()) notify('Smart Stage is restarting. Admin will reconnect automatically.');
    else if (!$('pairing').open) notify(error.message, true);
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
  state = next;
  renderRemoteStage();
  const current = next.cues.find(c => c.id === next.activeCueId);
  $('play-state').textContent = next.state;
  $('current-cue').textContent = current ? `${current.position}. ${current.label}` : next.state === 'error' ? 'Operator attention needed' : 'Ready when you are';
  $('time').textContent = `${clock(next.elapsed)}${next.duration > 0 ? ` / ${clock(next.duration)}` : ''}`;
  $('playback-error').hidden = !next.lastError; $('playback-error').textContent = next.lastError;
  renderCues();
  if (adminPage && role === 'admin') {
    $('stage-state').textContent = next.stageEnabled ? 'Stage output enabled · STOP returns to background' : 'Stage output disabled · music is independent';
    renderBackgroundStatus();
    const job = next.validationJob;
    $('validate').textContent = job.running ? `Validating ${job.completed}/${job.total}…` : 'Validate all cues';
    for (const c of next.cues) {
      const row = playlistRows.get(c.id);
      if (row) {
        const reason = playlist?.cues.find(item => item.id === c.id)?.cache.reason;
        row.validation.textContent = `${c.kind || 'Unknown type'} · ${c.duration ? clock(c.duration) : 'Duration unknown'} · ${c.validation}${reason ? ` · ${reason}` : ''}`;
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
      ['Background', next.cues.find(c => c.id === next.backgroundCueId)?.label || 'None — black'],
      ['Stage image', next.cues.find(c => c.id === next.imageCueId)?.label || 'None'],
      ['Background error', next.backgroundError || 'None'],
      ['Playlist revision', next.playlistRevision], ['Validation', job.running ? `${job.completed} of ${job.total}` : 'Idle']
    ]) { details.append(element('dt', label), element('dd', String(value))); }
    if (playlist && playlist.playlistRevision !== next.playlistRevision && !playlistBusy && !playlistRefresh) {
      if ($('playlist').contains(document.activeElement)) notify('The playlist changed in another tab. Finish or discard your edit, then Reload.', true);
      else void loadPlaylist();
    }
    renderEditAvailability(); renderUpdateStatus();
  }
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
    node.firstChild.textContent = cue.label;
    const color = validCueColor(cue.color);
    node.classList.toggle('custom-color', Boolean(color));
    if (color) { node.style.setProperty('--cue-fill', color); node.style.setProperty('--cue-ink', cueTextColor(color)); }
    else { node.style.removeProperty('--cue-fill'); node.style.removeProperty('--cue-ink'); }
    const foreground = state.activeCueId === cue.id && ['loading', 'playing'].includes(state.state);
    const image = state.stageEnabled && state.imageCueId === cue.id;
    const background = Boolean(cue.background && state.backgroundCueId === cue.id);
    const active = foreground || image || background;
    let action = cue.background ? 'Set background ↗' : cue.kind === 'image' ? 'Show image ↗' : 'Start cue ↗';
    if (background) action = 'Background selected';
    if (image) action = 'On stage';
    if (foreground) action = cue.kind === 'audio' && state.stage?.toggleAudio ? 'Press again to stop' : state.state;
    if (cue.validation !== 'ready' && !active) action = cue.validation;
    node.lastChild.replaceChildren(element('span', `${String(cue.position).padStart(2, '0')} · ${cue.background ? 'background ' : ''}${cue.kind || 'unchecked'}`), element('span', action));
    node.classList.toggle('active', active); node.setAttribute('aria-pressed', String(active));
    node.disabled = !online || updatePending() || ['missing', 'unsupported', 'error'].includes(cue.validation);
    if (renderedOrder !== order) $('cue-grid').append(node);
  }
  renderedOrder = order; $('empty-cues').hidden = visible.length > 0;
  $('empty-cues').textContent = state.cues.length ? 'No visible buttons. Show cue buttons in Admin on the host computer.' : 'Your show is empty. Add cues on the host computer to get started.';
}
function validCueColor(value) { return /^#[0-9a-f]{6}$/i.test(value || '') ? value : ''; }
function cueTextColor(color) {
  const channels = [1, 3, 5].map(offset => parseInt(color.slice(offset, offset + 2), 16) / 255).map(value => value <= .04045 ? value / 12.92 : ((value + .055) / 1.055) ** 2.4);
  const luminance = channels[0] * .2126 + channels[1] * .7152 + channels[2] * .0722;
  return luminance > .179 ? '#000000' : '#ffffff';
}
function renderRemoteStage() {
  const enabled = Boolean(state?.stageEnabled), control = $('remote-stage');
  control.textContent = enabled ? 'Stage on' : 'Stage off';
  control.setAttribute('aria-pressed', String(enabled));
  control.setAttribute('aria-label', enabled ? 'Disable stage output' : 'Enable stage output');
  control.disabled = !online || !state || (!enabled && (state.outputFault || updatePending()));
  control.title = enabled ? 'Close the stage display; music keeps playing' : 'Open the stage display; music keeps playing';
}
async function trigger(cueId) {
  if (updatePending()) { notify('Smart Stage is preparing an update. Playback is unavailable until it finishes.'); return; }
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
  event.preventDefault(); pairError();
  const submit = $('pair-form').querySelector('button[type=submit]'); submit.disabled = true;
  let token = $('pair-key').value.trim(); $('pair-key').value = '';
  try {
    const result = await api('POST', '/api/pair', { key: token });
    role = result.role; csrf = result.csrfToken; $('pairing').close();
    await initializeSession();
  } catch (error) { pairError(error.message); }
  finally { token = ''; submit.disabled = false; }
});
$('logout').addEventListener('click', async () => {
  try { await api('POST', '/api/logout', {}); csrf = ''; role = ''; pairError(); showPair(); } catch (error) { notify(error.message, true); }
});

function renderRemoteLink() {
  const index = remoteLinks.findIndex(link => link.url === $('remote-network').value);
  const link = remoteLinks[index];
  if (!link) return;
  const changed = selectedRemoteURL !== link.url;
  selectedRemoteURL = link.url;
  $('remote-url').textContent = link.url; $('remote-url').href = link.url;
  $('open-remote-url').href = link.url;
  // The QR is served by the host, without sending the link to another service.
  if (changed || $('remote-qr').getAttribute('src') !== link.qrURL || ($('remote-qr').complete && !$('remote-qr').naturalWidth)) {
    $('remote-qr').src = link.qrURL;
    $('remote-message').textContent = '';
  }
}
function clearRemoteLinks() {
  remoteLinks = []; selectedRemoteURL = '';
  $('remote-ready').hidden = true; $('remote-unavailable').hidden = false;
  $('remote-network').replaceChildren();
  $('remote-url').removeAttribute('href'); $('remote-url').textContent = '';
  $('open-remote-url').removeAttribute('href'); $('remote-qr').removeAttribute('src');
  $('remote-code').textContent = ''; $('remote-message').textContent = '';
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
    $('remote-code').textContent = result.token;
    $('remote-code-row').hidden = publicMode;
    $('remote-guidance').textContent = publicMode ? 'Scan this QR code or open the link from any phone or tablet with an internet connection. Keep Smart Stage running.' : 'Connect your phone or tablet to the same network, then scan this QR code with its camera or open the link.';
    $('remote-ready').hidden = !remoteLinks.length;
    $('remote-unavailable').hidden = remoteLinks.length > 0;
    if (!remoteLinks.length) {
      clearRemoteLinks();
      $('remote-unavailable').textContent = publicMode ? 'The public gateway is not connected. The remote link and QR code appear after Smart Stage connects.' : 'No network address is available. Connect this computer to Wi-Fi or Ethernet to use remote control.';
      $('remote-message').textContent = ''; return;
    }
    $('remote-network').replaceChildren(...remoteLinks.map(link => option(link.url, link.label)));
    $('remote-network-choice').hidden = remoteLinks.length < 2;
    $('remote-network').value = remoteLinks.some(link => link.url === selectedRemoteURL) ? selectedRemoteURL : remoteLinks[0].url;
    renderRemoteLink();
  } catch (error) {
    $('remote-message').textContent = `Could not refresh the remote control link. ${error.message}`;
  } finally { remoteRefresh = false; }
}
$('remote-network').addEventListener('change', renderRemoteLink);
$('remote-qr').addEventListener('error', () => {
  if (selectedRemoteURL) $('remote-message').textContent = 'The QR code could not load. Use the remote control link above.';
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
    copy.className = 'clipboard-copy'; copy.setAttribute('aria-label', 'Remote control link');
    document.body.append(copy); copy.select(); copy.setSelectionRange(0, copy.value.length);
    let copied = false;
    try { copied = document.execCommand('copy'); } catch { /* Show the manual fallback below. */ }
    finally { copy.remove(); previousFocus?.focus(); }
    if (!copied) { $('remote-message').textContent = 'Select and copy the link above to share it.'; return; }
  }
  $('remote-message').textContent = 'Remote control link copied.';
}
$('copy-remote-url').addEventListener('click', () => { void copyRemoteURL(); });

function sameGatewayURL(value) {
  return value.trim().replace(/\/+$/, '') === (gateway?.url || '').replace(/\/+$/, '');
}
function renderGateway() {
  if (!adminPage) return;
  const publicDraft = $('gateway-mode').value === 'gateway';
  const stored = Boolean(gateway?.hasToken && sameGatewayURL($('gateway-url').value));
  const unavailable = gatewayBusy || !online || role !== 'admin' || updatePending();
  $('gateway-fields').hidden = !publicDraft;
  $('gateway-url').required = publicDraft;
  $('gateway-token').required = publicDraft && !stored;
  $('gateway-token').placeholder = stored ? 'Leave blank to keep saved token' : 'Token from the gateway installer';
  $('gateway-token-hint').textContent = stored ? 'A token is stored for this gateway. Leave blank to keep it, or enter a replacement.' : 'Enter the token printed by the gateway installer. Changing the gateway URL requires its token.';
  for (const id of ['gateway-mode', 'gateway-url', 'gateway-token', 'save-gateway']) $(id).disabled = unavailable;
  $('reconnect-gateway').hidden = gateway?.mode !== 'gateway';
  $('reconnect-gateway').disabled = unavailable || gatewayDirty;
  const status = gateway?.status;
  const description = gateway?.mode === 'gateway' ? {
    unconfigured: 'Configure your gateway URL and token to connect. Local network remote access is disabled.',
    connecting: 'Connecting to public gateway… Local network remote access is disabled.',
    connected: 'Connected to public gateway. Local network remote access is disabled.',
    error: 'Public gateway is unavailable. Smart Stage will retry automatically. Local network remote access is disabled.',
    disabled: gateway?.url ? 'Public gateway is not connected. Local network remote access is disabled.' : 'Configure your gateway URL and token to connect. Local network remote access is disabled.'
  }[status] || 'Waiting for the public gateway connection…' : gateway ? 'Local network remote control is enabled.' : 'Loading connection settings…';
  $('gateway-status').textContent = description + (gateway?.message ? ` ${gateway.message}` : '');
  $('gateway-status').classList.toggle('error', status === 'error');
  $('lan-firewall-guidance').hidden = gateway?.mode !== 'lan';
  $('network-note-title').textContent = gateway?.mode === 'gateway' ? 'Public gateway over HTTPS' : 'Trusted LAN only';
  $('network-note-text').textContent = gateway?.mode === 'gateway' ? 'Remote commands travel through your gateway over HTTPS. Anyone with the remote link can control playback. Admin stays on this computer. HTTPS also allows supported phones and tablets to use Keep awake.' : 'HTTP traffic is not encrypted. Pairing protects control access, but cannot protect against someone listening on the network. Keep the host and controllers on a trusted network.';
}
function applyGateway(value) {
  if (value.mode === 'gateway' && (value.status !== 'connected' || (gateway?.remoteURL && gateway.remoteURL !== value.remoteURL))) {
    clearRemoteLinks();
    $('remote-unavailable').textContent = 'The public gateway is not connected. The remote link and QR code appear after Smart Stage connects.';
  }
  if (value.status === 'connected' && gateway?.status !== 'connected' && !$('gateway-message').classList.contains('error')) $('gateway-message').textContent = '';
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
    if (sequence === gatewaySequence) $('gateway-status').textContent = `Could not refresh gateway status. ${error.message}`;
  } finally { gatewayRefresh = false; }
}
for (const id of ['gateway-mode', 'gateway-url', 'gateway-token']) $(id).addEventListener('input', () => {
  gatewayDirty = true; $('gateway-message').textContent = ''; renderGateway();
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
  $('gateway-message').textContent = 'Saving connection…'; $('gateway-message').classList.remove('error');
  try {
    const result = await api('PUT', '/api/gateway', body);
    gatewayDirty = false; applyGateway(result);
    $('gateway-message').textContent = result.restart ? 'Saved. Smart Stage is restarting to enable local network remote control. Approve its firewall setup and allow Smart Stage incoming connections when prompted.' : result.mode === 'gateway' ? 'Saved. The public link and QR code appear once connected.' : 'Saved. Local network remote control is enabled. Allow Smart Stage incoming connections if your firewall asks.';
    await loadRemoteControl();
  } catch (error) {
    $('gateway-message').textContent = `Could not save the connection. ${error.message}`;
    $('gateway-message').classList.add('error');
  } finally { delete body.token; gatewayBusy = false; renderGateway(); }
});
$('reconnect-gateway').addEventListener('click', async () => {
  if (gatewayBusy || gatewayDirty || !online || role !== 'admin' || updatePending()) return;
  gatewayBusy = true; gatewaySequence++; renderGateway();
  $('gateway-message').textContent = 'Reconnecting…'; $('gateway-message').classList.remove('error');
  try {
    applyGateway(await api('POST', '/api/gateway/reconnect', {}));
    await loadRemoteControl();
    $('gateway-message').textContent = 'Reconnecting. Use the current link or QR code after the gateway connects.';
  } catch (error) { $('gateway-message').textContent = error.message; $('gateway-message').classList.add('error'); }
  finally { gatewayBusy = false; renderGateway(); }
});

function updatePending() { return Boolean(state?.updatePending || updatePreparing); }
function expectingUpdateRestart() {
  return Boolean(updateRestartInstance && updateRestartStarted && Date.now() - updateRestartStarted < 180000);
}
function renderEditAvailability() {
  if (!adminPage || !state) return;
  renderGateway();
  $('quit-app').disabled = !online || role !== 'admin' || quitBusy || appClosed || reloadingAdmin;
  $('choose-files').hidden = adminCapabilities.chooseFiles !== true;
  $('choose-files').disabled = !online || role !== 'admin' || chooseFilesBusy || quitBusy || appClosed || updatePending() || playlistBusy;
  $('playlist-drop-title').textContent = desktopAdmin ? 'Drop Finder files here' : adminCapabilities.chooseFiles === true ? 'Add media from this Mac' : 'Add media from this computer';
  $('playlist-drop-hint').textContent = desktopAdmin ? 'Use Choose Media, or drag files from Finder into this window. Originals stay in place; nothing is uploaded or copied.' : adminCapabilities.chooseFiles === true ? 'Use Choose Media to select files on this Mac. Browser drops cannot provide their original paths. You can also drop files onto Smart Stage’s Dock icon.' : 'Browsers cannot read original file paths from a drop. Use Add files by path below. On Mac, you can also open Smart Stage and drop files from Finder into its window.';
  $('media-path-settings').hidden = desktopAdmin || adminCapabilities.chooseFiles === true;
  for (const id of ['media-paths', 'add-media-paths']) $(id).disabled = !online || role !== 'admin' || !playlist || playlistBusy || quitBusy || appClosed || updatePending();
  const pending = updatePending();
  $('save-outputs').disabled = pending || !['stopped', 'error'].includes(state.state);
  $('enable-stage').disabled = !online || pending || state.outputFault;
  $('disable-stage').disabled = !online;
  const editingStage = !online || pending || playlistBusy || !playlist;
  for (const id of ['background-cue', 'background-audio', 'fade-enabled', 'toggle-audio', 'save-stage-settings']) $(id).disabled = editingStage;
  $('fade-seconds').disabled = editingStage || !$('fade-enabled').checked;
  $('validate').disabled = pending || state.validationJob.running;
  for (const id of ['audio-output', 'display-output', 'allow-primary']) $(id).disabled = pending;
  for (const [id, row] of playlistRows) {
    row.input.disabled = pending;
    row.play.disabled = pending || !online;
    row.up.disabled = pending || row.first;
    row.down.disabled = pending || row.last;
    row.remove.disabled = pending || id === state.activeCueId;
    row.color.disabled = pending;
    row.resetColor.disabled = pending || !row.customColor;
    row.hidden.disabled = pending || playlistBusy;
    const cue = playlist?.cues.find(c => c.id === id);
    const foreground = state.activeCueId === id && ['loading', 'playing'].includes(state.state);
    const selected = foreground || (state.stageEnabled && state.imageCueId === id) || (cue?.background && state.backgroundCueId === id);
    row.play.setAttribute('aria-pressed', String(Boolean(selected)));
    row.play.textContent = cue?.background ? 'Set background' : cue?.cache.media.kind === 'image' ? 'Show image' : foreground && cue?.cache.media.kind === 'audio' && state.stage?.toggleAudio ? 'Stop music' : 'Play';
    row.background.disabled = pending || playlistBusy || !['image', 'video'].includes(cue?.cache.media.kind);
    row.backgroundLabel.hidden = !['image', 'video'].includes(cue?.cache.media.kind) && !cue?.background;
  }
}
function releaseLink(value) {
  try {
    const url = new URL(value);
    if (url.origin === 'https://github.com' && !url.username && !url.password && !url.search && !url.hash &&
        /^\/arizzi74\/Smart-Stage\/releases\/tag\/v[0-9][A-Za-z0-9._-]*$/.test(url.pathname)) return url.href;
  } catch { /* Only this project's release pages may be opened. */ }
  return '';
}
function renderUpdateStatus() {
  if (!adminPage) return;
  const phase = updateStatus?.phase || 'idle';
  const busy = updateBusy || updatePending() || ['checking', 'downloading', 'restarting'].includes(phase);
  $('update-current').textContent = updateStatus?.currentVersion || 'Loading…';
  $('update-latest').textContent = updateStatus?.latestVersion || 'Not checked yet';
  const defaults = {
    idle: 'No newer version is available.', checking: 'Checking for updates…',
    available: 'A new version is available.', downloading: 'Downloading and verifying the update…',
    restarting: 'Restarting Smart Stage. Admin will reconnect automatically.',
    error: 'Could not check or install the update. Try again when connected.',
    unsupported: 'Automatic updates are not available for this installation.'
  };
  $('update-message').textContent = updateStatus?.message || (updateStatus ? defaults[phase] || 'Update status unavailable.' : 'Loading update status…');
  $('update-message').classList.toggle('error', phase === 'error');
  const outcome = updateStatus?.lastUpdate;
  $('update-outcome').hidden = !outcome?.message;
  $('update-outcome').textContent = outcome?.message || '';
  $('update-outcome').classList.toggle('error', ['error', 'rolled_back'].includes(outcome?.status));
  const safeToInstall = state?.state === 'stopped' && !state.stageEnabled && !updatePending();
  $('update-requirements').textContent = updatePending() ? (phase === 'checking' ? 'Checking for an update before starting. Playback and edits are temporarily unavailable; STOP remains available.' : 'Preparing the update. Playback and edits are temporarily unavailable; STOP remains available.') :
    safeToInstall ? 'Ready to update when a new version is available.' : 'Stop playback and disable stage output before updating.';
  $('check-update').disabled = !online || busy || phase === 'unsupported';
  $('check-update').textContent = phase === 'checking' ? 'Checking…' : 'Check for updates';
  $('install-update').disabled = !online || busy || !safeToInstall || !updateStatus?.available || !updateStatus?.canInstall;
  $('install-update').textContent = phase === 'downloading' ? 'Downloading…' : phase === 'restarting' ? 'Restarting…' : 'Update and restart';
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
  if (result.phase === 'error' || result.phase === 'unsupported') {
    updatePreparing = false; updateRestartInstance = ''; updateRestartStarted = 0;
  }
}
async function loadUpdateStatus() {
  if (!adminPage || role !== 'admin' || updateBusy || quitBusy || appClosed || reloadingAdmin) return;
  updateBusy = true;
  try { applyUpdateStatus(await api('GET', '/api/update')); }
  catch (error) {
    if (!expectingUpdateRestart()) updateStatus = { ...updateStatus, phase: 'error', message: `Update status unavailable. ${error.message}` };
  } finally { updateBusy = false; renderUpdateStatus(); renderEditAvailability(); }
}
async function updateAction(install) {
  if (!adminPage || role !== 'admin' || updateBusy || $(install ? 'install-update' : 'check-update').disabled) return;
  updateBusy = true; renderUpdateStatus();
  try {
    const result = await api('POST', `/api/update/${install ? 'install' : 'check'}`, {});
    if (install) {
      updatePreparing = true; updateRestartInstance = state.instanceId; updateRestartStarted = Date.now();
      notify('Preparing the update. Smart Stage will restart and Admin will reconnect automatically.');
    }
    applyUpdateStatus(result);
  } catch (error) {
    updateStatus = { ...updateStatus, phase: 'error', message: error.message };
    updatePreparing = false; updateRestartInstance = ''; updateRestartStarted = 0;
  } finally { updateBusy = false; renderUpdateStatus(); renderEditAvailability(); }
}
$('check-update').addEventListener('click', () => { void updateAction(false); });
$('install-update').addEventListener('click', () => { void updateAction(true); });

async function loadPlaylist() {
  if (playlistRefresh) return;
  playlistRefresh = true;
  try {
    const previous = playlist;
    playlist = await api('GET', '/api/playlist'); renderPlaylist();
    if (desktopAdmin && previous && previous.playlistRevision !== playlist.playlistRevision) {
      const previousIDs = new Set(previous.cues.map(cue => cue.id));
      const added = playlist.cues.filter(cue => !previousIDs.has(cue.id)).length;
      if (added) fileDropMessage(`Added ${added} ${added === 1 ? 'file' : 'files'} to the playlist. Originals stay in place.`);
    }
  }
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
  finally { validationRefresh = false; renderStageSettings(); renderEditAvailability(); }
}
function cueEdits() { return playlist.cues.map(({ id, label, path, color, hidden, background }) => ({ id, label, path, color: color || '', hidden: Boolean(hidden), background: Boolean(background) })); }
async function savePlaylist(cues) {
  if (updatePending()) { notify('Smart Stage is preparing an update. Wait before editing the show.'); return false; }
  if (playlistBusy) { notify('An edit is being saved. Wait before making another edit.', true); return false; }
  playlistBusy = true; renderEditAvailability();
  try {
    playlist = await api('PUT', '/api/playlist', { expectedRevision: playlist.playlistRevision, cues });
    renderPlaylist(); notify('Playlist saved.'); await refreshState(); return true;
  } catch (error) { notify(`${error.message} Your edit was not saved. Reload to use the host version.`, true); return false; }
  finally { playlistBusy = false; renderEditAvailability(); }
}
function fileDropMessage(message, error = false) {
  $('file-drop-message').textContent = message;
  $('file-drop-message').classList.toggle('error', error);
}
$('choose-files').addEventListener('click', async () => {
  if (!adminPage || adminCapabilities.chooseFiles !== true || $('choose-files').disabled) return;
  chooseFilesBusy = true; renderEditAvailability();
  try {
    const result = await api('POST', '/api/choose-files', {});
    if (result.choosing !== true) throw new Error('The host did not open the file chooser.');
    fileDropMessage('Choose files in the Mac dialog. Originals stay in place.');
  } catch (error) { fileDropMessage(error.message, true); }
  finally { chooseFilesBusy = false; renderEditAvailability(); }
});
$('media-path-form').addEventListener('submit', async event => {
  event.preventDefault();
  if ($('media-path-settings').hidden || !online || role !== 'admin' || !playlist || playlistBusy || updatePending()) return;
  const paths = [...new Set($('media-paths').value.split(/\r?\n/).map(value => {
    const path = value.trim();
    return path.length > 1 && ['"', "'"].includes(path[0]) && path.at(-1) === path[0] ? path.slice(1, -1) : path;
  }).filter(Boolean))];
  if (!paths.length) { fileDropMessage('Enter one original file path per line.', true); return; }
  if (paths.some(path => !/^(?:\/|[A-Za-z]:[\\/]|\\\\)/.test(path))) {
    fileDropMessage('Use absolute file paths on the computer running Smart Stage, one per line.', true); return;
  }
  if (playlist.cues.length + paths.length > 500) { fileDropMessage('A playlist can contain up to 500 cues. Add fewer files.', true); return; }
  if (await savePlaylist([...cueEdits(), ...paths.map(path => ({ id: '', label: '', path }))])) {
    $('media-paths').value = '';
    fileDropMessage(`Added ${paths.length} ${paths.length === 1 ? 'file' : 'files'} to the playlist. Originals stay in place.`);
  } else fileDropMessage('The files were not added. Check the message above and try again.', true);
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
    const message = desktopAdmin ? 'Drop files directly from Finder onto this Smart Stage window, or use Choose Media. Originals stay in place.' : adminCapabilities.chooseFiles === true ? 'Your browser cannot read Finder file paths. Use Choose Media, or drop files onto the Smart Stage Dock icon.' : 'Your browser cannot read original file paths from a drop. Use Add files by path in the Playlist, or drop Finder files into the Smart Stage Mac app.';
    fileDropMessage(message); notify(message);
  });
}
function renderPlaylist() {
  $('playlist').replaceChildren(); playlistRows.clear();
  $('playlist-revision').textContent = `${playlist.cues.length} cues · saved revision ${playlist.playlistRevision}`;
  if (!playlist.cues.length) $('playlist').append(element('p', 'Add audio, videos, or images to build your show.', 'empty'));
  const counts = new Map(); for (const c of playlist.cues) counts.set(c.label, (counts.get(c.label) || 0) + 1);
  playlist.cues.forEach((cue, index) => {
    const row = element('div', undefined, 'playlist-row'), info = element('div', undefined, 'playlist-info');
    const input = element('input'); input.value = cue.label; input.maxLength = 512; input.setAttribute('aria-label', `Label for cue ${index + 1}`);
    input.addEventListener('change', () => { const edited = cueEdits(); edited[index].label = input.value; void savePlaylist(edited); });
    const validation = element('div', `${cue.cache.media.kind || 'Unknown type'} · ${cue.cache.status}${cue.cache.reason ? ` · ${cue.cache.reason}` : ''}`, 'validation');
    info.append(input, element('p', cue.path, 'source-path'), validation);
    const colors = element('div', undefined, 'cue-color-controls'), colorLabel = element('label', 'Color');
    const color = element('input'); color.type = 'color'; color.value = validCueColor(cue.color) || '#1b2227';
    color.setAttribute('aria-label', `Color for cue ${index + 1}`);
    color.addEventListener('change', () => { const edited = cueEdits(); edited[index].color = color.value; void savePlaylist(edited); });
    colorLabel.append(color);
    const resetColor = button('Default', () => { const edited = cueEdits(); edited[index].color = ''; void savePlaylist(edited); });
    resetColor.setAttribute('aria-label', `Use default color for cue ${index + 1}`);
    colors.append(colorLabel, resetColor); info.append(colors);
    const options = element('div', undefined, 'cue-options');
    const hiddenLabel = element('label', undefined, 'check'), hidden = element('input'); hidden.type = 'checkbox'; hidden.checked = Boolean(cue.hidden);
    hidden.setAttribute('aria-label', `Hide remote button for cue ${index + 1}`);
    hidden.addEventListener('change', () => { const edited = cueEdits(); edited[index].hidden = hidden.checked; void savePlaylist(edited); });
    hiddenLabel.append(hidden, document.createTextNode('Hide remote button'));
    const backgroundLabel = element('label', undefined, 'check'), background = element('input'); background.type = 'checkbox'; background.checked = Boolean(cue.background);
    background.setAttribute('aria-label', `Use cue ${index + 1} as a background button`);
    background.addEventListener('change', () => { const edited = cueEdits(); edited[index].background = background.checked; void savePlaylist(edited); });
    backgroundLabel.append(background, document.createTextNode('Background button'));
    backgroundLabel.title = 'Pressing this button changes the stage background and keeps music playing.';
    options.append(hiddenLabel, backgroundLabel); info.append(options);
    if (counts.get(cue.label) > 1) info.append(element('p', 'Duplicate label — use cue position to distinguish.', 'hint'));
    const tools = element('div', undefined, 'cue-tools');
    const move = delta => { const edited = cueEdits(); [edited[index], edited[index + delta]] = [edited[index + delta], edited[index]]; void savePlaylist(edited); };
    const up = button('↑ Up', () => move(-1)); up.disabled = index === 0; up.setAttribute('aria-label', `Move cue ${index + 1} up`);
    const down = button('↓ Down', () => move(1)); down.disabled = index === playlist.cues.length - 1; down.setAttribute('aria-label', `Move cue ${index + 1} down`);
    const remove = button('Remove', () => { const edited = cueEdits(); edited.splice(index, 1); void savePlaylist(edited); });
    remove.disabled = state?.activeCueId === cue.id; remove.setAttribute('aria-label', `Remove cue ${index + 1} from playlist`);
    const play = button('Play', () => trigger(cue.id));
    tools.append(play, up, down, remove);
    row.append(element('span', String(index + 1).padStart(2, '0'), 'position'), info, tools);
    $('playlist').append(row); playlistRows.set(cue.id, { validation, remove, input, color, resetColor, hidden, background, backgroundLabel, customColor: Boolean(validCueColor(cue.color)), play, up, down, first: index === 0, last: index === playlist.cues.length - 1 });
  });
  renderStageSettings(); renderEditAvailability();
}
function renderBackgroundStatus() {
  if (!adminPage || !state) return;
  const current = state.cues.find(cue => cue.id === state.backgroundCueId);
  $('current-background').textContent = `Current background: ${current?.label || 'None — black'}${state.stageEnabled ? '' : ' · stage is off'}${state.backgroundError ? ` · ${state.backgroundError}` : ''}`;
}
function renderStageSettings() {
  if (!playlist || !adminPage) return;
  const settings = playlist.stage || {};
  const selected = stageSettingsDirty ? $('background-cue').value : settings.backgroundCueId || '';
  const options = [option('', 'None — black')];
  for (const cue of playlist.cues) {
    if (['image', 'video'].includes(cue.cache.media.kind) && cue.cache.status === 'ready') options.push(option(cue.id, `${cue.label} · ${cue.cache.media.kind}`));
  }
  if (selected && !options.some(item => item.value === selected)) {
    const cue = playlist.cues.find(item => item.id === selected);
    options.push(option(selected, `${cue?.label || 'Previous background'} · unavailable`));
  }
  $('background-cue').replaceChildren(...options); $('background-cue').value = selected;
  if (!stageSettingsDirty) {
    stageSettingsRevision = playlist.playlistRevision;
    $('background-audio').checked = Boolean(settings.backgroundAudio);
    $('fade-enabled').checked = Boolean(settings.fadeEnabled);
    $('fade-seconds').value = settings.fadeSeconds > 0 ? settings.fadeSeconds : 1;
    $('toggle-audio').checked = Boolean(settings.toggleAudio);
  }
  renderBackgroundStatus();
}
for (const id of ['background-cue', 'background-audio', 'fade-enabled', 'fade-seconds', 'toggle-audio']) $(id).addEventListener('input', () => {
  if (!stageSettingsDirty) stageSettingsRevision = playlist?.playlistRevision || 0;
  stageSettingsDirty = true; $('stage-settings-message').textContent = 'Unsaved changes.';
  $('stage-settings-message').classList.remove('error'); renderEditAvailability();
});
$('stage-settings-form').addEventListener('submit', async event => {
  event.preventDefault();
  if (!online || !playlist || playlistBusy || updatePending()) return;
  const fadeSeconds = Number($('fade-seconds').value);
  if (!Number.isFinite(fadeSeconds) || fadeSeconds < .1 || fadeSeconds > 30) {
    $('stage-settings-message').textContent = 'Choose a transition duration from 0.1 to 30 seconds.';
    $('stage-settings-message').classList.add('error'); return;
  }
  const settings = {
    backgroundCueId: $('background-cue').value, backgroundAudio: $('background-audio').checked,
    fadeEnabled: $('fade-enabled').checked, fadeSeconds, toggleAudio: $('toggle-audio').checked
  };
  playlistBusy = true; renderEditAvailability();
  try {
    playlist = await api('PUT', '/api/stage-settings', { expectedRevision: stageSettingsRevision || playlist.playlistRevision, settings });
    stageSettingsDirty = false; renderPlaylist();
    $('stage-settings-message').textContent = 'Stage and sound settings saved.';
    $('stage-settings-message').classList.remove('error'); await refreshState();
  } catch (error) {
    $('stage-settings-message').textContent = `${error.message} Settings were not saved. Reload the playlist before trying again.`;
    $('stage-settings-message').classList.add('error');
  } finally { playlistBusy = false; renderEditAvailability(); }
});
$('reload-playlist').addEventListener('click', () => { stageSettingsDirty = false; $('stage-settings-message').textContent = ''; void loadPlaylist(); });
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
  $('display-warning').textContent = !d ? 'Choose a display to show images, videos, and backgrounds.' : d.mirrored ? 'This display is mirrored. The desktop cannot present an independent stage image.' : d.primary || devices.displays.length === 1 ? 'Warning: enabling stage output or a video cue covers the primary/only display.' : 'Images and videos fill the selected display while preserving their aspect ratio.';
}
$('display-output').addEventListener('change', () => { $('allow-primary').checked = false; displayWarning(); });
$('refresh-devices').addEventListener('click', () => loadDevices());
$('save-outputs').addEventListener('click', async () => {
  try {
    await api('PUT', '/api/outputs', { audioId: $('audio-output').value, displayId: $('display-output').value, allowPrimary: $('allow-primary').checked });
    notify('Outputs saved. Stage output is disabled until you enable it or trigger video.'); await refreshState();
  } catch (error) { notify(error.message, true); }
});
async function setStageOutput(enabled) {
  if (!csrf) { showPair(); return; }
  try { await api('POST', '/api/stage-output', { enabled }); notify(enabled ? 'Stage enable accepted. Music keeps playing.' : 'Stage disable accepted. Music keeps playing.'); await refreshState(); }
  catch (error) { notify(`${enabled ? 'Stage enable' : 'Stage disable'} is unconfirmed. ${error.message}`, true); }
}
for (const [id, enabled] of [['enable-stage', true], ['disable-stage', false]]) $(id).addEventListener('click', () => { void setStageOutput(enabled); });
$('remote-stage').addEventListener('click', () => { void setStageOutput(!state?.stageEnabled); });
document.addEventListener('keydown', event => {
  if (event.key !== 'Escape' || event.repeat || !csrf || !online || $('pairing').open) return;
  event.preventDefault();
  const sequence = ++controlSequence;
  void api('POST', '/api/emergency-stop', { requestId: requestID() }).then(async () => {
    if (sequence === controlSequence) notify('Emergency stop accepted. All sound stops and the stage closes.');
    await refreshState();
  }).catch(error => { if (sequence === controlSequence) notify(`Emergency stop is unconfirmed. ${error.message}`, true); });
});
async function initializeSession() {
  if (!await refreshState()) return false;
  if (adminPage && role !== 'admin') { notify('Open Admin on the host computer.', true); return false; }
  $('admin-view').hidden = !adminPage; $('command-view').hidden = adminPage;
  $('logout').hidden = adminPage;
  $('transport-tools').hidden = adminPage;
  if (!adminPage) window.smartStageWakeLock?.setConnected(role === 'command');
  connectEvents();
  if (adminPage) { await Promise.all([loadGateway(), loadRemoteControl(), loadPlaylist(), loadDevices(), loadUpdateStatus()]); }
  return true;
}
setInterval(() => { if (Date.now() - lastSeen > 18000) connection(false); }, 2000);
setInterval(() => { if (!document.hidden) void loadRemoteControl(); }, 10000);
setInterval(() => { if (!document.hidden) void loadGateway(); }, 3000);
setInterval(() => { if (!document.hidden) void loadUpdateStatus(); }, 5000);
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
    showPair(); pairError(`${error.message} Use the current link or QR code from Admin on the host computer.`);
  } finally { token = ''; }
}
void start();
