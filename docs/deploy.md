# Deploying aeman (self-hosted, multi-user)

Runs aeman behind a Cloudflare Tunnel: each visitor signs in with GitHub and acts with **their own** token. No ports are opened on the VM — Cloudflare terminates TLS at the edge and tunnels to the container over an outbound connection.

```
browser ──HTTPS──► Cloudflare edge ──tunnel──► cloudflared ──http──► aeman:8765
```

## 1. Register a GitHub OAuth App

GitHub → Settings → Developer settings → **OAuth Apps** → **New OAuth App**:

- **Homepage URL:** `https://aeman.example.com`
- **Authorization callback URL:** `https://aeman.example.com/auth/callback`

Generate a client secret. Keep the **Client ID** and **Client secret**.

## 2. Create a Cloudflare Tunnel

Cloudflare **Zero Trust → Networks → Tunnels → Create a tunnel** (Cloudflared connector):

- Copy the tunnel **token**.
- Under **Public Hostnames**, add `aeman.example.com` → service **HTTP** → URL `aeman:8765`.

`aeman` is the compose service name; `cloudflared` reaches it over the compose network, so no host ports are needed.

## 3. Configure and run

```sh
cp .env.example .env
# fill AEMAN_GITHUB_CLIENT_ID / _SECRET, AEMAN_BASE_URL, TUNNEL_TOKEN
docker compose up -d --build
```

Open `https://aeman.example.com`, click **Sign in with GitHub**, and you're in. Each user loads the projects their token can see.

## Notes

- **Sessions live in memory** — restarting the container just forces users to sign in again (no token database).
- **Scopes:** `AEMAN_SCOPES` defaults to `repo project`, which covers Projects v2 + issues. Narrow it if you only need read access.
- **Secret rotation:** update `.env` and `docker compose up -d`.
- **Other reverse proxies:** drop the `cloudflared` service and point your Traefik/Nginx/Caddy at the `aeman` container's port `8765`; keep `AEMAN_BASE_URL` set to the public HTTPS URL so the OAuth callback matches.
- **Local gh mode is unchanged:** without the `AEMAN_GITHUB_*` env vars the binary still runs as a single-user local tool using `gh auth token`.
