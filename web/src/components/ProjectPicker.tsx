import { useRef, useState } from "react";
import { teamColor, teamInitial } from "../avatar";
import { Dropdown } from "./Dropdown";

interface ProjectPickerProps {
  /** The project the thing belongs to now ("" = the no-project bucket). */
  current: string;
  /** Every project the menu may offer, in board order. */
  projects: string[];
  /** What is being filed — "epic" or "process" — for the badge's tooltip. */
  entity: string;
  onPick: (project: string) => void;
}

/** The round badge a column or a process wears for its project, and the menu
 *  behind it. The badge is the affordance: what it shows is what the thing
 *  belongs to, and clicking it is how that is changed — the same gesture as a
 *  card's team badge, so there is one way to re-file anything on the board. */
export function ProjectPicker({
  current,
  projects,
  entity,
  onPick,
}: ProjectPickerProps) {
  const [open, setOpen] = useState(false);
  const anchor = useRef<HTMLElement | null>(null);
  return (
    <>
      <button
        type="button"
        className={`project-epic-avatar project-epic-avatar-pick${current ? "" : " project-epic-avatar-none"}`}
        style={current ? { background: teamColor(current) } : undefined}
        title={
          current
            ? `In ${current} — click to move this ${entity}`
            : `In no project — click to file this ${entity}`
        }
        ref={(el) => {
          anchor.current = el;
        }}
        onClick={(e) => {
          // The badge sits in a header that expands, drags or renames on a
          // click; re-filing is none of those.
          e.stopPropagation();
          setOpen((v) => !v);
        }}
        onPointerDown={(e) => e.stopPropagation()}
        onDoubleClick={(e) => e.stopPropagation()}
      >
        {current ? teamInitial(current) : "?"}
      </button>
      <Dropdown
        open={open}
        anchorRef={anchor}
        onClose={() => setOpen(false)}
        className="card-stage-menu"
      >
        {projects.map((p) => (
          <button
            key={p}
            type="button"
            className={`card-stage-item${p === current ? " card-stage-item-active" : ""}`}
            onClick={(e) => {
              e.stopPropagation();
              setOpen(false);
              onPick(p);
            }}
          >
            <span className="card-stage-dot" style={{ background: teamColor(p) }} />
            {p}
          </button>
        ))}
        <button
          type="button"
          className={`card-stage-item${current ? "" : " card-stage-item-active"}`}
          onClick={(e) => {
            e.stopPropagation();
            setOpen(false);
            onPick("");
          }}
        >
          <span className="card-stage-dot" style={{ background: "var(--line)" }} />
          No project
        </button>
      </Dropdown>
    </>
  );
}
