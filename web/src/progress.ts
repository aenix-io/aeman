/** Progress-bar geometry shared by the card slider and its tests. */

/** Snap a fraction of the bar's width (0..1, may fall outside when the pointer
 *  runs past either end) to the 10% grid the slider works in. A review/locked
 *  card is clamped to 10–90 so it never reads as empty or complete. */
export function snapProgress(fraction: number, locked: boolean): number {
  const min = locked ? 10 : 0;
  const max = locked ? 90 : 100;
  return Math.min(max, Math.max(min, Math.round(fraction * 10) * 10));
}
