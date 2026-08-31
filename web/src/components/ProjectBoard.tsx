import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import type {
  Board,
  Card as CardModel,
  EpicRef,
  Provider,
} from "../providers/types";
import { registerPendingCard } from "../api/pending";
import { addDays, mondayOf, todayIso, weeksBetween } from "../date";
import { teamColor, teamInitial } from "../avatar";
import { cardDomainBadge, offerableTeams } from "../domains";
import {
  canCreateInColumn,
  columnsOf,
  countedForProgress,
  projectsAColumnCanJoin,
  teamlessIsLawful,
  teamsACardCanTake,
  drawnAsSlot,
  drawnOnProjectBoard,
  teamFollowsParent,
  makeCardPlacements,
  type CardPlacements,
  movingSlot,
  removeFromProjectOutcome,
  rosterOf,
  settleMirrorDrop,
  slotDragPlan,
  slotDropMirrors,
} from "../placements";
import { PlacementMenu } from "./PlacementMenu";
import { deleteWarning, freeSubtasks } from "../removal";
import { Dropdown } from "./Dropdown";
import { ProjectPicker } from "./ProjectPicker";
import { STAGES } from "../stages";
import { TeamChips } from "./TeamChips";
import { ZoomControl } from "./ZoomControl";
import {
  MIN_COL_PX,
  type Zoom,
  anchoredScroll,
  clampZoom,
  columnFactor,
  wheelZoom,
} from "../projectZoom";

interface ProjectBoardProps {
  board: Board;
  provider: Provider;
  /** Which projects are in view (null = all; "" = the no-project bucket). */
  filter: string[] | null;
  onSetFilter: (keys: string[] | null) => void;
  /** Opens the project manager, which the App owns — both this tab and the
   *  Process tab reach it through their chip row. */
  onManageProjects: () => void;
  patchCard: (
    itemId: string,
    patch: Partial<CardModel> | ((c: CardModel) => Partial<CardModel>),
  ) => void;
  addCard: (card: CardModel) => void;
  replaceCard: (itemId: string, card: CardModel) => void;
  removeCard: (itemId: string) => void;
  reload: () => void;
  onError: (message: string) => void;
  onOpen: (card: CardModel) => void;
}

/** isoWeekNo is the ISO-8601 week number of a Monday. */
function isoWeekNo(monday: string): number {
  const [y, m, d] = monday.split("-").map(Number);
  const date = new Date(Date.UTC(y, m - 1, d));
  const jan4 = new Date(Date.UTC(date.getUTCFullYear(), 0, 4));
  const week1Mon = new Date(jan4);
  week1Mon.setUTCDate(jan4.getUTCDate() - ((jan4.getUTCDay() + 6) % 7));
  const diff = Math.round((date.getTime() - week1Mon.getTime()) / 86400000);
  if (diff < 0) {
    // The Monday belongs to the previous year's last ISO week.
    const prevJan4 = new Date(Date.UTC(date.getUTCFullYear() - 1, 0, 4));
    const prevW1 = new Date(prevJan4);
    prevW1.setUTCDate(prevJan4.getUTCDate() - ((prevJan4.getUTCDay() + 6) % 7));
    return Math.floor((date.getTime() - prevW1.getTime()) / (7 * 86400000)) + 1;
  }
  return Math.floor(diff / 7) + 1;
}

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

/** How far the pointer must travel before a press on a card becomes a drag. */
const DRAG_SLOP = 4;
const HEADER_PX = 26;
const WEEK_STEP = 8;

function weekLabel(monday: string): string {
  const [, m, d] = monday.split("-").map(Number);
  return `${String(d).padStart(2, "0")} ${MONTHS[m - 1]}`;
}

/** complete reports whether a card is finished — the board's own rule. */
function complete(card: CardModel): boolean {
  return card.stage === "done" || (!card.stage && (card.progress ?? 0) >= 100);
}

/** progressOf summarises a set of cards: how many of them are finished. Cards
 *  done, not progress averaged — a plan is tracked in work completed, and the
 *  percentage has to agree with the "n/m done" printed beside it. A half-built
 *  card counts for nothing here, which is the point: it is not done. */
function progressOf(list: CardModel[]): { pct: number; done: number; total: number } {
  const done = list.filter(complete).length;
  return {
    pct: list.length ? Math.round((done / list.length) * 100) : 0,
    done,
    total: list.length,
  };
}

/** slotTone is how a slot is doing, as a class: finished, taken into work by
 *  someone, past its end date, or both taken and past it. Done comes first —
 *  a finished card is finished whatever its dates say. */
function slotTone(card: CardModel, today: string): string {
  if (complete(card)) {
    return "project-slot-done";
  }
  // The server's one definition of overdue (board.Overdue), with the local
  // reading as a fallback for a card created a moment ago and not yet echoed
  // back: the two agree, and only one of them should exist.
  const late = card.overdue ?? (!!card.day && card.day < today);
  // Taken into WORK, not merely assigned: a plan names its owners months
  // ahead, so an assignee alone would paint the whole roadmap yellow and say
  // nothing. A slot joins a sprint (or moves off zero) when the work actually
  // starts — that is the board's own line between planned and underway.
  const taken = (card.progress ?? 0) > 0 || !!card.sprintStart;
  if (late) {
    return taken ? "project-slot-late-taken" : "project-slot-late";
  }
  return taken ? "project-slot-taken" : "";
}

/** colKey is a column's identity: the (project, epic) pair. Epic names repeat
 *  across projects, so a name alone is not a key. */
function colKey(project: string, epic: string): string {
  return `${project}\u0000${epic}`;
}

const LS_PROGRESS = "aeman.projectProgressOpen";
/** Per-column widths, kept as a RATIO to the shared width and keyed by the
 *  column itself, so a column carries its size from one project selection to
 *  the next — and the board's zoom scales it along with everything else. */
const LS_COLF = "aeman.projectColFactors";
/** The board's own zoom, one entry per axis. */
const LS_ZOOM = "aeman.projectZoom";

/** The cell the board is drawn from before any zoom or per-column width. */
/** How far a card steps aside to uncover the strip a new one starts from,
 *  and how long the pointer must rest there first — long enough that merely
 *  crossing the card on the way somewhere else never moves it. */
const NUDGE_PX = 13;
const NUDGE_DELAY_MS = 500;
const BASE_COL = 140;
const BASE_ROW = 28;

