import { mondayOf } from "./date";

/** The slice of a card the slot rules read. */
export interface Slotted {
  epic?: string;
  week?: string;
  day?: string;
}

/** isSlot reports whether a card is a Project-board slot: an epic column
 *  entry with both boundaries set. A slot's row on that board IS its span. */
export function isSlot(c: Slotted): boolean {
  return !!c.epic && !!c.week && !!c.day;
}

/** slotWeekPatch is the week that follows a card's dates. A card in a
 *  COLUMN has no week of its own — the server derives it from the start
 *  date (board.NewBoard: "a slot's row is its START date's week, never a
 *  week stored beside it") — so a client that moves the dates and leaves
 *  the week behind keeps drawing the card in the week it left, until the
 *  next full load says otherwise. Empty for a card outside every column,
 *  whose week is its own. */
export function slotWeekPatch(
  card: { epic?: string },
  start: string | null | undefined,
): { week?: string } {
  return card.epic && start ? { week: mondayOf(start) } : {};
}
