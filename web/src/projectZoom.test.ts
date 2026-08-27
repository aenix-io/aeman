import { describe, expect, it } from "vitest";
import {
  MAX_ZOOM,
  MIN_ZOOM,
  SNAP,
  clampZoom,
  columnFactor,
  wheelZoom,
} from "./projectZoom";

describe("clampZoom", () => {
  it("keeps a plain value", () => expect(clampZoom(1.4)).toBeCloseTo(1.4));
  it("stops at the ends", () => {
    expect(clampZoom(99)).toBe(MAX_ZOOM);
    expect(clampZoom(0.01)).toBe(MIN_ZOOM);
  });
});

describe("wheelZoom", () => {
  // Ctrl/Cmd+wheel moves BOTH axes by the same step from where they are, so
  // the two sliders keep their relative offset instead of collapsing together.
  it("moves both axes by the same step", () => {
    const z = wheelZoom({ x: 1, y: 1.5 }, -100);
    expect(z.x - 1).toBeCloseTo(z.y - 1.5, 5);
  });
  it("scrolling up zooms in, down zooms out", () => {
    expect(wheelZoom({ x: 1, y: 1 }, -100).x).toBeGreaterThan(1);
    expect(wheelZoom({ x: 1, y: 1 }, 100).x).toBeLessThan(1);
  });
  it("an axis already at the end does not drag the other past its own", () => {
    const z = wheelZoom({ x: MAX_ZOOM, y: 1 }, -100);
    expect(z.x).toBe(MAX_ZOOM);
    expect(z.y).toBeGreaterThan(1);
  });
});

describe("columnFactor", () => {
  // A dragged column remembers its size RELATIVE to the others, so zooming
  // keeps the relation and switching projects keeps the column.
  it("is the dragged width over the shared width", () => {
    expect(columnFactor(280, 140)).toBeCloseTo(2);
  });
  it("snaps back to the default near the others' width", () => {
    expect(columnFactor(144, 140)).toBeNull();
    expect(columnFactor(140 * (1 + SNAP / 2), 140)).toBeNull();
  });
  it("keeps a width that is clearly its own", () => {
    expect(columnFactor(140 * 1.5, 140)).toBeCloseTo(1.5);
  });
  it("never returns a factor below the floor", () => {
    const f = columnFactor(10, 140);
    expect(f).not.toBeNull();
    expect(140 * (f as number)).toBeGreaterThanOrEqual(60);
  });
});
