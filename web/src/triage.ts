// Which week's column a card stands in on the Triage board, and which cards
// nobody has placed at all. The week is the whole of the decision here: a
// card belongs to a week, or to the pile of work nobody has dated yet.
//
// Mirrors board.NeedsTriage / board.TriageWeekOf on the server.
import type { Card } from "./providers/types";
import { addDays, mondayOf } from "./date";
import { isPersonalDomain } from "./domains";

/** needsTriage reports whether nobody has said WHEN the card's work is due:
 *  an open card of its own, with no week. The week is the whole of the
 *  decision — a card on today's board was put there by the day's planning,
 *  not by a week's — and that is the pile the strip exists to show.
 *
 *  What is NOT asked about: a subtask and a review card, which follow the
 *  card they belong to; a card SENT to review, whose work is done and which
 *  waits on a reviewer rather than on a week; a personal board's card, which
 *  is nobody else's to plan; and work already finished. Mirrors
 *  board.NeedsTriage — the state cards it also excludes never reach the
 *  browser, so there is nothing to check here.
 */
export function needsTriage(
  c: Pick<Card, "parent" | "reviewOf" | "week" | "domain" | "stage" | "progress">,
): boolean {
  if (isPersonalDomain(c.domain ?? "")) {
    return false;
  }
  if (c.parent || c.reviewOf || c.week) {
    return false;
  }
  if (c.stage === "review") {
    return false;
  }
  return c.stage !== "done" && (c.progress ?? 0) < 100;
}

/** placedIn is the Monday of the column a card stands in — its week, and
 *  nothing else. Null means it stands in none: the strip holds it until
 *  somebody says when the work is due. Being on today's board is not that
 *  decision; the day's planning put it there, not the week's. */
export function placedIn(c: Pick<Card, "week">): string | null {
  return c.week || null;
}

/** pileRank is where a card stands in a week's pile — what a reader working
 *  down a column should meet first.
 *
 *  A debt before anything else: it was due and is not done, and that is true
 *  whatever kind of work it is. Then the PROJECT's own work, then the
 *  PROCESSES' — both are commitments made elsewhere and only passing through
 *  here, and neither is a zone anybody set on this board. Then the zones, in
 *  the order the Team board reads them: what must be done today, what turned
 *  up unasked, what was planned, and what to start if there is time.
 *
 *  Cards of the same rank keep the order the board holds them in, so the
 *  order somebody set by hand still means something among its peers. */
export function pileRank(
  c: Pick<Card, "overdue" | "epic" | "zone" | "task"> & { projected?: boolean },
): number {
  if (c.overdue) {
    return 0;
  }
  if (c.epic) {
    return 1;
  }
  // A process turn, whether already filed or only drawn ahead: it will take
  // the week's time whether or not anybody plans around it.
  if (c.task || c.projected) {
    return 2;
  }
  switch (c.zone) {
    case "red":
      return 3;
    case "yellow":
      return 4;
    case "gray":
      return 5;
    case "green":
      return 6;
    default:
      return 7;
  }
}

/** byPile sorts a cell's cards into that order, leaving equals as they were. */
export function byPile<T>(
  of: (item: T) => Pick<Card, "overdue" | "epic" | "zone"> & { projected?: boolean },
) {
  return (a: T, b: T) => pileRank(of(a)) - pileRank(of(b));
}

/** orderWith is a cell's order once `id` has been dropped at place `at`.
 *
 *  The card is taken OUT before it is put back in, so a card dragged down
 *  past its own neighbours lands where the pointer is rather than one short
 *  of it — the preview and the write have to agree about that, and both
 *  count places in the list the reader is looking at. */
export function orderWith(ids: readonly string[], id: string, at: number): string[] {
  const rest = ids.filter((other) => other !== id);
  const place = Math.max(0, Math.min(at, rest.length));
  return [...rest.slice(0, place), id, ...rest.slice(place)];
}

/** How a card's place in a cell is written down. The board's order is global
 *  and a cell is a slice of it, so the write names a NEIGHBOUR — the server
 *  resolves the true anchor from it — and never a position.
 *
 *  A card at the top has nothing before it to sit after, so it is written as
 *  standing before the card now second; a card alone in its cell has no
 *  neighbour at all and nothing to say. */
export type Anchor = { after: string } | { before: string } | null;

export function anchorFor(ids: readonly string[], id: string): Anchor {
  const place = ids.indexOf(id);
  if (place < 0 || ids.length < 2) {
    return null;
  }
  return place > 0 ? { after: ids[place - 1] } : { before: ids[1] };
}

/** weeksCovered is every week a card occupies: the week it was placed in,
 *  through the week its end date reaches. Stretching a card over three weeks
 *  says the work takes three weeks, and each of them counts it against what
 *  the team can do — mirrors board.WeeksCovered. */
