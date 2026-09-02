import { MAX_ZOOM, MIN_ZOOM, type Zoom } from "../projectZoom";

interface ZoomControlProps {
  zoom: Zoom;
  onChange: (z: Zoom) => void;
  /** Whether the rows may grow to hold what stands in them. Left out by a
   *  board whose rows are all one height and always will be. */
  fit?: boolean;
  onFit?: (fit: boolean) => void;
}

/** ZoomControl is the board's scale: one line with a handle on either side of
 *  it — above for column width, below for row height — each pointing at the
 *  line it rides. The reading beside them puts both back to 100%. Ctrl/Cmd +
 *  wheel over the board moves both at once, around the cursor. */
export function ZoomControl({ zoom, onChange, fit, onFit }: ZoomControlProps) {
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
      {onFit && (
        // The row-height slider sets ONE height for every row. This says the
        // rows may differ instead: a week several cards share grows to hold
        // them side by side down the column, rather than slicing the column
        // into slivers. The slider then sets the smallest a row may be.
        <button
          type="button"
          className={`zoom-fit${fit ? " zoom-fit-on" : ""}`}
          aria-pressed={fit}
          onClick={() => onFit(!fit)}
          title={
            fit
              ? "Rows grow to hold what is in them — click for one height for every row"
              : "Every row is the same height — click to let a busy week grow instead"
          }
        >
          {fit ? "⇕" : "≡"}
        </button>
      )}
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
