// Same modes and data-theme contract as web/src/theme.ts; separate site storage.
const key = 'aeman.site.themeMode';
const modes = ['light', 'dark', 'system'];

export function readMode(storage) {
  try {
    const mode = storage.getItem(key);
    return modes.includes(mode) ? mode : 'system';
  } catch { return 'system'; }
}

export function saveMode(storage, mode) {
  try { storage.setItem(key, mode); } catch { /* Private browsing may deny storage. */ }
}

export function resolveTheme(mode, prefersDark) {
  return mode === 'system' ? (prefersDark ? 'dark' : 'light') : mode;
}
