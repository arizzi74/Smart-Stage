/* Screen Wake Lock is optional: HTTP LAN pages cannot request it. */
(() => {
  'use strict';
  const t = (key, values) => window.smartStageI18n?.t(key, values) ?? key;
  const localizedText = (node, read) => { if (window.smartStageI18n) window.smartStageI18n.text(node, read); else node.textContent = read(); };
  const localizedAttribute = (node, name, read) => { if (window.smartStageI18n) window.smartStageI18n.attribute(node, name, read); else if (name === 'title' || name === 'placeholder') node[name] = read(); else node.setAttribute(name, read()); };
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
    localizedText(button, () => wanted ? t("Allow sleep") : t("Keep awake"));
    let label, detail;
    if (!window.isSecureContext) {
      label = t("Needs HTTPS");
      detail = t("Keeping the screen awake requires HTTPS. For this HTTP address, change Auto-Lock or Screen timeout in your device settings.");
    } else if (!supported) {
      label = t("Unavailable");
      detail = t("This browser does not support keeping the screen awake. Use your device Auto-Lock or Screen timeout setting.");
    } else if (active) {
      label = t("On");
      detail = t("Keeping this screen awake while the remote is visible. Switching apps, power saving, or locking the device can release it.");
    } else if (pending) {
      label = t("Requesting…");
      detail = t("Asking the browser to keep this screen awake.");
    } else if (failure) {
      label = t("Not active");
      detail = t(failure);
    } else if (wanted) {
      label = t("Paused");
      detail = t("The screen wake lock was released. It will be requested again when you return to this page; turn Keep awake off and on to retry now.");
    } else {
      label = t("Off");
      detail = t("Keep this screen awake while using the remote. The browser and device may still release the wake lock.");
    }
    localizedText(status, () => label);
    localizedAttribute(status, 'title', () => detail);
    localizedAttribute(status, 'aria-label', () => detail);
    localizedAttribute(button, 'title', () => detail);
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
        failure = "The browser could not keep the screen awake. Check power-saving settings, then turn Keep awake off and on to retry.";
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
  window.addEventListener('smartstage-languagechange', render);
  render();
})();
