// Placements: what a card's assign menu may do with where the card stands —
// put it on its own team's backlog — and the mark its left stripe draws. The
// server is the authority; everything here exists so the UI offers only what
// the server would accept, and mirrors its outcomes optimistically.

import { parkable, parkedLocally } from "./backlog";
import type { Card, CardPatch, Priority } from "./providers/types";

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

/** CardMark is what a card's left stripe says: a REVIEW waiting on this
 *  person — what arrived from elsewhere — else the card's effective PRIORITY
 *  (K15), in its hue; or nothing. Never two stripes: a review keeps its
 *  yellow one whatever its priority. */
export type CardMark = "review" | `prio-${Lowercase<Priority>}` | "";

type Marked = Pick<Card, "reviewOf" | "effectivePriority" | "effectivePriorityFrom">;

/** markOf answers it once, for every board that draws the mark: the day
 *  boards and the Backlog down a card's left edge (Card.tsx, BacklogBoard).
 *  Two boards deciding this separately is how one of them came to draw no
 *  mark at all after the weekly plan — whose band the stripe used to be —
 *  was taken out. */
export function markOf(c: Partial<Marked>): CardMark {
  if (c.reviewOf) {
    return "review";
  }
  return c.effectivePriority ? (`prio-${c.effectivePriority.toLowerCase()}` as CardMark) : "";
}

/** markClass is the classes a card wears for its stripe, with a leading
 *  space: the review's, or the priority's hue — always the full colour, own
 *  or inherited (the drawer's and the Roadmap's badges draw that
 *  difference; the stripe does not). */
export function markClass(c: Partial<Marked>): string {
  const mark = markOf(c);
  if (mark === "review") {
    return " card-review";
  }
  if (!mark) {
    return "";
  }
  return ` card-prio card-${mark}`;
}

/** reviewMark is what a review's stripe says: somebody else's work waiting
 *  on the person whose board it is. */
const reviewMark = "A review: somebody's work waiting on you";

/** markTitle is what a card's stripe says to a pointer: "P1", or
 *  "P1 (from «Goal»)" when inherited; a review's stripe says it is one;
 *  nothing on a card with neither. */
export function markTitle(c: Partial<Marked>): string {
  if (c.reviewOf) {
    return reviewMark;
  }
  if (!c.effectivePriority) {
    return "";
  }
  return c.effectivePriorityFrom ? `${c.effectivePriority} (from «${c.effectivePriorityFrom.title}»)` : c.effectivePriority;
}

/** markLabel names the stripe for a screen reader: its kind, then its
 *  title — "Priority P1 (from «Goal»)", or the review's own sentence. */
export function markLabel(c: Partial<Marked>): string {
  const title = markTitle(c);
  if (!title || c.reviewOf) {
    return title;
  }
  return `Priority ${title}`;
}
