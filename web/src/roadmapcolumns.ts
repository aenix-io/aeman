// The Roadmap's table columns, sized by the reader: a drag on a boundary in
// the header, a double-click to put a column back, arrow keys on a focused
// handle. The last boundary — Status | the timeline — is the split between
// the table and the timeline, since the table is as wide as its columns add
// up. Remembered per viewer; applied as CSS variables on the board, so a
// drag repaints the rows without re-rendering them.

export type RoadmapColumn = "title" | "owner" | "start" | "end" | "status";

export const ROADMAP_COLUMNS: readonly RoadmapColumn[] = ["title", "owner", "start", "end", "status"];

export type ColumnWidths = Record<RoadmapColumn, number>;

/** COLUMN_DEFAULTS are the widths the board opens with: a 712px table. */
export const COLUMN_DEFAULTS: ColumnWidths = { title: 290, owner: 150, start: 64, end: 64, status: 144 };

/** COLUMN_MINS are the floors: no column collapses, and Status keeps room
 *  for its widest badge — the two-part "P0 | IN PROGRESS" (K15), 116px in
 *  the browser, plus the cell's padding before the timeline — with some to
 *  spare for a wider font. A width stored before (the single pill's 112) is
 *  clamped up to it on reading. */
export const COLUMN_MINS: ColumnWidths = { title: 120, owner: 60, start: 48, end: 48, status: 140 };

/** COLUMN_MAX is the ceiling of any column. */
export const COLUMN_MAX = 1200;
const STEP = 8;
const BIG_STEP = 32;

export const ROADMAP_COLUMNS_KEY = "aeman.roadmap.columns";

function clamp(col: RoadmapColumn, w: number): number {
  return Math.min(COLUMN_MAX, Math.max(COLUMN_MINS[col], Math.round(w)));
}

/** resized is the widths with one column dragged to `w`, clamped. */
export function resized(widths: ColumnWidths, col: RoadmapColumn, w: number): ColumnWidths {
  return { ...widths, [col]: clamp(col, w) };
}

/** draggedWidths is the widths during a drag on `col`'s handle: the width
 *  it had at the press plus the pointer's travel since, clamped. */
export function draggedWidths(widths: ColumnWidths, col: RoadmapColumn, startW: number, startX: number, x: number): ColumnWidths {
  return resized(widths, col, startW + x - startX);
}

/** resetColumn puts one column back to its default. */
export function resetColumn(widths: ColumnWidths, col: RoadmapColumn): ColumnWidths {
  return { ...widths, [col]: COLUMN_DEFAULTS[col] };
}

/** steppedWidth is a column's width after an arrow key on its handle, or
 *  null for any other key. */
export function steppedWidth(widths: ColumnWidths, col: RoadmapColumn, key: string, big: boolean): number | null {
  const step = big ? BIG_STEP : STEP;
  if (key === "ArrowRight") {
    return clamp(col, widths[col] + step);
  }
  if (key === "ArrowLeft") {
    return clamp(col, widths[col] - step);
  }
  return null;
}

/** storedColumns reads remembered widths: each one clamped, anything that
 *  is not a column's number left at its default. */
export function storedColumns(raw: string | null): ColumnWidths {
  const out = { ...COLUMN_DEFAULTS };
  if (!raw) {
    return out;
  }
  try {
    const parsed = JSON.parse(raw) as Record<string, unknown>;
    for (const col of ROADMAP_COLUMNS) {
      const v = parsed?.[col];
      if (typeof v === "number" && Number.isFinite(v)) {
        out[col] = clamp(col, v);
      }
    }
  } catch {
    // not ours: the defaults
  }
  return out;
}

/** tableWidth is the table part's width: its columns added up. */
export function tableWidth(widths: ColumnWidths): number {
  return ROADMAP_COLUMNS.reduce((s, c) => s + widths[c], 0);
}

/** columnVars are the widths as the CSS variables the grid reads. */
export function columnVars(widths: ColumnWidths): Record<string, string> {
  const out: Record<string, string> = {};
  for (const col of ROADMAP_COLUMNS) {
    out[`--rm-${col}`] = `${widths[col]}px`;
  }
  out["--rm-table"] = `${tableWidth(widths)}px`;
  return out;
}
