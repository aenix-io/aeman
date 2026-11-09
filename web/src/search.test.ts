import { describe, expect, it } from "vitest";

import { highlight, searchCards } from "./search";
import type { Card } from "./providers/types";

const card = (itemId: string, title: string, over: Partial<Card> = {}): Card =>
  ({ itemId, title, assignees: [], progress: 0, ...over }) as Card;

const ids = (cs: Card[]) => cs.map((c) => c.itemId);

// The ⌘K search: every card the visitor can read, ranked the way a launcher
// ranks — what the title IS before what it merely contains, open work before
// finished work — so the card meant is the first row and Enter opens it.
describe("searchCards", () => {
  it("finds nothing for an empty or blank query", () => {
    expect(searchCards([card("a", "Backups")], "")).toEqual([]);
    expect(searchCards([card("a", "Backups")], "   ")).toEqual([]);
  });

  it("matches regardless of case, in any script", () => {
    const cs = [card("a", "Обновить Talos у GooshCloud"), card("b", "Backups")];
    expect(ids(searchCards(cs, "talos"))).toEqual(["a"]);
    expect(ids(searchCards(cs, "ОБНОВИТЬ"))).toEqual(["a"]);
  });

  it("needs every word, in any order", () => {
    const cs = [card("a", "Обновить talos у gooshcloud"), card("b", "Обновить gooshcloud до 1.7")];
    expect(ids(searchCards(cs, "talos gooshcloud"))).toEqual(["a"]);
    expect(ids(searchCards(cs, "gooshcloud обновить"))).toEqual(["a", "b"]);
  });

  it("puts the exact title first, then a title that starts with it, then one that contains it", () => {
    const cs = [
      card("contains", "Fix the backups"),
      card("starts", "Backups: restore drill"),
      card("exact", "Backups"),
      card("word", "Nightly backups"),
    ];
    // "contains" and "word" both hold it at a word's start: equal, so they
    // keep the order they came in.
    expect(ids(searchCards(cs, "backups"))).toEqual(["exact", "starts", "contains", "word"]);
  });

  it("ranks a match at a word's start above one inside a word", () => {
    const cs = [card("inside", "Superstack"), card("start", "Stack overflow")];
    expect(ids(searchCards(cs, "stack"))).toEqual(["start", "inside"]);
  });

  it("puts open work before finished work of the same strength", () => {
    const cs = [
      card("done", "Talos upgrade", { progress: 100, stage: "done" }),
      card("open", "Talos upgrade"),
    ];
    expect(ids(searchCards(cs, "talos"))).toEqual(["open", "done"]);
  });

  it("breaks ties by the newest card", () => {
    const cs = [
      card("old", "Talos upgrade", { createdAt: "2026-01-01T00:00:00Z" }),
      card("new", "Talos upgrade", { createdAt: "2026-11-01T00:00:00Z" }),
    ];
    expect(ids(searchCards(cs, "talos"))).toEqual(["new", "old"]);
  });

  it("finds a card by a word of the plan it stands under, below a title match", () => {
    const cs = [
      card("under", "Upgrade Talos", { lineage: [{ uid: "e", type: "epic", title: "GooshCloud" }] }),
      card("named", "GooshCloud contract"),
    ];
    expect(ids(searchCards(cs, "gooshcloud"))).toEqual(["named", "under"]);
    expect(ids(searchCards(cs, "gooshcloud talos"))).toEqual(["under"]);
  });

  it("opens a card by its pasted uid", () => {
    const cs = [card("01M6ZW2RAYRVW4NTRKEVTVFD15", "Обновить talos"), card("b", "01M6ZW2 in the title")];
    expect(ids(searchCards(cs, "01m6zw2rayrvw4ntrkevtvfd15"))).toEqual(["01M6ZW2RAYRVW4NTRKEVTVFD15"]);
  });

  it("leaves the hidden sprint-state cards out", () => {
    expect(searchCards([card("s", "aeman:sprint-state")], "sprint")).toEqual([]);
  });

  it("stops at the limit", () => {
    const cs = Array.from({ length: 80 }, (_, i) => card(`c${i}`, `Task ${i}`));
    expect(searchCards(cs, "task")).toHaveLength(50);
    expect(searchCards(cs, "task", 5)).toHaveLength(5);
  });
});

// What the row marks in the title: every place a word of the query matched.
describe("highlight", () => {
  it("splits a title into plain and matched runs", () => {
    expect(highlight("Обновить talos у Talos", "talos")).toEqual([
      { text: "Обновить ", hit: false },
      { text: "talos", hit: true },
      { text: " у ", hit: false },
      { text: "Talos", hit: true },
    ]);
  });

  it("merges overlapping words into one run", () => {
    expect(highlight("backups", "back ups kup")).toEqual([{ text: "backups", hit: true }]);
  });

  it("gives the title back whole with nothing to mark", () => {
    expect(highlight("Backups", "")).toEqual([{ text: "Backups", hit: false }]);
    expect(highlight("Backups", "zzz")).toEqual([{ text: "Backups", hit: false }]);
  });
});
