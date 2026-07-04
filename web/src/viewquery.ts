// The LIST/watch selector for a board view, shared by the data fetch and the
// scoped watch so both scope to exactly what the active board shows.

export type ViewMode = "me" | "team";

// viewQuery builds the selector for a board view. Me is the personal board — the
// server resolves "who am I", so no user is sent. Team is the multi-team lead
// board: it names every team it shows as a comma-separated set. Both ask for
// reviews=true so each card's linked review card rides along for the reviewer
// badge without a second request.
export function viewQuery(
  view: ViewMode,
  day: string,
  teams: string[],
): Record<string, string> {
  const q: Record<string, string> = { view, day, reviews: "true" };
  if (view === "team") {
    q.team = teams.join(",");
  }
  return q;
}

// queryString serialises a selector to a URL fragment with a stable key order,
// so it both drives the fetch and keys the watch subscription — the watch
// re-subscribes only when the selection actually changes.
export function queryString(q: Record<string, string>): string {
  return Object.keys(q)
    .sort()
    .map((k) => `${k}=${encodeURIComponent(q[k])}`)
    .join("&");
}
