// What the Backlog board draws and what its gestures mean — the board's
// decisions, apart from the component that draws them (docs/design/backlog.md,
// rules K1–K9).
//
// The board is ONE list: the PARKED OPEN TASKS (K2) of every selected team,
// the no-team group included, MIXED in one order — the order the server keeps
// them (board.BacklogOrder: by hand first, then oldest first; rank is one
// board-wide list, so the merged order is the board order of those cards).
// One wide card per row, drawn as the Me and Team boards draw a card — a
// review nobody has taken as a review card, in the same list, ranked at the
// top when it is made — with its team's avatar and, on the right, the
// planning cards it sits under. Dragging reorders by the neighbours in the
// list (K5); the owner picker with a team alone hands the task to that team,
// still parked, and with a person takes it into work (K4), and it leaves the
// backlog at once.
//
// The listing is the backlog view; the WATCH is every card of the shown teams
// (viewquery.watchQuery), so a card parked, taken into work, re-teamed or
// finished by anybody lands in the board's cards and the list is simply
// recomputed from them (K6). Nothing here keeps state of its own.
//
// Mirrors pkg/board/backlog.go (OnBacklog) and the doors boardservice opens
// on a parked card (Own, Move).
import { gapAnchor } from "./insertgap";
import { isPlanning, isSubtask } from "./subtasks";
import { isComplete } from "./stages";
import type { Owner } from "./owner";
import { placedTeamOf, placerOf } from "./ownership";
import type { Card, LineageRef, Provider } from "./providers/types";

/** onBacklog reports whether a card is listed in its team's backlog (K2): a
 *  PARKED task that answers for its own place — not a planning card (the
 *  Strategy's), not a review card (it follows the card it reviews), not a subtask (it rides its parent) —
 *  and not finished: the backlog never shows work that is over. Mirrors
 *  board.OnBacklog.
 *
 *  The FLAG decides, as board.InBacklog reads it: a task with no date and no
 *  sprint that is not parked is in no backlog (schema 3 parks such a card),
 *  and a parked card a direct writer left dates on is in one — drawn once,
 *  where it can be dealt with. */
export function onBacklog(
  c: Pick<Card, "parked" | "type" | "reviewOf" | "parent" | "lineage" | "stage" | "progress" | "assignees">,
): boolean {
  if (!c.parked || isComplete(c)) {
    return false;
  }
  if (needsReviewer(c)) {
    return true;
  }
  return !isPlanning(c) && !c.reviewOf && !isSubtask(c);
}

/** needsReviewer mirrors board.NeedsReviewer (K10): an open review card
 *  nobody has taken — what putting a task on review without naming a
 *  reviewer makes, parked in the task's team's backlog. */
export function needsReviewer(c: Pick<Card, "reviewOf" | "assignees" | "stage" | "progress">): boolean {
  return !!c.reviewOf && (c.assignees ?? []).length === 0 && !isComplete(c);
}

/** shownTeams is the teams whose backlogs the board shows: the chips'
 *  selection, or — with none — every team and the no-team group, the same
 *  set the board's listing asks for (App: viewQueries). */
export function shownTeams(filter: readonly string[] | null, roster: readonly string[]): string[] {
  return filter ? [...filter] : [...new Set([...roster, ""])];
}

/** backlogList is the board: the backlog cards of the shown teams, MIXED, in
 *  the board's order (Q2, board.BacklogOrder) read off `order` — the
 *  listing's `boardOrder`, which every Ordering frame replaces: the reviews
 *  nobody has taken first (K10), then every task by its own place. A task's
 *  rank IS its queue place, so the plan items above it decide nothing. A
 *  card the order does not name yet (one a watch frame brought in) stands
 *  after the named ones, in the order the board holds the cards; with no
 *  order at all (an older server) that is the whole list. */
export function backlogList(cards: readonly Card[], teams: readonly string[], order?: readonly string[]): Card[] {
  const shown = new Set(teams);
  // A backlog holds the tasks STANDING UNDER its team (O13): its own, else
  // the plan item's above it.
  const place = placerOf(cards);
  const pos = new Map((order ?? []).map((uid, i) => [uid, i]));
  const base = pos.size;
  const keyed = cards
    .map((c, i) => ({ c, i }))
    .filter(({ c }) => onBacklog(c) && shown.has(place(c)))
    .map(({ c, i }) => ({ c, review: needsReviewer(c), at: pos.get(c.itemId) ?? base + i }));
  keyed.sort((a, b) => (a.review !== b.review ? (a.review ? -1 : 1) : a.at - b.at));
  return keyed.map(({ c }) => c);
}

