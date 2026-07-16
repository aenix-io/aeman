# Deploying aeman (self-hosted, multi-user)

Runs aeman behind Caddy, which terminates TLS with an automatic Let's Encrypt certificate. Each visitor signs in with GitHub and acts with **their own** token.

```
browser ──HTTPS──► Caddy (:443, auto-TLS) ──http──► aeman:8765
```

## 1. Register a GitHub OAuth App

GitHub → Settings → Developer settings → **OAuth Apps** → **New OAuth App**:

- **Homepage URL:** `https://aeman.example.com`
- **Authorization callback URL:** `https://aeman.example.com/auth/callback`

Generate a client secret. Keep the **Client ID** and **Client secret**.

## 2. DNS and firewall

- Add a DNS **A record**: `aeman.example.com` → your server IP (DNS-only / not proxied, so Caddy can solve the ACME challenge).
- Open inbound TCP **80** and **443** on the server.

## 3. Configure and run

```sh
git clone https://github.com/aenix-io/aeman.git
cd aeman
cp .env.example .env
# fill AEMAN_GITHUB_CLIENT_ID / _SECRET, AEMAN_BASE_URL, AEMAN_DOMAIN
docker compose up -d --build
```

Caddy issues the certificate on first start (needs DNS + ports 80/443 already in place). Open `https://aeman.example.com`, click **Sign in with GitHub**, and you're in.

## Lock to one board (optional)

To pin every visitor to a single project and hide the board picker, set in `.env`:

```sh
AEMAN_OWNER=acme
AEMAN_PROJECT=37
AEMAN_LOCK_BOARD=true
```

A user whose token can't read that board sees an access-denied screen rather than the board.

## Notes

- **Sessions & the session store.** `AEMAN_SESSION_FILE` (on the `aeman_sessions` volume) always persists the dynamic MCP client registry. Sessions — the GitHub tokens — are written there only when `AEMAN_SESSION_KEY` is set, encrypted with it (AES-256-GCM); then restarts and redeploys keep users signed in and MCP tokens live. Without the key, sessions stay in memory and a restart signs everyone out — no plaintext token ever touches disk either way. Keep the key stable (changing or losing it just signs everyone out) and store it outside the session volume so a leak of that volume alone exposes nothing. Classic OAuth App tokens don't expire, so a session simply lasts up to 14 days.
- **Scopes:** `AEMAN_SCOPES` defaults to `repo project` (Projects v2 + issues).
- **Cert storage:** the `caddy_data` volume persists issued certificates across restarts.
- **Local gh mode is unchanged:** without the `AEMAN_GITHUB_*` env vars the binary still runs as a single-user local tool using `gh auth token`.
- **Behind Cloudflare Tunnel instead of Caddy?** Drop the `caddy` service, add a `cloudflared` service with your tunnel token, and route the public hostname to `http://aeman:8765`; keep `AEMAN_BASE_URL` pointed at the public HTTPS URL.
