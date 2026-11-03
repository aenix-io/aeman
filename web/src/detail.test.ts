import { describe, expect, it } from "vitest";

import { followFrame, followPatch } from "./detail";
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
