import { useEffect, useState, type ReactNode } from "react";
import type { Card as CardModel } from "../providers/types";
import { ZONES } from "../zones";
import { ProgressSlider } from "./ProgressSlider";
import { useDebouncedCallback } from "../hooks/useDebouncedCallback";

interface CardProps {
  card: CardModel;
  mode: "ford" | "nixon";
  onProgress: (card: CardModel, value: number) => void;
  /** Extra controls rendered in the card footer (used by the Nixon board). */
  zoneSelect?: ReactNode;
  daySelect?: ReactNode;
}

/** Card renders a single project item, shared by the Ford and Nixon boards. */
export function Card({ card, mode, onProgress, zoneSelect, daySelect }: CardProps) {
  const [progress, setProgress] = useState<number>(card.progress ?? 0);

  // Keep local progress in sync when the card changes underneath us.
  useEffect(() => {
    setProgress(card.progress ?? 0);
  }, [card.progress]);

  const pushProgress = useDebouncedCallback((value: number) => {
    onProgress(card, value);
  }, 400);

  const handleProgress = (value: number) => {
    setProgress(value);
    pushProgress(value);
  };

  const handleDragStart = (e: React.DragEvent<HTMLDivElement>) => {
    e.dataTransfer.setData("text/plain", card.itemId);
    e.dataTransfer.effectAllowed = "move";
  };

  return (
    <div
      className="card"
      draggable={mode === "ford"}
      onDragStart={mode === "ford" ? handleDragStart : undefined}
    >
      <div className="card-title">
        {card.url ? (
          <a href={card.url} target="_blank" rel="noreferrer">
            {card.title}
          </a>
        ) : (
          card.title
        )}
      </div>

      <div className="card-progress" title={`${progress}% ready`}>
        <div
          className="card-progress-fill"
          style={{
            width: `${progress}%`,
            backgroundColor: ZONES.green.accent,
          }}
        />
      </div>
      <div className="card-progress-label">{progress}%</div>

      <div className="card-meta">
        {card.repository && (
          <span className="pill">
            {card.repository}
            {card.number !== undefined ? ` #${card.number}` : ""}
          </span>
        )}
        {card.isDraft && <span className="badge badge-draft">draft</span>}
        {card.assignees.map((a) => (
          <span className="chip" key={a}>
            @{a}
          </span>
        ))}
      </div>

      <div className="card-controls">
        <ProgressSlider value={progress} onChange={handleProgress} />
        {(zoneSelect || daySelect) && (
          <div className="card-controls-row">
            {zoneSelect}
            {daySelect}
          </div>
        )}
      </div>
    </div>
  );
}
