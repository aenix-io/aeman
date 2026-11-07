import { describe, expect, it } from "vitest";

import { cardIsGone, drawerEscapes, followFrame, followPatch, joinsTree, placementAnswer, withoutDeleted } from "./detail";
import type { Card } from "./providers/types";

const card = (over: Partial<Card>): Card => ({ itemId: "t", title: "Invoices", assignees: [], progress: 0, ...over }) as Card;

// THE DRAWER FOLLOWS ITS CARD even when the board does not list it — a task
// opened from a review card's crumb on the Backlog, fetched by uid. A write
// made from the drawer lands on it from the server's answer, and so does a
// watch frame for its uid; a card it is not showing is left alone.
describe("the drawer's card", () => {
  it("takes the answer of a write made from it, though no board lists it", () => {
    const shown = card({ parent: undefined, lineage: undefined });
    const got = followPatch(shown, "t", { parent: "e", lineage: [{ uid: "e", type: "epic", title: "Billing" }] });
    expect(got?.parent).toBe("e");
    expect(got?.lineage?.[0].title).toBe("Billing");
  });

  it("takes an updater the way the board does", () => {
    expect(followPatch(card({ progress: 10 }), "t", (c) => ({ progress: (c.progress ?? 0) + 10 }))?.progress).toBe(20);
  });

  it("leaves a card it is not showing alone, and nothing when nothing is shown", () => {
    const shown = card({});
    expect(followPatch(shown, "other", { title: "x" })).toBe(shown);
    expect(followPatch(null, "t", { title: "x" })).toBeNull();
  });

  it("takes a watch frame for its uid, keeping the body and notes a frame does not carry", () => {
    const shown = card({ description: "the body", notes: [] });
    const got = followFrame(shown, card({ title: "Invoices v2", parent: "e", description: undefined }), false);
    expect(got?.title).toBe("Invoices v2");
    expect(got?.parent).toBe("e");
    expect(got?.description).toBe("the body");
    expect(got?.notes).toEqual([]);
  });

  it("clears what the frame cleared", () => {
    const got = followFrame(card({ parent: "e" }), card({ parent: undefined }), false);
    expect(got?.parent).toBeUndefined();
  });

  it("closes on a deletion of its card, and ignores another card's frame", () => {
    const shown = card({});
    expect(followFrame(shown, card({}), true)).toBeNull();
    expect(followFrame(shown, card({ itemId: "x" }), true)).toBe(shown);
  });
});

// A scoped watch's DELETED says a card LEFT the board's selection — on a
// filtered Roadmap, the author's own reassignment does that — and carries the
// card as it now is; only a card deleted outright arrives as a bare uid. The
// drawer closes for the second alone.
describe("a DELETED frame", () => {
  it("is a card gone only when it carries nothing but its uid", () => {
    expect(cardIsGone({ type: "DELETED", object: { metadata: { uid: "t" }, spec: { title: "" } } })).toBe(true);
    expect(cardIsGone({ type: "DELETED", object: { metadata: { uid: "t" }, spec: { title: "Invoices" } } })).toBe(false);
    expect(cardIsGone({ type: "MODIFIED", object: { metadata: { uid: "t" }, spec: { title: "" } } })).toBe(false);
  });
});

// O13 — a write that can move where a card stands (own, a team, a parent)
// is answered with the card as the server now places it: the drawer's board
// takes its team, the team it stands under, its customers, its sprint and
// its chain from that answer rather than keeping what it guessed.
describe("the answer to a placing write", () => {
  it("carries the team the card stands under and the sprint it was moved to", () => {
    const answer = card({ team: undefined, effectiveTeam: "docs", effectiveCustomers: ["cu1"], sprintStart: "2026-01-07", parent: "e", lineage: [] });
    expect(placementAnswer(answer)).toEqual({
      team: undefined,
      effectiveTeam: "docs",
      effectiveCustomers: ["cu1"],
      sprintStart: "2026-01-07",
      parent: "e",
      lineage: [],
    });
  });
});

// Esc closes the drawer — but not when something inside it took the key
// first: an add form's Esc cancels the form and leaves the drawer open.
describe("Esc in the drawer", () => {
  it("closes it unless a control inside took it, or the title or a dialog is open", () => {
    expect(drawerEscapes({ key: "Escape", defaultPrevented: false }, false)).toBe(true);
    expect(drawerEscapes({ key: "Escape", defaultPrevented: true }, false)).toBe(false);
    expect(drawerEscapes({ key: "Escape", defaultPrevented: false }, true)).toBe(false);
    expect(drawerEscapes({ key: "Enter", defaultPrevented: false }, false)).toBe(false);
  });
});

// THE DRAWER'S TREE takes a card made in another tab: an ADDED frame for a
// card whose parent chain, through the cards the tree holds, reaches the
// card the drawer shows. Anything else is another tree's.
describe("a card made elsewhere under the drawer's card", () => {
  const tree = [card({ itemId: "e", type: "epic" }), card({ itemId: "t1", parent: "e" })];
  it("joins when its chain reaches the drawer's card", () => {
    expect(joinsTree(tree, card({ itemId: "new", parent: "e" }), "e")).toBe(true);
    expect(joinsTree(tree, card({ itemId: "sub", parent: "t1" }), "e")).toBe(true);
  });
  it("does not when its chain ends elsewhere, or it is already there", () => {
    expect(joinsTree(tree, card({ itemId: "far", parent: "other" }), "e")).toBe(false);
    expect(joinsTree(tree, card({ itemId: "root" }), "e")).toBe(false);
    expect(joinsTree(tree, card({ itemId: "t1", parent: "e" }), "e")).toBe(false);
    // A cycle a direct writer made ends the walk.
    const loop = [card({ itemId: "a", parent: "b" }), card({ itemId: "b", parent: "a" })];
    expect(joinsTree(loop, card({ itemId: "n", parent: "a" }), "e")).toBe(false);
  });
});

// BACK never lands on a deleted card: a card deleted outright leaves the
// drawer's history as it leaves the drawer.
describe("the drawer's history", () => {
  it("drops a card deleted outright", () => {
    const stack = [card({ itemId: "a" }), card({ itemId: "b" }), card({ itemId: "a" })];
    expect(withoutDeleted(stack, "a").map((c) => c.itemId)).toEqual(["b"]);
    expect(withoutDeleted(stack, "z")).toBe(stack);
  });
});
