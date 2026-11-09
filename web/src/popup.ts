/** Rect is the part of a DOMRect a popup is placed by. */
export interface Rect {
  left: number;
  top: number;
  right: number;
  bottom: number;
}

export interface PopupPlacement {
  top: number;
  left: number;
  /** The most the popup may be tall where it opened; its list scrolls past it. */
  maxHeight: number;
  placement: "below" | "above";
}

export interface PopupOptions {
  /** "start" lines the popup's left edge up with the control's; "end" (the
   *  default) its right edge with the control's. */
  align?: "start" | "end";
  /** A box the popup must stay inside as well as the viewport — the drawer
   *  it was opened from. */
  bounds?: Rect;
  gap?: number;
  margin?: number;
}

/** anchorShown reports whether a popup's control is laid out on the screen.
 *  A control taken out of the layout (display: none — the card's hover-only
 *  status flag once the pointer leaves the card for the menu) measures as an
 *  empty box at 0,0, and a popup placed by it lands in the top-left corner;
 *  such a measurement is no place to move a popup to. */
export function anchorShown(r: Rect & { width: number; height: number }): boolean {
  return r.width > 0 || r.height > 0;
}

/** placePopup is where a popup opens for the control that was pressed: under
 *  it, or above it when the room below is short and above there is more;
 *  never taller than the room it opened into (the list scrolls instead), and
 *  never past the viewport's — or the bounds' — left and right edges. A
 *  popup wider than the room is pinned to its start, so its beginning, where
 *  the search field is, stays in view. */
export function placePopup(
  anchor: Rect,
  size: { width: number; height: number },
  viewport: { width: number; height: number },
  opts: PopupOptions = {},
): PopupPlacement {
  const gap = opts.gap ?? 4;
  const margin = opts.margin ?? 8;
  const minX = Math.max(margin, (opts.bounds?.left ?? -Infinity) + margin);
  const maxX = Math.min(viewport.width - margin, (opts.bounds?.right ?? Infinity) - margin);

  const wanted = opts.align === "start" ? anchor.left : anchor.right - size.width;
  const left = Math.max(minX, Math.min(wanted, maxX - size.width));

  const below = viewport.height - margin - (anchor.bottom + gap);
  const above = anchor.top - gap - margin;
  if (size.height > below && above > below) {
    const maxHeight = Math.max(0, above);
    const height = Math.min(size.height, maxHeight);
    return { top: anchor.top - gap - height, left, maxHeight, placement: "above" };
  }
  return { top: anchor.bottom + gap, left, maxHeight: Math.max(0, below), placement: "below" };
}
