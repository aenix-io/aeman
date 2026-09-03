import { describe, expect, it } from "vitest";
import type { Card } from "./providers/types";
import { gripOf, removableOnTriage } from "./triage";

const card = (over: Partial<Card> = {}): Card =>
  ({ itemId: "c1", title: "x", assignees: [], ...over }) as Card;

const CYCLE = { from: "2026-08-31", to: "2026-09-21" };

// How far a card may be carried in TIME on the Triage board. Every card
// changes hands freely; what the catch and the card's kind decide is the
// week.
describe("the grip on a card's week", () => {
  it("lets an ordinary card go to any week", () => {
    expect(gripOf(card(), { unlocked: false, accumulates: false })).toBe("free");
  });

  // Its weeks are the Project board's: on this board it changes hands, not
  // dates, until the catch is lifted.
  it("pins a project card, and frees it with the catch", () => {
    const slot = card({ epic: "Storage" });
    expect(gripOf(slot, { unlocked: false, accumulates: false })).toBe("pinned");
    expect(gripOf(slot, { unlocked: true, accumulates: false })).toBe("free");
  });

  // A turn is one turn of its process's calendar. Past the next due date it
  // stands where the NEXT turn belongs, and the two read as one process
  // running twice.
  it("holds a process turn inside its own cycle", () => {
    const turn = card({ task: "t1", cycle: CYCLE });
    expect(gripOf(turn, { unlocked: false, accumulates: false })).toEqual(CYCLE);
  });

  // The catch frees only the tasks whose turns are MEANT to pile up.
  it("frees an accumulating turn with the catch, and no other", () => {
    const turn = card({ task: "t1", cycle: CYCLE });
    expect(gripOf(turn, { unlocked: true, accumulates: true })).toBe("free");
    expect(gripOf(turn, { unlocked: true, accumulates: false })).toEqual(CYCLE);
  });

  it("does not move a turn whose task has no calendar at all", () => {
    // Per-sprint recurrence has no dates to reckon with, so there is no
    // occurrence to stay inside — and no way to say where it may go.
    const turn = card({ task: "t1" });
    expect(gripOf(turn, { unlocked: false, accumulates: false })).toBe("pinned");
    expect(gripOf(turn, { unlocked: true, accumulates: false })).toBe("pinned");
  });
});

// The × on this board destroys the board's own work and nothing else: a
// project card is a commitment made elsewhere, a turn is a process's record
// of a week it owed. Both answer to the catch.
describe("where the × is drawn on Triage", () => {
  it("is drawn on an ordinary card", () => {
    expect(removableOnTriage(card(), false)).toBe(true);
  });

  it("is not drawn on a project card or a process turn", () => {
    expect(removableOnTriage(card({ epic: "Storage" }), false)).toBe(false);
    expect(removableOnTriage(card({ task: "t1" }), false)).toBe(false);
  });

  it("is drawn on both once the catch is lifted", () => {
    expect(removableOnTriage(card({ epic: "Storage" }), true)).toBe(true);
    expect(removableOnTriage(card({ task: "t1" }), true)).toBe(true);
  });
});
