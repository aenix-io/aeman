// The number over a person's column on the TRIAGE board: the points they are
// carrying against the points a week they get through — "21/40" — red past
// it, and the load alone while nobody has set a capacity. Whole across every
// team whatever filter the board is read through, because the server computes
// both (metadata.members). Clicking it sets the capacity: a lead knows a
// person is on a half week, or new, or covering for two.
import { useState } from "react";
import type { Member } from "../users";
import { loadLabel, loadState } from "../load";

interface PersonLoadProps {
  member: Member | undefined;
  /** Set the person's capacity (0 takes a set number back). Absent = read-only. */
  onSetCapacity?: (points: number) => void;
}

export function PersonLoad({ member, onSetCapacity }: PersonLoadProps) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState("");
  const load = member?.load ?? 0;
  const capacity = member?.capacity;
  const state = loadState(load, capacity);
  const title =
    state === "unknown"
      ? `${load} points carried; nobody has set a capacity — click to set one`
      : `${load} of ${capacity} points a week (click to change, 0 takes it back)${state === "over" ? " — more than fits the week" : ""}`;

  const commit = () => {
    setEditing(false);
    const n = Number.parseInt(draft, 10);
    if (Number.isNaN(n) || n < 0 || n === (capacity ?? 0)) {
      return;
    }
    onSetCapacity?.(n);
  };

  if (editing) {
    return (
      <input
        className="person-load-input"
        type="number"
        min={0}
        max={999}
        value={draft}
        autoFocus
        aria-label="Points a week"
        onChange={(e) => setDraft(e.target.value)}
        onBlur={commit}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            commit();
          } else if (e.key === "Escape") {
            setEditing(false);
          }
        }}
        onClick={(e) => e.stopPropagation()}
      />
    );
  }
  return (
    <button
      type="button"
      className={`person-load person-load-${state}`}
      title={title}
      disabled={!onSetCapacity}
      onClick={(e) => {
        e.stopPropagation();
        setDraft(capacity ? String(capacity) : "");
        setEditing(true);
      }}
    >
      {loadLabel(load, capacity)}
    </button>
  );
}
