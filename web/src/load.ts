// What a person's LOAD reads as beside their name: the points they are
// carrying against the points a week they get through, and whether that is
// too much. Drawn on the Team and Triage boards over every column; the
// numbers come from the server (metadata.members: board.LoadNow and
// board.CapacityOfPerson), whole across every team whatever the filter.

import type { Card } from "./providers/types";
import { parked } from "./backlog";
import { isComplete } from "./stages";
import { pointsOf } from "./size";
import { placedIn, weeksCovered } from "./triage";

export type LoadState = "ok" | "full" | "over" | "unknown";

/** loadState says how a person's load stands against their capacity.
 *
 *  - `unknown`: no capacity — nobody set one and there is no record to
 *    derive one from. The number is shown alone; nothing is red, because
 *    the board cannot say what it does not know.
 *  - `ok`: below capacity.
 *  - `full`: exactly at capacity — a week that fits with nothing to spare.
 *  - `over`: past it — the plan does not fit the week. Bright red: this is
 *    the one thing a daily sync exists to notice. */
export function loadState(load: number, capacity: number | undefined): LoadState {
  if (!capacity) {
    return "unknown";
  }
  if (load > capacity) {
    return "over";
  }
  return load === capacity ? "full" : "ok";
}

/** loadLabel is the text beside the name: "21/40", or "21" with no
 *  capacity to measure against. */
export function loadLabel(load: number, capacity: number | undefined): string {
  return capacity ? `${load}/${capacity}` : `${load}`;
}

/** plannable is what a team can put into a week: its points a week less
 *  the share that history says will arrive on its own; with no share known,
 *  the whole of it. Mirrors board.Plannable. */
export function plannable(pointsAWeek: number, reactiveShare: number, known: boolean): number {
  if (!known) {
    return pointsAWeek;
  }
  return Math.floor((pointsAWeek * (100 - reactiveShare)) / 100);
}

/** weekPoints is what each week of the Triage board CARRIES in points, for
 *  the teams on screen: every open card standing in a week — a card of
 *  several weeks counting in each of them, like the card count does — and
 *  the strip's cards in the first row, where the board draws them. Held
 *  against `plannable` for the same teams, it says whether the week's plan
 *  fits. Subtasks ride their parent (pointsOf weighs them there); a parked
 *  card is on no week. */
export function weekPoints(
  cards: readonly Card[],
  teams: readonly string[],
  thisWeek: string,
): Map<string, number> {
  const out = new Map<string, number>();
  for (const c of cards) {
    if (c.parent || parked(c) || isComplete(c) || !teams.includes(c.team ?? "")) {
      continue;
    }
    const weeks = weeksCovered(c);
    const rows = weeks.length ? weeks : placedIn(c) ? [placedIn(c) as string] : [thisWeek];
    const pts = pointsOf(cards, c);
    for (const w of rows) {
      out.set(w, (out.get(w) ?? 0) + pts);
    }
  }
  return out;
}
