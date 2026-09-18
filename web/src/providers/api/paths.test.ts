import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { isSignedOut } from "../../session";
import { ApiError, apiProvider, showingDay, standingOn } from "./apiProvider";

// WHERE EACH CALL GOES. The board a reader is standing on is part of the
// address now, and nothing else in this suite reads a URL: the two bugs this
// change shipped and had to fix — a watch that stopped upgrading, an × the
// server no longer recognised as addressing its card — were both a path
// nobody asserted. These are the paths, asserted.

interface Call {
  method: string;
  url: string;
  body?: unknown;
  headers: Headers;
}

const calls: Call[] = [];

const card = {
  kind: "Card",
  metadata: { uid: "c1" },
  spec: { title: "x", assignees: [], progress: 0, dates: {} },
  status: { complete: false, inProgress: false },
  items: [],
};

function answer(status: number, statusText: string, body?: unknown): Response {
  if (body === undefined) {
    return new Response(null, { status, statusText });
  }
  // A string body is sent as itself — that is a proxy's error page, which the
  // problem reader has to fall off rather than parse.
  if (typeof body === "string") {
    return new Response(body, { status, statusText, headers: { "Content-Type": "text/html" } });
  }
  return new Response(JSON.stringify(body), {
    status,
    statusText,
    headers: { "Content-Type": "application/json" },
  });
}

let respond: () => Response;

beforeEach(() => {
  calls.length = 0;
  respond = () => answer(200, "OK", card);
  // The client is given ONE Request, not (url, init), and it looks `fetch` up
  // per call — so a stub installed here reaches a client built at import time.
  // The assertions below read the path back off that Request, which is what
  // keeps them the same sentences they were before it existed.
  vi.stubGlobal("fetch", async (request: Request) => {
    const url = new URL(request.url);
    calls.push({
      method: request.method,
      url: `${url.pathname}${url.search}`,
      body: request.body ? ((await request.json()) as unknown) : undefined,
      headers: request.headers,
    });
    return respond();
  });
});

afterEach(() => {
  vi.unstubAllGlobals();
  standingOn("view=all");
  showingDay("");
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

// WHAT RIDES ALONG. Two headers carry state no URL does: which tab made the
// change, and which day the person is looking at. The second is what stops a
// write being made from a picture of a day that ended, so its absence on
// today's board is as much the contract as its presence on a past one.
describe("the headers a request carries", () => {
  it("names the tab, and the day when the board is showing a past one", async () => {
    showingDay("2026-09-11");
    await apiProvider.getCard("c1");
    expect(calls[0].headers.get("X-Aeman-Client")).toBeTruthy();
    expect(calls[0].headers.get("X-Aeman-As-Of")).toBe("2026-09-11");
  });

  it("leaves the day off while the board is showing today", async () => {
    await apiProvider.getCard("c1");
    expect(calls[0].headers.get("X-Aeman-Client")).toBeTruthy();
    expect(calls[0].headers.get("X-Aeman-As-Of")).toBeNull();
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
    respond = () => answer(502, "Bad Gateway", "<html>502 Bad Gateway</html>");
    const err = await apiProvider.getCard("c1").catch((e: unknown) => e);
    expect(err).toMatchObject({ message: "Bad Gateway", status: 502 });
    expect((err as ApiError).code).toBeUndefined();
  });

  // HTTP/2 carries no reason phrase, so `statusText` is empty on every answer
  // a server speaking it gives. That makes the status the last thing left to
  // say, and without it the person reads an error with no text at all.
  it("names the status when there is no reason phrase either", async () => {
    respond = () => answer(500, "");
    const err = await apiProvider.getCard("c1").catch((e: unknown) => e);
    expect(err).toMatchObject({ message: "HTTP 500", status: 500 });
  });

  // The session can end under an open tab. session.isSignedOut recognises
  // that by class and status, so a refusal raised as anything but an ApiError
  // carrying 401 leaves the tab making calls behind a gate it cannot see —
  // and a method answering void has no result to read the status out of.
  it("raises a gone session as the sign-out the guard recognises", async () => {
    respond = () =>
      answer(401, "Unauthorized", {
        type: "about:blank",
        title: "Unauthorized",
        status: 401,
        detail: "not signed in",
        code: "notAuthenticated",
      });
    const err = await apiProvider.deleteCard("c1").catch((e: unknown) => e);
    expect(isSignedOut(err)).toBe(true);
  });
});
