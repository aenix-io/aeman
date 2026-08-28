// GET /api/healthz as the UI reads it: whether the server's commits are reaching
// the remote. "degraded" means they have not been pushed for longer than the
// server's threshold — the board still works (every change is committed
// locally), but the other replicas do not see them yet.

/** HealthStatus is the /api/healthz body. */
export interface HealthStatus {
  status: string;
  /** Age of the oldest commit not yet pushed, in seconds. */
  unpushedAgeSeconds?: number;
  aliases?: object[];
  ghosts?: object[];
}

/** unpushedNotice is the banner line for a degraded server, or null while
 *  the server is healthy (or not yet asked). */
export function unpushedNotice(health: HealthStatus | null): string | null {
  if (!health || health.status !== "degraded") {
    return null;
  }
  const base = "Changes are saved locally but have not been pushed";
  if (health.unpushedAgeSeconds === undefined) {
    return base;
  }
  const minutes = Math.max(1, Math.round(health.unpushedAgeSeconds / 60));
  return `${base} for ${minutes} ${minutes === 1 ? "minute" : "minutes"}`;
}
