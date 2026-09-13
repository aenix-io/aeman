// Optional maintainer tool, not part of the Pages build. Requires Playwright
// (PLAYWRIGHT_MODULE may point to an existing installation) and web/npm ci.
// Renders the real App.tsx with local sample API responses; no real board is read.
import { createRequire } from 'node:module';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { mkdir } from 'node:fs/promises';
import { resolve } from 'node:path';

const root = fileURLToPath(new URL('..', import.meta.url));
const require = createRequire(resolve(root, 'web/package.json'));
const { createServer } = await import(pathToFileURL(require.resolve('vite')));
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const day = '2026-09-11';
const examples = [
  ['alex', 'urgent', 'Restore the production rollout', 60, ''],
  ['alex', 'unplanned', 'Investigate registry timeouts', 30, ''],
  ['alex', 'planned', 'Roll out the new node image', 80, 'review'],
  ['alex', 'planned', 'Document the rollback procedure', 20, ''],
  ['alex', 'niceToHave', 'Simplify the local dev setup', 0, ''],
  ['maya', 'urgent', 'Resolve the certificate renewal alert', 90, ''],
  ['maya', 'unplanned', 'Help debug a failing deployment', 40, ''],
  ['maya', 'planned', 'Review the cluster upgrade plan', 70, ''],
  ['maya', 'niceToHave', 'Clean up old dashboard panels', 0, ''],
];
const cards = examples.map(([person, zone, title, progress, stage], index) => ({
  kind: 'Card', metadata: { uid: `sample-${index}`, author: person, createdAt: `${day}T08:00:00Z` },
  spec: { title, assignees: [person], team: 'platform', zone, progress, stage,
    dates: { start: day, sprint: day }, description: 'Sample card for the AEMAN product preview.' },
  status: { domain: 'engineering', complete: progress === 100 },
}));
const server = await createServer({ root: resolve(root, 'web'), server: { host: '127.0.0.1', port: 5174 } });
await server.listen();
const browser = await chromium.launch({ headless: true,
  ...(process.env.BROWSER_PATH ? { executablePath: process.env.BROWSER_PATH } : {}) });
try {
  await mkdir(resolve(root, 'site/assets'), { recursive: true });
  for (const theme of ['light', 'dark']) {
    for (const view of ['me', 'team']) {
      const context = await browser.newContext({ viewport: { width: 1280, height: 640 },
        deviceScaleFactor: 1, colorScheme: theme, timezoneId: 'UTC' });
      const page = await context.newPage();
      const errors = [];
      page.on('pageerror', error => errors.push(error.message));
      await page.clock.install({ time: new Date(`${day}T10:00:00Z`) });
      await page.addInitScript(({ theme, view }) => {
        localStorage.setItem('aeman.themeMode', theme);
        localStorage.setItem('aeman.view', view);
      }, { theme, view });
      await page.routeWebSocket(/\/api\//, socket => socket.onMessage(() => {}));
      await page.route(url => url.pathname.startsWith('/api/'), async route => {
        const pathname = new URL(route.request().url()).pathname;
        let body;
        if (pathname === '/api/config') body = { mode: 'local', version: 'preview', login: 'alex',
          tokenAvailable: true, authenticated: true, tz: 'UTC' };
        else if (pathname === '/api/healthz') body = { status: 'ok' };
        else if (pathname === '/api/v1/board') body = { kind: 'Board', metadata: {
          title: 'Engineering', teams: ['platform'], projects: [],
          members: ['alex', 'maya'].map(login => ({ login })),
          domains: [{ name: 'engineering', writable: true, members: ['alex', 'maya'] }],
        } };
        else if (pathname === '/api/v1/sprints') body = { kind: 'SprintList', items: [
          { kind: 'Sprint', metadata: { team: 'platform' }, spec: { current: day } },
        ] };
        else if (pathname.endsWith('/cards')) body = { kind: 'CardList', items: cards };
        else if (pathname.endsWith('/notes')) body = { kind: 'NoteList', items: [] };
        else if (pathname.endsWith('/logs')) body = { cards: {
          'sample-0': [{ type: 'note', id: 'note-1', actor: 'alex', at: `${day}T09:10:00Z`,
            text: 'Canary rollout is healthy. Checking error rates before expanding to the remaining nodes.' }],
          'sample-2': [{ type: 'note', id: 'note-2', actor: 'alex', at: `${day}T09:35:00Z`,
            text: 'Node image verified in staging. Ready for a second pair of eyes on the rollout plan.' }],
        } };
        else if (pathname.endsWith('/processes')) body = { items: [] };
        else if (pathname.endsWith('/presence')) body = {};
        else { errors.push(`Unexpected preview API request: ${pathname}`); body = {}; }
        await route.fulfill({ contentType: 'application/json', body: JSON.stringify(body) });
      });
      // Fail closed: screenshot fixtures never send requests to a forge/CDN.
      await page.route(/^https?:\/\/(?!127\.0\.0\.1)/, route => route.abort());
      await page.goto(server.resolvedUrls.local[0]);
      try {
        await page.locator('.card-title[title="Restore the production rollout"]').waitFor({ timeout: 10000 });
        if (view === 'me') {
          await page.getByText('Canary rollout is healthy. Checking error rates before expanding to the remaining nodes.',
            { exact: true }).waitFor({ timeout: 10000 });
        }
      } catch (error) {
        console.error(errors, await page.locator('body').innerText());
        await page.screenshot({ path: '/tmp/aeman-capture-error.png' });
        throw error;
      }
      if (errors.length) throw new Error(errors.join('\n'));
      await page.screenshot({ path: resolve(root, `site/assets/${view}-${theme}.png`) });
      console.log(`Captured actual ${view} view (${theme}), sample data`);
      await context.close();
    }
  }
} finally {
  await browser.close();
  await server.close();
}
