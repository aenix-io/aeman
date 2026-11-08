import { describe, expect, it } from "vitest";

import { migrateKeys, STRATEGY_KEY_MOVES } from "./storagemigrate";

// The Roadmap became the Strategy board, and the keys its reader's choices
// were kept under move with it: read once under the old name, written under
// the new, the old one taken off — nobody loses their filters or widths.
function memory(init: Record<string, string>) {
  const m = new Map(Object.entries(init));
  return {
    map: m,
    getItem: (k: string) => (m.has(k) ? (m.get(k) as string) : null),
    setItem: (k: string, v: string) => void m.set(k, v),
    removeItem: (k: string) => void m.delete(k),
  };
}

describe("moving the Strategy board's remembered choices", () => {
  it("moves an old key to the new one and takes the old off", () => {
    const s = memory({ "aeman.roadmap.filters": '{"teams":["a"]}' });
    migrateKeys(s, [["aeman.roadmap.filters", "aeman.strategy.filters"]]);
    expect(s.getItem("aeman.strategy.filters")).toBe('{"teams":["a"]}');
    expect(s.getItem("aeman.roadmap.filters")).toBeNull();
  });

  it("keeps a new key that is already there, and still takes the old off", () => {
    const s = memory({ "aeman.roadmap.depth": "1", "aeman.strategy.depth": "3" });
    migrateKeys(s, [["aeman.roadmap.depth", "aeman.strategy.depth"]]);
    expect(s.getItem("aeman.strategy.depth")).toBe("3");
    expect(s.getItem("aeman.roadmap.depth")).toBeNull();
  });

  it("does nothing when there is nothing to move, and nothing the second time", () => {
    const s = memory({ other: "x" });
    migrateKeys(s, STRATEGY_KEY_MOVES);
    expect([...s.map.keys()]).toEqual(["other"]);
    const t = memory({ "aeman.roadmap.zoom": "1y" });
    migrateKeys(t, STRATEGY_KEY_MOVES);
    migrateKeys(t, STRATEGY_KEY_MOVES);
    expect(Object.fromEntries(t.map)).toEqual({ "aeman.strategy.zoom": "1y" });
  });

  it("moves every key the board keeps, and the remembered tab's old name", () => {
    expect(STRATEGY_KEY_MOVES.map(([from]) => from).sort()).toEqual([
      "aeman.roadmap.columns",
      "aeman.roadmap.depth",
      "aeman.roadmap.filters",
      "aeman.roadmap.folded",
      "aeman.roadmap.zoom",
    ]);
    const s = memory({ "aeman.view": "roadmap" });
    migrateKeys(s, STRATEGY_KEY_MOVES);
    expect(s.getItem("aeman.view")).toBe("strategy");
  });
});
