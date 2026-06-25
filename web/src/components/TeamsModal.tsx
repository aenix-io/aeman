import { useState } from "react";
import { teamColor } from "../avatar";

interface TeamsModalProps {
  teams: string[];
  onAdd: (name: string) => void;
  onRename: (from: string, to: string) => void;
  onRemove: (team: string) => void;
  onReorder: (ordered: string[]) => void;
  onClose: () => void;
}

/** TeamsModal manages the roster: create, rename (double-click), delete, reorder. */
export function TeamsModal({
  teams,
  onAdd,
  onRename,
  onRemove,
  onReorder,
  onClose,
}: TeamsModalProps) {
  const [newName, setNewName] = useState("");
  const [editing, setEditing] = useState<string | null>(null);
  const [editValue, setEditValue] = useState("");

  const addNew = () => {
    const t = newName.trim();
    if (t) {
      onAdd(t);
    }
    setNewName("");
  };

  const startEdit = (team: string) => {
    setEditing(team);
    setEditValue(team);
  };

  const commitEdit = (from: string) => {
    const to = editValue.trim();
    setEditing(null);
    if (to && to !== from) {
      onRename(from, to);
    }
  };

  const move = (index: number, dir: -1 | 1) => {
    const j = index + dir;
    if (j < 0 || j >= teams.length) {
      return;
    }
    const next = [...teams];
    [next[index], next[j]] = [next[j], next[index]];
    onReorder(next);
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
          <h2 className="modal-title">Manage teams</h2>
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
          <ul className="teams-manage-list">
            {teams.length === 0 && (
              <li className="teams-manage-empty">No teams yet.</li>
            )}
            {teams.map((team, i) => (
              <li className="teams-manage-row" key={team}>
                <span className="team-dot" style={{ background: teamColor(team) }} />
                {editing === team ? (
                  <input
                    type="text"
                    className="add-card-input teams-manage-input"
                    autoFocus
                    value={editValue}
                    onChange={(e) => setEditValue(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === "Enter") {
                        commitEdit(team);
                      } else if (e.key === "Escape") {
                        setEditing(null);
                      }
                    }}
                    onBlur={() => commitEdit(team)}
                  />
                ) : (
                  <button
                    type="button"
                    className="teams-manage-name"
                    onDoubleClick={() => startEdit(team)}
                    title="Double-click to rename"
                  >
                    {team}
                  </button>
                )}
                <span className="teams-manage-actions">
                  <button
                    type="button"
                    className="teams-manage-btn"
                    onClick={() => move(i, -1)}
                    disabled={i === 0}
                    aria-label="Move up"
                    title="Move up"
                  >
                    ▲
                  </button>
                  <button
                    type="button"
                    className="teams-manage-btn"
                    onClick={() => move(i, 1)}
                    disabled={i === teams.length - 1}
                    aria-label="Move down"
                    title="Move down"
                  >
                    ▼
                  </button>
                  <button
                    type="button"
                    className="teams-manage-btn teams-manage-del"
                    onClick={() => onRemove(team)}
                    aria-label={`Delete ${team}`}
                    title="Delete"
                  >
                    ✕
                  </button>
                </span>
              </li>
            ))}
          </ul>

          <div className="teams-manage-add">
            <input
              type="text"
              className="add-card-input"
              placeholder="New team name…"
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
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
