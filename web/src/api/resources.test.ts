import { describe, expect, it } from "vitest";
import { STAGE_ORDER } from "../stages";
import { resourceToCard, type CardResource } from "./resources";

const res = (spec: Partial<CardResource["spec"]>): CardResource =>
  ({
    kind: "Card",
    metadata: { uid: "c1" },
    spec: { title: "x", assignees: [], progress: 0, ...spec },
  }) as CardResource;

// The stage table on the way in is a whitelist, and a key missing from it
// does not fall back — it becomes NO stage, and the card renders as ordinary
// work. `refuse` arrived from the server, was dropped here, and its black bar
// came out green. Every stage the app knows must survive the wire.
describe("the stage survives the wire", () => {
  for (const stage of STAGE_ORDER) {
    it(`keeps ${stage}`, () => {
      expect(resourceToCard(res({ stage })).stage).toBe(stage);
    });
  }

  it("leaves a card with no stage without one", () => {
    expect(resourceToCard(res({})).stage).toBeUndefined();
  });
});
