'use strict';

// Installation commands remain selectable and complete without JavaScript.
// Clipboard access is offered only when the browser provides it.
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
        button.textContent = 'Copied';
        status.textContent = 'Command copied. Paste it into the terminal indicated above.';
      } catch {
        button.textContent = 'Select and copy above';
        status.textContent = 'Clipboard access was unavailable. Select the command and copy it manually.';
      } finally {
        button.disabled = false;
        window.setTimeout(() => { button.textContent = 'Copy command'; }, 2500);
      }
    });
  }
}
