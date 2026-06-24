import { useRef, useState } from "react";
import type { Card as CardModel, StageKey } from "../providers/types";
import { STAGES, STAGE_ORDER, DEFAULT_BAR_COLOR } from "../stages";
import { initials, teamColor, teamInitial } from "../avatar";
import { addDays, todayIso } from "../date";
import { Dropdown } from "./Dropdown";

interface CardProps {
  card: CardModel;
  selected: boolean;
  onSelect: (card: CardModel) => void;
  onProgress: (card: CardModel, value: number) => void;
  onDelete: (card: CardModel) => void;
  onStage: (card: CardModel, stage: StageKey | null) => void;
  onRename: (card: CardModel, title: string) => void;
  onOpen: (card: CardModel) => void;
  /** Locking requires a reason, gathered in a modal lifted to App. */
  onRequestLock: (card: CardModel) => void;
  /** Move the card's start date (Team board only); absent hides the control. */
  onMoveStart?: (card: CardModel, newStart: string) => void;
  /** Reassign the card's team / person from the avatar menu (when provided). */
  teams?: string[];
  people?: string[];
  onSetTeam?: (card: CardModel, team: string | null) => void;
  onSetAssignee?: (card: CardModel, login: string | null) => void;
}

const SEGMENTS = 10;

/** ticket renders the monospace ticket reference for a card with a number. */
function ticket(card: CardModel): string | null {
  if (card.number === undefined) {
    return null;
  }
  return card.repository ? `${card.repository}#${card.number}` : `#${card.number}`;
}

/** barColor is the fill colour for the progress segments, driven by stage. */
function barColor(stage?: StageKey): string {
  return stage ? STAGES[stage].color : DEFAULT_BAR_COLOR;
}

