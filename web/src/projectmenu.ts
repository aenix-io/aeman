// What a project card's "…" menu offers, apart from the menu that draws it
// (ProjectBoard). A project card lives and dies in its column — there is no
// taking it out of its project, and the one way off the board is Delete,
// which the menu does at once: the pick IS the confirmation. The card's
// pane keeps its own Delete, which asks, as every pane's does.
import { isComplete } from "./stages";
import type { Card } from "./providers/types";

export type SlotMenuItem =
  | { key: "open"; label: string }
  | { key: "done"; label: string; reopen: boolean }
  | { key: "delete"; label: string; asks: false };

/** slotMenuItems is the menu, top to bottom: open the card, mark it done (or
 *  back), delete it. Below them the menu lists the goals, initiatives and
 *  epics to file it under, with their search (ParentOptionList) — at once,
 *  with no "Attach to…" item opening a second picker (#288). */
export function slotMenuItems(c: Pick<Card, "stage" | "progress">): SlotMenuItem[] {
  const reopen = isComplete(c);
  return [
    { key: "open", label: "Open" },
    { key: "done", label: reopen ? "Reopen" : "Mark as done", reopen },
    { key: "delete", label: "Delete", asks: false },
  ];
}
