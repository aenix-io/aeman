import { describe, expect, it } from "vitest";
import type { Card } from "./providers/types";
import { gripOf, removableOnTriage } from "./triage";

const card = (over: Partial<Card> = {}): Card =>
  ({ itemId: "c1", title: "x", assignees: [], ...over }) as Card;

// How far a card may be carried in TIME on the Triage board. Every card
// changes hands freely; what the catch and the card's kind decide is the
// week.
describe("the grip on a card's week", () => {
  it("lets an ordinary card go to any week", () => {
    expect(gripOf(card(), { unlocked: false })).toBe("free");
  });

  // Its weeks are the Project board's: on this board it changes hands, not
  // dates, until the catch is lifted.
  it("pins a project card, and frees it with the catch", () => {
    const slot = card({ scope: "Storage" });
    expect(gripOf(slot, { unlocked: false })).toBe("pinned");
    expect(gripOf(slot, { unlocked: true })).toBe("free");
  });

  // A card's own recurrence is not a grip: the card comes round again by
  // carry-over, and the copy standing in a week is this board's to move.
  it("does not hold a recurrent card to a window", () => {
    const repeating = card({ stage: "recurrent", recurrence: "month" });
    expect(gripOf(repeating, { unlocked: false })).toBe("free");
  });
});

// The × on this board destroys the board's own work and nothing else: a
// project card is a commitment made elsewhere. It answers to the catch.
describe("where the × is drawn on Triage", () => {
  it("is drawn on an ordinary card", () => {
    expect(removableOnTriage(card(), false)).toBe(true);
  });

  it("is not drawn on a project card", () => {
    expect(removableOnTriage(card({ scope: "Storage" }), false)).toBe(false);
  });

  it("is drawn on a project card once the catch is lifted", () => {
    expect(removableOnTriage(card({ scope: "Storage" }), true)).toBe(true);
  });

  // ...but only while there is something to offer. With the catch lifted, a
  // project card that NOBODY has taken has an empty list: the one option
  // would move it to Unassigned, which is where it already stands. The × was
  // drawn all the same and opened a dialog with no buttons in it, closing
  // only on Escape — an × that does nothing reads as a delete that failed,
  // which is the reading this whole gesture was rebuilt to avoid.
  it("is not drawn when the card has no answer to give", () => {
    expect(removableOnTriage(card({ scope: "Storage" }), true, [])).toBe(false);
    expect(removableOnTriage(card(), false, [])).toBe(false);
  });

  it("is drawn once the card has one", () => {
    expect(removableOnTriage(card({ scope: "Storage" }), true, ["unassign"])).toBe(true);
  });
});