/** takeOwner is what TAKE leaves on the card, the server's `take` (K11)
 *  mirrored for the optimistic card: the VIEWER as the person, on the card's
 *  own team — taken into work (ownPlan's "take", K4); on a review nobody has
 *  taken the viewer becomes its reviewer (K10). A refusal from the server
 *  (work already in hand) puts the card back and is shown. */
export function takeOwner(c: Pick<Card, "team">, viewer: string): Owner {
  return { team: c.team ?? "", person: viewer };
}

/** reviewOwnPlan is what the picker does on a review nobody has taken: a
 *  PERSON becomes its reviewer and takes it into work (the server's `own` on
 *  the review card, K10); any other answer is the review link's to give, and
 *  the board offers nothing for it. */
export function reviewOwnPlan(owner: Owner): "take" | "none" {
  return owner.person ? "take" : "none";
}

/** orderWith is the list's order once `id` has been dropped at place `at`.
 *
 *  The card is taken OUT before it is put back in, so a card dragged down
 *  past its own neighbours lands where the pointer is rather than one short
 *  of it — the preview and the write have to agree about that, and both
 *  count places in the list the reader is looking at. */
export function orderWith(ids: readonly string[], id: string, at: number): string[] {
  const rest = ids.filter((other) => other !== id);
  const place = Math.max(0, Math.min(at, rest.length));
  return [...rest.slice(0, place), id, ...rest.slice(place)];
}

/** How a card's place in the list is written down. The board's order is one
 *  list and the backlog is a slice of it, so the write names a NEIGHBOUR in
 *  the merged list — of whichever team — and the server resolves the rank
 *  from it; never a position (K5).
 *
 *  A card at the top has nothing before it to sit after, so it is written as
 *  standing before the card now second; a card alone in the list has no
 *  neighbour at all and nothing to say. */
export type Anchor = { after: string } | { before: string } | null;

export function anchorFor(ids: readonly string[], id: string): Anchor {
  const place = ids.indexOf(id);
  if (place < 0 || ids.length < 2) {
    return null;
  }
  return place > 0 ? { after: ids[place - 1] } : { before: ids[1] };
}

/** spliceOrder is the board-wide order once the list has been re-ordered:
 *  the list's cards take the slots they already held, in the list's new
 *  order, and every other card stays where it was. It is what the board keeps locally until the server's
 *  Ordering frame says the same. */
export function spliceOrder(all: readonly string[], column: readonly string[]): string[] {
  const inColumn = new Set(column);
  let i = 0;
  return all.map((id) => (inColumn.has(id) ? column[i++] : id));
}

/** BacklogDrop is what a drop means. */
export type BacklogDrop = { kind: "none" } | { kind: "forbidden" } | { kind: "reorder"; order: string[]; anchor: Anchor };

/** dropPlan reads a drop at place `at` of the list without the card (Q4,
 *  K5): a task goes between ANY two neighbours, of whichever epic or team —
 *  it becomes an exception to its epic's order, and the epic's key does not
 *  change. It is anchored after the TASK above it, else before the task
 *  below it; a review nobody has taken is no anchor (its group is pinned on
 *  top, K10), so a drop among the reviews lands before the first task. A
 *  review itself is never dragged: its drop is forbidden. The team does not
 *  change by a drop: handing a task to another team is the owner picker's
 *  (ownPlan). A card put back where it was writes nothing. `list` is the
 *  cards as drawn, the card included. */
export function dropPlan(id: string, list: readonly Card[], at: number): BacklogDrop {
  const ids = list.map((c) => c.itemId);
  const byId = new Map(list.map((c) => [c.itemId, c]));
  const moved = byId.get(id);
  if (!moved) {
    return { kind: "none" };
  }
  if (needsReviewer(moved)) {
    return { kind: "forbidden" };
  }
  const order = orderWith(ids, id, at);
  const tasks = order.filter((x) => x === id || !needsReviewer(byId.get(x)!));
  const reviews = order.filter((x) => x !== id && needsReviewer(byId.get(x)!));
  const shown = [...reviews, ...tasks];
  if (shown.every((x, i) => x === ids[i])) {
    return { kind: "none" };
  }
  const place = tasks.indexOf(id);
  if (place > 0) {
    return { kind: "reorder", order: shown, anchor: { after: tasks[place - 1] } };
  }
  if (tasks.length > 1) {
    return { kind: "reorder", order: shown, anchor: { before: tasks[1] } };
  }
  return { kind: "none" };
}

