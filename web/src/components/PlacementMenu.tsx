import { useState } from "react";

import type { ProjectTargets } from "../placements";

interface PlacementMenuProps {
  /** The row's label — "Attach to project…", "Mirror to…". */
  label: string;
  /** Project → epics targets; a click on a project unfolds its columns. */
  targets?: ProjectTargets[];
  /** Flat mode — processes have no columns, so the list is one level. */
  flat?: string[];
  /** Picking a column calls with (project, epic); flat mode with (item, ""). */
  onPick: (project: string, epic: string) => void;
}

/** PlacementMenu is the attach/mirror section of a card's assign menu: a
 *  row that unfolds IN PLACE into the projects of the card's repository,
 *  each unfolding into its epic columns. In place, not as a flyout: the
 *  assign menu is a portal that closes on any pointer-down outside its own
 *  DOM, so a nested portal would dismiss it — and an accordion inside a
 *  scrollable menu can never leave the screen, which a flyout chained off
 *  a flyout regularly does. */
export function PlacementMenu({ label, targets, flat, onPick }: PlacementMenuProps) {
  const [open, setOpen] = useState(false);
  const [project, setProject] = useState<string | null>(null);
  const items = flat ?? [];
  const cols = targets ?? [];
  if (cols.length === 0 && items.length === 0) {
    return null;
  }
  return (
    <div className="card-placements">
      <button
        type="button"
        className={`card-stage-item card-placements-head${open ? " card-placements-open" : ""}`}
        onClick={() => setOpen((v) => !v)}
      >
        <span className="card-placements-caret">{open ? "▾" : "▸"}</span>
        {label}
      </button>
      {open && (
        <div className="card-placements-list">
          {items.map((p) => (
            <button
              key={p}
              type="button"
              className="card-stage-item card-placements-leaf"
              onClick={() => onPick(p, "")}
            >
              {p}
            </button>
          ))}
          {cols.map((p) => (
            <div key={p.name}>
              <button
                type="button"
                className="card-stage-item card-placements-project"
                onClick={() => setProject((cur) => (cur === p.name ? null : p.name))}
              >
                <span className="card-placements-caret">
                  {project === p.name ? "▾" : "▸"}
                </span>
                {p.name}
              </button>
              {project === p.name &&
                p.epics.map((e) => (
                  <button
                    key={e}
                    type="button"
                    className="card-stage-item card-placements-leaf"
                    onClick={() => onPick(p.name, e)}
                  >
                    {e}
                  </button>
                ))}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
