import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import {
  DndContext,
  DragOverlay,
  PointerSensor,
  pointerWithin,
  rectIntersection,
  useDraggable,
  useDroppable,
  useSensor,
  useSensors,
  type CollisionDetection,
  type DragEndEvent,
  type DragMoveEvent,
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
  SECTION_LABEL,
  STATUS_LABEL,
  barOf,
  buildTree,
  childrenNote,
  daysBetween,
  DropWriteError,
  dropCommit,
  droppable,
  fractionOf,
  hierarchyChoices,
  isUnder,
  place,
  placementFor,
  planningRemovalPlan,
  previewCards,
  rangeFor,
  rows as rowsOf,
  sectionsOf,
  statusOf,
  ticks as ticksOf,
  visibleLevels,
  zoneOf,
  type DepthRange,
  type DropZone,
  type HierarchyChoice,
  type Placement,
  type RoadmapKind,
  type RoadmapSection,
  type Row,
  type Zoom,
  writeDrop,
  PROJECT_CARDS_DEFAULT,
  projectCardsShown,
} from "../roadmap";
import { barColor } from "../stages";
import { isPlanning } from "../subtasks";
import { ownerLabel, ownerOf, type Owner } from "../owner";
import { displayName, type Avatars, type Names } from "../users";
import { AddCard } from "./AddCard";
import { KindIcon } from "./KindIcon";
import { OwnerBadge } from "./OwnerBadge";
import { OwnerPicker } from "./OwnerPicker";
import { RemoveChoiceDialog } from "./RemoveChoiceDialog";

// The Roadmap: the hierarchy as a tree of rows on the left and a timeline
// of bars on the right, one row per card from a goal down to a subtask
// (docs/design/hierarchy.md, "The Roadmap tab"). What a row IS, which rows a
// depth range draws, what a row's status is, where its bar lands, and what
// a drag may do are roadmap.ts's answers; this component draws them and
// makes the board's gestures — a create under a row, its ×, and the drag.

