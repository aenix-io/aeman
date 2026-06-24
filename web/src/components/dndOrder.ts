import type { Board } from "../providers/types";

/**
 * globalOrderFromGroups builds the new GLOBAL order of board.cards from the
 * final per-group ordering produced by a drop. The visible cards are taken in
 * group order (groups already concatenated in a stable order by the caller);
 * any card not present in the groups (off-view: other day / other engineer)
 * keeps its relative position and is appended after the visible ones — matching
 * reorderCards' own semantics so the two stay consistent.
 */
export function globalOrderFromGroups(
  board: Board,
  groupedIds: string[][],
): string[] {
  const visible = groupedIds.flat();
  const seen = new Set(visible);
  const rest = board.cards
    .map((c) => c.itemId)
    .filter((id) => !seen.has(id));
  return [...visible, ...rest];
}

/**
 * afterIdFor returns the item id immediately preceding cardId in the given
 * global order, or null when cardId is first (or absent). This is the anchor
 * passed to provider.moveCard.
 */
export function afterIdFor(order: string[], cardId: string): string | null {
  const idx = order.indexOf(cardId);
  if (idx <= 0) {
    return null;
  }
  return order[idx - 1];
}
