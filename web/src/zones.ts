import type { ZoneKey } from "./providers/types";

export interface ZoneDef {
  key: ZoneKey;
  title: string;
  /** Short uppercase label shown rotated on the zone's left spine. */
  spine: string;
  description: string;
  accent: string;
  background: string;
}

// Zone semantics follow Flant's Ford:
//   gray   - regular, planned work
//   green  - start only when every other zone is clear
//   yellow - popped up unplanned during the day
//   red    - must be resolved before the end of the working day
export const ZONES: Record<ZoneKey, ZoneDef> = {
  gray: {
    key: "gray",
    title: "Planned",
    spine: "PLANNED",
    description: "Regular, planned work",
    accent: "#8b949e",
    background: "#f3f4f6",
  },
  green: {
    key: "green",
    title: "If time left",
    spine: "NICE TO HAVE",
    description: "Start only when every other zone is clear",
    accent: "#3fb950",
    background: "#eafaef",
  },
  yellow: {
    key: "yellow",
    title: "Unplanned",
    spine: "UNPLANNED",
    description: "Popped up unplanned during the day",
    accent: "#d4a72c",
    background: "#fdf6df",
  },
  red: {
    key: "red",
    title: "Critical today",
    spine: "URGENT",
    description: "Must be resolved before the end of the day",
    accent: "#f85149",
    background: "#fdecea",
  },
};

// Display order of the colour areas, top to bottom: critical first, then
// unplanned, planned and finally "if time left" at the bottom (matching the
// Ford screenshots, where green sits at the bottom).
export const ZONE_ORDER: ZoneKey[] = ["red", "yellow", "gray", "green"];
