// Which area of the Triage board a card belongs to, and which week's column
// it stands in. The board has two areas per week and a strip above them, and
// what separates them is where the work came from — not a field on the card.
//
// PLAN is what the weekly panel shows for that week: the founders' plan cards
// (a band), the Project-board slots whose span covers it, and the process
// turns filed into it. WORK is the rest of what is scheduled for the week —
// the board's own cards, the ones somebody put on a week rather than into the
// plan. The strip holds the cards nobody has put anywhere yet.
//
// Mirrors board.NeedsTriage / board.TriageWeekOf on the server.
import type { Card } from "./providers/types";
import { isSlot } from "./weekly";
import { addDays, mondayOf } from "./date";
import { isPersonalDomain } from "./domains";

/** Area is where a card is drawn in a week's column. */
export type Area = "plan" | "work";

/** areaOf reports which area a card belongs to. A card is PLAN when the
 *  weekly panel would show it: it carries a plan band, it is a Project-board
 *  slot (its span IS its plan), or it is a process turn. Everything else the
 *  board schedules is WORK. */
export function areaOf(c: Pick<Card, "plan" | "epic" | "week" | "day" | "task">): Area {
  if (c.plan || c.task || isSlot(c)) {
    return "plan";
  }
  return "work";
}

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
