import { describe, expect, it } from "vitest";
import type { Card } from "./providers/types";
import { needsTriage, placedIn, reachOf, weeksCovered } from "./triage";

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
