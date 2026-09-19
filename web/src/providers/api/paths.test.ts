import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ApiError, apiProvider, standingOn } from "./apiProvider";

// WHERE EACH CALL GOES. The board a reader is standing on is part of the
// address now, and nothing else in this suite reads a URL: the two bugs this
// change shipped and had to fix — a watch that stopped upgrading, an × the
// server no longer recognised as addressing its card — were both a path
// nobody asserted. These are the paths, asserted.

const calls: { method: string; url: string; body?: unknown }[] = [];

const card = {
  kind: "Card",
  metadata: { uid: "c1" },
  spec: { title: "x", assignees: [] },
  status: {},
  items: [],
};

function answer(status: number, statusText: string, body?: unknown): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText,
    json: () =>
      body === undefined
        ? Promise.reject(new SyntaxError("Unexpected end of JSON input"))
        : Promise.resolve(body),
  } as unknown as Response;
}

let respond: () => Response;

beforeEach(() => {
  calls.length = 0;
  respond = () => answer(200, "OK", card);
  vi.stubGlobal(
    "fetch",
    (url: string, init: RequestInit = {}) => {
      calls.push({
        method: init.method ?? "GET",
        url,
        body: init.body ? JSON.parse(init.body as string) : undefined,
      });
      return Promise.resolve(respond());
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
    standingOn("day=2026-09-11&reviews=true&view=me");
    await apiProvider.removeCard("c1", "unassign");
    expect(calls[0].method).toBe("POST");
    // The scope is the LISTING's, reviews and all: the server judges the
    // press against the board as it was drawn.
    expect(calls[0].url).toBe(
      "/api/v1/views/me/cards/c1/actions/remove?day=2026-09-11&reviews=true",
    );
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

// WHAT COMES BACK. A write with nothing to show answers 204 and no body; a
// refusal is an RFC 9457 problem whose detail is the sentence a person reads.
describe("what the server answers", () => {
  it("takes 204 as done, with nothing to parse", async () => {
    respond = () => answer(204, "No Content");
    await expect(apiProvider.renameTeam("test", "platform")).resolves.toBeUndefined();
  });

  it("raises a problem as an ApiError carrying its detail, status, code and action", async () => {
    respond = () =>
      answer(503, "Service Unavailable", {
        type: "about:blank",
        title: "Service Unavailable",
        status: 503,
        detail: "the app cannot reach acme/board",
        code: "setupRequired",
        actionUrl: "https://forge.example/install",
      });
    const err = await apiProvider.getCard("c1").catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err).toMatchObject({
      message: "the app cannot reach acme/board",
      status: 503,
      code: "setupRequired",
      actionUrl: "https://forge.example/install",
    });
  });

  it("falls back to the status text when the refusal is not a problem", async () => {
    respond = () => answer(502, "Bad Gateway");
    const err = await apiProvider.getCard("c1").catch((e: unknown) => e);
    expect(err).toMatchObject({ message: "Bad Gateway", status: 502 });
    expect((err as ApiError).code).toBeUndefined();
  });
});
