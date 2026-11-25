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
    moveTogether: record("moveTogether"),
    setInProgress: record("setInProgress"),
  } as unknown as Provider;
  return { provider, calls };
}

// "old" is a card of a team whose sprint has moved on: a record of that
// evening. "live" is a card of a team still inside that sprint.
// The refusal calls back with the uid it refused, and what comes back is the
// message. In the app that callback is what LEAVES the record — the day jumps
// to today and the team is offered its carry — and it answers with no message
// at all; here it answers with one, so the refusals stay legible.
const asked: string[] = [];
const guard = (p: Provider, hasRecords = true) =>
  frozenProvider(
    p,
    (uid) => uid === "old",
    () => hasRecords,
    (uid) => {
      asked.push(uid);
      return REASON;
    },
  );

describe("frozenProvider", () => {
  it("hands the refused card's uid to the callback", async () => {
    const { provider } = spyProvider();
    asked.length = 0;
    await expect(guard(provider).patchCard("old", {})).rejects.toThrow(REASON);
    // A write that names no card is refused for the whole board, and says so
    // with an empty uid: there is no one card to leave the record for.
    await expect(guard(provider).carryOver("portal")).rejects.toThrow(REASON);
    expect(asked).toEqual(["old", ""]);
  });

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

  // A block names its cards in a list: one record among them refuses the
  // whole block, and a block of live cards goes through.
  it("refuses a block that holds a record, and passes a live one", async () => {
    const { provider, calls } = spyProvider();
    const frozen = guard(provider);
    await expect(frozen.moveTogether(["live", "old"], { after: null })).rejects.toThrow(REASON);
    await frozen.moveTogether(["live"], { after: null });
    expect(calls).toEqual(["moveTogether"]);
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
    await expect(guard(provider).carryOver("portal")).rejects.toThrow(REASON);
    expect(calls).toEqual([]);
  });

  // A create names its team, and the server judges it by that team: taken on
  // a day the team still works, refused out loud (dayIsARecord) on one it has
  // moved past. Refused here, it dropped a card typed into a live team's cell
  // silently whenever another team on the screen had settled.
  it("lets a create through for the server to judge by its team", async () => {
    const { provider, calls } = spyProvider();
    await guard(provider).createCard({ title: "x" } as never);
    expect(calls).toEqual(["createCard"]);
  });

  it("allows it again once the view holds none", async () => {
    const { provider, calls } = spyProvider();
    const frozen = guard(provider, false);
    await frozen.createCard({ title: "x" } as never);
    await frozen.carryOver("portal");
    expect(calls).toEqual(["createCard", "carryOver"]);
  });

  // Presence is who is looking at what; on a RECORD view (a past day) looking
  // is still looking, so it is swallowed rather than errored per selection.
  it("swallows presence on a record view instead of erroring", async () => {
    const { provider, calls } = spyProvider();
    await expect(
      guard(provider).setPresence("kvaps", "old"),
    ).resolves.toBeUndefined();
    expect(calls).toEqual([]);
  });

  // On a LIVE board it must actually be sent, or the shared-cursor feature
  // never fires: swallowing it unconditionally is what once killed presence
  // (a selection in Me showed up nowhere).
  it("sends presence on a live board", async () => {
    const { provider, calls } = spyProvider();
    await guard(provider, false).setPresence("kvaps", "live");
    expect(calls).toEqual(["setPresence"]);
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
