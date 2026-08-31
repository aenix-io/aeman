import { addDays, mondayOf } from "./date";

/** The slice of a card the weekly-band rules read. */
export interface Banded {
  plan?: "wed" | "fri";
  epic?: string;
  week?: string;
  day?: string;
}

/** isSlot reports whether a card is a Project-board slot: an epic column
 *  entry with both boundaries set. A slot's span IS its weekly plan. */
export function isSlot(c: Banded): boolean {
  return !!c.epic && !!c.week && !!c.day;
}

/** planRemoveOffered reports whether the weekly panel's × has anything to
 *  do for this card. Taking a card out of the plan clears its band — but a
 *  SLOT is on the panel by derivation, its span standing in for a band, so
 *  clearing one changes nothing anyone can see: the card stays exactly
 *  where it was. An × that does nothing is worse than no ×, so the panel
 *  does not offer one. (Off the panel is a matter of the slot's dates or
 *  its column, neither of which an × means.)
 *
 *  Nor does a card with nothing in the plan's records at all — no band and
 *  no week. The panel draws such cards as the nested SUBTASK rows of an
 *  expanded parent: they ride along visibly, and the server's plan × has
 *  nothing to empty for them (it returns without a write), so an × there
 *  would be the inert one this rule exists to prevent. */
export function planRemoveOffered(c: Banded): boolean {
  if (isSlot(c)) {
    return false;
  }
  // Mirrors what the server's plan × actually writes (Remove from="plan"):
  // a BAND is always something to empty, and a bare WEEK only on a card
  // outside every column — on a column card the week is the row it is
  // drawn in, not plan membership, so there is nothing to take away and
  // the × would be the inert one this rule exists to prevent.
  return !!c.plan || (!c.epic && !!c.week);
}

/** slotBand derives the weekly-plan band a band-less slot occupies on `week`'s
 *  panel. Only the week the slot ENDS in can be a by-Wednesday week; every
 *  earlier covered week holds the slot open through its Friday. Mirrors the
 *  PlanNone arm of pkg/board WeeklyPlanAt. */
export function slotBand(week: string, endDay: string): "wed" | "fri" {
  return mondayOf(endDay) === week && endDay <= addDays(week, 2)
    ? "wed"
    : "fri";
}

/** owedIn is the week a card was owed in: the one it belongs to, or — for a
 *  Project-board slot, whose span IS its plan — the week its span ENDS in,
 *  the only week of that span with a deadline in it. Mirrors pkg/board
 *  owedIn. */
export function owedIn(c: Banded): string {
  // A slot's span IS its plan, and the week it ENDS in is the only week of
  // that span with a deadline in it. Asked exactly as pkg/board owedIn asks
  // it — a column and an end date, the WEEK not required — since a direct
  // git writer can leave the week off and the two answers must still
  // agree.
  if (!c.plan && !!c.epic && !!c.day) {
    return mondayOf(c.day);
  }
  return c.week ?? "";
}

/** effectiveBand is the band a card carries as its own: the stored band when
 *  set (hand placement outranks derivation), else — for a slot — the band of
 *  the week its end falls in. A band-less non-slot has none.
 *
 *  On a PANEL, pass that panel's week: a card owed in an earlier week is a
 *  DEBT there and stands in the by-Wednesday band whatever it carries, since
 *  it is already late and the nearest deadline of the week it is shown in is
 *  the one it faces (mirrors pkg/board WeeklyPlanAt). Without the week the
 *  answer is the card's own band, which is what a grid row shows. */
export function effectiveBand(c: Banded, week?: string): "wed" | "fri" | undefined {
  const owed = owedIn(c);
  if (week !== undefined && owed !== "" && owed < week) {
    return "wed";
  }
  if (c.plan) {
    return c.plan;
  }
  if (!isSlot(c)) {
    return undefined;
  }
  // Against the PANEL's week when there is one, exactly as pkg/board
  // derives it: a slot spanning three weeks ends by Wednesday of the last
  // of them and stands in the by-Friday band of the earlier ones, so a
  // stripe read against the card's own week marked it "wed" while it sat
  // under "fri".
  return slotBand(week ?? mondayOf(c.day as string), c.day as string);
}
