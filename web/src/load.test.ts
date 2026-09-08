import { describe, expect, it } from "vitest";
import { loadLabel, loadState, plannable, weekPoints } from "./load";

// The number beside a person on a sync: what they carry against what they
// get through, and red only when the plan does not fit. Mirrors the server's
// load and capacity (board.LoadNow, board.CapacityOfPerson).
describe("loadState", () => {
  it("is over when the load passes the capacity — the red case", () => {
    expect(loadState(60, 40)).toBe("over");
    expect(loadState(41, 40)).toBe("over");
  });

  it("is full exactly at capacity, and ok below it", () => {
    expect(loadState(40, 40)).toBe("full");
    expect(loadState(21, 40)).toBe("ok");
    expect(loadState(0, 40)).toBe("ok");
  });

  it("is unknown without a capacity, however heavy the load", () => {
    expect(loadState(60, 0)).toBe("unknown");
    expect(loadState(60, undefined)).toBe("unknown");
  });
});

describe("loadLabel", () => {
  it("reads as load over capacity", () => {
    expect(loadLabel(21, 40)).toBe("21/40");
  });

  it("shows the load alone when nothing is known to measure it against", () => {
    expect(loadLabel(21, 0)).toBe("21");
    expect(loadLabel(0, undefined)).toBe("0");
  });
});

// What a team can plan for a week: capacity less the reactive share.
// Mirrors board.Plannable.
describe("plannable", () => {
  it("leaves the capacity less the reactive share", () => {
    expect(plannable(100, 40, true)).toBe(60);
    expect(plannable(35, 30, true)).toBe(24);
  });

  it("leaves the whole capacity when no share is known", () => {
    expect(plannable(100, 40, false)).toBe(100);
  });

  it("leaves nothing of nothing", () => {
    expect(plannable(0, 40, true)).toBe(0);
  });
});

// What a week carries in points for the teams on screen: open cards in the
// week, a card of several weeks in each, the strip in the first row, and
// nothing that is done, parked, a subtask or another team's.
describe("weekPoints", () => {
  const cards = [
    { itemId: "a", title: "", assignees: [], team: "portal", week: "2026-09-07", size: "L" as const },
    { itemId: "b", title: "", assignees: [], team: "portal", week: "2026-09-14", size: "M" as const },
    // two weeks long: counts in both
    { itemId: "c", title: "", assignees: [], team: "portal", week: "2026-09-07", day: "2026-09-18", size: "XL" as const },
    // the strip: no week, stands in the first row
    { itemId: "d", title: "", assignees: [], team: "portal", size: "S" as const },
    // done: not carried
    { itemId: "e", title: "", assignees: [], team: "portal", week: "2026-09-07", size: "XL" as const, progress: 100 },
    // parked: on no week
    { itemId: "f", title: "", assignees: [], team: "portal", parked: true, size: "XL" as const },
    // another team
    { itemId: "g", title: "", assignees: [], team: "sales", week: "2026-09-07", size: "XL" as const },
    // a subtask rides its parent
    { itemId: "h", title: "", assignees: [], team: "portal", parent: "a", size: "M" as const },
  ];

  it("weighs each week for the teams on screen", () => {
    const m = weekPoints(cards, ["portal"], "2026-09-07");
    // a's weight is its child's (2), c is 8 in both weeks, d is 1 in the first row
    expect(m.get("2026-09-07")).toBe(2 + 8 + 1);
    expect(m.get("2026-09-14")).toBe(2 + 8);
  });

  it("counts another team only when it is on screen", () => {
    expect(weekPoints(cards, ["portal", "sales"], "2026-09-07").get("2026-09-07")).toBe(2 + 8 + 1 + 8);
  });
});
