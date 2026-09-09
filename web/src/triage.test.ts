import { describe, expect, it } from "vitest";
import type { Card } from "./providers/types";
import {
  anchorFor,
  byPile,
  deferred,
  needsTriage,
  orderWith,
  pileRank,
  placedAhead,
  placedIn,
  reachOf,
  weeksCovered,
  broughtBack,
  ordersWithinCell,
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

  it("asks nothing of a parked card — the answer was 'not now'", () => {
    // The strip is the question "when?", and parking a card ANSWERS it: not
    // now. Leaving it in the strip would ask again every morning, which is
    // how an inbox stops meaning anything.
    expect(needsTriage(card({ parked: true }))).toBe(false);
  });

  it("asks again the moment a card comes off its shelf", () => {
    expect(needsTriage(card({ parked: false }))).toBe(true);
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

  // A process turn is a commitment made elsewhere too, and it will take the
  // week's time whether or not anybody plans around it — so it reads right
  // after the project's work and before anything a zone decides. Already
  // filed or only drawn ahead makes no difference to the week it costs.
  it("puts the processes' work right after the project's", () => {
    const turn = card({ task: "t1", zone: "green" });
    const ghost = { ...card({ zone: "green" }), projected: true };
    for (const c of [turn, ghost]) {
      expect(pileRank(c)).toBeGreaterThan(pileRank(card({ epic: "Storage" })));
      expect(pileRank(c)).toBeLessThan(pileRank(card({ zone: "red" })));
    }
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
  it("is nothing for a parked card, whatever week it carries", () => {
    // The two are exclusive and every door that gives a week takes the card
    // off its shelf — but the storage is a git repository anything may write
    // to, so a card CAN arrive carrying both. Drawn by its week it would
    // stand in the grid AND in the drawer: the same work twice, counted
    // twice against the week. Mirrors board.TriageWeekOf.
    expect(placedIn(card({ week: "2026-09-07", parked: true }))).toBeNull();
  });

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

// Deferring is the act of taking a card off the board until a later day, so
// its week must not keep drawing it today: a card pushed a month out went on
// standing in this week's grid, on a board the person had just cleared it
// from. Mirrors board.deferred.
describe("deferred", () => {
  it("says a card scheduled for a later day is off today's board", () => {
    expect(deferred({ startDate: "2026-10-03" }, "2026-09-03")).toBe(true);
  });

  it("says nothing of a card standing on today, or on a day gone by", () => {
    expect(deferred({ startDate: "2026-09-03" }, "2026-09-03")).toBe(false);
    expect(deferred({ startDate: "2026-08-20" }, "2026-09-03")).toBe(false);
  });

  it("says nothing of a card with no date at all — a week is not a deferral", () => {
    expect(deferred({}, "2026-09-03")).toBe(false);
  });
});

// A card whose days ran out is drawn only on the Triage grid, in the week it
// was owed in. Dragging it into the week the team is working is the gesture
// that says "this is being done now", so it has to come back with days, or the
// drag moves the column and nothing else — which is where nine process turns
// sat on the production board, three of them a month past their week. Mirrors
// boardservice.Place / daysRanOut.
describe("brought back into the week being worked", () => {
  const TODAY = "2026-09-09";
  const WEEK = "2026-09-07";

  it("re-dates a card whose days are all behind this week", () => {
    expect(
      broughtBack({ startDate: "2026-08-24", day: "2026-08-30" }, WEEK, TODAY),
    ).toEqual({ startDate: TODAY, day: "2026-09-13" });
  });

  it("starts it TODAY, not on the week's Monday — a start before the sprint began joins the previous one", () => {
    const got = broughtBack({ startDate: "2026-08-24", day: "2026-08-30" }, WEEK, TODAY);
    expect(got?.startDate).toBe(TODAY);
    expect(got?.startDate).not.toBe(WEEK);
  });

  it("leaves a range that still reaches this week alone", () => {
    expect(
      broughtBack({ startDate: "2026-08-31", day: "2026-09-09" }, WEEK, TODAY),
    ).toBeNull();
    // the boundary: a card whose last day IS the week's Monday still stands
    expect(
      broughtBack({ startDate: "2026-08-31", day: WEEK }, WEEK, TODAY),
    ).toBeNull();
  });

  it("leaves a card with no days at all — that one joins the sprint instead", () => {
    expect(broughtBack({}, WEEK, TODAY)).toBeNull();
  });

  it("never re-dates a project slot: its dates are its row", () => {
    expect(
      broughtBack({ startDate: "2026-08-24", day: "2026-08-30", epic: "Auth" }, WEEK, TODAY),
    ).toBeNull();
  });

  it("says nothing about a week that is not the one being worked", () => {
    expect(
      broughtBack({ startDate: "2026-08-24", day: "2026-08-30" }, "2026-08-31", TODAY),
    ).toBeNull();
  });
});

// Mirrors board.TestAReviewStandsInTheWeekItsDatesFallIn. A review has no
// week of its own — the week belongs to the card it reviews — but it is work
// in the reviewer's hands and counts in their load, and drawn by nothing it
// stood on no board at all once its dates ran out.
describe("where a review card stands", () => {
  it("stands in the week its own dates fall in", () => {
    expect(placedIn({ reviewOf: "orig", startDate: "2026-07-23", day: "2026-07-23" } as Card)).toBe(
      "2026-07-20",
    );
    expect(placedIn({ reviewOf: "orig", day: "2026-09-09" } as Card)).toBe("2026-09-07");
  });

  it("keeps a week of its own, and invents none from nothing", () => {
    expect(placedIn({ reviewOf: "orig", week: "2026-09-07", startDate: "2026-07-23" } as Card)).toBe(
      "2026-09-07",
    );
    expect(placedIn({ reviewOf: "orig" } as Card)).toBeNull();
  });

  it("leaves an ordinary dated card in no column: dates are not a week", () => {
    expect(placedIn({ startDate: "2026-07-23", day: "2026-07-23" } as Card)).toBeNull();
  });
});

// A review is not one of the boxes stacked in a cell — it is folded into the
// line at the cell's foot — so it takes no part in that cell's manual order.
// Counting it made the list of ids and the boxes on screen disagree, and a
// drop then wrote "before" a card the reader could not see.
describe("what takes part in a cell's order", () => {
  it("leaves review cards out, and everything else in", () => {
    expect(ordersWithinCell({ reviewOf: "orig" } as Card)).toBe(false);
    expect(ordersWithinCell({} as Card)).toBe(true);
    expect(ordersWithinCell({ reviewOf: undefined } as Card)).toBe(true);
  });
});

// The reading order of a week's pile is about DEBTS — a card whose day has
// passed and is still open — while the late MARK is about a promise made on
// another board (board.Owed vs board.Overdue). They part company on the
// ordinary card: it is a debt in the current week's column and it is not
// painted late, so the pile must not read the mark to find it.
describe("a debt reads first, whether or not it is marked late", () => {
  const THIS = "2026-09-07";
  it("puts a card owed in an earlier week at the top", () => {
    const debt = card({ week: "2026-08-31", zone: "green" });
    expect(pileRank(debt, THIS)).toBe(0);
    // Without a week to compare against, it is just its zone again.
    expect(pileRank(debt)).toBeGreaterThan(0);
  });

  it("leaves this week's own work in its zone order", () => {
    expect(pileRank(card({ week: THIS, zone: "red" }), THIS)).toBeGreaterThan(0);
    expect(pileRank(card({ week: "2026-09-14", zone: "red" }), THIS)).toBeGreaterThan(0);
  });

  it("still puts a card the server marked late first", () => {
    expect(pileRank(card({ overdue: true, week: THIS, zone: "green" }), THIS)).toBe(0);
  });
});
