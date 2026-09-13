# Project website

A separate, dependency-free static landing page. It does not change or bundle
the product application. Node.js 20+ is enough to build it; Python is optional
for the local preview server.

```sh
node --test site/appearance.test.mjs
node site/build.mjs
node --test site/build.test.mjs
python3 -m http.server 8080 --directory site/dist
```

Open `http://localhost:8080`. `site/dist/` is generated and ignored by Git.
All navigation and assets use relative URLs, including under `/aeman/`.

## Deployment

`.github/workflows/pages.yml` validates relevant pull requests and publishes
relevant pushes to `main`, or a manual run on `main`, using the official Pages
artifact and OIDC deployment actions. No deployment runs on pull requests.
After merging, select **Settings → Pages → Build and deployment → Source:
GitHub Actions** once.

The upstream URL is `https://aenix-io.github.io/aeman/`. The workflow sets
`SITE_URL` from the repository owner/name, so a fork publishes its own canonical
and social URLs. A local build defaults to the upstream URL. For a custom domain,
set `SITE_URL` in the workflow to that domain's full URL, including a trailing slash.

## Product reuse

- `build.mjs` reads the theme declarations from `web/src/styles.css` into
  `assets/tokens.css`: neutrals, semantic zones, radius, and all product palettes.
  It adds the same dark variables as a system-preference fallback for no-JS users.
- The inline wordmark comes from `docs/logo.svg` (the same artwork as `Logo.tsx`);
  the favicon is `docs/assets/app-icon.png`. They are not redrawn.
- `appearance.mjs` uses the product's Light / Dark / System and `data-theme`
  contract. `aeman.site.themeMode` keeps the landing preference separate from
  the product. Storage failures fall back to System.
- Screenshots render the actual `web/src/App.tsx`, its boards, cards and CSS.
  They contain **sample data**, not a real team's board, and are committed assets.
  No API calls, analytics, external fonts or React are shipped on the landing page.

## Refresh screenshots

Install the existing frontend dependencies (`cd web && npm ci`) and have
Playwright with Chromium available in your development environment. The optional
capture tool is not a Pages build dependency:

```sh
node site/capture.mjs
```

`PLAYWRIGHT_MODULE` can point to an existing Playwright module, and `BROWSER_PATH`
can select an installed Chromium browser. The tool starts Vite, uses a fixed
date and synthetic API responses, blocks external requests, and captures Me/Team
in both themes. It never reads a real board or uses credentials. Refresh the
four `site/assets/*.png` files when product UI changes, then rebuild the site.

The screenshot switcher, theme selector and copy button are the only page
interactions. The mobile menu uses native `details`. With JavaScript disabled,
the Team preview, system theme, navigation, commands and documentation remain
available. Dynamic star counts, animations and a live hosted demo are intentionally
omitted.
