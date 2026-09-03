// Thin client over the aeman Go server's bootstrap endpoints: /api/config for
// the session and build facts, /api/healthz for the sync state. The board itself
// is read through the /api/v1 provider (providers/api/apiProvider.ts).

import type { HealthStatus } from "../health";

/** clientId identifies this browser tab to the server: REST mutations carry it
 * as X-Aeman-Client and the watch subscription passes it as ?client=, so the
 * server does not echo a tab's own changes back over its watch stream. */
export const clientId: string =
  typeof crypto !== "undefined" && "randomUUID" in crypto
    ? crypto.randomUUID()
    : `c-${Math.random().toString(36).slice(2)}`;

export interface AppConfig {
  mode: string;
  version: string;
  login?: string;
  tokenAvailable: boolean;
  /** Whether the visitor is signed in (always true in local gh-token mode). */
  authenticated: boolean;
  /** OAuth mode: URL to start the forge sign-in flow. */
  authUrl?: string;
  /** OAuth mode: URL to sign out. */
  logoutUrl?: string;
  /** The board's day time zone (IANA name): "today" is computed in it so
   *  every user sees the same board day. "Local" = server-local, unset zone. */
  tz?: string;
  /** Fingerprint of the frontend bundle the server carries. A tab whose own
   *  build differs is running code that has since been replaced. */
  build?: string;
  /** The forge the board lives on and signs in with. An older server sends
   *  none of these four: that is GitHub (see forge.ts). */
  forge?: "github" | "gitlab";
  /** Human name of the forge: "GitHub" | "GitLab". */
  forgeLabel?: string;
  /** The forge's own CLI, "gh" | "glab" — one of the sources a local run reads its token from, after the environment and the OS keychain. */
  cli?: string;
  /** The forge's host: "github.com", "gitlab.com", or a self-hosted one. */
  forgeHost?: string;
}

export async function fetchConfig(): Promise<AppConfig> {
  const res = await fetch("/api/config");
  if (!res.ok) {
    throw new Error(`config request failed: HTTP ${res.status}`);
  }
  return (await res.json()) as AppConfig;
}

/** fetchHealth reads /api/healthz: "ok", or "degraded" when the server's commits
 * have not been pushed for longer than its threshold. A degraded server still
 * answers with a body, so a non-2xx status is not an error here. */
export async function fetchHealth(): Promise<HealthStatus> {
  const res = await fetch("/api/healthz");
  return (await res.json()) as HealthStatus;
}
