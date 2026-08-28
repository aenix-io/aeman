import { useState } from "react";
import { teamColor } from "../avatar";
import { declareDomain, type DomainInfo } from "../domains";
import { nameConflict, type RosterKind } from "../names";
import { DomainSelect } from "./DomainSelect";
import {
  DndContext,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import {
  SortableContext,
  useSortable,
  verticalListSortingStrategy,
  arrayMove,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";

interface TeamsModalProps {
  teams: string[];
  /** Dialog heading; defaults to the team roster's. */
  title?: string;
  /** What one row is, used in the add field. The Project board manages its
   *  projects through this same dialog. */
  entity?: string;
  /** The board's repositories; with more than one writable, the add row asks
   *  which one the new entry is declared in. */
  domains?: DomainInfo[];
  onAdd: (name: string, domain?: string) => void;
  onRename: (from: string, to: string) => void;
  onRemove: (team: string) => void;
  onReorder: (ordered: string[]) => void;
  onClose: () => void;
}

interface TeamRowProps {
  team: string;
  editing: boolean;
  editValue: string;
  onEditValue: (v: string) => void;
  onStartEdit: () => void;
  onCommitEdit: () => void;
  onCancelEdit: () => void;
  onRemove: () => void;
}

/** A draggable team row inside the manage dialog. */
function TeamRow({
  team,
  editing,
  editValue,
  onEditValue,
  onStartEdit,
  onCommitEdit,
  onCancelEdit,
  onRemove,
}: TeamRowProps) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } =
    useSortable({ id: team });
  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
  };
  return (
    <li
      ref={setNodeRef}
      style={style}
      className={`teams-manage-row${isDragging ? " teams-manage-row-dragging" : ""}`}
    >
      <span
        className="teams-manage-grip"
        {...attributes}
        {...listeners}
        title="Drag to reorder"
      >
        ⠿
      </span>
      <span className="team-dot" style={{ background: teamColor(team) }} />
      {editing ? (
        <input
          type="text"
          className="add-card-input teams-manage-input"
          autoFocus
          value={editValue}
          onChange={(e) => onEditValue(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              onCommitEdit();
            } else if (e.key === "Escape") {
              onCancelEdit();
            }
          }}
          onBlur={onCommitEdit}
        />
      ) : (
        <button
          type="button"
          className="teams-manage-name"
          onDoubleClick={onStartEdit}
          title="Double-click to rename"
        >
          {team}
        </button>
      )}
      <button
        type="button"
        className="teams-manage-btn teams-manage-del"
        onClick={onRemove}
        aria-label={`Delete ${team}`}
        title="Delete"
      >
        ✕
      </button>
    </li>
  );
}

/** TeamsModal manages the roster: create, rename (double-click), delete, drag to reorder. */
export function TeamsModal({
  teams,
  title = "Manage teams",
  entity = "team",
  domains = [],
  onAdd,
  onRename,
  onRemove,
  onReorder,
  onClose,
}: TeamsModalProps) {
  const [newName, setNewName] = useState("");
  const [newDomain, setNewDomain] = useState("");
  const [editing, setEditing] = useState<string | null>(null);
  const [editValue, setEditValue] = useState("");
  // Why the last add or rename was not sent: a name another entry has. Names
  // are one namespace across the board's repositories, and the server would
  // refuse it too — the dialog says so before anything leaves.
  const [error, setError] = useState<string | null>(null);

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
  );

  const addNew = () => {
    const t = newName.trim();
    if (!t) {
      return;
    }
    const conflict = nameConflict(entity as RosterKind, teams, t);
    if (conflict) {
      setError(conflict);
      return;
    }
    setError(null);
    onAdd(t, declareDomain(domains, newDomain));
    setNewName("");
  };

  const startEdit = (team: string) => {
    setEditing(team);
    setEditValue(team);
  };

  const commitEdit = (from: string) => {
    const to = editValue.trim();
    setEditing(null);
    if (!to || to === from) {
      return;
    }
    const conflict = nameConflict(entity as RosterKind, teams, to, from);
    if (conflict) {
      setError(conflict);
      return;
    }
    setError(null);
    onRename(from, to);
  };

  const onDragEnd = (e: DragEndEvent) => {
    const { active, over } = e;
    if (!over || active.id === over.id) {
      return;
    }
    const from = teams.indexOf(String(active.id));
    const to = teams.indexOf(String(over.id));
    if (from === -1 || to === -1) {
      return;
    }
    onReorder(arrayMove(teams, from, to));
  };

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div
        className="modal"
        role="dialog"
        aria-modal="true"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="modal-header">
          <h2 className="modal-title">{title}</h2>
          <button
            type="button"
            className="modal-close"
            onClick={onClose}
            aria-label="Close"
          >
            ✕
          </button>
        </div>

        <div className="modal-body">
          <DndContext
            sensors={sensors}
            collisionDetection={closestCenter}
            onDragEnd={onDragEnd}
          >
            <SortableContext items={teams} strategy={verticalListSortingStrategy}>
              <ul className="teams-manage-list">
                {teams.length === 0 && (
                  <li className="teams-manage-empty">No {entity}s yet.</li>
                )}
                {teams.map((team) => (
                  <TeamRow
                    key={team}
                    team={team}
                    editing={editing === team}
                    editValue={editValue}
                    onEditValue={setEditValue}
                    onStartEdit={() => startEdit(team)}
                    onCommitEdit={() => commitEdit(team)}
                    onCancelEdit={() => setEditing(null)}
                    onRemove={() => onRemove(team)}
                  />
                ))}
              </ul>
            </SortableContext>
          </DndContext>

          {error && (
            <p className="form-error" role="alert">
              {error}
            </p>
          )}
          <div className="teams-manage-add">
            <DomainSelect domains={domains} value={newDomain} onChange={setNewDomain} />
            <input
              type="text"
              className="add-card-input"
              placeholder={`New ${entity} name…`}
              value={newName}
              onChange={(e) => {
                setNewName(e.target.value);
                setError(null);
              }}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  addNew();
                } else if (e.key === "Escape") {
                  onClose();
                }
              }}
            />
            <button
              type="button"
              className="btn btn-primary"
              onClick={addNew}
              disabled={!newName.trim()}
            >
              Add
            </button>
          </div>
        </div>

        <div className="modal-footer">
          <button type="button" className="btn" onClick={onClose}>
            Done
          </button>
        </div>
      </div>
    </div>
  );
}
