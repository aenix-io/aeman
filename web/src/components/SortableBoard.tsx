import { useEffect, useMemo, useRef, useState, type ReactNode, type Ref } from "react";
import {
  DndContext,
  DragOverlay,
  PointerSensor,
  closestCorners,
  useDroppable,
  useSensor,
  useSensors,
  type CollisionDetection,
  type DragEndEvent,
  type DragMoveEvent,
  type DragOverEvent,
  type DragStartEvent,
} from "@dnd-kit/core";
import {
  SortableContext,
  useSortable,
  verticalListSortingStrategy,
  arrayMove,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import type { Card as CardModel } from "../providers/types";

/** A group is one sortable container (a zone band, or an engineer×zone cell). */
export interface BoardGroup<Meta> {
  /** Stable container id, unique within the board. */
  key: string;
  /** Provider-facing info (zone, engineer) needed to persist a drop. */
  meta: Meta;
  /** Cards currently in this group, in display order. */
  cards: CardModel[];
}

/** Result of a committed drop, handed to the parent to persist. */
export interface DropResult<Meta> {
  card: CardModel;
  fromMeta: Meta;
  toMeta: Meta;
  /** Final per-group ordering of card ids across the whole (visible) board. */
  groups: { meta: Meta; ids: string[] }[];
  /** The itemId the card was dropped under as a subtask, resolved exactly as
   *  the placeholder previewed it (indent level, Todoist-style), or null for
   *  a standalone drop. */
  groupUnder: string | null;
}

interface SortableBoardProps<Meta> {
  /** Source-of-truth groups derived from board state. */
  groups: BoardGroup<Meta>[];
  /** Renders the container shell + an inner list region for a group.
   *  dropRef MUST be attached to the element that should accept drops (so empty
   *  groups still receive the card); isOver is true while the active card hovers. */
  renderGroup: (
    group: BoardGroup<Meta>,
    body: ReactNode,
    state: { isOver: boolean; dropRef: Ref<HTMLElement> },
  ) => ReactNode;
  /** Renders a single card (without sortable wiring — that is added here). */
  renderCard: (card: CardModel, group: BoardGroup<Meta>) => ReactNode;
  /** Renders the lifted card inside the DragOverlay. */
  renderOverlay: (card: CardModel) => ReactNode;
  /** Optional layout wrapper. Receives every rendered group node keyed by
   *  group.key and arranges them (e.g. the Team grid groups cells into columns).
   *  Defaults to rendering the group nodes in their natural order. */
  renderLayout?: (nodes: Map<string, ReactNode>, groups: BoardGroup<Meta>[]) => ReactNode;
  /** Commit a drop: persist zone/engineer/position optimistically. */
  onDrop: (result: DropResult<Meta>) => void;
  /** Cards draggable from `extra`, keyed by their dnd id, for overlay + drop. */
  externalCards?: Map<string, CardModel>;
  /** Commit a drop of an external (extra) draggable onto an over id. */
  onExternalDrop?: (card: CardModel, overId: string | null) => void;
  /** Commit a drop on a card's middle band (over id "grp:<dnd id>"): group the
   *  dragged card under it as a subtask. */
  onGroupDrop?: (card: CardModel, parentId: string) => void;
  /** Fires with the card id the drag would group under (its middle band is
   *  hovered), null when the drag leaves it — the board highlights the target. */
  onHoverCard?: (cardId: string | null) => void;
  /** Fires with the lifted card's itemId on drag start and null when the drag
   *  settles — the board folds an expanded parent for the flight. */
  onDragActiveCard?: (cardId: string | null) => void;
  /** Whether the active card may group under the target (e.g. depth limits). */
  canGroup?: (active: CardModel, target: CardModel) => boolean;
  /** Whether a card may cross from one group into another. A drop the board
   *  will refuse must not be PREVIEWED either: the placeholder opening a gap
   *  in a group that then rejects the card is a promise the board does not
   *  keep, and the card visibly springs back on release. Grouping is asked
   *  separately (canGroup) — a subtask joins its parent's group by nesting,
   *  not by crossing. Absent means every crossing is allowed. */
  canMoveTo?: (card: CardModel, from: Meta, to: Meta) => boolean;
  /** Optional class wrapping the laid-out groups (e.g. a horizontal scroller). */
  scrollClassName?: string;
  /** Whether a card is a RECORD — what it was on a day its team has since
   *  moved past. It cannot be dragged: a card that slid across a day that is
   *  over would be a change to today's board made from a picture. The rest
   *  of the same board is live work and drags as usual. */
  isRecord?: (card: CardModel) => boolean;
}

type LocalGroups<Meta> = { key: string; meta: Meta; ids: string[] }[];

/** Wraps one card so dnd-kit can sort it; keeps the body as the drag handle. */
function SortableCard({
  id,
  groupable,
  nested,
  frozen,
  children,
}: {
  id: string;
  groupable: boolean;
  /** The Notion-style tuck-in preview: the placeholder slides to the subtask
   *  indent while the drop would group it under the card above. */
  nested: boolean;
  /** A record of a day its team has moved past: it does not move. */
  frozen: boolean;
  children: ReactNode;
}) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } =
    useSortable({ id, disabled: frozen });
  // A second droppable on the same node is the grouping target: the collision
  // detector retargets to it while a drag hovers this card's middle band, so
  // grouping needs no reorder preview and nothing shifts under the pointer.
  const { setNodeRef: setGroupRef } = useDroppable({
    id: `grp:${id}`,
    disabled: !groupable,
  });
  // The dragged item stays put as a dashed gap (the DragOverlay shows the lift);
  // only the OTHER items get the sortable shift transform so they push apart.
  // dnd-kit's inline transition would override the stylesheet's margin-left
  // one (the tuck-in slide), so the two are merged here.
  const style = {
    transform: isDragging ? undefined : CSS.Translate.toString(transform),
    transition: transition
      ? `${transition}, margin-left 120ms ease`
      : undefined,
  };
  return (
    <div
      ref={(node) => {
        setNodeRef(node);
        setGroupRef(node);
      }}
      style={style}
      className={`sortable-card${isDragging ? " sortable-card-placeholder" : ""}${
        nested ? " sortable-card-nested" : ""
      }`}
      {...attributes}
      {...listeners}
    >
      {children}
    </div>
  );
}

