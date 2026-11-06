import { describe, expect, it } from "vitest";

import {
  COLUMN_DEFAULTS,
  COLUMN_MINS,
  ROADMAP_COLUMNS_KEY,
  columnVars,
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
    expect(tableWidth(COLUMN_DEFAULTS)).toBe(680);
  });

  it("clamp a drag to the column's floor and to a ceiling", () => {
    expect(resized(COLUMN_DEFAULTS, "owner", 10).owner).toBe(COLUMN_MINS.owner);
    expect(resized(COLUMN_DEFAULTS, "title", 5000).title).toBe(1200);
    expect(resized(COLUMN_DEFAULTS, "start", 80.6).start).toBe(81);
    // The others stay as they were.
    expect(resized(COLUMN_DEFAULTS, "start", 80).owner).toBe(COLUMN_DEFAULTS.owner);
  });

  it("read back what was stored, clamped, and nothing that is not theirs", () => {
    const raw = JSON.stringify({ title: 400, owner: 5, status: "wide", bogus: 3 });
    expect(storedColumns(raw)).toEqual({ ...COLUMN_DEFAULTS, title: 400, owner: COLUMN_MINS.owner });
    expect(storedColumns("not json")).toEqual(COLUMN_DEFAULTS);
    expect(ROADMAP_COLUMNS_KEY).toBe("aeman.roadmap.columns");
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
      "--rm-status": "112px",
      "--rm-table": "680px",
    });
  });
});
