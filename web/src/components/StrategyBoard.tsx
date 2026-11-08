import { useEffect, useLayoutEffect, useMemo, useRef, useState, type CSSProperties, type PointerEvent as ReactPointerEvent, type ReactNode } from "react";
import {
  DndContext,
  DragOverlay,
  PointerSensor,
  useDraggable,
  useSensor,
  useSensors,
  type DragEndEvent,
  type DragStartEvent,
} from "@dnd-kit/core";
import { registerPendingCard } from "../api/pending";
import { todayIso } from "../date";
import type { Board, Card as CardModel, CardType, Provider } from "../providers/types";
import {
  DEFAULT_DEPTH,
  DELETE_INTENT,
  KIND_LABEL,
  LEVELS,
  barOf,
  childLevels,
  buildTree,
  childrenNote,
  daysBetween,
  dropCommit,
  dropRequest,
  isFlatRange,
  dropShown,
  statusMenu,
  rolledUpAfter,
  workPatchOf,
  unassignPreview,
  unassignNotice,
  NOTHING_IN_WORK,
  unassignQuestion,
  UNASSIGNED_LOCALLY,
  droppable,
  fractionOf,
  hierarchyChoices,
  isUnder,
  place,
  placementFor,
  planningRemovalPlan,
  rangeFor,
  statusOf,
  ticks as ticksOf,
  dropMark,
  dropTargetAt,
  boxesInContent,
  pointerContentY,
  revealStep,
  type DepthRange,
  type DropMark,
  type DropTarget,
  type RowBox,
  type HierarchyChoice,
  type Placement,
  type StrategyKind,
  type Row,
  type Zoom,
  writeDrop,
  strategyLines,
  type StrategyLine,
} from "../strategy";
import { barColor } from "../stages";
import { FilterSelect } from "./FilterSelect";
import { HIDE_DONE, STATUS_FILTERS, STATUS_LABELS } from "../workstatus";
import { customerOptions, teamOptions } from "../filteroptions";
import {
  COLUMN_MAX,
  COLUMN_MINS,
  STRATEGY_COLUMNS_KEY,
  columnVars,
  draggedWidths,
  resetColumn,
  resized,
  steppedWidth,
  storedColumns,
  type ColumnWidths,
  type StrategyColumn,
} from "../strategycolumns";
import { NO_STRATEGY_FILTERS, ownerFilterOptions, peopleOf, strategyOwnerCell, type StrategyFilters } from "../ownership";
import { isPlanning } from "../subtasks";
import { ownerOf, planOwnerOf, planOwnPatch, type Owner } from "../owner";
import { type Avatars, type Names } from "../users";
import { AddCard } from "./AddCard";
import { INDENT_PX, OwnerCell, ROW_PX, StatusPill, TreeTitle } from "./PlanTree";
import { KindIcon } from "./KindIcon";
import { OwnerPicker } from "./OwnerPicker";
import { RemoveChoiceDialog } from "./RemoveChoiceDialog";
import { Dropdown } from "./Dropdown";

// The Strategy board: the hierarchy as a tree of rows on the left and a timeline
// of bars on the right, one row per card from a goal down to a subtask
// (docs/design/hierarchy.md, "The Strategy board tab"). What a row IS, which rows a
// depth range draws, what a row's status is, where its bar lands, and what
// a drag may do are strategy.ts's answers; this component draws them and
// makes the board's gestures — a create under a row, its ×, and the drag.

interface StrategyBoardProps {
  board: Board;
  provider: Provider;
  /** The board's teams: what the Owner column offers. The Strategy board filters by
   *  nobody's team, since a planning card has none. */
  roster: string[];
  avatars?: Avatars;
  names?: Names;
  patchCard: (itemId: string, patch: Partial<CardModel>) => void;
  addCard: (card: CardModel) => void;
  replaceCard: (itemId: string, card: CardModel) => void;
  removeCard: (itemId: string) => void;
  /** The board's listing order, applied at once after a drop. */
  reorderCards?: (orderedIds: string[]) => void;
  reload: () => void;
  onError: (message: string) => void;
  onOpen: (card: CardModel) => void;
  /** A card to REVEAL: a breadcrumb was followed here. Its ancestors unfold
   *  and its row is marked; onFocused says it has been. */
  focusId?: string | null;
  onFocused?: () => void;
  /** The Team, Customer and Owner filters (ownership.md, O7, O8): the
   *  listing's selectors, held and remembered by the app. */
  filters: StrategyFilters;
  onFilters: (next: StrategyFilters) => void;
  /** Open the team and customer rosters' dialogs, from the bottom of the
   *  Team and Customer filters. */
  onManageTeams?: () => void;
  onManageCustomers?: () => void;
}

const LS_DEPTH = "aeman.strategy.depth";
const LS_ZOOM = "aeman.strategy.zoom";
const LS_FOLDED = "aeman.strategy.folded";

/** HEAD_PX is the height of the table's head; a row's (ROW_PX) and one
 *  level of the tree (INDENT_PX) are PlanTree's, shared with the drawer. */
const HEAD_PX = 26;

/** pxPerDay is how wide a day is drawn at a zoom: three months read a week
 *  at a glance, a year reads a month, and Fit stretches its span to about
 *  the width of a screen without going below a few pixels a day. */
function pxPerDay(zoom: Zoom, days: number): number {
  if (zoom === "3m") {
    return 12;
  }
  if (zoom === "1y") {
    return 4;
  }
  return Math.max(3, Math.min(16, Math.floor(1000 / Math.max(days, 1))));
}

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

/** shortDate is a date as a row shows it: "10 Feb", the year only when it
 *  is not this one. */
function shortDate(iso: string | undefined, today: string): string {
  if (!iso) {
    return "";
  }
  const [y, m, d] = iso.split("-").map(Number);
  const label = `${d} ${MONTHS[m - 1]}`;
  return iso.slice(0, 4) === today.slice(0, 4) ? label : `${label} ${y}`;
}

