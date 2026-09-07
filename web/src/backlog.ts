// The backlog: a card's THIRD place, beside a week and the Triage strip.
//
// The strip is the inbox — work that arrived and nobody has read. The backlog
// is where something goes once somebody HAS read it and said "not now".
// Keeping the two apart is the whole point: an inbox must stay short to mean
// anything, and a shelf may be as long as it likes.
//
// EVERY TEAM HAS ONE — the no-team group included, which is a team like any
// other here — and nothing declares it. A team that plans has work it is not
// planning; that is a fact about the team rather than something somebody sets
// up, and a backlog nobody had to create is one that is there the first time
// it is needed.
//
// Mirrors pkg/board/backlog.go and the rules boardservice.SetBacklog enforces.

import type { Card, ZoneKey } from "./providers/types";

/** parked reports whether the card is on its team's shelf. */
export function parked(c: Pick<Card, "parked">): boolean {
  return !!c.parked;
}

/** parkable reports whether this card may be parked at all.
 *
 *  A Project-board slot and a process TURN are refused: their week is another
 *  board's to say — a slot's follows its start date, a turn's is its process's
 *  record of what that week was owed — so parking one here would edit a plan
 *  this board does not own. The server refuses it too (ErrNotYoursToPark);
 *  this is so the board never offers what would come back as an error. */
export function parkable(c: Pick<Card, "epic" | "task">): boolean {
  return !c.epic && !c.task;
}

/** ShelfDrop is what a drop on a team's shelf writes.
 *
 *  Parking is not a reassignment — the shelf a card lands on is its own team's
 *  — but a card dropped on ANOTHER team's shelf is being handed to that team,
 *  and the patch says so. The server applies the team first for the same
 *  reason, so one request does both. */
export function parkPatch(
  c: Pick<Card, "team">,
  team: string,
): { parked: true; team?: string } {
  return team === (c.team ?? "") ? { parked: true } : { parked: true, team };
}

/** parkedLocally is what the board shows the moment a card is parked, before
 *  the server answers: the working area emptied and the work made PLANNED,
 *  exactly as SetBacklog does both. Every board applies it — the × dialog's
 *  backlog answer is offered on all of them — and a copy of this shape that
 *  forgot the week would leave the card drawn in its old cell until the next
 *  reload, while one that forgot the zone would show the shelf wearing the
 *  colours of a day nobody is planning. */
export function parkedLocally(): {
  parked: true;
  zone: ZoneKey;
  week: undefined;
  assignees: string[];
} {
  return { parked: true, zone: "gray", week: undefined, assignees: [] };
}

// There is deliberately no ordering rule here. The server delivers a shelf
// already in reading order (board.BacklogOrder: by hand first, then oldest
// first), and the card the client receives carries no rank to sort by — the
// API does not send one. A TS copy would be a rule that cannot run, so the
// order the server sent IS the mirror.
