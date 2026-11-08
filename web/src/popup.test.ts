import { describe, expect, it } from "vitest";

import { placePopup, type Rect } from "./popup";

const viewport = { width: 1280, height: 800 };
const rect = (left: number, top: number, width: number, height: number): Rect => ({
  left,
  top,
  right: left + width,
  bottom: top + height,
});

describe("placePopup", () => {
  it("opens below the control, aligned to its start, when there is room", () => {
    const p = placePopup(rect(500, 100, 120, 28), { width: 300, height: 200 }, viewport, { align: "start" });
    expect(p).toEqual({ top: 132, left: 500, maxHeight: 660, placement: "below" });
  });

  it("flips above when the room below is short and above is longer", () => {
    // The drawer's Depends on section low on a long card: 60px below.
    const p = placePopup(rect(500, 712, 120, 28), { width: 300, height: 300 }, viewport, { align: "start" });
    expect(p.placement).toBe("above");
    expect(p.top).toBe(712 - 4 - 300);
    expect(p.maxHeight).toBe(712 - 4 - 8);
  });

  it("stays below with a capped height when below still has more room than above", () => {
    const p = placePopup(rect(500, 300, 120, 28), { width: 300, height: 600 }, viewport, { align: "start" });
    expect(p.placement).toBe("below");
    expect(p.maxHeight).toBe(800 - 332 - 8);
    expect(p.top).toBe(332);
  });

  it("clamps the left edge so a popup near the right edge stays on screen", () => {
    const p = placePopup(rect(1200, 100, 60, 28), { width: 300, height: 100 }, viewport, { align: "start" });
    expect(p.left).toBe(1280 - 8 - 300);
  });

  it("aligns to the control's end by default, clamped at the left edge", () => {
    expect(placePopup(rect(600, 100, 100, 28), { width: 300, height: 100 }, viewport).left).toBe(400);
    expect(placePopup(rect(10, 100, 40, 28), { width: 300, height: 100 }, viewport).left).toBe(8);
  });

  it("keeps a start-aligned popup at its control near the left edge", () => {
    expect(placePopup(rect(20, 100, 120, 28), { width: 300, height: 100 }, viewport, { align: "start" }).left).toBe(20);
  });

  it("caps the height to a tiny viewport and lets the list scroll", () => {
    const p = placePopup(rect(10, 60, 100, 28), { width: 200, height: 400 }, { width: 320, height: 200 }, { align: "start" });
    expect(p.placement).toBe("below");
    expect(p.top).toBe(92);
    expect(p.maxHeight).toBe(200 - 92 - 8);
    expect(p.maxHeight).toBeGreaterThan(0);
  });

  it("keeps the popup inside its bounds, and pins one wider than them to their start", () => {
    const drawer = rect(700, 0, 520, 800);
    const inside = placePopup(rect(1150, 100, 60, 28), { width: 300, height: 100 }, viewport, { align: "start", bounds: drawer });
    expect(inside.left).toBe(1220 - 8 - 300);
    const wide = placePopup(rect(720, 100, 60, 28), { width: 900, height: 100 }, viewport, { align: "start", bounds: drawer });
    expect(wide.left).toBe(708);
  });
});
