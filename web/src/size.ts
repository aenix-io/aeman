// A card's SIZE — what somebody said it weighs — and the points a board sums
// from it. Mirrors pkg/board/size.go: the letter is stored, the points are
// derived, and a parent with sized subtasks weighs what its children weigh.
import type { Card, SizeKey } from "./providers/types";

/** The four sizes smallest first — the order a picker shows them. */
export const SIZE_ORDER: SizeKey[] = ["S", "M", "L", "XL"];

/** What each size means, in the words the rubric uses. */
export const SIZES: Record<SizeKey, { points: number; hint: string }> = {
  S: { points: 1, hint: "up to a couple of hours: one action in one place" },
  M: { points: 2, hint: "half a day to a day: one deliverable in one component" },
  L: { points: 4, hint: "two to five days: several parts or people" },
  XL: { points: 8, hint: "a week or more: epic-shaped, or an umbrella with subtasks" },
};

/** DEFAULT_SIZE is what a card nobody has sized WEIGHS on a board.
 *
 *  Not nothing. A board that weighs unsized work as zero tells a person their
 *  week is empty while they are drowning in it, and the number climbs as the
 *  cards are sized — which reads as the sizing having caused the load. M is
 *  the board's own middle: of 2194 sized cards 40% are S, 34% M, 23% L, 1%
 *  XL, so the median card is M and the mean 2.14 points, and assuming M costs
 *  less than any other guess. Mirrors board.DefaultSize. */
export const DEFAULT_SIZE: SizeKey = "M";

/** points is the weight of a size on the SCALE: 1, 2, 4, 8, and 0 for the
 *  empty size. What a CARD weighs is pointsOf, which applies the default. */
export function points(size: SizeKey | undefined | ""): number {
  return size ? SIZES[size].points : 0;
}

/** weigh is one card's own weight: its size, or — unsized — what its KIND
 *  usually costs. A REVIEW card weighs S: a review is somebody reading
 *  finished work and saying yes or no, the rubric calls it S by definition,
 *  and it is the one kind of card the board makes on its own, one for every
 *  card sent to review — weighing those as M put two points on a reviewer for
 *  each thing they were asked to look at. A review somebody DID size keeps
 *  that size. Mirrors board.weigh. */
function weigh(card: Pick<Card, "size" | "reviewOf">): number {
  if (card.size) {
    return points(card.size);
  }
  return points(card.reviewOf ? "S" : DEFAULT_SIZE);
}

/** sizeFromWire reads the letter the API sends; anything else is unsized. */
export function sizeFromWire(raw: string | undefined): SizeKey | undefined {
  return raw === "S" || raw === "M" || raw === "L" || raw === "XL" ? raw : undefined;
}

/** pointsOf is what a card WEIGHS on a board: its own size, or — once it has
 *  subtasks — the sum of theirs. The umbrella rule, dynamic on purpose:
 *  umbrellas are not born as umbrellas, so the parent's estimate stands until
 *  its children exist and is replaced by them once they do, and the total
 *  never counts twice. An unsized card, parent or child, weighs what its kind
 *  costs — the default, or S for a review.
 *  Mirrors board.PointsOf. */
export function pointsOf(
  cards: readonly Pick<Card, "itemId" | "parent" | "size" | "reviewOf">[],
  card: Pick<Card, "itemId" | "size" | "reviewOf">,
): number {
  let sum = 0;
  let has = false;
  for (const k of cards) {
    if (k.parent === card.itemId) {
      sum += weigh(k);
      has = true;
    }
  }
  return has ? sum : weigh(card);
}
