import { describe, expect, it } from "vitest";

import {
  customerChips,
  bornCustomers,
  customerPasses,
  placedTeamOf,
  placerOf,
  sprintTeamOf,
  strategyOwnerCell,
  effectiveCustomersOf,
  effectiveTeamOf,
  inheritedTeamOf,
  ownerFilterOptions,
  storedStrategyFilters,
  storeStrategyFilters,
} from "./ownership";
import type { Card } from "./providers/types";
import { HIDE_DONE } from "./workstatus";

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

  it("falls back to the server's when the ancestors are not on the board", () => {
    expect(effectiveTeamOf(card({ itemId: "lonely", parent: "gone", effectiveTeam: "ops" }), cards)).toBe("ops");
  });
});

describe("the effective customers (O4, O6)", () => {
  it("are every ancestor's and the card's own, ancestors first, each once", () => {
    expect(effectiveCustomersOf(cards[2], cards)).toEqual(["hidora", "acme", "zeta"]);
    expect(effectiveCustomersOf(cards[3], cards)).toEqual(["hidora", "acme"]);
  });

  // dependencies.md: the work waiting for a card brings its customers, and
  // what a plan item gets that way flows down to the cards under it.
  it("include those the waiting work brings, the card's own and every ancestor's", () => {
    const waited = cards.map((c) =>
      c.itemId === "e" ? { ...c, dependentCustomers: [{ customer: "globex", from: { uid: "w", title: "Launch" } }] } : c,
    );
    expect(effectiveCustomersOf(waited[2], waited)).toEqual(["hidora", "acme", "zeta", "globex"]);
    const own = { ...cards[3], dependentCustomers: [{ customer: "initech", from: { uid: "w", title: "Launch" } }] };
    expect(effectiveCustomersOf(own, cards)).toEqual(["hidora", "acme", "initech"]);
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

  // A card work for a customer waits for is for that customer too
  // (dependencies.md): grey like an inherited one, from the nearest card
  // that waits for it — and named once when it arrives both ways.
  it("add the customers of the work that waits for the card, from the card that waits", () => {
    const waited = {
      ...cards[2],
      dependentCustomers: [
        { customer: "globex", from: { uid: "w", title: "Launch" } },
        { customer: "hidora", from: { uid: "w", title: "Launch" } },
      ],
    };
    expect(customerChips(waited, cards)).toEqual([
      { id: "hidora", from: "Grow" },
      { id: "globex", from: "Launch" },
      { id: "acme" },
      { id: "zeta" },
    ]);
  });
});

// The Strategy board's filters are remembered per viewer: what was stored reads
// back, and anything else is no filter.
describe("the remembered Strategy filters", () => {
  it("reads back what was stored, and nothing else", () => {
    expect(storedStrategyFilters('{"v":2,"teams":["portal"],"customers":["acme"],"owners":["ann"],"statuses":["backlog","in-progress"]}')).toEqual({
      teams: ["portal"],
      customers: ["acme"],
      owners: ["ann"],
      statuses: ["backlog", "in-progress"],
    });
    // A choice remembered before the Status filter existed reads with none.
    expect(storedStrategyFilters('{"teams":["portal"],"customers":[],"owners":[]}').statuses).toEqual([]);
    // A status this client does not know is no filter, not a filter on nothing.
    expect(storedStrategyFilters('{"statuses":["in-progress","later"]}').statuses).toEqual(["in-progress"]);
    // The new values read back like the old.
    expect(storedStrategyFilters('{"v":2,"statuses":["locked","no-items"]}').statuses).toEqual(["locked", "no-items"]);
    expect(storedStrategyFilters(null)).toEqual({ teams: [], customers: [], owners: [], statuses: [] });
    expect(storedStrategyFilters("not json")).toEqual({ teams: [], customers: [], owners: [], statuses: [] });
  });
});

// O14 — "Hide done" was stored as In progress + Backlog before Locked and No
// items existed, and that was the only way to say "everything but done"; it
// reads back as today's Hide done, so the empty and the waiting epics come
// back. A choice stored since (versioned) reads back as it was, the same
// pair included.
describe("a Hide done remembered from before Locked and No items", () => {
  it("reads back as today's Hide done", () => {
    expect(storedStrategyFilters('{"teams":["portal"],"customers":[],"owners":[],"statuses":["backlog","in-progress"]}')).toEqual({
      teams: ["portal"],
      customers: [],
      owners: [],
      statuses: [...HIDE_DONE],
    });
    expect(storedStrategyFilters('{"statuses":["in-progress","backlog"]}').statuses).toEqual([...HIDE_DONE]);
  });
  it("leaves any other old choice, and every versioned one, as it was", () => {
    expect(storedStrategyFilters('{"statuses":["in-progress"]}').statuses).toEqual(["in-progress"]);
    expect(storedStrategyFilters('{"statuses":["backlog","done","in-progress"]}').statuses).toEqual(["backlog", "done", "in-progress"]);
    expect(storedStrategyFilters('{"v":2,"statuses":["backlog","in-progress"]}').statuses).toEqual(["backlog", "in-progress"]);
  });
  it("is stored versioned, and reads back unchanged", () => {
    const f = { teams: ["ops"], customers: [], owners: ["ann"], statuses: ["backlog", "in-progress"] };
    const raw = storeStrategyFilters(f);
    expect(JSON.parse(raw)).toMatchObject({ v: 2 });
    expect(storedStrategyFilters(raw)).toEqual(f);
  });
});

// O8 — the Owner filter is the Strategy board's one person filter: it offers the
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

  // The listing a filtered Strategy holds is the filter's answer, so the
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

// O3 on the Strategy board's Owner column: a card with no team of its own shows the
// team it takes from above, greyed, saying where from — the drawer's
// inherited value. A task someone holds, or one with its own team, shows
// what it has; a plan item's owner is its own only (O2), never inherited.
describe("the Strategy board's Owner cell", () => {
  it("shows a task's inherited team when it has no team and nobody", () => {
    const loose = card({ itemId: "x", title: "Task", parent: "e" });
    expect(strategyOwnerCell(loose, [...cards, loose], false)).toEqual({ owner: { team: "portal", person: "" }, from: "Grow" });
  });

  it("keeps a task's own team or person as they are", () => {
    expect(strategyOwnerCell(cards[3], cards, false)).toEqual({ owner: { team: "cozystack", person: "" } });
    const held = card({ itemId: "h", parent: "e", assignees: ["ann"] });
    expect(strategyOwnerCell(held, [...cards, held], false)).toEqual({ owner: { team: "", person: "ann" } });
  });

  it("shows a plan item's inherited team beside its own owner, never an inherited owner", () => {
    const epic = card({ ...cards[1], owner: "bob" });
    expect(strategyOwnerCell(epic, cards, true)).toEqual({ owner: { team: "portal", person: "bob" }, from: "Grow" });
    expect(strategyOwnerCell(cards[0], cards, true)).toEqual({ owner: { team: "portal", person: "" } });
  });

  it("falls back to the server's effective team when the ancestors are not held", () => {
    const lonely = card({ itemId: "l", parent: "gone", effectiveTeam: "ops" });
    expect(strategyOwnerCell(lonely, cards, false)).toEqual({ owner: { team: "ops", person: "" }, from: "" });
  });
});

// A card typed in while the Customer filter is on must not vanish at once:
// with ONE customer chosen it is born for that customer (the create carries
// it, as forcedTeam carries the team); with several there is no one to pick,
// so the board keeps the cards it made shown until the filter changes.
describe("a card typed in under the Customer filter", () => {
  it("is born for the one chosen customer, and for none otherwise", () => {
    expect(bornCustomers(["cu1"])).toEqual(["cu1"]);
    expect(bornCustomers([])).toBeUndefined();
    expect(bornCustomers(["cu1", "cu2"])).toBeUndefined();
  });

  it("stays shown while the filter stands, when the board keeps it", () => {
    const made = card({ itemId: "new", title: "New" });
    expect(customerPasses(made, [made], ["cu1", "cu2"])).toBe(false);
    expect(customerPasses(made, [made], ["cu1", "cu2"], new Set(["new"]))).toBe(true);
  });
});

// O13 — the team a card STANDS UNDER on the day boards and backlogs: its
// effective team (its own, else the nearest ancestor's; a review card's, its
// task's; a plan item's, its own). The server's
// board.PlacedTeam, mirrored for what a board holds; the server's word where
// the chain is not on the board.
describe("the team a card stands under", () => {
  it("is its own, else the plan item's above it", () => {
    expect(placedTeamOf(cards[2], cards)).toBe("portal");
    expect(placedTeamOf(cards[3], cards)).toBe("cozystack");
  });

  it("is a review card's task's when it has none of its own", () => {
    const review = card({ itemId: "rv", reviewOf: "t" });
    expect(placedTeamOf(review, [...cards, review])).toBe("portal");
  });

  it("is a plan item's own", () => {
    expect(placedTeamOf(cards[1], cards)).toBe("");
  });

  it("is the server's when the chain is not held", () => {
    expect(placedTeamOf(card({ itemId: "x", parent: "gone", effectiveTeam: "ops" }), cards)).toBe("ops");
    expect(placedTeamOf(card({ itemId: "rx", reviewOf: "gone", effectiveTeam: "ops" }), cards)).toBe("ops");
  });
});

// The placement read once per board render: one map of the held cards, the
// same answer card by card as placedTeamOf, which builds it per call.
describe("the board's placer", () => {
  it("answers as placedTeamOf does, card by card", () => {
    const review = card({ itemId: "rv", reviewOf: "t" });
    const held = [...cards, review, card({ itemId: "x", parent: "gone", effectiveTeam: "ops" })];
    const place = placerOf(held);
    for (const c of held) {
      expect(place(c)).toBe(placedTeamOf(c, held));
    }
  });
});

// O13 on the client's sprint reads: a sprint is resolved for the team a
// card stands under — the one a gesture names when it names one, else its
// own, else the plan item's above it — never the no-team group's for a task
// that inherits. null is the no-team group, as currentSprint takes it.
describe("the team a card's sprint is read by", () => {
  it("is the named team, else the one the card stands under", () => {
    expect(sprintTeamOf(cards[2], cards)).toBe("portal");
    expect(sprintTeamOf(cards[2], cards, "docs")).toBe("docs");
    expect(sprintTeamOf(cards[3], cards, "")).toBe("portal"); // own team taken off: the epic's
    expect(sprintTeamOf(card({ itemId: "loose" }), cards)).toBeNull();
  });
});

// The drawer's inherited team covers a review card: with no team of its own
// it counts under its task's, and says so.
describe("a review card's inherited team", () => {
  it("is its task's, from the task", () => {
    const review = card({ itemId: "rv", title: "review: Receipts", reviewOf: "t" });
    expect(inheritedTeamOf(review, [...cards, review])).toEqual({ team: "portal", from: "Receipts" });
  });

  it("is the server's when the task is not held, and none with a team of its own", () => {
    expect(inheritedTeamOf(card({ itemId: "rv", reviewOf: "gone", effectiveTeam: "ops" }), cards)).toEqual({ team: "ops", from: "" });
    expect(inheritedTeamOf(card({ itemId: "rv", reviewOf: "t", team: "ops" }), cards)).toBeNull();
  });
});
