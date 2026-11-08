import { describe, expect, it } from "vitest";

import {
  COLUMN_DEFAULTS,
  COLUMN_MINS,
  ROADMAP_COLUMNS_KEY,
  columnVars,
  draggedWidths,
  resized,
  resetColumn,
  steppedWidth,
  storedColumns,
  tableWidth,
} from "./roadmapcolumns";

// The Roadmap's table columns are the reader's to size: every boundary in
// the header — Card | Owner | Start | End | Status, and Status | the
// timeline, which is the split between the table and the timeline — drags,
// double-click puts the column back, and the widths are remembered per
// viewer. Nothing collapses: each column has a floor.
describe("the Roadmap's column widths", () => {
  it("start at the defaults, the table as wide as they add up", () => {
    expect(storedColumns(null)).toEqual(COLUMN_DEFAULTS);
    expect(tableWidth(COLUMN_DEFAULTS)).toBe(712);
  });

  it("clamp a drag to the column's floor and to a ceiling", () => {
    expect(resized(COLUMN_DEFAULTS, "owner", 10).owner).toBe(COLUMN_MINS.owner);
    expect(resized(COLUMN_DEFAULTS, "title", 5000).title).toBe(1200);
    expect(resized(COLUMN_DEFAULTS, "start", 80.6).start).toBe(81);
    // The others stay as they were.
    expect(resized(COLUMN_DEFAULTS, "start", 80).owner).toBe(COLUMN_DEFAULTS.owner);
  });

  // The badge is the work status alone: its widest, "IN PROGRESS", plus the
  // cell's 6px + 12px of padding before the timeline fits well under the
  // floor, which keeps room to spare for a wider font; the default sits
  // above it.
  it("keep the Status column wide enough for the widest badge, by default and at the floor", () => {
    expect(COLUMN_MINS.status).toBeGreaterThanOrEqual(140);
    expect(COLUMN_DEFAULTS.status).toBeGreaterThanOrEqual(COLUMN_MINS.status);
  });

  it("clamp a narrower Status width a viewer stored before up to the new floor", () => {
    // 112 was the default of the single pill; 110 its floor.
    expect(storedColumns(JSON.stringify({ status: 112 })).status).toBe(COLUMN_MINS.status);
    expect(storedColumns(JSON.stringify({ status: 110, title: 300 }))).toEqual({ ...COLUMN_DEFAULTS, title: 300, status: COLUMN_MINS.status });
    expect(storedColumns(JSON.stringify({ status: 200 })).status).toBe(200);
  });

  it("read back what was stored, clamped, and nothing that is not theirs", () => {
    const raw = JSON.stringify({ title: 400, owner: 5, status: "wide", bogus: 3 });
    expect(storedColumns(raw)).toEqual({ ...COLUMN_DEFAULTS, title: 400, owner: COLUMN_MINS.owner });
    expect(storedColumns("not json")).toEqual(COLUMN_DEFAULTS);
    expect(ROADMAP_COLUMNS_KEY).toBe("aeman.strategy.columns");
  });

  it("put one column back on a double-click", () => {
    const wide = resized(COLUMN_DEFAULTS, "owner", 300);
    expect(resetColumn(wide, "owner")).toEqual(COLUMN_DEFAULTS);
  });

  it("step with the arrow keys, further with Shift", () => {
    expect(steppedWidth(COLUMN_DEFAULTS, "end", "ArrowRight", false)).toBe(72);
    expect(steppedWidth(COLUMN_DEFAULTS, "end", "ArrowLeft", true)).toBe(COLUMN_MINS.end);
    expect(steppedWidth(COLUMN_DEFAULTS, "end", "Enter", false)).toBeNull();
  });

  it("reach the rows as CSS variables, so a drag repaints without re-rendering them", () => {
    expect(columnVars(COLUMN_DEFAULTS)).toEqual({
      "--rm-title": "290px",
      "--rm-owner": "150px",
      "--rm-start": "64px",
      "--rm-end": "64px",
      "--rm-status": "144px",
      "--rm-table": "712px",
    });
  });
});

// A drag is read off where it started: the pointer's travel since the press,
// added to the width the column had then, clamped.
describe("a drag on a column handle", () => {
  it("adds the pointer's travel to the width it started from", () => {
    expect(draggedWidths(COLUMN_DEFAULTS, "owner", 150, 100, 160).owner).toBe(210);
    expect(draggedWidths(COLUMN_DEFAULTS, "owner", 150, 100, -500).owner).toBe(COLUMN_MINS.owner);
  });
});
