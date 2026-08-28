import { useState } from "react";
import { declareDomain, type DomainInfo } from "../domains";
import { nameConflict, type RosterKind } from "../names";
import { DomainSelect, blurredIntoDomainSelect } from "./DomainSelect";

interface TeamChipsProps {
  label: string;
  /** Roster of names to show as chips. */
  teams: string[];
  /** What one chip stands for, used in the add/remove wording. The row is
   *  shared: the Project board files its projects through it too, and a chip
   *  row that says "team name…" while adding a project reads as a bug. */
  entity?: string;
  /** The selected group keys, or null for all (no filter). "" is the no-team
   *  group. Multi-select: Shift-click adds/removes a chip. */
  selectedKeys: string[] | null;
  /** Set the selection, or null to clear (show all). */
  onSelect: (keys: string[] | null) => void;
  /** The board's repositories; with more than one writable, the add field
   *  asks which one the new entry is declared in. */
  domains?: DomainInfo[];
  onAdd: (name: string, domain?: string) => void;
  onRemove: (team: string) => void;
  /** Rename on double-click. Omit where renaming is not supported. */
  onRename?: (from: string, to: string) => void;
  /** Label for the catch-all chip (key ""), e.g. "No team". Omit to hide it. */
  noneChip?: string;
  /** Allow removing / renaming teams (the × and double-click). */
  canManage?: boolean;
  /** When set, show a "manage" link that opens the roster manager. */
  onManage?: () => void;
  /** Optional eye toggle (Me board): focus the board on the selected teams. */
  filterToggle?: { on: boolean; onToggle: () => void };
  /** Optional "workable now" toggle (Me board), shown right after the eye and
   *  of the same shape: hide locked / on-review / done cards. */
  focusToggle?: { on: boolean; onToggle: () => void };
}

/** TeamChips is a single-select row of team chips with add/remove/rename. */
export function TeamChips({
  label,
  teams,
  entity = "team",
  selectedKeys,
  onSelect,
  domains = [],
  onAdd,
  onRemove,
  onRename,
  noneChip,
  canManage = true,
  onManage,
  filterToggle,
  focusToggle,
}: TeamChipsProps) {
  const [adding, setAdding] = useState(false);
  const [addValue, setAddValue] = useState("");
  const [addDomain, setAddDomain] = useState("");
  const [editingTeam, setEditingTeam] = useState<string | null>(null);
  const [editValue, setEditValue] = useState("");
  // Why the last add or rename was not sent: a name another chip already
  // has (names are one namespace across the board's repositories).
  const [error, setError] = useState<string | null>(null);

  const commitAdd = () => {
    const t = addValue.trim();
    if (t) {
      const conflict = nameConflict(entity as RosterKind, teams, t);
      if (conflict) {
        setError(conflict);
        return; // keep the field and its value: the person edits, not retypes
      }
      onAdd(t, declareDomain(domains, addDomain));
    }
    setError(null);
    setAddValue("");
    setAdding(false);
  };

  const commitEdit = (from: string) => {
    const to = editValue.trim();
    setEditingTeam(null);
    if (!to || to === from) {
      return;
    }
    const conflict = nameConflict(entity as RosterKind, teams, to, from);
    if (conflict) {
      setError(conflict);
      return;
    }
    setError(null);
    onRename?.(from, to);
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
                  canManage && onRename
                    ? () => {
                        setEditValue(t);
                        setEditingTeam(t);
                      }
                    : undefined
                }
                aria-pressed={on}
                title={
                  canManage && onRename
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
                  title={`Remove ${entity}`}
                >
                  ×
                </button>
              )}
            </span>
          );
        })}
        {noneChip && (
          <span
            className={`team-chip team-filter-chip${(selectedKeys?.includes("") ?? false) ? "" : " team-filter-chip-off"}`}
          >
            <button
              type="button"
              className="team-chip-toggle"
              onClick={(e) => handleClick("", e.shiftKey)}
              aria-pressed={selectedKeys?.includes("") ?? false}
              title={`Cards with no ${entity}`}
            >
              <span className="team-chip-name team-col-unassigned">{noneChip}</span>
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
        {focusToggle && (
          <button
            type="button"
            className={`team-eye${focusToggle.on ? " team-eye-on" : ""}`}
            onClick={focusToggle.onToggle}
            aria-pressed={focusToggle.on}
            title={
              focusToggle.on
                ? "Showing only cards you can work on now — click to show all"
                : "Show only cards you can work on now (hide locked / on-review / done)"
            }
          >
            <svg width="15" height="15" viewBox="-351 153 256 256" fill="currentColor" aria-hidden="true">
              <circle cx="-222.3" cy="188.5" r="31.1" />
              <path d="M-106.6,332.4c-0.4-0.6-0.9-1.1-1.4-1.6l-35.3-32.8l-22.8-49c-6.2-12.5-15.2-20.3-28.6-20.3h-57.5c-13.5,0-22.4,7.8-28.6,20.3l-22.8,49l-35.3,32.8c-0.5,0.5-1,1.1-1.4,1.6c-3.6,3.1-5.9,7.7-5.9,12.8c0,9.3,7.6,16.9,16.9,16.9c5.5,0,10.3-2.6,13.4-6.7c0.3-0.2,0.6-0.5,0.8-0.7l37.4-34.8c1.4-1.4,2.5-3,3.3-4.8l11.9-25.5l-0.6,45l-52.2,28.4c-9.5,5.2-14,16.4-10.6,26.7c3.4,10.3,13.6,16.7,24.3,15.2l78.1-20.2l78.1,20.2c10.7,1.5,21-4.9,24.3-15.2c3.4-10.3-1.1-21.5-10.6-26.7l-52.2-28.5l-0.6-45l11.9,25.5c0.8,1.8,2,3.4,3.3,4.8l37.4,34.8c0.3,0.3,0.5,0.5,0.8,0.7c3.1,4,7.9,6.7,13.4,6.7c9.3,0,16.9-7.6,16.9-16.9C-100.7,340-103,335.5-106.6,332.4z" />
            </svg>
          </button>
        )}
        {canManage &&
          (adding ? (
            <>
              <DomainSelect domains={domains} value={addDomain} onChange={setAddDomain} />
              <input
                type="text"
                className="add-card-input team-add-input"
                autoFocus
                value={addValue}
                placeholder={`${entity} name…`}
                onChange={(e) => {
                  setAddValue(e.target.value);
                  setError(null);
                }}
                onKeyDown={(e) => {
                  if (e.key === "Enter") {
                    commitAdd();
                  } else if (e.key === "Escape") {
                    setAddValue("");
                    setAdding(false);
                  }
                }}
                onBlur={(e) => {
                  if (!blurredIntoDomainSelect(e)) {
                    commitAdd();
                  }
                }}
              />
            </>
          ) : (
            <button type="button" className="add-card" onClick={() => setAdding(true)}>
              + add
            </button>
          ))}
        {error && (
          <span className="form-error" role="alert">
            {error}
          </span>
        )}
        {onManage && (
          <button type="button" className="add-card" onClick={onManage}>
            manage
          </button>
        )}
      </div>
    </div>
  );
}
