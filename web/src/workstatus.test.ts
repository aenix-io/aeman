import { describe, expect, it } from "vitest";
import { statusOf } from "./roadmap";
import { HIDE_DONE, statusSet, workStatus } from "./workstatus";

// The Roadmap's Status filter reads one work status per task, mirroring
// board.WorkStatus: done when complete; else backlog when parked or placed
// nowhere; else in progress — in hand by a date or a sprint, review, locked
// and refused included, since somebody holds them.
describe("a task's work status", () => {
  const today = "2026-11-08";
  it.each([
    ["finished", { progress: 100, startDate: today, sprintStart: today }, "done"],
    ["finished by its stage", { stage: "done" as const, parked: true }, "done"],
    ["parked", { parked: true }, "backlog"],
    ["placed nowhere", {}, "backlog"],
    ["dated", { startDate: today, day: today }, "in-progress"],
    ["in a sprint", { sprintStart: today }, "in-progress"],
    ["an end date alone", { day: today }, "in-progress"],
    ["on review", { stage: "review" as const, progress: 50, sprintStart: today }, "in-progress"],
    ["locked", { stage: "locked" as const, progress: 50, sprintStart: today }, "in-progress"],
    ["refused", { stage: "refuse" as const, sprintStart: today }, "in-progress"],
    // The filter follows PLACEMENT: parked is the backlog whatever the card
    // still carries — dates, a stage.
    ["parked with dates", { parked: true, startDate: today, day: today, sprintStart: today }, "backlog"],
    ["parked on review", { stage: "review" as const, progress: 50, parked: true }, "backlog"],
    ["parked and locked", { stage: "locked" as const, progress: 50, parked: true }, "backlog"],
    ["on review, placed nowhere", { stage: "review" as const, progress: 50 }, "backlog"],
    // Complete is the board's: a recurrent card at 100 is finished; a stage
    // that holds a card (review, locked) keeps it open at 100.
    ["recurrent at 100", { stage: "recurrent" as const, progress: 100, sprintStart: today }, "done"],
    ["on review at 100", { stage: "review" as const, progress: 100, sprintStart: today }, "in-progress"],
    ["locked at 100", { stage: "locked" as const, progress: 100, sprintStart: today }, "in-progress"],
  ])("%s", (_name, card, want) => {
    expect(workStatus(card)).toBe(want);
  });
});

// The Roadmap row's BADGE may name the stage a card is held in (review,
// locked, refused) where the filter reads placement; on everything else the
// badge and the filter say the same thing. A held card's filter status is
// its placement's: a parked card on review is in the backlog, badge "Review".
describe("the row badge against the filter", () => {
  const today = "2026-11-08";
  const cards = [
    { progress: 100 },
    { stage: "done" as const, parked: true },
    { parked: true },
    {},
    { startDate: today },
    { day: today },
    { sprintStart: today },
    { parked: true, startDate: today, sprintStart: today },
    { stage: "review" as const, progress: 50, sprintStart: today },
    { stage: "review" as const, progress: 50, parked: true },
    { stage: "review" as const, progress: 50 },
    { stage: "locked" as const, progress: 100, sprintStart: today },
    { stage: "refuse" as const, parked: true },
    { stage: "recurrent" as const, progress: 100 },
  ];
  it.each(cards.map((c) => [JSON.stringify(c), c] as const))("%s", (_name, c) => {
    const badge = statusOf({ type: undefined, hierarchy: undefined, stage: undefined, progress: 0, ...c });
    if (badge === "review" || badge === "locked" || badge === "refused") {
      expect(workStatus(c)).not.toBe("done");
    } else {
      expect(badge).toBe(workStatus(c));
    }
  });
  it("keeps the stage on a parked card's badge", () => {
    expect(statusOf({ type: undefined, hierarchy: undefined, stage: "review", progress: 50, parked: true })).toBe("review");
    expect(workStatus({ stage: "review", progress: 50, parked: true })).toBe("backlog");
  });
});

// "Hide done" is the owner's one-click ask: exactly In progress and Backlog.
describe("Hide done", () => {
  it("is every status but done", () => {
    expect([...HIDE_DONE].sort()).toEqual(["backlog", "in-progress"]);
  });
});

describe("the selector's status set", () => {
  it("is sorted and comma-joined, unknown values dropped", () => {
    expect(statusSet(["in-progress", "backlog"])).toBe("backlog,in-progress");
    expect(statusSet(["done", "nonsense"])).toBe("done");
    expect(statusSet([])).toBe("");
  });
});