function readColFactors(): Record<string, number> {
  try {
    const v: unknown = JSON.parse(localStorage.getItem(LS_COLF) ?? "null");
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

function readZoom(): Zoom {
  try {
    const v: unknown = JSON.parse(localStorage.getItem(LS_ZOOM) ?? "null");
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


/** The Project board: weeks as rows and one project's epics as columns, cards
 *  as slots that may span several weeks (dates start..end). Dragging down an
 *  empty column stretch selects a slot and creates a card in it; assigning a
 *  card to a team (the badge menu) also hands it to that team's weekly plan —
 *  which is how work planned here reaches the people who do it. */
export function ProjectBoard({
  board,
  provider,
  filter,
  onSetFilter,
  onManageProjects,
  patchCard,
  addCard,
  replaceCard,
  removeCard,
  reload,
  onError,
  onOpen,
}: ProjectBoardProps) {
  const today = todayIso();
  const thisMonday = mondayOf(today);

  // Which project(s) the chips select; null is every project. "" is the chip
  // for columns that belong to no project.
  // The chips' selection is owned by the App and shared with the Process tab.
  const selectFilter = onSetFilter;

  // The columns in view: the selected projects' epics, in board order. Every
  // other derived list follows from this one, so a column and its cards can
  // never disagree about being visible. A column is the (project, name) PAIR —
  // "Docs" exists in every project and they are different columns.
  // While a column is being dragged — and until the server's order catches
  // up — this is the order the columns are drawn in. Null means "the board's".
  const [order, setOrder] = useState<string[] | null>(null);

  const epics = useMemo(() => {
    const shown = board.epics.filter((e) => !filter || filter.includes(e.project));
    if (!order) {
      return shown;
    }
    const at = (e: EpicRef) => {
      const i = order.indexOf(colKey(e.project, e.name));
      return i === -1 ? Number.MAX_SAFE_INTEGER : i;
    };
    return [...shown].sort((a, b) => at(a) - at(b));
  }, [board.epics, filter, order]);

  // The board caught up (or the columns changed under us): stop overriding.
  useEffect(() => {
    if (!order) {
      return;
    }
    const live = board.epics
      .filter((e) => !filter || filter.includes(e.project))
      .map((e) => colKey(e.project, e.name));
    if (live.length === order.length && live.every((k, i) => k === order[i])) {
      setOrder(null);
    }
  }, [board.epics, filter, order]);

  // Columns belonging to no project are reachable through a "No project" chip
  // — but only while such a column exists, so the chip never sits there as a
  // permanently empty option.
  const looseEpics = useMemo(
    () => board.epics.some((e) => !e.project),
    [board.epics],
  );

  // Cards on the board: filed under one of the visible columns. Teams do not
  // filter here — a project spans teams, and the badge is where a card is
  // handed to one.
  const cards = useMemo(() => {
    const shown = new Set(epics.map((e) => colKey(e.project, e.name)));
    return board.cards.filter((c) => drawnOnProjectBoard(c, shown));
  }, [board.cards, epics]);

  // The week window: two weeks of history before today (or the earliest
  // card), through the latest card plus a quarter of runway to plan into.
  // How far the window reaches beyond the default, in weeks, grown by the
  // buttons at either end — planning is not confined to a fixed horizon.
  // The card currently stepped aside to leave room for a new one beside it.
  const [nudged, setNudged] = useState<string | null>(null);
  const nudgeTimer = useRef<number | null>(null);
  const cancelNudgeTimer = () => {
    if (nudgeTimer.current !== null) {
      window.clearTimeout(nudgeTimer.current);
      nudgeTimer.current = null;
    }
  };
  const [padBack, setPadBack] = useState(0);
  const [padFwd, setPadFwd] = useState(0);

  // Column width: null until dragged, when the columns just share the room —
  // the right default. One width governs every column of the selection,
  // because a plan reads as a grid and columns of assorted widths stop being
  // comparable; a different selection carries its own width.
  // How much bigger the board draws its cells, and which columns keep a width
  // of their own (a ratio to the shared width, so zoom scales it too).
  const [zoom, setZoomState] = useState<Zoom>(readZoom);
  const zoomRef = useRef(zoom);
  const [colFactors, setColFactorsState] = useState<Record<string, number>>(readColFactors);
  const factorsRef = useRef(colFactors);
  const [resizing, setResizing] = useState<{
    key: string;
    x: number;
    from: number;
  } | null>(null);

  const setZoom = (z: Zoom) => {
    zoomRef.current = z;
    setZoomState(z);
    localStorage.setItem(LS_ZOOM, JSON.stringify(z));
  };
  // The width a column has when it has no width of its own: what the columns
  // would share at zoom 1, scaled by the zoom. Measured from the board rather
  // than assumed, so zooming starts from what is actually on screen instead of
  // jumping to a constant the first time it is touched.
  const [boardW, setBoardW] = useState(0);
  const sharedCol = useMemo(() => {
    const room = Math.max(0, boardW - 54 - 34);
    const fill = epics.length > 0 ? room / epics.length : BASE_COL;
    return Math.max(MIN_COL_PX, Math.max(BASE_COL, fill) * zoom.x);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [boardW, epics.length, zoom.x]);
  const rowH = Math.max(12, Math.round(BASE_ROW * zoom.y));

  const setColFactor = (key: string, f: number | null) => {
    const next = { ...factorsRef.current };
    if (f === null) {
      delete next[key];
    } else {
      next[key] = f;
    }
    factorsRef.current = next;
    setColFactorsState(next);
    localStorage.setItem(LS_COLF, JSON.stringify(next));
  };

  // Dragging a column header sideways reorders the columns. A column can
  // always be moved among the columns of ITS OWN project — that order is what
  // the board stores — so dragging works whatever the chips are showing; a
  // header of another project simply is not a landing place.
  const colDrag = useRef<{
    key: string;
    project: string;
    x: number;
    moved: boolean;
  } | null>(null);
  const [dragCol, setDragCol] = useState<string | null>(null);

  const beginColDrag = (e: React.PointerEvent, col: EpicRef) => {
    if ((e.target as HTMLElement).closest("button, input, .project-col-resize")) {
      return;
    }
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    colDrag.current = {
      key: colKey(col.project, col.name),
      project: col.project,
      x: e.clientX,
      moved: false,
    };
  };

  const moveColDrag = (e: React.PointerEvent) => {
    const d = colDrag.current;
    if (!d) {
      return;
    }
    if (!d.moved) {
      if (Math.abs(e.clientX - d.x) < DRAG_SLOP) {
        return;
      }
      d.moved = true;
      setDragCol(d.key);
    }
    const over = epicAt(e.clientX);
    // Only among its own project's columns: dropping a column into another
    // project's run of columns would be a move between projects, which is a
    // different action with its own menu entry.
    if (!over || over.project !== d.project) {
      return;
    }
    const keys = epics.map((x) => colKey(x.project, x.name));
    const from = keys.indexOf(d.key);
    const to = keys.indexOf(colKey(over.project, over.name));
    if (from === -1 || to === -1 || from === to) {
      return;
    }
    const next = [...keys];
    next.splice(to, 0, ...next.splice(from, 1));
    setOrder(next);
  };

  const endColDrag = () => {
    const d = colDrag.current;
    colDrag.current = null;
    setDragCol(null);
    if (!d?.moved) {
      return;
    }
    // Only this project's columns are persisted — the others keep the order
    // the board already has for them.
    const names = epics.filter((x) => x.project === d.project).map((x) => x.name);
    // The preview order stays up until the Board frame confirms it (or a
    // failure puts the board's own order back).
    void provider
      .reorderEpics(d.project, names)
      .catch((err: unknown) => {
        setOrder(null); // put the board's own order back on screen
        onError(errText(err));
      });
  };

  // Dragging an edge resizes THAT column and nothing else: its width is
  // remembered as a ratio to the width the others share, so it survives a
  // zoom and a change of project selection. Dragged back to within a hair of
  // the others it gives the ratio up and rejoins them — a column should not
  // stay subtly different from a width nobody can see is different.
  const beginResize = (key: string) => (e: React.PointerEvent) => {
    e.preventDefault();
    e.stopPropagation();
    const head = (e.currentTarget as HTMLElement).closest(".project-epic-head");
    const from = head ? head.getBoundingClientRect().width : sharedCol;
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    setResizing({ key, x: e.clientX, from });
  };

  const moveResize = (e: React.PointerEvent) => {
    if (!resizing) {
      return;
    }
    const width = Math.max(MIN_COL_PX, Math.round(resizing.from + (e.clientX - resizing.x)));
    setColFactor(resizing.key, columnFactor(width, sharedCol));
  };

  const endResize = () => {
    if (!resizing) {
      return;
    }
    setResizing(null);
  };

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
        left: anchoredScroll(el.scrollLeft, e.clientX - box.left, 54, next.x / was.x),
        top: anchoredScroll(el.scrollTop, e.clientY - box.top, HEADER_PX, next.y / was.y),
      };
      setZoom(next);
    };
    // Not passive: the whole point is to take the gesture from the browser.
    el.addEventListener("wheel", onWheel, { passive: false });
    return () => el.removeEventListener("wheel", onWheel);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const weeks = useMemo(() => {
    let first = addDays(thisMonday, -14 - 7 * padBack);
    let last = addDays(thisMonday, 7 * (8 + padFwd));
    for (const c of cards) {
      const anchor = c.week ? mondayOf(c.week) : null;
      if (anchor && anchor < first) {
        first = anchor;
      }
      const end = c.day ? mondayOf(c.day) : anchor;
      if (end && addDays(end, 7 * 2) > last) {
        last = addDays(end, 7 * 2);
      }
    }
    const out: string[] = [];
    for (let w = first; w <= last; w = addDays(w, 7)) {
      out.push(w);
    }
    return out;
  }, [cards, thisMonday, padBack, padFwd]);

  // Prepending rows would slide the grid under the reader, so the scroll
  // position is moved by exactly the height added.
  const showEarlier = () => {
    const scroller = scrollRef.current;
    setPadBack(padBack + WEEK_STEP);
    if (scroller) {
      requestAnimationFrame(() => {
        scroller.scrollTop += WEEK_STEP * rowH;
      });
    }
  };

  // ---- drag-to-create (and resize): a pressed column stretch becomes a slot.
  // The gestures listen on the WINDOW for their whole life: the live
  // preview re-lanes and remounts slots, and a removed element takes its
  // pointer capture — and the rest of the gesture — with it (the stuck,
  // unreleasable drag). Window listeners are closures of the press-time
  // render, so the state they read lives in refs.
  const dragRef = useRef<typeof drag>(null);
  const moveRef = useRef<typeof move>(null);
  const [drag, setDragState] = useState<{
    epic: EpicRef;
    from: number;
    to: number;
    resize?: CardModel;
    // Which edge a resize is pulling: the top moves the slot's start (its
    // row), the bottom its end. The other edge stays where it is.
    edge?: "top" | "bottom";
  } | null>(null);
  const [draft, setDraft] = useState<{
    epic: EpicRef;
    from: number;
    to: number;
  } | null>(null);
  const gridRef = useRef<HTMLDivElement | null>(null);
  // The scrolling ancestor — .project-board, not the grid inside it.
  const scrollRef = useRef<HTMLDivElement | null>(null);
  // The deadline handles live in a layer ABOVE the board, not inside it: a
  // scroll container clips at its own edge, so a handle that has to straddle
  // that edge cannot be a child of it. The layer is placed over the board and
  // scrolled by hand, which keeps the handles glued to their rows.
  // The layer's containing block, measured against explicitly rather than via
  // offsetParent — which is whatever happens to be positioned up the tree.
  const wrapRef = useRef<HTMLDivElement | null>(null);
  const [boardBox, setBoardBox] = useState<{
    left: number;
    top: number;
    height: number;
    gridTop: number;
  } | null>(null);

  const setDrag = (v: typeof drag) => {
    dragRef.current = v;
    setDragState(v);
  };
  // A slot being dragged to another week / epic. It follows the pointer as a
  // preview and only writes on release.
  const [move, setMoveState] = useState<{
    card: CardModel;
    span: number;
    row: number;
    grabbed: { project: string; epic: string };
    epic: EpicRef;
  } | null>(null);
  const setMove = (v: typeof move) => {
    moveRef.current = v;
    setMoveState(v);
  };
  // The press behind a possible drag: held in a ref so that merely pressing a
  // card re-renders nothing, and so a click that never travels stays a click.
  const press = useRef<{
    card: CardModel;
    row: number;
    span: number;
    grabbed: { project: string; epic: string };
    grab: number;
    x: number;
    y: number;
  } | null>(null);

  const epicAt = (clientX: number): EpicRef | null => {
    const grid = gridRef.current;
    if (!grid) {
      return null;
    }
    const heads = grid.querySelectorAll(".project-epic-head:not(.project-epic-add)");
    for (let i = 0; i < heads.length; i++) {
      const r = heads[i].getBoundingClientRect();
      if (clientX >= r.left && clientX < r.right) {
        return epics[i] ?? null;
      }
    }
    return null;
  };

  const rowAt = (clientY: number): number => {
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
  };

  const beginDrag = (
    epic: EpicRef,
    week: number,
    e: React.PointerEvent,
    resize?: CardModel,
    edge: "top" | "bottom" = "bottom",
  ) => {
    e.preventDefault();
    e.stopPropagation();
    // "from" is the edge that stays put: the card's first row when the bottom
    // is pulled, its last row when the top is.
    setDrag({ epic, from: week, to: week, resize, edge });
    armGesture(moveDrag, endDrag, () => setDrag(null));
  };

  const moveDrag = (e: PointerEvent) => {
    const drag = dragRef.current;
    if (!drag) {
      return;
    }
    const row = rowAt(e.clientY);
    if (row !== drag.to) {
      // A new slot may be pulled either way from where it was started. A
      // resize may not cross the edge that is staying put: the bottom stops
      // at the first row, the top stops at the last one.
      let to = row;
      if (drag.resize) {
        to = drag.edge === "top"
          ? Math.min(drag.from, row)
          : Math.max(drag.from, row);
      }
      setDrag({ ...drag, to });
    }
  };

  const endDrag = () => {
    const drag = dragRef.current;
    if (!drag) {
      return;
    }
    const { epic, from, to, resize } = drag;
    setDrag(null);
    if (resize && drag.edge === "top") {
      // The top edge moves the slot's START; its end stays. The row follows
      // the start date on the server, so only the date is sent.
      const start = weeks[Math.min(from, to)];
      if (start === resize.startDate) {
        return;
      }
      const prev = { startDate: resize.startDate, week: resize.week };
      patchCard(resize.itemId, { startDate: start, week: start });
      void provider
        .patchCard(resize.itemId, { dates: { start } })
        .then(addCard)
        .catch((err: unknown) => {
          patchCard(resize.itemId, prev);
          onError(errText(err));
        });
      return;
    }
    if (resize) {
      // Stretch (or shrink) an existing slot: its anchor week stays, the end
      // lands on the Friday of the released week.
      const end = addDays(weeks[Math.max(from, to)], 4);
      if (end === resize.day) {
        return;
      }
      const prev = { day: resize.day };
      patchCard(resize.itemId, { day: end });
      void provider
        .patchCard(resize.itemId, { dates: { end } })
        .then(addCard)
        .catch((err: unknown) => {
          patchCard(resize.itemId, prev);
          onError(errText(err));
        });
      return;
    }
    // Pulled upward, "from" is the later row: the slot is the span between
    // them, whichever way the pointer went.
    setDraft({ epic, from: Math.min(from, to), to: Math.max(from, to) });
  };

  // armGesture wires a gesture's move/up/cancel to the window for its whole
  // life and tears them down when it ends, however it ends.
  const armGesture = (
    onMove: (e: PointerEvent) => void,
    onUp: () => void,
    onCancel: () => void,
  ) => {
    const up = () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", up);
      window.removeEventListener("pointercancel", cancel);
      onUp();
    };
    const cancel = () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", up);
      window.removeEventListener("pointercancel", cancel);
      onCancel();
    };
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", up);
    window.addEventListener("pointercancel", cancel);
  };

  const beginMove = (
    card: CardModel,
    row: number,
    span: number,
    grabbed: { project: string; epic: string },
    e: React.PointerEvent<HTMLDivElement>,
  ) => {
    // Only the slot's BODY drags. A press that started on a control inside it
    // (the team badge, delete, the resize grip) must reach that control: the
    // press bubbles up here, and capturing it — plus preventDefault — used to
    // swallow the click entirely, so the badge could never be pressed.
    if ((e.target as HTMLElement).closest("button, input, .project-slot-resize")) {
      return;
    }
    e.preventDefault();
    // Remember WHERE on the card the press landed. A press is not yet a drag:
    // it becomes one only once the pointer travels, so a stray click leaves
    // the card exactly where it was.
    press.current = {
      card,
      row,
      span,
      grabbed,
      grab: Math.max(0, rowAt(e.clientY) - row),
      x: e.clientX,
      y: e.clientY,
    };
    armGesture(moveMove, endMove, () => {
      press.current = null;
      setMove(null);
    });
  };

  const moveMove = (e: PointerEvent) => {
    const p = press.current;
    if (!p) {
      return;
    }
    const move = moveRef.current;
    if (!move) {
      if (Math.abs(e.clientX - p.x) < DRAG_SLOP && Math.abs(e.clientY - p.y) < DRAG_SLOP) {
        return;
      }
      setMove({
        card: p.card,
        span: p.span,
        row: p.row,
        grabbed: p.grabbed,
        epic: { name: p.grabbed.epic, project: p.grabbed.project },
      });
      return;
    }
    // The card follows the point it was grabbed by, not its own top edge:
    // grabbing a six-week slot by its last week and nudging it must not fling
    // it five weeks up the board.
    const row = Math.max(
      0,
      Math.min(rowAt(e.clientY) - p.grab, weeks.length - p.span),
    );
    const epic = epicAt(e.clientX) ?? move.epic;
    if (row !== move.row || colKey(epic.project, epic.name) !== colKey(move.epic.project, move.epic.name)) {
      setMove({ ...move, row, epic });
    }
  };

  const endMove = () => {
    press.current = null;
    const move = moveRef.current;
    if (!move) {
      return;
    }
    const { card, span, row, epic, grabbed } = move;
    setMove(null);
    const week = weeks[row];
    const end = addDays(weeks[Math.min(row + span - 1, weeks.length - 1)], 4);
    const target = { project: epic.project, epic: epic.name };
    const plan = slotDragPlan(card, grabbed, target);
    const weekChanged = week !== card.week;
    if (plan.kind === "dates" && !weekChanged) {
      return;
    }
    const prev = {
      week: card.week,
      epic: card.epic,
      project: card.project,
      startDate: card.startDate,
      day: card.day,
      mirrors: card.mirrors,
    };
    const rollback = (err: unknown) => {
      patchCard(card.itemId, prev);
      onError(errText(err));
    };
    // The date change is the card's, whatever placement was dragged.
    const dates: Partial<CardModel> = { week, startDate: week, day: end };
    switch (plan.kind) {
      case "dates": {
        patchCard(card.itemId, dates);
        void provider
          .patchCard(card.itemId, { dates: { start: week, end } })
          .then(addCard)
          .catch(rollback);
        return;
      }
      case "refileHome": {
        patchCard(card.itemId, {
          ...dates,
          epic: target.epic,
          project: target.project,
          // The server drops a mirror the home lands on; so does the patch,
          // or the grid draws the slot twice until the reload.
          mirrors: slotDropMirrors(card, grabbed, target, plan.kind),
        });
        void provider
          .patchCard(card.itemId, {
            epic: target.epic,
            project: target.project,
            // No plan.week: the server takes the row from dates.start.
            dates: { start: week, end },
          })
          .then(addCard)
          .catch(rollback);
        return;
      }
      case "moveMirror":
      case "collapseMirror": {
        // A dragged MIRROR copy moves its own entry — never the home. The
        // mirror is added before the old one goes, so a failure half-way
        // never leaves the card short a placement; a drop onto the home
        // simply folds the mirror away.
        patchCard(card.itemId, {
          ...(weekChanged ? dates : {}),
          mirrors: slotDropMirrors(card, grabbed, target, plan.kind),
        });
        void settleMirrorDrop(
          provider,
          card.itemId,
          grabbed,
          target,
          plan.kind,
          weekChanged ? { start: week, end } : null,
          {
            restore: () => patchCard(card.itemId, prev),
            reload,
            onError,
            errMessage: errText,
          },
        );
        return;
      }
    }
  };

  const cancelDraft = () => setDraft(null);

  // The roster's side of every domain question — the board's primary and
  // its projects' repositories — built once and handed to the rules, which
  // are the only place that compares two stamps (placements.sameRepository).
  const roster = rosterOf(board);

  // columnDomain: which repository a column was declared in, as the board
  // states it (metadata.epics[].domain). The server asks the COLUMN, never
  // its project — one project name may be declared twice, with its columns
  // merged under a single entry (G13).
  const columnDomain = (col: { project: string; name: string }) =>
    board.epics.find((e) => e.project === col.project && e.name === col.name)?.domain ?? "";

  const createSlot = (title: string) => {
    if (!draft || !title.trim()) {
      setDraft(null);
      return;
    }
    // A card created here carries no team, so its repository is its
    // project's — or the primary, when the column has no project. A
    // project-less column of another repository can hold no such card,
    // and the server says so; the board does not start the gesture.
    if (!canCreateInColumn(
      { project: draft.epic.project, domain: columnDomain({ project: draft.epic.project, name: draft.epic.name }) },
      roster,
    )) {
      onError("A card created here would have no team, and this column is not in the repository its project names");
      setDraft(null);
      return;
    }
    const { epic, from, to } = draft;
    setDraft(null);
    const week = weeks[from];
    const start = week;
    const end = addDays(weeks[to], 4); // the Friday of the last week
    const tempId = `tmp-${new Date().toISOString()}`;
    addCard({
      itemId: tempId,
      title: title.trim(),
      assignees: [],
      epic: epic.name,
      project: epic.project,
      week,
      startDate: start,
      day: end,
      description: "",
      progress: 0,
    });
    const created = provider.createCard({
      title: title.trim(),
      epic: epic.name,
      project: epic.project,
      week,
      start,
      day: end,
    });
    // Anything done to the new slot before the create lands — dragging it,
    // stretching it, handing it to a team — waits here for the real uid
    // instead of erroring out with "card is still being created", which is
    // what every other board already does.
    registerPendingCard(
      tempId,
      created.then((c) => c.itemId),
    );
    created
      .then((c) => replaceCard(tempId, c))
      .catch((err: unknown) => {
        removeCard(tempId);
        onError(errText(err));
      });
  };

  // ---- columns ----------------------------------------------------------
  const [addingEpic, setAddingEpic] = useState(false);
  // The column being renamed in its header, and whether the project manager
  // dialog is open.
  const [renaming, setRenaming] = useState<string | null>(null);

  // The status line remembers whether it was left open, as the Team board's
  // weekly plan does.
  const [progressOpen, setProgressOpen] = useState(
    () => localStorage.getItem(LS_PROGRESS) === "true",
  );
  const toggleProgress = () => {
    setProgressOpen((open) => {
      localStorage.setItem(LS_PROGRESS, String(!open));
      return !open;
    });
  };
  // The week whose menu is open, and the deadline being dragged to another
  // week (its line follows the pointer and only writes on release).
  const [weekMenu, setWeekMenu] = useState<string | null>(null);
  const weekAnchor = useRef<HTMLElement | null>(null);
  const [dragLine, setDragLine] = useState<{
    project: string;
    from: string;
    row: number;
  } | null>(null);
  const [teamMenu, setTeamMenu] = useState<string | null>(null);
  // Only one team menu is open at a time, so a single anchor ref serves
  // whichever badge was last pressed.
  const teamAnchor = useRef<HTMLButtonElement | null>(null);

  // A column belongs to exactly one project, so adding one is only offered
  // when a single project is in view — otherwise there is no answer to
  // "which project does this column go in".
  // The single chip in view, or null when several (or none) are. "" is the
  // no-project bucket, which is a destination like any other — a column and a
  // deadline can both live there.
  const targetProject = filter?.length === 1 ? filter[0] : null;
  // Several plans on screen at once: that is when a line needs its project's
  // colour and a column header needs its project's badge.
  const multi = targetProject === null;

  const addEpic = (name: string) => {
    setAddingEpic(false);
    if (!name.trim() || targetProject === null) {
      return;
    }
    void provider
      .addEpic(name.trim(), targetProject)
      .catch((err: unknown) => onError(errText(err)));
  };

  // Roster writes return as soon as the server's cache has them; the Board
  // watch frame that follows repaints this tab and every other open one. No
  // reload — the same way a card edit on Me or Team never reloads.
  const deleteEpic = (col: EpicRef) => {
    if (!window.confirm(`Delete the epic “${col.name}”?`)) {
      return;
    }
    void provider
      .deleteEpic(col.name, col.project)
      .catch((err: unknown) => onError(errText(err)));
  };

  // Re-filing a column moves its cards with it: a card's column is the pair,
  // and half a move would leave them in a column that is not theirs.
  const setEpicProject = (col: EpicRef, to: string) => {
    if (to === col.project) {
      return;
    }
    void provider
      .setEpicProject(col.project, col.name, to)
      .catch((err: unknown) => onError(errText(err)));
  };

  const renameEpic = (col: EpicRef, to: string) => {
    setRenaming(null);
    if (!to.trim() || to.trim() === col.name) {
      return;
    }
    void provider
      .renameEpic(col.project, col.name, to.trim())
      .catch((err: unknown) => onError(errText(err)));
  };

  // Assigning a team hands the card to that team's weekly plan: the band is
  // what places it there, so an unbanded card gets the week-end band.
  const assignTeam = (card: CardModel, team: string | null) => {
    setTeamMenu(null);
    const prev = { team: card.team, plan: card.plan };
    patchCard(card.itemId, { team: team ?? undefined, plan: card.plan ?? (team ? "fri" : undefined) });
    void provider
      // Just the team: the server files the slot in that team's weekly plan.
      .patchCard(card.itemId, { team: team ?? "" })
      .then(addCard)
      .catch((err: unknown) => {
        patchCard(card.itemId, prev);
        onError(errText(err));
      });
  };

  // A deadline belongs to the project whose plan it is part of, so the week's
  // menu offers one entry per project in view — with several on screen there
  // is no single "the" deadline to set.
  const setDeadline = (week: string, projectName: string, on: boolean) => {
    setWeekMenu(null);
    const call = on
      ? provider.addDeadline(week, projectName)
      : provider.deleteDeadline(week, projectName);
    void call.catch((err: unknown) => onError(errText(err)));
  };

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
  }, [weeks.length, epics.length, sharedCol, padBack, padFwd]);


  const beginLineDrag = (
    project: string,
    week: string,
    row: number,
    e: React.PointerEvent,
  ) => {
    e.preventDefault();
    e.stopPropagation();
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    setDragLine({ project, from: week, row });
  };

  const moveLineDrag = (e: React.PointerEvent) => {
    if (!dragLine) {
      return;
    }
    const row = Math.min(rowAt(e.clientY), weeks.length - 1);
    if (row !== dragLine.row) {
      setDragLine({ ...dragLine, row });
    }
  };

  const endLineDrag = () => {
    if (!dragLine) {
      return;
    }
    const { project: dlProject, from, row } = dragLine;
    setDragLine(null);
    const to = weeks[row];
    if (!to || to === from) {
      return;
    }
    void provider
      .moveDeadline(dlProject, from, to)
      .catch((err: unknown) => onError(errText(err)));
  };

  // Done, and back again. Marking done is the board's own rule — the server
  // clears the stage and fills 100 — and the way back RESTORES what done
  // overwrote: the server reads the pre-done progress from the card's own
  // log, so an accidental done+undo round-trips instead of leaving the card
  // at 90 and painted "taken into work".
  const setDone = (card: CardModel, done: boolean) => {
    setTeamMenu(null);
    const prev = { stage: card.stage, progress: card.progress };
    if (done) {
      patchCard(card.itemId, {
        stage: card.stage === "recurrent" ? "recurrent" : undefined,
        progress: 100,
      });
      void provider
        .patchCard(card.itemId, { stage: "done" })
        .then(addCard)
        .catch((err: unknown) => {
          patchCard(card.itemId, prev);
          onError(errText(err));
        });
      return;
    }
    // Optimistically only the stage clears; the restored progress arrives
    // with the response — the client cannot know it.
    patchCard(card.itemId, { stage: undefined });
    void provider
      .reopen(card.itemId)
      .then(addCard)
      .catch((err: unknown) => {
        patchCard(card.itemId, prev);
        onError(errText(err));
      });
  };

  // subtaskTitle words the ↳ marker: the parent's title when the query
  // carried it, and an honest fallback when it did not.
  const subtaskTitle = (title: string | undefined) =>
    title ? `Subtask of «${title}»` : "Subtask — its parent is not on this board";

  // placementsFor: the slot menu's "Mirror to…" section. The same factory
  // the Me and Team boards use, so the three cannot drift apart — and the
  // only way to mirror a card that lives ONLY in a column: such a card
  // never joins a sprint, so it appears on no other board (TeamGrid hides
  // an epic card until it does), and its card menu exists nowhere else.
  const placementsFor = (card: CardModel): CardPlacements =>
    makeCardPlacements(card, board, {
      provider,
      patchCard,
      reload,
      onError,
      errMessage: errText,
    });

  // The slot's ×: remove the card from THIS column. A mirror goes, the
  // home hands over to its first mirror, an orphaned worked card survives
  // in the working area — only the true delete asks, naming the loss.
  const removeFromColumn = (card: CardModel, project: string, epic: string) => {
    const outcome = removeFromProjectOutcome(card, project, epic);
    // The server would refuse this pair — a column the card does not stand
    // in, or none at all — so nothing is sent and nothing is patched. It
    // takes a stale render to get here; falling through to the last arm
    // emptied the card's column on the screen over a request the server
    // never accepted, and the truth came back only with the reload.
    if (outcome === "refused") {
      // Only a stale render gets here — the card is not in the column the
      // click named — so the screen is what is wrong: re-read it rather
      // than leave a click that did nothing and said nothing.
      reload();
      return;
    }
    if (outcome === "delete") {
      // The server cascades to the linked review card: the question names
      // everything that goes, or the person agrees to less than happens.
      const linkedReview = board.cards.find((c) => c.reviewOf === card.itemId);
      const warning =
        deleteWarning(card, linkedReview?.title ?? null) ?? `Delete "${card.title}"?`;
      if (!window.confirm(warning)) {
        return;
      }
      removeCard(card.itemId);
      // …and everything the server takes with it: the linked review card
      // goes, the subtasks are FREED into standalone cards. The other two
      // boards mirror both; here the reload papered over a window showing
      // state the server does not hold.
      if (linkedReview) {
        removeCard(linkedReview.itemId);
      }
      for (const f of freeSubtasks(board.cards, card.itemId)) {
        patchCard(f.itemId, f.patch);
      }
    } else if (outcome === "unmirror") {
      patchCard(card.itemId, {
        mirrors: (card.mirrors ?? []).filter(
          (m) => !(m.project === project && m.epic === epic),
        ),
      });
    } else if (outcome === "promote") {
      const heir = (card.mirrors ?? [])[0];
      patchCard(card.itemId, {
        project: heir.project,
        epic: heir.epic,
        mirrors: (card.mirrors ?? []).slice(1),
      });
    } else {
      // orphan: off the Project board and the weekly plan, kept by its work.
      patchCard(card.itemId, {
        project: undefined,
        epic: undefined,
        plan: undefined,
        week: undefined,
      });
    }
    void provider
      .removeFromProject(card.itemId, project, epic)
      .then(() => reload())
      .catch((err: unknown) => {
        onError(errText(err));
        reload();
      });
  };

  // While a slot is being stretched, its span follows the pointer — the drag
  // has to be visible on the thing being dragged, not only on the cursor.

  // Cards per column with their row spans and, when they overlap in time, the
  // lane each one sits in: cards sharing weeks split the column's width
  // between them instead of covering each other up.
  const slots = useMemo(() => {
    type Slot = {
      // null is the draft being pulled out right now: it takes a lane like
      // any other slot, so a new card never lands on top of an existing one.
      card: CardModel | null;
      row: number;
      span: number;
      lane: number;
      lanes: number;
      // How many lanes wide the slot is drawn: it grows rightwards over the
      // lanes that are free for every row it covers.
      width: number;
    };
    const byCol = new Map<string, Slot[]>();
    // A second, preview-less collection exists only while an edge is being
    // pulled: it tells the pin which lane the slot rests in.
    const rest = drag?.resize ? new Map<string, Slot[]>() : null;
    if (weeks.length === 0) {
      return byCol;
    }
    for (const c of cards) {
      // The row is the START date's week. A card's own stored week is only a
      // fallback for one that has no start at all.
      const from = c.startDate || c.week;
      if (!c.epic || !from) {
        continue;
      }
      const anchor = mondayOf(from);
      let row = weeksBetween(weeks[0], anchor);
      if (row < 0 || row >= weeks.length) {
        continue;
      }
      const endMon = c.day && c.day > anchor ? mondayOf(c.day) : anchor;
      let span = Math.max(1, Math.min(weeksBetween(anchor, endMon) + 1, weeks.length - row));
      let k = colKey(c.project ?? "", c.epic);
      // A card being dragged is packed WHERE IT WOULD LAND, not where it came
      // from: the lanes are what make room for it, so computing them from its
      // old place drew it on top of whatever already sat in the new one.
      if (move?.card.itemId === c.itemId) {
        row = move.row;
        span = Math.max(1, Math.min(move.span, weeks.length - row));
        k = colKey(move.epic.project, move.epic.name);
      } else if (drag?.resize?.itemId === c.itemId) {
        // The same for an edge being pulled: the slot's preview is its real
        // extent for as long as the pull lasts.
        if (drag.edge === "top") {
          const last = row + span - 1;
          const top = Math.max(0, Math.min(drag.to, last));
          span = last - top + 1;
          row = top;
        } else {
          span = Math.max(1, Math.min(drag.to - row + 1, weeks.length - row));
        }
      }
      const list = byCol.get(k) ?? [];
      list.push({ card: c, row, span, lane: 0, lanes: 1, width: 1 });
      byCol.set(k, list);
      // A mirrored card stands in every one of its columns: the same entry,
      // the same shared dates, once per placement — except while THIS card
      // is being dragged, when only the moving preview is drawn.
      if (move?.card.itemId !== c.itemId) {
        for (const mi of c.mirrors ?? []) {
          const mk = colKey(mi.project, mi.epic);
          const ml = byCol.get(mk) ?? [];
          ml.push({ card: c, row, span, lane: 0, lanes: 1, width: 1 });
          byCol.set(mk, ml);
        }
      }
      if (rest) {
        // The same card at REST — pre-preview geometry, for the lane pin.
        // Every placement, mirrors included: pulling an edge in a MIRROR
        // column must pin against that column's resting neighbours, not
        // find the map empty and let the slot hop lanes under the pointer.
        const rr = weeksBetween(weeks[0], anchor);
        if (rr >= 0 && rr < weeks.length) {
          const rs = Math.max(1, Math.min(weeksBetween(anchor, endMon) + 1, weeks.length - rr));
          const cols = [colKey(c.project ?? "", c.epic)].concat(
            (c.mirrors ?? []).map((mi) => colKey(mi.project, mi.epic)),
          );
          for (const rk of cols) {
            const rl = rest.get(rk) ?? [];
            rl.push({ card: c, row: rr, span: rs, lane: 0, lanes: 1, width: 1 });
            rest.set(rk, rl);
          }
        }
      }
    }
    if (draft) {
      const k = colKey(draft.epic.project, draft.epic.name);
      const list = byCol.get(k) ?? [];
      list.push({
        card: null,
        row: draft.from,
        span: draft.to - draft.from + 1,
        lane: 0,
        lanes: 1,
        width: 1,
      });
      byCol.set(k, list);
    }
    // Lanes are worked out per CLUSTER of overlapping slots, not per column:
    // splitting the whole column because two slots happen to share a fortnight
    // would leave every unrelated card at half width for no reason. A pin
    // holds one card in a KNOWN lane: while an edge is being pulled, the slot
    // must not hop lanes under the pointer — the neighbours pack around it.
    const pack = (
      lists: Map<string, Slot[]>,
      pin?: { id: string; lane: number; row: number; span: number },
    ) => {
      for (const list of lists.values()) {
        list.sort((a, b) => a.row - b.row || b.span - a.span);
        let cluster: typeof list = [];
        let laneEnd: number[] = [];
        const close = () => {
          const lanes = laneEnd.length;
          for (const s of cluster) {
            s.lanes = lanes;
            // A cluster's lane count is what the busiest week in it needs, but
            // a slot beside a quiet week has room the busy weeks do not: it
            // grows rightwards over every lane that is free for ALL of its own
            // rows. Without this a card sat at half width beside empty space,
            // as if the space were spoken for.
            let width = 1;
            while (s.lane + width < lanes) {
              const nextLane = s.lane + width;
              const taken = cluster.some(
                (o) =>
                  o !== s &&
                  o.lane === nextLane &&
                  o.row < s.row + s.span &&
                  s.row < o.row + o.span,
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
        // Whether a slot's rows overlap the pinned card's previewed extent.
        const meetsPin = (s: Slot) =>
          !!pin && s.row < pin.row + pin.span && pin.row < s.row + s.span;
        for (const s of list) {
          // A slot that begins after everything so far has ended starts a new
          // cluster: it shares its weeks with nothing before it.
          if (laneEnd.length && s.row >= Math.max(...laneEnd)) {
            close();
          }
          let lane: number;
          if (pin && s.card?.itemId === pin.id) {
            lane = pin.lane;
            while (laneEnd.length <= lane) {
              laneEnd.push(0);
            }
          } else {
            // First fit — but never into the pinned lane while sharing rows
            // with the pinned card, even before it is placed.
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
    };
    let pin: { id: string; lane: number; row: number; span: number } | undefined;
    if (rest && drag?.resize) {
      pack(rest);
      for (const list of rest.values()) {
        const own = list.find((s) => s.card?.itemId === drag.resize?.itemId);
        if (own) {
          const preview = [...byCol.values()]
            .flat()
            .find((s) => s.card?.itemId === drag.resize?.itemId);
          if (preview) {
            pin = { id: drag.resize.itemId, lane: own.lane, row: preview.row, span: preview.span };
          }
        }
      }
    }
    pack(byCol, pin);
    return byCol;
  }, [cards, weeks, draft, move, drag]);

  // One index of the board, for the rules that need to look a parent up.
  const byId = useMemo(
    () => new Map(board.cards.map((c) => [c.itemId, c])),
    [board.cards],
  );

  // How far along each column is, and the view as a whole. A card counts
  // once: a subtask whose PARENT stands in the same project is left to the
  // parent, whose progress already derives from its children — otherwise
  // the column, the overall bar and the project line disagree on one
  // screen.
  const counted = useMemo(
    () =>
      cards.filter(
        (c) => drawnAsSlot(c) && countedForProgress(c, byId, { project: c.project ?? "" }),
      ),
    [cards, byId],
  );
  const colProgress = useMemo(() => {
    const byCol = new Map<string, CardModel[]>();
    for (const c of cards.filter(drawnAsSlot)) {
      // Every column the card is DRAWN in, mirrors included: keyed by the
      // home pair alone, a mirror column reported a total of zero while
      // showing slots, and the header disagreed with the columns beneath
      // it on one screen. The de-duplication is asked PER COLUMN for the
      // same reason — a parent mirrored into this one answers for its
      // child here, whatever its home epic says.
      for (const col of columnsOf(c)) {
        if (!countedForProgress(c, byId, col)) {
          continue;
        }
        const k = colKey(col.project, col.epic);
        byCol.set(k, [...(byCol.get(k) ?? []), c]);
      }
    }
    const out = new Map<string, ReturnType<typeof progressOf>>();
    for (const [k, list] of byCol) {
      out.set(k, progressOf(list));
    }
    return out;
  }, [cards, byId]);
  const overall = useMemo(() => progressOf(counted), [counted]);

  // Every project's progress, filter or no filter: the point of opening the
  // status line is to see the ones you are NOT looking at. The no-project
  // bucket appears only when it actually holds work.
  const allProgress = useMemo(() => {
    const byProject = new Map<string, CardModel[]>();
    for (const c of board.cards) {
      if (!drawnAsSlot(c) || !countedForProgress(c, byId, { project: c.project ?? "" })) {
        continue;
      }
      const k = c.project ?? "";
      byProject.set(k, [...(byProject.get(k) ?? []), c]);
    }
    return [...board.projects, ""]
      .filter((p) => p !== "" || (byProject.get("")?.length ?? 0) > 0)
      .map((p) => ({ project: p, ...progressOf(byProject.get(p) ?? []) }));
  }, [board.cards, board.projects, byId]);

  const todayRow = weeks.indexOf(thisMonday);

  // Open on today. A plan reaches months back, and the board opened at its
  // very first week — someone arriving had to scroll past a spent quarter to
  // find out where the team is. Once per project shown: after that the scroll
  // is the reader's, and pressing "earlier weeks" must not yank it back.
  // Which selection is on screen — the identity the open-on-today scroll
  // remembers, so switching projects opens on today again.
  const selectionKey = filter ? [...filter].sort().join("\u0000") : "*";
  const scrolledFor = useRef<string | null>(null);
  useLayoutEffect(() => {
    const scroller = scrollRef.current;
    if (!scroller || !boardBox || todayRow < 0 || scrolledFor.current === selectionKey) {
      return;
    }
    const top = Math.max(0, boardBox.gridTop + HEADER_PX + (todayRow - 2) * rowH);
    // After the paint: switching project rebuilds the grid, and a scroll set
    // before it has its new height is clamped to the old one. The mark is set
    // in the callback, not here — this effect re-runs as the new board is
    // measured, and a mark set up front would cancel the pending frame and
    // then refuse to schedule another, which is why switching never moved.
    const frame = requestAnimationFrame(() => {
      scrolledFor.current = selectionKey;
      scroller.scrollTop = top;
    });
    return () => cancelAnimationFrame(frame);
  }, [boardBox, todayRow, selectionKey, weeks.length]);

  // The projects the week menu can act on: those in view, or the whole roster
  // when nothing is filtered. The no-project bucket is included in both cases
  // — deadlines belonging to no project exist and have to be manageable.
  const menuProjects = filter ?? [...board.projects, ""];

  // The lane the packing gave the draft, so the new card sits beside whatever
  // already occupies those weeks rather than on top of it.
  const draftPacked = draft
    ? (slots.get(colKey(draft.epic.project, draft.epic.name)) ?? []).find(
        (s) => s.card === null,
      )
    : undefined;
  const draftLane = draftPacked?.lane ?? 0;
  const draftLanes = draftPacked?.lanes ?? 1;
  const draftWidth = draftPacked?.width ?? 1;

  // Where a column sits among the visible ones (-1 while it is filtered out).
  const colIndex = (e: EpicRef) =>
    epics.findIndex((x) => colKey(x.project, x.name) === colKey(e.project, e.name));

  // Everything below renders through ONE return: the empty board differs only
  // in its body. An early return here used to leave the shared furniture — the
  // chip row, the manage dialog — out of the empty state, where they are
  // exactly what a person needs to get started.
  // A board with no columns stays on the empty state even while the first one
  // is being named: the field belongs there, in the middle of the screen.
  // Flipping to the grid put it in the 34px gutter beside a table that does
  // not exist yet — six pixels tall, in the far corner.
  const empty = epics.length === 0;

  return (
    <div className="project" ref={wrapRef}>
      {/* The same toolbar row the other boards wear — the chips are a shared
          control and must not look like a stray line of text here. */}
      <div className="board-toolbar">
        <TeamChips
          label="Project"
          entity="project"
          teams={board.projects}
          selectedKeys={filter}
          onSelect={selectFilter}
          onAdd={() => undefined}
          onRemove={() => undefined}
          canManage={false}
          onManage={onManageProjects}
          noneChip={looseEpics ? "No project" : undefined}
        />
        {!empty && <ZoomControl zoom={zoom} onChange={setZoom} />}
      </div>
      {empty && (
        <div className="project-empty">
          <p>
            The Project board maps a project&rsquo;s epics (columns) across
            weeks (rows).
          </p>
          {board.projects.length === 0 ? (
            <p className="project-empty-hint">
              Start with a project — “manage” above adds one.
            </p>
          ) : targetProject !== null ? (
            addingEpic ? (
              <input
                type="text"
                className="add-card-input project-empty-input"
                autoFocus
                placeholder={
                  targetProject ? `Epic in ${targetProject}…` : "Epic with no project…"
                }
                onKeyDown={(ev) => {
                  if (ev.key === "Enter") {
                    addEpic((ev.target as HTMLInputElement).value);
                  } else if (ev.key === "Escape") {
                    setAddingEpic(false);
                  }
                }}
                onBlur={(ev) => addEpic(ev.target.value)}
              />
            ) : (
              <button
                type="button"
                className="btn btn-primary"
                onClick={() => setAddingEpic(true)}
              >
                + Add the first epic{targetProject ? ` of ${targetProject}` : ""}
              </button>
            )
          ) : (
            <p className="project-empty-hint">
              Pick one project above to add its first epic.
            </p>
          )}
        </div>
      )}
      {!empty && (
      <div className="project-board" ref={scrollRef}>
      <button
        type="button"
        className="project-more"
        onClick={showEarlier}
        title={`Show ${WEEK_STEP} more weeks before`}
      >
        ↑ earlier weeks
      </button>
      <div
        className="project-grid"
        ref={gridRef}
        style={{
          // Until the columns are dragged they share the room; once dragged
          // they all take the width that was chosen.
          // 54px is what "17 Aug" needs and no more — the ISO number that
          // used to share this column is gone.
          // Every column is sized explicitly: a column with a width of its
          // own takes its ratio of the shared width, the rest take the shared
          // width itself. Text is never scaled — only the room around it.
          gridTemplateColumns: `54px ${epics
            .map((e) => {
              const f = colFactors[colKey(e.project, e.name)];
              return `${Math.round(sharedCol * (f ?? 1))}px`;
            })
            .join(" ")} 34px`,
          gridTemplateRows: `26px repeat(${weeks.length}, ${rowH}px)`,
        }}
      >
        {/* header row */}
        <div className="project-corner">
          {epics.some((e) => colFactors[colKey(e.project, e.name)] !== undefined) && (
            <button
              type="button"
              className="project-cols-reset"
              title="Give the columns on this board the same width again (columns not shown keep theirs)"
              onClick={() => {
                const next = { ...factorsRef.current };
                for (const e of epics) {
                  delete next[colKey(e.project, e.name)];
                }
                factorsRef.current = next;
                setColFactorsState(next);
                localStorage.setItem(LS_COLF, JSON.stringify(next));
              }}
            >
              ⇥⇤
            </button>
          )}
        </div>
        {epics.map((e) => {
          const k = colKey(e.project, e.name);
          if (renaming === k) {
            return (
              <div key={k} className="project-epic-head">
                <input
                  type="text"
                  className="project-epic-input"
                  autoFocus
                  defaultValue={e.name}
                  onKeyDown={(ev) => {
                    if (ev.key === "Enter") {
                      renameEpic(e, (ev.target as HTMLInputElement).value);
                    } else if (ev.key === "Escape") {
                      setRenaming(null);
                    }
                  }}
                  onBlur={(ev) => renameEpic(e, ev.target.value)}
                />
              </div>
            );
          }
          return (
            <div
              key={k}
              className={`project-epic-head project-epic-head-movable${
                dragCol === k ? " project-epic-head-dragging" : ""
              }`}
              title={`${e.name} — ${e.project || "no project"} · double-click to rename · drag to reorder`}
              onDoubleClick={() => setRenaming(k)}
              onPointerDown={(ev) => beginColDrag(ev, e)}
              onPointerMove={moveColDrag}
              onPointerUp={endColDrag}
              onPointerCancel={() => {
                // A cancelled gesture must not leave the preview standing:
                // the board's own order is the truth again.
                colDrag.current = null;
                setDragCol(null);
                setOrder(null);
              }}
            >
              <span className="project-epic-name">{e.name}</span>
              {(colProgress.get(k)?.total ?? 0) > 0 && (
                <span
                  className="project-epic-pct"
                  title={`${colProgress.get(k)?.done} of ${colProgress.get(k)?.total} cards done`}
                >
                  {colProgress.get(k)?.pct}%
                </span>
              )}
              {/* With several projects on screen the column header alone is
                  ambiguous, so it carries its project — as the same round
                  badge a team wears on a card, not as a second line of text
                  competing with the column's own name. Inside one project the
                  badge would repeat on every column and is left off. */}
              {/* A column cannot change REPOSITORY — its stub is written
                  back to the backend that holds it (G57) — so only the
                  projects of its own repository can take it; the rest are
                  refusals the picker has no business offering. */}
              <ProjectPicker
                current={e.project}
                projects={projectsAColumnCanJoin(
                  columnDomain(e),
                  board.projects,
                  board.projectDomains,
                  e.project,
                  roster,
                )}
                entity="epic"
                onPick={(to) => setEpicProject(e, to)}
              />
              <button
                type="button"
                className="card-action project-epic-del"
                title="Delete the epic (must be empty)"
                onClick={() => deleteEpic(e)}
              >
                ×
              </button>
              {/* The border is THIS column's grip: dragging it widens this
                  column alone and remembers the size as a ratio to the rest,
                  so it survives a zoom and a change of selection. Dragged
                  back near the others it lets the ratio go. */}
              <span
                className="project-col-resize"
                title="Drag to size this column · double-click to match the rest"
                onPointerDown={beginResize(k)}
                onPointerMove={moveResize}
                onPointerUp={endResize}
                onPointerCancel={() => setResizing(null)}
                onDoubleClick={(ev) => {
                  ev.stopPropagation();
                  setColFactor(k, null);
                }}
              />
            </div>
          );
        })}
        <div className="project-epic-head project-epic-add">
          {addingEpic ? (
            <input
              type="text"
              className="project-epic-input"
              autoFocus
              placeholder={targetProject ? `Epic in ${targetProject}…` : "Epic with no project…"}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  addEpic((e.target as HTMLInputElement).value);
                } else if (e.key === "Escape") {
                  setAddingEpic(false);
                }
              }}
              onBlur={(e) => addEpic(e.target.value)}
            />
          ) : (
            <button
              type="button"
              className="card-action"
              title={
                targetProject
                  ? `Add an epic to ${targetProject}`
                  : targetProject === null
                    ? "Pick one project first — a column belongs to a project"
                    : "Add an epic that belongs to no project"
              }
              disabled={targetProject === null}
              onClick={() => setAddingEpic(true)}
            >
              +
            </button>
          )}
        </div>

        {/* week label column + row stripes */}
        {weeks.map((w, i) => (
          <div
            key={w}
            className={`project-week${i === todayRow ? " project-week-today" : ""}`}
            style={{ gridRow: i + 2, gridColumn: 1 }}
            title={`ISO week ${isoWeekNo(w)} · click for the deadline`}
            onClick={(ev) => {
              weekAnchor.current = ev.currentTarget;
              setWeekMenu(weekMenu === w ? null : w);
            }}
          >
            <span className="project-week-date">{weekLabel(w)}</span>
          </div>
        ))}

        {/* cells: one per epic × week, the drag surface */}
        {epics.map((e, col) =>
          weeks.map((w, row) => (
            <div
              key={`${colKey(e.project, e.name)}/${w}`}
              className={`project-cell${row === todayRow ? " project-cell-today" : ""}${
                drag &&
                !drag.resize &&
                colKey(drag.epic.project, drag.epic.name) === colKey(e.project, e.name) &&
                row >= Math.min(drag.from, drag.to) &&
                row <= Math.max(drag.from, drag.to)
                  ? " project-cell-drag"
                  : ""
              }`}
              style={{ gridRow: row + 2, gridColumn: col + 2 }}
              onPointerDown={(ev) => beginDrag(e, row, ev)}
              onPointerLeave={() => {
                if (!drag) {
                  cancelNudgeTimer();
                  setNudged(null);
                }
              }}
              onPointerCancel={() => setDrag(null)}
            />
          )),
        )}

        {/* deadlines: one line per week, dragged by the dot on its left */}
        {board.deadlines
          .filter((d) => !filter || filter.includes(d.project))
          .map((d) => {
            const dragging =
              dragLine?.from === d.week && dragLine.project === d.project;
            const at = dragging ? dragLine.row : weeks.indexOf(d.week);
            if (at < 0 || at >= weeks.length) {
              return null;
            }
            // Inside one project the line is simply the deadline — plain red.
            // With several projects on screen there are several plans at once,
            // so each line takes its project's colour and steps back, or the
            // grid turns into a stack of red bars nobody can attribute.
            const colour = multi && d.project ? teamColor(d.project) : undefined;
            const tone = `${dragging ? " project-deadline-dragging" : ""}${
              multi ? " project-deadline-muted" : ""
            }`;
            const key = `${d.project}\u0000${d.week}`;
            // Two segments, because one element cannot be both above the
            // sticky week column (to cross the dates) and below the cards (so
            // it never cuts one in half). The head owns the dates and the
            // handle and stays put while the plan scrolls; the body runs the
            // width of the columns, behind them.
            return [
              <div
                key={`${key}\u0000head`}
                className={`project-deadline project-deadline-head${tone}`}
                style={{
                  gridRow: at + 2,
                  gridColumn: 1,
                  ...(colour ? { borderTopColor: colour } : {}),
                }}
              >
                {/* The handle belongs to the line, not to a layer above the
                    board: inside the grid it moves with the table itself —
                    through the overscroll bounce too, which no amount of
                    following from outside could manage — while the head's
                    stickiness keeps it in place as the columns scroll past. */}
                <span
                  className={`project-deadline-dot${dragging ? " project-deadline-dot-dragging" : ""}`}
                  style={colour ? { background: colour } : undefined}
                  title={`${d.project || "no project"} deadline — drag to another week · ${weekLabel(weeks[at])}`}
                  onPointerDown={(ev) => beginLineDrag(d.project, d.week, at, ev)}
                  onPointerMove={moveLineDrag}
                  onPointerUp={endLineDrag}
                  onPointerCancel={() => setDragLine(null)}
                />
              </div>,
              // Stops before the trailing "add a column" gutter: there is no
              // plan out there for a line to cross.
              <div
                key={`${key}\u0000body`}
                className={`project-deadline project-deadline-body${tone}`}
                style={{
                  gridRow: at + 2,
                  gridColumn: "2 / -2",
                  ...(colour ? { borderTopColor: colour } : {}),
                }}
              />,
            ];
          })}

        {/* the create draft, in the lane the packing gave it */}
        {draft && (
          <div
            className="project-slot project-slot-draft"
            style={{
              gridColumn: colIndex(draft.epic) + 2,
              gridRow: `${draft.from + 2} / span ${draft.to - draft.from + 1}`,
              ...(draftLanes > 1
                ? {
                    width: `calc(${(100 / draftLanes) * draftWidth}% - 2px)`,
                    marginLeft: `${(100 / draftLanes) * draftLane}%`,
                  }
                : {}),
            }}
          >
            <input
              type="text"
              className="project-slot-input"
              autoFocus
              placeholder="New card…"
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  createSlot((e.target as HTMLInputElement).value);
                } else if (e.key === "Escape") {
                  cancelDraft();
                }
              }}
              onBlur={(e) => createSlot(e.target.value)}
            />
          </div>
        )}

        {/* the slots */}
        {epics.map((e, col) =>
          (slots.get(colKey(e.project, e.name)) ?? [])
            .filter((s): s is typeof s & { card: CardModel } => s.card !== null)
            .map(({ card, row, span, lane, lanes, width: laneWidth }) => (
            <div
              key={`${colKey(e.project, e.name)}\u0000${card.itemId}`}
              className={`project-slot ${slotTone(card, today)}${
                movingSlot(move, card.itemId, { project: e.project, epic: e.name })
                  ? " project-slot-moving"
                  : ""
              }${nudged === card.itemId ? " project-slot-nudged" : ""}`}
              // A card that fills its column leaves nowhere to start the next
              // one. Hovering its right edge steps it aside just enough to
              // uncover the cell beneath, which is already the drag surface —
              // so a new card is pulled out beside an existing one without
              // moving anything or opening a menu.
              onPointerMove={(ev) => {
                if (move || drag) {
                  return;
                }
                // The card's own controls live at that same right edge — the
                // team badge, the delete button. Stepping aside under a
                // pointer that came to press one of them shrinks the card out
                // from under the click.
                if (
                  (ev.target as HTMLElement).closest(
                    "button, .project-slot-team, .project-slot-actions",
                  )
                ) {
                  cancelNudgeTimer();
                  setNudged((cur) => (cur === card.itemId ? null : cur));
                  return;
                }
                const r = (ev.currentTarget as HTMLElement).getBoundingClientRect();
                const inStrip = ev.clientX > r.right - NUDGE_PX * 2;
                if (!inStrip) {
                  cancelNudgeTimer();
                  setNudged((cur) => (cur === card.itemId ? null : cur));
                  return;
                }
                // Rest there first: a pointer merely crossing the card on its
                // way somewhere else must not shove it.
                if (nudged !== card.itemId && nudgeTimer.current === null) {
                  nudgeTimer.current = window.setTimeout(() => {
                    nudgeTimer.current = null;
                    setNudged(card.itemId);
                  }, NUDGE_DELAY_MS);
                }
              }}
              // Stepping aside puts the pointer in the gap that just opened —
              // which the browser reports as leaving the card. Undoing the
              // nudge there would close the gap under the pointer, reopen it,
              // and flicker forever; so a pointer inside the strip is not a
              // pointer that left.
              onPointerLeave={(ev) => {
                // A drag started in the strip holds the card aside until it
                // ends: the pointer travels far from the card by design, and
                // closing the gap mid-pull yanks the ground out from under it.
                if (drag) {
                  return;
                }
                cancelNudgeTimer();
                const r = (ev.currentTarget as HTMLElement).getBoundingClientRect();
                const intoTheGap =
                  ev.clientX >= r.right &&
                  ev.clientX <= r.right + NUDGE_PX + 2 &&
                  ev.clientY >= r.top &&
                  ev.clientY <= r.bottom;
                if (!intoTheGap) {
                  setNudged((cur) => (cur === card.itemId ? null : cur));
                }
              }}
              style={{
                // While dragged, the slot itself sits where it would land —
                // the preview IS the card. row/span/col come from the packing,
                // which places it there and moves its neighbours aside to make
                // the room.
                gridColumn: col + 2,
                gridRow: `${row + 2} / span ${span}`,
                borderLeftColor: card.team ? teamColor(card.team) : undefined,
                // Cards sharing weeks in one column split its width instead
                // of hiding each other. The width holds while the card is
                // being dragged too: widening it on press made the first click
                // of a double-click visibly inflate the card.
                // Cards sharing a column carry an EXPLICIT width, and a
                // margin cannot shrink that — the step aside has to come out
                // of the width itself, or a card with neighbours never moves.
                ...(lanes > 1
                  ? {
                      width: `calc(${(100 / lanes) * laneWidth}% - 2px${
                        nudged === card.itemId ? ` - ${NUDGE_PX}px` : ""
                      })`,
                      marginLeft: `${(100 / lanes) * lane}%`,
                    }
                  : {}),
              }}
              onPointerDown={(ev) =>
                beginMove(card, row, span, { project: e.project, epic: e.name }, ev)
              }
              onDoubleClick={() => onOpen(card)}
              title={card.title}
            >
              <span className="project-slot-title">
                {(card.mirrors ?? []).some(
                  (m) => m.project === e.project && m.epic === e.name,
                ) && (
                  <span
                    className="project-slot-mirror"
                    title={`Mirrored here from ${card.project} · ${card.epic}`}
                  >
                    ⧉
                  </span>
                )}
                {card.parent && (
                  <span
                    className="project-slot-subtask"
                    title={subtaskTitle(byId.get(card.parent)?.title)}
                  >
                    ↳
                  </span>
                )}
                {card.title}
                {cardDomainBadge(board.domains, card.domain) && (
                  <span
                    className="project-slot-domain"
                    title={`Stored in ${card.domain}`}
                  >
                    {card.domain}
                  </span>
                )}
              </span>
              <span className="project-slot-actions">
                <button
                  type="button"
                  className="card-action card-action-delete"
                  title="Delete"
                  onClick={(ev) => {
                    ev.stopPropagation();
                    removeFromColumn(card, e.project, e.name);
                  }}
                >
                  ×
                </button>
              </span>
              {/* The team badge is the card's owner, not an action: it stays
                  visible once assigned. Unassigned, it is a hover affordance.
                  Delete sits to its LEFT so the badge keeps the slot's edge —
                  it is the thing you point at, and it must not shift when the
                  hover-only actions appear. */}
              <button
                type="button"
                className={`project-slot-team${card.team ? "" : " project-slot-team-empty"}`}
                style={
                  card.team
                    ? { background: teamColor(card.team), color: "#fff" }
                    : undefined
                }
                // The badge is the slot's MENU HANDLE — "Mark as done", the
                // team list, "Mirror to…" all hang off it — so it stays
                // clickable for every card. A subtask's team follows its
                // parent (S9, and the server rewrites any other choice):
                // that makes the team LIST read-only inside the menu, not
                // the menu unreachable.
                title={
                  teamFollowsParent(card)
                    ? `Team: ${card.team || "none"} — follows the parent`
                    : card.team
                      ? `Team: ${card.team} — click to change`
                      : "Assign to a team"
                }
                onClick={(ev) => {
                  ev.stopPropagation();
                  teamAnchor.current = ev.currentTarget;
                  setTeamMenu(teamMenu === card.itemId ? null : card.itemId);
                }}
              >
                {card.team ? teamInitial(card.team) : "+"}
              </button>
              {card.stage && card.stage !== "done" && (
                <span
                  className="project-slot-stage"
                  style={{ background: STAGES[card.stage].color }}
                  title={STAGES[card.stage].label}
                />
              )}
              <Dropdown
                open={teamMenu === card.itemId}
                anchorRef={teamAnchor}
                onClose={() => setTeamMenu(null)}
                className="card-stage-menu"
              >
                <button
                  type="button"
                  className="card-stage-item project-menu-lead"
                  onClick={() => setDone(card, !complete(card))}
                >
                  <span
                    className="card-stage-dot"
                    style={{ background: STAGES.done.color }}
                  />
                  {complete(card) ? "Reopen" : "Mark as done"}
                </button>
                {/* Mirroring lives in this menu because the slot has no
                    other: a card in a column may be shown in a second one,
                    and for a card that never joined a sprint this is the
                    only place in the UI that can say so. */}
                {/* Only for the OPEN menu: React evaluates a Dropdown's
                    children whether or not it is open, and this walk is
                    O(projects × epics) per slot — on every frame of a
                    drag, since the slots recompute as the pointer moves. */}
                {(() => {
                  if (teamMenu !== card.itemId) {
                    return null;
                  }
                  const placements = placementsFor(card);
                  return placements.mirror ? (
                    <PlacementMenu
                      label="Mirror to…"
                      targets={placements.mirror}
                      onPick={(project, epic) => {
                        placements.onMirror(project, epic);
                        setTeamMenu(null);
                      }}
                    />
                  ) : null;
                })()}
                {/* The roster carries "" for the no-team group; the explicit
                    "No team" entry below is that, so skip the blank one. And
                    only the teams of the card's own repository are offered:
                    its project decides where it lives, so a team from another
                    repository would put the card out of reach of the people
                    it names — the server refuses that pair. What the card
                    already carries stays on the list, so a pair written
                    before the rule can be seen and fixed. */}
                {teamFollowsParent(card) ? (
                  <div className="card-stage-item card-stage-item-static">
                    <span
                      className="card-stage-dot"
                      style={card.team ? { background: teamColor(card.team) } : undefined}
                    />
                    {card.team || "No team"} — follows the parent
                  </div>
                ) : (
                  <>
                    {(card.project
                      ? offerableTeams(
                          board.teams,
                          board.teamDomains,
                          board.projectDomains,
                          card.project,
                          card.team ?? "",
                        )
                      : // No project to constrain it: the COLUMN does. A
                        // card in a column of another repository can only
                        // take that repository's teams — its team is what
                        // decides where it lives (G57).
                        teamsACardCanTake(
                          columnDomain({ project: "", name: card.epic ?? "" }),
                          board.teams,
                          board.teamDomains,
                          card.team ?? "",
                          roster,
                        ))
                      .filter((t) => t !== "")
                      .map((t) => (
                        <button
                          key={t}
                          type="button"
                          className={`card-stage-item${card.team === t ? " card-stage-item-active" : ""}`}
                          onClick={() => assignTeam(card, t)}
                        >
                          <span
                            className="card-stage-dot"
                            style={{ background: teamColor(t) }}
                          />
                          {t}
                        </button>
                      ))}
                    {/* A card with neither team nor project is held by the
                        PRIMARY repository, so a column of another one could
                        not show it: there the entry is a refusal with a
                        friendly label. A card with a project is placed by
                        it either way. What the card already carries stays
                        on the list. */}
                    {(!card.team ||
                      teamlessIsLawful(
                        columnDomain({ project: card.project ?? "", name: card.epic ?? "" }),
                        roster,
                        card.project ?? "",
                      )) && (
                      <button
                        type="button"
                        className={`card-stage-item${card.team ? "" : " card-stage-item-active"}`}
                        onClick={() => assignTeam(card, null)}
                      >
                        <span className="card-stage-dot card-stage-dot-none" />
                        No team
                      </button>
                    )}
                  </>
                )}
              </Dropdown>
              {/* One grip per edge: the top moves the slot's start, the
                  bottom its end. "from" is whichever edge stays put. */}
              <div
                className="project-slot-resize project-slot-resize-top"
                title="Drag to move the start"
                onPointerDown={(ev) => beginDrag(e, row + span - 1, ev, card, "top")}
              />
              <div
                className="project-slot-resize"
                title="Drag to stretch over more weeks"
                onPointerDown={(ev) => beginDrag(e, row, ev, card, "bottom")}
              />
            </div>
          )),
        )}
        </div>
        <button
          type="button"
          className="project-more"
          onClick={() => setPadFwd(padFwd + WEEK_STEP)}
          title={`Show ${WEEK_STEP} more weeks after`}
        >
          ↓ later weeks
        </button>
      </div>
      )}
      <Dropdown
        open={weekMenu !== null}
        anchorRef={weekAnchor}
        onClose={() => setWeekMenu(null)}
        className="card-stage-menu"
      >
        {menuProjects.length === 0 && (
          <span className="card-stage-item project-menu-hint">
            Add a project first — a deadline belongs to a plan
          </span>
        )}
        {menuProjects.map((p) => {
          const has = board.deadlines.some(
            (d) => d.week === weekMenu && d.project === p,
          );
          return (
            <button
              key={p || "\u0000none"}
              type="button"
              className="card-stage-item"
              onClick={() => setDeadline(weekMenu ?? "", p, !has)}
            >
              <span
                className="card-stage-dot"
                style={{ background: p ? teamColor(p) : "var(--danger)" }}
              />
              {p
                ? has
                  ? `Remove ${p}'s deadline`
                  : `Set a deadline for ${p}`
                : has
                  ? "Remove the deadline with no project"
                  : "Set a deadline with no project"}
            </button>
          );
        })}
      </Dropdown>
      {!empty && (
        <div className="project-footer">
          <div
            className="project-progress"
            role="progressbar"
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={overall.pct}
            aria-label={`${targetProject || "All projects"} progress`}
            title={`${overall.pct}% done across the plan`}
          >
            <div
              className="project-progress-fill"
              style={{ width: `${overall.pct}%` }}
            />
          </div>
          <div className="project-footer-head">
            <span className="project-progress-label">
              {targetProject === null
                ? "All projects"
                : targetProject || "No project"}{" "}
              · {overall.pct}% · {overall.done}/{overall.total} done
            </span>
            <button
              type="button"
              className="project-footer-toggle"
              onClick={toggleProgress}
              aria-expanded={progressOpen}
              aria-label={
                progressOpen
                  ? "Hide every project's progress"
                  : "Show every project's progress"
              }
              title={progressOpen ? "Collapse" : "Every project"}
            >
              {progressOpen ? "▼" : "▲"}
            </button>
          </div>
          {progressOpen && (
            <div className="project-footer-list">
              {allProgress.length === 0 && (
                <p className="project-empty-hint">No projects yet.</p>
              )}
              {allProgress.map((p) => (
                <button
                  key={p.project || "\u0000none"}
                  type="button"
                  className={`project-footer-row${
                    targetProject === p.project ? " project-footer-row-on" : ""
                  }`}
                  onClick={() => selectFilter([p.project])}
                  title={`Show only ${p.project || "the columns with no project"}`}
                >
                  <span className="project-footer-name">
                    {p.project ? (
                      <>
                        <span
                          className="team-dot"
                          style={{ background: teamColor(p.project) }}
                        />
                        {p.project}
                      </>
                    ) : (
                      <em>No project</em>
                    )}
                  </span>
                  <span className="project-footer-bar">
                    <span
                      className="project-footer-bar-fill"
                      style={{ width: `${p.pct}%` }}
                    />
                  </span>
                  <span className="project-footer-stat">
                    {p.pct}% · {p.done}/{p.total}
                  </span>
                </button>
              ))}
            </div>
          )}
        </div>
      )}

    </div>
  );
}

function errText(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