function readDepth(): DepthRange {
  try {
    const raw = localStorage.getItem(LS_DEPTH);
    if (raw) {
      const parsed = JSON.parse(raw) as Partial<DepthRange>;
      if (LEVELS.includes(parsed.from as StrategyKind) && LEVELS.includes(parsed.to as StrategyKind)) {
        return { from: parsed.from as StrategyKind, to: parsed.to as StrategyKind };
      }
    }
  } catch {
    // no storage, or a value from another build: the default
  }
  return DEFAULT_DEPTH;
}

function readZoom(): Zoom {
  try {
    const raw = localStorage.getItem(LS_ZOOM);
    if (raw === "3m" || raw === "1y" || raw === "fit") {
      return raw;
    }
  } catch {
    // the default
  }
  return "3m";
}

function readFolded(): Set<string> {
  try {
    const raw = localStorage.getItem(LS_FOLDED);
    const parsed = raw ? (JSON.parse(raw) as unknown) : [];
    return new Set(Array.isArray(parsed) ? parsed.filter((x): x is string => typeof x === "string") : []);
  } catch {
    return new Set();
  }
}

const errText = (err: unknown) => (err instanceof Error ? err.message : String(err));

/** A row line of the table: a row of the tree or of a section (the other
 *  line is a section's header). The timeline draws a lane per line, so the
 *  two panes stay in step whatever stands between the rows. */
type RowLine = Extract<StrategyLine, { kind: "row" }>;

/** Hover is where the drag stands: the row and zone under the pointer
 *  (strategy.dropTargetAt), the placement that reads as, and whether the
 *  level rule allows it. */
interface Hover {
  target: DropTarget;
  placement: Placement | null;
  valid: boolean;
}

/** The style a drop mark takes on its row: the line's indent, by depth. */
const markStyle = (mark: DropMark | null): CSSProperties | undefined =>
  mark && mark.edge !== "inside" ? ({ "--drop-indent": `${6 + mark.depth * INDENT_PX}px` } as CSSProperties) : undefined;

interface RowProps {
  row: Row;
  today: string;
  open: boolean;
  focus: boolean;
  ghost: boolean;
  forbid: boolean;
  /** Where a drag would land, when that is on this row: a line on its top
   *  or bottom edge, or the row itself — drawn over it, taking no room. */
  mark: DropMark | null;
  dragging: boolean;
  onToggle: () => void;
  onOpen: () => void;
  onAdd: (() => void) | null;
  onRemove: (() => void) | null;
  /** The Owner cell's content, for a task-kind row; nothing on a planning
   *  card, which has none of a task's. */
  owner: ReactNode;
  /** The badge's menu (UNASSIGNED, K16), opened from the
   *  badge; null on a row that offers none. */
  onStatus: ((badge: HTMLElement) => void) | null;
  /** The status menu stands open on this row (aria-expanded). */
  menuOpen: boolean;
  rowRef: (el: HTMLDivElement | null) => void;
}

/** StrategyRow is one line of the table: draggable, and the row's own
 *  controls. The tree is drawn by indent guides and a chevron with a hand's
 *  worth of hit area, the kind by its pictogram. While a drag runs the rows
 *  stay where they are — the dragged rows dimmed in place — and where it
 *  would land is a mark over the row (strategy.dropMark). */
function StrategyRow({ row, today, open, focus, ghost, forbid, mark, dragging, onToggle, onOpen, onAdd, onRemove, owner, onStatus, menuOpen, rowRef }: RowProps) {
  const card = row.node.card;
  const id = card.itemId;
  const { attributes, listeners, setNodeRef: setDragRef, isDragging } = useDraggable({ id });
  const cls = [
    "strategy-row",
    focus ? "strategy-row-focus" : "",
    ghost || isDragging ? "strategy-row-ghost" : "",
    forbid ? "strategy-row-forbid" : "",
    mark ? `strategy-row-drop-${mark.edge}` : "",
    dragging ? "strategy-row-while-drag" : "",
  ]
    .filter(Boolean)
    .join(" ");
  return (
    <div
      ref={(el) => {
        setDragRef(el);
        rowRef(el);
      }}
      className={cls}
      style={{ height: ROW_PX, ...markStyle(mark) }}
      onClick={onOpen}
      {...attributes}
      {...listeners}
    >
      <TreeTitle row={row} open={open} onToggle={onToggle} onAdd={onAdd} onRemove={onRemove} />
      <span className="strategy-cell strategy-cell-owner">{owner}</span>
      <span className="strategy-cell strategy-cell-date">{shortDate(card.startDate, today)}</span>
      <span className="strategy-cell strategy-cell-date">{shortDate(card.day, today)}</span>
      <span className="strategy-cell strategy-cell-status">
        <StatusPill card={card} onMenu={onStatus ?? undefined} expanded={menuOpen} />
      </span>
    </div>
  );
}

/** OwnerCell is what a task row shows in its Owner column: the owner badge
 *  (the team's avatar, the person's over it, one label) — and the picker
 *  behind it. */
/** ColumnHandle is one boundary of the table's header (strategycolumns.ts):
 *  a pointer drag sizes the column on its left — the last one, the table
 *  against the timeline — a double-click puts it back, and the arrow keys
 *  step it on a focused handle. While a drag runs only the board's CSS
 *  variables move (`onLive`); the width is kept when it ends (`onCommit`). */
