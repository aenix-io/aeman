import type { ProjectField, ZoneKey } from "./providers/types";

export interface ZoneDef {
  key: ZoneKey;
  title: string;
  description: string;
  accent: string;
  background: string;
  /** GitHub single-select option colours that map onto this zone. */
  ghColors: string[];
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
    description: "Regular, planned work",
    accent: "#8b949e",
    background: "#f3f4f6",
    ghColors: ["GRAY"],
  },
  green: {
    key: "green",
    title: "If time left",
    description: "Start only when every other zone is clear",
    accent: "#3fb950",
    background: "#eafaef",
    ghColors: ["GREEN"],
  },
  yellow: {
    key: "yellow",
    title: "Unplanned",
    description: "Popped up unplanned during the day",
    accent: "#d4a72c",
    background: "#fdf6df",
    ghColors: ["YELLOW", "ORANGE"],
  },
  red: {
    key: "red",
    title: "Critical today",
    description: "Must be resolved before the end of the day",
    accent: "#f85149",
    background: "#fdecea",
    ghColors: ["RED", "PINK"],
  },
};

// Display order of the colour areas, top to bottom: critical first, then
// unplanned, planned and finally "if time left" at the bottom (matching the
// Ford screenshots, where green sits at the bottom).
export const ZONE_ORDER: ZoneKey[] = ["red", "yellow", "gray", "green"];

const COLOR_TO_ZONE = new Map<string, ZoneKey>();
for (const zone of Object.values(ZONES)) {
  for (const color of zone.ghColors) {
    COLOR_TO_ZONE.set(color.toUpperCase(), zone.key);
  }
}

/** zoneFromColor maps a GitHub single-select option colour onto a zone. */
export function zoneFromColor(color?: string): ZoneKey | undefined {
  if (!color) {
    return undefined;
  }
  return COLOR_TO_ZONE.get(color.toUpperCase());
}

/** optionIdForZone finds the option id in a single-select field for a zone. */
export function optionIdForZone(
  field: ProjectField | undefined,
  zone: ZoneKey,
): string | undefined {
  if (!field?.options) {
    return undefined;
  }
  const wanted = ZONES[zone].ghColors.map((c) => c.toUpperCase());
  const match = field.options.find((o) => wanted.includes(o.color.toUpperCase()));
  return match?.id;
}
