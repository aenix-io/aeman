// What a person's LOAD reads as beside their name: the points they are
// carrying against the points a week they get through, and whether that is
// too much. Drawn on the TRIAGE board over every column — the board where a
// week is planned, and the one place the number has an answer to give; the
// numbers come from the server (metadata.members: board.LoadNow and
// board.CapacityOfPerson), whole across every team whatever the filter.

export type LoadState = "ok" | "full" | "over" | "unknown";

/** loadState says how a person's load stands against their capacity.
 *
 *  - `unknown`: no capacity — nobody has set one, and nothing derives it.
 *    The number is shown alone; nothing is red, because the board cannot
 *    say what it does not know.
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