interface RoadmapBoardProps {
  board: Board;
  provider: Provider;
  /** The board's teams: what the Owner column offers. The Roadmap filters by
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
}

const LS_DEPTH = "aeman.roadmap.depth";
const LS_ZOOM = "aeman.roadmap.zoom";
const LS_FOLDED = "aeman.roadmap.folded";
const LS_PROJECTS = "aeman.roadmap.projectCards";

/** ROW_PX is the height of one row, on both sides, so the table and the
 *  timeline stay aligned without ever measuring each other. */
const ROW_PX = 30;
const HEAD_PX = 26;
const TABLE_PX = 680;
/** INDENT_PX is one level of the tree. */
const INDENT_PX = 18;

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
      if (LEVELS.includes(parsed.from as RoadmapKind) && LEVELS.includes(parsed.to as RoadmapKind)) {
        return { from: parsed.from as RoadmapKind, to: parsed.to as RoadmapKind };
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

function readProjects(): boolean {
  try {
    const raw = localStorage.getItem(LS_PROJECTS);
    return raw === null ? PROJECT_CARDS_DEFAULT : raw === "1";
  } catch {
    return PROJECT_CARDS_DEFAULT;
  }
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

/** childLevels is what the + on a planning row may make under it: the
 *  levels below its own, and under an epic a TASK — born with no team and
 *  no day, it is parked in the no-team backlog until somebody takes it into
 *  work (H7, K3). */
function childLevels(kind: RoadmapKind): (CardType | "task")[] {
  if (kind === "goal") {
    return ["initiative", "epic"];
  }
  if (kind === "initiative") {
    return ["epic"];
  }
  if (kind === "epic") {
    return ["task"];
  }
  return [];
}

/** The pointer picks the row under it; when it is over none (the gap at the
 *  bottom), the nearest overlapping one. */
const detect: CollisionDetection = (args) => {
  const within = pointerWithin(args);
  return within.length > 0 ? within : rectIntersection(args);
};

/** A line of the table: a row of the tree or of a section, or a section's
 *  header. The timeline draws a lane per line, so the two panes stay in
 *  step whatever stands between the rows. */
type RowLine = { kind: "row"; row: Row; section: RoadmapSection };
type Line = RowLine | { kind: "header"; key: string; label: string };
type RoadmapNodeLike = Parameters<typeof rowsOf>[0][number];

/** Hover is where the drag stands: the row under it, the zone of that row,
 *  the placement that reads as, and whether the level rule allows it. */
interface Hover {
  overId: string;
  zone: DropZone;
  placement: Placement | null;
  valid: boolean;
}

interface RowProps {
  row: Row;
  /** Where the row stands: in the tree, or in one of the sections below it.
   *  A section row drags INTO the tree and is dropped on by nothing —
   *  dragging within the sections does nothing. */
  section: RoadmapSection;
  today: string;
  open: boolean;
  focus: boolean;
  ghost: boolean;
  forbid: boolean;
  dragging: boolean;
  onToggle: () => void;
  onOpen: () => void;
  onAdd: (() => void) | null;
  onRemove: (() => void) | null;
  /** The Owner cell's content, for a task-kind row; nothing on a planning
   *  card or a project card, which have no owner. */
  owner: ReactNode;
  rowRef: (el: HTMLDivElement | null) => void;
}

/** RoadmapRow is one line of the table: draggable, a drop target, and the
 *  row's own controls. The tree is drawn by indent guides and a chevron
 *  with a hand's worth of hit area, the kind by its pictogram. A drag's
 *  preview is the moved rows themselves, dimmed — the row under the pointer
 *  wears no outline and no line of its own. */
function RoadmapRow({ row, section, today, open, focus, ghost, forbid, dragging, onToggle, onOpen, onAdd, onRemove, owner, rowRef }: RowProps) {
  const { node, depth, foldable } = row;
  const card = node.card;
  const id = card.itemId;
  const { attributes, listeners, setNodeRef: setDragRef, isDragging } = useDraggable({ id });
  const { setNodeRef: setDropRef } = useDroppable({ id, disabled: section !== "tree" });
  const planning = isPlanning(card);
  const status = statusOf(card);
  const cls = [
    "roadmap-row",
    focus ? "roadmap-row-focus" : "",
    ghost || isDragging ? "roadmap-row-ghost" : "",
    forbid ? "roadmap-row-forbid" : "",
    dragging ? "roadmap-row-while-drag" : "",
  ]
    .filter(Boolean)
    .join(" ");
  return (
    <div
      ref={(el) => {
        setDragRef(el);
        setDropRef(el);
        rowRef(el);
      }}
      className={cls}
      style={{ height: ROW_PX }}
      onClick={onOpen}
      {...attributes}
      {...listeners}
    >
      <span className="roadmap-cell roadmap-cell-title" style={{ paddingLeft: 6 + depth * INDENT_PX }}>
        {Array.from({ length: depth }, (_, i) => (
          <span key={i} className="roadmap-guide" style={{ left: 6 + i * INDENT_PX + 10 }} aria-hidden="true" />
        ))}
        {foldable > 0 ? (
          <button
            type="button"
            className={`roadmap-toggle${open ? " roadmap-toggle-open" : ""}`}
            aria-label={open ? "Fold" : `Unfold ${foldable}`}
            aria-expanded={open}
            onClick={(e) => {
              e.stopPropagation();
              onToggle();
            }}
            onPointerDown={(e) => e.stopPropagation()}
          >
            <svg viewBox="0 0 16 16" width="12" height="12" aria-hidden="true">
              <path d="M5.5 3.5 10 8l-4.5 4.5" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
          </button>
        ) : (
          // A leaf keeps the chevron's width so the indent lines up, and
          // nothing else: not a button, nothing to hover or focus.
          <span className="roadmap-toggle-spacer" aria-hidden="true" />
        )}
        <KindIcon kind={node.kind} size={15} />
        <span className="roadmap-title" title={card.title}>
          {card.title}
        </span>
        {planning && card.hierarchy && card.hierarchy.total > 0 && (
          <span className="roadmap-count" title={`${card.hierarchy.done} of ${card.hierarchy.total} tasks done`}>
            {card.hierarchy.done}/{card.hierarchy.total}
          </span>
        )}
        {onAdd && (
          <button
            type="button"
            className="roadmap-row-add"
            title={`Add under «${card.title}»`}
            aria-label={`Add under ${card.title}`}
            onClick={(e) => {
              e.stopPropagation();
              onAdd();
            }}
            onPointerDown={(e) => e.stopPropagation()}
          >
            +
          </button>
        )}
        {onRemove && (
          <button
            type="button"
            className="roadmap-row-x"
            title={`Delete «${card.title}», or take it out from under its parent`}
            aria-label={`Delete ${card.title}`}
            onClick={(e) => {
              e.stopPropagation();
              onRemove();
            }}
            onPointerDown={(e) => e.stopPropagation()}
          >
            ×
          </button>
        )}
      </span>
      <span className="roadmap-cell roadmap-cell-owner">{owner}</span>
      <span className="roadmap-cell roadmap-cell-date">{shortDate(card.startDate, today)}</span>
      <span className="roadmap-cell roadmap-cell-date">{shortDate(card.day, today)}</span>
      <span className="roadmap-cell">
        <span className={`roadmap-status roadmap-status-${status}`}>{STATUS_LABEL[status]}</span>
      </span>
    </div>
  );
}

/** OwnerCell is what a task row shows in its Owner column: the owner badge
 *  (the team's avatar, the person's over it, one label) — and the picker
 *  behind it. */
function OwnerCell({ owner, avatars, names }: { owner: Owner; avatars?: Avatars; names?: Names }) {
  const label = ownerLabel(owner, (l) => displayName(l, names));
  return (
    <span className={`roadmap-owner${owner.team || owner.person ? "" : " roadmap-owner-none"}`} title={label}>
      <OwnerBadge owner={owner} avatars={avatars} names={names} />
    </span>
  );
}

export function RoadmapBoard({
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
}: RoadmapBoardProps) {
  const today = todayIso();
  const [depth, setDepthState] = useState<DepthRange>(readDepth);
  const [zoom, setZoomState] = useState<Zoom>(readZoom);
  const [folded, setFoldedState] = useState<Set<string>>(readFolded);
  const [showProjects, setShowProjectsState] = useState<boolean>(readProjects);
  const [whoFilter, setWhoFilter] = useState<string | null>(null);
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
  const setShowProjects = (next: boolean) => {
    setShowProjectsState(next);
    try {
      localStorage.setItem(LS_PROJECTS, next ? "1" : "0");
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

  // The people on the Roadmap, for the one filter it has: every assignee a
  // task carries. There is no team filter — a planning card has no team,
  // and the rows a lead reads here are the planning cards.
  const people = useMemo(() => {
    const out = new Set<string>();
    for (const c of board.cards) {
      for (const a of c.assignees) {
        out.add(a);
      }
    }
    return [...out].sort();
  }, [board.cards]);

  // A filter keeps the cards that match and every card ABOVE one (the
  // lineage names them), so a matching task still stands under its epic
  // and the epic under its goal — a tree with holes is no tree.
  const shown = useMemo(() => {
    if (!whoFilter) {
      return board.cards;
    }
    const keep = new Set<string>();
    for (const c of board.cards) {
      if (c.assignees.includes(whoFilter)) {
        keep.add(c.itemId);
        for (const up of c.lineage ?? []) {
          keep.add(up.uid);
        }
      }
    }
    return board.cards.filter((c) => keep.has(c.itemId));
  }, [board.cards, whoFilter]);

  // ---- the drag: where it stands, and the preview of where it would land ----
  const [dragging, setDragging] = useState<string | null>(null);
  const [hover, setHover] = useState<Hover | null>(null);
  const justDropped = useRef(false);
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 5 } }));

  const expanded = (id: string) => !folded.has(id);
  // The lines of the table: the tree's rows first, then each section below
  // it — a header line and its rows. The sections draw tasks and their
  // subtasks (when the depth range reaches them); the depth range's upper
  // levels are the tree's business.
  const lines = (cards: readonly CardModel[]): Line[] => {
    const sections = sectionsOf(buildTree(projectCardsShown(cards, showProjects)));
    const sub = visibleLevels(depth).has("subtask");
    const flat: DepthRange = { from: "task", to: sub ? "subtask" : "task" };
    const out: Line[] = rowsOf(sections.tree, depth, expanded).map((row) => ({ kind: "row", row, section: "tree" as const }));
    const add = (section: Exclude<RoadmapSection, "tree">, label: string, nodes: readonly RoadmapNodeLike[], key: string) => {
      if (nodes.length === 0) {
        return;
      }
      out.push({ kind: "header", key, label });
      for (const row of rowsOf(nodes, flat, expanded)) {
        out.push({ kind: "row", row, section });
      }
    };
    add("in-work", SECTION_LABEL["in-work"], sections.inWork, "in-work");
    for (const shelf of sections.backlog) {
      add("backlog", `${SECTION_LABEL.backlog} · ${shelf.team || "no team"}`, shelf.nodes, `backlog-${shelf.team}`);
    }
    return out;
  };
  const baseLines = useMemo(() => lines(shown), [shown, depth, folded]); // eslint-disable-line react-hooks/exhaustive-deps
  // The tree's rows alone, which is what a drop is read against: a section
  // row is dropped on by nothing.
  const baseRows = useMemo(() => baseLines.filter((l) => l.kind === "row" && l.section === "tree").map((l) => (l as RowLine).row), [baseLines]);
  // While a drag stands over a place it may land, the lines are the listing
  // as it would read after the drop: the subtree is drawn there, dimmed,
  // before the pointer is released (roadmap.previewCards) — a section row
  // dragged into the tree leaves its section in the preview too.
  const previewed = dragging && hover?.valid && hover.placement ? previewCards(shown, dragging, hover.placement) : null;
  const shownLines = useMemo(
    () => (previewed ? lines(previewed) : baseLines),
    [previewed, baseLines, depth, folded], // eslint-disable-line react-hooks/exhaustive-deps
  );
  const rows = useMemo(() => shownLines.filter((l): l is RowLine => l.kind === "row").map((l) => l.row), [shownLines]);
  const ghosts = useMemo(() => {
    if (!dragging || !previewed) {
      return new Set<string>();
    }
    const out = new Set<string>([dragging]);
    for (const c of previewed) {
      if (isUnder(previewed, c.itemId, dragging)) {
        out.add(c.itemId);
      }
    }
    return out;
  }, [dragging, previewed]);

  const onDragStart = (e: DragStartEvent) => {
    setDragging(String(e.active.id));
    setHover(null);
    setAddingUnder(null);
  };
  const onDragMove = (e: DragMoveEvent) => {
    if (!dragging) {
      return;
    }
    const over = e.over;
    if (!over) {
      setHover(null);
      return;
    }
    const overId = String(over.id);
    // The preview puts the dragged rows under the pointer; a reading off
    // one of them would undo the preview that put them there. Keep what
    // was read off the row before.
    if (overId === dragging || (previewed && isUnder(previewed, overId, dragging))) {
      return;
    }
    const lifted = e.active.rect.current.translated;
    if (!lifted) {
      return;
    }
    const fraction = (lifted.top + lifted.height / 2 - over.rect.top) / Math.max(over.rect.height, 1);
    const zone = zoneOf(fraction);
    const placement = placementFor(shown, baseRows, dragging, overId, zone);
    const valid = !!placement && droppable(shown, dragging, placement.parent);
    setHover((cur) =>
      cur && cur.overId === overId && cur.zone === zone && cur.valid === valid ? cur : { overId, zone, placement, valid },
    );
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
    const parent = commit.parent ? board.cards.find((c) => c.itemId === commit.parent) : undefined;
    if (commit.parentChanged) {
      patchCard(id, {
        parent: commit.parent ?? undefined,
        lineage: parent ? [...(parent.lineage ?? []), { uid: parent.itemId, type: parent.type, title: parent.title }] : undefined,
      });
    }
    // The listing order is the board's; with a filter on, the order of what
    // is shown says nothing about the rest, so it is left to the response.
    if (shown === board.cards) {
      reorderCards?.(commit.order);
    }
    // A parent the server took stays: only a refused parent is rolled
    // back, and the reload settles the order either way.
    void writeDrop(id, commit, provider).catch((err: unknown) => {
      const taken = err instanceof DropWriteError && err.parentWritten;
      if (!taken) {
        patchCard(id, before);
      }
      onError(errText(err instanceof DropWriteError ? err.cause : err));
      reload();
    });
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
    onFocused?.();
    // Unfolding is a preference write, not a dependency of the reveal.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [focusId, board.cards]);
  useEffect(() => {
    if (focusRow) {
      rowRefs.current.get(focusRow)?.scrollIntoView({ block: "center" });
    }
  }, [focusRow, rows]);

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
  const ownerCell = (card: CardModel, kind: RoadmapKind) => {
    if (kind !== "task" && kind !== "subtask") {
      return null;
    }
    const owner = ownerOf(card);
    const cell = <OwnerCell owner={owner} avatars={avatars} names={names} />;
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
      void provider.removeCard(plan.request.uid, DELETE_INTENT).catch((err: unknown) => {
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
        className={`roadmap-lane${ghosts.has(card.itemId) ? " roadmap-lane-ghost" : ""}`}
        style={{ height: ROW_PX }}
        onClick={() => onOpen(card)}
      >
        {placed && bar && (
          <div
            className={[
              "roadmap-bar",
              `roadmap-bar-${r.node.kind}`,
              done ? "roadmap-bar-done" : "",
              bar.point ? "roadmap-bar-point" : "",
              placed.clippedLeft ? "roadmap-bar-clip-l" : "",
              placed.clippedRight ? "roadmap-bar-clip-r" : "",
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

  return (
    <div className="roadmap">
      <div className="board-toolbar roadmap-toolbar">
        <AddCard
          placeholder="Add a goal, an initiative or an epic…"
          forcedTeam={null}
          picker={{
            title: "Level",
            options: planningOptions(["initiative", "epic", "goal"]),
            initial: "initiative",
          }}
          onCreate={(title, _team, picked) => createUnder(title, (picked as CardType) || "initiative")}
        />
        <label className="roadmap-depth">
          <span className="roadmap-depth-label">Hierarchy</span>
          <select
            value={depth.from}
            onChange={(e) => setDepth({ ...depth, from: e.target.value as RoadmapKind })}
            aria-label="From level"
          >
            {levelOptions()}
          </select>
          <span className="roadmap-depth-to">to</span>
          <select
            value={depth.to}
            onChange={(e) => setDepth({ ...depth, to: e.target.value as RoadmapKind })}
            aria-label="To level"
          >
            {levelOptions()}
          </select>
        </label>
        <button
          type="button"
          className={`segment roadmap-projects${showProjects ? " segment-active" : ""}`}
          aria-pressed={showProjects}
          title={showProjects ? "Hide the Project board's cards" : "Show the Project board's cards in the tree"}
          onClick={() => setShowProjects(!showProjects)}
        >
          Project cards
        </button>
        <div className="segmented roadmap-zoom" role="group" aria-label="Zoom">
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
        <span className="roadmap-range" title="The window the timeline shows">
          {shortDate(range.from, today)} – {shortDate(range.to, today)}
        </span>
        {people.length > 0 && (
          <select
            className="roadmap-who"
            value={whoFilter ?? ""}
            onChange={(e) => setWhoFilter(e.target.value || null)}
            aria-label="Assignee filter"
          >
            <option value="">Anyone</option>
            {people.map((p) => (
              <option key={p} value={p}>
                {p}
              </option>
            ))}
          </select>
        )}
      </div>

      {shownLines.length === 0 ? (
        <p className="placeholder roadmap-empty">
          {board.cards.length === 0
            ? "Nothing on the roadmap yet: add a goal, an initiative or an epic, and file work under it from a card's pane."
            : "Nothing to draw at this depth and filter."}
        </p>
      ) : (
        <DndContext
          sensors={sensors}
          collisionDetection={detect}
          onDragStart={onDragStart}
          onDragMove={onDragMove}
          onDragEnd={onDragEnd}
          onDragCancel={settle}
        >
          <div className={`roadmap-body${dragging ? " roadmap-body-dragging" : ""}`}>
            <div className="roadmap-table" style={{ width: TABLE_PX }}>
              <div className="roadmap-head" style={{ height: HEAD_PX }}>
                <span className="roadmap-cell roadmap-cell-title">Card</span>
                <span className="roadmap-cell roadmap-cell-owner">Owner</span>
                <span className="roadmap-cell roadmap-cell-date">Start</span>
                <span className="roadmap-cell roadmap-cell-date">End</span>
                <span className="roadmap-cell">Status</span>
              </div>
              {shownLines.map((line) => {
                if (line.kind === "header") {
                  return (
                    <div key={line.key} className="roadmap-row roadmap-section" style={{ height: ROW_PX }} aria-hidden="false">
                      <span className="roadmap-cell roadmap-cell-title roadmap-section-label">{line.label}</span>
                    </div>
                  );
                }
                const r = line.row;
                const card = r.node.card;
                const planning = isPlanning(card);
                const kids = childLevels(r.node.kind);
                const isOver = !!hover && hover.overId === card.itemId;
                return (
                  <div key={card.itemId} className="roadmap-rowgroup">
                    <RoadmapRow
                      row={r}
                      section={line.section}
                      today={today}
                      open={!folded.has(card.itemId)}
                      focus={focusRow === card.itemId}
                      ghost={ghosts.has(card.itemId)}
                      forbid={isOver && !hover.valid}
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
                      rowRef={(el) => {
                        if (el) {
                          rowRefs.current.set(card.itemId, el);
                        } else {
                          rowRefs.current.delete(card.itemId);
                        }
                      }}
                    />
                    {addingRow && addingRow.node.card.itemId === card.itemId && (
                      <div className="roadmap-row roadmap-row-form" style={{ paddingLeft: 30 + (r.depth + 1) * INDENT_PX }}>
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
            </div>
            <div className="roadmap-timeline">
              <div className="roadmap-track" style={{ width: trackW }}>
                <div className="roadmap-ticks" style={{ height: HEAD_PX }}>
                  {ticks.map((t) => (
                    <span key={t.date} className="roadmap-tick" style={{ left: `${fractionOf(t.date, range) * 100}%` }}>
                      {t.label}
                    </span>
                  ))}
                </div>
                {todayAt !== null && (
                  <div className="roadmap-today" style={{ left: `${todayAt * 100}%` }} title={`Today, ${today}`} />
                )}
                {shownLines.map((line, i) =>
                  line.kind === "header" ? (
                    <div key={line.key} className="roadmap-lane roadmap-lane-section" style={{ height: ROW_PX }} />
                  ) : (
                    <div key={line.row.node.card.itemId} className="roadmap-lanegroup">
                      {lane(line.row, i)}
                      {addingRow && addingRow.node.card.itemId === line.row.node.card.itemId && (
                        <div className="roadmap-lane" style={{ height: ROW_PX }} />
                      )}
                    </div>
                  ),
                )}
              </div>
            </div>
          </div>
          <DragOverlay dropAnimation={null}>
            {draggedCard && (
              <div className={`roadmap-drag-overlay${hover && !hover.valid ? " roadmap-drag-overlay-forbid" : ""}`}>
                <KindIcon kind={buildTree([draggedCard])[0].kind} size={15} />
                <span className="roadmap-title">{draggedCard.title}</span>
              </div>
            )}
          </DragOverlay>
        </DndContext>
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
