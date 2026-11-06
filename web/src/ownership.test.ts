import { describe, expect, it } from "vitest";

import {
  customerChips,
  customerPasses,
  effectiveCustomersOf,
  effectiveTeamOf,
  inheritedTeamOf,
  ownerFilterOptions,
  storedRoadmapFilters,
} from "./ownership";
import type { Card } from "./providers/types";

// docs/design/ownership.md, mirrored for what the drawer draws at once: the
// effective team (O3) and customers (O4) are the server's
// (status.effectiveTeam / effectiveCustomers), and the board works them out
// the same way from the cards it holds, so an edit to an ancestor shows on
// the cards below before the server says so.

const card = (over: Partial<Card>): Card => ({ itemId: "c", title: "x", assignees: [], progress: 0, ...over }) as Card;
const cards = [
  card({ itemId: "g", title: "Grow", type: "goal", team: "portal", customers: ["hidora"] }),
  card({ itemId: "e", title: "Billing", type: "epic", parent: "g", customers: ["acme"] }),
  card({ itemId: "t", title: "Receipts", parent: "e", customers: ["acme", "zeta"] }),
  card({ itemId: "own", title: "Own team", parent: "e", team: "cozystack" }),
  card({ itemId: "r", title: "review: Receipts", reviewOf: "t", team: "portal" }),
];

describe("the effective team (O3)", () => {
  it("is the card's own, else the nearest ancestor's", () => {
    expect(effectiveTeamOf(cards[2], cards)).toBe("portal");
    expect(effectiveTeamOf(cards[3], cards)).toBe("cozystack");
    expect(effectiveTeamOf(cards[1], cards)).toBe("portal");
  });

  it("says where an inherited team comes from, and nothing for a card's own", () => {
    expect(inheritedTeamOf(cards[2], cards)).toEqual({ team: "portal", from: "Grow" });
    expect(inheritedTeamOf(cards[3], cards)).toBeNull();
  });

  it("is none on a project card, whatever an old file says (H4)", () => {
    expect(effectiveTeamOf(card({ itemId: "slot", scope: "Security", project: "core", team: "ops" }), cards)).toBe("");
    expect(inheritedTeamOf(card({ itemId: "slot", scope: "Security", project: "core" }), cards)).toBeNull();
  });

  it("falls back to the server's when the ancestors are not on the board", () => {
    expect(effectiveTeamOf(card({ itemId: "lonely", parent: "gone", effectiveTeam: "ops" }), cards)).toBe("ops");
  });
});

describe("the effective customers (O4, O6)", () => {
  it("are every ancestor's and the card's own, ancestors first, each once", () => {
    expect(effectiveCustomersOf(cards[2], cards)).toEqual(["hidora", "acme", "zeta"]);
    expect(effectiveCustomersOf(cards[3], cards)).toEqual(["hidora", "acme"]);
  });

  it("are a review card's task's", () => {
    expect(effectiveCustomersOf(cards[4], cards)).toEqual(["hidora", "acme", "zeta"]);
  });

  it("fall back to the server's when the ancestors are not on the board", () => {
    expect(effectiveCustomersOf(card({ itemId: "l", parent: "gone", effectiveCustomers: ["x"] }), cards)).toEqual(["x"]);
  });

  it("are the server's for a review card whose task the board does not hold", () => {
    // The Team board holds a review card without its task (another team's):
    // the task's customers are the server's to say, not "none".
    const review = card({ itemId: "r2", reviewOf: "elsewhere", team: "portal", effectiveCustomers: ["hidora"] });
    expect(effectiveCustomersOf(review, cards)).toEqual(["hidora"]);
  });
});

// The drawer's Customers field: the card's own are chips it can remove; the
// inherited ones are grey, say which ancestor they come from, and cannot be
// removed there.
describe("the customer chips", () => {
  it("are the own ones removable and the inherited ones from their ancestor", () => {
    expect(customerChips(cards[2], cards)).toEqual([
      { id: "hidora", from: "Grow" },
      { id: "acme" },
      { id: "zeta" },
    ]);
  });
});

// The Roadmap's filters are remembered per viewer: what was stored reads
// back, and anything else is no filter.
describe("the remembered Roadmap filters", () => {
  it("reads back what was stored, and nothing else", () => {
    expect(storedRoadmapFilters('{"teams":["portal"],"customers":["acme"],"owners":["ann"]}')).toEqual({
      teams: ["portal"],
      customers: ["acme"],
      owners: ["ann"],
    });
    expect(storedRoadmapFilters(null)).toEqual({ teams: [], customers: [], owners: [] });
    expect(storedRoadmapFilters("not json")).toEqual({ teams: [], customers: [], owners: [] });
  });
});

// O8 — the Owner filter is the Roadmap's one person filter: it offers the
// board's people, every plan item's owner, every task's assignee and
// whoever is already chosen (a person who left the board stays choosable to
// be taken off), once each, sorted.
describe("the owner filter's options", () => {
  it("are the members, the plan owners, the assignees and the selection", () => {
    const held = [
      card({ itemId: "g", type: "goal", owner: "gwen" }),
      card({ itemId: "t", parent: "g", assignees: ["bob", "ann"] }),
    ];
    expect(ownerFilterOptions(["ann", "carl"], held, ["zed"])).toEqual(["ann", "bob", "carl", "gwen", "zed"]);
  });

  // The listing a filtered Roadmap holds is the filter's answer, so the
  // people on it shrink to the ones chosen; the options must not shrink with
  // it, or the filter could only ever be narrowed.
  it("keep the people the board has shown before, though the filtered listing no longer holds them", () => {
    const filtered = [card({ itemId: "t", assignees: ["ann"] })];
    expect(ownerFilterOptions([], filtered, ["ann"], ["bob", "gwen"])).toEqual(["ann", "bob", "gwen"]);
  });
});

// O11 — the day boards' Customer filter, mirrored for what a board holds: a
// card passes when one of its EFFECTIVE customers (its own plus every
// ancestor's, a review card's its task's) is chosen; nothing chosen passes
// everything. The server applies the same rule to the listing and the watch.
describe("the day boards' customer filter", () => {
  it("passes everything while nothing is chosen", () => {
    expect(customerPasses(cards[3], cards, null)).toBe(true);
    expect(customerPasses(cards[3], cards, [])).toBe(true);
  });

  it("matches by effective customers, an ancestor's included", () => {
    expect(customerPasses(cards[3], cards, ["hidora"])).toBe(true); // the goal's
    expect(customerPasses(cards[3], cards, ["zeta"])).toBe(false); // t's own, not own's
    expect(customerPasses(cards[2], cards, ["zeta", "nobody"])).toBe(true);
    expect(customerPasses(cards[4], cards, ["zeta"])).toBe(true); // the review card: its task's
  });
});
