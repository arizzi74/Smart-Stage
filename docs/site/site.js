'use strict';

(() => {
  const preferenceKey = 'smartstage.website.language';
  const supported = ['en', 'it'];

  // Ordinary links still work when JavaScript or browser storage is unavailable.
  for (const link of document.querySelectorAll('[data-language]')) {
    link.addEventListener('click', () => {
      const language = link.dataset.language;
      if (!supported.includes(language)) return;
      try { localStorage.setItem(preferenceKey, language); } catch { /* Private browsing policy. */ }
      const target = new URL(link.href);
      target.search = location.search;
      target.hash = location.hash;
      link.href = target.href;
    });
  }

  // Only the neutral entry point selects a language. Explicit /en/ and /it/
  // URLs always stay accessible, even when they differ from a saved preference.
  if (document.documentElement.dataset.languageAuto === 'true') {
    let language;
    try { language = localStorage.getItem(preferenceKey); } catch { /* Use browser preference. */ }
    if (!supported.includes(language)) {
      const preferences = [...(navigator.languages || []), navigator.language];
      language = preferences
        .filter(value => typeof value === 'string')
        .map(value => value.toLowerCase().split(/[-_]/)[0])
        .find(value => supported.includes(value)) || 'en';
    }
    const target = new URL(`${language}/`, new URL('./', location.href));
    target.search = location.search;
    target.hash = location.hash;
    location.replace(target.href);
    return;
  }

  const messages = document.documentElement.lang === 'it' ? {
    playDemo: 'Riproduci demo',
    pauseDemo: 'Ferma demo',
    copy: 'Copia comando',
    copied: 'Copiato',
    success: 'Comando copiato. Incollalo nel terminale indicato sopra.',
    manual: 'Seleziona e copia il comando',
    unavailable: 'Accesso agli appunti non disponibile. Seleziona il comando e copialo manualmente.'
  } : {
    playDemo: 'Play demo',
    pauseDemo: 'Stop demo',
    copy: 'Copy command',
    copied: 'Copied',
    success: 'Command copied. Paste it into the terminal indicated above.',
    manual: 'Select and copy above',
    unavailable: 'Clipboard access was unavailable. Select the command and copy it manually.'
  };

  // Real GIF recordings play only while visible. Reduced-motion users see the
  // static poster and can opt in. Without JS the poster links to the GIF.
  const motion = window.matchMedia('(prefers-reduced-motion: reduce)');
  for (const demo of document.querySelectorAll('[data-demo]')) {
    const img = demo.querySelector('img[data-gif]');
    const button = demo.querySelector('.demo-toggle');
    if (!img || !button) continue;
    const poster = img.getAttribute('src');
    let playing = false;
    let visible = false;
    let manual = false;
    const setPlaying = next => {
      playing = next;
      img.setAttribute('src', next ? img.dataset.gif : poster);
      button.textContent = next ? messages.pauseDemo : messages.playDemo;
      button.setAttribute('aria-pressed', String(next));
    };
    button.hidden = false;
    button.addEventListener('click', () => { manual = true; setPlaying(!playing); });
    motion.addEventListener('change', () => {
      manual = false;
      setPlaying(visible && !motion.matches);
    });
    if ('IntersectionObserver' in window) {
      new IntersectionObserver(entries => {
        visible = entries[0].isIntersecting;
        if (!visible) setPlaying(false);
        else if (!manual) setPlaying(!motion.matches);
      }, { threshold: 0.1 }).observe(demo);
    }
  }

  // Installation commands remain selectable and complete without JavaScript.
  if (navigator.clipboard && typeof navigator.clipboard.writeText === 'function') {
    const status = document.getElementById('copy-status');
    for (const button of document.querySelectorAll('[data-copy]')) {
      const command = document.getElementById(button.dataset.copy);
      if (!command) continue;
      button.hidden = false;
      button.addEventListener('click', async () => {
        button.disabled = true;
        try {
          await navigator.clipboard.writeText(command.textContent.trim());
          button.textContent = messages.copied;
          status.textContent = messages.success;
        } catch {
          button.textContent = messages.manual;
          status.textContent = messages.unavailable;
        } finally {
          button.disabled = false;
          window.setTimeout(() => { button.textContent = messages.copy; }, 2500);
        }
      });
    }
  }
})();
