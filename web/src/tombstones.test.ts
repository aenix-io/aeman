import { describe, expect, it } from "vitest";

import type { Provider } from "./providers/types";
import { Tombstones, withTombstones } from "./tombstones";

// A card this tab deleted stays deleted on its board. The delete is several
// writes on the server (a plan item's children move up first), and a frame
// sent between them can still carry the card — arriving before the delete's
// answer, or after the DELETED. For a while after the delete such a frame is
// ignored; the DELETED itself always gets through. A delete the server
// refused lifts the mark at once, so the card comes back with the reload.
describe("the cards this tab deleted", () => {
  it("turns away a frame that would bring one back", () => {
    const t = new Tombstones(60_000);
    t.mark("epic", 0);
    expect(t.blocks("epic", "MODIFIED", 1_000)).toBe(true);
    expect(t.blocks("epic", "ADDED", 1_000)).toBe(true);
    expect(t.blocks("epic", "DELETED", 1_000)).toBe(false);
    expect(t.blocks("other", "MODIFIED", 1_000)).toBe(false);
  });

  it("forgets the mark after a while, and at once when the delete was refused", () => {
    const t = new Tombstones(60_000);
    t.mark("epic", 0);
    expect(t.blocks("epic", "MODIFIED", 60_001)).toBe(false);
    t.mark("task", 0);
    t.lift("task");
    expect(t.blocks("task", "MODIFIED", 1)).toBe(false);
  });
});

// Which writes mark a card: every deleteCard; the remove of a card the board
// that sent it knows the remove deletes (a plan item's ×, where it says so);
// never a day board's remove of a task, which the server may answer by
// handing it back. A refused delete lifts the mark. A card whose create is
// still in flight is marked under both ids, so its real id is covered once
// the server names it.
describe("the writes that mark a card", () => {
  const fake = (fail = false) =>
    ({
      deleteCard: () => (fail ? Promise.reject(new Error("no")) : Promise.resolve()),
      removeCard: () => (fail ? Promise.reject(new Error("no")) : Promise.resolve()),
    }) as unknown as Provider;
  const resolve = async (id: string) => (id === "tmp-1" ? "real-1" : id);

  it("marks a deleted card, and lifts the mark when the delete is refused", async () => {
    const t = new Tombstones();
    await withTombstones(fake(), t, resolve).deleteCard("a");
    expect(t.blocks("a", "MODIFIED")).toBe(true);
    const t2 = new Tombstones();
    await withTombstones(fake(true), t2, resolve).deleteCard("a").catch(() => undefined);
    expect(t2.blocks("a", "MODIFIED")).toBe(false);
  });

  it("marks a remove only where the board says it deletes", async () => {
    const t = new Tombstones();
    const p = withTombstones(fake(), t, resolve);
    await p.removeCard("task", "off-board");
    expect(t.blocks("task", "MODIFIED")).toBe(false);
    await p.removeCard("epic", "off-board", { deletes: true });
    expect(t.blocks("epic", "MODIFIED")).toBe(true);
  });

  it("marks the real id of a card still being made", async () => {
    const t = new Tombstones();
    await withTombstones(fake(), t, resolve).deleteCard("tmp-1");
    expect(t.blocks("tmp-1", "MODIFIED")).toBe(true);
    expect(t.blocks("real-1", "MODIFIED")).toBe(true);
  });

  it("forgets the expired marks as it makes new ones", () => {
    const t = new Tombstones(10);
    t.mark("old", 0);
    t.mark("new", 100);
    expect(t.size).toBe(1);
  });
});
