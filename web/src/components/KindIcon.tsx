import { KIND_LABEL, type RoadmapKind } from "../roadmap";

interface KindIconProps {
  kind: RoadmapKind;
  /** Pixels; the glyphs are drawn on a 16-unit grid. */
  size?: number;
  className?: string;
  /** Spoken by a screen reader; absent, the icon is decoration beside a
   *  title that already says what the card is. */
  label?: boolean;
}

/** KindIcon is the pictogram a card's kind wears wherever the hierarchy is
 *  drawn — a Roadmap row, a breadcrumb, a parent picker, a child row — one
 *  shape per level, readable at 14px, coloured by the kind's own class:
 *
 *  - goal: a target (what the company is aiming at);
 *  - initiative: a flag (a bet planted on the map);
 *  - epic: a stack of layers (a body of work);
 *  - task: a ticked box (the day boards' own card);
 *  - subtask: a branch off its task.
 *
 *  The same mark everywhere is the point: the drawer's crumb and the
 *  Roadmap's row must read as the same thing. */
export function KindIcon({ kind, size = 14, className, label }: KindIconProps) {
  const cls = `kind-icon kind-icon-${kind}${className ? ` ${className}` : ""}`;
  const common = {
    width: size,
    height: size,
    viewBox: "0 0 16 16",
    fill: "none",
    stroke: "currentColor",
    strokeWidth: 1.6,
    strokeLinecap: "round" as const,
    strokeLinejoin: "round" as const,
    className: cls,
    role: label ? "img" : undefined,
    "aria-label": label ? KIND_LABEL[kind] : undefined,
    "aria-hidden": label ? undefined : true,
  };
  switch (kind) {
    case "goal":
      return (
        <svg {...common}>
          <circle cx="8" cy="8" r="6.2" />
          <circle cx="8" cy="8" r="3" />
          <circle cx="8" cy="8" r="0.9" fill="currentColor" stroke="none" />
        </svg>
      );
    case "initiative":
      return (
        <svg {...common}>
          <path d="M4 14V2.5" />
          <path d="M4 3h7.5l-1.8 2.6 1.8 2.6H4z" fill="currentColor" fillOpacity="0.25" />
        </svg>
      );
    case "epic":
      return (
        <svg {...common}>
          <path d="M8 2.5 14 5.5 8 8.5 2 5.5z" fill="currentColor" fillOpacity="0.25" />
          <path d="M2 8.5l6 3 6-3" />
          <path d="M2 11.5l6 3 6-3" />
        </svg>
      );
    case "task":
      return (
        <svg {...common}>
          <rect x="2.5" y="2.5" width="11" height="11" rx="2.2" />
          <path d="M5.2 8.2l2 2 3.8-4.2" />
        </svg>
      );
    case "subtask":
      return (
        <svg {...common}>
          <path d="M4 2.5v6.5a2 2 0 0 0 2 2h6.5" />
          <path d="M10 8.5 12.5 11 10 13.5" />
        </svg>
      );
  }
}
