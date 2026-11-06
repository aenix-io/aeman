import { describe, expect, it } from "vitest";

import { keptAfter, keptFor, type Kept } from "./keptcreated";

// The cards a board made while its Customer filter named several customers
// stay shown until the filter changes (ownership.bornCustomers): a change of
// the filter forgets them, since the board is then answering a new question.
describe("the cards a board keeps shown under its filter", () => {
  const none: Kept = { key: "cu1,cu2", ids: new Set() };

  it("adds what the board makes under the standing filter", () => {
    const kept = keptAfter(keptAfter(none, "cu1,cu2", ["tmp-1"]), "cu1,cu2", ["real-1"]);
    expect([...keptFor(kept, "cu1,cu2")]).toEqual(["tmp-1", "real-1"]);
  });

  it("forgets them when the filter changes", () => {
    const kept = keptAfter(none, "cu1,cu2", ["tmp-1"]);
    expect(keptFor(kept, "cu1").size).toBe(0);
    expect([...keptFor(keptAfter(kept, "cu1", ["x"]), "cu1")]).toEqual(["x"]);
  });
});
