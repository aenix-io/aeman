import { describe, expect, it } from "vitest";

import { makeCardPlacements, markOf, type CardPlacements } from "./placements";
import type { Card } from "./providers/types";
import { parkedLocally } from "./backlog";

/** card builds the slice of a card these rules read, TYPED. A cast through
 *  `unknown` asserts nothing about the shape it hides, and these cases are
 *  the rules' documentation. */
const mk = (c: Partial<Card>): Card => c as Card;

describe("makeCardPlacements", () => {
  const run = (card: Card, act: (p: CardPlacements) => void) => {
    const patches: Partial<Card>[] = [];
    const deps = {
      provider: {
        patchCard: () => Promise.resolve(undefined),
      },
      patchCard: (_: string, patch: Partial<Card>) => {
        patches.push(patch);
      },
      reload: () => {},
      onError: () => {},
      errMessage: () => "",
    };
    act(makeCardPlacements(card, deps));
    return patches[0];
  };

  // Parking from the assign menu is the same door as every other into the
  // backlog: out of work — nobody, no day, no sprint — and planned (K1, K3).
  it("parks the card out of work, the way every door into the backlog does", () => {
    const patch = run(mk({ itemId: "c1", assignees: ["kvaps"], startDate: "2026-09-07", sprintStart: "2026-09-07" }), (p) =>
      p.onPark?.(),
    );
    expect(patch).toEqual(parkedLocally());
  });

  // Attaching to a Project-board column was the menu's other section; the
  // Project board is gone (H15), and a placement offers parking alone.
  it("offers nothing but parking", () => {
    expect(Object.keys(makeCardPlacements(mk({ itemId: "c1", team: "alpha" }), {
      provider: { patchCard: () => Promise.resolve(undefined) },
      patchCard: () => {},
      reload: () => {},
      onError: () => {},
      errMessage: () => "",
    }))).toEqual(["onPark"]);
  });
});

describe("markOf", () => {
  // A review is what arrived from elsewhere — the one mark the stripe draws.
  it("marks a review, and nothing else", () => {
    expect(markOf({ reviewOf: "c1" })).toBe("review");
    expect(markOf({})).toBe("");
  });
});
