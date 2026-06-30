import { useState } from "react";

interface TeamChipsProps {
  label: string;
  /** Roster of teams to show as chips. */
  teams: string[];
  /** The selected group keys, or null for all (no filter). "" is the no-team
   *  group. Multi-select: Shift-click adds/removes a chip. */
  selectedKeys: string[] | null;
  /** Set the selection, or null to clear (show all). */
  onSelect: (keys: string[] | null) => void;
  onAdd: (name: string) => void;
  onRemove: (team: string) => void;
  onRename: (from: string, to: string) => void;
  /** Show a "No team" chip (key "") — used by the Team filter. */
  noTeamChip?: boolean;
  /** Allow removing / renaming teams (the × and double-click). */
  canManage?: boolean;
  /** When set, show a "manage" link that opens the roster manager. */
  onManage?: () => void;
  /** Optional eye toggle (Me board): focus the board on the selected teams. */
  filterToggle?: { on: boolean; onToggle: () => void };
}

/** TeamChips is a single-select row of team chips with add/remove/rename. */
export function TeamChips({
  label,
  teams,
  selectedKeys,
  onSelect,
  onAdd,
  onRemove,
  onRename,
  noTeamChip = false,
  canManage = true,
  onManage,
  filterToggle,
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

  // Plain click selects just this chip (clearing it if it was the only one);
  // Shift-click adds/removes the chip in a multi-select.
  const handleClick = (key: string, shift: boolean) => {
    if (shift) {
      const base = selectedKeys ?? [];
      const next = base.includes(key)
        ? base.filter((k) => k !== key)
        : [...base, key];
      onSelect(next.length ? next : null);
      return;
    }
    onSelect(
      selectedKeys?.length === 1 && selectedKeys[0] === key ? null : [key],
    );
  };

  return (
    <div className="field field-inline team-select">
      <span>{label}</span>
      <div className="team-chips">
        {teams.map((t) => {
          const on = selectedKeys?.includes(t) ?? false;
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
                onClick={(e) => handleClick(t, e.shiftKey)}
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
                  canManage
                    ? "Click to select · Shift-click to add · double-click to rename"
                    : "Click to select · Shift-click to add"
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
            className={`team-chip team-filter-chip${(selectedKeys?.includes("") ?? false) ? "" : " team-filter-chip-off"}`}
          >
            <button
              type="button"
              className="team-chip-toggle"
              onClick={(e) => handleClick("", e.shiftKey)}
              aria-pressed={selectedKeys?.includes("") ?? false}
              title="Cards with no team"
            >
              <span className="team-chip-name team-col-unassigned">No team</span>
            </button>
          </span>
        )}
        {filterToggle && (
          <button
            type="button"
            className={`team-eye${filterToggle.on ? " team-eye-on" : ""}`}
            onClick={filterToggle.onToggle}
            aria-pressed={filterToggle.on}
            title={
              filterToggle.on
                ? "Showing only the selected teams — click to show all"
                : "Show only the selected teams"
            }
          >
            <svg
              width="15"
              height="15"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
              aria-hidden="true"
            >
              <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z" />
              <circle cx="12" cy="12" r="3" />
            </svg>
          </button>
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
        {onManage && (
          <button type="button" className="add-card" onClick={onManage}>
            manage
          </button>
        )}
      </div>
    </div>
  );
}
