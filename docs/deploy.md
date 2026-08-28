# Deploying aeman (self-hosted, multi-user)

Runs aeman behind Caddy, which terminates TLS with an automatic Let's Encrypt certificate. Each visitor signs in with the forge the board repositories live on — GitHub or GitLab (gitlab.com or self-hosted) — and acts with **their own** token; the server fetches and pushes with its own (`AEMAN_GIT_TOKEN`).

```
browser ──HTTPS──► Caddy (:443, auto-TLS) ──http──► aeman:8765
```

## 1. Register an OAuth application

One forge per instance: register the application at GitHub **or** GitLab and fill in that pair of variables only. On both, the callback is `<AEMAN_BASE_URL>/auth/callback`.

### GitHub

GitHub → Settings → Developer settings → **OAuth Apps** → **New OAuth App**:

- **Homepage URL:** `https://aeman.example.com`
- **Authorization callback URL:** `https://aeman.example.com/auth/callback`

Generate a client secret. Keep the **Client ID** and **Client secret** — they become `AEMAN_GITHUB_CLIENT_ID` / `AEMAN_GITHUB_CLIENT_SECRET`.

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
- **Scopes:** `AEMAN_SCOPES` overrides what the sign-in asks for; the default is `repo project` on GitHub and `read_user read_api write_repository` on GitLab.
- **Cert storage:** the `caddy_data` volume persists issued certificates across restarts.
- **Local CLI mode is unchanged:** without a client id/secret pair the binary still runs as a single-user local tool on the identity of the forge CLI signed in on the machine — `gh` on GitHub, `glab` on GitLab.
- **Behind Cloudflare Tunnel instead of Caddy?** Drop the `caddy` service, add a `cloudflared` service with your tunnel token, and route the public hostname to `http://aeman:8765`; keep `AEMAN_BASE_URL` pointed at the public HTTPS URL.
