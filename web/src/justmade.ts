// The cards the reader made in the view they are looking at NOW.
//
// It is what the × needs to know to act without asking. A card made a moment
// ago, here, is one the reader has the whole of in their head: they typed it,
// they can see it, and the × they reach for means "no, not that" — putting a
// dialog in front of that is ceremony over a line that took a second to write.
//
// Anything else is a card they are MEETING again: made in a view they have
// since left, or on a day they have come back to. They no longer hold all of
// it, and the × is a decision — which is exactly when it has to be named
// before it happens.
//
// The memory is deliberately this narrow. It used to be "made today and still
// at 0%", which sounds like the same thing and is not: a card typed at nine in
// the morning is still "today" at six in the evening, long after the reader
// stopped holding it, and the × went on deleting it in silence.

const made = new Set<string>();

/** noteMade records a card as made here. A create is drawn under a tmp id and
 *  swapped for the real one when the server answers; both are the same card to
 *  the reader and the × may fall on either, so both are recorded. */
export function noteMade(id: string): void {
  if (id) {
    made.add(id);
  }
}

/** justMade reports whether this card was made in the view being looked at. */
export function justMade(id: string): boolean {
  return made.has(id);
}

/** forgetMade is called when the reader moves to another view: what they made
 *  in the one they left is no longer in front of them. */
export function forgetMade(): void {
  made.clear();
}
