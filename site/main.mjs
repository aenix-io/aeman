import { readMode, resolveTheme, saveMode } from './appearance.mjs';

const root = document.documentElement;
const media = matchMedia('(prefers-color-scheme: dark)');
const selector = document.querySelector('#theme');
// Accessing localStorage itself may throw, not just getItem/setItem.
let storage;
try { storage = localStorage; } catch { /* Fall back to system without persistence. */ }
let mode = readMode(storage);
selector.value = mode;

function applyTheme() {
  const theme = resolveTheme(mode, media.matches);
  root.dataset.theme = theme;
  document.querySelector('meta[name="theme-color"]').content = theme === 'dark' ? '#0d1117' : '#ffffff';
  for (const source of document.querySelectorAll('picture source')) {
    source.media = theme === 'dark' ? 'all' : 'not all';
  }
  for (const link of document.querySelectorAll('[data-fullsize]')) {
    link.href = `assets/${link.dataset.fullsize}-${theme}.png`;
  }
}
selector.addEventListener('change', () => {
  mode = selector.value;
  saveMode(storage, mode);
  applyTheme();
});
media.addEventListener('change', applyTheme);
applyTheme();

for (const button of document.querySelectorAll('[data-view]')) {
  button.addEventListener('click', () => {
    document.querySelector('.preview-hint').textContent = button.dataset.view === 'me'
      ? 'Your day across your teams, with notes alongside.'
      : 'People × zones. Everyone’s day at a glance.';
    for (const toggle of document.querySelectorAll('[data-view]')) {
      toggle.setAttribute('aria-pressed', String(toggle.dataset.view === button.dataset.view));
    }
    for (const panel of document.querySelectorAll('[data-board]')) {
      panel.hidden = panel.dataset.board !== button.dataset.view;
    }
  });
}

document.querySelector('#copy').addEventListener('click', async () => {
  const status = document.querySelector('#copy-status');
  try {
    await navigator.clipboard.writeText(document.querySelector('#commands').textContent);
    status.textContent = 'Commands copied.';
  } catch {
    status.textContent = 'Select the commands and copy them manually.';
  }
});

// Native details handles the mobile menu and keyboard interaction without JS.
for (const link of document.querySelectorAll('.mobile-nav a')) {
  link.addEventListener('click', () => { document.querySelector('.mobile-nav').open = false; });
}
