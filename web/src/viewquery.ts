// The LIST/watch selectors for a board view, shared by the data fetch and the
// scoped watch so both cover exactly what the active board shows.

import { todayIso } from "./date";
import type { RoadmapFilters } from "./ownership";

export type ViewMode = "me" | "team" | "backlog" | "roadmap" | "project";

/** ViewOptions are the board's own toggles that the SERVER answers: the
 *  Roadmap's "Project cards" (projectCards). They ride the listing and the
 *  watch alike, so turning one changes the keys the board refetches and
 *  re-subscribes on — nothing is filtered out of what was already sent. */
export interface ViewOptions {
  projectCards?: boolean;
  /** The Roadmap's team, customer and owner filters (ownership.md, O7,
   *  O8): selectors the server applies, each a comma-separated set. */
  filters?: RoadmapFilters;
}

function roadmapQuery(opts: ViewOptions): Record<string, string> {
  const q: Record<string, string> = opts.projectCards ? { view: "roadmap", projectCards: "true" } : { view: "roadmap" };
  const f = opts.filters;
  if (f?.teams.length) {
    q.team = f.teams.join(",");
  }
  if (f?.customers.length) {
    q.customer = f.customers.join(",");
  }
  if (f?.owners.length) {
    q.owner = f.owners.join(",");
  }
  return q;
}

// viewQueries builds the LIST selectors for a board view — possibly several,
// fetched together and merged. Me is the reader's own day: the server resolves
// "who am I" unless the user is impersonating someone (viewAs), which is sent
// as an explicit user. Team is the multi-team lead board's day grid. Grid/me
// queries ask for reviews=true so each card's linked review card rides along
// for the reviewer badge.
export function viewQueries(
  view: ViewMode,
  day: string,
  teams: string[],
  viewAs?: string,
  today = todayIso(),
  opts: ViewOptions = {},
): Record<string, string>[] {
  // A day already past is asked for AS IT WAS: the server answers from the
  // board's history rather than filtering today's cards by that day's dates
  // (see snapshotDay). Tomorrow is not a snapshot — nothing has happened
  // there yet — and neither is today, which is still happening.
  const snap: Record<string, string> = snapshotDay(view, day, today)
    ? { snapshot: "1" }
    : {};
  if (view === "me") {
    const q: Record<string, string> = { view: "me", day, reviews: "true", ...snap };
    if (viewAs) {
      q.user = viewAs;
    }
    return [q];
  }
  if (view === "project") {
    // Every scope-filed card of every project, all weeks: the Project board
    // lays the table out itself and the project chips filter client-side, so
    // switching projects costs no request.
    return [{ view: "project" }];
  }
  if (view === "roadmap") {
    // The hierarchy: every planning card and every card under one, board-
    // wide. The tree is assembled client-side from `parent`; never a
    // snapshot — it is not a day board (docs/design/hierarchy.md). The
    // project cards are the server's to leave out unless asked for.
    return [roadmapQuery(opts)];
  }
  const team = teams.join(",");
  if (view === "backlog") {
    // Each shown team's parked open tasks, in the server's BacklogOrder
    // (K2, docs/design/backlog.md). Not a day board: no day, never a
    // snapshot. The no-team group is the empty member of the set.
    return [{ view: "backlog", team }];
  }
  return [{ view: "team", team, day, reviews: "true", ...snap }];
}

// snapshotDay reports that a day is one the board can be shown AS IT WAS: a
// past day on a daily board (Me or Team). Today is live, tomorrow has not
// happened, and the Project board is not a day board at all.
// Mirrors the server's own condition (boardOfRequest, internal/server/api.go).
export function snapshotDay(view: ViewMode, day: string, today = todayIso()): boolean {
  return (view === "me" || view === "team") && day !== "" && day < today;
}

// watchQuery is the scoped-watch selector for a view. The Team board watches
// every card of the teams it shows (view=all&team=set, no day), so a change
// streams whatever day it lands on; the Backlog board watches the same, so a
// card taken into work, re-teamed or finished — one that LEAVES a backlog —
// is heard too (K6); the Me board watches its own day.
export function watchQuery(
  view: ViewMode,
  day: string,
  teams: string[],
  viewAs?: string,
  opts: ViewOptions = {},
): Record<string, string> {
  if (view === "project") {
    return { view: "project" };
  }
  if (view === "roadmap") {
    return roadmapQuery(opts);
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
// socket each.
export function watchQueries(
  view: ViewMode,
  day: string,
  teams: string[],
  viewAs?: string,
  opts: ViewOptions = {},
): Record<string, string>[] {
  return [watchQuery(view, day, teams, viewAs, opts)];
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

/** viewPath is where a selector is fetched or watched: the BOARD it names is a
 *  path segment (`/views/{view}/cards`) and everything that narrows it stays a
 *  query. It takes the serialised selector, so the fetch and the watch — which
 *  both key off queryString — address the same board the same way.
 *
 *  Mirrors the server's routes (docs/design/view-scoped-api.md). A selector
 *  that names no board is the escape hatch: nothing here sends one, and
 *  answering "all" is what the bare collection used to do minus the guessing.
 */
export function viewPath(query: string, kind: "cards" | "watch"): string {
  const rest = query
    .split("&")
    .filter((part) => part && !part.startsWith("view="));
  return `/views/${viewOf(query)}/${kind}${rest.length > 0 ? `?${rest.join("&")}` : ""}`;
}

/** viewOf is the BOARD a serialised selector names — the segment viewPath
 *  puts in the path, and the answer to "which board is open" for everything
 *  else that has to know (a create belongs to the board it was typed into). */
export function viewOf(query: string): string {
  for (const part of query.split("&")) {
    if (part.startsWith("view=")) {
      return decodeURIComponent(part.slice("view=".length));
    }
  }
  return "all";
}

/** storedView is the board a remembered tab name opens. A name a board used
 *  to have still lands on the board that carries it now: "plan" was the
 *  Project tab, "nixon" the Team tab — and "triage" was retired for the
 *  Backlog (docs/design/backlog.md, K9), so a browser that remembered it
 *  lands on the board that replaced it rather than silently on Me. */
export function storedView(raw: string | null): ViewMode {
  switch (raw) {
    case "team":
    case "nixon":
      return "team";
    case "project":
    case "plan":
      return "project";
    case "backlog":
    case "triage":
      return "backlog";
    case "roadmap":
      return "roadmap";
    default:
      return "me";
  }
}
