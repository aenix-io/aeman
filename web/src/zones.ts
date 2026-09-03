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
//
// Colours are CSS custom properties (defined in styles.css) rather than literal
// hex so the opt-in colour-blind mode can repaint them via a single palette
// override without any component change. accent drives spine/border; background
// tints the area.
export const ZONES: Record<ZoneKey, ZoneDef> = {
  gray: {
    key: "gray",
    title: "Planned",
    spine: "PLANNED",
    description: "Regular, planned work",
    accent: "var(--zone-gray-accent)",
    background: "var(--zone-gray-bg)",
  },
  green: {
    key: "green",
    title: "If time left",
    spine: "NICE TO HAVE",
    description: "Start only when every other zone is clear",
    accent: "var(--zone-green-accent)",
    background: "var(--zone-green-bg)",
  },
  yellow: {
    key: "yellow",
    title: "Unplanned",
    spine: "UNPLANNED",
    description: "Popped up unplanned during the day",
    accent: "var(--zone-yellow-accent)",
    background: "var(--zone-yellow-bg)",
  },
  red: {
    key: "red",
    title: "Critical",
    spine: "URGENT",
    description: "Must be resolved before the end of the day",
    accent: "var(--zone-red-accent)",
    background: "var(--zone-red-bg)",
  },
};

// Display order of the colour areas, top to bottom: critical first, then
// unplanned, planned and finally "if time left" at the bottom (matching the
// Ford screenshots, where green sits at the bottom).
export const ZONE_ORDER: ZoneKey[] = ["red", "yellow", "gray", "green"];
