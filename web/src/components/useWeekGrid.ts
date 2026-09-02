/** The week grid's machinery: what a board holding one has to keep.
 *
 * The geometry itself is in `../weekgrid` and knows no React. This hook is
 * the running board around it — the window the reader has widened, the zoom
 * and the per-column widths they have chosen, the measurements the layers
 * above the grid are placed by, and the two hit tests a pointer gesture asks
 * ("which week is under the cursor", "which column"). Everything a second
 * board would otherwise have had to write again.
 *
 * What it does NOT know is what a column is. It counts them, it measures
 * them, and it hands back an index; naming them is the board's business.
 */
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { mondayOf, todayIso } from "../date";
import { MIN_COL_PX, type Zoom, anchoredScroll, clampZoom, wheelZoom } from "../projectZoom";
import {
  type Dated,
  HEADER_PX,
  WEEK_COL_PX,
  WEEK_STEP,
  rowPx,
  sharedColumnPx,
  weekWindow,
} from "../weekgrid";

/** Where the board sits inside its host, and where the grid starts inside the
 *  board. A layer drawn OVER the grid — the deadline handles — is placed with
 *  it: a scroll container clips at its own edge, so a handle that straddles
 *  that edge cannot be a child of it. */
export interface BoardBox {
  left: number;
  top: number;
  height: number;
  gridTop: number;
}

export interface WeekGridOptions {
  /** The cards the window has to be wide enough to hold. */
  dated: readonly Dated[];
  /** How many columns stand on the board — they share the width. */
  columns: number;
  /** Where the board's own zoom and column widths are kept, so two boards
   *  never read each other's. */
  store: { zoom: string; widths: string };
  /** What the reader is looking at. The board scrolls to today once per
   *  selection: coming back to one they have scrolled away from should not
   *  yank them back, but choosing another view should open on today. */
  selection?: string;
  /** How many weeks the window opens before today. A board that folds what is
   *  overdue into this week has no past rows and passes 0. */
  back?: number;
}

export interface WeekGrid {
  /** Every week the grid draws, in order, and where today's week sits in it
   *  (-1 when today is outside the window). */
  weeks: string[];
  todayRow: number;
  today: string;
  /** A row's height and a column's shared width, at the current zoom. */
  rowH: number;
  sharedCol: number;
  /** The columns that carry a width of their own, as a ratio of the shared
   *  one, keyed by the caller's own column key. */
  colFactors: Record<string, number>;
  setColFactor: (key: string, factor: number | null) => void;
  resetColFactors: (keys: readonly string[]) => void;
  zoom: Zoom;
  setZoom: (z: Zoom) => void;
  /** Widening the window. Going back moves the scroll by exactly the height
   *  added, or the rows would slide out from under the reader. */
  showEarlier: () => void;
  showLater: () => void;
  /** The scrolling ancestor, the grid inside it, and the containing block the
   *  layers above the grid are measured against. */
  scrollRef: React.MutableRefObject<HTMLDivElement | null>;
  gridRef: React.MutableRefObject<HTMLDivElement | null>;
  wrapRef: React.MutableRefObject<HTMLDivElement | null>;
  boardBox: BoardBox | null;
  /** Which week, and which column, is under a pointer. Both read the live
   *  DOM: the grid's own tracks are the only honest answer once the columns
   *  carry widths of their own. */
  rowAt: (clientY: number) => number;
  columnAt: (clientX: number) => number | null;
}

function readColFactors(key: string): Record<string, number> {
  try {
    const v: unknown = JSON.parse(localStorage.getItem(key) ?? "null");
    if (v && typeof v === "object") {
      return Object.fromEntries(
        Object.entries(v as Record<string, unknown>).filter(
          ([, f]) => typeof f === "number" && f > 0 && f < 20,
        ),
      ) as Record<string, number>;
    }
  } catch {
    // A corrupt entry is not worth a broken board.
  }
  return {};
}

function readZoom(key: string): Zoom {
  try {
    const v: unknown = JSON.parse(localStorage.getItem(key) ?? "null");
    if (v && typeof v === "object") {
      const z = v as { x?: unknown; y?: unknown };
      return {
        x: clampZoom(typeof z.x === "number" ? z.x : 1),
        y: clampZoom(typeof z.y === "number" ? z.y : 1),
      };
    }
  } catch {
    // ditto
  }
  return { x: 1, y: 1 };
}

