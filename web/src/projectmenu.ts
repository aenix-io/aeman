// What a project card's "…" menu offers, apart from the menu that draws it
// (ProjectBoard). A project card lives and dies in its column — there is no
// taking it out of its project, and the one way off the board is Delete,
// which the menu does at once: the pick IS the confirmation. The card's
// pane keeps its own Delete, which asks, as every pane's does.
import { isComplete } from "./stages";
import type { Card } from "./providers/types";

export type SlotMenuItem =
  | { key: "open"; label: string }
  | { key: "attach"; label: string }
  | { key: "done"; label: string; reopen: boolean }
  | { key: "delete"; label: string; asks: false };

/** slotMenuItems is the menu, top to bottom: open the card, file it under a
 *  goal, an initiative or an epic, mark it done (or back), delete it. */
export function slotMenuItems(c: Pick<Card, "stage" | "progress">): SlotMenuItem[] {
  const reopen = isComplete(c);
  return [
    { key: "open", label: "Open" },
    { key: "attach", label: "Attach to goal, initiative or epic…" },
    { key: "done", label: reopen ? "Reopen" : "Mark as done", reopen },
    { key: "delete", label: "Delete", asks: false },
  ];
}
