/** The Project board's zoom: how much bigger its cells are drawn than the
 *  board's own idea of a cell. Text is never scaled — only the room around it,
 *  so a dense quarter can be spread out to be read and squeezed back to be
 *  seen whole. */
export interface Zoom {
  /** Column width multiplier. */
  x: number;
  /** Row height multiplier. */
  y: number;
}

export const MIN_ZOOM = 0.5;
export const MAX_ZOOM = 3;
/** How close to the shared width a dragged column must come before it gives
 *  up its own width and rejoins the others. */
export const SNAP = 0.08;
/** The narrowest a column may be dragged, in pixels. */
export const MIN_COL_PX = 60;
/** One wheel notch, as a share of the current scale. */
const WHEEL_STEP = 0.0015;

export function clampZoom(v: number): number {
  return Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, v));
}

/** wheelZoom moves BOTH axes by the same step from where they are — the two
 *  sliders keep whatever offset they have instead of snapping together. An
 *  axis that has hit its end simply stops there; it does not hold the other
 *  back, or a board zoomed wide could never be made taller. */
export function wheelZoom(z: Zoom, deltaY: number): Zoom {
  const step = -deltaY * WHEEL_STEP;
  return { x: clampZoom(z.x + step), y: clampZoom(z.y + step) };
}

/** columnFactor turns a dragged width into what the column remembers: its size
 *  relative to the width the other columns share. null means "no width of its
 *  own" — either it was dragged back to within SNAP of the others, or it never
 *  left. Kept as a RATIO, not pixels, so zooming keeps the relation and a
 *  column carries its size from one project selection to the next. */
export function columnFactor(widthPx: number, sharedPx: number): number | null {
  if (sharedPx <= 0) {
    return null;
  }
  const floor = MIN_COL_PX / sharedPx;
  const factor = Math.max(floor, widthPx / sharedPx);
  return Math.abs(factor - 1) < SNAP ? null : factor;
}

/** anchoredScroll keeps the point under the cursor where it is while the board
 *  scales around it.
 *
 *  Only the cells scale: the date column on the left and the header row on top
 *  keep their size whatever the zoom, so `fixed` is subtracted before scaling
 *  and added back after. A cursor over that fixed part has nothing to anchor —
 *  the scroll stays put.
 *
 *  @param scroll  the scroller's current offset
 *  @param cursor  the cursor's distance from the scroller's own edge
 *  @param fixed   the gutter that does not scale (54px column, 26px header)
 *  @param k       the scale change — new zoom over old
 */
export function anchoredScroll(
  scroll: number,
  cursor: number,
  fixed: number,
  k: number,
): number {
  const point = scroll + cursor;
  if (point <= fixed) {
    return scroll;
  }
  const scaled = fixed + (point - fixed) * k;
  return Math.max(0, scaled - cursor);
}
