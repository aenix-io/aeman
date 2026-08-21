// The LIST/watch selectors for a board view, shared by the data fetch and the
// scoped watch so both cover exactly what the active board shows.

import { mondayOf } from "./date";

export type ViewMode = "me" | "team" | "plan";

// viewQueries builds the LIST selectors for a board view — possibly several,
// fetched together and merged. Me is the personal board: the server resolves
// "who am I" unless the user is impersonating someone (viewAs), which is sent
// as an explicit user. Team is the multi-team lead board: the day grid PLUS the
// weekly-plan cards of the shown teams (the plan panel renders from the same
// card set). Grid/me queries ask for reviews=true so each card's linked review
// card rides along for the reviewer badge.
export function viewQueries(
  view: ViewMode,
  day: string,
  teams: string[],
  viewAs?: string,
): Record<string, string>[] {
  if (view === "me") {
    const q: Record<string, string> = { view: "me", day, reviews: "true" };
    if (viewAs) {
      q.user = viewAs;
    }
    return [q];
  }
  if (view === "plan") {
    // Every epic-filed card, all weeks: the Plan board lays the table out
    // itself and the team chips filter client-side (columns are global).
    return [{ view: "plan" }];
  }
  const team = teams.join(",");
  return [
    { view: "team", team, day, reviews: "true" },
    { view: "weekly", team, week: mondayOf(day) },
  ];
}

// watchQuery is the scoped-watch selector for a view. The Team board watches
// every card of the teams it shows (view=all&team=set, no day), so both grid
// and weekly-plan changes stream; the Me board watches its own day selection.
export function watchQuery(
  view: ViewMode,
  day: string,
  teams: string[],
  viewAs?: string,
): Record<string, string> {
  if (view === "plan") {
    return { view: "plan" };
  }
  if (view === "me") {
    const q: Record<string, string> = { view: "me", day, reviews: "true" };
    if (viewAs) {
      q.user = viewAs;
    }
    return q;
  }
  return { view: "all", team: teams.join(",") };
}

// queryString serialises a selector to a URL fragment with a stable key order,
// so it can both drive a fetch and key the watch subscription — the watch
// re-subscribes only when the selection actually changes.
export function queryString(q: Record<string, string>): string {
  return Object.keys(q)
    .sort()
    .map((k) => `${k}=${encodeURIComponent(q[k])}`)
    .join("&");
}
