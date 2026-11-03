// The board's local card order, set by a reorder the server's Ordering frame
// will confirm. An order may be given as the ids, or as an UPDATER over the
// current ids: a placement worked out when an answer arrives (a card typed
// into a gap) is applied to the board as it is then, so cards that arrived
// in between keep their place instead of being pushed to the end.

/** OrderArg is a reorder: the ids in their new order, or an updater from
 *  the current ids to them. */
export type OrderArg = string[] | ((current: string[]) => string[]);

/** applyOrder sorts the cards by the order: the listed ones in its order,
 *  the unlisted after them in theirs. */
export function applyOrder<T extends { itemId: string }>(cards: readonly T[], order: OrderArg): T[] {
  const ids = typeof order === "function" ? order(cards.map((c) => c.itemId)) : order;
  const rank = new Map(ids.map((id, i) => [id, i]));
  const ranked = cards.filter((c) => rank.has(c.itemId)).sort((a, b) => rank.get(a.itemId)! - rank.get(b.itemId)!);
  const rest = cards.filter((c) => !rank.has(c.itemId));
  return [...ranked, ...rest];
}
