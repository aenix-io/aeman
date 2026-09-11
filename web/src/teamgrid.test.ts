import { describe, expect, it } from "vitest";

import { inHandOn, inSprintOn } from "./teamgrid";
import type { Card } from "./providers/types";

const card = (over: Partial<Card>): Card =>
  ({ itemId: "c", title: "a card", assignees: [], progress: 0, ...over }) as Card;

// The Team board's rule, card by card, against the same cases pkg/board pins
// in TestTeamGrid: the two answers must agree before the server has spoken,
// or the optimistic board draws a card the next frame takes away.
describe("inHandOn", () => {
  const DAY = "2026-06-23";
  const TODAY = "2026-06-23";

  it("holds open work whose day has come", () => {
    expect(inHandOn(card({ startDate: "2026-06-01" }), DAY, TODAY)).toBe(true);
  });

  it("holds a debt: a week gone by, still open", () => {
    const debt = card({ week: "2026-06-08", startDate: "2026-06-08" });
    expect(inHandOn(debt, DAY, TODAY)).toBe(true);
  });

  it("leaves a subtask to its parent, and a parked card to the shelf", () => {
    expect(inHandOn(card({ parent: "p", startDate: "2026-06-01" }), DAY, TODAY)).toBe(false);
    expect(inHandOn(card({ startDate: "2026-06-01", parked: true }), DAY, TODAY)).toBe(false);
  });

  it("never draws the Triage strip: nobody has said when", () => {
    expect(inHandOn(card({}), DAY, TODAY)).toBe(false);
  });

  it("keeps a week ahead off the board until its Monday", () => {
    const ahead = card({ week: "2026-07-06" });
    expect(inHandOn(ahead, "2026-07-05", TODAY)).toBe(false);
    expect(inHandOn(ahead, "2026-07-06", TODAY)).toBe(true);
  });

  it("gives finished work to the day it recorded, and to no other", () => {
    const done = card({ startDate: "2026-06-01", progress: 100, doneAt: "2026-06-22" });
    expect(inHandOn(done, "2026-06-22", TODAY)).toBe(true);
    expect(inHandOn(done, DAY, TODAY)).toBe(false);
    // Nothing is guessed out of the card's dates when it recorded nothing.
    const silent = card({ startDate: "2026-06-01", day: "2026-06-23", progress: 100 });
    expect(inHandOn(silent, DAY, TODAY)).toBe(false);
  });

  it("gives a sprint's own day the whole sprint, closed work included", () => {
    // Read from a day after the work was closed, as anybody actually would:
    // a card is never finished on a day still to come.
    const now = "2026-07-25";
    const late = card({
      startDate: "2026-06-01",
      sprintStart: "2026-06-15",
      progress: 100,
      doneAt: "2026-07-20",
    });
    // Its sprint's day holds it — that is the day the lead reads the sprint on
    // — and so does the day it was actually closed. No other day does.
    expect(inHandOn(late, "2026-06-15", now)).toBe(true);
    expect(inHandOn(late, "2026-07-20", now)).toBe(true);
    expect(inHandOn(late, "2026-06-16", now)).toBe(false);
  });

  it("lets a deferred card out of the sprint's day at once", () => {
    // Sent to tomorrow: gone from today's sprint, arriving tomorrow. Sent
    // three days out: the sprint that opens tomorrow starts without it.
    const now = "2026-09-09";
    const opened = "2026-09-07";
    const soon = card({ sprintStart: opened, startDate: "2026-09-10", day: "2026-09-10" });
    const later = card({ sprintStart: opened, startDate: "2026-09-12", day: "2026-09-12" });
    expect(inHandOn(soon, opened, now)).toBe(false);
    expect(inHandOn(later, opened, now)).toBe(false);
    expect(inHandOn(soon, "2026-09-10", now)).toBe(true);
    expect(inHandOn(later, "2026-09-10", now)).toBe(false);
    expect(inHandOn(later, "2026-09-12", now)).toBe(true);
  });

  it("holds a card put off until the day it was put off to", () => {
    const later = card({ startDate: "2026-07-02", sprintStart: "2026-06-22" });
    expect(inHandOn(later, DAY, TODAY)).toBe(false);
    expect(inHandOn(later, "2026-07-02", "2026-07-02")).toBe(true);
  });

  it("shows a sprint's day the work created INSIDE the sprint", () => {
    // Typed on the Tuesday of a sprint that opened on the Monday: the sprint's
    // own day must hold it, or a sprint read on Wednesday shows almost none of
    // its work.
    const midSprint = card({ sprintStart: "2026-06-22", startDate: "2026-06-23" });
    expect(inHandOn(midSprint, "2026-06-22", "2026-06-24")).toBe(true);
    expect(inHandOn(midSprint, "2026-06-24", "2026-06-24")).toBe(true);
  });

  it("takes a card deferred past today out of the sprint's day at once", () => {
    const putOff = card({ sprintStart: "2026-06-22", startDate: "2026-07-30" });
    expect(inHandOn(putOff, "2026-06-22", TODAY)).toBe(false);
  });
});