function ColumnHandle({
  col,
  widths,
  label,
  onLive,
  onCommit,
}: {
  col: StrategyColumn;
  widths: ColumnWidths;
  label: string;
  onLive: (next: ColumnWidths) => void;
  onCommit: (next: ColumnWidths) => void;
}) {
  // The pointer is captured on the handle for the drag, so its moves and
  // its end come here wherever it goes, and nothing is left listening on
  // the window if the board goes away mid-drag. A cancelled drag (the
  // browser took the pointer) puts the width back.
  const drag = useRef<{ id: number; startX: number; startW: number; next: ColumnWidths } | null>(null);
  const onPointerDown = (e: ReactPointerEvent<HTMLSpanElement>) => {
    if (e.button !== 0) {
      return;
    }
    e.preventDefault();
    e.stopPropagation();
    e.currentTarget.setPointerCapture?.(e.pointerId);
    drag.current = { id: e.pointerId, startX: e.clientX, startW: widths[col], next: widths };
  };
  const onPointerMove = (e: ReactPointerEvent<HTMLSpanElement>) => {
    const d = drag.current;
    if (!d || d.id !== e.pointerId) {
      return;
    }
    d.next = draggedWidths(widths, col, d.startW, d.startX, e.clientX);
    onLive(d.next);
  };
  const end = (e: ReactPointerEvent<HTMLSpanElement>, cancelled: boolean) => {
    const d = drag.current;
    if (!d || d.id !== e.pointerId) {
      return;
    }
    drag.current = null;
    e.currentTarget.releasePointerCapture?.(e.pointerId);
    if (cancelled) {
      onLive(widths);
    } else {
      onCommit(d.next);
    }
  };
  return (
    <span
      className="strategy-col-handle"
      role="separator"
      aria-orientation="vertical"
      aria-label={label}
      aria-valuenow={widths[col]}
      aria-valuemin={COLUMN_MINS[col]}
      aria-valuemax={COLUMN_MAX}
      tabIndex={0}
      title="Drag to size · double-click to reset"
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={(e) => end(e, false)}
      onPointerCancel={(e) => end(e, true)}
      onLostPointerCapture={(e) => end(e, true)}
      onDoubleClick={(e) => {
        e.stopPropagation();
        onCommit(resetColumn(widths, col));
      }}
      onKeyDown={(e) => {
        const w = steppedWidth(widths, col, e.key, e.shiftKey);
        if (w !== null) {
          e.preventDefault();
          onCommit(resized(widths, col, w));
        }
      }}
    />
  );
}

