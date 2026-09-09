// What the Team board draws on a day: the mirror of board.TeamGrid, card by
// card. It lives in a file of its own for the reason every other mirror does
// — the rule is the server's, the optimistic UI has to give the same answer
// before the server has spoken, and a copy sitting inside a component is a
// copy nothing tests. This one had drifted twice before it was a file.
import { mondayOf } from "./date";
import { parked } from "./backlog";
import { deferredPast, finishedOn, isComplete } from "./stages";
import type { Card } from "./providers/types";

/** inHandOn reports whether a card is in hand on a day: what the person is
 *  WORKING ON then, and nothing else. Mirrors board.TeamGrid (the team gate
 *  is the caller's — a board loads the teams it draws).
 *
 *  A card is in hand when it is not a subtask (those render nested under
 *  their parent), is not parked on a list, somebody has said WHEN it is for,
 *  it was not planned into a week after this day's, it is open or was
 *  finished on that very day, and it was not deferred past the day.
 *
 *  That is the whole rule. It used to be seven layered ones, and between them
 *  they put work nobody was doing on the day while dropping a card scheduled
 *  for last Tuesday and never finished, which no rule reached any more. */
export function inHandOn(c: Partial<Card>, day: string, today: string): boolean {
  if (c.parent || parked(c)) {
    return false;
  }
  // Somebody has to have said WHEN: a week, a date or a sprint. A card with
  // none of the three is the Triage strip — an inbox, not a day's work — and
  // drawing it here would land the inbox in every column and keep it there.
  if (!c.week && !c.startDate && !c.day && !c.sprintStart) {
    return false;
  }
  // A card planned into a week AHEAD of this day's is on no day board until
  // its Monday; the weeks BEHIND are never held back, so a debt does not fall
  // off the board when the week it was owed in ends.
  if (c.week && c.week > mondayOf(day)) {
    return false;
  }
  // Finished work belongs to the day it recorded, and to no other.
  if (isComplete(c) && !finishedOn(c, day)) {
    return false;
  }
  // The day a SPRINT began shows that sprint's own open work, ALL of it: most
  // of a sprint is created inside it, and a day that only showed what existed
  // on the Monday would show almost none of the work by Wednesday. Above the
  // deferral gate for that reason — and a card put off past TODAY is still
  // gone, because deferring is the act of taking it out of the sprint in
  // progress.
  if (c.sprintStart === day && !deferredPast(c, today)) {
    return true;
  }
  // Put off to a later day: gone until that day comes.
  return !deferredPast(c, day);
}
