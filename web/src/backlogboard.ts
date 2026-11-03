// What the Backlog board draws and what its gestures mean — the board's
// decisions, apart from the component that draws them (docs/design/backlog.md,
// rules K1–K9).
//
// The board is one column per selected team, the no-team group included; a
// column is that team's PARKED OPEN TASKS (K2), one wide card per row, in the
// order the server keeps them (board.BacklogOrder: by hand first, then oldest
// first). Dragging within a column reorders; dragging to another team's column
// hands the task to that team, still parked (K5); the owner picker with a
// person takes it into work (K4), and it leaves the backlog at once.
//
// The listing is the backlog view; the WATCH is every card of the shown teams
// (viewquery.watchQuery), so a card parked, taken into work, re-teamed or
// finished by anybody lands in the board's cards and the columns are simply
// recomputed from them (K6). Nothing here keeps state of its own.
//
// Mirrors pkg/board/backlog.go (OnBacklog) and the doors boardservice opens
// on a parked card (Own, Move).
import { isPlanning, isSubtask } from "./subtasks";
import { isComplete } from "./stages";
import type { Owner } from "./owner";
import type { Card, LineageRef } from "./providers/types";

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
  c: Pick<Card, "parked" | "type" | "scope" | "reviewOf" | "parent" | "lineage" | "stage" | "progress">,
): boolean {
  if (!c.parked || isComplete(c)) {
    return false;
  }
  return !isPlanning(c) && !c.scope && !c.reviewOf && !isSubtask(c);
}

/** columnTeams is the teams the board draws a column for: the chips'
 *  selection, or — with none — every team and the no-team group, the same
 *  set the board's listing asks for (App: viewQueries). */
export function columnTeams(filter: readonly string[] | null, roster: readonly string[]): string[] {
  return filter ? [...filter] : [...new Set([...roster, ""])];
}

/** BacklogColumn is one selected team's backlog: "" is the no-team group. */
export interface BacklogColumn {
  team: string;
  cards: Card[];
}

/** backlogColumns lays the board out: one column per selected team, in the
 *  order the team chips put them — a team with nothing parked still gets its
 *  column, since that is where its add box is — and in each the team's
 *  backlog cards in the order they stand in on the board.
 *
 *  There is no sort here on purpose. The client is sent no rank: the listing
 *  arrives in BacklogOrder, local moves splice the board's order
 *  (spliceOrder), and the server's Ordering frames re-sort it — so the order
 *  the cards stand in IS the server's order, and a TS copy of the rule would
 *  be a rule with nothing to run on. */
export function backlogColumns(cards: readonly Card[], teams: readonly string[]): BacklogColumn[] {
  const byTeam = new Map<string, Card[]>(teams.map((t) => [t, []]));
  for (const c of cards) {
    if (!onBacklog(c)) {
      continue;
    }
    byTeam.get(c.team ?? "")?.push(c);
  }
  return teams.map((team) => ({ team, cards: byTeam.get(team) ?? [] }));
}

/** orderWith is a column's order once `id` has been dropped at place `at`.
 *
 *  The card is taken OUT before it is put back in, so a card dragged down
 *  past its own neighbours lands where the pointer is rather than one short
 *  of it — the preview and the write have to agree about that, and both
 *  count places in the list the reader is looking at. A card that came from
 *  another column is simply inserted. */
export function orderWith(ids: readonly string[], id: string, at: number): string[] {
  const rest = ids.filter((other) => other !== id);
  const place = Math.max(0, Math.min(at, rest.length));
  return [...rest.slice(0, place), id, ...rest.slice(place)];
}

/** How a card's place in a column is written down. The board's order is one
 *  list and a column is a slice of it, so the write names a NEIGHBOUR — the
 *  server resolves the rank from it — and never a position (K5).
 *
 *  A card at the top has nothing before it to sit after, so it is written as
 *  standing before the card now second; a card alone in its column has no
 *  neighbour at all and nothing to say. */
export type Anchor = { after: string } | { before: string } | null;

export function anchorFor(ids: readonly string[], id: string): Anchor {
  const place = ids.indexOf(id);
  if (place < 0 || ids.length < 2) {
    return null;
  }
  return place > 0 ? { after: ids[place - 1] } : { before: ids[1] };
}

/** spliceOrder is the board-wide order once a column has been re-ordered:
 *  the column's cards take the slots they (and a card dropped in from
 *  elsewhere) already held, in the column's new order, and every other card
 *  stays where it was. It is what the board keeps locally until the server's
 *  Ordering frame says the same. */
export function spliceOrder(all: readonly string[], column: readonly string[]): string[] {
  const inColumn = new Set(column);
  let i = 0;
  return all.map((id) => (inColumn.has(id) ? column[i++] : id));
}

/** BacklogDrop is what a drop means. */
export type BacklogDrop =
  | { kind: "none" }
  | { kind: "reorder"; order: string[]; anchor: Anchor }
  | { kind: "hand-over"; team: string; order: string[]; anchor: Anchor };

/** dropPlan reads a drop: within the card's own column it is a REORDER by
 *  the neighbours at the drop; on another team's column it HANDS the card to
 *  that team — still parked — at the place it was dropped (K5). A card put
 *  back where it was writes nothing.
 *
 *  `target` is the target column's ids as drawn, the card itself included
 *  when it is that column's own. */
export function dropPlan(
  id: string,
  fromTeam: string,
  toTeam: string,
  target: readonly string[],
  at: number,
): BacklogDrop {
  const order = orderWith(target, id, at);
  if (fromTeam === toTeam) {
    if (order.every((x, i) => x === target[i])) {
      return { kind: "none" };
    }
    return { kind: "reorder", order, anchor: anchorFor(order, id) };
  }
  return { kind: "hand-over", team: toTeam, order, anchor: anchorFor(order, id) };
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

/** nearestPlanning is the card's tag: the nearest planning card above it —
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
