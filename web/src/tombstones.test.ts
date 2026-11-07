import { describe, expect, it } from "vitest";

import { Tombstones } from "./tombstones";

// A card this tab deleted stays deleted on its board. The delete is several
// writes on the server (a plan item's children move up first), and a frame
// sent between them can still carry the card — arriving before the delete's
// answer, or after the DELETED. For a while after the delete such a frame is
// ignored; the DELETED itself always gets through. A delete the server
// refused lifts the mark at once, so the card comes back with the reload.
describe("the cards this tab deleted", () => {
  it("turns away a frame that would bring one back", () => {
    const t = new Tombstones(60_000);
    t.mark("epic", 0);
    expect(t.blocks("epic", "MODIFIED", 1_000)).toBe(true);
    expect(t.blocks("epic", "ADDED", 1_000)).toBe(true);
    expect(t.blocks("epic", "DELETED", 1_000)).toBe(false);
    expect(t.blocks("other", "MODIFIED", 1_000)).toBe(false);
  });

  it("forgets the mark after a while, and at once when the delete was refused", () => {
    const t = new Tombstones(60_000);
    t.mark("epic", 0);
    expect(t.blocks("epic", "MODIFIED", 60_001)).toBe(false);
    t.mark("task", 0);
    t.lift("task");
    expect(t.blocks("task", "MODIFIED", 1)).toBe(false);
  });
});
