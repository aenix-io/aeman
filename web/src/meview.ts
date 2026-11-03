// What the Me board draws on a day: the mirror of board.MeView, card by card.
// It lived inside MeBoard.tsx until a rule changed on the server and the
// component went on dropping the cards the server now sent — a copy sitting
// inside a component is a copy nothing tests. Whose card it is (the viewer's,
// or a parent of theirs) and the board's own toggles stay the caller's.
import { parked } from "./backlog";
import { activeSprint } from "./sprint";
import { isComplete } from "./stages";
import { isPlanning, isSubtask } from "./subtasks";
import type { Board, Card } from "./providers/types";

/** onMyDay reports whether a card of the viewer's is on their board on a day.
 *
 *  A task PARKED in its team's backlog is on nobody's day: it is waiting to
 *  be taken into work (K1). A task has no week (K7) — the week gates that
 *  used to draw a week-only card once its Monday came (D17) are retired, and
 *  a week an older repository left on a task is read as nothing.
 *
 *  Everything else is the day rule: a deferred card from its day on, a range
 *  across its days, a sprint-less day card from its day, and a sprint's work
 *  on every day of the sprints it spans. */
export function onMyDay(
  c: Partial<Card>,
  board: Board,
  day: string,
  today: string,
): boolean {
  // A planning card is the Roadmap's, not a person's day (H3).
  if (isPlanning(c)) {
    return false;
  }
  // Subtasks render nested under their parent, never as zone rows. A task
  // under an epic is not one (H1). Mirrors board.MeView.
  if (isSubtask(c)) {
    return false;
  }
  // A card in a Scope column is a PROJECT CARD and is drawn on the Project
  // board alone (H4): it has no owner to look for it here. Mirrors
  // board.MeView.
  if (c.scope) {
    return false;
  }
  // Waiting to be taken into work, not being worked on (K1).
  if (parked(c)) {
    return false;
  }
  // A deferred / future-scheduled card (startDate past today) is hidden
  // until that day, then shows from it on (Carry Over re-syncs its sprint).
  if (c.startDate && c.startDate > today) {
    return day >= c.startDate;
  }
  // An OPEN card of the viewer's keeps standing on their LIVE board (today)
  // even when its sprint has fallen behind the team's current one: a card left
  // out of a carry-over is still unfinished, and the Me board keeps everything
  // of theirs that is not closed. It is the open card the range gate (end day
  // past) and the sprint-span gate (sprint older than the active one) below
  // would both drop. Scoped to today and to a card that HAS a sprint, so a day
  // still to come is a plan, a past day reads by its own sprint, and an old
  // sprint-LESS stray still does not resurface. Mirrors board.MeView.
  if (day === today && c.sprintStart && !isComplete(c) && (!c.startDate || c.startDate <= day)) {
    return true;
  }
  // A card with an end date spans a range: it shows on every day from its
  // start through its end regardless of sprint boundaries.
  if (c.startDate && c.day && c.day >= c.startDate && day >= c.startDate && day <= c.day) {
    return true;
  }
  const as = activeSprint(board, c.team ?? null, day);
  // A sprint-less day card (a "next sprint" create) stays visible from its
  // scheduled day on — the sprint gate below would otherwise hide it right
  // when its day arrives, until a carry-over adopts it. Only cards scheduled
  // into the sprint active on the viewed day (or later) qualify: an old
  // sprint-less stray stays on its own past days.
  if (!c.sprintStart && c.startDate && c.startDate <= day && c.startDate >= as) {
    return true;
  }
  const ss = c.sprintStart;
  // A card shows on every day of the sprints it spans — from the one it
  // started in up to the sprint it now belongs to — so a carried-over card
  // still appears on the previous sprint's days it came from.
  return as !== "" && ss !== undefined && as <= ss && (!c.startDate || c.startDate <= day);
}