/** DroppableGroup makes a whole group region a drop target keyed by group.key,
 *  so cards can be dropped onto empty groups (where there is no card to hover). */
function DroppableGroup<Meta>({
  group,
  ids,
  activeId,
  nestedId,
  cardById,
  groupable,
  renderGroup,
  renderCard,
  isRecord,
}: {
  group: BoardGroup<Meta>;
  ids: string[];
  activeId: string | null;
  /** The dnd id whose placeholder previews at the subtask indent (tuck-in). */
  nestedId: string | null;
  cardById: Map<string, CardModel>;
  groupable: boolean;
  renderGroup: SortableBoardProps<Meta>["renderGroup"];
  renderCard: (card: CardModel, group: BoardGroup<Meta>) => ReactNode;
  isRecord?: (card: CardModel) => boolean;
}) {
  const { setNodeRef, isOver } = useDroppable({ id: group.key });
  const body = (
    <SortableContext items={ids} strategy={verticalListSortingStrategy}>
      {ids.map((id) => {
        const c = cardById.get(id);
        if (!c) {
          return null;
        }
        return (
          <SortableCard
            key={id}
            id={id}
            groupable={groupable}
            nested={id === nestedId}
            // A record does not move: a drag that cannot land is worse than
            // no drag at all — the card follows the cursor and springs back.
            frozen={isRecord?.(c) ?? false}
          >
            {renderCard(c, group)}
          </SortableCard>
        );
      })}
    </SortableContext>
  );
  const hovering = isOver || (activeId !== null && ids.includes(activeId));
  return (
    <>
      {renderGroup(group, body, {
        isOver: hovering,
        dropRef: setNodeRef as Ref<HTMLElement>,
      })}
    </>
  );
}

/**
 * SortableBoard implements the dnd-kit multiple-containers pattern shared by the
 * Me and Team views. Groups are sortable containers; while dragging, the active
 * card is moved between containers in a LOCAL working copy so the gap/preview
 * updates live (within a group and across groups, including into empty ones).
 */
