import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFile, access } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { resolve } from 'node:path';

const root = fileURLToPath(new URL('..', import.meta.url));
const output = resolve(root, 'site/dist');
const html = await readFile(resolve(output, 'index.html'), 'utf8');

test('all local links, anchors and images work at a project Pages base path', async () => {
  const ids = new Set([...html.matchAll(/\bid="([^"]+)"/g)].map(match => match[1]));
  for (const [, reference] of html.matchAll(/\b(?:href|src|srcset)="([^"]+)"/g)) {
    const url = new URL(reference, 'https://example.github.io/aeman/');
    if (url.origin !== 'https://example.github.io') continue;
    assert.ok(url.pathname.startsWith('/aeman/'), reference);
    const relative = url.pathname.slice('/aeman/'.length) || 'index.html';
    await access(resolve(output, relative));
    if (url.hash) assert.ok(ids.has(decodeURIComponent(url.hash.slice(1))), reference);
  }
});

test('documentation links and quick-start commands match this checkout', async () => {
  const readme = await readFile(resolve(root, 'README.md'), 'utf8');
  const commands = html.match(/<code id="commands">([^<]+)<\/code>/)[1];
  for (const command of commands.split('\n')) assert.ok(readme.includes(command), command);
  for (const [, path] of html.matchAll(/https:\/\/github.com\/aenix-io\/aeman\/(?:blob|tree)\/main\/([^"#]+)/g)) {
    await access(resolve(root, path));
  }
});

test('tokens and logo derive from the product; the output is framework-free', async () => {
  const tokens = await readFile(resolve(output, 'assets/tokens.css'), 'utf8');
  for (const token of ['--fg: #1f2328', '--bg: #ffffff', '--accent: #0969da',
    '--fg: #e6edf3', '--bg: #0d1117', '--accent: #4493f8', '--radius: 6px']) {
    assert.ok(tokens.includes(token), token);
  }
  assert.ok(tokens.includes('prefers-color-scheme: dark'));
  assert.ok(!tokens.includes('.app-header'));
  assert.ok(html.includes('viewBox="0 0 491.299 100.0"'));
  assert.ok(!html.includes('{{'));
  assert.ok(!html.includes('/src/'));
  assert.match(html, /<script type="module" src="main.mjs"><\/script>/);
});

test('both real board screenshots and both themes are shipped with explicit sample labels', async () => {
  assert.ok(html.includes('sample data'));
  for (const view of ['me', 'team']) {
    for (const theme of ['light', 'dark']) {
      const bytes = await readFile(resolve(output, `assets/${view}-${theme}.png`));
      assert.equal(bytes.subarray(1, 4).toString(), 'PNG');
      assert.equal(bytes.readUInt32BE(16), 1280);
      assert.equal(bytes.readUInt32BE(20), 640);
    }
  }
});

test('canonical and social URLs share the configured deployment base', () => {
  const canonical = html.match(/rel="canonical" href="([^"]+)"/)[1];
  assert.ok(canonical.endsWith('/'));
  assert.ok(html.includes(`property="og:url" content="${canonical}"`));
  assert.ok(html.includes(`property="og:image" content="${canonical}assets/team-light.png"`));
});
