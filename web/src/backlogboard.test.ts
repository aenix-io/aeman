import { describe, expect, it } from "vitest";

import {
  anchorFor,
  backlogColumns,
  columnTeams,
  dropPlan,
  dueLabel,
  dueOf,
  handedOverLocally,
  nearestPlanning,
  onBacklog,
  orderWith,
  ownPlan,
  spliceOrder,
  takenIntoWorkLocally,
} from "./backlogboard";
import type { Card, LineageRef } from "./providers/types";

const card = (over: Partial<Card>): Card =>
  ({ itemId: "c", title: "a task", assignees: [], progress: 0, parked: true, ...over }) as Card;

const EPIC: LineageRef = { uid: "e", type: "epic", title: "Billing v2", end: "2026-12-18" };
const GOAL: LineageRef = { uid: "g", type: "goal", title: "Grow revenue", end: "2027-03-31" };

// K2: the backlog lists a team's PARKED OPEN TASKS and nothing else. Mirrors
// board.OnBacklog, case by case — the two must agree, or a card the server
// listed is hidden here (or one it never listed is drawn).
describe("what a backlog lists (K2)", () => {
  it("lists a parked task, and a parked task under an epic (a task like any other, H1)", () => {
    expect(onBacklog(card({}))).toBe(true);
    expect(onBacklog(card({ parent: "e", lineage: [EPIC] }))).toBe(true);
  });

  it("lists nothing that is in work: not parked means not in the backlog (K1)", () => {
    expect(onBacklog(card({ parked: false, startDate: "2026-11-03" }))).toBe(false);
    expect(onBacklog(card({ parked: undefined, sprintStart: "2026-11-02" }))).toBe(false);
    // Not even a task in neither place — the shape schema 3 parks: the flag
    // is what puts a card in a backlog, never the absence of dates.
    expect(onBacklog(card({ parked: undefined }))).toBe(false);
  });

  it("lists a parked task a direct writer left dates on: the flag decides, once (board.InBacklog)", () => {
    expect(onBacklog(card({ startDate: "2026-11-03", day: "2026-11-03" }))).toBe(true);
  });

  it("lists no planning card, no project card, no review card", () => {
    expect(onBacklog(card({ type: "epic" }))).toBe(false);
    expect(onBacklog(card({ scope: "Storage", project: "P" }))).toBe(false);
    expect(onBacklog(card({ reviewOf: "c9" }))).toBe(false);
  });

  it("lists no subtask on its own: it rides its parent", () => {
    expect(onBacklog(card({ parent: "t", lineage: [{ uid: "t", title: "a task" }] }))).toBe(false);
    // Under a parent this board cannot see, still a subtask by its parent's id.
    expect(onBacklog(card({ parent: "t" }))).toBe(false);
  });

  it("lists nothing finished, however it was finished", () => {
    expect(onBacklog(card({ progress: 100 }))).toBe(false);
    expect(onBacklog(card({ stage: "done" }))).toBe(false);
    expect(onBacklog(card({ progress: 90 }))).toBe(true);
  });
});

// Each selected team is a column of its own, in the order the chips put
// them, the no-team group included. Within a column the order is the order
// the cards stand in on the board — the listing's (BacklogOrder: by hand
// first, then oldest first), then every local move and Ordering frame since.
// Cards carry no rank on the wire, so the order on the board IS the mirror.
describe("the columns (K2)", () => {
  // The chips are the Team board's, and what they select is what the board
  // lists (viewquery.viewQueries): no selection is every team AND the no-team
  // group, which has a backlog like any team.
  it("are the selected teams, or every team and the no-team group when none is selected", () => {
    expect(columnTeams(["portal", "cozystack"], ["a", "b"])).toEqual(["portal", "cozystack"]);
    expect(columnTeams(null, ["a", "b"])).toEqual(["a", "b", ""]);
    expect(columnTeams(null, ["a", ""])).toEqual(["a", ""]);
  });

  const a1 = card({ itemId: "a1", team: "alpha" });
  const b1 = card({ itemId: "b1", team: "beta" });
  const a2 = card({ itemId: "a2", team: "alpha" });
  const none = card({ itemId: "n1" });
  const working = card({ itemId: "w", team: "alpha", parked: false, startDate: "2026-11-03" });

  it("draws one column per selected team, in the chips' order, empty ones too", () => {
    const cols = backlogColumns([a1, b1, a2, none], ["beta", "alpha", "gamma"]);
    expect(cols.map((c) => c.team)).toEqual(["beta", "alpha", "gamma"]);
    expect(cols.map((c) => c.cards.map((x) => x.itemId))).toEqual([["b1"], ["a1", "a2"], []]);
  });

  it("gives the no-team group a column like any team's", () => {
    const cols = backlogColumns([a1, none], ["", "alpha"]);
    expect(cols[0]).toEqual({ team: "", cards: [none] });
  });

  it("keeps the board's order within a column, and only the backlog's cards", () => {
    const cols = backlogColumns([a2, working, a1], ["alpha"]);
    expect(cols[0].cards.map((c) => c.itemId)).toEqual(["a2", "a1"]);
  });

  it("draws no column for a team that is not selected, and no card of one", () => {
    expect(backlogColumns([a1, b1], ["alpha"]).flatMap((c) => c.cards.map((x) => x.itemId))).toEqual(["a1"]);
  });
});

