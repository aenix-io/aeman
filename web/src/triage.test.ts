import { describe, expect, it } from "vitest";
import type { Card } from "./providers/types";
import { anchorFor, needsTriage, orderWith, placedIn, reachOf, weeksCovered } from "./triage";

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
