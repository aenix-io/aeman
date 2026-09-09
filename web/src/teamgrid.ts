// What the Team board draws on a day: the mirror of board.TeamGrid, card by
// card. It lives in a file of its own for the reason every other mirror does
// — the rule is the server's, the optimistic UI has to give the same answer
// before the server has spoken, and a copy sitting inside a component is a
// copy nothing tests. This one had drifted twice before it was a file.
import { activeOnDay, mondayOf } from "./date";
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
 *  finished on that very day, it was not deferred past the day, and — for a
 *  day still to come — somebody actually planned it for that day.
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
  if (deferredPast(c, day)) {
    return false;
  }
  // A day still to COME is a plan, not a state (see plannedFor).
  return day <= today || plannedFor(c, day);
}

/** plannedFor reports whether somebody put the card on a day: its own dates
 *  reach that day, or it was placed in the week the day belongs to.
 *
 *  Asked of the days AHEAD only, and that asymmetry is the point. TODAY is
 *  what is in hand — a card planned for last Tuesday and still open stands
 *  there, because work that ran over is the work most in need of being looked
 *  at. TOMORROW is a plan: it holds what somebody actually put there, and
 *  today's unfinished work is today's problem. Without the bound every open
 *  card stood on every future day, so a month out was simply the team's whole
 *  backlog. Mirrors board.plannedFor. */
export function plannedFor(c: Partial<Card>, day: string): boolean {
  if (c.week && c.week === mondayOf(day)) {
    return true;
  }
  return activeOnDay(c.startDate, c.day, day);
}
