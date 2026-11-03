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
import { isPlanning, isSubtask } from "./subtasks";
import { isComplete } from "./stages";
import type { Owner } from "./owner";
import type { Card, LineageRef, Provider } from "./providers/types";

/** onBacklog reports whether a card is listed in its team's backlog (K2): a
 *  PARKED task that answers for its own place — not a planning card (the
 *  Roadmap's), not a project card (the Project board's), not a review card
 *  (it follows the card it reviews), not a subtask (it rides its parent) —
 *  and not finished: the backlog never shows work that is over. Mirrors
 *  board.OnBacklog.
 *
 *  The FLAG decides, as board.InBacklog reads it: a task with no date and no
 *  sprint that is not parked is in no backlog (schema 3 parks such a card),
 *  and a parked card a direct writer left dates on is in one — drawn once,
 *  where it can be dealt with. */
export function onBacklog(
  c: Pick<Card, "parked" | "type" | "scope" | "reviewOf" | "parent" | "lineage" | "stage" | "progress" | "assignees">,
): boolean {
  if (!c.parked || isComplete(c)) {
    return false;
  }
  if (needsReviewer(c)) {
    return true;
  }
  return !isPlanning(c) && !c.scope && !c.reviewOf && !isSubtask(c);
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
 *  the order they stand in on the board.
 *
 *  There is no sort here on purpose. The client is sent no rank: the listing
 *  arrives in BacklogOrder, local moves splice the board's order
 *  (spliceOrder), and the server's Ordering frames re-sort it — so the order
 *  the cards stand in IS the server's order, and a TS copy of the rule would
 *  be a rule with nothing to run on. */
export function backlogList(cards: readonly Card[], teams: readonly string[]): Card[] {
  const shown = new Set(teams);
  return cards.filter((c) => onBacklog(c) && shown.has(c.team ?? ""));
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
export type BacklogDrop = { kind: "none" } | { kind: "reorder"; order: string[]; anchor: Anchor };

/** dropPlan reads a drop: a REORDER by the neighbours at the drop in the
 *  merged list (K5). The team does not change by a drop — handing a task to
 *  another team is the owner picker's (ownPlan). A card put back where it was
 *  writes nothing. `list` is the ids as drawn, the card itself included. */
export function dropPlan(id: string, list: readonly string[], at: number): BacklogDrop {
  const order = orderWith(list, id, at);
  if (order.every((x, i) => x === list[i])) {
    return { kind: "none" };
  }
  return { kind: "reorder", order, anchor: anchorFor(order, id) };
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
}

/** insertSpot reads the gap `at` of the list as drawn: 0 is above the
 *  first card, list.length below the last. */
export function insertSpot(list: readonly Pick<Card, "itemId" | "team">[], at: number, teams: readonly string[]): InsertSpot {
  const above = at > 0 ? list[at - 1] : undefined;
  const below = at < list.length ? list[at] : undefined;
  const anchor: Anchor = above ? { after: above.itemId } : below ? { before: below.itemId } : null;
  if (teams.length === 1) {
    return { anchor, box: { forcedTeam: teams[0] || null } };
  }
  const aboveTeam = above?.team ?? "";
  const start = above && teams.includes(aboveTeam) ? aboveTeam : (teams[0] ?? "");
  return {
    anchor,
    box: { teams: teams.filter((t) => t !== ""), allowNoTeam: teams.includes(""), initialTeam: start || null },
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
 *  lineage carries them. A task ancestor (a subtask's) is not part of it.
 *  A REVIEW card's chain ends in the task it reviews (status.reviewed), a
 *  crumb of its own that opens it. */
export function parentChain(c: Pick<Card, "lineage" | "reviewed">): LineageRef[] {
  const up = (c.lineage ?? []).filter((r) => !!r.type);
  return c.reviewed ? [...up, { uid: c.reviewed.uid, title: c.reviewed.title }] : up;
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
