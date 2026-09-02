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
 *  whatever kind of work it is. Then the project's own work, which is a
 *  commitment made elsewhere and only passing through here. Then the zones,
 *  in the order the Team board reads them: what must be done today, what
 *  turned up unasked, what was planned, and what to start if there is time.
 *
 *  Cards of the same rank keep the order the board holds them in, so the
 *  order somebody set by hand still means something among its peers. */
export function pileRank(
  c: Pick<Card, "overdue" | "epic" | "zone"> & { projected?: boolean },
): number {
  if (c.overdue) {
    return 0;
  }
  if (c.epic) {
    return 1;
  }
  // A turn a process will file is a commitment made elsewhere too, and it
  // will take the week's time whether or not anybody plans around it.
  if (c.projected) {
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

/** reachOf is the last week a card reaches — its own when it was never
 *  stretched. */
export function reachOf(c: Pick<Card, "week" | "day">): string {
  const weeks = weeksCovered(c);
  return weeks[weeks.length - 1] ?? "";
}