export function StrategyBoard({
  board,
  provider,
  roster,
  avatars,
  names,
  patchCard,
  addCard,
  replaceCard,
  removeCard,
  reorderCards,
  reload,
  onError,
  onOpen,
  focusId,
  onFocused,
  filters,
  onFilters,
  onManageTeams,
  onManageCustomers,
}: StrategyBoardProps) {
  const today = todayIso();
  // The table's column widths (strategycolumns.ts), the reader's own. A drag
  // moves only the CSS variables on the board (rootRef); the state and the
  // remembered value follow when it ends.
  const rootRef = useRef<HTMLDivElement | null>(null);
  const [widths, setWidthsState] = useState<ColumnWidths>(() => {
    try {
      return storedColumns(localStorage.getItem(STRATEGY_COLUMNS_KEY));
    } catch {
      return storedColumns(null);
    }
  });
  const liveWidths = (next: ColumnWidths) => {
    const el = rootRef.current;
    if (!el) {
      return;
    }
    for (const [k, v] of Object.entries(columnVars(next))) {
      el.style.setProperty(k, v);
    }
  };
  const commitWidths = (next: ColumnWidths) => {
    liveWidths(next);
    setWidthsState(next);
    try {
      localStorage.setItem(STRATEGY_COLUMNS_KEY, JSON.stringify(next));
    } catch {
      // a preference, not state
    }
  };
  const handle = (col: StrategyColumn, label: string) => (
    <ColumnHandle col={col} widths={widths} label={label} onLive={liveWidths} onCommit={commitWidths} />
  );
  const [depth, setDepthState] = useState<DepthRange>(readDepth);
  const [zoom, setZoomState] = useState<Zoom>(readZoom);
  const [folded, setFoldedState] = useState<Set<string>>(readFolded);
  // The planning row the + was pressed on, whose create form is open.
  const [addingUnder, setAddingUnder] = useState<string | null>(null);
  const [focusRow, setFocusRow] = useState<string | null>(null);
  const rowRefs = useRef(new Map<string, HTMLDivElement>());

  const setDepth = (next: DepthRange) => {
    setDepthState(next);
    try {
      localStorage.setItem(LS_DEPTH, JSON.stringify(next));
    } catch {
      // a preference, not state
    }
  };
  const setZoom = (next: Zoom) => {
    setZoomState(next);
    try {
      localStorage.setItem(LS_ZOOM, next);
    } catch {
      // a preference, not state
    }
  };
  const setFolded = (next: Set<string>) => {
    setFoldedState(next);
    try {
      localStorage.setItem(LS_FOLDED, JSON.stringify([...next]));
    } catch {
      // a preference, not state
    }
  };
  const toggle = (id: string) => {
    const next = new Set(folded);
    if (next.has(id)) {
      next.delete(id);
    } else {
      next.add(id);
    }
    setFolded(next);
  };

  // Whom the Owner filter offers — the Strategy board's one person filter (O8):
  // the board's people, the plan items' owners, the tasks' assignees and
  // whoever is chosen. The filters are the server's (O7, O8), so what was
  // sent is what is drawn.
  // Everyone the board has shown since it opened: a filtered listing holds
  // only the filter's answer, and the options must not shrink with it.
  const seenPeople = useRef(new Set<string>());
  const owners = useMemo(() => {
    for (const p of peopleOf(board.cards)) {
      seenPeople.current.add(p);
    }
    return ownerFilterOptions(board.members.map((m) => m.login), board.cards, filters.owners, seenPeople.current);
  }, [board.members, board.cards, filters.owners]);
  const filtering =
    filters.teams.length > 0 || filters.customers.length > 0 || filters.owners.length > 0 || filters.statuses.length > 0;
  const shown = board.cards;

  // ---- the drag: where it stands, and the mark of where it would land ----
  const [dragging, setDragging] = useState<string | null>(null);
  const [hover, setHover] = useState<Hover | null>(null);
  const justDropped = useRef(false);
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 5 } }));
  // The tree's rows as they stand during the drag, in the list's content
  // frame (strategy.boxesInContent) — what every reading of the drag is made
  // against (strategy.dropTargetAt). Nothing the drag draws moves a row, so
  // the snapshot is the list the reader sees; it is re-taken when the rows
  // themselves change under the drag (a watch frame), in the same frame.
  const boxes = useRef<RowBox[] | null>(null);
  // The pointer's last viewport y while a drag runs, kept by a window
  // listener (capture phase, so it is current before dnd-kit's own handler
  // reports the move). A KeyboardSensor would give none: with no position
  // there is no target, and a keyboard drag would land nowhere — none is
  // registered here.
  const pointerY = useRef<number | null>(null);
  const bodyRef = useRef<HTMLDivElement | null>(null);
  const headRef = useRef<HTMLDivElement | null>(null);
  // How far the board and the page are scrolled: the content frame's offset.
  const scrolled = () => (bodyRef.current?.scrollTop ?? 0) + window.scrollY;

  const expanded = (id: string) => !folded.has(id);
  // The lines of the table (strategy.strategyLines), keyed on everything that
  // decides them. What arrives is what is drawn.
  const shownLines = useMemo(() => strategyLines(shown, { depth, expanded }), [shown, depth, folded]); // eslint-disable-line react-hooks/exhaustive-deps
  // The tree's rows alone, which is what a drop is read against: a section
  // row is dropped on by nothing.
  const baseRows = useMemo(() => shownLines.filter((l) => l.kind === "row" && l.section === "tree").map((l) => (l as RowLine).row), [shownLines]);
  const rows = useMemo(() => shownLines.filter((l): l is RowLine => l.kind === "row").map((l) => l.row), [shownLines]);
  // The rows a drag carries — the dragged card and everything under it —
  // dimmed in place for as long as it runs, and a target for nothing.
  const ghosts = useMemo(() => {
    if (!dragging) {
      return new Set<string>();
    }
    const out = new Set<string>([dragging]);
    for (const c of shown) {
      if (isUnder(shown, c.itemId, dragging)) {
        out.add(c.itemId);
      }
    }
    return out;
  }, [dragging, shown]);
  const mark = hover?.valid && hover.placement ? dropMark(baseRows, hover.target, hover.placement) : null;

  // The snapshot is taken once the drag's first frame is laid out (an add
  // form open under a row is closed by the drag's start), and again whenever
  // the tree's rows change while it runs.
  useLayoutEffect(() => {
    if (!dragging) {
      boxes.current = null;
      return;
    }
    const measured = baseRows.flatMap((r) => {
      const el = rowRefs.current.get(r.node.card.itemId);
      if (!el) {
        return [];
      }
      const b = el.getBoundingClientRect();
      return [{ id: r.node.card.itemId, top: b.top, height: b.height }];
    });
    boxes.current = boxesInContent(measured, scrolled());
  }, [dragging, baseRows]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    if (!dragging) {
      pointerY.current = null;
      return;
    }
    const track = (e: PointerEvent) => {
      pointerY.current = e.clientY;
    };
    window.addEventListener("pointermove", track, { capture: true });
    return () => window.removeEventListener("pointermove", track, { capture: true });
  }, [dragging]);

  const onDragStart = (e: DragStartEvent) => {
    const start = e.activatorEvent as PointerEvent | null;
    pointerY.current = start && typeof start.clientY === "number" ? start.clientY : null;
    setDragging(String(e.active.id));
    setHover(null);
    setAddingUnder(null);
  };
  // dnd-kit reports a move on a pointer move and on a scroll alike; either
  // way the reading is the pointer now, in the content frame.
  const onDragMove = () => {
    if (!dragging || !boxes.current) {
      return;
    }
    const headBottom = headRef.current?.getBoundingClientRect().bottom ?? -Infinity;
    const y = pointerContentY(pointerY.current, scrolled(), headBottom);
    const target = y === null ? null : dropTargetAt(boxes.current, y, hover?.target ?? null, ghosts);
    if (!target) {
      setHover(null);
      return;
    }
    if (hover && hover.target === target) {
      return;
    }
    const placement = placementFor(shown, baseRows, dragging, target.overId, target.zone, isFlatRange(depth));
    const valid = !!placement && droppable(shown, dragging, placement.parent);
    setHover({ target, placement, valid });
  };
  const settle = () => {
    setDragging(null);
    setHover(null);
    justDropped.current = true;
    window.setTimeout(() => {
      justDropped.current = false;
    }, 0);
  };
  const onDragEnd = (e: DragEndEvent) => {
    const id = String(e.active.id);
    const h = hover;
    settle();
    if (!h || !h.valid || !h.placement) {
      return;
    }
    const card = board.cards.find((c) => c.itemId === id);
    if (!card) {
      return;
    }
    const commit = dropCommit(shown, id, h.placement);
    const before = { parent: card.parent, lineage: card.lineage };
    // Shown at once, filter or no filter (strategy.dropShown): the watch
    // does not echo this tab's own move, so nothing else would show it.
    const now = dropShown(shown, id, h.placement);
    if (now.patch) {
      patchCard(id, now.patch);
    }
    reorderCards?.(now.order);
    // ONE request, one action (ordering.md): a reorder within the epic, or a
    // move — an epic's gathers its tasks, a task dropped into another epic
    // is placed after that epic's last parked task.
    const request = dropRequest(shown, id, commit);
    void writeDrop(id, request, provider)
      .then((res) => {
        if (request.kind === "move" && request.parent !== undefined && !request.after && !request.before) {
          // The server chose the place: read it back.
          reload();
        }
        if (card.type === "epic" && res.actionId && res.moved > 1) {
          offerUndo(res.actionId, `Epic moved · ${res.moved - 1} ${res.moved === 2 ? "task" : "tasks"}`);
        }
      })
      .catch((err: unknown) => {
        patchCard(id, before);
        onError(errText(err));
        reload();
      });
  };

  // The Undo of a mass move (ordering.md, Q13): offered for ten seconds.
  const [undo, setUndo] = useState<{ actionId: string; text: string } | null>(null);
  const undoTimer = useRef<number | null>(null);
  const offerUndo = (actionId: string, text: string) => {
    if (undoTimer.current !== null) {
      window.clearTimeout(undoTimer.current);
    }
    setUndo({ actionId, text });
    undoTimer.current = window.setTimeout(() => setUndo(null), 10_000);
  };
  const pressUndo = () => {
    const u = undo;
    setUndo(null);
    if (!u) {
      return;
    }
    void provider
      .undoAction(u.actionId)
      .then(() => reload())
      .catch((err: unknown) => onError(errText(err)));
  };

  // One bar (or none) per LINE, so a lane's index is its line's: a header
  // has no bar and an empty lane.
  const bars = useMemo(() => shownLines.map((l) => (l.kind === "row" ? barOf(l.row.node.card) : null)), [shownLines]);
  const range = useMemo(() => rangeFor(zoom, today, bars), [zoom, today, bars]);
  const days = daysBetween(range.from, range.to) + 1;
  const trackW = days * pxPerDay(zoom, days);
  const ticks = useMemo(() => ticksOf(range), [range]);
  const todayAt = today >= range.from && today <= range.to ? fractionOf(today, range) : null;

  // A breadcrumb followed here: unfold the way down to the card, mark its
  // row and bring it into view. The lineage says which rows to unfold.
  const revealPending = useRef<string | null>(null);
  useEffect(() => {
    if (!focusId) {
      return;
    }
    const card = board.cards.find((c) => c.itemId === focusId);
    if (card) {
      const next = new Set(folded);
      for (const up of card.lineage ?? []) {
        next.delete(up.uid);
      }
      if (next.size !== folded.size) {
        setFolded(next);
      }
    }
    setFocusRow(focusId);
    revealPending.current = focusId;
    onFocused?.();
    // Unfolding is a preference write, not a dependency of the reveal.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [focusId, board.cards]);
  // The followed row is scrolled to ONCE (strategy.revealStep): it stays
  // marked, but a later change of the rows — a watch frame, a drop — does
  // not pull the list back to it, and nothing scrolls while a drag runs.
  useEffect(() => {
    const pending = revealPending.current;
    const step = revealStep(pending, !!dragging, !!pending && rowRefs.current.has(pending));
    if (step.scroll && pending) {
      rowRefs.current.get(pending)?.scrollIntoView({ block: "center" });
    }
    revealPending.current = step.pending;
  }, [focusRow, rows, dragging]);

  // ---- the create: a planning card at the top, or anything under a planning row ----
  const createUnder = (title: string, level: CardType | "task", parent?: string) => {
    const trimmed = title.trim();
    if (!trimmed) {
      return;
    }
    const tempId = `tmp-${new Date().toISOString()}`;
    const up = parent ? board.cards.find((c) => c.itemId === parent) : undefined;
    const type = level === "task" ? undefined : level;
    addCard({
      itemId: tempId,
      title: trimmed,
      assignees: [],
      type,
      parent,
      // Its lineage is its parent's plus the parent, so the tree draws the
      // new row where it was made before the server answers.
      lineage: up ? [...(up.lineage ?? []), { uid: up.itemId, type: up.type, title: up.title }] : undefined,
      description: "",
      progress: 0,
    });
    const created = provider.createCard({ title: trimmed, type, parent: parent ?? null, underPlanning: level === "task" });
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

  // ---- the owner: whose is this task, and where does that put it ----
  // The pair goes to the `own` gesture, which files loose work where the
  // pair says (a person's on their board today, a team's in its backlog,
  // nobody's in the no-team backlog, K3); the board applies the team and the person
  // at once and takes the rest — the day, the band, the sprint, the shelf —
  // from the answer.
  const members = useMemo(() => board.members.map((m) => m.login), [board.members]);
  const own = (card: CardModel, owner: Owner) => {
    const current = ownerOf(card);
    if (current.team === owner.team && current.person === owner.person) {
      return;
    }
    const before: Partial<CardModel> = {
      team: card.team,
      assignees: card.assignees,
      startDate: card.startDate,
      day: card.day,
      zone: card.zone,
      sprintStart: card.sprintStart,
      parked: card.parked,
    };
    patchCard(card.itemId, { team: owner.team || undefined, assignees: owner.person ? [owner.person] : [] });
    void provider
      .ownCard(card.itemId, owner.team, owner.person)
      .then((updated) =>
        patchCard(card.itemId, {
          team: updated.team,
          assignees: updated.assignees,
          startDate: updated.startDate,
          day: updated.day,
          zone: updated.zone,
          sprintStart: updated.sprintStart,
          parked: updated.parked,
        }),
      )
      .catch((err: unknown) => {
        patchCard(card.itemId, before);
        onError(errText(err));
      });
  };
  // A plan item's team and owner (ownership.md, O1, O9): the same gesture,
  // where a person needs no team and nothing reaches a day board.
  const ownPlan = (card: CardModel, next: Owner) => {
    const change = planOwnPatch(planOwnerOf(card), next);
    if (!change) {
      return;
    }
    const before: Partial<CardModel> = { team: card.team, owner: card.owner };
    patchCard(card.itemId, { team: next.team || undefined, owner: next.person || undefined });
    void provider
      .patchCard(card.itemId, change)
      .then((updated) => patchCard(card.itemId, { team: updated.team, owner: updated.owner }))
      .catch((err: unknown) => {
        patchCard(card.itemId, before);
        onError(errText(err));
      });
  };
  const ownerCell = (card: CardModel, kind: StrategyKind) => {
    if (kind === "goal" || kind === "initiative" || kind === "epic") {
      const owner = planOwnerOf(card);
      const shown = strategyOwnerCell(card, board.cards, true);
      return (
        <OwnerPicker
          planning
          current={owner}
          teams={roster}
          people={members}
          cards={board.cards}
          avatars={avatars}
          names={names}
          onPick={(o) => ownPlan(card, o)}
          title={`Who carries «${card.title}» out, and who answers for it`}
        >
          <OwnerCell owner={shown.owner} from={shown.from} avatars={avatars} names={names} />
        </OwnerPicker>
      );
    }
    if (kind !== "task" && kind !== "subtask") {
      return null;
    }
    const owner = ownerOf(card);
    const shown = strategyOwnerCell(card, board.cards, false);
    const cell = <OwnerCell owner={shown.owner} from={shown.from} avatars={avatars} names={names} />;
    // A subtask's owner is its task's (the server cascades it); the cell
    // reads and does not offer the menu.
    if (kind === "subtask") {
      return cell;
    }
    return (
      <OwnerPicker
        current={owner}
        teams={roster}
        people={members}
        cards={board.cards}
        avatars={avatars}
        names={names}
        onPick={(o) => own(card, o)}
        title={`Whose is «${card.title}»`}
      >
        {cell}
      </OwnerPicker>
    );
  };

  // ---- the ×: the Team board's own question — delete it, or only unlink it ----
  const [removeChoice, setRemoveChoice] = useState<CardModel | null>(null);

  // ---- the badge menu: UNASSIGNED (K16) ----
  // The badge of a task or a plan item opens it; what each answer reaches is
  // strategy.statusMenu's, applied at once and settled by the server's answer.
  const [statusFor, setStatusFor] = useState<CardModel | null>(null);
  const statusAnchor = useRef<HTMLElement | null>(null);
  const [unassigning, setUnassigning] = useState<{ card: CardModel; uids: string[] } | null>(null);
  const menu = statusFor ? statusMenu(board.cards, statusFor) : null;
  // A status gesture is shown at once — the tasks it reaches and the plan
  // items above them, re-rolled-up (strategy.rolledUpAfter) — then settled by
  // the server's answer, which names the tasks it changed as they stand now:
  // the watch does not echo the tab's own write. A refusal puts back what
  // was there.
  const applyWork = (uids: readonly string[], change: Partial<CardModel>, send: () => Promise<CardModel[]>) => {
    const before = board.cards.filter((c) => uids.includes(c.itemId));
    const after = board.cards.map((c) => (uids.includes(c.itemId) ? { ...c, ...change } : c));
    const rolled = rolledUpAfter(after, uids);
    const plansBefore = board.cards.filter((c) => rolled.has(c.itemId));
    for (const id of uids) {
      patchCard(id, change);
    }
    for (const [id, hierarchy] of rolled) {
      patchCard(id, { hierarchy });
    }
    void send()
      .then((changed) => {
        for (const c of changed) {
          patchCard(c.itemId, workPatchOf(c));
        }
      })
      .catch((err: unknown) => {
        for (const c of before) {
          patchCard(c.itemId, workPatchOf(c));
        }
        for (const p of plansBefore) {
          patchCard(p.itemId, { hierarchy: p.hierarchy });
        }
        onError(errText(err));
        reload();
      });
  };
  const answerUnassign = (card: CardModel, uids: string[]) => {
    // Exactly the tasks the confirm counted.
    applyWork(uids, UNASSIGNED_LOCALLY, () => provider.unassignWork(card.itemId, uids));
  };
  const answerRemoval = (card: CardModel, choice: HierarchyChoice) => {
    const plan = planningRemovalPlan(board.cards, card.itemId, choice);
    const before = { parent: card.parent, lineage: card.lineage };
    for (const p of plan.patches) {
      // A child moving up inherits the deleted card's own ancestors as its
      // lineage; the card coming out from under its parent has none. Either
      // way the tree draws right before the watch frames confirm it.
      patchCard(p.itemId, { parent: p.parent, lineage: p.parent ? card.lineage : undefined });
    }
    if (plan.gone) {
      removeCard(card.itemId);
      // The board knows this remove deletes: the plan item goes (tombstones.ts).
      void provider.removeCard(plan.request.uid, DELETE_INTENT, { deletes: true }).catch((err: unknown) => {
        onError(errText(err));
        reload();
      });
      return;
    }
    void provider.patchCard(card.itemId, { parent: "" }).catch((err: unknown) => {
      patchCard(card.itemId, before);
      onError(errText(err));
    });
  };

  const levelOptions = () =>
    LEVELS.map((k) => (
      <option key={k} value={k}>
        {KIND_LABEL[k]}
      </option>
    ));

  const planningOptions = (levels: (CardType | "task")[]) => levels.map((k) => ({ key: k, label: KIND_LABEL[k] }));

  const draggedCard = dragging ? board.cards.find((c) => c.itemId === dragging) : null;

  const lane = (r: Row, i: number) => {
    const card = r.node.card;
    const bar = bars[i]; // i is the LINE's index: bars runs over the lines
    const placed = bar ? place(bar, range) : null;
    const done = statusOf(card) === "done";
    const planning = isPlanning(card);
    const fill = planning ? undefined : done ? "var(--stage-done)" : barColor(card.stage);
    return (
      <div
        key={card.itemId}
        className={`strategy-lane${ghosts.has(card.itemId) ? " strategy-lane-ghost" : ""}`}
        style={{ height: ROW_PX }}
        onClick={() => onOpen(card)}
      >
        {placed && bar && (
          <div
            className={[
              "strategy-bar",
              `strategy-bar-${r.node.kind}`,
              done ? "strategy-bar-done" : "",
              bar.point ? "strategy-bar-point" : "",
              placed.clippedLeft ? "strategy-bar-clip-l" : "",
              placed.clippedRight ? "strategy-bar-clip-r" : "",
            ]
              .filter(Boolean)
              .join(" ")}
            style={{
              left: `${placed.left * 100}%`,
              width: bar.point ? undefined : `${placed.width * 100}%`,
              background: fill,
            }}
            title={`${card.title}: ${bar.start}${bar.point ? "" : ` → ${bar.end}`}`}
          />
        )}
      </div>
    );
  };

  // The create form under a planning row sits right after that row.
  const addingRow = rows.find((r) => r.node.card.itemId === addingUnder);

  // A plan item of the top level is added from a row of its own under the
  // tree, before the sections of loose work (In work, the backlogs): the
  // place a new root lands. Its level starts at the highest one shown, so
  // what it makes appears at this depth.
  const rootLevels: CardType[] = ["goal", "initiative", "epic"];
  const rootInitial: CardType = rootLevels.includes(depth.from as CardType) ? (depth.from as CardType) : "goal";
  const addRoot = (
    <div className="strategy-row strategy-row-form strategy-row-addroot" style={{ minHeight: ROW_PX }}>
      <AddCard
        placeholder="Add a goal, an initiative or an epic…"
        forcedTeam={null}
        picker={{
          title: "Level",
          options: planningOptions(rootLevels),
          initial: rootInitial,
        }}
        onCreate={(title, _team, picked) => createUnder(title, (picked as CardType) || rootInitial)}
      />
    </div>
  );
  const firstSection = shownLines.findIndex((l) => l.kind === "header");
  const addRootAt = firstSection < 0 ? shownLines.length : firstSection;

  return (
    <div className="strategy" ref={rootRef} style={columnVars(widths) as CSSProperties}>
      {undo && (
        <div className="undo-toast" role="status">
          <span>{undo.text}</span>
          <button type="button" className="undo-toast-btn" onClick={pressUndo}>
            Undo
          </button>
        </div>
      )}
      <div className="board-toolbar strategy-toolbar">
        <label className="strategy-depth">
          <span className="strategy-depth-label">Hierarchy</span>
          <select
            value={depth.from}
            onChange={(e) => setDepth({ ...depth, from: e.target.value as StrategyKind })}
            aria-label="From level"
          >
            {levelOptions()}
          </select>
          <span className="strategy-depth-to">to</span>
          <select
            value={depth.to}
            onChange={(e) => setDepth({ ...depth, to: e.target.value as StrategyKind })}
            aria-label="To level"
          >
            {levelOptions()}
          </select>
        </label>
        <div className="segmented strategy-zoom" role="group" aria-label="Zoom">
          {(["3m", "1y", "fit"] as Zoom[]).map((z) => (
            <button
              key={z}
              type="button"
              className={`segment${zoom === z ? " segment-active" : ""}`}
              onClick={() => setZoom(z)}
            >
              {z === "3m" ? "3M" : z === "1y" ? "1Y" : "Fit"}
            </button>
          ))}
        </div>
        <span className="strategy-range" title="The window the timeline shows">
          {shortDate(range.from, today)} – {shortDate(range.to, today)}
        </span>
        {/* What the plan is for, who carries it out, who answers for it
            (ownership.md, O7, O8): the server's filters, remembered. */}
        <FilterSelect
          label="Team"
          options={teamOptions(roster)}
          selected={filters.teams}
          onChange={(teams) => onFilters({ ...filters, teams })}
          manage={onManageTeams ? { label: "Manage teams", onClick: onManageTeams } : undefined}
        />
        <FilterSelect
          label="Customer"
          options={customerOptions(board.customers)}
          selected={filters.customers}
          onChange={(customers) => onFilters({ ...filters, customers })}
          manage={onManageCustomers ? { label: "Manage customers", onClick: onManageCustomers } : undefined}
        />
        <FilterSelect
          label="Owner"
          options={owners.map((p) => ({ value: p, label: p }))}
          selected={filters.owners}
          onChange={(owners) => onFilters({ ...filters, owners })}
        />
        {/* What stage the work is in (workstatus.ts): a task by its own
            and by its lock, a plan item by itself for Locked and No items
            and otherwise by the tasks under it — the server's filter too.
            "Hide done" is the one click for everything not done. */}
        <FilterSelect
          label="Status"
          anyLabel="all"
          options={STATUS_FILTERS.map((s) => ({ value: s, label: STATUS_LABELS[s] }))}
          selected={filters.statuses}
          onChange={(statuses) => onFilters({ ...filters, statuses })}
          quick={{
            label: "Hide done",
            values: HIDE_DONE,
            onClick: () => onFilters({ ...filters, statuses: [...HIDE_DONE] }),
          }}
          title="Show only work in progress, in a backlog, locked by a dependency, plan items with no items, or done"
        />
      </div>

      {shownLines.length === 0 ? (
        <>
        <p className="placeholder strategy-empty">
          {filtering ? (
            <>
              {board.cards.length === 0 ? "Nothing matches the filters." : "Nothing to draw at this depth and filter."}{" "}
              <button type="button" className="btn strategy-clear-filters" onClick={() => onFilters(NO_STRATEGY_FILTERS)}>
                Clear the filters
              </button>
            </>
          ) : board.cards.length === 0 ? (
            "Nothing on the Strategy board yet: add a goal, an initiative or an epic, and file work under it from a card's pane."
          ) : (
            "Nothing to draw at this depth."
          )}
        </p>
        {!filtering && addRoot}
        </>
      ) : (
        <DndContext
          sensors={sensors}
          onDragStart={onDragStart}
          onDragMove={onDragMove}
          onDragEnd={onDragEnd}
          onDragCancel={settle}
        >
          <div ref={bodyRef} className={`strategy-body${dragging ? " strategy-body-dragging" : ""}`}>
            <div className="strategy-table">
              <div ref={headRef} className="strategy-head" style={{ height: HEAD_PX }}>
                <span className="strategy-cell strategy-cell-title strategy-head-cell">
                  Card{handle("title", "Resize the Card column")}
                </span>
                <span className="strategy-cell strategy-cell-owner strategy-head-cell">
                  Owner{handle("owner", "Resize the Owner column")}
                </span>
                <span className="strategy-cell strategy-cell-date strategy-head-cell">
                  Start{handle("start", "Resize the Start column")}
                </span>
                <span className="strategy-cell strategy-cell-date strategy-head-cell">
                  End{handle("end", "Resize the End column")}
                </span>
                <span className="strategy-cell strategy-cell-status strategy-head-cell">
                  Status{handle("status", "Resize the table against the timeline")}
                </span>
              </div>
              {shownLines.map((line, i) => {
                const before = i === addRootAt ? <div key="addroot">{addRoot}</div> : null;
                if (line.kind === "header") {
                  return [
                    before,
                    <div key={line.key} className="strategy-row strategy-section" style={{ height: ROW_PX }} aria-hidden="false">
                      <span className="strategy-cell strategy-cell-title strategy-section-label">{line.label}</span>
                    </div>,
                  ];
                }
                const r = line.row;
                const card = r.node.card;
                const planning = isPlanning(card);
                const kids = childLevels(r.node.kind);
                const isOver = !!hover && hover.target.overId === card.itemId;
                return (
                  <div key={card.itemId} className="strategy-rowgroup">
                    <StrategyRow
                      row={r}
                      today={today}
                      open={!folded.has(card.itemId)}
                      focus={focusRow === card.itemId}
                      ghost={ghosts.has(card.itemId)}
                      forbid={isOver && !hover.valid}
                      mark={mark && mark.row === card.itemId ? mark : null}
                      dragging={!!dragging}
                      onToggle={() => toggle(card.itemId)}
                      onOpen={() => {
                        if (!justDropped.current && !dragging) {
                          onOpen(card);
                        }
                      }}
                      onAdd={planning && kids.length > 0 ? () => setAddingUnder(addingUnder === card.itemId ? null : card.itemId) : null}
                      onRemove={planning ? () => setRemoveChoice(card) : null}
                      owner={ownerCell(card, r.node.kind)}
                      onStatus={(badge) => {
                        statusAnchor.current = badge;
                        setStatusFor(card);
                      }}
                      menuOpen={statusFor?.itemId === card.itemId}
                      rowRef={(el) => {
                        if (el) {
                          rowRefs.current.set(card.itemId, el);
                        } else {
                          rowRefs.current.delete(card.itemId);
                        }
                      }}
                    />
                    {addingRow && addingRow.node.card.itemId === card.itemId && (
                      <div className="strategy-row strategy-row-form" style={{ paddingLeft: 30 + (r.depth + 1) * INDENT_PX }}>
                        <AddCard
                          autoOpen
                          compact
                          placeholder={`Under «${card.title}»…`}
                          forcedTeam={null}
                          picker={{
                            title: "Level",
                            options: planningOptions(kids),
                            initial: kids[0],
                          }}
                          onCreate={(title, _team, picked) => {
                            createUnder(title, (picked as CardType | "task") || kids[0], card.itemId);
                          }}
                          onClosed={() => setAddingUnder(null)}
                        />
                      </div>
                    )}
                  </div>
                );
              })}
              {addRootAt === shownLines.length && addRoot}
            </div>
            <div className="strategy-timeline">
              <div className="strategy-track" style={{ width: trackW }}>
                <div className="strategy-ticks" style={{ height: HEAD_PX }}>
                  {ticks.map((t) => (
                    <span key={t.date} className="strategy-tick" style={{ left: `${fractionOf(t.date, range) * 100}%` }}>
                      {t.label}
                    </span>
                  ))}
                </div>
                {todayAt !== null && (
                  <div className="strategy-today" style={{ left: `${todayAt * 100}%` }} title={`Today, ${today}`} />
                )}
                {shownLines.map((line, i) =>
                  line.kind === "header" ? (
                    [
                      i === addRootAt ? <div key="addroot-lane" className="strategy-lane strategy-lane-addroot" style={{ height: ROW_PX }} /> : null,
                      <div key={line.key} className="strategy-lane strategy-lane-section" style={{ height: ROW_PX }} />,
                    ]
                  ) : (
                    <div key={line.row.node.card.itemId} className="strategy-lanegroup">
                      {lane(line.row, i)}
                      {addingRow && addingRow.node.card.itemId === line.row.node.card.itemId && (
                        <div className="strategy-lane" style={{ height: ROW_PX }} />
                      )}
                    </div>
                  ),
                )}
                {addRootAt === shownLines.length && <div className="strategy-lane strategy-lane-addroot" style={{ height: ROW_PX }} />}
              </div>
            </div>
          </div>
          <DragOverlay dropAnimation={null}>
            {draggedCard && (
              <div className={`strategy-drag-overlay${hover && !hover.valid ? " strategy-drag-overlay-forbid" : ""}`}>
                <KindIcon kind={buildTree([draggedCard])[0].kind} size={15} />
                <span className="strategy-title">{draggedCard.title}</span>
              </div>
            )}
          </DragOverlay>
        </DndContext>
      )}
      <Dropdown open={!!statusFor} anchorRef={statusAnchor} onClose={() => setStatusFor(null)} className="strategy-status-menu" role="menu">
        {statusFor && menu && (
          <>
            <button
              type="button"
              role="menuitem"
              className="strategy-status-item"
              disabled={!menu.unassign}
              title={menu.unassign ? "Take out of work, back to the backlog" : NOTHING_IN_WORK}
              onClick={() => {
                // The confirm counts what the SERVER would take: the
                // Strategy may be filtered, and only the whole board knows
                // every task in work under the row.
                const card = statusFor;
                setStatusFor(null);
                void provider
                  .previewUnassign(card.itemId)
                  .then((would) => {
                    // Nothing in work under it any more: no confirm to open,
                    // but the press is answered rather than swallowed.
                    const uids = unassignPreview(would);
                    const notice = unassignNotice(uids);
                    if (notice) {
                      onError(notice);
                    } else if (uids) {
                      setUnassigning({ card, uids });
                    }
                  })
                  .catch((err: unknown) => onError(errText(err)));
              }}
            >
              <span className="strategy-status strategy-status-unassigned">Unassign</span>
            </button>
          </>
        )}
      </Dropdown>
      {unassigning && (
        <UnassignDialog
          count={unassigning.uids.length}
          onClose={() => setUnassigning(null)}
          onConfirm={() => answerUnassign(unassigning.card, unassigning.uids)}
        />
      )}
      {removeChoice && (
        <RemoveChoiceDialog
          title={removeChoice.title}
          progress={removeChoice.hierarchy ? Math.round((100 * removeChoice.hierarchy.done) / Math.max(removeChoice.hierarchy.total, 1)) : 0}
          choices={hierarchyChoices(removeChoice)}
          subtasks={0}
          note={childrenNote(board.cards, removeChoice.itemId)}
          onClose={() => setRemoveChoice(null)}
          onSubmit={(choice) => answerRemoval(removeChoice, choice as HierarchyChoice)}
        />
      )}
    </div>
  );
}

/** UnassignDialog asks before UNASSIGNED takes work out of hand (K16): how
 *  many tasks leave Team and Me for their team's backlog. */
export function UnassignDialog({ count, onClose, onConfirm }: { count: number; onClose: () => void; onConfirm: () => void }) {
  // The dialog takes the focus as it opens, so Escape reaches its handler.
  const dialog = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    dialog.current?.focus();
  }, []);
  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div
        ref={dialog}
        tabIndex={-1}
        className="modal modal-narrow"
        role="dialog"
        aria-modal="true"
        aria-label="Take out of work?"
        onClick={(e) => e.stopPropagation()}
        onKeyDown={(e) => {
          if (e.key === "Escape") {
            onClose();
          }
        }}
      >
        <div className="modal-header">
          <h2 className="modal-title">Take out of work?</h2>
          <button type="button" className="modal-close" onClick={onClose} aria-label="Close">
            ✕
          </button>
        </div>
        <div className="modal-body">
          <p className="take-dialog-text">{unassignQuestion(count)}</p>
          <div className="take-dialog-actions">
            <button type="button" className="btn" onClick={onClose}>
              Cancel
            </button>
            <button
              type="button"
              className="btn btn-primary"
              onClick={() => {
                onConfirm();
                onClose();
              }}
            >
              Take out of work
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

