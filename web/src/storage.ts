// localStorage housekeeping for the single-board UI. The server serves exactly
// one board now, so the per-board keys of the picker era (`<base>.<owner>/<n>`)
// collapse onto plain `<base>` keys; this module carries the one-time move so a
// returning user keeps the roster and filter they last saw.

/** StorageLike is the slice of the Web Storage API this module touches, so
 *  tests can hand in a plain map. */
export interface StorageLike {
  readonly length: number;
  key(index: number): string | null;
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
}

/** The picker era remembered the last-loaded board under these two keys. */
export const LS_LEGACY_OWNER = "aeman.owner";
export const LS_LEGACY_PROJECT = "aeman.project";
/** Set once the move has run — it must not repeat, or a later save under the
 *  plain key would be overwritten by stale scoped data. */
export const LS_SINGLE_BOARD_MARK = "aeman.singleBoard";

// scopedBoardKeys lists the `<owner>/<n>` suffixes that have saved state under
// any of the bases (a suffix without a slash is some other base's own key).
function scopedBoardKeys(storage: StorageLike, bases: readonly string[]): Set<string> {
  const found = new Set<string>();
  for (let i = 0; i < storage.length; i += 1) {
    const key = storage.key(i);
    if (!key) {
      continue;
    }
    for (const base of bases) {
      const prefix = `${base}.`;
      if (key.startsWith(prefix) && key.slice(prefix.length).includes("/")) {
        found.add(key.slice(prefix.length));
      }
    }
  }
  return found;
}

// legacyBoardKey picks the board whose saved state should carry over: the one
// the picker last loaded when that is recorded, else the only board that has
// any (a pinned deployment never wrote the pointer). Several boards and no
// pointer is ambiguous — better an empty start than another board's teams.
function legacyBoardKey(storage: StorageLike, bases: readonly string[]): string | null {
  const owner = storage.getItem(LS_LEGACY_OWNER);
  const project = storage.getItem(LS_LEGACY_PROJECT);
  if (owner && project) {
    return `${owner}/${project}`;
  }
  const boards = scopedBoardKeys(storage, bases);
  return boards.size === 1 ? [...boards][0] : null;
}

/** migrateBoardScopedKeys copies the last board's values of `bases` from their
 *  scoped keys onto the plain ones, once. A value already under a plain key is
 *  a pre-scoping leftover the scoped era ignored, so the scoped value wins; a
 *  base the board never saved is left as it is. Returns the board key the
 *  values came from, or null when nothing moved (already done, or no board). */
export function migrateBoardScopedKeys(
  storage: StorageLike,
  bases: readonly string[],
): string | null {
  try {
    if (storage.getItem(LS_SINGLE_BOARD_MARK)) {
      return null;
    }
    const board = legacyBoardKey(storage, bases);
    if (board) {
      for (const base of bases) {
        const value = storage.getItem(`${base}.${board}`);
        if (value !== null) {
          storage.setItem(base, value);
        }
      }
    }
    storage.setItem(LS_SINGLE_BOARD_MARK, "1");
    storage.removeItem(LS_LEGACY_OWNER);
    storage.removeItem(LS_LEGACY_PROJECT);
    return board;
  } catch {
    // A full or blocked store is not worth a broken board.
    return null;
  }
}