/** Card is a compact single-row item shared by the Me and Team boards. */
export function Card({
  card,
  selected,
  onSelect,
  onProgress,
  onDelete,
  onStage,
  onRename,
  onOpen,
  onRequestLock,
  onMoveStart,
  teams,
  people,
  onSetTeam,
  onSetAssignee,
}: CardProps) {
  const value = card.stage === "done" ? 100 : card.progress ?? 0;
  const fill = barColor(card.stage);
  const ref = ticket(card);

  const [menuOpen, setMenuOpen] = useState(false);
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(card.title);
  const [dragValue, setDragValue] = useState<number | null>(null);
  const [startMenuOpen, setStartMenuOpen] = useState(false);
  const [assignOpen, setAssignOpen] = useState(false);
  const [personInput, setPersonInput] = useState("");
  const menuRef = useRef<HTMLDivElement | null>(null);
  const startRef = useRef<HTMLDivElement | null>(null);
  const assignRef = useRef<HTMLDivElement | null>(null);
  const barRef = useRef<HTMLDivElement | null>(null);
  const customDateRef = useRef<HTMLInputElement | null>(null);

  const canAssign = Boolean(onSetTeam || onSetAssignee);

  // While dragging the handle, show the snapped drag value; otherwise the card's.
  const shown = dragValue ?? value;
  const filled = Math.round(shown / 10);

  // Progress is changed only by dragging the handle, which snaps to 10% steps.
  const onHandleDown = (e: React.PointerEvent<HTMLDivElement>) => {
    e.stopPropagation();
    e.preventDefault();
    e.currentTarget.setPointerCapture(e.pointerId);
    setDragValue(value);
  };

  const onHandleMove = (e: React.PointerEvent<HTMLDivElement>) => {
    if (dragValue === null || !barRef.current) {
      return;
    }
    const rect = barRef.current.getBoundingClientRect();
    const frac = rect.width > 0 ? (e.clientX - rect.left) / rect.width : 0;
    const snapped = Math.min(100, Math.max(0, Math.round(frac * 10) * 10));
    if (snapped !== dragValue) {
      setDragValue(snapped);
    }
  };

  const onHandleUp = (e: React.PointerEvent<HTMLDivElement>) => {
    if (dragValue === null) {
      return;
    }
    e.currentTarget.releasePointerCapture(e.pointerId);
    const final = dragValue;
    setDragValue(null);
    if (final !== value) {
      onProgress(card, final);
    }
  };

  const handleDelete = (e: React.MouseEvent<HTMLButtonElement>) => {
    e.stopPropagation();
    if (window.confirm(`Delete "${card.title}"?`)) {
      onDelete(card);
    }
  };

  const pickStage = (e: React.MouseEvent<HTMLButtonElement>, stage: StageKey | null) => {
    e.stopPropagation();
    setMenuOpen(false);
    onStage(card, stage);
  };

  // Locking opens a modal (lifted to App) to gather the reason note.
  const requestLock = (e: React.MouseEvent<HTMLButtonElement>) => {
    e.stopPropagation();
    setMenuOpen(false);
    onRequestLock(card);
  };

  const moveStartBy = (e: React.MouseEvent<HTMLButtonElement>, days: number) => {
    e.stopPropagation();
    setStartMenuOpen(false);
    onMoveStart?.(card, addDays(card.startDate ?? todayIso(), days));
  };

  const moveStartTo = (date: string) => {
    if (!date) {
      return;
    }
    setStartMenuOpen(false);
    onMoveStart?.(card, date);
  };

  // Open the native date picker directly (no visible dd/mm/yyyy text field).
  const openCustom = (e: React.MouseEvent<HTMLButtonElement>) => {
    e.stopPropagation();
    customDateRef.current?.showPicker();
  };

  const pickAssignTeam = (team: string | null) => {
    setAssignOpen(false);
    onSetTeam?.(card, team);
  };

  const pickAssignPerson = (login: string | null) => {
    setAssignOpen(false);
    setPersonInput("");
    onSetAssignee?.(card, login);
  };

  const startEdit = (e: React.MouseEvent<HTMLButtonElement>) => {
    e.stopPropagation();
    setDraft(card.title);
    setEditing(true);
  };

  const commitEdit = () => {
    const next = draft.trim();
    if (next && next !== card.title) {
      onRename(card, next);
    }
    setEditing(false);
  };

  return (
    <div
      className={`card${selected ? " card-selected" : ""}`}
      onClick={() => onSelect(card)}
      onDoubleClick={() => onOpen(card)}
      title={card.title}
    >
      {editing ? (
        <input
          type="text"
          className="card-title-input"
          autoFocus
          value={draft}
          onClick={(e) => e.stopPropagation()}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            e.stopPropagation();
            if (e.key === "Enter") {
              commitEdit();
            } else if (e.key === "Escape") {
              setEditing(false);
            }
          }}
          onBlur={() => setEditing(false)}
        />
      ) : (
        <span className="card-title">{card.title}</span>
      )}

      {ref && <span className="card-ticket">{ref}</span>}

      <span className="card-actions" aria-hidden={false}>
        <button
          type="button"
          className="card-action"
          onClick={startEdit}
          aria-label="Rename card"
          title="Rename"
        >
          ✎
        </button>
        <div className="card-stage" ref={menuRef}>
          <button
            type="button"
            className="card-action"
            onClick={(e) => {
              e.stopPropagation();
              setMenuOpen((o) => !o);
            }}
            aria-label="Set status"
            title="Status"
            style={card.stage ? { color: STAGES[card.stage].color } : undefined}
          >
            ⚑
          </button>
          <Dropdown
            open={menuOpen}
            anchorRef={menuRef}
            onClose={() => setMenuOpen(false)}
            className="card-stage-menu"
          >
            {STAGE_ORDER.map((stage) => (
              <button
                key={stage}
                type="button"
                className={`card-stage-item${card.stage === stage ? " card-stage-item-active" : ""}`}
                onClick={(e) =>
                  stage === "locked" ? requestLock(e) : pickStage(e, stage)
                }
              >
                <span
                  className="card-stage-dot"
                  style={{ background: STAGES[stage].color }}
                />
                {STAGES[stage].label}
              </button>
            ))}
            <button
              type="button"
              className="card-stage-item card-stage-clear"
              onClick={(e) => pickStage(e, null)}
            >
              Clear
            </button>
          </Dropdown>
        </div>
        {onMoveStart && (
          <div className="card-stage" ref={startRef}>
            <button
              type="button"
              className="card-action"
              onClick={(e) => {
                e.stopPropagation();
                setStartMenuOpen((o) => !o);
              }}
              aria-label="Move start date"
              title="Move start date"
            >
              »
            </button>
            <Dropdown
              open={startMenuOpen}
              anchorRef={startRef}
              onClose={() => setStartMenuOpen(false)}
              className="card-stage-menu"
            >
              <button
                type="button"
                className="card-stage-item"
                onClick={(e) => moveStartBy(e, 1)}
              >
                +1 day
              </button>
              <button
                type="button"
                className="card-stage-item"
                onClick={(e) => moveStartBy(e, 7)}
              >
                +1 week
              </button>
              <button type="button" className="card-stage-item" onClick={openCustom}>
                Custom…
              </button>
              <input
                ref={customDateRef}
                type="date"
                className="card-date-hidden"
                value={card.startDate ?? ""}
                onClick={(e) => e.stopPropagation()}
                onChange={(e) => moveStartTo(e.target.value)}
                tabIndex={-1}
                aria-hidden="true"
              />
            </Dropdown>
          </div>
        )}
        <button
          type="button"
          className="card-action card-action-delete"
          onClick={handleDelete}
          aria-label="Delete card"
          title="Delete"
        >
          ×
        </button>
      </span>

      {(card.team || canAssign) && (
        <div className="card-assign" ref={assignRef}>
          {card.team ? (
            <button
              type="button"
              className="team-avatar"
              style={{ backgroundColor: teamColor(card.team) }}
              title={canAssign ? "Reassign team / person" : `Team: ${card.team}`}
              onClick={
                canAssign
                  ? (e) => {
                      e.stopPropagation();
                      setAssignOpen((o) => !o);
                    }
                  : undefined
              }
            >
              {teamInitial(card.team)}
            </button>
          ) : (
            <button
              type="button"
              className="team-avatar team-avatar-empty"
              title="Assign team / person"
              onClick={(e) => {
                e.stopPropagation();
                setAssignOpen((o) => !o);
              }}
            >
              +
            </button>
          )}
          <Dropdown
            open={assignOpen}
            anchorRef={assignRef}
            onClose={() => setAssignOpen(false)}
            className="card-stage-menu card-assign-menu"
          >
            <div className="card-assign-cols">
              {onSetTeam && (
                <div className="card-assign-col">
                  <div className="card-assign-head">Team</div>
                  {(teams ?? []).map((t) => (
                    <button
                      key={`t-${t}`}
                      type="button"
                      className={`card-stage-item${card.team === t ? " card-stage-item-active" : ""}`}
                      onClick={() => pickAssignTeam(t)}
                    >
                      <span className="team-dot" style={{ background: teamColor(t) }} />
                      {t}
                    </button>
                  ))}
                  <button
                    type="button"
                    className="card-stage-item card-stage-clear"
                    onClick={() => pickAssignTeam(null)}
                  >
                    No team
                  </button>
                </div>
              )}
              {onSetAssignee && (
                <div className="card-assign-col">
                  <div className="card-assign-head">Person</div>
                  {(people ?? []).map((p) => (
                    <button
                      key={`p-${p}`}
                      type="button"
                      className={`card-stage-item${card.assignees.includes(p) ? " card-stage-item-active" : ""}`}
                      onClick={() => pickAssignPerson(p)}
                    >
                      <span className="avatar">{initials(p)}</span>
                      {p}
                    </button>
                  ))}
                  <button
                    type="button"
                    className="card-stage-item card-stage-clear"
                    onClick={() => pickAssignPerson(null)}
                  >
                    Unassigned
                  </button>
                  <input
                    type="text"
                    className="add-card-input card-assign-input"
                    placeholder="login…"
                    value={personInput}
                    onClick={(e) => e.stopPropagation()}
                    onChange={(e) => setPersonInput(e.target.value)}
                    onKeyDown={(e) => {
                      e.stopPropagation();
                      if (e.key === "Enter" && personInput.trim()) {
                        pickAssignPerson(personInput.trim());
                      } else if (e.key === "Escape") {
                        setAssignOpen(false);
                      }
                    }}
                  />
                </div>
              )}
            </div>
          </Dropdown>
        </div>
      )}

      <div className="card-bar" ref={barRef} title={`${shown}%`}>
        {Array.from({ length: SEGMENTS }, (_, i) => (
          <span
            key={i}
            className={`card-seg${i < filled ? " card-seg-filled" : ""}`}
            style={i < filled ? { backgroundColor: fill } : undefined}
          />
        ))}
        <div
          className={`card-bar-handle${dragValue !== null ? " card-bar-handle-dragging" : ""}`}
          style={{ left: `${shown}%`, borderColor: fill }}
          role="slider"
          aria-label="Progress"
          aria-valuenow={shown}
          aria-valuemin={0}
          aria-valuemax={100}
          onClick={(e) => e.stopPropagation()}
          onPointerDown={onHandleDown}
          onPointerMove={onHandleMove}
          onPointerUp={onHandleUp}
        />
      </div>
    </div>
  );
}
