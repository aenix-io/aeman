import { describe, expect, it } from "vitest";

import { clickOn, presenceAfter, selectedByOf } from "./presence";

// WHO HAS A CARD SELECTED (#290): one rule for every board that draws the
// marks — Team, Me and Backlog. A login's selection is the card uid it has
// selected; a collapsed parent wears its subtasks' marks, an expanded one
// leaves them on the subtask rows.
describe("who has a card selected", () => {
  // Keyed by the TAB (the client) a selection is from, as the server keys it.
  const presence = {
    t1: { login: "ann", card: "a" },
    t2: { login: "bob", card: "s1" },
    t3: { login: "cat", card: "b" },
  };

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

  // Two tabs of one person are two selections: closing one does not erase
  // the other's mark, and one person on a card through two tabs is one
  // mark. The reader's own other tabs are not somebody else's mark.
  it("keeps two tabs of one person apart, draws them once, and never draws the reader", () => {
    const two = { t1: { login: "ann", card: "a" }, t2: { login: "ann", card: "a" }, t3: { login: "me", card: "a" } };
    expect(selectedByOf(two, { itemId: "a" }, undefined, "me")).toEqual(["ann"]);
    const { t1: _gone, ...one } = two;
    expect(selectedByOf(one, { itemId: "a" }, undefined, "me")).toEqual(["ann"]);
  });
});

// A presence frame lands in the map by its tab: a selection sets that tab's
// entry, an empty card clears that tab's alone. A frame from a server that
// names no tab is keyed by its login, as before.
describe("a presence frame", () => {
  it("sets and clears one tab's selection", () => {
    let map = presenceAfter({}, { login: "ann", card: "a", client: "t1" });
    map = presenceAfter(map, { login: "ann", card: "b", client: "t2" });
    expect(Object.keys(map).sort()).toEqual(["t1", "t2"]);
    map = presenceAfter(map, { login: "ann", card: "", client: "t1" });
    expect(map).toEqual({ t2: { login: "ann", card: "b" } });
  });

  it("is keyed by its login when it names no tab", () => {
    expect(presenceAfter({}, { login: "ann", card: "a" })).toEqual({ ann: { login: "ann", card: "a" } });
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
