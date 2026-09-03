import { describe, expect, it } from "vitest";
import type { Card } from "./providers/types";
import {
  anchorFor,
  byPile,
  needsTriage,
  orderWith,
  pileRank,
  inWeek,
  placedAhead,
  placedIn,
  reachOf,
  weeksCovered,
} from "./triage";

const card = (over: Partial<Card> = {}): Card =>
  ({ itemId: "c1", title: "A card", assignees: [], ...over }) as Card;

describe("needsTriage", () => {
  it("wants a week for an open card of its own", () => {
    expect(needsTriage(card())).toBe(true);
  });

  it("asks nothing of a card that already has one", () => {
    expect(needsTriage(card({ week: "2026-08-31" }))).toBe(false);
  });

  it("asks nothing of a review — it follows the card it reviews", () => {
    // A review is not work anybody schedules: it lives and dies with its
    // original, so putting it in the strip only asked for a decision that
    // is not the reader's to make.
    expect(needsTriage(card({ reviewOf: "c9" }))).toBe(false);
  });

  it("asks nothing of a subtask — it follows its parent", () => {
    expect(needsTriage(card({ parent: "c9" }))).toBe(false);
  });

  it("asks nothing of a card on a personal board", () => {
    expect(needsTriage(card({ domain: "~kvaps" }))).toBe(false);
  });

  it("asks nothing of a card sent to review — it waits on a reviewer", () => {
    // Its work is done; a week is not what it is missing, and asking for one
    // asks the reader to decide something nobody is waiting on them for.
    expect(needsTriage(card({ stage: "review", progress: 85 }))).toBe(false);
  });

  it("still asks about a locked card — that is neither done nor in review", () => {
    expect(needsTriage(card({ stage: "locked" }))).toBe(true);
  });

  it("asks nothing of work already finished", () => {
    expect(needsTriage(card({ stage: "done" }))).toBe(false);
    expect(needsTriage(card({ progress: 100 }))).toBe(false);
  });

  it("still asks about a card that is merely under way", () => {
    expect(needsTriage(card({ progress: 40 }))).toBe(true);
  });

  it("does not take a day on the board for an answer", () => {
    // The day's planning put it there, not the week's: nobody has said how
    // long the work takes, which is the whole of the question.
    expect(needsTriage(card({ day: "2026-09-02" }))).toBe(true);
  });
});

describe("pileRank", () => {
  it("reads the zones the way the Team board does", () => {
    expect(pileRank(card({ zone: "red" }))).toBeLessThan(pileRank(card({ zone: "yellow" })));
    expect(pileRank(card({ zone: "yellow" }))).toBeLessThan(pileRank(card({ zone: "gray" })));
    expect(pileRank(card({ zone: "gray" }))).toBeLessThan(pileRank(card({ zone: "green" })));
  });

  it("puts a debt before everything, whatever kind of work it is", () => {
    // It was due and is not done; that outranks what sort of thing it is.
    expect(pileRank(card({ overdue: true, zone: "green" }))).toBeLessThan(
      pileRank(card({ zone: "red" })),
    );
  });

  it("puts the project's own work after the debts and before the zones", () => {
    const slot = card({ epic: "Storage" });
    expect(pileRank(slot)).toBeGreaterThan(pileRank(card({ overdue: true })));
    expect(pileRank(slot)).toBeLessThan(pileRank(card({ zone: "red" })));
  });

  it("leaves a card of no zone at the back", () => {
    expect(pileRank(card())).toBeGreaterThan(pileRank(card({ zone: "green" })));
  });

  it("sorts a cell without disturbing what stands equal", () => {
    // Two cards of one zone keep the order the board holds them in, so an
    // order somebody set by hand still means something among its peers.
    const pile = [
      card({ itemId: "green", zone: "green" }),
      card({ itemId: "planned-1", zone: "gray" }),
      card({ itemId: "late", overdue: true, zone: "green" }),
      card({ itemId: "planned-2", zone: "gray" }),
      card({ itemId: "urgent", zone: "red" }),
    ];
    expect([...pile].sort(byPile((c) => c)).map((c) => c.itemId)).toEqual([
      "late",
      "urgent",
      "planned-1",
      "planned-2",
      "green",
    ]);
  });
});

describe("orderWith", () => {
  const cell = ["a", "b", "c"];

  it("puts a newcomer where the pointer says", () => {
    expect(orderWith(cell, "x", 0)).toEqual(["x", "a", "b", "c"]);
    expect(orderWith(cell, "x", 2)).toEqual(["a", "b", "x", "c"]);
  });

  it("puts one dropped past the end at the end", () => {
    expect(orderWith(cell, "x", 9)).toEqual(["a", "b", "c", "x"]);
  });

  it("takes a card out before putting it back, so dragging DOWN lands where the pointer is", () => {
    // The off-by-one this exists for: "a" dragged to the third place must
    // end up third, not second — the reader is aiming at a list that no
    // longer holds the card they are carrying.
    expect(orderWith(cell, "a", 2)).toEqual(["b", "c", "a"]);
  });

  it("and dragging UP lands there too", () => {
    expect(orderWith(cell, "c", 0)).toEqual(["c", "a", "b"]);
  });

  it("leaves the order alone when the card is put back where it was", () => {
    expect(orderWith(cell, "b", 1)).toEqual(cell);
  });

  it("makes a list of one out of an empty cell", () => {
    expect(orderWith([], "x", 3)).toEqual(["x"]);
  });
});

