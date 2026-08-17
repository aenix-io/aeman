import { describe, expect, it } from "vitest";
import { snapProgress } from "./progress";

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
