import { describe, expect, it } from "vitest";

import { keepPlaced, parkPatch, parkable, parked, parkedLocally, unplaced } from "./backlog";
import type { Card } from "./providers/types";

const card = (over: Partial<Card> = {}): Card =>
  ({ itemId: "c1", title: "A card", assignees: [], ...over }) as Card;

// A task is in work or in its team's backlog (K1), and the board must not
// offer to put work in a backlog where the server would refuse it.

describe("what may be parked", () => {
  it("takes ordinary work", () => {
    expect(parkable(card())).toBe(true);
  });

  // Its place is another board's to say: a slot's row follows its start date.
  it("refuses a project card", () => {
    expect(parkable(card({ scope: "Auth" }))).toBe(false);
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
  // K1: parked means out of work — no person, no day, no sprint. A shape that
  // kept the dates would be in both places at once, and the Backlog board
  // would draw a card the Team board's next frame also drew.
  it("takes it out of work: nobody, no day, no sprint, planned (K1, K3)", () => {
    expect(parkedLocally()).toEqual({
      parked: true,
      zone: "gray",
      assignees: [],
      startDate: undefined,
      day: undefined,
      sprintStart: undefined,
    });
  });

  it("says nothing about a week: a task has none (K7)", () => {
    expect(parkedLocally()).not.toHaveProperty("week");
  });

  // The zone is the half that was missing: the drawer draws a card by its
  // zone, so without it the shelf wore the colours of a day nobody is
  // planning until the answer came back.
  it("says the zone, not only that the card is parked", () => {
    expect(parkedLocally().zone).toBe("gray");
  });
});

// K1 is an invariant the server holds after every write (boardservice
// keepPlaced): an open task left with no date, no sprint and no parent to
// ride is parked in its team's backlog in the same request — a PLAIN park,
// nothing else about the card moves. The optimistic patches of the doors
// that can do that (ungrouping, detaching, freeing a deleted parent's
// subtasks) carry the same answer, or the card vanishes from every board
// until the next reload.
describe("an open task never stands nowhere (K1)", () => {
  it("is unplaced with no date, no sprint and no flag", () => {
    expect(unplaced(card(), false)).toBe(true);
    expect(unplaced(card({ progress: 30 }), false)).toBe(true);
  });

  it("is placed by a date, an end date, a sprint or the flag", () => {
    for (const over of [{ startDate: "2026-09-01" }, { day: "2026-09-01" }, { sprintStart: "2026-09-01" }, { parked: true }]) {
      expect(unplaced(card(over), false)).toBe(false);
    }
  });

  it("asks nothing of what has no place of its own: a subtask, a review, a project card, a planning card", () => {
    expect(unplaced(card({ parent: "t" }), true)).toBe(false);
    expect(unplaced(card({ reviewOf: "o" }), false)).toBe(false);
    expect(unplaced(card({ scope: "Auth" }), false)).toBe(false);
    expect(unplaced(card({ type: "epic" }), false)).toBe(false);
  });

  it("asks nothing of finished work", () => {
    expect(unplaced(card({ progress: 100 }), false)).toBe(false);
    expect(unplaced(card({ stage: "done" }), false)).toBe(false);
  });

  it("parks an unplaced task with a plain park and leaves a placed one alone", () => {
    expect(keepPlaced(card(), false)).toEqual({ parked: true });
    expect(keepPlaced(card({ sprintStart: "2026-09-01" }), false)).toEqual({});
  });
});
