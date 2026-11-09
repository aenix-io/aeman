// What the Me board draws on a day: the mirror of board.MeView, card by card.
// It lived inside MeBoard.tsx until a rule changed on the server and the
// component went on dropping the cards the server now sent — a copy sitting
// inside a component is a copy nothing tests. Whose card it is (the viewer's,
// or a parent of theirs) and the board's own toggles stay the caller's.
import { currentSprint } from "./sprint";
import { inHandOn } from "./teamgrid";
import { placerOf } from "./ownership";
import type { Board, Card } from "./providers/types";

/** onMyDay reports whether a card of the viewer's is on their board on a day:
 *  the Team board's rule (teamgrid.inHandOn) by the sprint of the team the
 *  card stands under (O13) — the Me board is the Team board for one person
 *  (D6). Mirrors board.MeView. It used to be a rule of its own, and the two
 *  boards drifted: a card closed on a day of a later sprint stood on the Team
 *  board that day and on no day of its owner's board. */
export function onMyDay(
  c: Partial<Card>,
  board: Board,
  day: string,
  today: string,
  place: (c: Card) => string = placerOf(board.cards ?? []),
): boolean {
  return inHandOn(c, day, today, currentSprint(board, place(c as Card) || null) ?? "");
}

/** meFilterPasses is the Me board's Team filter on one of the viewer's
 *  cards (O12): the team it stands under (O13: its own, else the plan
 *  item's above it) is among the chosen ones (the no-team group being ""),
 *  none chosen passing every card. The Me board has no Customer filter. */
export function meFilterPasses(c: Card, place: (c: Card) => string, teams: readonly string[] | null): boolean {
  return !teams || teams.length === 0 || teams.includes(place(c));
}

/** hiddenByFilters is how many of the cards the board would draw on the
 *  day with no filter the filter hides — the "N more tasks" button. The
 *  board draws rows and, nested under each, its subtasks on the day
 *  (`kidsOf`, unfiltered): a hidden row hides its subtasks with it, and a
 *  shown row's subtask the filter leaves out is hidden on its own. */
export function hiddenByFilters(
  rows: readonly Card[],
  kidsOf: (id: string) => readonly Card[],
  passes: (c: Card) => boolean,
): number {
  let n = 0;
  for (const r of rows) {
    const kids = kidsOf(r.itemId);
    n += passes(r) ? kids.filter((k) => !passes(k)).length : 1 + kids.length;
  }
  return n;
}

/** moreTasksLabel is the button's words. */
export function moreTasksLabel(n: number): string {
  return `${n} more ${n === 1 ? "task" : "tasks"}`;
}
