// The LIST/watch selectors for a board view, shared by the data fetch and the
// scoped watch so both cover exactly what the active board shows.

import { mondayOf } from "./date";

export type ViewMode = "me" | "team" | "project" | "process";

// viewQueries builds the LIST selectors for a board view — possibly several,
// fetched together and merged. Me is the personal board: the server resolves
// "who am I" unless the user is impersonating someone (viewAs), which is sent
// as an explicit user. Team is the multi-team lead board: the day grid PLUS the
// weekly-plan cards of the shown teams (the plan panel renders from the same
// card set). Grid/me queries ask for reviews=true so each card's linked review
// card rides along for the reviewer badge. With a personal board linked, Me
// also loads it (view=personal) — the viewer's own, so not while impersonating
// someone else.
export function viewQueries(
  view: ViewMode,
  day: string,
  teams: string[],
  viewAs?: string,
  personal = false,
): Record<string, string>[] {
  if (view === "me") {
    const q: Record<string, string> = { view: "me", day, reviews: "true" };
    if (viewAs) {
      q.user = viewAs;
    }
    // The personal column follows the day being looked at, like the day
    // board beside it: flipped to tomorrow, it shows tomorrow's plan.
    return personal && !viewAs ? [q, { view: "personal", day }] : [q];
  }
  if (view === "project" || view === "process") {
    // Every epic-filed card of every project, all weeks: the Project board
    // lays the table out itself and the project chips filter client-side, so
    // switching projects costs no request. The Process tab reads its own
    // endpoint for the processes and needs no cards, but keeps the same
    // selection so flipping between the two tabs costs nothing either.
    return [{ view: "project" }];
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
  if (view === "project" || view === "process") {
    return { view: "project" };
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

// watchQueries lists the selectors the active view keeps a watch on — one
// socket each: the view's own (watchQuery) and, on the Me board with a
// personal board linked and nobody impersonated, the personal selection.
export function watchQueries(
  view: ViewMode,
  day: string,
  teams: string[],
  viewAs?: string,
  personal = false,
): Record<string, string>[] {
  const base = watchQuery(view, day, teams, viewAs);
  return view === "me" && personal && !viewAs ? [base, { view: "personal", day }] : [base];
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