/** AddBox is what an insert "+" offers once opened: with one team shown,
 *  that team and no selector (forcedTeam); with several, a selector of them
 *  — the no-team group among them when it is shown — starting on
 *  initialTeam. AddCard's own props. */
export type AddBox =
  | { forcedTeam: string | null }
  | { teams: string[]; allowNoTeam: boolean; initialTeam: string | null };

/** InsertSpot is what the "+" in one gap of the list does: the place it
 *  names in the create (after the card above, or before the card below at
 *  the top; nothing in an empty backlog, where the card is simply the
 *  first), and the team box it opens with — starting on the team of the
 *  card above when that team is shown, else the first shown team. */
export interface InsertSpot {
  anchor: Anchor;
  box: AddBox;
  /** The epic the new task is filed under (Q10): inheritedEpic of the gap.
   *  The form shows it as a chip with an ×, and the create names it as the
   *  parent unless the × took it off. */
  epic: LineageRef | null;
}

/** epicOf is a card's epic as its lineage carries it: the nearest epic
 *  above it, or none — a NEUTRAL task. Mirrors board.Index.EpicOf. */
export function epicOf(c: Pick<Card, "lineage">): LineageRef | null {
  const up = c.lineage ?? [];
  for (let i = up.length - 1; i >= 0; i--) {
    if (up[i].type === "epic") {
      return up[i];
    }
  }
  return null;
}

/** inheritedEpic is the epic a task typed into gap `at` of the list as
 *  drawn inherits (Q10, board.InheritedEpic): the card above's epic — the
 *  same as below's when the two agree, the card above's at a boundary
 *  (which may be none) — or, at the top, the card below's; none in an empty
 *  list. A review nobody has taken is no neighbour: it is skipped. */
export function inheritedEpic(list: readonly Card[], at: number): LineageRef | null {
  for (let i = Math.min(at, list.length) - 1; i >= 0; i--) {
    if (!needsReviewer(list[i])) {
      return epicOf(list[i]);
    }
  }
  for (let i = Math.max(at, 0); i < list.length; i++) {
    if (!needsReviewer(list[i])) {
      return epicOf(list[i]);
    }
  }
  return null;
}

/** insertSpot reads the gap `at` of the list as drawn: 0 is above the
 *  first card, list.length below the last. */
export function insertSpot(list: readonly Card[], at: number, teams: readonly string[]): InsertSpot {
  const above = at > 0 ? list[at - 1] : undefined;
  const anchor = gapAnchor(
    list.map((c) => c.itemId),
    at,
  );
  const epic = inheritedEpic(list, at);
  if (teams.length === 1) {
    return { anchor, box: { forcedTeam: teams[0] || null }, epic };
  }
  // The team the card above STANDS UNDER (O13); the new card is made with
  // it as its own.
  const aboveTeam = above ? placedTeamOf(above, list) : "";
  const start = above && teams.includes(aboveTeam) ? aboveTeam : (teams[0] ?? "");
  return {
    anchor,
    box: { teams: teams.filter((t) => t !== ""), allowNoTeam: teams.includes(""), initialTeam: start || null },
    epic,
  };
}

/** insertedOrder is the board-wide order with a card just made at an insert
 *  "+": right after the card above it, right before the card below it at
 *  the top, last with no place — what the board shows until the server's
 *  Ordering frame says the same. */
export function insertedOrder(all: readonly string[], id: string, anchor: Anchor): string[] {
  const rest = all.filter((x) => x !== id);
  if (!anchor) {
    return [...rest, id];
  }
  const at = "after" in anchor ? rest.indexOf(anchor.after) + 1 : rest.indexOf(anchor.before);
  if (at < 0 || ("after" in anchor && at === 0)) {
    return [...rest, id];
  }
  return [...rest.slice(0, at), id, ...rest.slice(at)];
}

/** ownPlan is what the owner picker does on a backlog card. A PERSON takes
 *  the task into work (K4): the server files it as the Me board files a
 *  create — today, the planned band, the team's sprint — and it leaves the
 *  backlog at once. A team alone, or nobody, hands it to that team's
 *  backlog (the no-team group's for nobody), still parked (K3, K5). */
