/** The week grid — the geometry two boards share.
 *
 * A week grid is a table whose rows are weeks and whose columns are something
 * a card belongs to: an epic on the Project board, a person on Triage. The
 * rows, their labels, how far the window reaches, how wide a column is at a
 * given zoom and where a card's dates land in the grid are the same questions
 * on both, and they are answered here — once, without a DOM — so the two
 * boards cannot drift apart.
 *
 * Nothing in this file knows what a column is. That is the whole point of the
 * seam: the caller names its columns, this file lays out the weeks.
 */
import { addDays, mondayOf, weeksBetween } from "./date";

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

/** How many weeks a click on "earlier"/"later" adds. */
export const WEEK_STEP = 8;
/** The header row's height, in pixels. Must match the grid's first row track. */
export const HEADER_PX = 26;
/** The week column's width, in pixels: what "17 Aug" needs and no more. */
export const WEEK_COL_PX = 54;
/** The gutter after the last column, where a new column is added. */
export const GUTTER_PX = 34;
/** A column's width, and a row's height, at zoom 1. */
export const BASE_COL = 140;
export const BASE_ROW = 28;
/** How many weeks the window shows before and after today by default. */
export const WEEKS_BACK = 2;
export const WEEKS_FWD = 8;
/** How far past the last card the window keeps running, so there is room to
 *  drag one further out without the board having to grow first. */
export const RUNWAY_WEEKS = 2;

/** isoWeekNo is the ISO-8601 week number of a Monday.
 *
 *  A week belongs to the year of its THURSDAY — that one rule settles both
 *  ends of December, where a week is shared between two years. */
export function isoWeekNo(monday: string): number {
  const [y, m, d] = monday.split("-").map(Number);
  const thu = Date.UTC(y, m - 1, d + 3);
  const year = new Date(thu).getUTCFullYear();
  return Math.floor((thu - Date.UTC(year, 0, 1)) / (7 * 86400000)) + 1;
}

/** weekLabel is how a week names itself in the grid's left column. */
export function weekLabel(monday: string): string {
  const [, m, d] = monday.split("-").map(Number);
  return `${String(d).padStart(2, "0")} ${MONTHS[m - 1]}`;
}

/** A dated thing the window must be wide enough to hold: its week and, when
 *  it was stretched, the day it runs to. Both boards call cards this. */
export interface Dated {
  week?: string;
  day?: string;
  startDate?: string;
}

/** weekWindow is every week the grid draws, in order.
 *
 * It opens on a fixed window around today, then widens until it holds every
 * card: a card standing before the window pulls the first Monday back to it,
 * a card ending after it pushes the last Monday out — plus a runway, so the
 * board is never exactly as long as its longest card. `padBack`/`padFwd` are
 * the weeks the reader asked for on top, and only ever add. */
export function weekWindow(
  dated: readonly Dated[],
  thisMonday: string,
  padBack: number,
  padFwd: number,
): string[] {
  let first = addDays(thisMonday, -7 * (WEEKS_BACK + padBack));
  let last = addDays(thisMonday, 7 * (WEEKS_FWD + padFwd));
  for (const c of dated) {
    const anchor = c.week ? mondayOf(c.week) : null;
    if (anchor && anchor < first) {
      first = anchor;
    }
    const end = c.day ? mondayOf(c.day) : anchor;
    if (end && addDays(end, 7 * RUNWAY_WEEKS) > last) {
      last = addDays(end, 7 * RUNWAY_WEEKS);
    }
  }
  const out: string[] = [];
  for (let w = first; w <= last; w = addDays(w, 7)) {
    out.push(w);
  }
  return out;
}

/** sharedColumnPx is the width every column takes before any of them was
 *  dragged: the room left over, split evenly, but never below what a column
 *  needs to be read, and then scaled by the zoom. */
export function sharedColumnPx(
  boardW: number,
  columns: number,
  zoomX: number,
  floor: number,
): number {
  const room = Math.max(0, boardW - WEEK_COL_PX - GUTTER_PX);
  const fill = columns > 0 ? room / columns : BASE_COL;
  return Math.max(floor, Math.max(BASE_COL, fill) * zoomX);
}

/** rowPx is a week's height at a given zoom. A row never collapses to
 *  nothing: below this the grid stops being a grid. */
export function rowPx(zoomY: number): number {
  return Math.max(12, Math.round(BASE_ROW * zoomY));
}

/** Where a card stands in the grid: the row it starts on and how many rows it
 *  covers. */
export interface Extent {
  row: number;
  span: number;
}

/** extentOf places a card's dates in the window, or reports that they fall
 *  outside it. A card starts in the week of its start date (its week when it
 *  has none) and runs to the week of its end date; a card that ends before it
 *  starts takes the one row, never a negative span, and one that would run
 *  past the last week stops there. */
export function extentOf(card: Dated, weeks: readonly string[]): Extent | null {
  const from = card.startDate || card.week;
  if (!from || weeks.length === 0) {
    return null;
  }
  const anchor = mondayOf(from);
  const row = weeksBetween(weeks[0], anchor);
  if (row < 0 || row >= weeks.length) {
    return null;
  }
  const endMon = card.day && card.day > anchor ? mondayOf(card.day) : anchor;
  const span = Math.max(1, Math.min(weeksBetween(anchor, endMon) + 1, weeks.length - row));
  return { row, span };
}
