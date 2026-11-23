// A recurrent card's reseed cycle, as the stage menu names it.

/** RecurrenceCycle is a recurrent card's stored reseed cycle: "" is the
 *  board's own turn — every sprint. */
export type RecurrenceCycle = "" | "week" | "month";

export const recurrenceCycles: readonly RecurrenceCycle[] = ["", "week", "month"];

/** recurrenceLabel names a cycle in the stage menu. */
export function recurrenceLabel(cycle: RecurrenceCycle): string {
  switch (cycle) {
    case "week":
      return "Weekly";
    case "month":
      return "Monthly";
    default:
      return "Every sprint";
  }
}

/** recurrenceTitle is the status icon's tooltip on a recurrent card. */
export function recurrenceTitle(cycle: string | undefined): string {
  switch (cycle) {
    case "week":
      return "Recurrent (weekly)";
    case "month":
      return "Recurrent (monthly)";
    default:
      return "Recurrent";
  }
}

/** Repeat is the card pane's one choice for the stage menu's Recurrent and
 *  its cycle: "no", or the cycle a recurrent card is reseeded on. */
export type Repeat = "no" | RecurrenceCycle;

/** repeatOf reads a card's Repeat. */
export function repeatOf(c: { stage?: string; recurrence?: string }): Repeat {
  if (c.stage !== "recurrent") {
    return "no";
  }
  return (c.recurrence ?? "") as RecurrenceCycle;
}

/** repeatPatch is what choosing a Repeat writes — the same stage patch the
 *  stage menu's Recurrent makes (a cycle sets the stage and the cycle
 *  together; "no" clears both) — or null when the card already is that. */
export function repeatPatch(
  c: { stage?: string; recurrence?: string },
  to: Repeat,
): { stage: "recurrent" | ""; recurrence: RecurrenceCycle } | null {
  if (repeatOf(c) === to) {
    return null;
  }
  return to === "no" ? { stage: "", recurrence: "" } : { stage: "recurrent", recurrence: to };
}
