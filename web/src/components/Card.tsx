import type { Card as CardModel } from "../providers/types";
import { ZONES } from "../zones";

interface CardProps {
  card: CardModel;
  me: string;
  selected: boolean;
  onSelect: (card: CardModel) => void;
  onProgress: (card: CardModel, value: number) => void;
  onDelete: (card: CardModel) => void;
  draggable?: boolean;
}

/** initials reduces a login to one or two uppercase characters for an avatar. */
function initials(login: string): string {
  const parts = login.split(/[-_.\s]+/).filter(Boolean);
  if (parts.length >= 2) {
    return (parts[0][0] + parts[1][0]).toUpperCase();
  }
  const clean = login.replace(/[^A-Za-z0-9]/g, "");
  return (clean.slice(0, 2) || login.slice(0, 2)).toUpperCase();
}

/** ticket renders the monospace ticket reference for a card with a number. */
function ticket(card: CardModel): string | null {
  if (card.number === undefined) {
    return null;
  }
  return card.repository ? `${card.repository}#${card.number}` : `#${card.number}`;
}

/** Card is a compact single-row item shared by the Me and Team boards. */
export function Card({
  card,
  me,
  selected,
  onSelect,
  onProgress,
  onDelete,
  draggable = true,
}: CardProps) {
  const progress = card.progress ?? 0;
  const ref = ticket(card);

  const handleDragStart = (e: React.DragEvent<HTMLDivElement>) => {
    e.dataTransfer.setData("text/plain", card.itemId);
    e.dataTransfer.effectAllowed = "move";
  };

  const handleBarClick = (e: React.MouseEvent<HTMLDivElement>) => {
    e.stopPropagation();
    const rect = e.currentTarget.getBoundingClientRect();
    const fraction = rect.width > 0 ? (e.clientX - rect.left) / rect.width : 0;
    const clamped = Math.min(1, Math.max(0, fraction));
    const value = Math.round((clamped * 100) / 5) * 5;
    onProgress(card, value);
  };

  const handleDelete = (e: React.MouseEvent<HTMLButtonElement>) => {
    e.stopPropagation();
    if (window.confirm(`Delete "${card.title}"?`)) {
      onDelete(card);
    }
  };

  return (
    <div
      className={`card${selected ? " card-selected" : ""}`}
      draggable={draggable}
      onDragStart={draggable ? handleDragStart : undefined}
      onClick={() => onSelect(card)}
      title={card.title}
    >
      <button
        type="button"
        className="card-delete"
        onClick={handleDelete}
        aria-label="Delete card"
      >
        ×
      </button>

      <span className="card-glyph" aria-hidden="true">
        {card.isDraft ? "▦" : "#"}
      </span>

      <span className="card-title">{card.title}</span>

      {ref && <span className="card-ticket">{ref}</span>}

      <span className="card-avatars">
        {card.assignees.map((login) => (
          <span
            key={login}
            className={`avatar${login === me ? " avatar-me" : ""}`}
            title={login}
          >
            {initials(login)}
          </span>
        ))}
      </span>

      <div
        className="card-bar"
        title={`${progress}% — click to set`}
        onClick={handleBarClick}
      >
        <div
          className="card-bar-fill"
          style={{ width: `${progress}%`, backgroundColor: ZONES.green.accent }}
        />
      </div>
    </div>
  );
}
