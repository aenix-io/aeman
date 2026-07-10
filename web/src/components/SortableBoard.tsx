import { useMemo, useState, type ReactNode, type Ref } from "react";
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

/** Where a dragged subtask row (id "subrow:<itemId>") landed. */
export type SubtaskDropTarget<Meta> =
  | { kind: "parent"; parentId: string }
  | { kind: "before"; beforeId: string }
  | { kind: "group"; meta: Meta };

/** Result of a committed drop, handed to the parent to persist. */
export interface DropResult<Meta> {
  card: CardModel;
  fromMeta: Meta;
  toMeta: Meta;
  /** Final per-group ordering of card ids across the whole (visible) board. */
  groups: { meta: Meta; ids: string[] }[];
}

interface SortableBoardProps<Meta> {
  /** Source-of-truth groups derived from board state. */
  groups: BoardGroup<Meta>[];
  /** The dnd id for a card within a group. Defaults to the card's itemId; a group
   *  can namespace it (e.g. "plan:<id>") so the same card may live in two groups
   *  at once without an id clash. */
  idForCard?: (card: CardModel, group: BoardGroup<Meta>) => string;
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
  /** Children rendered inside the DndContext (e.g. a weekly drop area). */
  children?: ReactNode;
  /** Cards draggable from `extra`, keyed by their dnd id, for overlay + drop. */
  externalCards?: Map<string, CardModel>;
  /** Commit a drop of an external (extra) draggable onto an over id. */
  onExternalDrop?: (card: CardModel, overId: string | null) => void;
  /** Commit a drop that groups the dragged card under another card — either
   *  onto its middle band (over id "grp:<dnd id>") or into an expanded subtask
   *  area (over id "sub:<parentId>"). */
  onGroupDrop?: (card: CardModel, parentId: string) => void;
  /** Fires with the card id the drag would group under (its middle band is
   *  hovered), null when the drag leaves it — the board highlights the target. */
  onHoverCard?: (cardId: string | null) => void;
  /** Whether the active card may group under the target (e.g. depth limits). */
  canGroup?: (active: CardModel, target: CardModel) => boolean;
  /** Cards rendered as subtask rows (itemId → card), draggable as
   *  "subrow:<itemId>" from inside their parent's list. */
  subtaskCards?: Map<string, CardModel>;
  /** Commit a subtask-row drop: regroup under a card, reorder before a
   *  sibling, or pull the card out into a group. */
  onSubtaskDrop?: (card: CardModel, target: SubtaskDropTarget<Meta>) => void;
  /** Optional class wrapping the laid-out groups (e.g. a horizontal scroller). */
  scrollClassName?: string;
}

type LocalGroups<Meta> = { key: string; meta: Meta; ids: string[] }[];