export function ownPlan(c: Pick<Card, "team">, owner: Owner): "take" | "hand-over" | "none" {
  if (owner.person) {
    return "take";
  }
  return owner.team === (c.team ?? "") ? "none" : "hand-over";
}

/** takenIntoWorkLocally is the card the moment a person takes it, before the
 *  server answers — what boardservice.ownByPerson writes on an unplanned
 *  card: the team (joining its current sprint, or starting one today), the
 *  dates today, the planned band, the person, and out of the backlog. */
export function takenIntoWorkLocally(
  owner: Owner,
  today: string,
  teamSprint: string | null,
): Partial<Card> {
  return {
    team: owner.team || undefined,
    assignees: [owner.person],
    parked: false,
    startDate: today,
    day: today,
    zone: "gray",
    sprintStart: teamSprint ?? today,
  };
}

/** handedOverLocally is a task handed to another team's backlog: the team,
 *  nobody holding it, and still parked (K5) — `own` with a team alone drops
 *  the person, as it does on any card. */
export function handedOverLocally(team: string): Partial<Card> {
  return { team: team || undefined, assignees: [], parked: true };
}

/** parentChain is the card's PARENT CHAIN, drawn on its right: every
 *  planning card above it — goal, initiative, epic — root first, as the
 *  lineage carries them. A task ancestor (a subtask's) is not part of it,
 *  and neither is the task a REVIEW card reviews: the chain is the plan,
 *  and the drawer's breadcrumb, which ends in the task, is the way to it. */
export function parentChain(c: Pick<Card, "lineage">): LineageRef[] {
  return (c.lineage ?? []).filter((r) => !!r.type);
}

/** openPlanning opens a card's drawer from its name in a chain — a planning
 *  card, or the task a review card reviews: the backlog may not hold it, so
 *  it is fetched by uid and handed to the drawer. */
export async function openPlanning(
  provider: Pick<Provider, "getCard">,
  uid: string,
  onOpen: (card: Card) => void,
  onError: (message: string) => void,
): Promise<void> {
  try {
    onOpen(await provider.getCard(uid));
  } catch (err) {
    onError(err instanceof Error ? err.message : String(err));
  }
}

/** nearestPlanning is the card's due-date source: the nearest planning card above it —
 *  the last lineage entry that is a goal, an initiative or an epic — or
 *  none. */
export function nearestPlanning(c: Pick<Card, "lineage">): LineageRef | null {
  const up = c.lineage ?? [];
  for (let i = up.length - 1; i >= 0; i--) {
    if (up[i].type) {
      return up[i];
    }
  }
  return null;
}

/** dueOf is the task's due date, as the SERVER says it (status.dueDate): its
 *  nearest planning card's end, never one borrowed from a farther ancestor.
 *  The rule is the server's; the board only reads it. */
export function dueOf(c: Pick<Card, "dueDate">): string | undefined {
  return c.dueDate || undefined;
}

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

/** dueLabel is how a due date reads on a card: "18 Dec". */
export function dueLabel(iso: string): string {
  const [, m, d] = iso.split("-").map(Number);
  return `${d} ${MONTHS[m - 1]}`;
}

/** Readiness is the Backlog's All / Ready / Blocked filter (ordering.md):
 *  a dependency never changes the order — a blocked task stays where it
 *  stands — so the filter only hides. Ready is every card that waits for
 *  nothing (status.locked false); Blocked the ones that do. All, the
 *  default, hides nothing. */
export type Readiness = "all" | "ready" | "blocked";

export function byReadiness<T extends Pick<Card, "locked">>(list: readonly T[], mode: Readiness): T[] {
  if (mode === "all") {
    return [...list];
  }
  return list.filter((c) => (mode === "blocked" ? !!c.locked : !c.locked));
}

/** splitEpics is the epics whose tasks do not stand together in a team's
 *  queue (board.Index.Split): another task stands inside the epic's area.
 *  `list` is the backlog in its order, any mix of teams; each team's queue
 *  is read apart, and the reviews nobody has taken are no part of one. */
