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

// The Backlog board shows a task's due date as the end of the nearest planning
// card above it (docs/design/backlog.md, "The card"), and that end rides on
// the lineage entry. A mapper that dropped it would leave every backlog card
// without a due date and nothing failing.
describe("the lineage keeps each ancestor's end", () => {
  const withLineage = (lineage: NonNullable<CardResource["status"]>["lineage"]): CardResource =>
    ({ ...res({}), status: { complete: false, inProgress: false, lineage } }) as CardResource;

  it("maps an ancestor's end date", () => {
    const card = resourceToCard(
      withLineage([
        { uid: "g", type: "goal", title: "Grow" },
        { uid: "e", type: "epic", title: "Billing", end: "2026-12-18" },
      ]),
    );
    expect(card.lineage).toEqual([
      { uid: "g", type: "goal", title: "Grow" },
      { uid: "e", type: "epic", title: "Billing", end: "2026-12-18" },
    ]);
  });

  it("leaves an ancestor with no end without one, not an empty string", () => {
    const card = resourceToCard(withLineage([{ uid: "e", type: "epic", title: "Billing", end: "" }]));
    expect(card.lineage?.[0]).not.toHaveProperty("end");
  });
});

// K9: the Triage status fields are gone from the wire, and a task has no
// week (K7) — nothing of either reaches the card model.
describe("nothing of the Triage board reaches the card", () => {
  it("carries no triage, triageWeek, due or week", () => {
    const stale = { ...res({}), status: { complete: false, inProgress: false, triage: true, triageWeek: "2026-09-07", due: ["2026-09-14"] } } as unknown as CardResource;
    (stale.spec as Record<string, unknown>).week = "2026-09-07";
    const card = resourceToCard(stale) as unknown as Record<string, unknown>;
    for (const gone of ["triage", "triageWeek", "due", "week"]) {
      expect(card).not.toHaveProperty(gone);
    }
  });
});
