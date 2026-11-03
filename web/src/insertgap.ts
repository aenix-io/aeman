// The insert "+" shared by the Backlog, the Team grid and the Me board: where
// a list of cards has gaps, and where a card typed into one is placed. The
// place rides the create itself (after / before), so the server puts the
// card there in the same request (boardservice.CreateCard).

import type { Anchor } from "./backlogboard";
import { acceptsNewCard } from "./meboard";
import type { Card, ZoneKey } from "./providers/types";
import { isSubtask } from "./subtasks";

/** gapAnchor is the place a card typed into gap `at` of a list names: after
 *  the card above it, or before the card below it at the top; nothing in an
 *  empty list. 0 is above the first card, ids.length below the last. */
export function gapAnchor(ids: readonly string[], at: number): Anchor {
  if (at > 0 && at <= ids.length) {
    return { after: ids[at - 1] };
  }
  if (at === 0 && ids.length > 0) {
    return { before: ids[0] };
  }
  return null;
}

/** cellGaps is the gaps a cell of a day board draws: one above the first
 *  card and one after every card — except where the next card is a subtask,
 *  which rides under its parent: a card typed between a parent and its
 *  subtasks would split the family. */
export function cellGaps(cards: readonly Pick<Card, "parent" | "lineage">[]): number[] {
  const out = [0];
  cards.forEach((_, i) => {
    const next = cards[i + 1];
    if (!next || !isSubtask(next)) {
      out.push(i + 1);
    }
  });
  return out;
}

/** gapsInZone reports whether a band of a day board has gaps: every band
 *  on the Team grid; on the Me board only the band it adds in, the
 *  unplanned one — the server refuses any other there. */
export function gapsInZone(board: "me" | "team", zone: ZoneKey): boolean {
  return board === "team" || acceptsNewCard(zone);
}
