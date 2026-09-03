import { describe, expect, it, vi } from "vitest";

import { frozenProvider } from "./frozen";
import type { Provider } from "./types";

const REASON = "this is the board as it was that day";

// A stand-in for the real provider: every call records itself, so the test
// can tell "passed through" from "refused".
function spyProvider(): { provider: Provider; calls: string[] } {
  const calls: string[] = [];
  const record =
    (name: string) =>
    (...args: unknown[]) => {
      calls.push(name);
      return Promise.resolve(args.length);
    };
  const provider = {
    loadBoard: record("loadBoard"),
    listCards: record("listCards"),
    getCard: record("getCard"),
    listLog: record("listLog"),
    listNotes: record("listNotes"),
    setPresence: record("setPresence"),
    patchCard: record("patchCard"),
    deleteCard: record("deleteCard"),
    removeCard: record("removeCard"),
    carryOver: record("carryOver"),
    createCard: record("createCard"),
    moveCard: record("moveCard"),
    setInProgress: record("setInProgress"),
  } as unknown as Provider;
  return { provider, calls };
}

// "old" is a card of a team whose sprint has moved on: a record of that
// evening. "live" is a card of a team still inside that sprint.
const guard = (p: Provider, hasRecords = true) =>
  frozenProvider(p, (uid) => uid === "old", () => hasRecords, REASON);

describe("frozenProvider", () => {
  it("lets the board read what it needs", async () => {
    const { provider, calls } = spyProvider();
    const frozen = guard(provider);
    await frozen.loadBoard();
    await frozen.listCards({ view: "team" });
    await frozen.getCard("old");
    await frozen.listLog("old");
    await frozen.listNotes("old");
    expect(calls).toEqual([
      "loadBoard",
      "listCards",
      "getCard",
      "listLog",
      "listNotes",
    ]);
  });

  // The point of the wrapper: a handler that forgot the card is a record
  // must not reach the board with it.
  it("refuses every write to a card that is a record", async () => {
    const { provider, calls } = spyProvider();
    const frozen = guard(provider);
    for (const write of [
      () => frozen.patchCard("old", { progress: 50 }),
      () => frozen.deleteCard("old"),
      () => frozen.removeCard("old", "off-board"),
      () => frozen.moveCard("old", null),
      () => frozen.setInProgress("old"),
    ]) {
      await expect(write()).rejects.toThrow(REASON);
    }
    expect(calls).toEqual([]);
  });

  // …and the rest of the same board is today's work, which must not be
  // taken away: one screen, two moments.
  it("lets a live card be written on the same board", async () => {
    const { provider, calls } = spyProvider();
    const frozen = guard(provider);
    await frozen.patchCard("live", { progress: 50 });
    await frozen.removeCard("live", "off-board");
    await frozen.setInProgress("live");
    expect(calls).toEqual(["patchCard", "removeCard", "setInProgress"]);
  });

  // A write that names no card cannot be judged card by card, and made from
  // a view holding records it would land in a board nobody is looking at.
  it("refuses a card-less write while any record is on screen", async () => {
    const { provider, calls } = spyProvider();
    await expect(
      guard(provider).createCard({ title: "x" } as never),
    ).rejects.toThrow(REASON);
    await expect(guard(provider).carryOver("portal")).rejects.toThrow(REASON);
    expect(calls).toEqual([]);
  });

  it("allows it again once the view holds none", async () => {
    const { provider, calls } = spyProvider();
    const frozen = guard(provider, false);
    await frozen.createCard({ title: "x" } as never);
    await frozen.carryOver("portal");
    expect(calls).toEqual(["createCard", "carryOver"]);
  });

  // Presence is who is looking at what; looking at a past day is still
  // looking, and an error per selection would be noise.
  it("swallows presence instead of erroring", async () => {
    const { provider, calls } = spyProvider();
    await expect(
      guard(provider).setPresence("kvaps", "old"),
    ).resolves.toBeUndefined();
    expect(calls).toEqual([]);
  });

  // A method nobody classified is a write until proven otherwise.
  it("guards a call it has never heard of", async () => {
    const { provider } = spyProvider();
    const frozen = guard(provider) as unknown as {
      somethingNew: () => Promise<void>;
    };
    (provider as unknown as { somethingNew: () => Promise<void> }).somethingNew =
      vi.fn();
    await expect(frozen.somethingNew()).rejects.toThrow(REASON);
  });
});
