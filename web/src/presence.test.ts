import { describe, expect, it } from "vitest";

import { clickOn, selectedByOf } from "./presence";

// WHO HAS A CARD SELECTED (#290): one rule for every board that draws the
// marks — Team, Me and Backlog. A login's selection is the card uid it has
// selected; a collapsed parent wears its subtasks' marks, an expanded one
// leaves them on the subtask rows.
describe("who has a card selected", () => {
  const presence = { ann: "a", bob: "s1", cat: "b" };

  it("is every login whose selection is the card", () => {
    expect(selectedByOf(presence, { itemId: "a" })).toEqual(["ann"]);
    expect(selectedByOf(presence, { itemId: "z" })).toBeUndefined();
  });

  it("puts a subtask's mark on its collapsed parent, and on the subtask when it is open", () => {
    const kids = { p: [{ itemId: "s1" }] };
    expect(selectedByOf(presence, { itemId: "p" }, { children: (id) => kids[id as "p"] ?? [], open: () => false })).toEqual(["bob"]);
    expect(selectedByOf(presence, { itemId: "p" }, { children: (id) => kids[id as "p"] ?? [], open: () => true })).toBeUndefined();
  });

  it("is nobody without presence", () => {
    expect(selectedByOf(undefined, { itemId: "a" })).toBeUndefined();
  });
});

// THE FIRST CLICK SELECTS, THE SECOND OPENS (#290, as the owner changed
// it): a click on a card that is not selected selects it — and the
// selection is what the others see — and a click on the selected card
// opens it.
describe("a click on a card", () => {
  it("selects a card that is not selected", () => {
    expect(clickOn(false)).toBe("select");
  });

  it("opens the card that is already selected", () => {
    expect(clickOn(true)).toBe("open");
  });
});
