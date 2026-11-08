import { describe, expect, it } from "vitest";
import type { Card } from "./providers/types";
import {
  blocksOf,
  dependencyOptions,
  dependenciesToFetch,
  dependencyRollback,
  derivedLockOf,
  dependencyRows,
  lockTitle,
  withDependency,
  withFetched,
  withoutDependency,
} from "./dependencies";
import { latestWins } from "./roadmap";

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

// The day boards draw the derived lock with the locked stage's look — the
// card can still be moved and planned — when no stage of a person's says
// more and the card is not done.
describe("the day boards' lock", () => {
  it("shows the derived lock on an unstaged, unfinished card, with what it waits for", () => {
    const waiting = { locked: true, blockedBy: [{ uid: "a", title: "Auth" }] };
    expect(derivedLockOf(card({ itemId: "x", ...waiting }))).toBe("waiting for «Auth»");
    expect(derivedLockOf(card({ itemId: "x", ...waiting, stage: "review" }))).toBeNull();
    expect(derivedLockOf(card({ itemId: "x", ...waiting, progress: 100 }))).toBeNull();
    expect(derivedLockOf(card({ itemId: "x" }))).toBeNull();
  });
});

// A dependency the drawer's listing does not carry is fetched by id ONCE per
// drawer card, whatever the answer: a hidden target, a deleted one or a
// network error answers the same next time, and asking again on every render
// was a request loop that never stopped.
describe("the dependencies the drawer fetches", () => {
  it("asks for what it neither holds nor has asked for", () => {
    expect(dependenciesToFetch(["a", "b", "c"], new Set(["a"]), new Set(["b"]))).toEqual(["c"]);
  });
  it("does not ask again for an id that failed", () => {
    expect(dependenciesToFetch(["gone"], new Set(), new Set(["gone"]))).toEqual([]);
  });
  it("asks nothing for a card with no dependencies", () => {
    expect(dependenciesToFetch(undefined, new Set(), new Set())).toEqual([]);
  });
  it("keeps the same list when nothing came back, so no effect reruns", () => {
    const prev = [t1];
    expect(withFetched(prev, [null, null])).toBe(prev);
    expect(withFetched(prev, [])).toBe(prev);
  });
  it("adds what did come back, once", () => {
    const prev = [t1];
    expect(withFetched(prev, [t2, null, t1]).map((c) => c.itemId)).toEqual(["t1", "t2"]);
  });
});

// A refused dependency write takes the list back to what it was before THAT
// press — only while it is still the latest press on the card: an older
// press refused after a newer one was shown must not undo the newer list.
describe("a refused dependency write", () => {
  it("rolls back the latest press", () => {
    const seq = latestWins();
    const n = seq.next("t1");
    expect(dependencyRollback(seq, "t1", n, ["a"])).toEqual({ dependsOn: ["a"] });
  });
  it("leaves a newer press standing when an older one is refused", () => {
    const seq = latestWins();
    const older = seq.next("t1");
    seq.next("t1");
    expect(dependencyRollback(seq, "t1", older, [])).toBeNull();
  });
  it("puts back an empty list as no list", () => {
    const seq = latestWins();
    const n = seq.next("t1");
    expect(dependencyRollback(seq, "t1", n, [])).toEqual({ dependsOn: undefined });
  });
});
