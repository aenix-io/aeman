# Deploying aeman (self-hosted, multi-user)

Runs aeman behind Caddy, which terminates TLS with an automatic Let's Encrypt certificate. Each visitor signs in with the forge the board repositories live on — GitHub or GitLab (gitlab.com or self-hosted) — and acts with **their own** token; the server fetches and pushes with its own (`AEMAN_GIT_TOKEN`).

```
browser ──HTTPS──► Caddy (:443, auto-TLS) ──http──► aeman:8765
```

## 1. Register an OAuth application

One forge per instance: register the application at GitHub **or** GitLab and fill in that pair of variables only. On both, the callback is `<AEMAN_BASE_URL>/auth/callback`.

### GitHub — a GitHub App (recommended)

A GitHub App gives both halves at once: the sign-in (its Client ID/secret work in the same OAuth endpoints, and the consent screen shows the app's per-repository permissions instead of `repo` — full access to every private repository the person has), and the **server credential without a PAT** — the server signs a JWT with the app's private key and mints installation tokens, scoped to the repositories the app is installed on, renewed automatically. Nothing to issue by hand, nothing that quietly expires in a `.env` file.

GitHub → the organisation's **Settings** → Developer settings → **GitHub Apps** → **New GitHub App**:

- **Homepage URL:** `https://aeman.example.com`
- **Callback URL:** `https://aeman.example.com/auth/callback`; keep **Expire user authorization tokens** on, and leave **Request user authorization (OAuth) during installation** OFF — aeman starts its own sign-in from its login button, and this option makes GitHub start one after every installation, landing people on the callback out of the blue (the server treats an installation-signed callback as the setup event, but the extra round trip serves nobody)
- **Webhook:** off
- **Setup URL (optional):** `https://aeman.example.com/auth/setup`, with **Redirect on update** checked — after someone installs the app (say, on their personal repository), GitHub sends them back to the board, which retries the attach at once
- **Repository permissions:** **Contents: Read and write**, **Metadata: Read-only** (automatic)
- **Where can this app be installed:** Any account, if boards of other organisations (or personal boards) will use it

Generate a **client secret** and a **private key** (a `.pem` download). Then **install the app**: on each organisation the board's repositories live in, choosing those repositories.

**Personal boards, if the app also signs people in.** A personal board is cloned and pushed with its *owner's* credential — the server's has no business in someone's private repository — so what that credential can reach follows the sign-in. Through a GitHub App a person's token reaches only the repositories the app is installed on, so each person who wants a personal board installs the app on their own repository once (the same install link, choosing their own account); the board says so, with the link, when it refuses. To avoid that step entirely, sign people in through an **OAuth App** (below) while keeping the GitHub App as the server credential: the two are configured independently, the PAT still disappears, and only the consent screen stays wide. The five variables: `AEMAN_GITHUB_CLIENT_ID` / `AEMAN_GITHUB_CLIENT_SECRET` (sign-in), `AEMAN_GITHUB_APP_ID` (the numeric App ID) and `AEMAN_GITHUB_APP_KEY` (the key PEM, or base64 of it — a `.env` file cannot hold a multiline value) or `AEMAN_GITHUB_APP_KEY_FILE` (a path). With the App configured, `AEMAN_GIT_TOKEN*` is not needed; a repository that names its own token keeps it, the App covers the rest.

### GitHub — an OAuth App (the static-token alternative)

GitHub → Settings → Developer settings → **OAuth Apps** → **New OAuth App**:

- **Homepage URL:** `https://aeman.example.com`
- **Authorization callback URL:** `https://aeman.example.com/auth/callback`

Generate a client secret. Keep the **Client ID** and **Client secret** — they become `AEMAN_GITHUB_CLIENT_ID` / `AEMAN_GITHUB_CLIENT_SECRET`. In this mode the sign-in asks for the `repo` scope (a private board repository is invisible to a token without it — and the consent screen therefore offers access to everything private the person has), and the server needs its own PAT: `AEMAN_GIT_TOKEN`, or one per repository.

### GitLab

GitLab → User Settings (or a group's **Settings**, or the **Admin area**) → **Applications** → **Add new application**:

- **Name:** `aeman`
- **Redirect URI:** `https://aeman.example.com/auth/callback`
- **Confidential:** yes
- **Scopes:** `read_user`, `read_api`, `write_repository`

Save it. Keep the **Application ID** and **Secret** — they become `AEMAN_GITLAB_CLIENT_ID` / `AEMAN_GITLAB_CLIENT_SECRET`. On a self-hosted GitLab also set `AEMAN_GITLAB_URL` to its base URL (`https://gitlab.example.com`); on gitlab.com nothing more is needed.

## 2. DNS and firewall

- Add a DNS **A record**: `aeman.example.com` → your server IP (DNS-only / not proxied, so Caddy can solve the ACME challenge).
- Open inbound TCP **80** and **443** on the server.

## 3. Configure and run

```sh
git clone https://github.com/aenix-io/aeman.git
cd aeman
cp .env.example .env
# fill AEMAN_REPOS, AEMAN_GIT_TOKEN, AEMAN_BASE_URL, AEMAN_DOMAIN and ONE pair:
#   AEMAN_GITHUB_CLIENT_ID / _SECRET, or
#   AEMAN_GITLAB_CLIENT_ID / _SECRET (+ AEMAN_GITLAB_URL for a self-hosted GitLab)
docker compose up -d --build
```

`AEMAN_REPOS` names the board's repositories as `name=url`, comma-separated, the primary first — HTTPS clone URLs on the forge, e.g. `aeman-db=https://github.com/acme/aeman-db.git` or `aeman-db=https://gitlab.com/acme/aeman-db.git`. `AEMAN_GIT_TOKEN` is the server's own credential for them (fetch, push, and the membership checks behind the assignee pickers); it is required in this mode. An **empty** repository becomes a board first, on either forge: `aeman init --repo <url>` — from this directory, `docker compose run --rm aeman init --repo https://gitlab.com/acme/aeman-db.git`. `serve` refuses an unborn remote and names that command.

`AEMAN_GIT_TOKEN` is the server's own credential for all of them. A board whose repositories live in **two organisations** gives each its own instead: `AEMAN_GIT_TOKEN_<NAME>`, named after the domain (upper-cased, anything but a letter or a digit an underscore — `founders` → `AEMAN_GIT_TOKEN_FOUNDERS`). This is what lets each token stay narrow: a fine-grained GitHub token belongs to one organisation, so one token cannot cover repositories in two, and a classic token that could is wider than either repository needs. Pass the variables you use through the compose file's `environment` block.

Caddy issues the certificate on first start (needs DNS + ports 80/443 already in place). Open `https://aeman.example.com`, click **Sign in with GitHub** (or **GitLab**), and you're in.

## Choosing the forge (optional)

`AEMAN_FORGE=github|gitlab` names the forge explicitly. Unset, it follows the primary repository's host: `github.com` → GitHub, a host containing `gitlab` → GitLab, anything else → GitHub unless `AEMAN_GITLAB_URL` is set. A self-hosted GitLab on a host without `gitlab` in its name therefore needs `AEMAN_GITLAB_URL` (or `AEMAN_FORGE=gitlab`, in which case the URL defaults to `https://<host of the primary repository>`).

## Lock to one board (optional)

To pin every visitor to a single project and hide the board picker, set in `.env`:

```sh
AEMAN_OWNER=acme
AEMAN_BOARD=37
AEMAN_LOCK_BOARD=true
```

A user whose token can't read that board sees an access-denied screen rather than the board.

## Notes

- **Who sees what.** A visitor's own token decides per repository. GitHub: the repository's `permissions` — `pull` reads, `push`/`maintain`/`admin` write. GitLab: the project's access level — Reporter (20) reads, Developer (30) and above write; a Guest sees the project but not its board; public and internal projects read for anyone signed in. The assignee pickers list the people who can read the repository: on GitHub each login's collaborator permission, asked with the server token; on GitLab the project's member list (inherited group members included), which also supplies display names and avatars.
- **Sessions & the session store.** `AEMAN_SESSION_FILE` (on the `aeman_sessions` volume) always persists the dynamic MCP client registry. Sessions — the visitors' forge tokens — are written there only when `AEMAN_SESSION_KEY` is set, encrypted with it (AES-256-GCM); then restarts and redeploys keep users signed in and MCP tokens live. Without the key, sessions stay in memory and a restart signs everyone out — no plaintext token ever touches disk either way. Keep the key stable (changing or losing it just signs everyone out) and store it outside the session volume so a leak of that volume alone exposes nothing. GitHub's classic OAuth App tokens don't expire, so a session simply lasts up to 14 days.
- **Scopes:** `AEMAN_SCOPES` overrides what the sign-in asks for; the default is `repo` on GitHub (a GitHub App ignores the parameter — its permissions come from the installation) and `read_user read_api write_repository` on GitLab.
- **Cert storage:** the `caddy_data` volume persists issued certificates across restarts.
- **Local CLI mode is unchanged:** without a client id/secret pair the binary still runs as a single-user local tool on the identity of the forge CLI signed in on the machine — `gh` on GitHub, `glab` on GitLab.
- **Behind Cloudflare Tunnel instead of Caddy?** Drop the `caddy` service, add a `cloudflared` service with your tunnel token, and route the public hostname to `http://aeman:8765`; keep `AEMAN_BASE_URL` pointed at the public HTTPS URL.
