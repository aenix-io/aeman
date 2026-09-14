// What the Me board draws on a day: the mirror of board.MeView, card by card.
// It lived inside MeBoard.tsx until a rule changed on the server and the
// component went on dropping the cards the server now sent — a copy sitting
// inside a component is a copy nothing tests. Whose card it is (the viewer's,
// or a parent of theirs) and the board's own toggles stay the caller's.
import { mondayOf } from "./date";
import { parked } from "./backlog";
import { activeSprint } from "./sprint";
import { isComplete } from "./stages";
import type { Board, Card } from "./providers/types";

/** onMyDay reports whether a card of the viewer's is on their board on a day.
 *
 *  A card planned into a week AHEAD of the day being looked at is on no day
 *  board until its Monday (B1) — judged against that day, as the Team board
 *  judges it. A card PLANNED FOR A WEEK and never dated is there once its week
 *  has come: dropping a card into a week ahead takes its dates and its sprint
 *  away, so when the Monday arrives it has a week and nothing else, and no
 *  carry-over adopts a card with no sprint. Finished, such a card belongs to
 *  the day it was finished on and no other.
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
  // Subtasks render nested under their parent, never as zone rows.
  if (c.parent) {
    return false;
  }
  if (c.week && c.week > mondayOf(day)) {
    return false;
  }
  if (parked(c)) {
    return false;
  }
  if (c.week && !c.startDate && !c.day && !c.sprintStart) {
    return !isComplete(c) || c.doneAt === day;
  }
  // A deferred / future-scheduled card (startDate past today) is hidden
  // until that day, then shows from it on (Carry Over re-syncs its sprint).
  if (c.startDate && c.startDate > today) {
    return day >= c.startDate;
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
