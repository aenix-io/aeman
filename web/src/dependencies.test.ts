import { describe, expect, it } from "vitest";
import type { Card } from "./providers/types";
import { blocksOf, dependencyOptions, dependencyRows, lockTitle, withDependency, withoutDependency } from "./dependencies";

const card = (c: Partial<Card> & { itemId: string }): Card => ({ title: c.itemId, assignees: [], ...c });

// goal > epic > task t1 (with subtask s1); a loose task t2; a review r of t2;
// another epic e2 with a task t3.
const goal = card({ itemId: "goal", type: "goal" });
const epic = card({ itemId: "epic", type: "epic", parent: "goal", lineage: [{ uid: "goal", type: "goal", title: "goal" }] });
const t1 = card({ itemId: "t1", parent: "epic" });
const s1 = card({ itemId: "s1", parent: "t1" });
const t2 = card({ itemId: "t2", title: "Billing API" });
const r = card({ itemId: "r", reviewOf: "t2" });
const e2 = card({ itemId: "e2", type: "epic" });
const t3 = card({ itemId: "t3", parent: "e2", dependsOn: ["t1"] });
const all = [goal, epic, t1, s1, t2, r, e2, t3];

describe("the dependency picker", () => {
  it("offers any card but a review, the card itself, its ancestors, its descendants and those already listed", () => {
    const self = { ...t1, dependsOn: ["t2"] };
    const offered = dependencyOptions(all, self, "").map((o) => o.card.itemId);
    expect(offered).toEqual(["e2", "t3"]);
  });

  it("does not offer a plan item the work under it, nor the work its plan item", () => {
    expect(dependencyOptions(all, epic, "").map((o) => o.card.itemId)).toEqual(["t2", "e2", "t3"]);
  });

  it("finds a card by its own title or an ancestor's, and gives its chain", () => {
    const found = dependencyOptions(all, e2, "goal");
    expect(found.map((o) => o.card.itemId)).toEqual(["goal", "epic", "t1", "s1"]);
    expect(found[2].chain.map((c) => c.itemId)).toEqual(["goal", "epic"]);
    expect(dependencyOptions(all, e2, "billing").map((o) => o.card.itemId)).toEqual(["t2"]);
  });

  it("offers nothing to a review card, which neither waits nor is waited for", () => {
    expect(dependencyOptions(all, r, "")).toEqual([]);
  });
});

describe("the lock's tooltip", () => {
  it("names the blockers it may show and counts the hidden ones", () => {
    expect(lockTitle(card({ itemId: "x" }))).toBeNull();
    expect(
      lockTitle(card({ itemId: "x", locked: true, blockedBy: [{ uid: "a", title: "A" }, { uid: "b", title: "B" }] })),
    ).toBe("waiting for «A», «B»");
    expect(lockTitle(card({ itemId: "x", locked: true, blockedBy: [{ uid: "a", title: "A" }], hiddenBlockers: 2 }))).toBe(
      "waiting for «A» and 2 hidden",
    );
    expect(lockTitle(card({ itemId: "x", locked: true, hiddenBlockers: 1 }))).toBe("waiting for 1 hidden card");
    expect(lockTitle(card({ itemId: "x", locked: true, hiddenBlockers: 3 }))).toBe("waiting for 3 hidden cards");
  });

  it("says which ancestor an inherited wait comes from", () => {
    expect(
      lockTitle(
        card({ itemId: "x", locked: true, blockedBy: [{ uid: "a", title: "A", via: "epic" }] }),
        new Map([["epic", "Billing"]]),
      ),
    ).toBe("waiting for «A» (through «Billing»)");
  });
});

describe("the drawer's lists", () => {
  it("lists a card's dependencies with what the board knows of each", () => {
    const self = card({ itemId: "x", dependsOn: ["t2", "gone"], blockedBy: [{ uid: "gone", title: "Far away", type: "epic" }] });
    expect(dependencyRows(self, all).map((d) => [d.uid, d.title, d.card?.itemId])).toEqual([
      ["t2", "Billing API", "t2"],
      ["gone", "Far away", undefined],
    ]);
  });

  it("lists what waits for a card: the server's answer, else the cards held", () => {
    expect(blocksOf(t1, all).map((b) => b.uid)).toEqual(["t3"]);
    expect(blocksOf({ ...t1, blocks: [{ uid: "z", title: "Z" }] }, all).map((b) => b.uid)).toEqual(["z"]);
  });

  it("adds an id once and removes it", () => {
    expect(withDependency(undefined, "a")).toEqual(["a"]);
    expect(withDependency(["a"], "a")).toEqual(["a"]);
    expect(withDependency(["a"], "b")).toEqual(["a", "b"]);
    expect(withoutDependency(["a", "b"], "a")).toEqual(["b"]);
  });
});
