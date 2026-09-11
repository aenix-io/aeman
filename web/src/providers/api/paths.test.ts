import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { apiProvider, standingOn } from "./apiProvider";

// WHERE EACH CALL GOES. The board a reader is standing on is part of the
// address now, and nothing else in this suite reads a URL: the two bugs this
// change shipped and had to fix — a watch that stopped upgrading, an × the
// server no longer recognised as addressing its card — were both a path
// nobody asserted. These are the paths, asserted.

const calls: { method: string; url: string; body?: unknown }[] = [];

beforeEach(() => {
  calls.length = 0;
  vi.stubGlobal(
    "fetch",
    (url: string, init: RequestInit = {}) => {
      calls.push({
        method: init.method ?? "GET",
        url,
        body: init.body ? JSON.parse(init.body as string) : undefined,
      });
      return Promise.resolve({
        ok: true,
        status: 200,
        statusText: "OK",
        json: () =>
          Promise.resolve({
            kind: "Card",
            metadata: { uid: "c1" },
            spec: { title: "x", assignees: [] },
            status: {},
            items: [],
          }),
      } as unknown as Response);
    },
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
  standingOn("view=all");
});

describe("the board in the address", () => {
  it("lists the board the selector names, with the rest as a query", async () => {
    await apiProvider.listCards({ view: "team", team: "portal", day: "2026-09-11" });
    expect(calls[0].url).toBe("/api/v1/views/team/cards?team=portal&day=2026-09-11");
  });

  // A gesture is made FROM the open board, with the scope that board's own
  // listing used — that is what lets the server say "this card is not on that
  // board" instead of acting on a card the person cannot see.
  it("makes a gesture through the board that is open", async () => {
    standingOn("day=2026-09-11&view=me");
    await apiProvider.removeCard("c1", "unassign");
    expect(calls[0].method).toBe("POST");
    expect(calls[0].url).toBe("/api/v1/views/me/cards/c1/actions/remove?day=2026-09-11");
    expect(calls[0].body).toEqual({ intent: "unassign" });
  });

  it("drops a card into a week through the Triage board", async () => {
    standingOn("from=2026-09-07&team=portal&view=triage&weeks=9");
    await apiProvider.placeCard("c1", "2026-09-14");
    expect(calls[0].url).toBe(
      "/api/v1/views/triage/cards/c1/actions/place?from=2026-09-07&team=portal&weeks=9",
    );
  });

  // A create belongs to the board it was typed into: the open one, unless
  // what the add box filled in names another.
  it("creates into the board that is open", async () => {
    standingOn("day=2026-09-11&view=me");
    await apiProvider.createCard({ title: "came up", zone: "yellow", team: null });
    expect(calls[0].url).toBe("/api/v1/views/me/cards");
  });

  it("and into the board the shape names, whatever is open", async () => {
    standingOn("team=portal&view=triage");
    await apiProvider.createCard({ title: "someday", zone: "gray", team: "portal", parked: true });
    expect(calls[0].url).toBe("/api/v1/views/backlog/cards");
    await apiProvider.createCard({ title: "later", zone: "gray", team: "portal", week: "2026-09-14" });
    expect(calls[1].url).toBe("/api/v1/views/triage/cards");
  });

  // The card itself is addressed as itself, whatever board it was found on:
  // one uid, one address, and the actions that mean the same thing anywhere.
  it("leaves the card's own address alone", async () => {
    standingOn("day=2026-09-11&view=me");
    await apiProvider.deferCard("c1", 1);
    expect(calls[0].url).toBe("/api/v1/cards/c1/actions/defer");
    await apiProvider.patchCard("c1", { progress: 40 });
    expect(calls[1].url).toBe("/api/v1/cards/c1");
    expect(calls[1].method).toBe("PATCH");
  });
});
