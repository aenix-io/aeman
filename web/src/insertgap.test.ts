import { describe, expect, it } from "vitest";

import { cellGaps, gapAnchor, gapsInZone } from "./insertgap";
import type { Card } from "./providers/types";

const card = (over: Partial<Card>): Card => ({ itemId: "c", title: "x", assignees: [], progress: 0, ...over }) as Card;

// THE INSERT "+", shared by the Backlog, the Team grid and the Me board: a
// list of cards has a gap above the first, between two and below the last,
// and a card typed into a gap names its place in the create — after the
// card above it, or before the card below it at the top.
describe("where a gap puts a card", () => {
  it("after the card above it", () => {
    expect(gapAnchor(["a", "b"], 1)).toEqual({ after: "a" });
    expect(gapAnchor(["a", "b"], 2)).toEqual({ after: "b" });
  });

  it("before the first card, at the top", () => {
    expect(gapAnchor(["a", "b"], 0)).toEqual({ before: "a" });
  });

  it("nowhere in particular in an empty list", () => {
    expect(gapAnchor([], 0)).toBeNull();
  });
});

// A subtask rides under its parent in a cell: there is no gap between a
// parent and its subtasks, nor between two subtasks — a card typed there
// would split the family. The gap after a parent is after its last subtask.
describe("which gaps a cell draws", () => {
  it("draws one above the first card and one after every card", () => {
    expect(cellGaps([card({ itemId: "a" }), card({ itemId: "b" })])).toEqual([0, 1, 2]);
  });

  it("draws none inside a parent's subtasks", () => {
    const parent = card({ itemId: "p" });
    const sub1 = card({ itemId: "s1", parent: "p" });
    const sub2 = card({ itemId: "s2", parent: "p" });
    const next = card({ itemId: "n" });
    expect(cellGaps([parent, sub1, sub2, next])).toEqual([0, 3, 4]);
  });

  it("draws one gap in an empty cell", () => {
    expect(cellGaps([])).toEqual([0]);
  });
});

// The Me board adds in ONE band, the unplanned one (meboard.acceptsNewCard):
// the server refuses any other zone on view=me, so the Me board draws no gap
// anywhere else. The Team grid draws them in every band.
describe("which bands have gaps", () => {
  it("is the unplanned band alone on the Me board", () => {
    expect(gapsInZone("me", "yellow")).toBe(true);
    for (const z of ["red", "gray", "green"] as const) {
      expect(gapsInZone("me", z)).toBe(false);
    }
  });

  it("is every band on the Team grid", () => {
    for (const z of ["red", "yellow", "gray", "green"] as const) {
      expect(gapsInZone("team", z)).toBe(true);
    }
  });
});
