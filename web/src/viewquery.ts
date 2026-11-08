// The LIST/watch selectors for a board view, shared by the data fetch and the
// scoped watch so both cover exactly what the active board shows.

import { todayIso } from "./date";
import type { RoadmapFilters } from "./ownership";
import { statusSet } from "./workstatus";

export type ViewMode = "me" | "team" | "backlog" | "roadmap";

/** ViewOptions are the board's own filters that the SERVER answers. They
 *  ride the listing and the watch alike, so changing one changes the keys
 *  the board refetches and re-subscribes on — nothing is filtered out of
 *  what was already sent. */
export interface ViewOptions {
  /** The Roadmap's team, customer, owner and status filters (ownership.md,
   *  O7, O8): selectors the server applies, each a comma-separated set. */
  filters?: RoadmapFilters;
  /** The Team board's and the Backlog's Customer filter (O11): customer
   *  ids, matched by the server against effective customers. The Me board
   *  filters what it holds itself — it counts what its filters hide. */
  customers?: string[];
}

/** teamSet writes a team selection as the selector's set. The no-team group
 *  is its empty member, so the group ALONE is "," — an empty `team=` would
 *  ask for every team (O11). */
export function teamSet(teams: readonly string[]): string {
  const s = teams.join(",");
  return teams.length > 0 && s === "" ? "," : s;
}

function withCustomers(q: Record<string, string>, opts: ViewOptions): Record<string, string> {
  return opts.customers?.length ? { ...q, customer: opts.customers.join(",") } : q;
}

function roadmapQuery(opts: ViewOptions): Record<string, string> {
  const q: Record<string, string> = { view: wireView("roadmap") };
  const f = opts.filters;
  if (f?.teams.length) {
    q.team = teamSet(f.teams);
  }
  if (f?.customers.length) {
    q.customer = f.customers.join(",");
  }
  if (f?.owners.length) {
    q.owner = f.owners.join(",");
  }
  const status = statusSet(f?.statuses ?? []);
  if (status) {
    q.status = status;
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
  if (view === "roadmap") {
    // The hierarchy: every planning card and every card under one, board-
    // wide. The tree is assembled client-side from `parent`; never a
    // snapshot — it is not a day board (docs/design/hierarchy.md).
    return [roadmapQuery(opts)];
  }
  const team = teamSet(teams);
  if (view === "backlog") {
    // Each shown team's parked open tasks, in the server's BacklogOrder
    // (K2, docs/design/backlog.md). Not a day board: no day, never a
    // snapshot. The no-team group is the empty member of the set.
    return [withCustomers({ view: "backlog", team }, opts)];
  }
  return [withCustomers({ view: "team", team, day, reviews: "true", ...snap }, opts)];
}

// snapshotDay reports that a day is one the board can be shown AS IT WAS: a
// past day on a daily board (Me or Team). Today is live, tomorrow has not
// happened, and the Backlog and the Roadmap are not day boards at all.
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
  return withCustomers({ view: "all", team: teamSet(teams) }, opts);
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

/** wireView is a board's name as the API and the address know it: the
 *  board the code calls the roadmap is the Strategy board everywhere a
 *  reader or a request sees it ("roadmap" is the server's deprecated alias). */
export function wireView(view: string): string {
  return view === "roadmap" ? "strategy" : view;
}

/** storedView is the board a remembered tab name opens. The Strategy board
 *  was the Roadmap: both names open it. A name a board used
 *  to have still lands on the board that carries it now: "nixon" was the
 *  Team tab, "triage" was retired for the Backlog (docs/design/backlog.md,
 *  K9), and the Project board ("project", once "plan") was removed with
 *  project cards (H15) — the plan lives on the Roadmap — so a browser that
 *  remembered one lands on the board that carries its work rather than
 *  silently on Me. */
export function storedView(raw: string | null): ViewMode {
  switch (raw) {
    case "team":
    case "nixon":
      return "team";
    case "backlog":
    case "triage":
      return "backlog";
    case "strategy":
    case "roadmap":
    case "project":
    case "plan":
      return "roadmap";
    default:
      return "me";
  }
}
