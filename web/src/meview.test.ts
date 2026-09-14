import { describe, expect, it } from "vitest";

import { onMyDay } from "./meview";
import type { Board, Card } from "./providers/types";

const card = (over: Partial<Card>): Card =>
  ({ itemId: "c", title: "a card", assignees: ["u"], progress: 0, team: "alpha", ...over }) as Card;

// The team's sprint began on the Monday of the week under test.
const board = { sprintStates: { alpha: { current: "2026-09-14", previous: "2026-09-07" } } } as unknown as Board;

const TODAY = "2026-09-16"; // a Wednesday; its week begins 2026-09-14
const THIS_WEEK = "2026-09-14";
const NEXT_WEEK = "2026-09-21";
const LAST_WEEK = "2026-09-07";

// The Me board's rule for a day, card by card — the mirror of board.MeView,
// against the cases TestTheMeBoardDrawsAWeekThatHasCome pins. The two answers
// must agree before the server has spoken, or the board draws a card the next
// frame takes away (or never draws one the server sent).
describe("a card planned for a week, on its owner's board", () => {
  // Dropped into a week ahead on the Triage board it lost its dates and its
  // sprint; when that Monday came it had a week and nothing else, and the Me
  // board drew nothing — while the Team board drew it in the owner's column.
  it("is there once its week has come", () => {
    expect(onMyDay(card({ week: THIS_WEEK }), board, TODAY, TODAY)).toBe(true);
  });

  it("stays while it is owed, when its week has gone by", () => {
    expect(onMyDay(card({ week: LAST_WEEK }), board, TODAY, TODAY)).toBe(true);
  });

  // Not before its Monday, on the day being looked at: flipping forward to
  // next week is looking at next week's plan, and the card belongs to it.
  it("is off every day before its Monday, and on the days of its week", () => {
    const ahead = card({ week: NEXT_WEEK });
    expect(onMyDay(ahead, board, TODAY, TODAY)).toBe(false);
    expect(onMyDay(ahead, board, "2026-09-20", TODAY)).toBe(false);
    expect(onMyDay(ahead, board, NEXT_WEEK, TODAY)).toBe(true);
  });

  // Nothing sweeps a card with no sprint, so a finished one would stand on
  // every day for ever: it belongs to the day it was finished on.
  it("belongs, finished, to the day it was finished on and no other", () => {
    const done = card({ week: THIS_WEEK, progress: 100, doneAt: TODAY });
    expect(onMyDay(done, board, TODAY, TODAY)).toBe(true);
    expect(onMyDay(done, board, NEXT_WEEK, TODAY)).toBe(false);
  });

  it("leaves the strip and the shelf alone", () => {
    expect(onMyDay(card({}), board, TODAY, TODAY)).toBe(false);
    expect(onMyDay(card({ week: THIS_WEEK, parked: true }), board, TODAY, TODAY)).toBe(false);
  });
});

// The rule it sits beside is unchanged, and pinned here because it now lives
// in this file rather than inside the component.
describe("the rest of a day on the Me board", () => {
  it("draws the sprint's work once its day has come", () => {
    expect(onMyDay(card({ sprintStart: THIS_WEEK, startDate: THIS_WEEK }), board, TODAY, TODAY)).toBe(
      true,
    );
    expect(onMyDay(card({ sprintStart: THIS_WEEK, startDate: "2026-09-17" }), board, TODAY, TODAY)).toBe(
      false,
    );
  });

  it("shows a card deferred ahead from its day on", () => {
    const later = card({ sprintStart: THIS_WEEK, startDate: "2026-09-18" });
    expect(onMyDay(later, board, TODAY, TODAY)).toBe(false);
    expect(onMyDay(later, board, "2026-09-18", TODAY)).toBe(true);
  });

  it("spans a range across its days", () => {
    const range = card({ startDate: LAST_WEEK, day: "2026-09-15" });
    expect(onMyDay(range, board, "2026-09-15", TODAY)).toBe(true);
  });

  it("leaves a subtask to its parent", () => {
    expect(onMyDay(card({ parent: "p", week: THIS_WEEK }), board, TODAY, TODAY)).toBe(false);
  });
});
