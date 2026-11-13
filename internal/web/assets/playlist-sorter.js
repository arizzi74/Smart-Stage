// Loaded explicitly by Admin. Remote controllers never import this module.
let stylesheetPromise;
function stylesheetReady() {
  if (!stylesheetPromise) stylesheetPromise = new Promise((resolve, reject) => {
    const link = document.createElement('link');
    link.rel = 'stylesheet'; link.href = new URL('./playlist-sorter.css', import.meta.url).href;
    link.dataset.playlistSorter = '';
    link.addEventListener('load', resolve, {once: true});
    link.addEventListener('error', () => {
      link.remove(); stylesheetPromise = null;
      reject(new Error('Could not load playlist sorting styles.'));
    }, {once: true});
    document.head.append(link);
  });
  return stylesheetPromise;
}

/**
 * open({playlistRevision, sections, cues}, opener?) copies names and identities.
 * onSave receives {expectedRevision, sections, cues:[{id,sectionId}]}: the caller
 * owns revision validation and maps identities to the complete current cues.
 * A false result or rejection keeps the draft. Only confirmed saves close it.
 */
export function createPlaylistSorter({t, onSave, onClose = () => {}, formatError = error => error?.message || ''}) {
  const ready = stylesheetReady();
  let dialog, list, heading, instructions, keyboardHelp, save, cancel, errorBox, availabilityBox, live;
  let groups = [], baseRevision = 0, originalOrder = '', opener, opening = false;
  let available = true, availabilityMessage = '', saving = false, cycle = 0, drag = null, frame = 0;
  const listeners = [];
  const node = (tag, className, text) => {
    const value = document.createElement(tag);
    if (className) value.className = className;
    if (text !== undefined) value.textContent = text;
    return value;
  };
  const order = () => JSON.stringify(groups.map(group => [group.id, group.cues.map(cue => cue.id)]));
  const snapshot = () => ({expectedRevision: baseRevision,
    sections: groups.map(({id, title, collapsed}) => ({id, title, collapsed})),
    cues: groups.flatMap(group => group.cues.map(cue => ({id: cue.id, sectionId: group.id}))) });
  const listen = (target, type, handler, options) => {
    target.addEventListener(type, handler, options);
    listeners.push(() => target.removeEventListener(type, handler, options));
  };
  function handleFor(kind, id) {
    return [...list.querySelectorAll('[data-sort-kind]')].find(handle => handle.dataset.sortKind === kind && handle.dataset.sortId === id);
  }
  function refreshLanguage() {
    if (!dialog) return;
    heading.textContent = t('Sort Playlist');
    instructions.textContent = t('Drag section and cue names to reorder. Drop cues into another section.');
    keyboardHelp.textContent = t('Keyboard: Alt + ↑/↓ to reorder; Alt + ←/→ to move a cue between sections.');
    list.setAttribute('aria-label', t('Playlist order'));
    cancel.textContent = t('Cancel');
    for (const handle of list.querySelectorAll('[data-sort-kind]')) {
      const group = groups.find(item => item.id === handle.dataset.sectionId);
      const label = handle.dataset.sortKind === 'section' ? group?.title : group?.cues.find(cue => cue.id === handle.dataset.cueId)?.label;
      handle.setAttribute('aria-label', t(handle.dataset.sortKind === 'section' ? 'Move section {0}' : 'Move cue {0}', {0: label || ''}));
    }
    for (const empty of list.querySelectorAll('.playlist-sorter-empty')) empty.textContent = t('Drop cues here');
    updateControls();
  }
  function updateControls() {
    if (!dialog) return;
    save.textContent = t(saving ? 'Saving…' : 'Save order');
    save.disabled = saving || !available || order() === originalOrder;
    cancel.disabled = saving;
    dialog.setAttribute('aria-busy', String(saving));
    availabilityBox.hidden = available;
    availabilityBox.textContent = available ? '' : t(availabilityMessage || 'Playlist editing is unavailable right now.');
    for (const handle of list.querySelectorAll('[data-sort-kind]')) {
      // aria-disabled keeps keyboard focus stable during connection changes.
      handle.setAttribute('aria-disabled', String(saving || !available));
    }
  }
  function makeHandle(kind, id, sectionId, label) {
    const handle = node('button', `playlist-sorter-handle playlist-sorter-${kind}-handle`);
    handle.type = 'button'; handle.draggable = false;
    handle.dataset.sortKind = kind; handle.dataset.sortId = id; handle.dataset.sectionId = sectionId;
    if (kind === 'cue') handle.dataset.cueId = id;
    handle.setAttribute('aria-describedby', 'playlist-sorter-keyboard');
    const grip = node('span', 'playlist-sorter-grip', '⠿'); grip.setAttribute('aria-hidden', 'true');
    handle.append(grip, node('span', 'playlist-sorter-name', label));
    return handle;
  }
  function render(focus) {
    list.replaceChildren();
    for (const group of groups) {
      const section = node('div', 'playlist-sorter-section');
      section.dataset.sectionId = group.id; section.setAttribute('role', 'listitem');
      const header = makeHandle('section', group.id, group.id, group.title);
      const cues = node('div', 'playlist-sorter-cues');
      cues.dataset.sectionId = group.id; cues.setAttribute('role', 'list'); cues.setAttribute('aria-label', group.title);
      for (const cue of group.cues) {
        const row = node('div', 'playlist-sorter-cue');
        row.dataset.cueId = cue.id; row.dataset.sectionId = group.id; row.setAttribute('role', 'listitem');
        row.append(makeHandle('cue', cue.id, group.id, cue.label)); cues.append(row);
      }
      if (!group.cues.length) cues.append(node('p', 'playlist-sorter-empty', t('Drop cues here')));
      section.append(header, cues); list.append(section);
    }
    refreshLanguage();
    if (focus) {
      const handle = handleFor(focus.kind, focus.id);
      handle?.focus({preventScroll: true}); handle?.scrollIntoView({block: 'nearest'});
    }
  }
  function announce(label) { live.textContent = t('Moved {0}.', {0: label}); }
  function move(kind, id, target) {
    const before = order();
    let label;
    if (kind === 'section') {
      const index = groups.findIndex(group => group.id === id);
      if (index < 0 || target.beforeSectionId === id) return;
      const [group] = groups.splice(index, 1); label = group.title;
      const destination = target.beforeSectionId ? groups.findIndex(item => item.id === target.beforeSectionId) : groups.length;
      groups.splice(destination < 0 ? groups.length : destination, 0, group);
    } else {
      const source = groups.find(group => group.cues.some(cue => cue.id === id));
      const destination = groups.find(group => group.id === target.sectionId);
      if (!source || !destination || target.beforeCueId === id) return;
      const [cue] = source.cues.splice(source.cues.findIndex(item => item.id === id), 1); label = cue.label;
      const index = target.beforeCueId ? destination.cues.findIndex(item => item.id === target.beforeCueId) : destination.cues.length;
      destination.cues.splice(index < 0 ? destination.cues.length : index, 0, cue);
    }
    if (order() !== before) { errorBox.hidden = true; render({kind, id}); announce(label); }
    else updateControls();
  }
  function targetAt(x, y, kind, id) {
    const bounds = list.getBoundingClientRect();
    if (x < bounds.left || x > bounds.right || y < bounds.top || y > bounds.bottom) return null;
    const sections = [...list.querySelectorAll('.playlist-sorter-section')];
    if (kind === 'section') {
      const before = sections.filter(section => section.dataset.sectionId !== id).find(section => {
        const rect = section.firstElementChild.getBoundingClientRect();
        return y < rect.top + rect.height / 2;
      });
      return {beforeSectionId: before?.dataset.sectionId || null, parent: list, before: before || null};
    }
    const section = sections.find(item => y <= item.getBoundingClientRect().bottom) || sections[sections.length - 1];
    if (!section) return null;
    const cues = section.querySelector('.playlist-sorter-cues');
    const before = [...cues.querySelectorAll('.playlist-sorter-cue')].filter(row => row.dataset.cueId !== id).find(row => {
      const rect = row.getBoundingClientRect(); return y < rect.top + rect.height / 2;
    });
    return {sectionId: section.dataset.sectionId, beforeCueId: before?.dataset.cueId || null,
      parent: cues, before: before || null};
  }
  function updateDrop() {
    if (!drag?.started) return;
    drag.ghost.style.transform = `translate(${drag.x - drag.offsetX}px, ${drag.y - drag.offsetY}px)`;
    // A previous marker changes geometry; remove it before measuring a target.
    drag.marker.remove();
    drag.target = targetAt(drag.x, drag.y, drag.kind, drag.id);
    if (drag.target) drag.target.parent.insertBefore(drag.marker, drag.target.before);
  }
  function autoScroll() {
    if (!drag?.started) return;
    const bounds = list.getBoundingClientRect();
    if (drag.x >= bounds.left && drag.x <= bounds.right) {
      const edge = 40, above = bounds.top + edge - drag.y, below = drag.y - (bounds.bottom - edge);
      const speed = above > 0 ? -Math.min(15, above / 3) : below > 0 ? Math.min(15, below / 3) : 0;
      if (speed) {
        const previous = list.scrollTop; list.scrollTop += speed;
        if (list.scrollTop !== previous) updateDrop();
      }
    }
    frame = requestAnimationFrame(autoScroll);
  }
  function endDrag(commit) {
    if (!drag) return;
    const completed = drag; drag = null;
    cancelAnimationFrame(frame); frame = 0;
    completed.ghost?.remove(); completed.marker?.remove();
    completed.source.classList.remove('is-dragging');
    dialog.classList.remove('is-dragging');
    try { if (completed.handle.hasPointerCapture(completed.pointerId)) completed.handle.releasePointerCapture(completed.pointerId); } catch {}
    if (commit && completed.started && completed.target && available && !saving) move(completed.kind, completed.id, completed.target);
  }
  function pointerDown(event) {
    const handle = event.target.closest('[data-sort-kind]');
    if (!handle || !list.contains(handle) || !event.isPrimary || event.button !== 0 || !available || saving || drag) return;
    event.preventDefault(); event.stopPropagation(); handle.focus({preventScroll: true});
    const rect = handle.getBoundingClientRect();
    drag = {handle, pointerId: event.pointerId, kind: handle.dataset.sortKind, id: handle.dataset.sortId,
      source: handle.closest(handle.dataset.sortKind === 'section' ? '.playlist-sorter-section' : '.playlist-sorter-cue'),
      startX: event.clientX, startY: event.clientY, x: event.clientX, y: event.clientY,
      offsetX: event.clientX - rect.left, offsetY: event.clientY - rect.top, width: rect.width, started: false};
  }
  function pointerMove(event) {
    if (!drag || event.pointerId !== drag.pointerId) return;
    drag.x = event.clientX; drag.y = event.clientY;
    if (!drag.started && Math.hypot(drag.x - drag.startX, drag.y - drag.startY) < 4) return;
    event.preventDefault();
    if (!drag.started) {
      drag.started = true;
      drag.ghost = node('div', 'playlist-sorter-ghost', drag.handle.querySelector('.playlist-sorter-name').textContent);
      drag.ghost.style.width = `${drag.width}px`; drag.ghost.setAttribute('aria-hidden', 'true');
      drag.marker = node('div', 'playlist-sorter-drop-marker'); drag.marker.setAttribute('aria-hidden', 'true');
      dialog.append(drag.ghost); drag.source.classList.add('is-dragging'); dialog.classList.add('is-dragging');
      try { drag.handle.setPointerCapture(drag.pointerId); } catch {}
      frame = requestAnimationFrame(autoScroll);
    }
    updateDrop();
  }
  function keyboard(event) {
    if (event.key === 'Escape') {
      event.preventDefault(); event.stopPropagation();
      if (!saving) close();
      return;
    }
    const handle = event.target.closest('[data-sort-kind]');
    if (!handle || !event.altKey || !['ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight'].includes(event.key)) return;
    event.preventDefault(); event.stopPropagation();
    if (!available || saving || drag) return;
    const kind = handle.dataset.sortKind, id = handle.dataset.sortId;
    const groupIndex = groups.findIndex(group => group.id === handle.dataset.sectionId), group = groups[groupIndex];
    if (kind === 'section') {
      if (event.key === 'ArrowUp' && groupIndex > 0) move(kind, id, {beforeSectionId: groups[groupIndex - 1].id});
      if (event.key === 'ArrowDown' && groupIndex < groups.length - 1) move(kind, id, {beforeSectionId: groups[groupIndex + 2]?.id || null});
    } else if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') {
      const destination = groups[groupIndex + (event.key === 'ArrowLeft' ? -1 : 1)];
      if (destination) move(kind, id, {sectionId: destination.id, beforeCueId: null});
    } else {
      const index = group.cues.findIndex(cue => cue.id === id);
      if (event.key === 'ArrowUp' && index > 0) move(kind, id, {sectionId: group.id, beforeCueId: group.cues[index - 1].id});
      if (event.key === 'ArrowDown' && index < group.cues.length - 1) move(kind, id, {sectionId: group.id, beforeCueId: group.cues[index + 2]?.id || null});
    }
  }
  function close(reason = 'cancelled') {
    if (saving) return false;
    const wasOpen = opening || dialog?.open;
    ++cycle; opening = false; endDrag(false);
    if (dialog?.open) dialog.close();
    groups = [];
    if (wasOpen) {
      onClose(reason);
      if (opener?.isConnected && !opener.disabled) opener.focus({preventScroll: true});
    }
    return true;
  }
  async function saveOrder() {
    if (saving || !available || drag || order() === originalOrder) return;
    const currentCycle = cycle;
    saving = true; errorBox.hidden = true; updateControls();
    try {
      const result = await onSave(snapshot());
      if (currentCycle !== cycle) return;
      saving = false;
      if (result === false) {
        errorBox.textContent = t('Changes were not saved. Reload the playlist and try again.'); errorBox.hidden = false;
      } else close('saved');
    } catch (error) {
      if (currentCycle !== cycle) return;
      errorBox.textContent = formatError(error) || t('Could not save the playlist order.'); errorBox.hidden = false;
    } finally {
      if (currentCycle === cycle) { saving = false; updateControls(); }
    }
  }
  function ensureDialog() {
    if (dialog) return;
    dialog = node('dialog', 'playlist-sorter'); dialog.id = 'playlist-sorter';
    dialog.setAttribute('aria-labelledby', 'playlist-sorter-heading');
    dialog.setAttribute('aria-describedby', 'playlist-sorter-instructions');
    const header = node('div', 'playlist-sorter-header');
    heading = node('h2'); heading.id = 'playlist-sorter-heading';
    instructions = node('p'); instructions.id = 'playlist-sorter-instructions';
    header.append(heading, instructions);
    list = node('div', 'playlist-sorter-list'); list.setAttribute('role', 'list');
    keyboardHelp = node('p', 'playlist-sorter-keyboard'); keyboardHelp.id = 'playlist-sorter-keyboard';
    errorBox = node('p', 'playlist-sorter-error'); errorBox.id = 'playlist-sorter-error'; errorBox.setAttribute('role', 'alert'); errorBox.hidden = true;
    availabilityBox = node('p', 'playlist-sorter-availability'); availabilityBox.setAttribute('role', 'status'); availabilityBox.hidden = true;
    live = node('p', 'playlist-sorter-live'); live.setAttribute('role', 'status'); live.setAttribute('aria-live', 'polite');
    const footer = node('div', 'playlist-sorter-footer'), buttons = node('div', 'playlist-sorter-actions');
    cancel = node('button', 'quiet'); cancel.id = 'playlist-sorter-cancel'; cancel.type = 'button';
    save = node('button', 'accent'); save.id = 'playlist-sorter-save'; save.type = 'button';
    buttons.append(cancel, save); footer.append(keyboardHelp, availabilityBox, errorBox, buttons, live);
    dialog.append(header, list, footer); document.body.append(dialog);
    listen(cancel, 'click', () => close()); listen(save, 'click', () => { void saveOrder(); });
    listen(dialog, 'keydown', keyboard);
    listen(dialog, 'cancel', event => { event.preventDefault(); if (!saving) close(); });
    listen(dialog, 'dragstart', event => event.preventDefault());
    listen(list, 'pointerdown', pointerDown);
    listen(document, 'pointermove', pointerMove, {passive: false});
    listen(document, 'pointerup', event => {
      if (!drag || event.pointerId !== drag.pointerId) return;
      if (drag.started) { event.preventDefault(); drag.x = event.clientX; drag.y = event.clientY; updateDrop(); }
      endDrag(true);
    });
    listen(document, 'pointercancel', event => { if (event.pointerId === drag?.pointerId) endDrag(false); });
    listen(dialog, 'lostpointercapture', event => {
      // Touch can transfer implicit capture from a nested name span to the
      // handle. Only losing the handle's own capture cancels this drag.
      if (event.pointerId === drag?.pointerId && event.target === drag?.handle) endDrag(false);
    });
    listen(window, 'blur', () => endDrag(false));
  }
  return {
    ready,
    async open(playlist, returnFocus = document.activeElement) {
      if (opening || dialog?.open) return false;
      const revision = playlist.playlistRevision;
      const next = playlist.sections.map(section => ({id: section.id, title: section.title, collapsed: Boolean(section.collapsed), cues: []}));
      const byID = new Map(next.map(group => [group.id, group]));
      if (!next.length || byID.size !== next.length) throw new Error('Invalid playlist sorting data');
      const cueIDs = new Set();
      for (const cue of playlist.cues) {
        const group = byID.get(cue.sectionId || next[0].id);
        if (!group || cueIDs.has(cue.id)) throw new Error('Invalid playlist sorting data');
        cueIDs.add(cue.id); group.cues.push({id: cue.id, label: cue.label});
      }
      const currentCycle = ++cycle; opening = true; opener = returnFocus;
      try { await ready; } catch (error) { if (cycle === currentCycle) opening = false; throw error; }
      if (cycle !== currentCycle) return false;
      ensureDialog(); groups = next; baseRevision = revision; originalOrder = order();
      saving = false; errorBox.hidden = true; live.textContent = ''; render();
      opening = false; dialog.showModal();
      list.querySelector('[data-sort-kind]')?.focus({preventScroll: true});
      return true;
    },
    close,
    isOpen: () => Boolean(opening || dialog?.open),
    refreshLanguage,
    setAvailable(value, messageKey = '') {
      available = Boolean(value); availabilityMessage = messageKey;
      if (!available) endDrag(false);
      updateControls();
    },
    destroy() {
      saving = false; close();
      for (const remove of listeners) remove();
      dialog?.remove(); dialog = null;
    }
  };
}