describe("anchorFor", () => {
  const cell = ["a", "b", "c"];

  it("names the card before it", () => {
    expect(anchorFor(cell, "c")).toEqual({ after: "b" });
  });

  it("names the card now second when there is nothing before it", () => {
    // Nothing to sit after at the top, so the write says what it sits
    // before — the card that took the second place.
    expect(anchorFor(cell, "a")).toEqual({ before: "b" });
  });

  it("has nothing to say about a card alone in its cell", () => {
    expect(anchorFor(["a"], "a")).toBeNull();
  });

  it("has nothing to say about a card that is not there", () => {
    expect(anchorFor(cell, "x")).toBeNull();
  });
});

describe("placedIn", () => {
  it("is the card's week, and nothing else", () => {
    expect(placedIn(card({ week: "2026-08-31" }))).toBe("2026-08-31");
  });

  it("is nothing at all for a card nobody has dated", () => {
    expect(placedIn(card())).toBeNull();
  });
});

describe("weeksCovered", () => {
  it("is the one week for a card that was never stretched", () => {
    expect(weeksCovered(card({ week: "2026-08-31" }))).toEqual(["2026-08-31"]);
  });

  it("runs through the week the end date reaches", () => {
    expect(weeksCovered(card({ week: "2026-08-31", day: "2026-09-11" }))).toEqual([
      "2026-08-31",
      "2026-09-07",
    ]);
  });

  it("reaches nowhere on an end date inside its own week", () => {
    expect(weeksCovered(card({ week: "2026-08-31", day: "2026-09-04" }))).toEqual(["2026-08-31"]);
  });

  it("covers nothing at all without a week", () => {
    expect(weeksCovered(card({ day: "2026-09-11" }))).toEqual([]);
    expect(reachOf(card())).toBe("");
  });
});

// The week's own work — what its column holds on Triage, and what the Team
// board's grid carries all week so a card placed in a week is not invisible
// until somebody gives it a day. Mirrors board.InWeek; a rule kept on one
// side only is how the boards drifted before.
describe("the week's own work", () => {
  const TODAY = "2026-09-03"; // a Thursday
  const THIS = "2026-08-31";
  const NEXT = "2026-09-07";
  const LAST = "2026-08-24";

  it("is the card placed in that week", () => {
    expect(inWeek(card({ week: THIS }), THIS, TODAY)).toBe(true);
    expect(inWeek(card({ week: NEXT }), THIS, TODAY)).toBe(false);
  });

  it("is every week a stretched card covers", () => {
    const long = card({ week: THIS, day: "2026-09-11" });
    expect(inWeek(long, THIS, TODAY)).toBe(true);
    expect(inWeek(long, NEXT, TODAY)).toBe(true);
    expect(inWeek(long, LAST, TODAY)).toBe(false);
  });

  it("carries a DEBT into the current week, and nowhere else", () => {
    const debt = card({ week: LAST, overdue: true });
    expect(inWeek(debt, THIS, TODAY)).toBe(true);
    // Not onto some other week: a debt is settled where the work is now.
    expect(inWeek(debt, NEXT, TODAY)).toBe(false);
    // And it does not leave the week it was owed in.
    expect(inWeek(debt, LAST, TODAY)).toBe(true);
  });

  it("leaves a card of an earlier week that is NOT overdue where it was", () => {
    // Finished in its week, or otherwise not owed: nothing to carry forward.
    expect(inWeek(card({ week: LAST }), THIS, TODAY)).toBe(false);
  });

  it("is nothing at all for a card nobody placed", () => {
    expect(inWeek(card({ day: "2026-09-04" }), THIS, TODAY)).toBe(false);
  });
});

// A card placed in a week AHEAD is on no day board until its Monday: that is
// what makes the backlog a regulator rather than a list.
describe("placed ahead", () => {
  const TODAY = "2026-09-03";

  it("is a week after this one", () => {
    expect(placedAhead(card({ week: "2026-09-07" }), TODAY)).toBe(true);
  });

  it("is not this week, nor a week already past", () => {
    expect(placedAhead(card({ week: "2026-08-31" }), TODAY)).toBe(false);
    expect(placedAhead(card({ week: "2026-08-24" }), TODAY)).toBe(false);
  });

  it("is not a card with no week at all", () => {
    expect(placedAhead(card({}), TODAY)).toBe(false);
  });
});