// K6: the board is live. Every watch frame lands in the board's cards, and
// the columns are recomputed from them — so a card leaves the moment a frame
// says it is no longer a parked open task of a shown team.
describe("what drops out on a watch frame (K4, K6)", () => {
  const before = [card({ itemId: "a1", team: "alpha" }), card({ itemId: "a2", team: "alpha" })];
  const after = (patch: Partial<Card>) =>
    backlogColumns([{ ...before[0], ...patch }, before[1]], ["alpha", "beta"]);

  it("drops a task taken into work at once: unparked, dated, owned (K4)", () => {
    const taken = after({ parked: false, startDate: "2026-11-03", day: "2026-11-03", assignees: ["kvaps"] });
    expect(taken[0].cards.map((c) => c.itemId)).toEqual(["a2"]);
  });

  it("drops a task finished by anybody", () => {
    expect(after({ progress: 100 })[0].cards.map((c) => c.itemId)).toEqual(["a2"]);
  });

  it("moves a task re-teamed to another shown team into that column, still parked (K5)", () => {
    const cols = after({ team: "beta" });
    expect(cols[0].cards.map((c) => c.itemId)).toEqual(["a2"]);
    expect(cols[1].cards.map((c) => c.itemId)).toEqual(["a1"]);
  });

  it("drops a task re-teamed out of the shown teams", () => {
    expect(after({ team: "gamma" }).flatMap((c) => c.cards.map((x) => x.itemId))).toEqual(["a2"]);
  });

  it("drops a task filed under a task: it became a subtask", () => {
    expect(after({ parent: "a2" })[0].cards.map((c) => c.itemId)).toEqual(["a2"]);
  });

  it("takes in a task parked by anybody, where the board's order puts it", () => {
    const parkedElsewhere = card({ itemId: "a0", team: "alpha" });
    const cols = backlogColumns([parkedElsewhere, ...before], ["alpha"]);
    expect(cols[0].cards.map((c) => c.itemId)).toEqual(["a0", "a1", "a2"]);
  });
});

// A column is a slice of the one board-wide order (rank is one list). A drop
// is kept locally at once — the column's new order spliced into the board's
// order in the slots its cards already held — and written as a NEIGHBOUR,
// never a position: the server resolves the rank from the anchor.
describe("ordering within a column (K5)", () => {
  it("takes the card out before putting it back, so a drag down lands where the pointer is", () => {
    expect(orderWith(["a", "b", "c", "d"], "a", 2)).toEqual(["b", "c", "a", "d"]);
    expect(orderWith(["a", "b", "c"], "c", 0)).toEqual(["c", "a", "b"]);
  });

  it("clamps a place past either end to that end", () => {
    expect(orderWith(["a", "b"], "a", 9)).toEqual(["b", "a"]);
    expect(orderWith(["a", "b"], "b", -3)).toEqual(["b", "a"]);
  });

  it("inserts a card that came from another column", () => {
    expect(orderWith(["a", "b"], "x", 1)).toEqual(["a", "x", "b"]);
  });

  it("anchors after the card above it, or before the card below when it is first", () => {
    expect(anchorFor(["a", "x", "b"], "x")).toEqual({ after: "a" });
    expect(anchorFor(["x", "a", "b"], "x")).toEqual({ before: "a" });
  });

  it("has nothing to say for a card alone in its column, or one not in it", () => {
    expect(anchorFor(["x"], "x")).toBeNull();
    expect(anchorFor(["a", "b"], "x")).toBeNull();
  });

  it("splices a column's new order into the board's in the slots its cards held", () => {
    // Board order: a, p (someone else's), b, q, c. The column is a, b, c and c
    // is dragged to the top: the column's slots now hold c, a, b.
    expect(spliceOrder(["a", "p", "b", "q", "c"], ["c", "a", "b"])).toEqual(["c", "p", "a", "q", "b"]);
  });
});

