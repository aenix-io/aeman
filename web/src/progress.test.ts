import { describe, expect, it } from "vitest";
import { snapProgress, stepProgress } from "./progress";

describe("snapProgress", () => {
  it("snaps to the 10% grid", () => {
    expect(snapProgress(0, false)).toBe(0);
    expect(snapProgress(0.04, false)).toBe(0);
    expect(snapProgress(0.06, false)).toBe(10);
    expect(snapProgress(0.63, false)).toBe(60);
    expect(snapProgress(1, false)).toBe(100);
  });

  it("clamps a pointer dragged past either end", () => {
    expect(snapProgress(-0.4, false)).toBe(0);
    expect(snapProgress(1.7, false)).toBe(100);
  });

  it("keeps a review/locked card inside 10–90", () => {
    expect(snapProgress(0, true)).toBe(10);
    expect(snapProgress(1, true)).toBe(90);
    expect(snapProgress(0.5, true)).toBe(50);
  });
});

// The drawer's bar moves by the keyboard too (#289): an arrow steps it by
// 10 on the same grid the handle snaps to, Home and End go to its ends, and
// a review/locked card's reach is 10–90 as the handle's is.
describe("a keyboard step on the progress bar", () => {
  it("steps by 10 on the grid, up and down", () => {
    expect(stepProgress(40, "ArrowRight", false)).toBe(50);
    expect(stepProgress(40, "ArrowUp", false)).toBe(50);
    expect(stepProgress(40, "ArrowLeft", false)).toBe(30);
    expect(stepProgress(40, "ArrowDown", false)).toBe(30);
    expect(stepProgress(45, "ArrowRight", false)).toBe(60);
  });

  it("stops at the ends, a clamped card's at 10 and 90", () => {
    expect(stepProgress(100, "ArrowRight", false)).toBe(100);
    expect(stepProgress(0, "ArrowLeft", false)).toBe(0);
    expect(stepProgress(90, "ArrowRight", true)).toBe(90);
    expect(stepProgress(10, "ArrowLeft", true)).toBe(10);
    expect(stepProgress(40, "Home", false)).toBe(0);
    expect(stepProgress(40, "End", true)).toBe(90);
  });

  it("is no step for any other key", () => {
    expect(stepProgress(40, "Enter", false)).toBeNull();
  });
});
