import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readMode, resolveTheme, saveMode } from './appearance.mjs';

test('system is the default, including invalid and inaccessible storage', () => {
  for (const value of [null, '', 'sepia', 'LIGHT']) {
    assert.equal(readMode({ getItem: () => value }), 'system');
  }
  assert.equal(readMode({ getItem() { throw new Error('blocked'); } }), 'system');
});

test('explicit choices override the OS; system follows OS changes', () => {
  for (const prefersDark of [false, true]) {
    assert.equal(resolveTheme('light', prefersDark), 'light');
    assert.equal(resolveTheme('dark', prefersDark), 'dark');
    assert.equal(resolveTheme('system', prefersDark), prefersDark ? 'dark' : 'light');
  }
});

test('preferences persist separately from the app and tolerate failed writes', () => {
  const values = new Map([['aeman.themeMode', 'dark']]);
  const storage = { getItem: key => values.get(key), setItem: (key, value) => values.set(key, value) };
  saveMode(storage, 'light');
  assert.equal(readMode(storage), 'light');
  assert.equal(values.get('aeman.themeMode'), 'dark');
  assert.doesNotThrow(() => saveMode({ setItem() { throw new Error('quota'); } }, 'dark'));
});
