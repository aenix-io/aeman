import { describe, expect, it } from "vitest";
import { subtaskShows } from "./subtasks";

const base = { team: "portal", parent: "p1" };

describe("subtaskShows", () => {
  it("shows a subtask of the day's own sprint", () => {
    expect(subtaskShows({ ...base, sprintStart: "2026-08-26" },
      { today: "2026-08-26", day: "2026-08-26" })).toBe(true);
  });

  it("hides one deferred to a later day until that day arrives", () => {
    const c = { ...base, startDate: "2026-08-28", sprintStart: "2026-08-26" };
    expect(subtaskShows(c, { today: "2026-08-26", day: "2026-08-26" })).toBe(false);
    expect(subtaskShows(c, { today: "2026-08-26", day: "2026-08-28" })).toBe(true);
  });

  // The bug: a subtask FINISHED in an earlier sprint stays in that sprint by
  // design, while its parent carries on. Hiding it left the parent with no
  // children and no expand arrow — on a progress bar derived from exactly
  // those children. The person who inherited the card saw 90% and no way to
  // learn why.
  it("shows a subtask left behind in an earlier sprint — the parent's bar is made of it", () => {
    expect(subtaskShows({ ...base, sprintStart: "2026-08-25", progress: 100 },
      { today: "2026-08-26", day: "2026-08-26" })).toBe(true);
  });

  it("shows an OPEN subtask left behind too — it is still the group's work", () => {
    expect(subtaskShows({ ...base, sprintStart: "2026-08-25", progress: 40 },
      { today: "2026-08-26", day: "2026-08-26" })).toBe(true);
  });

  // Rolling the board back to an earlier day keeps the old behaviour: a
  // subtask that had not joined a sprint yet does not appear before its time.
  it("hides a subtask whose sprint had not started on the viewed day", () => {
    expect(subtaskShows({ ...base, sprintStart: "2026-08-26" },
      { today: "2026-08-26", day: "2026-08-25" })).toBe(false);
  });

  it("shows one with no sprint of its own", () => {
    expect(subtaskShows({ ...base }, { today: "2026-08-26", day: "2026-08-26" })).toBe(true);
  });
});
