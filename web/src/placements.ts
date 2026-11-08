// Placements: what a card's assign menu may do with where the card stands —
// put it on its own team's backlog — and the mark its left stripe draws. The
// server is the authority; everything here exists so the UI offers only what
// the server would accept, and mirrors its outcomes optimistically.

import { parkable, parkedLocally } from "./backlog";
import type { Card, CardPatch } from "./providers/types";

/** CardPlacements is everything the assign menu needs to re-place one card —
 *  callbacks landing on the provider. Absent means the board offers no
 *  placement editing. */
export interface CardPlacements {
  /** Put the card on its own team's backlog. Absent on a card that has no
   *  place of its own to be parked in (a review card, a subtask). */
  onPark?: () => void;
}

/** PlacementDeps is what makeCardPlacements needs from a board component:
 *  its provider slice, its optimistic cache and its error/refresh plumbing. */
export interface PlacementDeps {
  provider: {
    patchCard(uid: string, patch: CardPatch): Promise<unknown>;
  };
  patchCard(itemId: string, patch: Partial<Card>): void;
  reload(): void;
  onError(message: string): void;
  errMessage(err: unknown): string;
}

/** makeCardPlacements builds the assign menu's placement section for one
 *  card: callbacks that mirror the server's outcome optimistically and
 *  converge on the re-list. One factory for every board, so the Me and Team
 *  boards cannot drift apart — the ×'s history shows where per-board copies
 *  of one rule end up. */
export function makeCardPlacements(card: Card, deps: PlacementDeps): CardPlacements {
  const call = (p: Promise<unknown>) => {
    void p.then(() => deps.reload()).catch((err: unknown) => {
      deps.onError(deps.errMessage(err));
      deps.reload();
    });
  };
  return {
    // Parking is not a reassignment: the backlog is the card's own team's,
    // and every team has one — so there is nothing to pick and nothing to
    // check. It is the same door as every other into the backlog: out of
    // work, nobody, planned (K1, K3).
    onPark: parkable(card)
      ? () => {
          deps.patchCard(card.itemId, parkedLocally());
          call(deps.provider.patchCard(card.itemId, { parked: true }));
        }
      : undefined,
  };
}

/** CardMark is what a card's left stripe says about where the work came FROM:
 *  a REVIEW waiting on this person — or nothing, for the day board's own
 *  work. The stripe is worth a glance precisely because it marks what
 *  arrived from elsewhere. */
export type CardMark = "review" | "";

/** markOf answers it once, for every board that draws the mark: the day
 *  boards down a card's left edge (Card.tsx: .card-review). Two boards
 *  deciding this separately is how one of them came to draw no mark at all
 *  after the weekly plan — whose band the stripe used to be — was taken
 *  out. */
export function markOf(c: Pick<Card, "reviewOf">): CardMark {
  return c.reviewOf ? "review" : "";
}
