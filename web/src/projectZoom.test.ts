import { describe, expect, it } from "vitest";
import {
  MAX_ZOOM,
  MIN_ZOOM,
  SNAP,
  clampZoom,
  columnFactor,
  wheelZoom,
  anchoredScroll,
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

// Zooming around the cursor: the point of the board under the pointer must
// still be under it afterwards, or the board slides out from under the hand.
// Only the cells scale — the date column on the left and the header row on
// top keep their size — so the fixed part is subtracted before scaling and
// added back after.
describe("anchoredScroll", () => {
  it("keeps the point under the cursor still", () => {
    // 200px into the content, gutter 54, scaled 2x: that point moves to
    // 54 + (200-54)*2 = 346, so the scroll must grow by the same 146.
    expect(anchoredScroll(0, 200, 54, 2)).toBeCloseTo(146);
  });

  it("does not move when the zoom does not change", () => {
    expect(anchoredScroll(120, 300, 54, 1)).toBeCloseTo(120);
  });

  it("leaves a cursor over the gutter alone", () => {
    expect(anchoredScroll(0, 30, 54, 2)).toBe(0);
  });

  it("never scrolls to a negative offset", () => {
    expect(anchoredScroll(0, 60, 54, 0.5)).toBeGreaterThanOrEqual(0);
  });

  it("works from a board already scrolled", () => {
    // content point = 500 + 100 = 600; scaled: 54 + (600-54)*1.5 = 873;
    // the cursor stays 100 from the edge, so the scroll is 773.
    expect(anchoredScroll(500, 100, 54, 1.5)).toBeCloseTo(773);
  });
});
