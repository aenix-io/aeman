// The board-specific controls that stand in the app's top bar, beside the
// view tabs — where the Me and Team boards keep their day — rather than in a
// board's own toolbar, which keeps the filters. Each is drawn only on its
// own tab; the values are the app's (viewprefs.ts).
import {
  KIND_LABEL,
  LEVELS,
  type DepthRange,
  type StrategyKind,
  type Zoom,
} from "../strategy";
import type { BacklogMode } from "../backlogboard";

/** StrategyControls is the Strategy board's Hierarchy range and zoom. */
export function StrategyControls({
  depth,
  onDepth,
  zoom,
  onZoom,
  range,
}: {
  depth: DepthRange;
  onDepth: (d: DepthRange) => void;
  zoom: Zoom;
  onZoom: (z: Zoom) => void;
  /** The window the timeline shows, the board's answer to the zoom. */
  range?: string;
}) {
  const options = LEVELS.map((k) => (
    <option key={k} value={k}>
      {KIND_LABEL[k]}
    </option>
  ));
  return (
    <>
    <div className="topbar-controls">
      <label className="strategy-depth">
        <span className="strategy-depth-label">Hierarchy</span>
        <select
          value={depth.from}
          onChange={(e) =>
            onDepth({ ...depth, from: e.target.value as StrategyKind })
          }
          aria-label="From level"
        >
          {options}
        </select>
        <span className="strategy-depth-to">to</span>
        <select
          value={depth.to}
          onChange={(e) =>
            onDepth({ ...depth, to: e.target.value as StrategyKind })
          }
          aria-label="To level"
        >
          {options}
        </select>
      </label>
      {range && (
        <span className="strategy-range" title="The window the timeline shows">
          {range}
        </span>
      )}
    </div>
    {/* The zoom stands apart, in the middle of the bar. */}
    <div className="topbar-center">
        <div className="segmented strategy-zoom" role="group" aria-label="Zoom">
        {(["3m", "1y", "fit"] as Zoom[]).map((z) => (
          <button
            key={z}
            type="button"
            className={`segment${zoom === z ? " segment-active" : ""}`}
            onClick={() => onZoom(z)}
          >
            {z === "3m" ? "3M" : z === "1y" ? "1Y" : "Fit"}
          </button>
        ))}
        </div>
    </div>
    </>
  );
}

/** BacklogModeSwitch is the Backlog's Tasks | Epics: one order, two
 *  readings, and switching writes nothing. */
export function BacklogModeSwitch({
  mode,
  onMode,
}: {
  mode: BacklogMode;
  onMode: (m: BacklogMode) => void;
}) {
  return (
    <div className="topbar-controls">
      <div className="segmented backlog-mode" role="group" aria-label="Show">
        {(["tasks", "epics"] as const).map((m) => (
          <button
            key={m}
            type="button"
            className={`segment${mode === m ? " segment-active" : ""}`}
            aria-pressed={mode === m}
            onClick={() => onMode(m)}
          >
            {m === "tasks" ? "Tasks" : "Epics"}
          </button>
        ))}
      </div>
    </div>
  );
}
