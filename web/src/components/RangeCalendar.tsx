import { useState } from "react";
import { todayIso } from "../date";

const WEEKDAYS = ["Mo", "Tu", "We", "Th", "Fr", "Sa", "Su"];
const MONTHS = [
  "January",
  "February",
  "March",
  "April",
  "May",
  "June",
  "July",
  "August",
  "September",
  "October",
  "November",
  "December",
];

function ymd(y: number, m: number, d: number): string {
  return `${y}-${String(m + 1).padStart(2, "0")}-${String(d).padStart(2, "0")}`;
}

interface RangeCalendarProps {
  start: string | null;
  end: string | null;
  onChange: (start: string | null, end: string | null) => void;
}

/**
 * RangeCalendar is an inline month grid for picking a start..end range the way
 * flight pickers do: first click sets the start, the next sets the end.
 */
export function RangeCalendar({ start, end, onChange }: RangeCalendarProps) {
  const [iy, im] = (start ?? end ?? todayIso()).split("-").map(Number);
  const [view, setView] = useState<{ y: number; m: number }>({ y: iy, m: im - 1 });

  const pick = (day: string) => {
    if (!start || (start && end) || day < start) {
      onChange(day, null);
    } else {
      onChange(start, day);
    }
  };

  const shift = (delta: number) =>
    setView((v) => {
      const d = new Date(v.y, v.m + delta, 1);
      return { y: d.getFullYear(), m: d.getMonth() };
    });

  const firstWeekday = (new Date(view.y, view.m, 1).getDay() + 6) % 7; // Mon = 0
  const daysInMonth = new Date(view.y, view.m + 1, 0).getDate();
  const cells: (number | null)[] = [];
  for (let i = 0; i < firstWeekday; i += 1) {
    cells.push(null);
  }
  for (let d = 1; d <= daysInMonth; d += 1) {
    cells.push(d);
  }

  return (
    <div
      className="rcal"
      onClick={(e) => e.stopPropagation()}
      onDoubleClick={(e) => e.stopPropagation()}
    >
      <div className="rcal-head">
        <button
          type="button"
          className="rcal-nav"
          onClick={() => shift(-1)}
          aria-label="Previous month"
        >
          ‹
        </button>
        <span className="rcal-title">
          {MONTHS[view.m]} {view.y}
        </span>
        <button
          type="button"
          className="rcal-nav"
          onClick={() => shift(1)}
          aria-label="Next month"
        >
          ›
        </button>
      </div>
      <div className="rcal-grid">
        {WEEKDAYS.map((w) => (
          <span key={w} className="rcal-wd">
            {w}
          </span>
        ))}
        {cells.map((d, i) => {
          if (d === null) {
            return <span key={`e${i}`} className="rcal-empty" />;
          }
          const cur = ymd(view.y, view.m, d);
          const isStart = cur === start;
          const isEnd = cur === end;
          const inRange = Boolean(start && end && cur > start && cur < end);
          const cls = [
            "rcal-day",
            isStart ? "rcal-start" : "",
            isEnd ? "rcal-end" : "",
            inRange ? "rcal-in" : "",
          ]
            .filter(Boolean)
            .join(" ");
          return (
            <button key={`d${d}`} type="button" className={cls} onClick={() => pick(cur)}>
              {d}
            </button>
          );
        })}
      </div>
    </div>
  );
}
