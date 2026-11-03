// What the Backlog board draws and what its gestures mean — the board's
// decisions, apart from the component that draws them (docs/design/backlog.md,
// rules K1–K9).
//
// The board is ONE list: the PARKED OPEN TASKS (K2) of every selected team,
// the no-team group included, MIXED in one order — the order the server keeps
// them (board.BacklogOrder: by hand first, then oldest first; rank is one
// board-wide list, so the merged order is the board order of those cards).
// One wide card per row, each with a chip of its team and, on the right, the
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

/** BacklogSections is the board as it is drawn: the reviews nobody has taken
 *  PINNED on top, oldest first among themselves — asked for now, and not the
 *  lead's to rank — then the ranked tasks in the board's order (K10, K2).
 *  Only the tasks are dragged; a pinned review leaves when a reviewer has
 *  it. Mirrors apiserver.backlogCards. */
export interface BacklogSections {
  reviews: Card[];
  tasks: Card[];
}

export function backlogSections(cards: readonly Card[], teams: readonly string[]): BacklogSections {
  const all = backlogList(cards, teams);
  const reviews = all
    .filter((c) => needsReviewer(c))
    .sort((a, b) => (a.createdAt ?? "").localeCompare(b.createdAt ?? ""));
  return { reviews, tasks: all.filter((c) => !needsReviewer(c)) };
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

/** AddBox is what the one add box at the foot of the list offers: with one
 *  team shown, that team and no selector (forcedTeam); with several, a
 *  selector of them starting on the FIRST shown team — the no-team group
 *  among them when it is shown. AddCard's own props. */
export type AddBox =
  | { forcedTeam: string | null }
  | { teams: string[]; allowNoTeam: boolean; initialTeam: string | null };

export function addBox(teams: readonly string[]): AddBox {
  if (teams.length === 1) {
    return { forcedTeam: teams[0] || null };
  }
  return { teams: teams.filter((t) => t !== ""), allowNoTeam: teams.includes(""), initialTeam: teams[0] || null };
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
 *  lineage carries them. A task ancestor (a subtask's) is not part of it. */
export function parentChain(c: Pick<Card, "lineage">): LineageRef[] {
  return (c.lineage ?? []).filter((r) => !!r.type);
}

/** openPlanning opens a planning card's drawer from its name in a chain: the
 *  backlog holds no planning card, so it is fetched by uid and handed to the
 *  drawer the Roadmap opens. */
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

/** dueOf is the task's due date: its nearest planning card's end. Not
 *  borrowed from a farther ancestor — the epic is what the task is for, and
 *  a goal's end says nothing about when the epic is due. */
export function dueOf(c: Pick<Card, "lineage">): string | undefined {
  return nearestPlanning(c)?.end || undefined;
}

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

/** dueLabel is how a due date reads on a card: "18 Dec". */
export function dueLabel(iso: string): string {
  const [, m, d] = iso.split("-").map(Number);
  return `${d} ${MONTHS[m - 1]}`;
}
