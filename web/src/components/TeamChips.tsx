import { useState } from "react";

interface TeamChipsProps {
  label: string;
  /** Roster of teams to show as chips. */
  teams: string[];
  /** The selected group key, or null for none. "" is the no-team group. */
  selectedKey: string | null;
  /** Select a group, or null to clear. */
  onSelect: (key: string | null) => void;
  onAdd: (name: string) => void;
  onRemove: (team: string) => void;
  onRename: (from: string, to: string) => void;
  /** Show a "No team" chip (key "") — used by the Team filter. */
  noTeamChip?: boolean;
  /** Allow removing / renaming teams (the × and double-click). */
  canManage?: boolean;
}

/** TeamChips is a single-select row of team chips with add/remove/rename. */
export function TeamChips({
  label,
  teams,
  selectedKey,
  onSelect,
  onAdd,
  onRemove,
  onRename,
  noTeamChip = false,
  canManage = true,
}: TeamChipsProps) {
  const [adding, setAdding] = useState(false);
  const [addValue, setAddValue] = useState("");
  const [editingTeam, setEditingTeam] = useState<string | null>(null);
  const [editValue, setEditValue] = useState("");

  const commitAdd = () => {
    const t = addValue.trim();
    if (t) {
      onAdd(t);
    }
    setAddValue("");
    setAdding(false);
  };

  const commitEdit = (from: string) => {
    const to = editValue.trim();
    setEditingTeam(null);
    if (to && to !== from) {
      onRename(from, to);
    }
  };

  // Single-select: clicking the active chip clears the selection.
  const toggle = (key: string) => onSelect(selectedKey === key ? null : key);

  return (
    <div className="field field-inline team-select">
      <span>{label}</span>
      <div className="team-chips">
        {teams.map((t) => {
          const on = selectedKey === t;
          if (editingTeam === t) {
            return (
              <span className="team-chip team-filter-chip" key={t}>
                <input
                  type="text"
                  className="add-card-input team-add-input"
                  autoFocus
                  value={editValue}
                  onChange={(e) => setEditValue(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") {
                      commitEdit(t);
                    } else if (e.key === "Escape") {
                      setEditingTeam(null);
                    }
                  }}
                  onBlur={() => commitEdit(t)}
                />
              </span>
            );
          }
          return (
            <span
              className={`team-chip team-filter-chip${on ? "" : " team-filter-chip-off"}`}
              key={t}
            >
              <button
                type="button"
                className="team-chip-toggle"
                onClick={() => toggle(t)}
                onDoubleClick={
                  canManage
                    ? () => {
                        setEditValue(t);
                        setEditingTeam(t);
                      }
                    : undefined
                }
                aria-pressed={on}
                title={
                  canManage ? "Click to select · double-click to rename" : "Click to select"
                }
              >
                <span className="team-chip-name">{t}</span>
              </button>
              {canManage && (
                <button
                  type="button"
                  className="team-chip-x"
                  onClick={() => onRemove(t)}
                  aria-label={`Remove ${t}`}
                  title="Remove team"
                >
                  ×
                </button>
              )}
            </span>
          );
        })}
        {noTeamChip && (
          <span
            className={`team-chip team-filter-chip${selectedKey === "" ? "" : " team-filter-chip-off"}`}
          >
            <button
              type="button"
              className="team-chip-toggle"
              onClick={() => toggle("")}
              aria-pressed={selectedKey === ""}
              title="Cards with no team"
            >
              <span className="team-chip-name team-col-unassigned">No team</span>
            </button>
          </span>
        )}
        {canManage &&
          (adding ? (
            <input
              type="text"
              className="add-card-input team-add-input"
              autoFocus
              value={addValue}
              placeholder="team name…"
              onChange={(e) => setAddValue(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  commitAdd();
                } else if (e.key === "Escape") {
                  setAddValue("");
                  setAdding(false);
                }
              }}
              onBlur={commitAdd}
            />
          ) : (
            <button type="button" className="add-card" onClick={() => setAdding(true)}>
              + add
            </button>
          ))}
      </div>
    </div>
  );
}
