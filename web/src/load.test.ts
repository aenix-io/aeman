import { describe, expect, it } from "vitest";
import { loadLabel, loadState, plannable } from "./load";

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
