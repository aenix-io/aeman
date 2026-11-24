// A card's counterpart: the small avatar beside whose the card is.

import type { Card } from "./providers/types";

/** counterpartOf is the people a card's small avatar shows: on a review card
 *  whose work it reviews, on a task who reviews it. The linked card when the
 *  board holds it — it is the freshest — else what the server said about it
 *  on the card itself (status.reviewed, status.reviewedBy): a Me board holds
 *  only the reader's cards, so a review of somebody else's task found no task
 *  there and drew no avatar. */
export function counterpartOf(card: Card, cards: readonly Card[]): string[] {
  const linked = card.reviewOf
    ? cards.find((c) => c.itemId === card.reviewOf)
    : cards.find((c) => c.reviewOf === card.itemId);
  if (linked) {
    return linked.assignees;
  }
  if (card.reviewOf) {
    return card.reviewed?.assignees ?? [];
  }
  return card.reviewedBy ? [card.reviewedBy] : [];
}
