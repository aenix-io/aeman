import { describe, expect, it } from "vitest";

import { mergeCardLists } from "./cardmerge";
import type { Card } from "./providers/types";

const card = (itemId: string, extra: Partial<Card> = {}): Card => ({
  itemId,
  title: itemId,
  assignees: [],
  ...extra,
});

describe("mergeCardLists", () => {
  it("flattens the lists in order, deduping by item id", () => {
    const got = mergeCardLists([[card("a"), card("b")], [card("b"), card("c")]]);
    expect(got.map((c) => c.itemId)).toEqual(["a", "b", "c"]);
  });

  it("is empty for no lists", () => {
    expect(mergeCardLists([])).toEqual([]);
    expect(mergeCardLists([[], []])).toEqual([]);
  });

  // A listing is the board's row view: it carries no notes, no events and no
  // description. Replacing the board's cards with it threw away what had
  // already been fetched, so every view switch re-fetched a log per card —
  // a request each, seconds of git work — and the board settled visibly late.
  // What the fresh row does not carry, the card keeps.
  it("keeps the loaded notes, events and body of a card the listing brings back", () => {
    const loaded = card("a", {
      notes: [{ id: "n1", body: "note", createdAt: "2026-08-28T09:00:00Z", source: "comment" }],
      events: [{ id: "e1", kind: "progress", at: "2026-08-28T09:00:00Z" }],
      description: "the body",
      progress: 40,
    });
    const [fresh] = mergeCardLists([[card("a", { progress: 70 })]], [loaded]);
    expect(fresh.progress).toBe(70); // the listing is the truth about the row
    expect(fresh.notes).toEqual(loaded.notes);
    expect(fresh.events).toEqual(loaded.events);
    expect(fresh.description).toBe("the body");
  });

  it("does not invent them for a card that was never loaded", () => {
    const [fresh] = mergeCardLists([[card("new")]], [card("other", { notes: [] })]);
    expect(fresh.notes).toBeUndefined();
    expect(fresh.events).toBeUndefined();
    expect(fresh.description).toBeUndefined();
  });

  // A listing that DOES carry a body (a full fetch, a card echoed by a
  // mutation) is the fresher answer and wins.
  it("takes the listing's own body when it has one", () => {
    const [fresh] = mergeCardLists(
      [[card("a", { description: "new body" })]],
      [card("a", { description: "old body" })],
    );
    expect(fresh.description).toBe("new body");
  });

  it("works without a previous board", () => {
    expect(mergeCardLists([[card("a")]]).map((c) => c.itemId)).toEqual(["a"]);
  });
});