export function useWeekGrid({ dated, columns, store, selection, back }: WeekGridOptions): WeekGrid {
  const today = todayIso();
  const thisMonday = mondayOf(today);

  const [padBack, setPadBack] = useState(0);
  const [padFwd, setPadFwd] = useState(0);

  // How much bigger the board draws its cells, and which columns keep a width
  // of their own (a ratio to the shared width, so zoom scales it too). Both
  // are the reader's, and both outlive the session.
  const [zoom, setZoomState] = useState<Zoom>(() => readZoom(store.zoom));
  const zoomRef = useRef(zoom);
  const [colFactors, setColFactorsState] = useState<Record<string, number>>(() =>
    readColFactors(store.widths),
  );
  const factorsRef = useRef(colFactors);

  const setZoom = useCallback(
    (z: Zoom) => {
      zoomRef.current = z;
      setZoomState(z);
      localStorage.setItem(store.zoom, JSON.stringify(z));
    },
    [store.zoom],
  );

  const writeFactors = useCallback(
    (next: Record<string, number>) => {
      factorsRef.current = next;
      setColFactorsState(next);
      localStorage.setItem(store.widths, JSON.stringify(next));
    },
    [store.widths],
  );

  const setColFactor = useCallback(
    (key: string, factor: number | null) => {
      const next = { ...factorsRef.current };
      if (factor === null) {
        delete next[key];
      } else {
        next[key] = factor;
      }
      writeFactors(next);
    },
    [writeFactors],
  );

  // Only the columns shown give their width up: a column not on the board
  // keeps the one it was given.
  const resetColFactors = useCallback(
    (keys: readonly string[]) => {
      const next = { ...factorsRef.current };
      for (const k of keys) {
        delete next[k];
      }
      writeFactors(next);
    },
    [writeFactors],
  );

  // The width a column has when it has no width of its own: what the columns
  // would share at zoom 1, scaled by the zoom. Measured from the board rather
  // than assumed, so zooming starts from what is actually on screen instead of
  // jumping to a constant the first time it is touched.
  const [boardW, setBoardW] = useState(0);
  const sharedCol = useMemo(
    () => sharedColumnPx(boardW, columns, zoom.x, MIN_COL_PX),
    [boardW, columns, zoom.x],
  );
  const rowH = rowPx(zoom.y);

  const gridRef = useRef<HTMLDivElement | null>(null);
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const wrapRef = useRef<HTMLDivElement | null>(null);
  const [boardBox, setBoardBox] = useState<BoardBox | null>(null);

  const weeks = useMemo(
    () => weekWindow(dated, thisMonday, padBack, padFwd, back),
    [dated, thisMonday, padBack, padFwd, back],
  );
  const todayRow = weeks.indexOf(thisMonday);

  // Prepending rows would slide the grid under the reader, so the scroll
  // position is moved by exactly the height added.
  const showEarlier = useCallback(() => {
    const scroller = scrollRef.current;
    setPadBack((p) => p + WEEK_STEP);
    if (scroller) {
      requestAnimationFrame(() => {
        scroller.scrollTop += WEEK_STEP * rowH;
      });
    }
  }, [rowH]);

  const showLater = useCallback(() => setPadFwd((p) => p + WEEK_STEP), []);

  // Where the scroller must land after the next zoom-driven re-layout, so the
  // point under the cursor stays under it.
  const pendingScroll = useRef<{ left: number; top: number } | null>(null);
  useLayoutEffect(() => {
    const el = scrollRef.current;
    const want = pendingScroll.current;
    if (!el || !want) {
      return;
    }
    pendingScroll.current = null;
    el.scrollLeft = want.left;
    el.scrollTop = want.top;
  }, [zoom]);

  // Ctrl/Cmd + wheel zooms the board. Both axes move by the same step from
  // wherever they are, so the two sliders keep their offset; the browser's
  // own page zoom is what the modifier would otherwise do, hence preventing
  // the default.
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) {
      return;
    }
    const onWheel = (e: WheelEvent) => {
      if (!e.ctrlKey && !e.metaKey) {
        return;
      }
      e.preventDefault();
      const was = zoomRef.current;
      const next = wheelZoom(was, e.deltaY);
      // Zoom around the cursor: work out where the board must be scrolled to
      // leave the point under the pointer where it is, and apply it AFTER the
      // grid has been re-laid — a scroll set against the old size is clamped
      // to it, which is what makes a naive version drift.
      const box = el.getBoundingClientRect();
      pendingScroll.current = {
        left: anchoredScroll(el.scrollLeft, e.clientX - box.left, WEEK_COL_PX, next.x / was.x),
        top: anchoredScroll(el.scrollTop, e.clientY - box.top, HEADER_PX, next.y / was.y),
      };
      setZoom(next);
    };
    // Not passive: the whole point is to take the gesture from the browser.
    el.addEventListener("wheel", onWheel, { passive: false });
    return () => el.removeEventListener("wheel", onWheel);
  }, [setZoom]);

  // Measure the board (and where the grid starts inside it) whenever the
  // layout can have moved. Cheap, and never during a scroll.
  useLayoutEffect(() => {
    const board = scrollRef.current;
    const grid = gridRef.current;
    const host = wrapRef.current;
    if (!board || !grid || !host) {
      setBoardBox(null);
      return;
    }
    const measure = () => {
      const b = board.getBoundingClientRect();
      const h = host.getBoundingClientRect();
      const g = grid.getBoundingClientRect();
      setBoardBox({
        left: b.left - h.left,
        top: b.top - h.top,
        height: b.height,
        gridTop: g.top - b.top + board.scrollTop,
      });
      // The room the columns divide between them, for the zoom's base width.
      setBoardW(b.width);
    };
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(board);
    window.addEventListener("resize", measure);
    return () => {
      ro.disconnect();
      window.removeEventListener("resize", measure);
    };
  }, [weeks.length, columns, sharedCol, padBack, padFwd]);

  // Open on today. A plan reaches months back, and the board would otherwise
  // open at its very first week — someone arriving had to scroll past a spent
  // quarter to find out where the team is. Once per selection: after that the
  // scroll is the reader's, and pressing "earlier weeks" must not yank it
  // back.
  const scrolledFor = useRef<string | null>(null);
  const key = selection ?? "*";
  useLayoutEffect(() => {
    const scroller = scrollRef.current;
    if (!scroller || !boardBox || todayRow < 0 || scrolledFor.current === key) {
      return;
    }
    // Two weeks of context above today, and never past the top.
    const top = Math.max(0, boardBox.gridTop + HEADER_PX + (todayRow - 2) * rowH);
    // After the paint: switching selection rebuilds the grid, and a scroll set
    // before it has its new height is clamped to the old one. The mark is set
    // in the callback, not here — this effect re-runs as the new board is
    // measured, and a mark set up front would cancel the pending frame and
    // then refuse to schedule another, which is why switching never moved.
    const frame = requestAnimationFrame(() => {
      scrolledFor.current = key;
      scroller.scrollTop = top;
    });
    return () => cancelAnimationFrame(frame);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [boardBox, todayRow, key, weeks.length]);

  // Both hit tests read the live DOM rather than compute from the tracks:
  // once columns carry widths of their own, the elements are the only place
  // the truth is whole.
  const rowAt = useCallback((clientY: number): number => {
    const grid = gridRef.current;
    if (!grid) {
      return 0;
    }
    const rows = grid.querySelectorAll(".project-week");
    for (let i = 0; i < rows.length; i++) {
      const r = rows[i].getBoundingClientRect();
      if (clientY < r.bottom) {
        return i;
      }
    }
    return rows.length - 1;
  }, []);

  const columnAt = useCallback((clientX: number): number | null => {
    const grid = gridRef.current;
    if (!grid) {
      return null;
    }
    const heads = grid.querySelectorAll(".project-epic-head:not(.project-epic-add)");
    for (let i = 0; i < heads.length; i++) {
      const r = heads[i].getBoundingClientRect();
      if (clientX >= r.left && clientX < r.right) {
        return i;
      }
    }
    return null;
  }, []);

  return {
    weeks,
    todayRow,
    today,
    rowH,
    sharedCol,
    colFactors,
    setColFactor,
    resetColFactors,
    zoom,
    setZoom,
    showEarlier,
    showLater,
    scrollRef,
    gridRef,
    wrapRef,
    boardBox,
    rowAt,
    columnAt,
  };
}