/** Wraps one card so dnd-kit can sort it; keeps the body as the drag handle. */
function SortableCard({
  id,
  groupable,
  children,
}: {
  id: string;
  groupable: boolean;
  children: ReactNode;
}) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } =
    useSortable({ id });
  // A second droppable on the same node is the grouping target: the collision
  // detector retargets to it while a drag hovers this card's middle band, so
  // grouping needs no reshuffle preview and nothing moves under the pointer.
  const { setNodeRef: setGroupRef } = useDroppable({
    id: `grp:${id}`,
    disabled: !groupable,
  });
  // The dragged item stays put as a dashed gap (the DragOverlay shows the lift);
  // only the OTHER items get the sortable shift transform so they push apart.
  const style = {
    transform: isDragging ? undefined : CSS.Translate.toString(transform),
    transition,
  };
  return (
    <div
      ref={(node) => {
        setNodeRef(node);
        setGroupRef(node);
      }}
      style={style}
      className={`sortable-card${isDragging ? " sortable-card-placeholder" : ""}`}
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
  cardById,
  groupable,
  renderGroup,
  renderCard,
}: {
  group: BoardGroup<Meta>;
  ids: string[];
  activeId: string | null;
  cardById: Map<string, CardModel>;
  groupable: boolean;
  renderGroup: SortableBoardProps<Meta>["renderGroup"];
  renderCard: (card: CardModel, group: BoardGroup<Meta>) => ReactNode;
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
          <SortableCard key={id} id={id} groupable={groupable}>
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
  idForCard,
  renderGroup,
  renderCard,
  renderOverlay,
  renderLayout,
  onDrop,
  children,
  externalCards,
  onExternalDrop,
  onGroupDrop,
  onHoverCard,
  canGroup,
  subtaskCards,
  onSubtaskDrop,
  scrollClassName,
}: SortableBoardProps<Meta>) {
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
  );

  const isExternal = (id: string) => externalCards?.has(id) ?? false;
  const idOf = (c: CardModel, g: BoardGroup<Meta>) =>
    idForCard ? idForCard(c, g) : c.itemId;

  // While dragging: a local override of the grouped ids that the over-handler
  // mutates so cards visibly push apart. null when idle (groups are the truth).
  const [local, setLocal] = useState<LocalGroups<Meta> | null>(null);
  const [activeId, setActiveId] = useState<string | null>(null);

  // The groups actually rendered: local working copy while dragging, else props.
  const view: LocalGroups<Meta> = useMemo(() => {
    if (local) {
      return local;
    }
    return groups.map((g) => ({
      key: g.key,
      meta: g.meta,
      ids: g.cards.map((c) => idOf(c, g)),
    }));
  }, [local, groups]);

  // Fast id → card lookup across every group (active card may move containers).
  const cardById = useMemo(() => {
    const m = new Map<string, CardModel>();
    for (const g of groups) {
      for (const c of g.cards) {
        m.set(idOf(c, g), c);
      }
    }
    return m;
  }, [groups]);

  const findGroupIndex = (work: LocalGroups<Meta>, id: string): number =>
    work.findIndex((g) => g.ids.includes(id) || g.key === id);

  // The card behind any active/over dnd id, whichever registry it lives in.
  const lookupCard = (id: string): CardModel | undefined =>
    id.startsWith("subrow:")
      ? subtaskCards?.get(id.slice(7))
      : cardById.get(id) ?? externalCards?.get(id);

  // closestCorners with a grouping band: while the dragged card's centre is
  // over the vertical middle of another card, the drop retargets to that
  // card's grp: droppable — the card highlights as a group target and no
  // reorder preview runs, so nothing shifts under the pointer. The edges keep
  // the normal sorting behaviour. grp:/subrow: droppables share their row's
  // rect, so they are filtered out of the base pass to avoid duplicates.
  const detectCollisions: CollisionDetection = (args) => {
    const activeKey = String(args.active.id);
    const subDrag = activeKey.startsWith("subrow:");
    const base = closestCorners(args).filter((c) => {
      const id = String(c.id);
      if (id.startsWith("grp:")) {
        return false;
      }
      if (id.startsWith("subrow:")) {
        return subDrag; // sibling reorder targets, only for subtask drags
      }
      return true;
    });
    const first = base[0];
    if (!first || !onGroupDrop || isExternal(activeKey)) {
      return base;
    }
    const overKey = String(first.id);
    const target = cardById.get(overKey);
    const active = lookupCard(activeKey);
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
      return [{ id: `grp:${overKey}` }];
    }
    return base;
  };

  const handleStart = (e: DragStartEvent) => {
    const id = String(e.active.id);
    setActiveId(id);
    if (isExternal(id) || id.startsWith("subrow:")) {
      return; // external/subtask draggables don't reshuffle the working copy
    }
    setLocal(
      groups.map((g) => ({
        key: g.key,
        meta: g.meta,
        ids: g.cards.map((c) => idOf(c, g)),
      })),
    );
  };

  const handleOver = (e: DragOverEvent) => {
    const { active, over } = e;
    const overRaw = over ? String(over.id) : null;
    // Report the card the drag would group under (its middle band or its
    // expanded subtask area is hovered) so the board highlights the target.
    if (onHoverCard) {
      if (overRaw?.startsWith("grp:")) {
        onHoverCard(cardById.get(overRaw.slice(4))?.itemId ?? null);
      } else if (overRaw?.startsWith("sub:")) {
        onHoverCard(overRaw.slice(4));
      } else {
        onHoverCard(null);
      }
    }
    const activeKey = String(active.id);
    if (!over || isExternal(activeKey) || activeKey.startsWith("subrow:")) {
      return;
    }
    const overKey = String(over.id);
    if (overKey.startsWith("sub:") || overKey.startsWith("grp:") || overKey.startsWith("subrow:")) {
      return; // grouping/sibling target: no reshuffle preview
    }
    setLocal((cur) => {
      if (!cur) {
        return cur;
      }
      const from = findGroupIndex(cur, activeKey);
      const to = findGroupIndex(cur, overKey);
      if (from === -1 || to === -1 || from === to) {
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
    onHoverCard?.(null);
  };

  const handleEnd = (e: DragEndEvent) => {
    const activeKey = String(e.active.id);
    const overRaw = e.over ? String(e.over.id) : null;
    // A dragged subtask row: regroup, reorder before a sibling, or pull out.
    if (activeKey.startsWith("subrow:")) {
      const card = subtaskCards?.get(activeKey.slice(7));
      if (card && overRaw && onSubtaskDrop) {
        if (overRaw.startsWith("grp:")) {
          const t = cardById.get(overRaw.slice(4));
          if (t && t.itemId !== card.itemId) {
            onSubtaskDrop(card, { kind: "parent", parentId: t.itemId });
          }
        } else if (overRaw.startsWith("sub:")) {
          onSubtaskDrop(card, { kind: "parent", parentId: overRaw.slice(4) });
        } else if (overRaw.startsWith("subrow:")) {
          const beforeId = overRaw.slice(7);
          if (beforeId !== card.itemId) {
            onSubtaskDrop(card, { kind: "before", beforeId });
          }
        } else {
          const idx = findGroupIndex(view, overRaw);
          if (idx !== -1) {
            onSubtaskDrop(card, { kind: "group", meta: view[idx].meta });
          }
        }
      }
      reset();
      return;
    }
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
    if (overRaw?.startsWith("sub:") && !isExternal(activeKey)) {
      const card = cardById.get(activeKey);
      const parentId = overRaw.slice(4);
      if (card && card.itemId !== parentId) {
        onGroupDrop?.(card, parentId);
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
      g.cards.some((c) => idOf(c, g) === activeKey),
    );
    const toEntry = next.find((g) => g.ids.includes(activeKey));
    if (!fromGroup || !toEntry) {
      reset();
      return;
    }

    onDrop({
      card,
      fromMeta: fromGroup.meta,
      toMeta: toEntry.meta,
      groups: next.map((g) => ({ meta: g.meta, ids: g.ids })),
    });
    reset();
  };

  const activeCard = activeId ? lookupCard(activeId) ?? null : null;

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
        cardById={cardById}
        groupable={!!onGroupDrop}
        renderGroup={renderGroup}
        renderCard={renderCard}
      />,
    );
  }

  return (
    <DndContext
      sensors={sensors}
      collisionDetection={detectCollisions}
      onDragStart={handleStart}
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
      {children}
      {/* No drop animation: a plan card stays in its band, so the default
          "fly back to source" looks like the card returning after a drop. */}
      <DragOverlay dropAnimation={null}>
        {activeCard ? (
          <div className="dnd-overlay">{renderOverlay(activeCard)}</div>
        ) : null}
      </DragOverlay>
    </DndContext>
  );
}
