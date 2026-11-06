import { useRef, useState } from "react";
import { Dropdown } from "./Dropdown";

/** FilterSelect is one of the Roadmap's filters (ownership.md, O7, O8): a
 *  button saying what is chosen ("Team: portal, ops", or "anyone"), opening
 *  a list to tick several — several are OR. */
export function FilterSelect({
  label,
  options,
  selected,
  onChange,
}: {
  label: string;
  options: readonly { value: string; label: string }[];
  selected: readonly string[];
  onChange: (next: string[]) => void;
}) {
  const [open, setOpen] = useState(false);
  const anchor = useRef<HTMLSpanElement | null>(null);
  const names = selected.map((v) => options.find((o) => o.value === v)?.label ?? v);
  const toggle = (v: string) =>
    onChange(selected.includes(v) ? selected.filter((x) => x !== v) : [...selected, v]);
  return (
    <span className="filter-select" ref={anchor}>
      <button
        type="button"
        className={`btn filter-select-btn${selected.length ? " filter-select-on" : ""}`}
        aria-label={`${label} filter`}
        aria-haspopup="menu"
        aria-expanded={open}
        title={`Show only what these ${label.toLowerCase()}s answer for or carry`}
        onClick={() => setOpen((o) => !o)}
      >
        {label}: {names.length ? names.join(", ") : "anyone"}
      </button>
      <Dropdown open={open} anchorRef={anchor} onClose={() => setOpen(false)} className="card-stage-menu filter-select-menu">
        {options.length === 0 && <span className="card-stage-item project-menu-hint">Nothing to choose yet</span>}
        {options.map((o) => (
          <label key={o.value} className="card-stage-item filter-select-item">
            <input type="checkbox" checked={selected.includes(o.value)} onChange={() => toggle(o.value)} />
            {o.label}
          </label>
        ))}
        {selected.length > 0 && (
          <button type="button" className="card-stage-item card-stage-clear" onClick={() => onChange([])}>
            Anyone
          </button>
        )}
      </Dropdown>
    </span>
  );
}
