// The progress bar with its handle: ten segments and a knob that drags on
// the 10% grid. It is one component because it is drawn in two places — on
// a card of the day boards and in the card's drawer — and a bar that
// snapped differently in the two would be the slider-wars bug again.
import { useRef, useState } from "react";

import { snapProgress } from "../progress";

export const SEGMENTS = 10;

/** displayedFill snaps a value to the segment count the bar draws: a started
 *  card is never read as empty and a sub-100 one never as full, so an
 *  off-grid value the slider cannot produce (3 or 95, set over the API)
 *  shows one segment and nine — only a true 0 and 100 hit the extremes. */
export function displayedFill(shown: number): number {
  const pct = shown <= 0 ? 0 : shown >= 100 ? 100 : Math.min(90, Math.max(10, Math.round(shown / 10) * 10));
  return pct / 10;
}

export interface ProgressSliderProps {
  /** The card's progress, already clamped for its stage. */
  value: number;
  /** A review/locked card: the handle reaches 10–90 only. */
  locked: boolean;
  /** The fill colour (the stage band's). */
  fill?: string;
  /** Tooltip over the bar; defaults to the percentage. */
  title?: string;
  /** No handle at all: a record, or progress somebody else derives. */
  readOnly?: boolean;
  /** Called once, on release, with the snapped value when it changed. */
  onCommit: (value: number) => void;
}

/** ProgressSlider draws the bar and moves progress by dragging the handle.
 *  The bar itself stays inert on purpose: on a card it runs the full width,
 *  and a stray press while dragging cards around would silently rewrite
 *  someone's progress. */
export function ProgressSlider({ value, locked, fill, title, readOnly, onCommit }: ProgressSliderProps) {
  const [dragValue, setDragValue] = useState<number | null>(null);
  const barRef = useRef<HTMLDivElement | null>(null);
  // While dragging the handle, show the snapped drag value; otherwise the card's.
  const shown = dragValue ?? value;
  const filled = displayedFill(shown);

  // Where a pointer at clientX lands on the 10% grid. A clamped stage reaches
  // only 10–90%; every other card spans 0–100% (0% clears). The reach is the
  // same rule as the value: a handle that drags past its own limit and snaps
  // back when the server answers is the bug this shares its answer to avoid.
  const valueAt = (clientX: number): number => {
    const rect = barRef.current?.getBoundingClientRect();
    if (!rect || rect.width <= 0) {
      return value;
    }
    return snapProgress((clientX - rect.left) / rect.width, locked);
  };

  const onHandleDown = (e: React.PointerEvent<HTMLDivElement>) => {
    e.stopPropagation();
    e.preventDefault();
    e.currentTarget.setPointerCapture(e.pointerId);
    setDragValue(value);
  };

  const onHandleMove = (e: React.PointerEvent<HTMLDivElement>) => {
    if (dragValue === null) {
      return;
    }
    const snapped = valueAt(e.clientX);
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
      onCommit(final);
    }
  };

  return (
    <div className="card-bar" ref={barRef} title={title ?? `${shown}%`}>
      {Array.from({ length: SEGMENTS }, (_, i) => (
        <span
          key={i}
          className={`card-seg${i < filled ? " card-seg-filled" : ""}`}
          style={i < filled ? { backgroundColor: fill } : undefined}
        />
      ))}
      {!readOnly && (
        <div
          className={`card-bar-handle${dragValue !== null ? " card-bar-handle-dragging" : ""}`}
          // Sit the handle on the last filled 10% boundary, not the raw value, so
          // an off-grid value (e.g. 95) doesn't leave it floating past the fill.
          style={{ left: `${filled * 10}%`, borderColor: fill }}
          role="slider"
          aria-label="Progress"
          aria-valuenow={shown}
          aria-valuemin={0}
          aria-valuemax={100}
          onClick={(e) => e.stopPropagation()}
          onPointerDown={onHandleDown}
          onPointerMove={onHandleMove}
          onPointerUp={onHandleUp}
          onPointerCancel={onHandleUp}
        />
      )}
    </div>
  );
}
