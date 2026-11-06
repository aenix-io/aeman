// The card the drawer shows, kept in step with the server even when no board
// lists it: a card opened by uid (a crumb followed from a review card on the
// Backlog, a planning card from a day board) is not in the board's cards, so
// the board's own updates never reach it. These are the drawer's.
import type { Card } from "./providers/types";

/** followPatch applies to the drawer's card a patch the board applies — an
 *  optimistic edit, or the server's answer to a write made from the drawer —
 *  when it is that card; any other card, or none, is left as it is. */
export function followPatch(
  shown: Card | null,
  itemId: string,
  patch: Partial<Card> | ((c: Card) => Partial<Card>),
): Card | null {
  if (!shown || shown.itemId !== itemId) {
    return shown;
  }
  const p = typeof patch === "function" ? patch(shown) : patch;
  if (!p || Object.keys(p).length === 0) {
    return shown;
  }
  return { ...shown, ...p };
}

/** followFrame applies a watch frame for the drawer's card: the card as the
 *  server now has it — what the frame cleared cleared — keeping the body and
 *  the notes a listing frame does not carry; a deletion closes it (null).
 *  Another card's frame leaves it as it is. */
export function followFrame(shown: Card | null, framed: Card, deleted: boolean): Card | null {
  if (!shown || shown.itemId !== framed.itemId) {
    return shown;
  }
  if (deleted) {
    return null;
  }
  return {
    ...framed,
    description: framed.description ?? shown.description,
    notes: framed.notes ?? shown.notes,
  };
}

/** cardIsGone is a Card frame for a card deleted outright: a DELETED that
 *  carries nothing but the uid (the server's tombstone). A scoped watch's
 *  DELETED for a card that only LEFT the board's selection carries the card
 *  as it now is, and the drawer showing it stays open. */
export function cardIsGone(frame: { type: string; object: { metadata?: { uid?: string }; spec?: { title?: string } } }): boolean {
  return frame.type === "DELETED" && !frame.object.spec?.title;
}
