// Dependency-free static build. Product files are read, never rewritten.
import { mkdir, readFile, writeFile, copyFile, readdir } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { resolve } from 'node:path';

const root = fileURLToPath(new URL('..', import.meta.url));
const source = resolve(root, 'site');
const output = resolve(source, 'dist');
const url = new URL(process.env.SITE_URL || 'https://aenix-io.github.io/aeman/');
if (!['http:', 'https:'].includes(url.protocol) || url.search || url.hash || url.username || url.password) {
  throw new Error('SITE_URL must be an HTTP(S) site URL without credentials, query or fragment');
}
if (!url.pathname.endsWith('/')) url.pathname += '/';
const escape = value => value.replaceAll('&', '&amp;').replaceAll('"', '&quot;').replaceAll('<', '&lt;');
const css = await readFile(resolve(root, 'web/src/styles.css'), 'utf8');
const boundary = css.indexOf('html,\nbody,\n#root');
if (boundary < 0) throw new Error('Product CSS structure changed: review the theme token extraction');
const logo = (await readFile(resolve(root, 'docs/logo.svg'), 'utf8')).replaceAll('#1f2328', 'currentColor');
const html = (await readFile(resolve(source, 'index.html'), 'utf8'))
  .replaceAll('{{LOGO}}', logo).replaceAll('{{SITE_URL}}', escape(url.href));
if (/\{\{[A-Z_]+\}\}/.test(html)) throw new Error('Unresolved template placeholder');
await mkdir(resolve(output, 'assets'), { recursive: true });
await writeFile(resolve(output, 'index.html'), html);
const dark = css.match(/:root\[data-theme="dark"\]\s*\{([^}]+)\}/)?.[1];
if (!dark) throw new Error('Product dark theme not found');
await writeFile(resolve(output, 'assets/tokens.css'), css.slice(0, boundary)
  + `\n@media (prefers-color-scheme: dark) { :root:not([data-theme]) {${dark}} }\n`);
await copyFile(resolve(root, 'docs/assets/app-icon.png'), resolve(output, 'assets/favicon.png'));
for (const name of ['style.css', 'main.mjs', 'appearance.mjs']) {
  await copyFile(resolve(source, name), resolve(output, name));
}
for (const name of await readdir(resolve(source, 'assets'))) {
  if (name.endsWith('.png')) await copyFile(resolve(source, 'assets', name), resolve(output, 'assets', name));
}
await writeFile(resolve(output, '.nojekyll'), '');
console.log(`Built site/dist for ${url.href}`);
