/* Screen Wake Lock is optional: HTTP LAN pages cannot request it. */
(() => {
  'use strict';
  const button = document.getElementById('keep-awake');
  const status = document.getElementById('keep-awake-status');
  if (!button || !status) return;
  const supported = window.isSecureContext && typeof navigator.wakeLock?.request === 'function';
  let connected = false;
  let wanted = false;
  let sentinel = null;
  let pending = false;
  let epoch = 0;
  let failure = '';

  function render() {
    const active = !!sentinel && !sentinel.released;
    button.disabled = !connected || !supported;
    button.setAttribute('aria-pressed', String(wanted));
    button.textContent = wanted ? 'Allow sleep' : 'Keep awake';
    let label, detail;
    if (!window.isSecureContext) {
      label = 'Needs HTTPS';
      detail = 'Keeping the screen awake requires HTTPS. For this HTTP address, change Auto-Lock or Screen timeout in your device settings.';
    } else if (!supported) {
      label = 'Unavailable';
      detail = 'This browser does not support keeping the screen awake. Use your device Auto-Lock or Screen timeout setting.';
    } else if (active) {
      label = 'On';
      detail = 'Keeping this screen awake while the remote is visible. Switching apps, power saving, or locking the device can release it.';
    } else if (pending) {
      label = 'Requesting…';
      detail = 'Asking the browser to keep this screen awake.';
    } else if (failure) {
      label = 'Not active';
      detail = failure;
    } else if (wanted) {
      label = 'Paused';
      detail = 'The screen wake lock was released. It will be requested again when you return to this page; turn Keep awake off and on to retry now.';
    } else {
      label = 'Off';
      detail = 'Keep this screen awake while using the remote. The browser and device may still release the wake lock.';
    }
    status.textContent = label;
    status.title = detail;
    status.setAttribute('aria-label', detail);
    button.title = detail;
    button.setAttribute('aria-describedby', status.id);
  }

  function release() {
    epoch++;
    const previous = sentinel;
    sentinel = null;
    if (previous && !previous.released) Promise.resolve(previous.release()).catch(() => {});
    render();
  }

  async function request() {
    if (!connected || !wanted || !supported || pending || sentinel || document.visibilityState !== 'visible') return;
    const ticket = epoch;
    pending = true;
    failure = '';
    render();
    try {
      const acquired = await navigator.wakeLock.request('screen');
      if (ticket !== epoch || !connected || !wanted || document.visibilityState !== 'visible') {
        await acquired.release();
        return;
      }
      sentinel = acquired;
      acquired.addEventListener('release', () => {
        if (sentinel !== acquired) return;
        sentinel = null;
        // Do not repeatedly request a lock the OS has revoked.
        render();
      });
    } catch (_) {
      if (ticket === epoch) {
        failure = 'The browser could not keep the screen awake. Check power-saving settings, then turn Keep awake off and on to retry.';
      }
    } finally {
      pending = false;
      render();
      // A visibility change or a new connection may invalidate an in-flight
      // request. Honor a newer user intent without retaining a stale lock.
      if (ticket !== epoch && wanted && connected && document.visibilityState === 'visible') void request();
    }
  }

  button.addEventListener('click', () => {
    if (!connected || !supported) return;
    wanted = !wanted;
    failure = '';
    if (wanted) void request();
    else release();
    render();
  });
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') void request();
    else release();
  });
  window.addEventListener('pagehide', () => {
    wanted = false;
    release();
  });
  window.smartStageWakeLock = {
    setConnected(value) {
      connected = !!value;
      if (!connected) {
        wanted = false;
        failure = '';
        release();
      }
      render();
    }
  };
  render();
})();
