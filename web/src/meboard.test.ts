import { describe, expect, it } from "vitest";
import type { ZoneKey } from "./providers/types";
import { ADD_ZONE, acceptsNewCard, mayRemove, sortableWithin } from "./meboard";

// The Me board is the person's own view of the team's work, and what they may
// do to it there is narrower than what a lead may do on the Team board. The
// rules are here rather than in the component so both boards read one answer
// — the boards drifting apart on the × is what made this file necessary in
// the first place.

// A person adds work to their OWN board only as unplanned: something came up
// today. The other three zones are the plan, and the plan is the lead's to
// make — a person quietly filing their own work under Urgent or Planned is
// planning, on a board that has no room to argue with it.
describe("where a card can be added on the Me board", () => {
  it("takes a new card in the unplanned zone", () => {
    expect(acceptsNewCard("yellow")).toBe(true);
  });

  it("takes none in the zones the plan owns", () => {
    expect(acceptsNewCard("red")).toBe(false);
    expect(acceptsNewCard("gray")).toBe(false);
    expect(acceptsNewCard("green")).toBe(false);
  });
});

// The × on the Me board removes only what the person put there themselves,
// on this board: their own card, still standing in the one zone this board
// adds to. Work somebody else planned for them is not theirs to take off the
// board — and neither is work they planned SOMEWHERE ELSE, on the Team or
// Triage board, which is where a lead plans and where that × belongs. The
// answer to "I am not doing this" is the refuse stage, which leaves the card
// where the lead can see it and decide.
describe("what the × may remove on the Me board", () => {
  const me = "kvaps";
  const mine = { author: me, zone: "yellow" as ZoneKey };

  it("removes a card this person put on their own board", () => {
    expect(mayRemove(mine, me)).toBe(true);
  });

  it("leaves a card somebody else created", () => {
    expect(mayRemove({ ...mine, author: "lllamnyp" }, me)).toBe(false);
  });

  // The lead's plan is the other three zones, and a card of the person's OWN
  // making that has been planned into one of them is no longer only theirs:
  // somebody read it and gave it a place in the week. This is the case the
  // first rule missed — a lead saw an × on most of their own board, being the
  // author of most of what they are assigned.
  it("leaves a card of its own that the plan has taken up", () => {
    expect(mayRemove({ author: me, zone: "red" }, me)).toBe(false);
    expect(mayRemove({ author: me, zone: "gray" }, me)).toBe(false);
    expect(mayRemove({ author: me, zone: "green" }, me)).toBe(false);
  });

  it("leaves a card of its own that stands in no zone at all", () => {
    expect(mayRemove({ author: me }, me)).toBe(false);
  });

  it("leaves a card whose author nothing records", () => {
    // An old card, or one written straight into the repository: unattributed
    // work is not this person's to destroy.
    expect(mayRemove({ zone: "yellow" }, me)).toBe(false);
  });

  it("leaves everything when nobody is signed in", () => {
    expect(mayRemove(mine, undefined)).toBe(false);
    expect(mayRemove(mine, "")).toBe(false);
  });

  // A SUBTASK is a piece of the card it hangs under, not work assigned to
  // anyone: whoever can see the parent can add one and take it away again.
  // Its × is the parent's own gesture and this rule does not reach it.
  it("says nothing about subtasks — their × is the parent's", () => {
    expect(mayRemove({ author: "lllamnyp", parent: "p1" }, me)).toBe(true);
    expect(mayRemove({ author: "lllamnyp", parent: "p1", zone: "red" }, me)).toBe(true);
  });
});

// Dragging on the Me board reorders work inside a zone; it does not move a
// card between zones. The zone is the lead's statement of how the work was
// planned, and a person reordering their day must not rewrite it.
describe("what a drag may do on the Me board", () => {
  it("keeps a card in the zone it was dragged from", () => {
    expect(sortableWithin("yellow", "yellow")).toBe(true);
  });

  it("refuses a drop into another zone", () => {
    expect(sortableWithin("yellow", "red")).toBe(false);
    expect(sortableWithin("gray", "green")).toBe(false);
  });

  // GROUPING is the same crossing by another name: a card nested under a
  // parent in another zone renders under that parent, and the zone it was
  // planned in is gone. It was the door left open when only the plain drop
  // was guarded — the card could be carried anywhere by dropping it ON
  // something rather than beside it.
  it("answers for grouping too, which is the same crossing", () => {
    const grouping = (from: ZoneKey, to: ZoneKey) => sortableWithin(from, to);
    expect(grouping("yellow", "red")).toBe(false);
    expect(grouping("yellow", "yellow")).toBe(true);
  });
});

// The Me board's add form stands in the unplanned zone alone — that is what
// this board is FOR: something came up today, and the week's plan is made
// elsewhere. It is the board's own offer and nothing more: the Team and
// Triage grids are where planning is done, and they offer every zone in every
// column, one's own included. The server holds no rule about it — it did
// once, and because all three boards send the same create it refused a lead
// putting a card in their own column.
describe("what the Me board offers, and how far that reaches", () => {
  it("adds in the unplanned zone and no other", () => {
    const zones: ZoneKey[] = ["red", "yellow", "gray", "green"];
    expect(zones.filter(acceptsNewCard)).toEqual([ADD_ZONE]);
    expect(ADD_ZONE).toBe("yellow");
  });
});