export function splitEpics(list: readonly Card[], teamOf: (c: Card) => string): Set<string> {
  const runs = new Map<string, Map<string, number>>(); // team → epic → runs
  const last = new Map<string, string | null>(); // team → epic of the last task
  for (const c of list) {
    if (needsReviewer(c)) {
      continue;
    }
    const team = teamOf(c);
    const epic = epicOf(c)?.uid ?? null;
    if (epic && last.get(team) !== epic) {
      const byEpic = runs.get(team) ?? new Map<string, number>();
      byEpic.set(epic, (byEpic.get(epic) ?? 0) + 1);
      runs.set(team, byEpic);
    }
    last.set(team, epic);
  }
  const out = new Set<string>();
  for (const byEpic of runs.values()) {
    for (const [epic, n] of byEpic) {
      if (n > 1) {
        out.add(epic);
      }
    }
  }
  return out;
}

/** BacklogMode is the Backlog's Tasks | Epics switch; switching writes
 *  nothing — both read the same order. Remembered per viewer. */
export type BacklogMode = "tasks" | "epics";

export const BACKLOG_MODE_KEY = "aeman.backlog.mode";

export function readBacklogMode(raw: string | null): BacklogMode {
  return raw === "epics" ? "epics" : "tasks";
}

/** EpicRow is one line of the Epics mode: the epic, the goal and initiative
 *  above it, how many of its tasks wait in the shown backlogs, and whether
 *  they are scattered (Split). */
export interface EpicRow {
  epic: Card;
  chain: LineageRef[];
  parked: number;
  split: boolean;
}

/** epicRows is the Epics mode (ordering.md, "Backlog Epics mode"): the epics
 *  in the shown teams' scope — of a shown team, or with an open task
 *  standing under one; an epic of no team with the no-team group shown —
 *  empty ones included, done ones not, in the EPIC ORDER (the board's order,
 *  `order`). `scope` is the cards the epics and their tasks are read from
 *  (the Strategy listing), `list` the backlog as drawn. */
export function epicRows(
  scope: readonly Card[],
  list: readonly Card[],
  teams: readonly string[],
  order?: readonly string[],
): EpicRow[] {
  const shown = new Set(teams);
  const teamOf = placerOf(scope);
  const listTeamOf = placerOf(list);
  const inScope = new Set<string>();
  for (const c of scope) {
    if (c.type === "epic" || isPlanning(c) || isComplete(c)) {
      continue;
    }
    const e = epicOf(c);
    if (e && shown.has(teamOf(c))) {
      inScope.add(e.uid);
    }
  }
  const pos = new Map((order ?? []).map((uid, i) => [uid, i]));
  const at = (c: Card, i: number) => pos.get(c.itemId) ?? pos.size + i;
  const parked = new Map<string, number>();
  for (const c of list) {
    const e = epicOf(c);
    if (e && !needsReviewer(c)) {
      parked.set(e.uid, (parked.get(e.uid) ?? 0) + 1);
    }
  }
  const split = splitEpics(list, listTeamOf);
  return scope
    .map((c, i) => ({ c, i }))
    .filter(({ c }) => c.type === "epic" && c.hierarchy?.status !== "done")
    .filter(({ c }) => shown.has(c.team ?? "") || inScope.has(c.itemId))
    .sort((a, b) => at(a.c, a.i) - at(b.c, b.i))
    .map(({ c }) => ({
      epic: c,
      chain: (c.lineage ?? []).filter((r) => r.type === "goal" || r.type === "initiative"),
      parked: parked.get(c.itemId) ?? 0,
      split: split.has(c.itemId),
    }));
}

/** epicDrop is where an epic dropped at place `at` of the Epics list (the
 *  list without it) goes: right after the epic above it, or right before the
 *  first one at the top — the epic move, which gathers its tasks. Nothing
 *  for a drop where it already was. */
export function epicDrop(ids: readonly string[], id: string, at: number): { order: string[]; anchor: Anchor } | null {
  const order = orderWith(ids, id, at);
  if (order.every((x, i) => x === ids[i])) {
    return null;
  }
  return { order, anchor: anchorFor(order, id) };
}

/** epicsByReadiness is the All / Ready / Blocked filter on the Epics mode:
 *  Blocked is an epic locked by a dependency, its own or inherited
 *  (status.locked), Ready one that is not — the tasks' rule. It only hides. */
export function epicsByReadiness(rows: readonly EpicRow[], mode: Readiness): EpicRow[] {
  const keep = new Set(byReadiness(rows.map((r) => r.epic), mode).map((c) => c.itemId));
  return rows.filter((r) => keep.has(r.epic.itemId));
}