export function weeksCovered(c: Pick<Card, "week" | "day">): string[] {
  if (!c.week) {
    return [];
  }
  const out = [c.week];
  const last = c.day ? mondayOf(c.day) : "";
  for (let w = addDays(c.week, 7); last && w <= last; w = addDays(w, 7)) {
    out.push(w);
  }
  return out;
}

/** placedAhead reports whether the card is placed in a week ahead of today's.
 *  Such a card is on no day board until its Monday (B1): that is what makes
 *  the backlog a regulator rather than a list. Mirrors board.PlacedAhead. */
export function placedAhead(c: Pick<Card, "week">, today: string): boolean {
  return !!c.week && c.week > mondayOf(today);
}

/** inWeek reports whether the card is the given week's own work: any of the
 *  weeks it covers, or — in the CURRENT week — a DEBT owed in an earlier one,
 *  which stands beside that week's work without leaving the week it was owed
 *  in. It is what a week's column holds here and what the Team board's grid
 *  carries all week, so a card placed in a week is not invisible until
 *  somebody gives it a day. Mirrors board.InWeek.
 *
 *  The debt is the card's own `overdue`, which the server derives: a client
 *  re-deriving it from dates would answer differently the moment the two
 *  drifted. */
export function inWeek(
  c: Pick<Card, "week" | "day" | "overdue">,
  week: string,
  today: string,
): boolean {
  if (!c.week) {
    return false;
  }
  if (weeksCovered(c).includes(week)) {
    return true;
  }
  return week === mondayOf(today) && c.week < week && !!c.overdue;
}

/** reachOf is the last week a card reaches — its own when it was never
 *  stretched. */
export function reachOf(c: Pick<Card, "week" | "day">): string {
  const weeks = weeksCovered(c);
  return weeks[weeks.length - 1] ?? "";
}

/** Grip is how far a card may be carried in TIME on this board: anywhere,
 *  nowhere, or inside a window of weeks. Where it may go between PEOPLE is a
 *  separate question — every card changes hands freely. */
export type Grip = "free" | "pinned" | { from: string; to: string };

/** gripOf is what the catch (the padlock) and the card's own kind allow.
 *
 *  A PROJECT card's weeks are the Project board's: it is carried between
 *  hands and not in time, until the catch is lifted.
 *
 *  A PROCESS TURN belongs to one turn of its process's calendar, so it moves
 *  inside that occurrence and no further (`cycle`, sent by the server): a
 *  turn carried past the next due date stands where the next one belongs,
 *  and the two read as one process running twice. Lifting the catch frees
 *  only the tasks that ACCUMULATE — those are the ones whose turns are meant
 *  to pile up, so one standing in another's week is the point rather than a
 *  mistake. A turn whose task has no calendar at all (a per-sprint one) has
 *  no occurrence to stay inside and does not move in time.
 *
 *  Everything else the board draws is the board's own work, and its week is
 *  whichever one somebody drags it to. */
export function gripOf(
  c: Pick<Card, "epic" | "task" | "cycle">,
  opts: { unlocked: boolean; accumulates: boolean },
): Grip {
  if (c.epic) {
    return opts.unlocked ? "free" : "pinned";
  }
  if (c.task) {
    return opts.unlocked && opts.accumulates ? "free" : (c.cycle ?? "pinned");
  }
  return "free";
}

/** removableOnTriage reports whether the × is drawn on a card here. A PROJECT
 *  card and a PROCESS TURN are not this board's to destroy — one is a
 *  commitment made on the Project board, the other a process's record of a
 *  week it owed — so the × appears on them only with the catch lifted, the
 *  same gesture that admits everything else about them.
 *
 *  And only where it has something to OFFER. `choices` is what the card
 *  allows (removal.removeChoices): a project card or a turn that nobody has
 *  taken is already where the only option would put it, so the list is empty
 *  — and the × opened a dialog with no buttons in it, closing only on Escape.
 *  An × that does nothing reads as a delete that failed. */
export function removableOnTriage(
  c: Pick<Card, "epic" | "task">,
  unlocked: boolean,
  choices: readonly unknown[] = [1],
): boolean {
  if (choices.length === 0) {
    return false;
  }
  return (!c.epic && !c.task) || unlocked;
}

/** deferred reports that a card has been scheduled AWAY from today: its start
 *  date is still to come. Such a card lives on that day (and through its
 *  range) and is hidden everywhere else until the day arrives — on the day
 *  grid, and equally in the week it is due in, which is where it went on
 *  being drawn after somebody had deliberately taken it off today's board.
 *  Mirrors board.deferred (pkg/board/filters.go). */
export function deferred(
  c: Pick<Card, "startDate">,
  today: string,
): boolean {
  return !!c.startDate && c.startDate > today;
}
