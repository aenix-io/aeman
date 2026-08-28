// Merging a view's fresh listings into the board's cards.
//
// A listing is the board's ROW view: title, team, zone, dates, progress — no
// notes, no events, no description. Those arrive from the card's own
// subresources and are expensive on a git board (a log walks history). So a
// fresh listing is the truth about the row and nothing more: what it does not
// carry, the card keeps.

import type { Card } from "./providers/types";

/** mergeCardLists flattens a view's lists (e.g. the Team grid + its weekly
 *  plan), deduping by item id; board order within each list is preserved.
 *  `loaded` is the board's current cards, if any: a card coming back in the
 *  listing keeps the notes, events and body already fetched for it, so
 *  switching views does not throw away — and then re-fetch — a log per card. */
export function mergeCardLists(
  lists: Card[][],
  loaded: readonly Card[] = [],
): Card[] {
  const known = new Map(loaded.map((c) => [c.itemId, c]));
  const seen = new Set<string>();
  const out: Card[] = [];
  for (const fresh of lists.flat()) {
    if (seen.has(fresh.itemId)) {
      continue;
    }
    seen.add(fresh.itemId);
    out.push(withLoaded(fresh, known.get(fresh.itemId)));
  }
  return out;
}

/** withLoaded fills the fields a listing never carries from the card the
 *  board already had. The listing wins wherever it says anything at all. */
function withLoaded(fresh: Card, prev: Card | undefined): Card {
  if (!prev) {
    return fresh;
  }
  const merged = { ...fresh };
  if (merged.notes === undefined) {
    merged.notes = prev.notes;
  }
  if (merged.events === undefined) {
    merged.events = prev.events;
  }
  if (merged.description === undefined) {
    merged.description = prev.description;
  }
  return merged;
}
