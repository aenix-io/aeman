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

// A task's due date is the server's (status.dueDate), carried as it came.
describe("the due date survives the wire", () => {
  it("maps status.dueDate, and nothing when it is absent", () => {
    const withDue = { ...res({}), status: { complete: false, inProgress: false, dueDate: "2026-12-18" } } as CardResource;
    expect(resourceToCard(withDue).dueDate).toBe("2026-12-18");
    expect(resourceToCard(res({})).dueDate).toBeUndefined();
  });
});

// A review card names the card it reviews (status.reviewed), carried as it
// came, so the drawer and the backlog row can open it.
describe("the reviewed card survives the wire", () => {
  it("maps status.reviewed, and nothing when it is absent", () => {
    const rev = { ...res({ reviewOf: "t" }), status: { complete: false, inProgress: false, reviewed: { uid: "t", title: "Invoices" } } } as CardResource;
    expect(resourceToCard(rev).reviewed).toEqual({ uid: "t", title: "Invoices" });
    expect(resourceToCard(res({})).reviewed).toBeUndefined();
  });
});

// Ownership on the wire (ownership.md): the owner and customers as written,
// the effective team and customers as computed.
describe("ownership survives the wire", () => {
  it("maps spec.owner, spec.customers and the effective values", () => {
    const r = {
      ...res({ owner: "ann", customers: ["cu1"] } as never),
      status: { complete: false, inProgress: false, effectiveTeam: "portal", effectiveCustomers: ["cu0", "cu1"] },
    } as CardResource;
    const c = resourceToCard(r);
    expect(c.owner).toBe("ann");
    expect(c.customers).toEqual(["cu1"]);
    expect(c.effectiveTeam).toBe("portal");
    expect(c.effectiveCustomers).toEqual(["cu0", "cu1"]);
  });

  it("leaves them absent when the server sends none", () => {
    const c = resourceToCard(res({}));
    expect(c.owner).toBeUndefined();
    expect(c.customers).toBeUndefined();
  });
});

// What a card waits for (docs/design/dependencies.md) crosses the wire in
// both halves: its own list in spec, and the derived lock, its blockers,
// what waits for it and the customers that arrive through it in status.
describe("dependencies survive the wire", () => {
  it("maps the list, the lock, the blockers, the dependents and the customers they bring", () => {
    const card = resourceToCard({
      ...res({ dependsOn: ["a", "b"] }),
      status: {
        complete: false,
        inProgress: false,
        locked: true,
        blockedBy: [{ uid: "a", title: "Auth", type: "epic", via: "e" }],
        hiddenBlockers: 2,
        blocks: [{ uid: "l", title: "Launch" }],
        dependentCustomers: [{ customer: "acme", from: { uid: "l", title: "Launch" } }],
      },
    } as CardResource);
    expect(card.dependsOn).toEqual(["a", "b"]);
    expect(card.locked).toBe(true);
    expect(card.blockedBy).toEqual([{ uid: "a", title: "Auth", type: "epic", via: "e" }]);
    expect(card.hiddenBlockers).toBe(2);
    expect(card.blocks).toEqual([{ uid: "l", title: "Launch", type: undefined }]);
    expect(card.dependentCustomers).toEqual([{ customer: "acme", from: { uid: "l", title: "Launch", type: undefined } }]);
  });

  it("leaves a card that waits for nothing without any of it", () => {
    const card = resourceToCard(res({}));
    expect(card.dependsOn).toBeUndefined();
    expect(card.locked).toBeUndefined();
    expect(card.blockedBy).toBeUndefined();
    expect(card.blocks).toBeUndefined();
  });
});
