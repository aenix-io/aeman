// What a person may do to the team's work on their OWN board. The Me board
// shows the same cards the Team board does, but it is not the same seat: a
// lead plans, and a person works the plan. These rules are the difference,
// kept in one file so the two boards read one answer — them drifting apart on
// the × is what made this file necessary.
//
// The rule they exist for: a person's answer to work they will not do is the
// REFUSED stage, not the ×. Refusing leaves the card where the lead can see
// it and decide; removing it takes the decision away from them.
import type { ZoneKey } from "./providers/types";

/** ADD_ZONE is the only zone the Me board takes a new card in. Something that
 *  came up today is unplanned by definition; the other three zones are the
 *  plan, and the plan is the lead's to make on the Team board. A person
 *  filing their own work under Urgent or Planned is planning, on a board
 *  with no room to argue with it. */
export const ADD_ZONE: ZoneKey = "yellow";

/** acceptsNewCard reports whether the Me board offers its "add" form in a
 *  zone. */
export function acceptsNewCard(zone: ZoneKey): boolean {
  return zone === ADD_ZONE;
}

/** mayRemove reports whether the Me board draws an × on a card: only on one
 *  this person created themselves. Work somebody else planned for them is
 *  not theirs to take off the board — the answer to "I am not doing this" is
 *  the refused stage, which leaves the card standing for the lead.
 *
 *  A SUBTASK is out of the rule's reach: it is a piece of the card it hangs
 *  under rather than work assigned to anyone, so whoever can see the parent
 *  can add one and take it away again. */
export function mayRemove(
  c: { author?: string; parent?: string },
  me: string | undefined,
): boolean {
  if (c.parent) {
    return true;
  }
  return !!me && c.author === me;
}

/** sortableWithin reports whether a drag from one zone into another is
 *  allowed here: it is not. Dragging on the Me board reorders work inside a
 *  zone — the zone is the lead's statement of how the work was planned, and
 *  reordering your own day must not rewrite it.
 *
 *  It answers for EVERY gesture that would carry a card across, GROUPING
 *  included: a card nested under a parent in another zone renders under that
 *  parent, which is the crossing by another name — and it was the door left
 *  open when only the plain drop was guarded. */
export function sortableWithin(from: ZoneKey, to: ZoneKey): boolean {
  return from === to;
}
