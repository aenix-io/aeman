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

/** points is the weight of a size: 1, 2, 4, 8 — and 0 for a card nobody
 *  sized, which is honest: the board cannot say what it does not know. */
export function points(size: SizeKey | undefined | ""): number {
  return size ? SIZES[size].points : 0;
}

/** sizeFromWire reads the letter the API sends; anything else is unsized. */
export function sizeFromWire(raw: string | undefined): SizeKey | undefined {
  return raw === "S" || raw === "M" || raw === "L" || raw === "XL" ? raw : undefined;
}

/** pointsOf is what a card WEIGHS on a board: its own size, or — when it
 *  has subtasks somebody has sized — the sum of theirs. The umbrella rule,
 *  dynamic on purpose: umbrellas are not born as umbrellas, so the parent's
 *  estimate stands until its children exist and is replaced by their sum
 *  once they do, and the total never counts twice. Children nobody sized
 *  yet leave the parent's own size standing. Mirrors board.PointsOf. */
export function pointsOf(
  cards: readonly Pick<Card, "itemId" | "parent" | "size">[],
  card: Pick<Card, "itemId" | "size">,
): number {
  let sum = 0;
  let sized = false;
  for (const k of cards) {
    if (k.parent === card.itemId && k.size) {
      sum += points(k.size);
      sized = true;
    }
  }
  return sized ? sum : points(card.size);
}
