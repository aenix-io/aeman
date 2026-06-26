import { useRef, useState } from "react";
import type { Card as CardModel, StageKey } from "../providers/types";
import { STAGES, STAGE_ORDER, DEFAULT_BAR_COLOR } from "../stages";
import { teamColor, teamInitial } from "../avatar";
import { avatarUrlFor, displayName, type GhUser } from "../users";
import { addDays, daysSince, todayIso, mondayOf } from "../date";
import { Dropdown } from "./Dropdown";
import { RangeCalendar } from "./RangeCalendar";

// ageColor fades the age badge from light grey (fresh) to maroon-red by ~10 days.
function ageColor(days: number): string {
  const t = Math.min(1, days / 10);
  const from = [176, 181, 189]; // light grey
  const to = [140, 20, 40]; // maroon-red
  const ch = (i: number) => Math.round(from[i] + (to[i] - from[i]) * t);
  return `rgb(${ch(0)}, ${ch(1)}, ${ch(2)})`;
}

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
  /** Reassign the card's team / person from the avatar menu (when provided). */
  teams?: string[];
  people?: string[];
  users?: Record<string, GhUser>;
  onSetTeam?: (card: CardModel, team: string | null) => void;
  onSetAssignee?: (card: CardModel, login: string | null) => void;
  /** The day the board is showing; the age badge is measured up to it. */
  asOf?: string;
  /** Edit the card's start/end dates from the age badge (when provided). */
  onSetDates?: (card: CardModel, start: string | null, end: string | null) => void;
  /** Plan-card mode: the age-badge editor moves the plan week instead of dates. */
  weekMode?: boolean;
  onSetWeek?: (card: CardModel, week: string | null) => void;
  /** Tint a card that doesn't belong to the selected team (Me chip highlight). */
  offTeam?: boolean;
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
  teams,
  people,
  users,
  onSetTeam,
  onSetAssignee,
  asOf,
  onSetDates,
  weekMode,
  onSetWeek,
  offTeam,
}: CardProps) {
  const rawValue = card.stage === "done" ? 100 : card.progress ?? 0;
  // Locked / on-review cards show at least one segment, so the stage colour is
  // visible even before any progress has been set.
  const value =
    rawValue === 0 && (card.stage === "locked" || card.stage === "review")
      ? 10
      : rawValue;
  const fill = barColor(card.stage);
  const ref = ticket(card);

  const [menuOpen, setMenuOpen] = useState(false);
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(card.title);
  const [dragValue, setDragValue] = useState<number | null>(null);
  const [assignOpen, setAssignOpen] = useState(false);
  const [personInput, setPersonInput] = useState("");
  const menuRef = useRef<HTMLDivElement | null>(null);
  const assignRef = useRef<HTMLDivElement | null>(null);
  const barRef = useRef<HTMLDivElement | null>(null);
  const [datesOpen, setDatesOpen] = useState(false);
  const [startVal, setStartVal] = useState("");
  const [endVal, setEndVal] = useState("");
  const ageRef = useRef<HTMLDivElement | null>(null);

  const canAssign = Boolean(onSetTeam || onSetAssignee);
  const canEditDates = weekMode ? Boolean(onSetWeek) : Boolean(onSetDates);

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

  const pickAssignTeam = (team: string | null) => {
    setAssignOpen(false);
    onSetTeam?.(card, team);
  };

  const pickAssignPerson = (login: string | null) => {
    setAssignOpen(false);
    setPersonInput("");
    onSetAssignee?.(card, login);
  };

  const openDates = (e: React.MouseEvent<HTMLButtonElement>) => {
    e.stopPropagation();
    if (weekMode) {
      setStartVal(card.week ?? "");
    } else {
      setStartVal(card.startDate ?? "");
      setEndVal(card.sprintStart ?? "");
    }
    setDatesOpen((o) => !o);
  };

  const saveDates = () => {
    setDatesOpen(false);
    // A single picked date is a one-day range, not "clear the end".
    onSetDates?.(card, startVal || null, endVal || startVal || null);
  };

  // Quick-shift the start date by N days; push the finish along if overtaken.
  const moveStart = (days: number) => {
    const newStart = addDays(card.startDate ?? todayIso(), days);
    const end =
      card.sprintStart && card.sprintStart >= newStart
        ? card.sprintStart
        : newStart;
    setDatesOpen(false);
    onSetDates?.(card, newStart, end);
  };

  // Plan cards: shift the plan week forward, or set it by picking a date.
  const moveWeek = (days: number) => {
    const base = card.week ?? mondayOf(asOf ?? todayIso());
    setDatesOpen(false);
    onSetWeek?.(card, addDays(base, days));
  };

  const saveWeek = () => {
    setDatesOpen(false);
    onSetWeek?.(card, startVal || null);
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

  // A plan card not yet taken into work hasn't started aging: show 0d. Once it
  // has an assignee it ages normally; and it gets a green "taken" background.
  const taken = Boolean(weekMode) && card.assignees.length > 0;
  const ageDays =
    weekMode && card.assignees.length === 0 ? 0 : daysSince(card.createdAt, asOf);

  return (
    <div
      className={`card${selected ? " card-selected" : ""}${
        card.plan ? ` card-plan-${card.plan}` : ""
      }${taken ? " card-plan-taken" : ""}${offTeam ? " card-off-team" : ""}`}
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

      {card.createdAt && (
        <div
          className="card-age-wrap"
          ref={ageRef}
          onDoubleClick={(e) => e.stopPropagation()}
        >
          <button
            type="button"
            className="card-age"
            style={{ color: ageColor(ageDays) }}
            title={
              weekMode
                ? "Move the plan week"
                : onSetDates
                  ? "Edit start / end dates"
                  : `On the board ${ageDays} day(s)`
            }
            onClick={canEditDates ? openDates : undefined}
          >
            {ageDays}d
          </button>
          {canEditDates && (
            <Dropdown
              open={datesOpen}
              anchorRef={ageRef}
              onClose={() => setDatesOpen(false)}
              className="card-stage-menu card-dates-menu"
            >
              {weekMode ? (
                <>
                  <div className="card-move-quick">
                    <button
                      type="button"
                      className="card-move-quick-btn"
                      onClick={() => moveWeek(7)}
                    >
                      +1 week
                    </button>
                    <button
                      type="button"
                      className="card-move-quick-btn"
                      onClick={() => moveWeek(14)}
                    >
                      +2 week
                    </button>
                  </div>
                  <RangeCalendar
                    start={startVal || null}
                    end={null}
                    onChange={(s) => setStartVal(s ? mondayOf(s) : "")}
                  />
                  <button type="button" className="card-dates-save" onClick={saveWeek}>
                    Save
                  </button>
                  <button
                    type="button"
                    className="card-dates-cancel"
                    onClick={() => setDatesOpen(false)}
                  >
                    Cancel
                  </button>
                </>
              ) : (
                <>
                  <div className="card-move-quick">
                    <button
                      type="button"
                      className="card-move-quick-btn"
                      onClick={() => moveStart(1)}
                    >
                      +1 day
                    </button>
                    <button
                      type="button"
                      className="card-move-quick-btn"
                      onClick={() => moveStart(7)}
                    >
                      +1 week
                    </button>
                  </div>
                  <RangeCalendar
                    start={startVal || null}
                    end={endVal || null}
                    onChange={(s, e) => {
                      setStartVal(s ?? "");
                      setEndVal(e ?? "");
                    }}
                  />
                  <button type="button" className="card-dates-save" onClick={saveDates}>
                    Save
                  </button>
                  <button
                    type="button"
                    className="card-dates-cancel"
                    onClick={() => setDatesOpen(false)}
                  >
                    Cancel
                  </button>
                </>
              )}
            </Dropdown>
          )}
        </div>
      )}

      {(card.team || canAssign) && (
        <div className="card-assign" ref={assignRef}>
          {card.team ? (
            <button
              type="button"
              className="team-avatar"
              style={{ backgroundColor: teamColor(card.team) }}
              title={
                onSetAssignee
                  ? "Reassign team / person"
                  : onSetTeam
                    ? "Reassign team"
                    : `Team: ${card.team}`
              }
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
                      <img
                        className="avatar-img"
                        src={avatarUrlFor(p, users?.[p])}
                        alt={p}
                      />
                      {displayName(p, users?.[p])}
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
