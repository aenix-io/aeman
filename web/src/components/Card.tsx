import { useEffect, useRef, useState } from "react";
import type { Card as CardModel, StageKey } from "../providers/types";
import { STAGES, STAGE_ORDER, DEFAULT_BAR_COLOR } from "../stages";
import { initials, teamColor } from "../avatar";

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
}: CardProps) {
  const value = card.stage === "done" ? 100 : card.progress ?? 0;
  const fill = barColor(card.stage);
  const ref = ticket(card);

  const [menuOpen, setMenuOpen] = useState(false);
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(card.title);
  const [dragValue, setDragValue] = useState<number | null>(null);
  const menuRef = useRef<HTMLDivElement | null>(null);
  const barRef = useRef<HTMLDivElement | null>(null);

  // While dragging the handle, show the snapped drag value; otherwise the card's.
  const shown = dragValue ?? value;
  const filled = Math.round(shown / 10);

  // Close the stage menu on any outside click.
  useEffect(() => {
    if (!menuOpen) {
      return;
    }
    const onDocClick = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        setMenuOpen(false);
      }
    };
    document.addEventListener("mousedown", onDocClick);
    return () => document.removeEventListener("mousedown", onDocClick);
  }, [menuOpen]);

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
          {menuOpen && (
            <div className="card-stage-menu" onClick={(e) => e.stopPropagation()}>
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
            </div>
          )}
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

      {card.team && (
        <span
          className="team-avatar"
          style={{ backgroundColor: teamColor(card.team) }}
          title={card.team}
        >
          {initials(card.team)}
        </span>
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
