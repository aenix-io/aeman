import { addDays, todayIso } from "../date";

interface DayNavProps {
  selectedDate: string;
  onSelectDate: (day: string) => void;
  /** The way back to the sprint being worked, when there is one to go to:
   *  the day to land on and which side of the current one it lies. Null —
   *  no sprint, or the board is already standing on its day — draws no
   *  button at all, an arrow pointing nowhere being worse than none. */
  sprintJump?: { target: string; dir: "back" | "fwd" } | null;
}

/** DayNav is the day the board is looking at: a step back, the date itself, a
 *  step forward, and the way back to today.
 *
 *  It lives in the app's own toolbar rather than in each board's, because the
 *  day is the APP's state (App owns selectedDate) and every board that has one
 *  reads the same value — two copies of the control drifted in their labels
 *  and their disabled rules, and the day moved under a board that was not
 *  showing the control at all. */
export function DayNav({ selectedDate, onSelectDate, sprintJump }: DayNavProps) {
  const today = todayIso();
  return (
    <div className="day-nav">
      <button
        type="button"
        className="day-arrow"
        onClick={() => onSelectDate(addDays(selectedDate, -1))}
        aria-label="Previous day"
        title="Previous day"
      >
        ‹
      </button>
      <input
        type="date"
        value={selectedDate}
        aria-label="Day"
        onChange={(e) => onSelectDate(e.target.value || today)}
      />
      <button
        type="button"
        className="day-arrow"
        onClick={() => onSelectDate(addDays(selectedDate, 1))}
        aria-label="Next day"
        title="Next day"
      >
        ›
      </button>
      <button
        type="button"
        className="btn day-today"
        onClick={() => onSelectDate(today)}
        disabled={selectedDate === today}
        title="Jump to today"
      >
        Today
      </button>
      {sprintJump && (
        <button
          type="button"
          className="btn day-sprint"
          onClick={() => onSelectDate(sprintJump.target)}
          title="Jump to the current sprint's day"
        >
          {sprintJump.dir === "fwd" ? "Current sprint »" : "« Current sprint"}
        </button>
      )}
    </div>
  );
}
