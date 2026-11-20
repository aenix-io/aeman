interface FoldControlsProps {
  onExpand: () => void;
  onCollapse: () => void;
  /** What the buttons fold, said in their tooltips. */
  expandTitle?: string;
  collapseTitle?: string;
  className?: string;
}

/** FoldControls is Collapse all | Expand all as two icons — the Strategy
 *  board's rows, the Team and Me boards' plan groups or subtask lists. */
export function FoldControls({
  onExpand,
  onCollapse,
  expandTitle = "Expand all",
  collapseTitle = "Collapse all",
  className = "",
}: FoldControlsProps) {
  return (
    <div className={`segmented segmented-compact fold-controls ${className}`.trim()} role="group" aria-label="Folding">
      <button type="button" className="segment" title={collapseTitle} aria-label="Collapse all" onClick={onCollapse}>
        {/* Folded: three rows, each a closed triangle bullet (▸) — nothing
            nested. */}
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          <path d="M3 3.5v4.5l4-2.25z" fill="currentColor" stroke="none" />
          <path d="M10 5.75h11" />
          <path d="M3 9.75v4.5l4-2.25z" fill="currentColor" stroke="none" />
          <path d="M10 12h11" />
          <path d="M3 16v4.5l4-2.25z" fill="currentColor" stroke="none" />
          <path d="M10 18.25h11" />
        </svg>
      </button>
      <button type="button" className="segment" title={expandTitle} aria-label="Expand all" onClick={onExpand}>
        {/* Unfolded: three close rows, the first open (▾) and the two under
            it nested. */}
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          <path d="m2 5 3.5 3.5L9 5" />
          <path d="M11 6.5h10" />
          <path d="M12.5 12h8.5" />
          <path d="M12.5 17.5h8.5" />
        </svg>
      </button>
    </div>
  );
}
