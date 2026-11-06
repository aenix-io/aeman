// The cards a day board made while its Customer filter named SEVERAL
// customers (ownership.bornCustomers: with several chosen a new card is born
// for nobody, and the filter would hide it the moment it is typed in). The
// board keeps them shown until the filter changes; a changed filter is a new
// question, and they are forgotten.
import { useCallback, useState } from "react";

export interface Kept {
  /** The filter the ids were made under. */
  key: string;
  ids: ReadonlySet<string>;
}

/** keptAfter is the kept set with ids made under the filter `key` added; a
 *  different filter starts it over. */
export function keptAfter(kept: Kept, key: string, ids: readonly string[]): Kept {
  const base = kept.key === key ? [...kept.ids] : [];
  return { key, ids: new Set([...base, ...ids]) };
}

/** keptFor is what is kept under the filter `key`: nothing once it changed. */
export function keptFor(kept: Kept, key: string): ReadonlySet<string> {
  return kept.key === key ? kept.ids : new Set();
}

/** useKeptCreated holds the kept set for a board under its filter key. */
export function useKeptCreated(key: string): { kept: ReadonlySet<string>; keep: (...ids: string[]) => void } {
  const [state, setState] = useState<Kept>({ key, ids: new Set() });
  const keep = useCallback((...ids: string[]) => setState((s) => keptAfter(s, key, ids)), [key]);
  return { kept: keptFor(state, key), keep };
}
