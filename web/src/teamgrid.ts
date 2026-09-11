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
 *  day still to come — somebody actually planned it for that day. The day a
 *  SPRINT began answers for the whole sprint instead, closed work included.
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
  // THE SPRINT'S OWN DAY IS THE WHOLE SPRINT: the day it began holds the work
  // it opened with, the work typed into it since and the work already CLOSED —
  // above both the start-date gate and the finished gate for that reason. What
  // still leaves it is what was taken OUT of the sprint: a card deferred past
  // TODAY goes at once, and one planned into a week to come never reached it.
  // Mirrors board.inSprintOn.
  if (inSprintOn(c, day) && !deferredPast(c, today)) {
    return true;
  }
  // Finished work belongs to the day it recorded, and to no other.
  if (isComplete(c) && !finishedOn(c, day)) {
    return false;
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

/** inSprintOn reports that a day answers for the card's SPRINT, which is what
 *  makes that day hold the sprint's work whatever has become of it, finished
 *  included. ONE day does: the day the sprint BEGAN — its own page, the one
 *  the "current sprint" jump lands on. Deliberately not every day of a running
 *  sprint: today is what is in HAND, and a card finished on Tuesday standing
 *  on Wednesday too is the thing the day rule set out to end. Mirrors
 *  board.inSprintOn. */
export function inSprintOn(c: Partial<Card>, day: string): boolean {
  return !!c.sprintStart && c.sprintStart === day;
}
