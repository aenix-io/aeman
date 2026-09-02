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
 * the weeks the reader asked for on top, and only ever add.
 *
 * `back` is how far the window opens BEFORE today. A board that folds what is
 * overdue into this week passes 0 and has no past
 * rows at all; then only a card dated in the past widens it. */
export function weekWindow(
  dated: readonly Dated[],
  thisMonday: string,
  padBack: number,
  padFwd: number,
  back = WEEKS_BACK,
): string[] {
  let first = addDays(thisMonday, -7 * (back + padBack));
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

/** A slot standing in a column: the rows it covers, and — once packed — the
 *  lane it was given, how many lanes its cluster was split into and how many
 *  of them it is drawn across. */
export interface Laned extends Extent {
  lane: number;
  lanes: number;
  width: number;
  /** Where the card sits in its lane's stack, and how many share that stack.
   *  A card standing alone in its lane is 0 of 1. Only a board whose rows may
   *  grow ever stacks; a board starts a slot with 0 of 1. */
  stack: number;
  stacked: number;
}

/** A slot held in a lane it already has. While an edge is being pulled the
 *  slot must not hop lanes under the pointer: it keeps `lane`, and the others
 *  pack around the rows it previews. */
export interface Pin<S> {
  is: (slot: S) => boolean;
  lane: number;
  row: number;
  span: number;
}

/** packLanes gives every slot in every column a lane.
 *
 * Lanes are worked out per CLUSTER of overlapping slots, not per column:
 * splitting a whole column because two slots happen to share a fortnight
 * would leave every unrelated card at half width for no reason. Within a
 * cluster it is first fit, longest card first, and a slot then grows
 * rightwards over the lanes that are free for all of its own rows — a card
 * beside a quiet week has room the busy weeks do not.
 *
 * The lists are packed in place. */
export function packLanes<S extends Laned>(
  lists: Iterable<S[]>,
  pin?: Pin<S>,
  stack = false,
): void {
  for (const list of lists) {
    for (const s of list) {
      s.stack = 0;
      s.stacked = 1;
    }
    if (!stack) {
      packOne(list, pin);
      continue;
    }
    // The rows may grow, so cards that stand in a SINGLE week need no lane of
    // their own: they share one, one under the next. Each week's cards go in
    // as a single stand-in, and only what cannot be stacked — a card reaching
    // into another week, which is one tall rectangle — is packed beside them.
    const bundles = new Map<number, S[]>();
    const packing: Laned[] = [];
    for (const s of list) {
      if (s.span > 1) {
        packing.push(s);
        continue;
      }
      const week = bundles.get(s.row) ?? [];
      week.push(s);
      bundles.set(s.row, week);
    }
    const standIn = new Map<number, Laned>();
    for (const row of bundles.keys()) {
      const one: Laned = { row, span: 1, lane: 0, lanes: 1, width: 1, stack: 0, stacked: 1 };
      standIn.set(row, one);
      packing.push(one);
    }
    packOne(packing, pin as Pin<Laned> | undefined);
    for (const [row, week] of bundles) {
      const one = standIn.get(row);
      if (!one) {
        continue;
      }
      week.forEach((s, i) => {
        s.lane = one.lane;
        s.lanes = one.lanes;
        s.width = one.width;
        s.stack = i;
        s.stacked = week.length;
      });
    }
  }
}

/** packOne is the lane assignment for one column, taking every slot handed to
 *  it as needing a lane of its own. */
function packOne<S extends Laned>(list: S[], pin?: Pin<S>): void {
  {
    list.sort((a, b) => a.row - b.row || b.span - a.span);
    let cluster: S[] = [];
    let laneEnd: number[] = [];
    const close = () => {
      const lanes = laneEnd.length;
      for (const s of cluster) {
        s.lanes = lanes;
        let width = 1;
        while (s.lane + width < lanes) {
          const nextLane = s.lane + width;
          const taken = cluster.some(
            (o) => o !== s && o.lane === nextLane && o.row < s.row + s.span && s.row < o.row + o.span,
          );
          if (taken) {
            break;
          }
          width += 1;
        }
        s.width = width;
      }
      cluster = [];
      laneEnd = [];
    };
    // Whether a slot's rows overlap the pinned slot's previewed extent.
    const meetsPin = (s: S) => !!pin && s.row < pin.row + pin.span && pin.row < s.row + s.span;
    for (const s of list) {
      // A slot that begins after everything so far has ended starts a new
      // cluster: it shares its weeks with nothing before it.
      if (laneEnd.length && s.row >= Math.max(...laneEnd)) {
        close();
      }
      let lane: number;
      if (pin && pin.is(s)) {
        lane = pin.lane;
        while (laneEnd.length <= lane) {
          laneEnd.push(0);
        }
      } else {
        // First fit — but never into the pinned lane while sharing rows with
        // the pinned slot, even before it is placed.
        lane = laneEnd.findIndex(
          (end, i) => end <= s.row && !(pin && i === pin.lane && meetsPin(s)),
        );
        if (lane === -1) {
          lane = laneEnd.length;
          if (pin && lane === pin.lane && meetsPin(s)) {
            laneEnd.push(0);
            lane += 1;
          }
        }
      }
      laneEnd[lane] = Math.max(laneEnd[lane] ?? 0, s.row + s.span);
      s.lane = lane;
      cluster.push(s);
    }
    close();
  }
}

/** Where a slot sits in the room its cell has: a share of the column's width
 *  beside its neighbours, or a band of the row's height under them. */
export interface LanePlacement {
  width?: string;
  marginLeft?: string;
  height?: string;
  marginTop?: string;
}

/** laneStyle turns a slot's lane and its place in that lane's stack into the
 *  room it takes.
 *
 *  A lane is a share of the column's WIDTH, and it is what keeps apart cards
 *  the grid cannot stack: one reaching into another week is a single tall
 *  rectangle, and its band in this row and its band in the next are not
 *  adjacent, so it stands beside its neighbours rather than among them.
 *
 *  A stack is a share of the row's HEIGHT, and it is where cards that all
 *  stand in one week go — full lane width, one under the next, their week
 *  growing to hold them. Only a board whose rows may grow (`fit`) stacks.
 *
 *  The two compose: a week holding one stretched card and three ordinary
 *  ones is two lanes wide, and the three share the second lane in a stack.
 *
 *  `trim` is what the caller wants taken off the width on top of the margins
 *  — a card stepped aside to uncover the strip beside it. */
export function laneStyle(s: Laned, fit: boolean, rowH: number, trim = 0): LanePlacement {
  const placed: LanePlacement = {};
  if (s.lanes > 1) {
    placed.width = `calc(${(100 / s.lanes) * s.width}% - ${2 + trim}px)`;
    placed.marginLeft = `${(100 / s.lanes) * s.lane}%`;
  }
  if (fit) {
    // Every card exactly one row tall, less the margins — so they all read
    // the same however busy their week is, and the week grows by one row for
    // each card standing under the first.
    placed.height = `${Math.max(0, rowH - 4)}px`;
    placed.marginTop = `${s.stack * rowH}px`;
  }
  return placed;
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
