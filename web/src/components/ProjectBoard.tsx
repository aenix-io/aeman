import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import type {
  Board,
  Card as CardModel,
  EpicRef,
  Provider,
} from "../providers/types";
import { registerPendingCard } from "../api/pending";
import { addDays, mondayOf, todayIso } from "../date";
import { teamColor, teamInitial } from "../avatar";
import { Dropdown } from "./Dropdown";
import { STAGES } from "../stages";
import { TeamChips } from "./TeamChips";

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

/** weeksBetween counts whole weeks from Monday a to Monday b (0 = same week). */
function weeksBetween(a: string, b: string): number {
  const [ay, am, ad] = a.split("-").map(Number);
  const [by, bm, bd] = b.split("-").map(Number);
  return Math.round(
    (Date.UTC(by, bm - 1, bd) - Date.UTC(ay, am - 1, ad)) / (7 * 86400000),
  );
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

/** One grid row's height in px, and how many weeks each "more" press adds.
 *  ROW_PX must match .project-cell in styles.css — it is what keeps the view
 *  still when rows are prepended. */
const ROW_PX = 28;
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
  const late = !!card.day && card.day < today;
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

/** LS_COLW remembers how wide you dragged the columns — this browser's view
 *  of the board rather than the board's own state, so it stays local. (The
 *  project chips' selection lives in projectFilter.ts, shared with the
 *  Process tab.) */
const LS_COLW = "aeman.projectColWidth";
const LS_PROGRESS = "aeman.projectProgressOpen";

/** The narrowest a column may be dragged: below this a title is unreadable. */
const MIN_COL = 70;

/** Column widths, one per selection of projects: a plan of 21 columns and one
 *  of a single column want different widths, and a width that followed you
 *  between them would be wrong in one of the two. */
function readColWidths(): Record<string, number> {
  try {
    const raw = localStorage.getItem(LS_COLW);
    const v: unknown = raw ? JSON.parse(raw) : null;
    // The pre-selection format was one number for the whole board: keep it as
    // the all-projects width rather than dropping what someone dragged.
    if (typeof v === "number") {
      return v >= MIN_COL ? { "*": v } : {};
    }
    if (v && typeof v === "object") {
      return Object.fromEntries(
        Object.entries(v as Record<string, unknown>).filter(
          ([, w]) => typeof w === "number" && w >= MIN_COL,
        ),
      ) as Record<string, number>;
    }
  } catch {
    // A corrupt entry is not worth a broken board.
  }
  return {};
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
    return board.cards.filter(
      (c) => c.epic && !c.parent && shown.has(colKey(c.project ?? "", c.epic)),
    );
  }, [board.cards, epics]);

  // The week window: two weeks of history before today (or the earliest
  // card), through the latest card plus a quarter of runway to plan into.
  // How far the window reaches beyond the default, in weeks, grown by the
  // buttons at either end — planning is not confined to a fixed horizon.
  const [padBack, setPadBack] = useState(0);
  const [padFwd, setPadFwd] = useState(0);

  // Column width: null until dragged, when the columns just share the room —
  // the right default. One width governs every column of the selection,
  // because a plan reads as a grid and columns of assorted widths stop being
  // comparable; a different selection carries its own width.
  const [colWidths, setColWidths] = useState<Record<string, number>>(readColWidths);
  const widthsRef = useRef(colWidths);
  const [resizing, setResizing] = useState<{ x: number; from: number } | null>(null);

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
      .reorderEpics(board, d.project, names)
      .catch((err: unknown) => {
        setOrder(null); // put the board's own order back on screen
        onError(errText(err));
      });
  };

  // Which selection the width belongs to: the chips in view, or every project.
  const widthKey = filter ? [...filter].sort().join("\u0000") : "*";
  const colWidth = colWidths[widthKey] ?? null;

  const setColWidth = (w: number | null) => {
    const next = { ...widthsRef.current };
    if (w === null) {
      delete next[widthKey];
    } else {
      next[widthKey] = w;
    }
    widthsRef.current = next;
    setColWidths(next);
  };

  const persistWidths = () =>
    localStorage.setItem(LS_COLW, JSON.stringify(widthsRef.current));

  const beginResize = (e: React.PointerEvent) => {
    e.preventDefault();
    e.stopPropagation();
    const head = (e.currentTarget as HTMLElement).closest(".project-epic-head");
    const from = colWidth ?? (head ? head.getBoundingClientRect().width : 140);
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    setResizing({ x: e.clientX, from });
  };

  const moveResize = (e: React.PointerEvent) => {
    if (!resizing) {
      return;
    }
    setColWidth(Math.max(MIN_COL, Math.round(resizing.from + (e.clientX - resizing.x))));
  };

  const endResize = () => {
    if (!resizing) {
      return;
    }
    setResizing(null);
    persistWidths();
  };

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
        scroller.scrollTop += WEEK_STEP * ROW_PX;
      });
    }
  };

  // ---- drag-to-create (and resize): a pressed column stretch becomes a slot.
  const [drag, setDrag] = useState<{
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
  const handlesRef = useRef<HTMLDivElement | null>(null);
  const handlesClipRef = useRef<HTMLDivElement | null>(null);
  // The layer's containing block, measured against explicitly rather than via
  // offsetParent — which is whatever happens to be positioned up the tree.
  const wrapRef = useRef<HTMLDivElement | null>(null);
  const [boardBox, setBoardBox] = useState<{
    left: number;
    top: number;
    height: number;
    gridTop: number;
  } | null>(null);

  // A slot being dragged to another week / epic. It follows the pointer as a
  // preview and only writes on release.
  const [move, setMove] = useState<{
    card: CardModel;
    span: number;
    row: number;
    epic: EpicRef;
  } | null>(null);
  // The press behind a possible drag: held in a ref so that merely pressing a
  // card re-renders nothing, and so a click that never travels stays a click.
  const press = useRef<{
    card: CardModel;
    row: number;
    span: number;
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
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    // "from" is the edge that stays put: the card's first row when the bottom
    // is pulled, its last row when the top is.
    setDrag({ epic, from: week, to: week, resize, edge });
  };

  const moveDrag = (e: React.PointerEvent) => {
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
        .patchCard(board, resize.itemId, { dates: { start } })
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
        .patchCard(board, resize.itemId, { dates: { end } })
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

  const beginMove = (card: CardModel, row: number, span: number, e: React.PointerEvent) => {
    // Only the slot's BODY drags. A press that started on a control inside it
    // (the team badge, delete, the resize grip) must reach that control: the
    // press bubbles up here, and capturing it — plus preventDefault — used to
    // swallow the click entirely, so the badge could never be pressed.
    if ((e.target as HTMLElement).closest("button, input, .project-slot-resize")) {
      return;
    }
    e.preventDefault();
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    // Remember WHERE on the card the press landed. A press is not yet a drag:
    // it becomes one only once the pointer travels, so a stray click leaves
    // the card exactly where it was.
    press.current = {
      card,
      row,
      span,
      grab: Math.max(0, rowAt(e.clientY) - row),
      x: e.clientX,
      y: e.clientY,
    };
  };

  const moveMove = (e: React.PointerEvent) => {
    const p = press.current;
    if (!p) {
      return;
    }
    if (!move) {
      if (Math.abs(e.clientX - p.x) < DRAG_SLOP && Math.abs(e.clientY - p.y) < DRAG_SLOP) {
        return;
      }
      setMove({
        card: p.card,
        span: p.span,
        row: p.row,
        epic: { name: p.card.epic ?? "", project: p.card.project ?? "" },
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
    if (!move) {
      return;
    }
    const { card, span, row, epic } = move;
    setMove(null);
    const week = weeks[row];
    const end = addDays(weeks[Math.min(row + span - 1, weeks.length - 1)], 4);
    if (
      week === card.week &&
      colKey(epic.project, epic.name) === colKey(card.project ?? "", card.epic ?? "")
    ) {
      return;
    }
    const prev = {
      week: card.week,
      epic: card.epic,
      project: card.project,
      startDate: card.startDate,
      day: card.day,
    };
    patchCard(card.itemId, {
      week,
      epic: epic.name,
      project: epic.project,
      startDate: week,
      day: end,
    });
    void provider
      .patchCard(board, card.itemId, {
        epic: epic.name,
        project: epic.project,
        // No plan.week: the server takes the row from dates.start.
        dates: { start: week, end },
      })
      .then(addCard)
      .catch((err: unknown) => {
        patchCard(card.itemId, prev);
        onError(errText(err));
      });
  };

  const cancelDraft = () => setDraft(null);

  const createSlot = (title: string) => {
    if (!draft || !title.trim()) {
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
      isDraft: true,
      assignees: [],
      epic: epic.name,
      project: epic.project,
      week,
      startDate: start,
      day: end,
      description: "",
      progress: 0,
    });
    const created = provider.createCard(board, {
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
      .addEpic(board, name.trim(), targetProject)
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
      .deleteEpic(board, col.name, col.project)
      .catch((err: unknown) => onError(errText(err)));
  };

  const renameEpic = (col: EpicRef, to: string) => {
    setRenaming(null);
    if (!to.trim() || to.trim() === col.name) {
      return;
    }
    void provider
      .renameEpic(board, col.project, col.name, to.trim())
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
      .patchCard(board, card.itemId, { team: team ?? "" })
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
      ? provider.addDeadline(board, week, projectName)
      : provider.deleteDeadline(board, week, projectName);
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
    };
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(board);
    window.addEventListener("resize", measure);
    return () => {
      ro.disconnect();
      window.removeEventListener("resize", measure);
    };
  }, [weeks.length, epics.length, colWidth, padBack, padFwd]);

  // Follow the board's vertical scrolling by hand, so the handles never lag a
  // frame behind their rows (which a re-render would cost).
  useEffect(() => {
    const board = scrollRef.current;
    if (!board) {
      return;
    }
    const follow = () => {
      const layer = handlesRef.current;
      const clip = handlesClipRef.current;
      if (layer) {
        layer.style.transform = `translateY(${-board.scrollTop}px)`;
      }
      if (clip && boardBox) {
        // Hide handles that have scrolled up under the sticky header, and let
        // them overhang left and right so one can straddle the board's edge.
        const top = Math.max(0, boardBox.gridTop + HEADER_PX - board.scrollTop);
        clip.style.clipPath = `inset(${top}px -60px 0px -60px)`;
      }
    };
    follow();
    board.addEventListener("scroll", follow, { passive: true });
    return () => board.removeEventListener("scroll", follow);
  }, [boardBox]);

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
      .moveDeadline(board, dlProject, from, to)
      .catch((err: unknown) => onError(errText(err)));
  };

  // Done, and back again. Marking done is the board's own rule — the server
  // clears the stage and fills 100 — and the way back is the In Progress
  // action, which nudges the card into the working band instead of inventing
  // a number: the same thing the other boards do when a card reopens.
  const setDone = (card: CardModel, done: boolean) => {
    setTeamMenu(null);
    const prev = { stage: card.stage, progress: card.progress };
    if (done) {
      patchCard(card.itemId, {
        stage: card.stage === "recurrent" ? "recurrent" : undefined,
        progress: 100,
      });
      void provider
        .patchCard(board, card.itemId, { stage: "done" })
        .then(addCard)
        .catch((err: unknown) => {
          patchCard(card.itemId, prev);
          onError(errText(err));
        });
      return;
    }
    patchCard(card.itemId, { stage: undefined, progress: 90 });
    void provider
      .setInProgress(board, card.itemId)
      .then(addCard)
      .catch((err: unknown) => {
        patchCard(card.itemId, prev);
        onError(errText(err));
      });
  };

  const deleteCard = (card: CardModel) => {
    if (!window.confirm(`Delete "${card.title}"?`)) {
      return;
    }
    removeCard(card.itemId);
    void provider.deleteCard(board, card.itemId).catch((err: unknown) => {
      onError(errText(err));
      reload();
    });
  };

  // While a slot is being stretched, its span follows the pointer — the drag
  // has to be visible on the thing being dragged, not only on the cursor.
  // While an edge is being pulled, the slot itself shows where it would land:
  // the preview IS the card, so there is nothing to imagine.
  const previewRow = (card: CardModel, row: number, span: number): number => {
    if (!drag?.resize || drag.resize.itemId !== card.itemId || drag.edge !== "top") {
      return row;
    }
    return Math.max(0, Math.min(drag.to, row + span - 1));
  };

  const previewSpan = (card: CardModel, row: number, span: number): number => {
    if (!drag?.resize || drag.resize.itemId !== card.itemId) {
      return span;
    }
    if (drag.edge === "top") {
      return row + span - previewRow(card, row, span);
    }
    return Math.max(1, Math.min(drag.to - row + 1, weeks.length - row));
  };

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
    };
    const byCol = new Map<string, Slot[]>();
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
      const row = weeksBetween(weeks[0], anchor);
      if (row < 0 || row >= weeks.length) {
        continue;
      }
      const endMon = c.day && c.day > anchor ? mondayOf(c.day) : anchor;
      const span = Math.max(1, Math.min(weeksBetween(anchor, endMon) + 1, weeks.length - row));
      const k = colKey(c.project ?? "", c.epic);
      const list = byCol.get(k) ?? [];
      list.push({ card: c, row, span, lane: 0, lanes: 1 });
      byCol.set(k, list);
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
      });
      byCol.set(k, list);
    }
    // Lanes are worked out per CLUSTER of overlapping slots, not per column:
    // splitting the whole column because two slots happen to share a fortnight
    // would leave every unrelated card at half width for no reason.
    for (const list of byCol.values()) {
      list.sort((a, b) => a.row - b.row || b.span - a.span);
      let cluster: typeof list = [];
      let laneEnd: number[] = [];
      const close = () => {
        for (const s of cluster) {
          s.lanes = laneEnd.length;
        }
        cluster = [];
        laneEnd = [];
      };
      for (const s of list) {
        // A slot that begins after everything so far has ended starts a new
        // cluster: it shares its weeks with nothing before it.
        if (laneEnd.length && s.row >= Math.max(...laneEnd)) {
          close();
        }
        let lane = laneEnd.findIndex((end) => end <= s.row);
        if (lane === -1) {
          lane = laneEnd.length;
        }
        laneEnd[lane] = s.row + s.span;
        s.lane = lane;
        cluster.push(s);
      }
      close();
    }
    return byCol;
  }, [cards, weeks, draft]);

  // How far along each column is, and the view as a whole.
  const colProgress = useMemo(() => {
    const byCol = new Map<string, CardModel[]>();
    for (const c of cards) {
      const k = colKey(c.project ?? "", c.epic ?? "");
      byCol.set(k, [...(byCol.get(k) ?? []), c]);
    }
    const out = new Map<string, ReturnType<typeof progressOf>>();
    for (const [k, list] of byCol) {
      out.set(k, progressOf(list));
    }
    return out;
  }, [cards]);
  const overall = useMemo(() => progressOf(cards), [cards]);

  // Every project's progress, filter or no filter: the point of opening the
  // status line is to see the ones you are NOT looking at. The no-project
  // bucket appears only when it actually holds work.
  const allProgress = useMemo(() => {
    const byProject = new Map<string, CardModel[]>();
    for (const c of board.cards) {
      if (!c.epic || c.parent) {
        continue;
      }
      const k = c.project ?? "";
      byProject.set(k, [...(byProject.get(k) ?? []), c]);
    }
    return [...board.projects, ""]
      .filter((p) => p !== "" || (byProject.get("")?.length ?? 0) > 0)
      .map((p) => ({ project: p, ...progressOf(byProject.get(p) ?? []) }));
  }, [board.cards, board.projects]);

  const todayRow = weeks.indexOf(thisMonday);

  // Open on today. A plan reaches months back, and the board opened at its
  // very first week — someone arriving had to scroll past a spent quarter to
  // find out where the team is. Once per project shown: after that the scroll
  // is the reader's, and pressing "earlier weeks" must not yank it back.
  const scrolledFor = useRef<string | null>(null);
  useLayoutEffect(() => {
    const scroller = scrollRef.current;
    if (!scroller || !boardBox || todayRow < 0 || scrolledFor.current === widthKey) {
      return;
    }
    const top = Math.max(0, boardBox.gridTop + HEADER_PX + (todayRow - 2) * ROW_PX);
    // After the paint: switching project rebuilds the grid, and a scroll set
    // before it has its new height is clamped to the old one. The mark is set
    // in the callback, not here — this effect re-runs as the new board is
    // measured, and a mark set up front would cancel the pending frame and
    // then refuse to schedule another, which is why switching never moved.
    const frame = requestAnimationFrame(() => {
      scrolledFor.current = widthKey;
      scroller.scrollTop = top;
    });
    return () => cancelAnimationFrame(frame);
  }, [boardBox, todayRow, widthKey, weeks.length]);

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
          gridTemplateColumns: `54px repeat(${epics.length}, ${
            colWidth === null ? "minmax(140px, 1fr)" : `${colWidth}px`
          }) 34px`,
          gridTemplateRows: `26px repeat(${weeks.length}, 28px)`,
        }}
      >
        {/* header row */}
        <div className="project-corner" />
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
              {multi && e.project && (
                <span
                  className="project-epic-avatar"
                  style={{ background: teamColor(e.project) }}
                  title={e.project}
                >
                  {teamInitial(e.project)}
                </span>
              )}
              <button
                type="button"
                className="card-action project-epic-del"
                title="Delete the epic (must be empty)"
                onClick={() => deleteEpic(e)}
              >
                ×
              </button>
              {/* The border between two headers is the grip: dragging it sets
                  the width of every column at once, double-click gives the
                  room back to be shared evenly. */}
              <span
                className="project-col-resize"
                title="Drag to set every column's width · double-click to fit"
                onPointerDown={beginResize}
                onPointerMove={moveResize}
                onPointerUp={endResize}
                onPointerCancel={() => setResizing(null)}
                onDoubleClick={(ev) => {
                  // Back to sharing the room evenly — for this selection only.
                  ev.stopPropagation();
                  setColWidth(null);
                  persistWidths();
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
              onPointerMove={moveDrag}
              onPointerUp={endDrag}
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
              />,
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
                    width: `calc(${100 / draftLanes}% - 2px)`,
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
            .map(({ card, row, span, lane, lanes }) => (
            <div
              key={card.itemId}
              className={`project-slot ${slotTone(card, today)}${
                move?.card.itemId === card.itemId ? " project-slot-moving" : ""
              }`}
              style={{
                // While dragged, the slot itself sits where it would land —
                // the preview IS the card, so there is nothing to guess.
                gridColumn:
                  (move?.card.itemId === card.itemId ? colIndex(move.epic) : col) + 2,
                gridRow: `${(move?.card.itemId === card.itemId ? move.row : previewRow(card, row, span)) + 2} / span ${previewSpan(card, row, span)}`,
                borderLeftColor: card.team ? teamColor(card.team) : undefined,
                // Cards sharing weeks in one column split its width instead
                // of hiding each other. The width holds while the card is
                // being dragged too: widening it on press made the first click
                // of a double-click visibly inflate the card.
                ...(lanes > 1
                  ? {
                      width: `calc(${100 / lanes}% - 2px)`,
                      marginLeft: `${(100 / lanes) * lane}%`,
                    }
                  : {}),
              }}
              onPointerDown={(ev) => beginMove(card, row, previewSpan(card, row, span), ev)}
              onPointerMove={moveMove}
              onPointerUp={endMove}
              onPointerCancel={() => {
                press.current = null;
                setMove(null);
              }}
              onDoubleClick={() => onOpen(card)}
              title={card.title}
            >
              <span className="project-slot-title">{card.title}</span>
              <span className="project-slot-actions">
                <button
                  type="button"
                  className="card-action card-action-delete"
                  title="Delete"
                  onClick={(ev) => {
                    ev.stopPropagation();
                    deleteCard(card);
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
                title={card.team ? `Team: ${card.team} — click to change` : "Assign to a team"}
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
                {/* The roster carries "" for the no-team group; the explicit
                    "No team" entry below is that, so skip the blank one. */}
                {board.teams.filter((t) => t !== "").map((t) => (
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
                <button
                  type="button"
                  className={`card-stage-item${card.team ? "" : " card-stage-item-active"}`}
                  onClick={() => assignTeam(card, null)}
                >
                  <span className="card-stage-dot card-stage-dot-none" />
                  No team
                </button>
              </Dropdown>
              {/* One grip per edge: the top moves the slot's start, the
                  bottom its end. "from" is whichever edge stays put. */}
              <div
                className="project-slot-resize project-slot-resize-top"
                title="Drag to move the start"
                onPointerDown={(ev) => beginDrag(e, row + span - 1, ev, card, "top")}
                onPointerCancel={() => setDrag(null)}
                onPointerMove={moveDrag}
                onPointerUp={endDrag}
              />
              <div
                className="project-slot-resize"
                title="Drag to stretch over more weeks"
                onPointerDown={(ev) => beginDrag(e, row, ev, card, "bottom")}
                onPointerCancel={() => setDrag(null)}
                onPointerMove={moveDrag}
                onPointerUp={endDrag}
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

      {/* The handles: centred on the board's left edge, which is why they are
          here and not inside it. */}
      {boardBox && (
        <div
          ref={handlesClipRef}
          className="project-handles"
          style={{ left: boardBox.left, top: boardBox.top, height: boardBox.height }}
        >
          <div ref={handlesRef} className="project-handles-inner">
            {board.deadlines
              .filter((d) => !filter || filter.includes(d.project))
              .map((d) => {
                const dragging =
                  dragLine?.from === d.week && dragLine.project === d.project;
                const at = dragging ? dragLine.row : weeks.indexOf(d.week);
                if (at < 0 || at >= weeks.length) {
                  return null;
                }
                const colour = multi && d.project ? teamColor(d.project) : undefined;
                return (
                  <span
                    key={`${d.project}\u0000${d.week}`}
                    className={`project-deadline-dot${dragging ? " project-deadline-dot-dragging" : ""}`}
                    style={{
                      // The row's bottom edge, less half the line's 2px so
                      // the handle's centre lands on the line's centre.
                      top: boardBox.gridTop + HEADER_PX + (at + 1) * ROW_PX - 1,
                      ...(colour ? { background: colour } : {}),
                    }}
                    title={`${d.project || "no project"} deadline — drag to another week · ${weekLabel(weeks[at])}`}
                    onPointerDown={(ev) => beginLineDrag(d.project, d.week, at, ev)}
                    onPointerMove={moveLineDrag}
                    onPointerUp={endLineDrag}
                    onPointerCancel={() => setDragLine(null)}
                  />
                );
              })}
          </div>
        </div>
      )}
    </div>
  );
}

function errText(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
