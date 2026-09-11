import { describe, expect, it } from "vitest";

import { parkPatch, parkable, parked, parkedLocally } from "./backlog";
import type { Card } from "./providers/types";

const card = (over: Partial<Card> = {}): Card =>
  ({ itemId: "c1", title: "A card", assignees: [], ...over }) as Card;

// The shelf is the third place a card can be, and the board must not offer to
// put work somewhere the server would refuse.

describe("what may be parked", () => {
  it("takes ordinary work", () => {
    expect(parkable(card())).toBe(true);
  });

  // Their week is another board's to say: a slot's follows its start date, a
  // turn's is its process's record of the week it owed.
  it("refuses a project card and a process turn", () => {
    expect(parkable(card({ epic: "Auth" }))).toBe(false);
    expect(parkable(card({ task: "t1" }))).toBe(false);
  });

  // Neither has a place of its own to be parked out of: a review follows the
  // card it reviews, a subtask stands inside its parent. The × never offered
  // the shelf for them (removal.ts) while the drawer still did, so the same
  // card was both parkable and not depending on which gesture was used.
  it("refuses a review card and a subtask", () => {
    expect(parkable(card({ reviewOf: "c9" }))).toBe(false);
    expect(parkable(card({ parent: "c9" }))).toBe(false);
  });
});

describe("whether a card is parked", () => {
  it("is what the flag says, and nothing else", () => {
    expect(parked(card({ parked: true }))).toBe(true);
    expect(parked(card())).toBe(false);
    expect(parked(card({ parked: false }))).toBe(false);
  });
});

// Parking is not a reassignment — until it is. A card dropped on ANOTHER
// team's shelf is being handed to that team, and the patch has to say so or
// the card lands on a shelf the drawer does not draw it in.
describe("the patch a drop on a shelf writes", () => {
  it("says only 'parked' for the card's own team", () => {
    expect(parkPatch(card({ team: "alpha" }), "alpha")).toEqual({
      parked: true,
    });
  });

  it("carries the team when the shelf belongs to another one", () => {
    expect(parkPatch(card({ team: "alpha" }), "beta")).toEqual({
      parked: true,
      team: "beta",
    });
  });

  it("treats the no-team group as a team on both sides", () => {
    // Dropping a team's card on the no-team shelf takes its team away, and a
    // no-team card dropped on its own shelf changes nothing.
    expect(parkPatch(card({ team: "alpha" }), "")).toEqual({
      parked: true,
      team: "",
    });
    expect(parkPatch(card({}), "")).toEqual({ parked: true });
  });
});

// What the board shows the instant a card is parked, before the server
// answers. It has to be everything SetBacklog does, or the card sits wrong
// until the round trip lands — and stays wrong if the write fails.
describe("the optimistic shape of a parked card", () => {
  it("empties the working area and makes the work planned", () => {
    expect(parkedLocally()).toEqual({
      parked: true,
      zone: "gray",
      week: undefined,
      assignees: [],
    });
  });

  // The zone is the half that was missing: the drawer draws a card by its
  // zone, so without it the shelf wore the colours of a day nobody is
  // planning until the answer came back.
  it("says the zone, not only that the card is parked", () => {
    expect(parkedLocally().zone).toBe("gray");
  });
});
