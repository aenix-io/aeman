import { describe, expect, it } from "vitest";

import { insertedOrder } from "./backlogboard";
import { applyOrder } from "./cardorder";

const ids = (cards: { itemId: string }[]) => cards.map((c) => c.itemId);
const c = (itemId: string) => ({ itemId });

// A card typed into a gap is placed when the create answers, and cards may
// have arrived meanwhile (a watch frame, another create). The order is then
// worked out over the board AS IT IS when it is applied — an updater over
// the current ids — so those cards keep their place; an order computed from
// an earlier snapshot would push every card it did not know to the end.
describe("applying an order to the board", () => {
  it("keeps a card that arrived meanwhile where it stands", () => {
    const now = [c("a"), c("x"), c("b"), c("n")];
    const got = applyOrder(now, (cur) => insertedOrder(cur, "n", { after: "a" }));
    expect(ids(got)).toEqual(["a", "n", "x", "b"]);
  });

  it("takes a plain list of ids as before, the unlisted after it", () => {
    expect(ids(applyOrder([c("a"), c("b"), c("z")], ["b", "a"]))).toEqual(["b", "a", "z"]);
  });
});