// What a drop means: within its own column, a reorder; on another team's
// column, a hand-over to that team — still parked (K5) — at the place it was
// dropped. A drop that changes nothing writes nothing.
describe("what a drop does (K5)", () => {
  it("reorders within the column by the neighbours at the drop", () => {
    expect(dropPlan("x", "alpha", "alpha", ["a", "x", "b"], 3)).toEqual({
      kind: "reorder",
      order: ["a", "b", "x"],
      anchor: { after: "b" },
    });
  });

  it("writes nothing for a card put back where it was", () => {
    expect(dropPlan("x", "alpha", "alpha", ["a", "x", "b"], 1)).toEqual({ kind: "none" });
  });

  it("hands the card to the column's team, placed among that column's cards", () => {
    expect(dropPlan("x", "alpha", "beta", ["b1", "b2"], 1)).toEqual({
      kind: "hand-over",
      team: "beta",
      order: ["b1", "x", "b2"],
      anchor: { after: "b1" },
    });
  });

  it("hands it to the no-team group like any team", () => {
    expect(dropPlan("x", "alpha", "", [], 0)).toEqual({
      kind: "hand-over",
      team: "",
      order: ["x"],
      anchor: null,
    });
  });
});

// The owner picker on a backlog card: a PERSON takes it into work (K4) — it
// leaves the backlog and lands on the Team and Me boards today; a team alone
// (or nobody) hands it to that team's backlog, still parked (K5, K3).
describe("what the owner picker does on a backlog card (K3, K4, K5)", () => {
  const c = card({ itemId: "x", team: "alpha" });

  it("takes it into work when a person is picked", () => {
    expect(ownPlan(c, { team: "alpha", person: "kvaps" })).toBe("take");
    expect(ownPlan(c, { team: "beta", person: "kvaps" })).toBe("take");
  });

  it("hands it over, still parked, when only a team is picked — or nobody", () => {
    expect(ownPlan(c, { team: "beta", person: "" })).toBe("hand-over");
    expect(ownPlan(c, { team: "", person: "" })).toBe("hand-over");
  });

  it("does nothing when the answer is where it already is", () => {
    expect(ownPlan(c, { team: "alpha", person: "" })).toBe("none");
  });
});

// The optimistic shapes, so the board shows the server's answer before the
// server has given it. Taking into work mirrors boardservice.ownByPerson on an
// unplanned card: today, the unplanned band, the team's sprint, the person,
// and out of the backlog.
describe("the optimistic shapes", () => {
  it("takes a task into work today, in the unplanned band and the team's sprint (K4)", () => {
    expect(takenIntoWorkLocally({ team: "alpha", person: "kvaps" }, "2026-11-03", "2026-11-02")).toEqual({
      team: "alpha",
      assignees: ["kvaps"],
      parked: false,
      startDate: "2026-11-03",
      day: "2026-11-03",
      zone: "yellow",
      sprintStart: "2026-11-02",
    });
  });

  it("starts the sprint today when the team has none yet", () => {
    expect(takenIntoWorkLocally({ team: "alpha", person: "kvaps" }, "2026-11-03", null).sprintStart).toBe(
      "2026-11-03",
    );
  });

  it("hands a task over still parked, nobody holding it (K5)", () => {
    expect(handedOverLocally("beta")).toEqual({ team: "beta", assignees: [], parked: true });
    expect(handedOverLocally("")).toEqual({ team: undefined, assignees: [], parked: true });
  });
});

// The card's tag is the nearest planning card above it — the last entry of
// the lineage that is a goal, an initiative or an epic — and its due date is
// that card's end.
describe("the tag and the due date", () => {
  it("is the nearest planning ancestor, not the root", () => {
    expect(nearestPlanning(card({ lineage: [GOAL, EPIC] }))).toEqual(EPIC);
  });

  it("is none for a task under no planning card", () => {
    expect(nearestPlanning(card({}))).toBeNull();
    expect(nearestPlanning(card({ lineage: [] }))).toBeNull();
  });

  it("is that planning card's end, and nothing when it has none", () => {
    expect(dueOf(card({ lineage: [GOAL, EPIC] }))).toBe("2026-12-18");
    expect(dueOf(card({ lineage: [GOAL, { ...EPIC, end: undefined }] }))).toBeUndefined();
    expect(dueOf(card({}))).toBeUndefined();
  });

  it("is not borrowed from a farther ancestor when the nearest has no end", () => {
    // The epic is what the task is for; the goal's end says nothing about
    // when the epic is due.
    expect(dueOf(card({ lineage: [GOAL, { uid: "e", type: "epic", title: "Billing v2" }] }))).toBeUndefined();
  });

  it("reads as a day and a month", () => {
    expect(dueLabel("2026-12-18")).toBe("18 Dec");
    expect(dueLabel("2027-01-05")).toBe("5 Jan");
  });
});
