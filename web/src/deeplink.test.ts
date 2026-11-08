import { describe, expect, it } from "vitest";

import { buildLink, cardLink, cardUnreadable, linkChange, parseLink, saveReturnTo, startDay, takeReturnTo } from "./deeplink";

// THE BOARD'S ADDRESS IS ITS STATE. The view and the open card live in the
// query of the root document — /?view=strategy&card=<uid> — and a day board
// (Me, Team) looked at on another day than today carries it too
// (&day=YYYY-MM-DD). The query of the root and not a path, so the address is
// always the one document the server serves uncached, nothing it routes
// (/api, /auth, /oauth, /mcp) can ever be shadowed by a board path, and the
// bundle's asset paths never depend on how deep the address is.
const TODAY = "2026-11-07";

describe("reading the board's address", () => {
  it("reads the view, the card and the day", () => {
    expect(parseLink("?view=strategy&card=01JABC")).toEqual({ view: "strategy", card: "01JABC", day: null });
    expect(parseLink("?view=team&day=2026-11-03")).toEqual({ view: "team", card: null, day: "2026-11-03" });
    expect(parseLink("")).toEqual({ view: null, card: null, day: null });
  });

  it("knows the old names of a view, and takes nothing for an unknown one", () => {
    expect(parseLink("?view=triage").view).toBe("backlog");
    expect(parseLink("?view=strategy").view).toBe("strategy");
    expect(buildLink(parseLink("?view=strategy&card=01JABC"), TODAY)).toBe("?view=strategy&card=01JABC");
    // The Strategy board's old name is no board any more: an old link is
    // taken like any unknown view.
    expect(parseLink("?view=roadmap").view).toBeNull();
    expect(parseLink("?view=whatever").view).toBeNull();
  });

  it("takes no card id or day that is not one", () => {
    expect(parseLink("?card=").card).toBeNull();
    expect(parseLink("?card=" + encodeURIComponent("<script>")).card).toBeNull();
    expect(parseLink("?card=" + "x".repeat(200)).card).toBeNull();
    expect(parseLink("?day=yesterday").day).toBeNull();
    // A day is a real calendar day, not four, two and two digits.
    expect(parseLink("?day=2026-02-31").day).toBeNull();
    expect(parseLink("?day=2026-99-99").day).toBeNull();
    expect(parseLink("?day=2028-02-29").day).toBe("2028-02-29");
  });

  // The linked day outlives the board's start: the config that sets the
  // board's zone puts the board on the linked day, and on today only when the
  // link named none.
  it("starts the board on the linked day", () => {
    expect(startDay({ view: "team", card: null, day: "2026-11-03" }, TODAY)).toBe("2026-11-03");
    expect(startDay({ view: "team", card: null, day: null }, TODAY)).toBe(TODAY);
  });
});

describe("writing the board's address", () => {
  it("names the view and the open card", () => {
    expect(buildLink({ view: "strategy", card: "01JABC", day: TODAY }, TODAY)).toBe("?view=strategy&card=01JABC");
    expect(buildLink({ view: "me", card: null, day: TODAY }, TODAY)).toBe("?view=me");
  });

  it("names the day on a day board when it is not today, and never elsewhere", () => {
    expect(buildLink({ view: "team", card: null, day: "2026-11-03" }, TODAY)).toBe("?view=team&day=2026-11-03");
    expect(buildLink({ view: "backlog", card: null, day: "2026-11-03" }, TODAY)).toBe("?view=backlog");
  });

  it("reads back what it wrote", () => {
    const s = { view: "me" as const, card: "01JABC", day: "2026-11-03" };
    expect(parseLink(buildLink(s, TODAY))).toEqual(s);
  });

  it("makes a card's link on this board, for sharing — the day a day board shows included", () => {
    expect(cardLink("https://board.example", "/", "backlog", "01JABC", "2026-11-03", TODAY)).toBe(
      "https://board.example/?view=backlog&card=01JABC",
    );
    expect(cardLink("https://board.example", "/", "team", "01JABC", "2026-11-03", TODAY)).toBe(
      "https://board.example/?view=team&day=2026-11-03&card=01JABC",
    );
    expect(cardLink("https://board.example", "/", "me", "01JABC", TODAY, TODAY)).toBe("https://board.example/?view=me&card=01JABC");
  });
});

// Opening or closing a card is a step Back undoes (pushState); switching the
// tab or the day only rewrites the address (replaceState), so Back does not
// walk through every tab ever looked at.
describe("how a change reaches the address", () => {
  it("pushes a card opened or closed, and replaces anything else", () => {
    expect(linkChange("?view=me", "?view=me&card=01J")).toBe("push");
    expect(linkChange("?view=me&card=01J", "?view=me")).toBe("push");
    expect(linkChange("?view=me&card=01J", "?view=me&card=01K")).toBe("push");
    expect(linkChange("?view=me", "?view=team")).toBe("replace");
    expect(linkChange("?view=me&card=01J", "?view=team&card=01J")).toBe("replace");
    expect(linkChange("?view=me", "?view=me")).toBe("none");
  });
});

// A deep link followed before signing in survives the round trip through
// the forge: the address is kept in the tab's session storage and given back
// once — only an address on this board.
describe("the address kept across signing in", () => {
  const storage = () => {
    const m = new Map<string, string>();
    return {
      getItem: (k: string) => m.get(k) ?? null,
      setItem: (k: string, v: string) => void m.set(k, v),
      removeItem: (k: string) => void m.delete(k),
    };
  };

  it("is given back once", () => {
    const s = storage();
    saveReturnTo(s, "/?view=strategy&card=01JABC", 1000);
    expect(takeReturnTo(s, 2000)).toBe("/?view=strategy&card=01JABC");
    expect(takeReturnTo(s, 2000)).toBeNull();
  });

  // A sign-in abandoned and the board opened again much later must not
  // send the visitor to an address they left long ago: it is kept for the
  // sign-in's own span (the forge's state lives ten minutes), and taken off
  // on the next load whatever happens to it.
  it("is not given back after the sign-in's span", () => {
    const s = storage();
    saveReturnTo(s, "/?view=strategy&card=01JABC", 0);
    expect(takeReturnTo(s, 11 * 60 * 1000)).toBeNull();
    expect(s.getItem("aeman.returnTo")).toBeNull();
  });

  it("is only ever an address on this board", () => {
    const s = storage();
    for (const evil of ["//evil.example/", "https://evil.example/", "/\\evil", "javascript:alert(1)", "/\t/evil.example", "/\u0000x"]) {
      s.setItem("aeman.returnTo", JSON.stringify({ path: evil, at: 0 }));
      expect(takeReturnTo(s, 1)).toBeNull();
      saveReturnTo(s, evil, 0);
      expect(takeReturnTo(s, 1)).toBeNull();
    }
    s.setItem("aeman.returnTo", "not json");
    expect(takeReturnTo(s, 1)).toBeNull();
  });
});

// A card the address names that the visitor may not read, or that is gone,
// is a notice and leaves the address; anything else (the network, the
// server) is an error to show, and the address keeps the card.
describe("a linked card that cannot be opened", () => {
  it("is unreadable on 403 and 404 alone", () => {
    expect(cardUnreadable({ status: 404 })).toBe(true);
    expect(cardUnreadable({ status: 403 })).toBe(true);
    expect(cardUnreadable({ status: 502 })).toBe(false);
    expect(cardUnreadable(new Error("network"))).toBe(false);
  });
});
