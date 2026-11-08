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

/** BacklogKey is a backlog card's place in the list — board.BacklogKey,
 *  mirrored: whether it is a review nobody has taken (K10: those stand first,
 *  as a group of their own, each placed by the task it reviews), whether it
 *  is LOOSE (no plan item above it: after every planned card), and its PATH — the places in the board's order
 *  from the root of its tree down to the card (K14), the root's level ahead
 *  of its place, as the Strategy board draws its roots level by level. */
export interface BacklogKey {
  review: boolean;
  loose: boolean;
  path: number[];
}

const ROOT_LEVEL: Record<string, number> = { goal: 0, initiative: 1, epic: 2 };
const PLANNING = new Set(["goal", "initiative", "epic"]);
/** A place nobody named: after every named one. */
const UNNAMED = Number.MAX_SAFE_INTEGER;

/** backlogKey reads a card's key off the board's order (`pos`, uid → place).
 *  A card or an ancestor the order does not name stands after the named ones;
 *  `fallback` is the card's own place then. */
export function backlogKey(
  c: Pick<Card, "itemId" | "lineage" | "reviewOf" | "assignees" | "stage" | "progress">,
  pos: ReadonlyMap<string, number>,
  fallback = UNNAMED,
): BacklogKey {
  // A review nobody has taken (K10) stands in the group on top, placed by
  // the task it reviews: its lineage is the task's planning chain, and the
  // task's own place ends the path.
  const review = needsReviewer(c);
  const own = review ? (pos.get(c.reviewOf ?? "") ?? pos.get(c.itemId) ?? fallback) : (pos.get(c.itemId) ?? fallback);
  const up = c.lineage ?? [];
  if (!up.some((a) => a.type && PLANNING.has(a.type))) {
    return { review, loose: true, path: [own] };
  }
  const root = ROOT_LEVEL[up[0].type ?? ""] ?? 3;
  return { review, loose: false, path: [root, ...up.map((a) => pos.get(a.uid) ?? UNNAMED), own] };
}

/** compareBacklogKeys orders two keys: negative when a stands first. */
export function compareBacklogKeys(a: BacklogKey, b: BacklogKey): number {
  if (a.review !== b.review) {
    return a.review ? -1 : 1;
  }
  if (a.loose !== b.loose) {
    return a.loose ? 1 : -1;
  }
  for (let i = 0; i < a.path.length && i < b.path.length; i++) {
    if (a.path[i] !== b.path[i]) {
      return a.path[i] - b.path[i];
    }
  }
  return a.path.length - b.path.length;
}

/** backlogList is the board: the backlog cards of the shown teams, MIXED, in
 *  the Strategy board's order (backlogKey) read off `order` — the listing's
 *  `boardOrder`, which every Ordering frame replaces. A card the order does
 *  not name yet (one a watch frame brought in) stands after the named ones,
 *  in the order the board holds the cards; with no order at all (an older
 *  server) that is the whole list. */
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
    .map(({ c, i }) => ({ c, key: backlogKey(c, pos, base + i) }));
  keyed.sort((a, b) => compareBacklogKeys(a.key, b.key));
  return keyed.map(({ c }) => c);
}

/** runOf is which cards a backlog card is a SIBLING of (K14): a review nobody
 *  has taken among the other such reviews; a task among the tasks under the
 *  same parent — the loose tasks, with no plan item above, being one run. */
function runOf(c: Pick<Card, "itemId" | "parent" | "lineage" | "reviewOf" | "assignees" | "stage" | "progress">): string {
  if (needsReviewer(c)) {
    // Its place is its task's (K10): it is no one's sibling, so no drop of
    // it, or by it, is a place.
    return `review:${c.itemId}`;
  }
  const planned = (c.lineage ?? []).some((a) => a.type && PLANNING.has(a.type));
  return planned ? (c.parent ?? "") : "";
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

/** dropPlan reads a drop at place `at` of the list without the card (K5,
 *  K14): ONE order, decided by the tree, so the card moves among its
 *  SIBLINGS only (runOf) — anchored after the sibling just above it, else
 *  before the sibling just below — and a drop with neither neighbour a
 *  sibling is FORBIDDEN. The team does not change by a drop: handing a task
 *  to another team is the owner picker's (ownPlan). A card put back where it
 *  was writes nothing. `list` is the cards as drawn, the card included. */
export function dropPlan(id: string, list: readonly Card[], at: number): BacklogDrop {
  const ids = list.map((c) => c.itemId);
  const order = orderWith(ids, id, at);
  if (order.every((x, i) => x === ids[i])) {
    return { kind: "none" };
  }
  const byId = new Map(list.map((c) => [c.itemId, c]));
  const moved = byId.get(id);
  if (!moved) {
    return { kind: "none" };
  }
  const run = runOf(moved);
  const place = order.indexOf(id);
  const above = place > 0 ? byId.get(order[place - 1]) : undefined;
  const below = place < order.length - 1 ? byId.get(order[place + 1]) : undefined;
  if (above && runOf(above) === run) {
    return { kind: "reorder", order, anchor: { after: above.itemId } };
  }
  if (below && runOf(below) === run) {
    return { kind: "reorder", order, anchor: { before: below.itemId } };
  }
  return { kind: "forbidden" };
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
 *  create — today, the unplanned band, the team's sprint — and it leaves the
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
 *  dates today, the unplanned band, the person, and out of the backlog. */
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
    zone: "yellow",
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
