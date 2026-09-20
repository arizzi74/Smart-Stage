const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const source = fs.readFileSync(path.join(__dirname, '../internal/web/assets/wake-lock.js'), 'utf8');

function target() {
  const listeners = new Map();
  return {
    attrs: {},
    addEventListener(type, fn) { const list = listeners.get(type) || []; list.push(fn); listeners.set(type, list); },
    emit(type) { for (const fn of listeners.get(type) || []) fn(); },
    setAttribute(key, value) { this.attrs[key] = value; },
  };
}
function lock() {
  return Object.assign(target(), {released: false, releases: 0, release() {
    this.releases++; this.released = true; this.emit('release'); return Promise.resolve();
  }});
}
function setup({secure = true, supported = true, request} = {}) {
  const button = target(), status = Object.assign(target(), {id: 'keep-awake-status'});
  const document = Object.assign(target(), {visibilityState: 'visible', getElementById: id => id === 'keep-awake' ? button : status});
  const window = Object.assign(target(), {isSecureContext: secure});
  const locks = [], calls = [];
  const navigator = supported ? {wakeLock: {request(type) {
    calls.push(type);
    if (request) return request();
    const acquired = lock(); locks.push(acquired); return Promise.resolve(acquired);
  }}} : {};
  vm.runInNewContext(source, {window, document, navigator, Promise});
  const api = window.smartStageWakeLock;
  return {button, status, document, window, api, locks, calls,
    visible(value) { document.visibilityState = value ? 'visible' : 'hidden'; document.emit('visibilitychange'); }};
}
const settle = () => new Promise(resolve => setImmediate(resolve));

test('HTTP and unsupported browsers explain why no lock can be requested', async () => {
  for (const options of [{secure: false}, {supported: false}]) {
    const h = setup(options); h.api.setConnected(true); h.button.emit('click'); await settle();
    assert.equal(h.button.disabled, true); assert.equal(h.calls.length, 0);
    assert.match(h.status.title, /device|settings/);
    assert.equal(h.status.textContent, options.secure === false ? 'Needs HTTPS' : 'Unavailable');
  }
});
test('lock is requested only after connection and deliberate press, released on disconnect', async () => {
  const h = setup(); h.button.emit('click'); assert.equal(h.calls.length, 0);
  h.api.setConnected(true); assert.equal(h.calls.length, 0);
  h.button.emit('click'); await settle();
  assert.deepEqual(h.calls, ['screen']); assert.equal(h.status.textContent, 'On');
  h.api.setConnected(false); await settle();
  assert.equal(h.locks[0].releases, 1); assert.equal(h.status.textContent, 'Off');
  h.api.setConnected(true); assert.equal(h.calls.length, 1);
});
test('visibility releases and reacquires; system revocation never causes a retry loop', async () => {
  const h = setup(); h.api.setConnected(true); h.button.emit('click'); await settle();
  h.visible(false); await settle(); assert.equal(h.status.textContent, 'Paused');
  assert.equal(h.locks[0].released, true);
  h.visible(true); await settle(); assert.equal(h.calls.length, 2); assert.equal(h.status.textContent, 'On');
  await h.locks[1].release(); await settle();
  assert.equal(h.status.textContent, 'Paused'); assert.equal(h.calls.length, 2);
  h.button.emit('click'); h.visible(false); h.visible(true); await settle();
  assert.equal(h.calls.length, 2); assert.equal(h.status.textContent, 'Off');
});
test('denial is visible without claiming active or repeatedly requesting', async () => {
  const h = setup({request: () => Promise.reject(new Error('NotAllowedError'))});
  h.api.setConnected(true); h.button.emit('click'); await settle();
  assert.equal(h.status.textContent, 'Not active'); assert.equal(h.calls.length, 1);
  assert.match(h.status.title, /power-saving/);
});
test('disconnect during a pending request releases the late lock', async () => {
  let resolve; const h = setup({request: () => new Promise(r => {resolve = r;})});
  h.api.setConnected(true); h.button.emit('click'); h.api.setConnected(false);
  const acquired = lock(); resolve(acquired); await settle();
  assert.equal(acquired.released, true); assert.equal(h.status.textContent, 'Off');
});
test('off/on during a pending request discards the stale lock then fulfills new intent', async () => {
  const resolvers = []; const h = setup({request: () => new Promise(r => resolvers.push(r))});
  h.api.setConnected(true); h.button.emit('click'); h.button.emit('click'); h.button.emit('click');
  const stale = lock(); resolvers[0](stale); await settle();
  assert.equal(stale.released, true); assert.equal(h.calls.length, 2);
  const current = lock(); resolvers[1](current); await settle();
  assert.equal(h.status.textContent, 'On'); assert.equal(current.released, false);
  h.window.emit('pagehide'); await settle();
  assert.equal(current.released, true); assert.equal(h.status.textContent, 'Off');
});
