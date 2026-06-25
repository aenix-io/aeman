import { useMemo, useState, type ReactNode, type Ref } from "react";
import {
  DndContext,
  DragOverlay,
  PointerSensor,
  closestCorners,
  useDroppable,
  useSensor,
  useSensors,
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
  /** Renders the container shell + an inner list region for a group.
   *  dropRef MUST be attached to the element that should accept drops (so empty
   *  groups still receive the card); isOver is true while the active card hovers. */
  renderGroup: (
    group: BoardGroup<Meta>,
    body: ReactNode,
    state: { isOver: boolean; dropRef: Ref<HTMLElement> },
  ) => ReactNode;
  /** Renders a single card (without sortable wiring — that is added here). */
  renderCard: (card: CardModel) => ReactNode;
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
  /** Optional class wrapping the laid-out groups (e.g. a horizontal scroller). */
  scrollClassName?: string;
}

type LocalGroups<Meta> = { key: string; meta: Meta; ids: string[] }[];

/** Wraps one card so dnd-kit can sort it; keeps the body as the drag handle. */
function SortableCard({
  id,
  children,
}: {
  id: string;
  children: ReactNode;
}) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } =
    useSortable({ id });
  // The dragged item stays put as a dashed gap (the DragOverlay shows the lift);
  // only the OTHER items get the sortable shift transform so they push apart.
  const style = {
    transform: isDragging ? undefined : CSS.Translate.toString(transform),
    transition,
  };
  return (
    <div
      ref={setNodeRef}
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
  renderGroup,
  renderCard,
}: {
  group: BoardGroup<Meta>;
  ids: string[];
  activeId: string | null;
  cardById: Map<string, CardModel>;
  renderGroup: SortableBoardProps<Meta>["renderGroup"];
  renderCard: (card: CardModel) => ReactNode;
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
          <SortableCard key={id} id={id}>
            {renderCard(c)}
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
  children,
  externalCards,
  onExternalDrop,
  scrollClassName,
}: SortableBoardProps<Meta>) {
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
  );

  const isExternal = (id: string) => externalCards?.has(id) ?? false;

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
      ids: g.cards.map((c) => c.itemId),
    }));
  }, [local, groups]);

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

  const handleStart = (e: DragStartEvent) => {
    const id = String(e.active.id);
    setActiveId(id);
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

  const handleOver = (e: DragOverEvent) => {
    const { active, over } = e;
    if (!over || isExternal(String(active.id))) {
      return;
    }
    const activeKey = String(active.id);
    const overKey = String(over.id);
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
  };

  const handleEnd = (e: DragEndEvent) => {
    const activeKey = String(e.active.id);
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

    onDrop({
      card,
      fromMeta: fromGroup.meta,
      toMeta: toEntry.meta,
      groups: next.map((g) => ({ meta: g.meta, ids: g.ids })),
    });
    reset();
  };

  const activeCard = activeId
    ? cardById.get(activeId) ?? externalCards?.get(activeId) ?? null
    : null;

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
        renderGroup={renderGroup}
        renderCard={renderCard}
      />,
    );
  }

  return (
    <DndContext
      sensors={sensors}
      collisionDetection={closestCorners}
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
      <DragOverlay>
        {activeCard ? (
          <div className="dnd-overlay">{renderOverlay(activeCard)}</div>
        ) : null}
      </DragOverlay>
    </DndContext>
  );
}
