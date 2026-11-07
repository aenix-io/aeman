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

/** placementAnswer is what a board takes from the server's answer to a
 *  write that can move where a card stands — own, a team, a parent (O13):
 *  its own team, the team it stands under, its customers, its sprint and
 *  its chain, as the server now has them. */
export function placementAnswer(u: Card): Partial<Card> {
  return {
    team: u.team,
    effectiveTeam: u.effectiveTeam,
    effectiveCustomers: u.effectiveCustomers,
    sprintStart: u.sprintStart,
    parent: u.parent,
    lineage: u.lineage,
  };
}

/** drawerEscapes reports an Esc that closes the drawer: not one a control
 *  inside it took first (an add form's Esc cancels the form and marks the
 *  key handled), nor one while the title is being edited or a dialog is
 *  open over the drawer (`busy`). */
export function drawerEscapes(e: { key: string; defaultPrevented: boolean }, busy: boolean): boolean {
  return e.key === "Escape" && !e.defaultPrevented && !busy;
}

/** joinsTree reports a card made elsewhere — an ADDED frame — that belongs
 *  in the drawer's tree: its parent chain, through the cards the tree holds,
 *  reaches the card the drawer shows. One already there is not new, and a
 *  hand-written cycle ends the walk. */
export function joinsTree(tree: readonly Card[], framed: Card, rootId: string): boolean {
  const byId = new Map(tree.map((c) => [c.itemId, c]));
  if (byId.has(framed.itemId)) {
    return false;
  }
  const seen = new Set<string>();
  let up = framed.parent;
  while (up && !seen.has(up)) {
    if (up === rootId) {
      return true;
    }
    seen.add(up);
    up = byId.get(up)?.parent;
  }
  return false;
}

/** withoutDeleted takes a card deleted outright out of the drawer's
 *  history, so Back never lands on it; the same array when it was not in it. */
export function withoutDeleted(stack: Card[], uid: string): Card[] {
  return stack.some((c) => c.itemId === uid) ? stack.filter((c) => c.itemId !== uid) : stack;
}
