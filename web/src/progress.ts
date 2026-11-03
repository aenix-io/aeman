/** Progress-bar geometry shared by the card slider and its tests. */

/** Snap a fraction of the bar's width (0..1, may fall outside when the pointer
 *  runs past either end) to the 10% grid the slider works in. A review/locked
 *  card is clamped to 10–90 so it never reads as empty or complete. */
export function snapProgress(fraction: number, locked: boolean): number {
  const min = locked ? 10 : 0;
  const max = locked ? 90 : 100;
  return Math.min(max, Math.max(min, Math.round(fraction * 10) * 10));
}

/** stepProgress is a keyboard step on the progress bar: an arrow moves it by
 *  10 on the grid the handle snaps to (right and up add, left and down take
 *  away), Home and End go to its ends — 10 and 90 for a review/locked card,
 *  as the handle's reach is. Any other key is no step (null). */
export function stepProgress(value: number, key: string, locked: boolean): number | null {
  const min = locked ? 10 : 0;
  const max = locked ? 90 : 100;
  const onGrid = Math.round(value / 10) * 10;
  let next: number;
  switch (key) {
    case "ArrowRight":
    case "ArrowUp":
      next = onGrid + 10;
      break;
    case "ArrowLeft":
    case "ArrowDown":
      next = onGrid - 10;
      break;
    case "Home":
      next = min;
      break;
    case "End":
      next = max;
      break;
    default:
      return null;
  }
  return Math.min(max, Math.max(min, next));
}
