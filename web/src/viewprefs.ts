// The reader's view preferences the APP holds for a board and draws in its
// top bar: the Strategy board's depth range and zoom, the Backlog's Tasks |
// Epics mode. Each is the viewer's own, remembered in this browser, and
// none of them writes anything to the board — switching one changes what
// is drawn, never the order.
import { DEFAULT_DEPTH, LEVELS, type DepthRange, type StrategyKind, type Zoom } from "./strategy";
import { BACKLOG_MODE_KEY, readBacklogMode, type BacklogMode } from "./backlogboard";

export const LS_DEPTH = "aeman.strategy.depth";
export const LS_ZOOM = "aeman.strategy.zoom";

/** parseDepth is a remembered depth range, or the default for anything else. */
export function parseDepth(raw: string | null): DepthRange {
  try {
    if (raw) {
      const parsed = JSON.parse(raw) as Partial<DepthRange>;
      if (LEVELS.includes(parsed.from as StrategyKind) && LEVELS.includes(parsed.to as StrategyKind)) {
        return { from: parsed.from as StrategyKind, to: parsed.to as StrategyKind };
      }
    }
  } catch {
    // a value from another build: the default
  }
  return DEFAULT_DEPTH;
}

/** parseZoom is a remembered zoom, or three months. */
export function parseZoom(raw: string | null): Zoom {
  return raw === "3m" || raw === "1y" || raw === "fit" ? raw : "3m";
}

function read(key: string): string | null {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

function write(key: string, value: string): void {
  try {
    localStorage.setItem(key, value);
  } catch {
    // a preference, not state
  }
}

export const readDepth = (): DepthRange => parseDepth(read(LS_DEPTH));
export const storeDepth = (d: DepthRange): void => write(LS_DEPTH, JSON.stringify(d));
export const readZoom = (): Zoom => parseZoom(read(LS_ZOOM));
export const storeZoom = (z: Zoom): void => write(LS_ZOOM, z);
export const readMode = (): BacklogMode => readBacklogMode(read(BACKLOG_MODE_KEY));
export const storeMode = (m: BacklogMode): void => write(BACKLOG_MODE_KEY, m);
