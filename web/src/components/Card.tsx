import { useRef, useState } from "react";
import type { Card as CardModel, StageKey } from "../providers/types";
import { STAGES, STAGE_ORDER, DEFAULT_BAR_COLOR, isInProgress } from "../stages";
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
  /** Pick the implicit "In Progress" status: clears the stage and clamps the
   *  card's progress into [10, 90]. */
  onInProgress: (card: CardModel) => void;
  onRename: (card: CardModel, title: string) => void;
  onOpen: (card: CardModel) => void;
  /** Reassign the card's team / person from the avatar menu (when provided). */
  teams?: string[];
  people?: string[];
  users?: Record<string, GhUser>;
  onSetTeam?: (card: CardModel, team: string | null) => void;
  onSetAssignee?: (card: CardModel, login: string | null) => void;
  /** This card has a linked review card; deleting it cascades (the board owns
   *  the combined confirmation, so the card skips its own delete prompt). */
  hasLinkedReview?: boolean;
  /** Assignees of the linked counterpart card — the reviewer(s) on an original
   *  under review, the implementer(s) on a review card. */
  counterpartAssignees?: string[];
  /** Manage the linked review card from the counterpart menu: a login reassigns
   *  the review card, null deletes it. Only when this card has a linked review. */
  onSetReviewAssignee?: (card: CardModel, login: string | null) => void;
  /** The day the board is showing; the age badge is measured up to it. */
  asOf?: string;
  /** Edit the card's start/end dates from the age badge (when provided). */
  onSetDates?: (card: CardModel, start: string | null, end: string | null) => void;
  /** Plan-card mode: the age-badge editor moves the plan week instead of dates. */
  weekMode?: boolean;
  onSetWeek?: (card: CardModel, week: string | null) => void;
  /** Dim the card's team avatar to 50%. Set unless this card is the selected
   *  team, so only the selected team's avatars stay at full opacity. */
  dimAvatar?: boolean;
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
  onInProgress,
  onRename,
  onOpen,
  teams,
  people,
  users,
  onSetTeam,
  onSetAssignee,
  hasLinkedReview,
  counterpartAssignees,
  onSetReviewAssignee,
  asOf,
  onSetDates,
  weekMode,
  onSetWeek,
  dimAvatar,
}: CardProps) {
  const rawValue = card.stage === "done" ? 100 : card.progress ?? 0;
  // Locked / on-review cards are clamped to a 10–90% band, so they always show
  // the stage colour and never read as complete.
  const value =
    card.stage === "locked" || card.stage === "review"
      ? Math.min(90, Math.max(10, rawValue))
      : rawValue;
  const fill = barColor(card.stage);
  const ref = ticket(card);

  const [menuOpen, setMenuOpen] = useState(false);
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(card.title);
  const [dragValue, setDragValue] = useState<number | null>(null);
  const [assignOpen, setAssignOpen] = useState(false);
  const [personInput, setPersonInput] = useState("");
  const [reviewerInput, setReviewerInput] = useState("");
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
    // review/locked are clamped to 10–90%; other cards span 0–100% (0% clears).
    const locked = card.stage === "review" || card.stage === "locked";
    const min = locked ? 10 : 0;
    const max = locked ? 90 : 100;
    const snapped = Math.min(max, Math.max(min, Math.round(frac * 10) * 10));
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
    // Cards with a linked review card delegate the (combined) confirmation to
    // the board, which deletes both the original and its review card.
    if (hasLinkedReview) {
      onDelete(card);
      return;
    }
    if (window.confirm(`Delete "${card.title}"?`)) {
      onDelete(card);
    }
  };

  const pickStage = (e: React.MouseEvent<HTMLButtonElement>, stage: StageKey | null) => {
    e.stopPropagation();
    setMenuOpen(false);
    onStage(card, stage);
  };

  const pickInProgress = (e: React.MouseEvent<HTMLButtonElement>) => {
    e.stopPropagation();
    setMenuOpen(false);
    onInProgress(card);
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

  // Defer the card: push the sprint it belongs to (sprintStart) N days ahead of
  // today — or ahead of its already-deferred slot — keeping startDate as the day
  // it actually started. The boards hide it from today until the new sprint day,
  // so deferring an old card always counts from today, not from its creation.
  const moveStart = (days: number) => {
    const today = todayIso();
    const base =
      card.sprintStart && card.sprintStart > today ? card.sprintStart : today;
    setDatesOpen(false);
    onSetDates?.(card, card.startDate ?? null, addDays(base, days));
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
      }${taken ? " card-plan-taken" : ""}${card.reviewOf ? " card-review" : ""}${
        dimAvatar ? " card-dim-avatar" : ""
      }`}
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
          onDoubleClick={(e) => e.stopPropagation()}
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
        <span className="card-title" title={card.title}>
          {card.title}
        </span>
      )}

      {ref && <span className="card-ticket">{ref}</span>}

      <span className="card-actions" aria-hidden={false}>
        <button
          type="button"
          className="card-action card-hoveronly"
          onClick={startEdit}
          aria-label="Rename card"
          title="Rename"
        >
          ✎
        </button>
        <button
          type="button"
          className="card-action card-hoveronly card-action-delete"
          onClick={handleDelete}
          aria-label="Delete card"
          title="Delete"
        >
          ×
        </button>
        <div
          className={`card-stage${card.stage === "review" || card.stage === "locked" ? "" : " card-hoveronly"}`}
          ref={menuRef}
        >
          <button
            type="button"
            className="card-action card-status-btn"
            onClick={(e) => {
              e.stopPropagation();
              setMenuOpen((o) => !o);
            }}
            aria-label="Set status"
            title={
              card.stage === "review"
                ? "On review"
                : card.stage === "locked"
                  ? "Locked"
                  : "Status"
            }
            style={card.stage ? { color: STAGES[card.stage].color } : undefined}
          >
            {card.stage === "review" ? (
              <svg
                width="13"
                height="13"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2.4"
                strokeLinecap="round"
                strokeLinejoin="round"
                aria-hidden="true"
              >
                <path d="M21 2v6h-6" />
                <path d="M3 12a9 9 0 0 1 15-6.7L21 8" />
                <path d="M3 22v-6h6" />
                <path d="M21 12a9 9 0 0 1-15 6.7L3 16" />
              </svg>
            ) : card.stage === "locked" ? (
              <svg
                width="13"
                height="13"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2.2"
                strokeLinecap="round"
                strokeLinejoin="round"
                aria-hidden="true"
              >
                <path d="M12 3 2 21h20L12 3z" />
                <line x1="12" y1="10" x2="12" y2="15" />
                <line x1="12" y1="18" x2="12.01" y2="18" />
              </svg>
            ) : (
              "⚑"
            )}
          </button>
          <Dropdown
            open={menuOpen}
            anchorRef={menuRef}
            onClose={() => setMenuOpen(false)}
            className="card-stage-menu"
          >
            <button
              type="button"
              className={`card-stage-item${isInProgress(card) ? " card-stage-item-active" : ""}`}
              onClick={pickInProgress}
            >
              <span
                className="card-stage-dot"
                style={{ background: DEFAULT_BAR_COLOR }}
              />
              In Progress
            </button>
            {STAGE_ORDER.map((stage) =>
              // A review card cannot be put on the "review" stage itself.
              stage === "review" && card.reviewOf ? null : (
                <button
                  key={stage}
                  type="button"
                  className={`card-stage-item${card.stage === stage ? " card-stage-item-active" : ""}`}
                  onClick={(e) => pickStage(e, stage)}
                >
                  <span
                    className="card-stage-dot"
                    style={{ background: STAGES[stage].color }}
                  />
                  {STAGES[stage].label}
                </button>
              ),
            )}
          </Dropdown>
        </div>
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

      {(card.team || canAssign || (counterpartAssignees?.length ?? 0) > 0) && (
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
          {counterpartAssignees && counterpartAssignees.length > 0 && (
            <button
              type="button"
              className="card-counterpart-avatar-btn"
              title={`${card.reviewOf ? "In implementation" : "On review"}: ${counterpartAssignees
                .map((p) => displayName(p, users?.[p]))
                .join(", ")}`}
              onClick={
                canAssign
                  ? (e) => {
                      e.stopPropagation();
                      setAssignOpen((o) => !o);
                    }
                  : undefined
              }
            >
              <img
                className="card-counterpart-avatar"
                src={avatarUrlFor(
                  counterpartAssignees[0],
                  users?.[counterpartAssignees[0]],
                )}
                alt={displayName(
                  counterpartAssignees[0],
                  users?.[counterpartAssignees[0]],
                )}
              />
            </button>
          )}
          <Dropdown
            open={assignOpen}
            anchorRef={assignRef}
            onClose={() => setAssignOpen(false)}
            className="card-stage-menu card-assign-menu"
          >
            {card.author && (
              <div className="card-counterpart-head">
                Created by: {displayName(card.author, users?.[card.author])}
              </div>
            )}
            {counterpartAssignees && counterpartAssignees.length > 0 && (
              <div className="card-counterpart-head">
                {card.reviewOf ? "In implementation" : "On review"}:{" "}
                {counterpartAssignees
                  .map((p) => displayName(p, users?.[p]))
                  .join(", ")}
              </div>
            )}
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
              {card.stage === "review" && onSetReviewAssignee && (
                <div className="card-assign-col">
                  <div className="card-assign-head">Reviewer</div>
                  {(people ?? []).map((p) => (
                    <button
                      key={`r-${p}`}
                      type="button"
                      className={`card-stage-item${(counterpartAssignees ?? []).includes(p) ? " card-stage-item-active" : ""}`}
                      onClick={() => {
                        onSetReviewAssignee(card, p);
                        setAssignOpen(false);
                      }}
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
                    onClick={() => {
                      onSetReviewAssignee(card, null);
                      setAssignOpen(false);
                    }}
                  >
                    Remove reviewer
                  </button>
                  <input
                    type="text"
                    className="add-card-input card-assign-input"
                    placeholder="reviewer login…"
                    value={reviewerInput}
                    onClick={(e) => e.stopPropagation()}
                    onChange={(e) => setReviewerInput(e.target.value)}
                    onKeyDown={(e) => {
                      e.stopPropagation();
                      if (e.key === "Enter" && reviewerInput.trim()) {
                        onSetReviewAssignee(card, reviewerInput.trim());
                        setReviewerInput("");
                        setAssignOpen(false);
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
