import { describe, expect, it } from "vitest";

import { makeCardPlacements, markClass, markLabel, markOf, markTitle, type CardPlacements } from "./placements";
import type { Card } from "./providers/types";
import { parkedLocally } from "./backlog";

/** card builds the slice of a card these rules read, TYPED. A cast through
 *  `unknown` asserts nothing about the shape it hides, and these cases are
 *  the rules' documentation. */
const mk = (c: Partial<Card>): Card => c as Card;

const REVIEW_MARK = "A review: somebody's work waiting on you";

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
  // The stripe down a card's left edge: a REVIEW (what arrived from
  // elsewhere), else the card's effective PRIORITY (K15) — never both: a
  // review keeps its yellow stripe whatever its priority.
  it("marks a review, else the effective priority, else nothing", () => {
    expect(markOf({ reviewOf: "c1" })).toBe("review");
    expect(markOf({ reviewOf: "c1", effectivePriority: "P0" })).toBe("review");
    expect(markOf({ effectivePriority: "P1" })).toBe("prio-p1");
    expect(markOf({ effectivePriority: "P4", effectivePriorityFrom: { uid: "e", title: "Billing" } })).toBe("prio-p4");
    expect(markOf({})).toBe("");
  });

  // The class a card wears for its stripe: the review's, or the priority's
  // hue — the full colour whether the priority is its own or inherited.
  it("names the stripe's classes, the same for an inherited priority", () => {
    expect(markClass({ reviewOf: "c1", effectivePriority: "P0" })).toBe(" card-review");
    expect(markClass({ effectivePriority: "P2" })).toBe(" card-prio card-prio-p2");
    expect(markClass({ effectivePriority: "P2", effectivePriorityFrom: { uid: "e", title: "Billing" } })).toBe(" card-prio card-prio-p2");
    expect(markClass({})).toBe("");
  });

  // The stripe says what it is to a pointer and a screen reader.
  it("titles a priority stripe, naming where an inherited one comes from", () => {
    expect(markTitle({ effectivePriority: "P1" })).toBe("P1");
    expect(markTitle({ effectivePriority: "P1", effectivePriorityFrom: { uid: "g", title: "Grow" } })).toBe("P1 (from «Grow»)");
    expect(markTitle({ reviewOf: "c1", effectivePriority: "P1" })).toBe(REVIEW_MARK);
    expect(markTitle({})).toBe("");
  });

  // A review's stripe says what it is too, and a screen reader hears the
  // stripe's kind before its title.
  it("names a stripe for a screen reader by its kind", () => {
    expect(markLabel({ effectivePriority: "P2" })).toBe("Priority P2");
    expect(markLabel({ reviewOf: "c1" })).toBe(REVIEW_MARK);
    expect(markLabel({})).toBe("");
  });
});
