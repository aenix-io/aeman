import { describe, expect, it } from "vitest";
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
  ])("%s", (_name, card, want) => {
    expect(workStatus(card)).toBe(want);
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
