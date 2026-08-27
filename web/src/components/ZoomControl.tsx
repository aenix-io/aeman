import { MAX_ZOOM, MIN_ZOOM, type Zoom } from "../projectZoom";

interface ZoomControlProps {
  zoom: Zoom;
  onChange: (z: Zoom) => void;
}

/** ZoomControl is the board's scale, as two thin sliders stacked around a
 *  reading of the current size: the top one widens the columns, the bottom one
 *  heightens the rows, and the button between puts both back to 100%. The same
 *  scale answers Ctrl/Cmd + wheel over the board, which moves both at once. */
export function ZoomControl({ zoom, onChange }: ZoomControlProps) {
  const at100 = Math.abs(zoom.x - 1) < 0.005 && Math.abs(zoom.y - 1) < 0.005;
  const slider = (
    axis: "x" | "y",
    label: string,
  ) => (
    <input
      type="range"
      className="zoom-slider"
      min={MIN_ZOOM}
      max={MAX_ZOOM}
      step={0.05}
      value={zoom[axis]}
      aria-label={label}
      title={`${label} — ${Math.round(zoom[axis] * 100)}%`}
      onChange={(e) => onChange({ ...zoom, [axis]: Number(e.target.value) })}
    />
  );

  return (
    <div className="zoom-control" title="Ctrl/Cmd + scroll over the board zooms both">
      <div className="zoom-sliders">
        {slider("x", "Column width")}
        {slider("y", "Row height")}
      </div>
      <button
        type="button"
        className="zoom-reset"
        onClick={() => onChange({ x: 1, y: 1 })}
        disabled={at100}
        title="Back to 100%"
      >
        {Math.round(((zoom.x + zoom.y) / 2) * 100)}%
      </button>
    </div>
  );
}
