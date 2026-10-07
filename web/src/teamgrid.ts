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
export function inHandOn(
  c: Partial<Card>,
  day: string,
  today: string,
  currentSprint = "",
): boolean {
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
  // THE CURRENT SPRINT ACCUMULATES ALL ITS WORK ON EVERY DAY: the day it began,
  // the days since and today each hold the work it opened with, the work typed
  // into it since, and the work already CLOSED — above both the start-date
  // gate and the finished gate for that reason. What still leaves it is what
  // was taken OUT of the sprint: a card deferred past TODAY goes at once, and
  // one planned into a week to come never reached it. Mirrors board.inSprintOn.
  if (inSprintOn(c, day, today, currentSprint) && !deferredPast(c, today)) {
    return true;
  }
  // Finished work belongs to the day it recorded — except on TODAY, where a
  // done card of a sprint the team has CARRIED PAST was left behind on purpose
  // and does not cling to today even if finished today; it stays on its own
  // sprint's day. A done card of the current sprint is kept above, and one in
  // no sprint is never closed. Mirrors board.inClosedSprint.
  const closedSprint =
    !!c.sprintStart && currentSprint !== "" && c.sprintStart !== currentSprint;
  if (isComplete(c) && (!finishedOn(c, day) || (day === today && closedSprint))) {
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
 *  included. The team's CURRENT sprint accumulates all of its work on every
 *  one of its days, from the day it began through today, so a lead opening any
 *  day of the running sprint reads "what has this sprint been" — closed work
 *  included — rather than "what is left". Carry Over leaves a finished card on
 *  the closing sprint, so once a new sprint opens that work is a PREVIOUS
 *  sprint's and stops showing on the current board. A sprint that is no longer
 *  current answers for its whole self only on the day it BEGAN; with no sprint
 *  pointer the current sprint is unknown, so the same narrow answer holds.
 *  Mirrors board.inSprintOn. */
export function inSprintOn(
  c: Partial<Card>,
  day: string,
  today = "",
  currentSprint = "",
): boolean {
  if (!c.sprintStart) {
    return false;
  }
  if (c.sprintStart === currentSprint) {
    return c.sprintStart <= day && day <= today;
  }
  return c.sprintStart === day;
}
