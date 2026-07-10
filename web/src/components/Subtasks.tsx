import { type ReactNode } from "react";
import { useDroppable } from "@dnd-kit/core";
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
}

/**
 * Subtasks is the list under an expanded card: its subtask rows (full cards,
 * indented), an add row, and — while a drag hovers the parent — the drop area
 * that turns the dragged card into a subtask (droppable id "sub:<parentId>").
 */
export function Subtasks({ parent, subs, renderChild, onUngroup, onCreate }: SubtasksProps) {
  const { setNodeRef, isOver } = useDroppable({ id: `sub:${parent.itemId}` });
  return (
    <div
      ref={setNodeRef}
      className={`subtasks${isOver ? " subtasks-over" : ""}`}
    >
      {subs.length === 0 && (
        <div className="subtasks-empty">Drop a card here to group it as a subtask</div>
      )}
      {subs.map((c) => (
        <div className="subtask-row" key={c.itemId}>
          <div className="subtask-card">{renderChild(c)}</div>
          <button
            type="button"
            className="subtask-out"
            onClick={() => onUngroup(c)}
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
      ))}
      <AddCard onCreate={(title) => onCreate(title)} placeholder="Add a subtask…" />
    </div>
  );
}