// Looking FORWARD the board answers a different question: today is what is in
// hand, a day still to come is a plan. Mirrors board.plannedFor and the case
// pkg/board pins in TestATomorrowIsAPlanAndTodayIsAState.
describe("inHandOn, looking ahead", () => {
  const TODAY = "2026-09-09"; // a Wednesday
  const TOMORROW = "2026-09-10";
  const NEXT_MONDAY = "2026-09-14";

  it("keeps work that ran over on TODAY", () => {
    const ranOver = card({ startDate: "2026-09-04", day: "2026-09-04" });
    expect(inHandOn(ranOver, TODAY, TODAY)).toBe(true);
    expect(inHandOn(ranOver, TOMORROW, TODAY)).toBe(false);
  });

  it("leaves a debt of a week gone by on today, and off tomorrow", () => {
    const debt = card({ week: "2026-08-31" });
    expect(inHandOn(debt, TODAY, TODAY)).toBe(true);
    expect(inHandOn(debt, TOMORROW, TODAY)).toBe(false);
  });

  it("holds what reaches the day ahead", () => {
    expect(inHandOn(card({ startDate: TODAY, day: TOMORROW }), TOMORROW, TODAY)).toBe(true);
    expect(inHandOn(card({ startDate: TOMORROW, day: TOMORROW }), TOMORROW, TODAY)).toBe(true);
  });

  it("holds a card on the days of the week it was placed in, and no other", () => {
    const ahead = card({ week: NEXT_MONDAY });
    expect(inHandOn(ahead, NEXT_MONDAY, TODAY)).toBe(true);
    expect(inHandOn(ahead, "2026-09-16", TODAY)).toBe(true);
    expect(inHandOn(ahead, "2026-09-21", TODAY)).toBe(false);
    // And this week's card does not reach into next week.
    expect(inHandOn(card({ week: "2026-09-07" }), NEXT_MONDAY, TODAY)).toBe(false);
  });
});

// The sprint's own day keeps the work it finished — and only that day does.
// Every other day of the sprint answers "what is in hand". Mirrors
// board.inSprintOn.
describe("the sprint's own day keeps the work it finished", () => {
  const TODAY = "2026-09-11";
  const OPENED = "2026-09-09";

  it("holds the sprint's finished work on the day it began", () => {
    const done = card({ sprintStart: OPENED, progress: 100, doneAt: "2026-09-10" });
    expect(inHandOn(done, OPENED, TODAY)).toBe(true);
  });

  it("does not drag it onto the other days of the sprint", () => {
    const done = card({ sprintStart: OPENED, progress: 100, doneAt: "2026-09-10" });
    expect(inHandOn(done, TODAY, TODAY)).toBe(false);
    // The day it was finished on still keeps it, as every finished card.
    expect(inHandOn(done, "2026-09-10", TODAY)).toBe(true);
  });

  it("answers for one sprint only", () => {
    expect(inSprintOn(card({ sprintStart: OPENED }), OPENED)).toBe(true);
    expect(inSprintOn(card({ sprintStart: OPENED }), TODAY)).toBe(false);
    expect(inSprintOn(card({}), OPENED)).toBe(false);
  });
});
