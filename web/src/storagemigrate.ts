// The Roadmap became the Strategy board (its name in the UI, the API and the
// address). What a reader chose on it — its filters, its column widths, how
// deep it opens, its zoom, what is folded — was kept under aeman.roadmap.*;
// those keys move to aeman.strategy.* once, so nobody loses a choice, and
// the remembered tab's old name is written under its new one.

/** KeyStore is the slice of localStorage a move needs. */
export interface KeyStore {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
}

/** STRATEGY_KEY_MOVES are the Strategy board's keys under their old names
 *  and their new ones. */
export const STRATEGY_KEY_MOVES: readonly (readonly [string, string])[] = [
  ["aeman.roadmap.filters", "aeman.strategy.filters"],
  ["aeman.roadmap.columns", "aeman.strategy.columns"],
  ["aeman.roadmap.depth", "aeman.strategy.depth"],
  ["aeman.roadmap.zoom", "aeman.strategy.zoom"],
  ["aeman.roadmap.folded", "aeman.strategy.folded"],
];

const LS_VIEW = "aeman.view";

/** migrateKeys moves each old key to its new name: copied when the new one is
 *  not there yet (a choice made under the new name wins), the old one taken
 *  off either way. Idempotent: a second run finds nothing to move. The
 *  remembered tab named "roadmap" is written as "strategy". */
export function migrateKeys(s: KeyStore, moves: readonly (readonly [string, string])[]): void {
  for (const [from, to] of moves) {
    const old = s.getItem(from);
    if (old === null) {
      continue;
    }
    if (s.getItem(to) === null) {
      s.setItem(to, old);
    }
    s.removeItem(from);
  }
  if (s.getItem(LS_VIEW) === "roadmap") {
    s.setItem(LS_VIEW, "strategy");
  }
}

/** migrateStorage runs the move against the browser's storage, once per
 *  load, before anything reads a key; a browser that refuses storage keeps
 *  working without it. */
export function migrateStorage(): void {
  try {
    migrateKeys(window.localStorage, STRATEGY_KEY_MOVES);
  } catch {
    // Storage disabled: nothing to move.
  }
}
