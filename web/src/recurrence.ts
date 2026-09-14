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
