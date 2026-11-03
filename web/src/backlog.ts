// The backlog: one ranked list per team of the tasks it has accepted and has
// not taken into work (docs/design/backlog.md). A task is in exactly one of
// two places (K1): IN WORK — it has a date or a sprint, and stands on the day
// boards — or PARKED in its team's backlog, on no day board. There is no
// third place: the Triage strip is gone, and every door that used to leave a
// task there parks it instead (K3).
//
// EVERY TEAM HAS ONE — the no-team group included, which is a team like any
// other here — and nothing declares it. A team that plans has work it is not
// planning; that is a fact about the team rather than something somebody sets
// up, and a backlog nobody had to create is one that is there the first time
// it is needed.
//
// Mirrors pkg/board/backlog.go and the rules boardservice.SetBacklog enforces.

import type { Card, ZoneKey } from "./providers/types";
import { isPlanning, isSubtask } from "./subtasks";

/** parked reports whether the card is on its team's shelf. */
export function parked(c: Pick<Card, "parked">): boolean {
  return !!c.parked;
}

/** parkable reports whether this card may be parked at all.
 *
 *  A PROJECT card is refused: it is the Project board's plan and stands in
 *  its column alone (H4), so parking it here would edit a plan this board
 *  does not own. The server refuses it too (ErrNotYoursToPark); this is so
 *  the board never offers what would come back as an error.
 *
 *  A REVIEW card and a SUBTASK are refused for the other reason: neither has
 *  a place of its own to be parked out of. One follows the card it reviews,
 *  the other stands inside its parent, so a shelf takes it out of the only
 *  place it is drawn. The × already withheld the answer for both
 *  (removal.ts) while the drawer went on accepting the drop, which made the
 *  same card parkable or not depending on which gesture was used;
 *  ErrNoPlaceOfItsOwn is the server's side of it. */
export function parkable(c: Pick<Card, "scope" | "reviewOf" | "parent" | "lineage" | "type">): boolean {
  // A planning card has no shelf (H3); a task under an epic has a place of
  // its own and may be parked like any task (H1).
  return !c.scope && !c.reviewOf && !isSubtask(c) && !isPlanning(c);
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
 *  the server answers: out of work — nobody, no day, no sprint — and the work
 *  made PLANNED, exactly as SetBacklog does (K1, K3). Every board applies it —
 *  the × dialog's backlog answer is offered on all of them — and a copy of
 *  this shape that kept the dates would leave the card in work and in the
 *  backlog at once until the next reload, while one that forgot the zone
 *  would show the backlog wearing the colours of a day nobody is planning. */
export function parkedLocally(): {
  parked: true;
  zone: ZoneKey;
  assignees: string[];
  startDate: undefined;
  day: undefined;
  sprintStart: undefined;
} {
  return {
    parked: true,
    zone: "gray",
    assignees: [],
    startDate: undefined,
    day: undefined,
    sprintStart: undefined,
  };
}

// There is deliberately no ordering rule here. The server delivers a shelf
// already in reading order (board.BacklogOrder: by hand first, then oldest
// first), and the card the client receives carries no rank to sort by — the
// API does not send one. A TS copy would be a rule that cannot run, so the
// order the server sent IS the mirror.
