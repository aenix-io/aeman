import { type ReactNode } from "react";
import { useDraggable, useDroppable } from "@dnd-kit/core";
import type { Card as CardModel } from "../providers/types";
import { AddCard } from "./AddCard";

interface SubtasksProps {
  parent: CardModel;
  /** The parent's subtasks, already filtered by the board (team focus etc.). */
  subs: CardModel[];
  /** Renders one subtask row (the board supplies its full Card wiring). */
  renderChild: (card: CardModel) => ReactNode;
  /** Pull a subtask back out as a standalone card. */
  onUngroup: (card: CardModel) => void;
  /** Create a new subtask under this parent. */
  onCreate: (title: string) => void;
  /** Open with the add form already expanded (the card's + button). */
  adding?: boolean;
  /** The add form closed; created reports whether a subtask was submitted. */
  onAddingDone?: (created: boolean) => void;
}

/** One subtask row: draggable on its own (id "subrow:<itemId>") so lifting a
 * subtask never lifts the parent, and a droppable so siblings reorder. */
function SubtaskRow({
  card,
  onUngroup,
  children,
}: {
  card: CardModel;
  onUngroup: (card: CardModel) => void;
  children: ReactNode;
}) {
  const {
    attributes,
    listeners,
    setNodeRef: setDragRef,
    isDragging,
  } = useDraggable({ id: `subrow:${card.itemId}` });
  const { setNodeRef: setDropRef, isOver } = useDroppable({
    id: `subrow:${card.itemId}`,
  });
  return (
    <div
      ref={(node) => {
        setDragRef(node);
        setDropRef(node);
      }}
      className={`subtask-row${isDragging ? " subtask-row-dragging" : ""}${
        isOver && !isDragging ? " subtask-row-over" : ""
      }`}
      {...attributes}
      {...listeners}
    >
      <div className="subtask-card">{children}</div>
      <button
        type="button"
        className="subtask-out"
        onClick={() => onUngroup(card)}
        aria-label="Ungroup the subtask"
        title="Pull back out as a standalone card"
      >
        <svg
          width="11"
          height="11"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2.2"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
        >
          <path d="M7 17 17 7" />
          <path d="M9 7h8v8" />
        </svg>
      </button>
    </div>
  );
}

/**
 * Subtasks is the list under an expanded card: its subtask rows (full cards,
 * indented) and an add row. The whole area is also a drop target that groups
 * a dragged card (droppable id "sub:<parentId>").
 */
export function Subtasks({
  parent,
  subs,
  renderChild,
  onUngroup,
  onCreate,
  adding,
  onAddingDone,
}: SubtasksProps) {
  const { setNodeRef, isOver } = useDroppable({ id: `sub:${parent.itemId}` });
  return (
    <div
      ref={setNodeRef}
      className={`subtasks${isOver ? " subtasks-over" : ""}`}
      // Rows drag and the add form types on their own: never bubble pointer
      // presses up into the parent card's sortable listeners.
      onPointerDown={(e) => e.stopPropagation()}
    >
      {subs.map((c) => (
        <SubtaskRow key={c.itemId} card={c} onUngroup={onUngroup}>
          {renderChild(c)}
        </SubtaskRow>
      ))}
      <AddCard
        key={adding ? "adding" : "idle"}
        autoOpen={adding}
        onClosed={onAddingDone}
        onCreate={(title) => onCreate(title)}
        placeholder="Add a subtask…"
      />
    </div>
  );
}
