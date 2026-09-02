import { MAX_ZOOM, MIN_ZOOM, type Zoom } from "../projectZoom";

interface ZoomControlProps {
  zoom: Zoom;
  onChange: (z: Zoom) => void;
}

/** ZoomControl is the board's scale: one line with a handle on either side of
 *  it — above for column width, below for row height — each pointing at the
 *  line it rides. The reading beside them puts both back to 100%. Ctrl/Cmd +
 *  wheel over the board moves both at once, around the cursor. */
export function ZoomControl({ zoom, onChange }: ZoomControlProps) {
  const at100 = Math.abs(zoom.x - 1) < 0.005 && Math.abs(zoom.y - 1) < 0.005;
  const slider = (axis: "x" | "y", label: string) => (
    <input
      type="range"
      className={`zoom-slider zoom-slider-${axis}`}
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
    <div className="zoom-control" title="Ctrl/Cmd + scroll over the board zooms around the cursor">
      <div className="zoom-track">
        <div className="zoom-line" />
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
