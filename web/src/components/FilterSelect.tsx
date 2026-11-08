import { useRef, useState } from "react";
import { Dropdown } from "./Dropdown";

export interface FilterOption {
  value: string;
  label: string;
}

/** filterLabel is what the button says: the chosen values by their labels,
 *  or `anyLabel` when nothing narrows the board — or the board's quick
 *  choice by its own name when the selection is exactly the set it picks
 *  ("Status: Hide done", not the four values it stands for). */
export function filterLabel(
  label: string,
  options: readonly FilterOption[],
  selected: readonly string[],
  anyLabel = "anyone",
  quick?: Pick<QuickChoice, "label" | "values">,
): string {
  if (quick?.values && isQuick(selected, quick.values)) {
    return `${label}: ${quick.label}`;
  }
  const names = selected.map((v) => options.find((o) => o.value === v)?.label ?? v);
  return `${label}: ${names.length ? names.join(", ") : anyLabel}`;
}

/** toggled is the selection after one value's checkbox is pressed. */
export function toggled(selected: readonly string[], v: string): string[] {
  return selected.includes(v) ? selected.filter((x) => x !== v) : [...selected, v];
}

/** QuickChoice is a board's one-click shortcut — the Strategy board's "Hide
 *  done" — offered at the foot of its filter's menu in the place and the
 *  look of the way back to "all" while nothing is chosen; once something is,
 *  "all" stands there instead. */
export interface QuickChoice {
  label: string;
  /** values is the selection the choice makes, when it makes one: the
   *  button then names such a selection by the choice's label. */
  values?: readonly string[];
  onClick: () => void;
}

/** isQuick reports a selection that is exactly a quick choice's set, in
 *  any order. */
function isQuick(selected: readonly string[], values: readonly string[]): boolean {
  return selected.length === values.length && values.every((v) => selected.includes(v));
}

/** pressQuick applies a quick choice as the one-shot it is: the menu closes
 *  first — focus back on its button, and no second press on the same spot
 *  (now "all") can undo it straight away — then the choice is made. */
export function pressQuick(quick: QuickChoice, onClose: () => void): void {
  onClose();
  quick.onClick();
}

/** FilterSelectMenu is the open menu: a checkbox per option (several are
 *  OR), then at its foot the way back to "any" when something is chosen —
 *  or, while nothing is, a board's quick choice where it offers one — and the roster's "Manage…" entry at
 *  the bottom where the board offers one. */
export function FilterSelectMenu({
  options,
  selected,
  anyLabel = "anyone",
  onChange,
  manage,
  quick,
  onClose,
}: {
  options: readonly FilterOption[];
  selected: readonly string[];
  anyLabel?: string;
  onChange: (next: string[]) => void;
  manage?: { label: string; onClick: () => void };
  quick?: QuickChoice;
  onClose: () => void;
}) {
  return (
    <>
      {options.length === 0 && <span className="card-stage-item filter-menu-hint">Nothing to choose yet</span>}
      {options.map((o) => {
        const on = selected.includes(o.value);
        return (
          <button
            type="button"
            key={o.value}
            role="menuitemcheckbox"
            aria-checked={on}
            className={`card-stage-item filter-select-item${o.value === "" ? " filter-select-none" : ""}`}
            onClick={() => onChange(toggled(selected, o.value))}
          >
            <span className={`filter-select-check${on ? " filter-select-check-on" : ""}`} aria-hidden="true">
              {on ? "✓" : ""}
            </span>
            {o.label}
          </button>
        );
      })}
      {quick && selected.length === 0 && (
        <button type="button" role="menuitem" className="card-stage-item card-stage-clear filter-select-quick" onClick={() => pressQuick(quick, onClose)}>
          {quick.label}
        </button>
      )}
      {selected.length > 0 && (
        <button type="button" role="menuitem" className="card-stage-item card-stage-clear" onClick={() => onChange([])}>
          {anyLabel.charAt(0).toUpperCase() + anyLabel.slice(1)}
        </button>
      )}
      {manage && (
        <button
          type="button"
          role="menuitem"
          className="card-stage-item filter-select-manage"
          onClick={() => {
            onClose();
            manage.onClick();
          }}
        >
          {manage.label}
        </button>
      )}
    </>
  );
}

/** FilterSelect is the one filter dropdown of every board's toolbar —
 *  Team, Customer, Owner (ownership.md, O7, O8, O11): a button
 *  saying what is chosen ("Team: portal, No team", or `anyLabel`), opening
 *  a list to tick several — several are OR — with the roster's "Manage…"
 *  entry at the bottom where the board offers one. An empty selection is no
 *  filter. */
export function FilterSelect({
  label,
  options,
  selected,
  onChange,
  anyLabel = "anyone",
  manage,
  quick,
  title,
}: {
  label: string;
  options: readonly FilterOption[];
  selected: readonly string[];
  onChange: (next: string[]) => void;
  anyLabel?: string;
  manage?: { label: string; onClick: () => void };
  quick?: QuickChoice;
  title?: string;
}) {
  const [open, setOpen] = useState(false);
  const anchor = useRef<HTMLSpanElement | null>(null);
  return (
    <span className="filter-select" ref={anchor}>
      <button
        type="button"
        className={`btn filter-select-btn${selected.length ? " filter-select-on" : ""}`}
        aria-label={`${label} filter`}
        aria-haspopup="menu"
        aria-expanded={open}
        // What is chosen, in full, once something is — the button may cut it.
        title={selected.length ? filterLabel(label, options, selected, anyLabel, quick) : (title ?? `Show only what these ${label.toLowerCase()}s answer for or carry`)}
        onClick={() => setOpen((o) => !o)}
      >
        {filterLabel(label, options, selected, anyLabel, quick)}
      </button>
      <Dropdown open={open} anchorRef={anchor} onClose={() => setOpen(false)} className="card-stage-menu filter-select-menu" role="menu">
        <FilterSelectMenu
          options={options}
          selected={selected}
          anyLabel={anyLabel}
          onChange={onChange}
          manage={manage}
          quick={quick}
          onClose={() => {
            setOpen(false);
            // The menu lives in a portal: closing it from inside would drop
            // the focus on <body>. It goes back to the button.
            anchor.current?.querySelector<HTMLElement>("button")?.focus();
          }}
        />
      </Dropdown>
    </span>
  );
}
