import { describe, expect, it, vi } from "vitest";

import { frozenProvider } from "./frozen";
import type { Provider } from "./types";

const REASON = "это снимок прошлого дня";

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

describe("frozenProvider", () => {
  it("lets the snapshot read what it needs", async () => {
    const { provider, calls } = spyProvider();
    const frozen = frozenProvider(provider, REASON);
    await frozen.loadBoard();
    await frozen.listCards({ view: "team" });
    await frozen.getCard("c1");
    await frozen.listLog("c1");
    await frozen.listNotes("c1");
    expect(calls).toEqual([
      "loadBoard",
      "listCards",
      "getCard",
      "listLog",
      "listNotes",
    ]);
  });

  // The point of the wrapper: a handler that forgot the day is in the past
  // must not reach the board. Every write is refused, with the reason.
  it("refuses every write, with the reason", async () => {
    const { provider, calls } = spyProvider();
    const frozen = frozenProvider(provider, REASON);
    for (const write of [
      () => frozen.patchCard("c1", { progress: 50 }),
      () => frozen.deleteCard("c1"),
      () => frozen.removeCard("c1", "grid"),
      () => frozen.createCard({ title: "x" } as never),
      () => frozen.moveCard("c1", null),
      () => frozen.setInProgress("c1"),
      () => frozen.carryOver("portal"),
    ]) {
      await expect(write()).rejects.toThrow(REASON);
    }
    expect(calls).toEqual([]);
  });

  // Presence is who is looking at what; looking at a past day is still
  // looking, and an error per selection would be noise.
  it("swallows presence instead of erroring", async () => {
    const { provider, calls } = spyProvider();
    await expect(
      frozenProvider(provider, REASON).setPresence("kvaps", "c1"),
    ).resolves.toBeUndefined();
    expect(calls).toEqual([]);
  });

  // A method nobody classified is a write until proven otherwise: refusing
  // it is visible, letting it through would edit the live board from a view
  // of the past.
  it("refuses a call it has never heard of", async () => {
    const { provider } = spyProvider();
    const frozen = frozenProvider(provider, REASON) as unknown as {
      somethingNew: () => Promise<void>;
    };
    (provider as unknown as { somethingNew: () => Promise<void> }).somethingNew =
      vi.fn();
    await expect(frozen.somethingNew()).rejects.toThrow(REASON);
  });
});