export function SortableBoard<Meta>({
  groups,
  renderGroup,
  renderCard,
  renderOverlay,
  renderLayout,
  onDrop,
  externalCards,
  onExternalDrop,
  onGroupDrop,
  onHoverCard,
  onDragActiveCard,
  canGroup,
  canMoveTo,
  scrollClassName,
  isRecord,
}: SortableBoardProps<Meta>) {
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
  );

  const isExternal = (id: string) => externalCards?.has(id) ?? false;

  // While dragging: a local override of the grouped ids that the over-handler
  // mutates so cards visibly push apart. null when idle (groups are the truth).
  const [local, setLocal] = useState<LocalGroups<Meta> | null>(null);
  const [activeId, setActiveId] = useState<string | null>(null);
  // While a drag hovers a card's middle band: the dnd id of that card. The
  // active placeholder previews right below it at the subtask indent.
  const [nestUnder, setNestUnder] = useState<string | null>(null);
  // The Todoist-style indent level of the drag: held to the right = "nest
  // under the card above", flush left = "standalone". Guarded against
  // accidental wobble by hysteresis (enter past the indent, leave well before
  // it) plus a short persistence window before the level flips.
  const [indentOn, setIndentOn] = useState(false);
  const indentOnRef = useRef(false);
  const indentFlipAt = useRef<number | null>(null);
  // The two STRONG horizontal gestures, far past the ordinary indent band:
  // dragging well LEFT of the rows is the only way a subtask leaves its
  // group, and dragging well RIGHT of a card is the only way to open a NEW
  // group (a collapsed or childless parent). Distance is the guard.
  const strongDedent = useRef(false);
  const strongNest = useRef(false);
  // The middle-band tuck-in needs a short dwell before it arms, so sweeping
  // a card across the board does not flip into grouping mode in passing.
  const bandTarget = useRef<string | null>(null);
  const bandSince = useRef(0);
  // Indent-based nesting arms only after the drag has PARKED on its current
  // slot for a moment — a fast diagonal drop can carry a stray indent, and it
  // must not adopt a parent in passing. Reordering inside the card's own
  // block never waits.
  const slotArmed = useRef(false);
  const slotTimer = useRef<number | null>(null);
  // Whether the drag currently hovers a card ROW (not a container's empty
  // area): adopting a parent is only offered within a card's height of the
  // rows themselves — dropping into the space below a list stays main-level.
  const overRow = useRef(false);
  const [, bumpRender] = useState(0);
  const disarmSlot = () => {
    if (slotTimer.current !== null) {
      window.clearTimeout(slotTimer.current);
      slotTimer.current = null;
    }
    slotArmed.current = false;
  };
  const armSlotSoon = () => {
    disarmSlot();
    slotTimer.current = window.setTimeout(() => {
      slotArmed.current = true;
      bumpRender((x) => x + 1); // the placeholder indent preview appears now
    }, 300);
  };

  // The groups actually rendered: local working copy while dragging, else props.
  const view: LocalGroups<Meta> = useMemo(() => {
    if (local) {
      return local;
    }
    return groups.map((g) => ({
      key: g.key,
      meta: g.meta,
      ids: g.cards.map((c) => c.itemId),
    }));
  }, [local, groups]);

  // Mid-drag board changes — an auto-expanded parent's subtasks appearing, a
  // watch update — reconcile INTO the working copy: new ids slot in after
  // their nearest fresh predecessor, vanished ids drop out, and the active
  // placeholder keeps its previewed position.
  useEffect(() => {
    setLocal((cur) => {
      if (!cur) {
        return cur;
      }
      const fresh = groups.map((g) => ({
        key: g.key,
        ids: g.cards.map((c) => c.itemId),
      }));
      const freshAll = new Set(fresh.flatMap((g) => g.ids));
      const next = cur.map((g) => ({ ...g, ids: [...g.ids] }));
      let changed = false;
      for (const g of next) {
        const kept = g.ids.filter((id) => freshAll.has(id) || id === activeId);
        if (kept.length !== g.ids.length) {
          g.ids = kept;
          changed = true;
        }
      }
      const has = (id: string) => next.some((g) => g.ids.includes(id));
      for (const f of fresh) {
        const target = next.find((g) => g.key === f.key);
        if (!target) {
          continue;
        }
        f.ids.forEach((id, i) => {
          if (has(id)) {
            return;
          }
          let at = -1;
          for (let j = i - 1; j >= 0; j--) {
            const k = target.ids.indexOf(f.ids[j]);
            if (k !== -1) {
              at = k;
              break;
            }
          }
          target.ids.splice(at + 1, 0, id);
          changed = true;
        });
      }
      return changed ? next : cur;
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [groups]);

  // Fast id → card lookup across every group (active card may move containers).
  const cardById = useMemo(() => {
    const m = new Map<string, CardModel>();
    for (const g of groups) {
      for (const c of g.cards) {
        m.set(c.itemId, c);
      }
    }
    return m;
  }, [groups]);

  const findGroupIndex = (work: LocalGroups<Meta>, id: string): number =>
    work.findIndex((g) => g.ids.includes(id) || g.key === id);

  // The card the active placeholder would nest under, resolved from its slot
  // in the working copy and the held indent level — Todoist-style:
  //  - a middle-band hover (nestUnder) always nests under that card;
  //  - a slot strictly INSIDE a block (more subtasks follow below) is
  //    unambiguous: it nests regardless of indent — a standalone card cannot
  //    sit between a parent and its subtasks, so no flush preview there;
  //  - below the LAST subtask of a block, or below any plain card, the indent
  //    decides — held to the right nests, flush left stays standalone.
  // The drop commits exactly this resolution, so the preview never lies.
  const nestPreview = (
    work: LocalGroups<Meta> | null,
  ): { id: string; positional: boolean } | null => {
    if (!activeId || !work) {
      return null;
    }
    if (nestUnder !== null) {
      const t = cardById.get(nestUnder)?.itemId;
      return t ? { id: t, positional: false } : null;
    }
    const entry = work.find((g) => g.ids.includes(activeId));
    if (!entry) {
      return null;
    }
    const idx = entry.ids.indexOf(activeId);
    const above = idx > 0 ? cardById.get(entry.ids[idx - 1]) : undefined;
    const below =
      idx >= 0 && idx + 1 < entry.ids.length
        ? cardById.get(entry.ids[idx + 1])
        : undefined;
    const active = cardById.get(activeId);
    if (!above || !active) {
      return null;
    }
    // The exit gesture: dragged FAR left of the rows and parked there.
    const leaving = strongDedent.current && slotArmed.current;
    let target: CardModel | undefined;
    let positional = false;
    if (below?.parent) {
      // Strictly inside a block: subtasks continue below, so the slot can
      // only be one of them — the dedent gesture exists on the last slot only.
      target = cardById.get(below.parent);
      positional = !indentOn;
    } else if (above.parent) {
      if (above.parent === active.parent) {
        // The own block's last slot: a subtask STAYS a subtask unless it is
        // pulled far left and parked — distance is the exit guard.
        if (!leaving) {
          target = cardById.get(above.parent);
        }
      } else if (indentOn && slotArmed.current && overRow.current) {
        // A foreign block's last slot: joining takes a parked indent, held
        // over the rows themselves.
        target = cardById.get(above.parent);
      }
    } else if (above.itemId === active.parent) {
      // Right under the own parent (the card is its only visible subtask):
      // grouped unless pulled far left and parked, like the block's last slot.
      if (!leaving) {
        target = above;
      }
    } else if (strongNest.current && slotArmed.current && overRow.current) {
      // A NEW parent (a collapsed or childless card) opens only for the
      // far-right gesture held in place over the rows — the first subtask is
      // a deliberate act, not a passing indent.
      target = above;
    }
    // A dragged subtask never ungroups without the exit gesture: wherever
    // the slot slid, it snaps back to its own parent.
    if (!target && active.parent && !leaving) {
      target = cardById.get(active.parent);
    }
    if (!target || target.itemId === active.itemId) {
      return null;
    }
    if (active.parent === target.itemId) {
      return { id: target.itemId, positional }; // staying among its siblings
    }
    if (canGroup && !canGroup(active, target)) {
      return null;
    }
    return { id: target.itemId, positional };
  };

  // closestCorners with a grouping band: while the dragged card's centre is
  // over the vertical middle of another card, the drop retargets to that
  // card's grp: droppable — dropping there groups the card as its subtask.
  // The edges keep the normal sorting behaviour. grp: droppables share their
  // card's rect, so they are filtered out of the base pass.
  const detectCollisions: CollisionDetection = (args) => {
    const activeKey = String(args.active.id);
    const base = closestCorners(args).filter(
      (c) => !String(c.id).startsWith("grp:"),
    );
    const first = base[0];
    if (!first || !onGroupDrop || isExternal(activeKey)) {
      return base;
    }
    const overKey = String(first.id);
    const target = cardById.get(overKey);
    const active = cardById.get(activeKey);
    if (!target || !active || target.itemId === active.itemId) {
      return base;
    }
    if (canGroup && !canGroup(active, target)) {
      return base;
    }
    const rect = args.droppableRects.get(first.id);
    const centerY = args.collisionRect.top + args.collisionRect.height / 2;
    if (
      rect &&
      centerY > rect.top + rect.height * 0.3 &&
      centerY < rect.top + rect.height * 0.7
    ) {
      // Arm the tuck-in only after a ~300ms dwell on the SAME card's middle:
      // by default a drag passing over cards keeps the main level.
      const now = performance.now();
      if (bandTarget.current !== overKey) {
        bandTarget.current = overKey;
        bandSince.current = now;
        return base;
      }
      if (now - bandSince.current < 300) {
        return base;
      }
      return [{ id: `grp:${overKey}` }];
    }
    bandTarget.current = null;
    return base;
  };

  const handleStart = (e: DragStartEvent) => {
    const id = String(e.active.id);
    setActiveId(id);
    onDragActiveCard?.(cardById.get(id)?.itemId ?? null);
    const seed = !!cardById.get(id)?.parent;
    indentOnRef.current = seed;
    indentFlipAt.current = null;
    strongDedent.current = false;
    strongNest.current = false;
    bandTarget.current = null;
    armSlotSoon();
    setIndentOn(seed);
    if (isExternal(id)) {
      return; // external draggables don't reshuffle the grid's working copy
    }
    setLocal(
      groups.map((g) => ({
        key: g.key,
        meta: g.meta,
        ids: g.cards.map((c) => c.itemId),
      })),
    );
  };

  const handleMove = (e: DragMoveEvent) => {
    const translated = e.active.rect.current.translated;
    const overRect = e.over?.rect;
    if (!translated || !overRect) {
      return;
    }
    const offset = translated.left - overRect.left;
    // The strong gestures flip on distance alone (with their own hysteresis):
    // far left (-16px past the row edge) is the leave-the-group gesture, far
    // right (+56px) is the open-a-new-group gesture.
    const dedent = strongDedent.current ? offset < -8 : offset < -16;
    const nest = strongNest.current ? offset > 40 : offset > 56;
    if (dedent !== strongDedent.current || nest !== strongNest.current) {
      strongDedent.current = dedent;
      strongNest.current = nest;
      bumpRender((x) => x + 1);
    }
    const cur = indentOnRef.current;
    // Hysteresis: enter the nest level past the indent (26px), leave it only
    // well before (12px); the band in between is sticky.
    const want = cur ? offset > 12 : offset > 26;
    if (want === cur) {
      indentFlipAt.current = null;
      return;
    }
    // The crossing must persist ~80ms before the level flips, so a wobble on
    // the way down never re-nests (or un-nests) a card by accident.
    const now = performance.now();
    if (indentFlipAt.current === null) {
      indentFlipAt.current = now;
      return;
    }
    if (now - indentFlipAt.current < 80) {
      return;
    }
    indentFlipAt.current = null;
    indentOnRef.current = want;
    setIndentOn(want);
  };

  const handleOver = (e: DragOverEvent) => {
    const { active, over } = e;
    const overRaw = over ? String(over.id) : null;
    overRow.current =
      overRaw !== null &&
      (overRaw.startsWith("grp:") || cardById.has(overRaw));
    // The slot changed: indent-based nesting re-arms after a fresh pause.
    armSlotSoon();
    // Report the card the drag would group under so the board highlights it.
    if (onHoverCard) {
      if (overRaw?.startsWith("grp:")) {
        onHoverCard(cardById.get(overRaw.slice(4))?.itemId ?? null);
      } else {
        onHoverCard(null);
      }
    }
    const activeKey = String(active.id);
    if (!over || isExternal(activeKey)) {
      return;
    }
    const overKey = String(over.id);
    // The grouping band: preview the active placeholder right below the
    // target at the subtask indent (the Notion-style tuck-in), instead of the
    // usual reshuffle.
    if (overKey.startsWith("grp:")) {
      const targetId = overKey.slice(4);
      setNestUnder(targetId);
      setLocal((cur) => {
        if (!cur) {
          return cur;
        }
        const from = findGroupIndex(cur, activeKey);
        const to = findGroupIndex(cur, targetId);
        if (from === -1 || to === -1) {
          return cur;
        }
        const next = cur.map((g) => ({ ...g, ids: [...g.ids] }));
        next[from].ids = next[from].ids.filter((x) => x !== activeKey);
        // The card lands at the END of the target's list, so the placeholder
        // previews after its last visible subtask, not squeezed in first.
        const targetItem = cardById.get(targetId)?.itemId ?? "";
        let at = next[to].ids.indexOf(targetId) + 1;
        while (at < next[to].ids.length) {
          const c = cardById.get(next[to].ids[at]);
          if (!c || c.parent !== targetItem) {
            break;
          }
          at++;
        }
        next[to].ids.splice(at, 0, activeKey);
        return next;
      });
      return;
    }
    setNestUnder(null);
    setLocal((cur) => {
      if (!cur) {
        return cur;
      }
      const from = findGroupIndex(cur, activeKey);
      const to = findGroupIndex(cur, overKey);
      if (from === -1 || to === -1) {
        return cur;
      }
      // Within one container: reorder the working copy live, so the
      // placeholder physically sits at the previewed slot (its subtask
      // indent/guide line and the drop resolution then read the true
      // neighbours, not the pre-drag ones).
      if (from === to) {
        const ids = cur[from].ids;
        const oldIndex = ids.indexOf(activeKey);
        const newIndex = ids.includes(overKey)
          ? ids.indexOf(overKey)
          : ids.length - 1;
        if (oldIndex === newIndex || oldIndex === -1 || newIndex === -1) {
          return cur;
        }
        return cur.map((g, i) =>
          i === from ? { ...g, ids: arrayMove(ids, oldIndex, newIndex) } : g,
        );
      }
      // Crossing into another group: only where the board would take it.
      // Previewing a move that the drop then refuses opens a gap the card
      // springs back out of, which reads as the board losing the drag.
      const card = cardById.get(activeKey);
      if (canMoveTo && card && !canMoveTo(card, cur[from].meta, cur[to].meta)) {
        return cur;
      }
      // Move the active card out of its group into the target group, at the
      // index of the card it is hovering (or the end when over the container).
      const next = cur.map((g) => ({ ...g, ids: [...g.ids] }));
      next[from].ids = next[from].ids.filter((x) => x !== activeKey);
      const overIsCard = next[to].ids.includes(overKey);
      const insertAt = overIsCard
        ? next[to].ids.indexOf(overKey)
        : next[to].ids.length;
      next[to].ids.splice(insertAt, 0, activeKey);
      return next;
    });
  };

  const reset = () => {
    setLocal(null);
    setActiveId(null);
    setNestUnder(null);
    indentOnRef.current = false;
    indentFlipAt.current = null;
    strongDedent.current = false;
    strongNest.current = false;
    bandTarget.current = null;
    overRow.current = false;
    disarmSlot();
    setIndentOn(false);
    onHoverCard?.(null);
    onDragActiveCard?.(null);
  };

  const handleEnd = (e: DragEndEvent) => {
    const activeKey = String(e.active.id);
    const overRaw = e.over ? String(e.over.id) : null;
    // A drop on a card's middle band groups the dragged card under it.
    if (overRaw?.startsWith("grp:") && !isExternal(activeKey)) {
      const card = cardById.get(activeKey);
      const target = cardById.get(overRaw.slice(4));
      if (card && target && card.itemId !== target.itemId) {
        onGroupDrop?.(card, target.itemId);
      }
      reset();
      return;
    }
    if (isExternal(activeKey)) {
      const ext = externalCards?.get(activeKey);
      if (ext) {
        onExternalDrop?.(ext, e.over ? String(e.over.id) : null);
      }
      reset();
      return;
    }
    const work = local;
    const card = cardById.get(activeKey);
    if (!work || !card) {
      reset();
      return;
    }
    const over = e.over;
    let next = work;
    if (over) {
      const overKey = String(over.id);
      const from = findGroupIndex(work, activeKey);
      const to = findGroupIndex(work, overKey);
      if (from !== -1 && to !== -1) {
        if (from === to) {
          // Reorder within the same container.
          const ids = work[from].ids;
          const oldIndex = ids.indexOf(activeKey);
          const newIndex = ids.includes(overKey)
            ? ids.indexOf(overKey)
            : ids.length - 1;
          if (oldIndex !== newIndex && oldIndex !== -1 && newIndex !== -1) {
            next = work.map((g, i) =>
              i === from ? { ...g, ids: arrayMove(ids, oldIndex, newIndex) } : g,
            );
          }
        }
        // Cross-group moves were already applied live in handleOver.
      }
    }

    // Find the original group (from props) to report the source meta.
    const fromGroup = groups.find((g) =>
      g.cards.some((c) => c.itemId === activeKey),
    );
    const toEntry = next.find((g) => g.ids.includes(activeKey));
    if (!fromGroup || !toEntry) {
      reset();
      return;
    }

    const nest = nestPreview(next);
    onDrop({
      card,
      fromMeta: fromGroup.meta,
      toMeta: toEntry.meta,
      groups: next.map((g) => ({ meta: g.meta, ids: g.ids })),
      groupUnder: nest?.id ?? null,
    });
    reset();
  };

  const activeCard = activeId
    ? cardById.get(activeId) ?? externalCards?.get(activeId) ?? null
    : null;

  // The placeholder previews at the indent exactly when the drop would nest.
  const nestPreviewId =
    activeId !== null && nestPreview(local) !== null ? activeId : null;


  // Render each group as a node, keyed by group.key, so a custom layout can
  // arrange them (Team groups cells into engineer columns); default is flat.
  const nodes = new Map<string, ReactNode>();
  for (const entry of view) {
    const group = groups.find((g) => g.key === entry.key);
    if (!group) {
      continue;
    }
    nodes.set(
      entry.key,
      <DroppableGroup
        key={entry.key}
        group={group}
        ids={entry.ids}
        activeId={activeId}
        nestedId={nestPreviewId}
        cardById={cardById}
        groupable={!!onGroupDrop}
        renderGroup={renderGroup}
        renderCard={renderCard}
        isRecord={isRecord}
      />,
    );
  }

  return (
    <DndContext
      sensors={sensors}
      collisionDetection={detectCollisions}
      onDragStart={handleStart}
      onDragMove={handleMove}
      onDragOver={handleOver}
      onDragEnd={handleEnd}
      onDragCancel={reset}
    >
      {scrollClassName ? (
        <div className={scrollClassName}>
          {renderLayout
            ? renderLayout(nodes, groups)
            : groups.map((g) => nodes.get(g.key))}
        </div>
      ) : renderLayout ? (
        renderLayout(nodes, groups)
      ) : (
        groups.map((g) => nodes.get(g.key))
      )}
      {/* No drop animation: the card is already redrawn where it landed, so
          the default "fly back to source" reads as the drop being undone. */}
      <DragOverlay dropAnimation={null}>
        {activeCard ? (
          <div className="dnd-overlay">{renderOverlay(activeCard)}</div>
        ) : null}
      </DragOverlay>
    </DndContext>
  );
}
