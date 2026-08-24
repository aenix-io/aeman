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

/** slotBand derives the weekly-plan band a band-less slot occupies on `week`'s
 *  panel. Only the week the slot ENDS in can be a by-Wednesday week; every
 *  earlier covered week holds the slot open through its Friday. Mirrors the
 *  PlanNone arm of pkg/board WeeklyPlanAt. */
export function slotBand(week: string, endDay: string): "wed" | "fri" {
  return mondayOf(endDay) === week && endDay <= addDays(week, 2)
    ? "wed"
    : "fri";
}

/** effectiveBand is the band a card carries as its own: the stored band when
 *  set (hand placement outranks derivation), else — for a slot — the band of
 *  the week its end falls in. A band-less non-slot has none. */
export function effectiveBand(c: Banded): "wed" | "fri" | undefined {
  if (c.plan) {
    return c.plan;
  }
  if (!isSlot(c)) {
    return undefined;
  }
  return slotBand(mondayOf(c.day as string), c.day as string);
}
