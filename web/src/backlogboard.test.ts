import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { isSubtask } from "./subtasks";
import { describe, expect, it } from "vitest";

import {
  anchorFor,
  backlogList,
  dropPlan,
  dueLabel,
  dueOf,
  handedOverLocally,
  nearestPlanning,
  onBacklog,
  needsReviewer,
  openPlanning,
  orderWith,
  ownPlan,
  reviewOwnPlan,
  parentChain,
  shownTeams,
  takeOwner,
  spliceOrder,
  takenIntoWorkLocally,
  insertSpot,
  insertedOrder,
  backlogKey,
  compareBacklogKeys,
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

  it("lists no planning card, no review card a reviewer has", () => {
    expect(onBacklog(card({ type: "epic" }))).toBe(false);
    expect(onBacklog(card({ reviewOf: "c9", assignees: ["bob"] }))).toBe(false);
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
// ONE list: the backlogs of every selected team MIXED in one order — rank is
// board-global, so the merged BacklogOrder is just the board order of those
// cards — each card wearing a chip of its team.
describe("the one list (K2)", () => {
  // The chips are the Team board's, and what they select is what the board
  // lists (viewquery.viewQueries): no selection is every team AND the no-team
  // group, which has a backlog like any team.
  it("shows the selected teams, or every team and the no-team group when none is selected", () => {
    expect(shownTeams(["portal", "cozystack"], ["a", "b"])).toEqual(["portal", "cozystack"]);
    expect(shownTeams(null, ["a", "b"])).toEqual(["a", "b", ""]);
    expect(shownTeams(null, ["a", ""])).toEqual(["a", ""]);
  });

  const a1 = card({ itemId: "a1", team: "alpha" });
  const b1 = card({ itemId: "b1", team: "beta" });
  const a2 = card({ itemId: "a2", team: "alpha" });
  const none = card({ itemId: "n1" });
  const working = card({ itemId: "w", team: "alpha", parked: false, startDate: "2026-11-03" });

  it("mixes the selected teams' backlogs in the board's one order, not grouped by team", () => {
    expect(backlogList([a1, b1, a2, none], ["beta", "alpha"]).map((c) => c.itemId)).toEqual(["a1", "b1", "a2"]);
  });

  it("holds the no-team group's backlog like any team's", () => {
    expect(backlogList([a1, none], ["", "alpha"]).map((c) => c.itemId)).toEqual(["a1", "n1"]);
  });

  it("holds only the backlog's cards, and none of a team that is not selected", () => {
    expect(backlogList([a2, working, a1, b1], ["alpha"]).map((c) => c.itemId)).toEqual(["a2", "a1"]);
  });
});

// K6: the board is live. Every watch frame lands in the board's cards, and
// the list is recomputed from them — so a card leaves the moment a frame
// says it is no longer a parked open task of a shown team.
describe("what drops out on a watch frame (K4, K6)", () => {
  const before = [card({ itemId: "a1", team: "alpha" }), card({ itemId: "a2", team: "alpha" })];
  const after = (patch: Partial<Card>) =>
    backlogList([{ ...before[0], ...patch }, before[1]], ["alpha", "beta"]).map((c) => c.itemId);

  it("drops a task taken into work at once: unparked, dated, owned (K4)", () => {
    expect(after({ parked: false, startDate: "2026-11-03", day: "2026-11-03", assignees: ["kvaps"] })).toEqual(["a2"]);
  });

  it("drops a task finished by anybody", () => {
    expect(after({ progress: 100 })).toEqual(["a2"]);
  });

  it("keeps a task re-teamed to another shown team where it stands, still parked (K5)", () => {
    expect(after({ team: "beta" })).toEqual(["a1", "a2"]);
  });

  it("drops a task re-teamed out of the shown teams", () => {
    expect(after({ team: "gamma" })).toEqual(["a2"]);
  });

  it("drops a task filed under a task: it became a subtask", () => {
    expect(after({ parent: "a2" })).toEqual(["a2"]);
  });

  it("takes in a task parked by anybody, where the board's order puts it", () => {
    const parkedElsewhere = card({ itemId: "a0", team: "alpha" });
    expect(backlogList([parkedElsewhere, ...before], ["alpha"]).map((c) => c.itemId)).toEqual(["a0", "a1", "a2"]);
  });
});

// The list is a slice of the one board-wide order (rank is one list). A drop
// is kept locally at once — the list's new order spliced into the board's
// order in the slots its cards already held — and written as a NEIGHBOUR in
// the merged list, never a position: the server resolves the rank from it.
describe("ordering within the list (K5)", () => {
  it("takes the card out before putting it back, so a drag down lands where the pointer is", () => {
    expect(orderWith(["a", "b", "c", "d"], "a", 2)).toEqual(["b", "c", "a", "d"]);
    expect(orderWith(["a", "b", "c"], "c", 0)).toEqual(["c", "a", "b"]);
  });

  it("clamps a place past either end to that end", () => {
    expect(orderWith(["a", "b"], "a", 9)).toEqual(["b", "a"]);
    expect(orderWith(["a", "b"], "b", -3)).toEqual(["b", "a"]);
  });

  it("anchors after the card above it, or before the card below when it is first", () => {
    expect(anchorFor(["a", "x", "b"], "x")).toEqual({ after: "a" });
    expect(anchorFor(["x", "a", "b"], "x")).toEqual({ before: "a" });
  });

  it("has nothing to say for a card alone in the list, or one not in it", () => {
    expect(anchorFor(["x"], "x")).toBeNull();
    expect(anchorFor(["a", "b"], "x")).toBeNull();
  });

  it("splices the list's new order into the board's in the slots its cards held", () => {
    // Board order: a, p (not in the backlog), b, q, c. The list is a, b, c
    // and c is dragged to the top: the list's slots now hold c, a, b.
    expect(spliceOrder(["a", "p", "b", "q", "c"], ["c", "a", "b"])).toEqual(["c", "p", "a", "q", "b"]);
  });
});

// What a drop means: a reorder in the merged list, anchored on the
// neighbours there — whichever team they belong to, since the rank is one
// list. The team does not change by a drop: handing a card to another team
// is the owner picker's (ownPlan). A drop that changes nothing writes
// nothing.
describe("what a drop does (K5)", () => {
  const loose = (...ids: string[]) => ids.map((itemId) => card({ itemId, team: itemId === "b" ? "beta" : "alpha" }));
  it("reorders by the neighbours at the drop in the merged list, of any team", () => {
    expect(dropPlan("x", loose("a", "x", "b"), 3)).toEqual({ kind: "reorder", order: ["a", "b", "x"], anchor: { after: "b" } });
    expect(dropPlan("x", loose("a", "b", "x"), 0)).toEqual({ kind: "reorder", order: ["x", "a", "b"], anchor: { before: "a" } });
  });

  it("writes nothing for a card put back where it was", () => {
    expect(dropPlan("x", loose("a", "x", "b"), 1)).toEqual({ kind: "none" });
  });
});

// ONE ORDER, decided by the tree (K14): a drop moves a task among its
// SIBLINGS — the tasks under the same parent — which moves it on the Strategy board too. A drop whose neighbours are
// none of its siblings is not a place it can go: forbidden, nothing moves.
describe("a drop moves a task among its siblings only (K14)", () => {
  const under = (itemId: string, parent: string, over: Partial<Card> = {}) =>
    card({ itemId, parent, lineage: [{ uid: parent, type: "epic", title: parent }], ...over });
  const a1 = under("a1", "ea");
  const a2 = under("a2", "ea");
  const a3 = under("a3", "ea");
  const b1 = under("b1", "eb");
  const b2 = under("b2", "eb");
  const list = [a1, a2, a3, b1, b2];

  it("reorders within its parent, anchored on the sibling beside it", () => {
    // a3 to the top of ea's run: before a1.
    expect(dropPlan("a3", list, 0)).toEqual({ kind: "reorder", order: ["a3", "a1", "a2", "b1", "b2"], anchor: { before: "a1" } });
    // a1 down between a2 and a3: after a2.
    expect(dropPlan("a1", list, 1)).toEqual({ kind: "reorder", order: ["a2", "a1", "a3", "b1", "b2"], anchor: { after: "a2" } });
  });

  it("lands after the last sibling at the end of its run, even with another parent's task below", () => {
    // a1 dropped between a3 and b1: a3 is a sibling above it.
    expect(dropPlan("a1", list, 2)).toEqual({ kind: "reorder", order: ["a2", "a3", "a1", "b1", "b2"], anchor: { after: "a3" } });
  });

  it("is forbidden next to the tasks of another parent only", () => {
    // a1 between b1 and b2: neither neighbour is under ea.
    expect(dropPlan("a1", list, 3)).toEqual({ kind: "forbidden" });
    // b2 to the very top, above ea's run.
    expect(dropPlan("b2", list, 0)).toEqual({ kind: "forbidden" });
  });

  // A review's place follows its task's (K10): dragging it is no gesture —
  // every drop of a review is forbidden and writes nothing.
  it("never moves a review nobody has taken: its place is its task's", () => {
    const rA = card({ itemId: "rA", reviewOf: "x" });
    const rB = card({ itemId: "rB", reviewOf: "y" });
    const rC = card({ itemId: "rC", reviewOf: "z" });
    const list = [rA, rB, a1, rC];
    expect(dropPlan("rB", list, 0)).toEqual({ kind: "forbidden" });
    expect(dropPlan("rC", list, 1)).toEqual({ kind: "forbidden" });
    expect(dropPlan("rA", list, 2)).toEqual({ kind: "forbidden" });
    // And a task does not sit by a review as by a sibling.
    expect(dropPlan("a1", [rA, a1], 0)).toEqual({ kind: "forbidden" });
  });

  it("treats the tasks with no plan item above as one run of siblings", () => {
    const l1 = card({ itemId: "l1" });
    const l2 = card({ itemId: "l2", team: "beta" });
    expect(dropPlan("l2", [a1, l1, l2], 1)).toEqual({ kind: "reorder", order: ["a1", "l2", "l1"], anchor: { before: "l1" } });
    expect(dropPlan("l2", [a1, l1, l2], 0)).toEqual({ kind: "forbidden" });
  });
});

// The backlog reads in the STRATEGY's order (K14) — a mirror of
// board.BacklogKey, case for case with TestABacklogReadsInTheStrategysOrder:
// the path of places in the board's order from the root plan item down,
// roots level by level, tasks with no plan item after every planned one, the
// reviews nobody has taken in a group of their own on top (K10).
// (Priorities were removed: the order is the place in the Strategy board tree.)
describe("the Strategy board's order in the backlog (K14)", () => {
  const ref = (uid: string, type: "goal" | "initiative" | "epic"): LineageRef => ({ uid, type, title: uid });
  const G1 = ref("g1", "goal");
  const G2 = ref("g2", "goal");
  const I1 = ref("i1", "initiative");
  const E1 = ref("e1", "epic");
  const E2 = ref("e2", "epic");
  const IR = ref("i-root", "initiative");
  // The board's order — deliberately not the tree's.
  const boardOrder = ["loose-1", "t-e2", "g2", "e2", "t-g2", "g1", "loose-2", "i1", "e1", "t-e1-b", "t-e1-a", "t-i1", "i-root", "t-iroot"];
  const t = (itemId: string, lineage: LineageRef[], over: Partial<Card> = {}) => card({ itemId, lineage, parent: lineage.at(-1)?.uid, ...over });
  const cards = [
    t("loose-1", []),
    t("t-e2", [G1, I1, E2]),
    t("t-g2", [G2]),
    t("loose-2", []),
    t("t-e1-b", [G1, I1, E1]),
    t("t-e1-a", [G1, I1, E1]),
    t("t-i1", [G1, I1]),
    t("t-iroot", [IR]),
  ];

  it("lists the tasks in the Strategy board's draw order, the loose ones last", () => {
    expect(backlogList(cards, [""], boardOrder).map((c) => c.itemId)).toEqual([
      "t-g2", "t-e2", "t-e1-b", "t-e1-a", "t-i1", "t-iroot", "loose-1", "loose-2",
    ]);
  });

  // A `priority:` a card still carries (an older file) is no part of the
  // order: the Strategy board's order alone decides.
  it("reads the Strategy board's order alone, whatever priority a card once had", () => {
    const order = ["g1", "g2", "a", "b", "c", "loose"];
    const list = [t("c", [G2]), t("b", [G1], { priority: "P0" } as Partial<Card>), t("loose", []), t("a", [G1])];
    expect(backlogList(list, [""], order).map((c) => c.itemId)).toEqual(["a", "b", "c", "loose"]);
  });

  // Mirrors TestTheReviewsNobodyHasTakenStandFirst (K10): the reviews nobody
  // has taken stand in a group of their own on top, in their tasks' Strategy
  // order; the tasks follow in theirs.
  it("puts the reviews nobody has taken first, in their tasks' Strategy order", () => {
    const order = ["g1", "g2", "r-late", "r-loose", "r-early", "o-early", "t1", "o-late", "o-loose", "t0"];
    const list = [
      card({ itemId: "r-late", reviewOf: "o-late", lineage: [G1] }),
      card({ itemId: "r-loose", reviewOf: "o-loose" }),
      card({ itemId: "r-early", reviewOf: "o-early", lineage: [G1] }),
      t("t1", [G1]),
      t("t0", []),
    ];
    expect(backlogList(list, [""], order).map((c) => c.itemId)).toEqual(["r-early", "r-late", "r-loose", "t1", "t0"]);
  });

  // Mirrors TestAReviewOfASubtaskIsPlacedByThePlanningChain: a review of a
  // SUBTASK is placed by its planning chain (status.lineage carries no task)
  // and the subtask's own place.
  it("places a review of a subtask by its planning chain and the subtask's place", () => {
    const E = ref("e", "epic");
    const list = [card({ itemId: "r-sub", reviewOf: "s", lineage: [E] }), card({ itemId: "r-t2", reviewOf: "t2", lineage: [E] })];
    expect(backlogList(list, [""], ["e", "t", "t2", "s", "r-sub", "r-t2"]).map((c) => c.itemId)).toEqual(["r-t2", "r-sub"]);
  });

  it("re-sorts on a new board order (an Ordering frame): a goal moved up takes its tasks with it", () => {
    const moved = ["g1", "loose-1", "t-e2", "g2", "e2", "t-g2", "loose-2", "i1", "e1", "t-e1-b", "t-e1-a", "t-i1", "i-root", "t-iroot"];
    expect(backlogList(cards, [""], moved).map((c) => c.itemId).slice(0, 5)).toEqual(["t-e2", "t-e1-b", "t-e1-a", "t-i1", "t-g2"]);
  });

  it("places a card the order does not name yet after the ones it does, in the board's order", () => {
    const fresh = t("fresh", [G1, I1, E1]);
    const ids = backlogList([...cards, fresh], [""], boardOrder).map((c) => c.itemId);
    expect(ids.indexOf("fresh")).toBe(ids.indexOf("t-e1-a") + 1);
  });

  it("compares keys step by step, a key never before itself", () => {
    const pos = new Map(["g", "under-g", "e", "under-e"].map((u, i) => [u, i]));
    const G = ref("g", "goal");
    const E = ref("e", "epic");
    const ug = backlogKey(t("under-g", [G]), pos);
    const ue = backlogKey(t("under-e", [G, E]), pos);
    expect(compareBacklogKeys(ug, ue)).toBeLessThan(0);
    expect(compareBacklogKeys(ue, ug)).toBeGreaterThan(0);
    expect(compareBacklogKeys(ug, ug)).toBe(0);
  });
});

// ONE add box at the foot of the list. With one team selected it adds to
// that team and asks nothing; with several, a team selector stands beside it
// and starts on the FIRST selected team. The task is parked on the chosen
// team, last.
// THE INSERT "+": there is no add box at the foot of the list; every gap —
// above the first card, between two, below the last — has a "+" that types
// a card in RIGHT THERE. The create names its place in the same request
// (after the card above the gap, or before the card below it at the top),
// and the team it is parked on defaults to the card above's, else the first
// shown team; with one team shown there is nothing to choose.
describe("the insert +", () => {
  const list = [
    card({ itemId: "a", team: "portal" }),
    card({ itemId: "b", team: "cozystack" }),
  ];

  it("places a card typed between two right after the one above it", () => {
    expect(insertSpot(list, 1, ["portal", "cozystack"]).anchor).toEqual({ after: "a" });
    expect(insertSpot(list, 2, ["portal", "cozystack"]).anchor).toEqual({ after: "b" });
  });

  it("places a card typed above the first right before it", () => {
    expect(insertSpot(list, 0, ["portal", "cozystack"]).anchor).toEqual({ before: "a" });
  });

  it("names no place in an empty backlog: its one + adds the first card", () => {
    expect(insertSpot([], 0, ["portal"]).anchor).toBeNull();
  });

  it("starts the team on the card above's, when that team is shown", () => {
    expect(insertSpot(list, 2, ["portal", "cozystack"]).box).toEqual({
      teams: ["portal", "cozystack"],
      allowNoTeam: false,
      initialTeam: "cozystack",
    });
  });

  it("starts on the first shown team above the first card, or when the card above's team is not shown", () => {
    expect(insertSpot(list, 0, ["cozystack", "portal"]).box).toMatchObject({ initialTeam: "cozystack" });
    expect(insertSpot([card({ itemId: "x", team: "gone" })], 1, ["portal", "cozystack"]).box).toMatchObject({
      initialTeam: "portal",
    });
  });

  it("offers no choice with one team shown, the no-team group included", () => {
    expect(insertSpot(list, 1, ["portal"]).box).toEqual({ forcedTeam: "portal" });
    expect(insertSpot([], 0, [""]).box).toEqual({ forcedTeam: null });
  });

  it("offers the no-team group when it is shown, and starts on it below a no-team card", () => {
    expect(insertSpot([card({ itemId: "n", team: undefined })], 1, ["portal", ""]).box).toEqual({
      teams: ["portal"],
      allowNoTeam: true,
      initialTeam: null,
    });
  });

  // The board shows the new card in place at once: in the board-wide order,
  // right after the card above, or right before the card below at the top.
  it("draws the made card in its place in the board's order before the server says so", () => {
    expect(insertedOrder(["x", "a", "y", "b"], "n", { after: "a" })).toEqual(["x", "a", "n", "y", "b"]);
    expect(insertedOrder(["x", "a", "y", "b"], "n", { before: "a" })).toEqual(["x", "n", "a", "y", "b"]);
    expect(insertedOrder(["x", "a"], "n", null)).toEqual(["x", "a", "n"]);
    expect(insertedOrder(["x", "a", "n"], "n", { after: "x" })).toEqual(["x", "n", "a"]);
  });
});

// Each card shows its PARENT CHAIN on the right: every planning card above
// it, root first — goal, initiative, epic — each a way into that card's
// drawer. A task parent (a subtask's) is not part of it; a backlog card is
// never a subtask anyway.
describe("the parent chain", () => {
  const INIT: LineageRef = { uid: "i", type: "initiative", title: "Portugal" };

  it("is every planning ancestor, root first", () => {
    expect(parentChain(card({ lineage: [GOAL, INIT, EPIC] })).map((r) => r.uid)).toEqual(["g", "i", "e"]);
  });

  it("is empty for a task under no planning card", () => {
    expect(parentChain(card({}))).toEqual([]);
  });

  it("leaves out a task ancestor", () => {
    expect(parentChain(card({ lineage: [EPIC, { uid: "t", title: "a task" }] })).map((r) => r.uid)).toEqual(["e"]);
  });

  // Clicking a name opens that planning card's drawer: the backlog holds no
  // planning card, so it is fetched by uid and handed to the drawer.
  it("opens a planning card's drawer by fetching it", async () => {
    const opened: Card[] = [];
    const errors: string[] = [];
    const epic = card({ itemId: "e", title: "Billing v2", type: "epic" });
    await openPlanning({ getCard: (uid) => (uid === "e" ? Promise.resolve(epic) : Promise.reject(new Error("gone"))) }, "e", (c) => opened.push(c), (m) => errors.push(m));
    await openPlanning({ getCard: () => Promise.reject(new Error("gone")) }, "x", (c) => opened.push(c), (m) => errors.push(m));
    expect(opened).toEqual([epic]);
    expect(errors).toEqual(["gone"]);
  });

  // A REVIEW card's chain is the plan alone, as a task's: the task it
  // reviews is not a crumb (the drawer's "Reviews: «task»" leads there).
  it("does not end a review card's chain in the task it reviews", () => {
    const chain = parentChain(card({ reviewOf: "t", lineage: [GOAL, EPIC], reviewed: { uid: "t", title: "Invoices" } }));
    expect(chain.map((r) => r.uid)).toEqual(["g", "e"]);
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
// the server's status.dueDate.
describe("the tag and the due date", () => {
  it("is the nearest planning ancestor, not the root", () => {
    expect(nearestPlanning(card({ lineage: [GOAL, EPIC] }))).toEqual(EPIC);
  });

  it("is none for a task under no planning card", () => {
    expect(nearestPlanning(card({}))).toBeNull();
    expect(nearestPlanning(card({ lineage: [] }))).toBeNull();
  });

  // The due date is the SERVER's (status.dueDate: the nearest planning card's
  // end, never a farther one's — apiserver TestATaskIsDueWhenItsNearestPlanningCardEnds);
  // the board reads it and works nothing out of the lineage.
  it("is the due date the server sends, and nothing when it sends none", () => {
    expect(dueOf(card({ dueDate: "2026-12-18" }))).toBe("2026-12-18");
    expect(dueOf(card({ lineage: [GOAL, EPIC] }))).toBeUndefined();
    expect(dueOf(card({}))).toBeUndefined();
  });

  it("reads as a day and a month", () => {
    expect(dueLabel("2026-12-18")).toBe("18 Dec");
    expect(dueLabel("2027-01-05")).toBe("5 Jan");
  });
});

// K10: a task put on review with no reviewer makes a review card NOBODY has
// taken, parked in the team's backlog. The board PINS these on top, above
// the ranked tasks, oldest first, out of the drag; a person picked on one
// takes it into work, and it leaves the backlog at once.
describe("the reviews nobody has taken (K10)", () => {
  const waiting = (over: Partial<Card>) => card({ reviewOf: "t", title: "review: Invoices", ...over });

  it("needs a reviewer while open and nobody has it", () => {
    expect(needsReviewer(waiting({}))).toBe(true);
    expect(needsReviewer(waiting({ assignees: ["bob"] }))).toBe(false);
    expect(needsReviewer(waiting({ progress: 100 }))).toBe(false);
    expect(needsReviewer(card({}))).toBe(false);
  });

  it("is in the backlog parked, and a reviewer's review card is not", () => {
    expect(onBacklog(waiting({}))).toBe(true);
    expect(onBacklog(waiting({ assignees: ["bob"] }))).toBe(false);
  });

  // In the same list as the tasks, in the group on top of them (K10), and
  // never dragged: its place is its task's.
  it("stands in the group on top of the tasks, and does not move by a drop", () => {
    const t1 = card({ itemId: "t1", team: "alpha" });
    const t2 = card({ itemId: "t2", team: "beta" });
    const top = waiting({ itemId: "r-top", team: "alpha" });
    const dragged = waiting({ itemId: "r-low", team: "beta" });
    const list = backlogList([top, t1, dragged, t2], ["alpha", "beta"]);
    expect(list.map((c) => c.itemId)).toEqual(["r-top", "r-low", "t1", "t2"]);
    expect(dropPlan("r-top", list, 1)).toEqual({ kind: "forbidden" });
    expect(dropPlan("r-top", list, 2)).toEqual({ kind: "forbidden" });
  });

  it("drops out the moment a reviewer has it", () => {
    const r = waiting({ itemId: "r", team: "alpha" });
    const taken = { ...r, assignees: ["bob"], parked: false, startDate: "2026-11-03" };
    expect(backlogList([taken], ["alpha"])).toEqual([]);
  });

  it("takes a person as its reviewer, and nothing else", () => {
    expect(reviewOwnPlan({ team: "alpha", person: "bob" })).toBe("take");
    expect(reviewOwnPlan({ team: "beta", person: "" })).toBe("none");
    expect(reviewOwnPlan({ team: "", person: "" })).toBe("none");
  });
});

// TAKE is "I take this into work": confirmed, the card is the VIEWER's — own
// with the viewer as the person and the card's own team (a review nobody
// has taken makes the viewer its reviewer) — and it leaves the backlog.
describe("take", () => {
  it("is own with the viewer as the person, on the card's own team", () => {
    expect(takeOwner(card({ team: "alpha" }), "kvaps")).toEqual({ team: "alpha", person: "kvaps" });
    expect(takeOwner(card({ team: "beta", reviewOf: "t" }), "bob")).toEqual({ team: "beta", person: "bob" });
  });

  it("is a take into work, by the picker's own rule", () => {
    expect(ownPlan(card({ team: "alpha" }), takeOwner(card({ team: "alpha" }), "kvaps"))).toBe("take");
  });
});

// O13 — a backlog holds the parked tasks STANDING UNDER its team: one with no
// team of its own under an epic of portal is portal's, and not the no-team
// group's (the server's word, status.effectiveTeam, where the epic is not on
// the board).
describe("a backlog by the team a task stands under", () => {
  it("lists an inheriting task under its plan item's team", () => {
    const inherited = card({ itemId: "i", parked: true, parent: "e", lineage: [EPIC], effectiveTeam: "portal" });
    expect(backlogList([inherited], ["portal"]).map((c) => c.itemId)).toEqual(["i"]);
    expect(backlogList([inherited], [""]).map((c) => c.itemId)).toEqual([]);
  });

  it("opens the add box on the team the card above stands under", () => {
    const inherited = card({ itemId: "i", parked: true, parent: "e", lineage: [EPIC], effectiveTeam: "portal" });
    const spot = insertSpot([inherited], 1, ["", "portal"]);
    expect("initialTeam" in spot.box && spot.box.initialTeam).toBe("portal");
  });
});

// THE BACKLOG'S ORDER IS ONE RULE ON BOTH SIDES — the same fixture as
// board's TestTheBacklogOrderMatchesTheSharedFixture, read from the Go
// package's testdata. Each card carries what the server sends for it
// (status.lineage; apiserver's TestTheBacklogFixtureWireIsTheServersAnswer
// holds the fixture to that), so the client orders exactly the input it
// would get: reviews nobody has taken in their group on top, a review with a
// stray parent of its own, a review of a review.
describe("the backlog's order, shared with the server", () => {
  type Raw = Partial<Card> & { itemId: string };
  const fx = JSON.parse(
    readFileSync(fileURLToPath(new URL("../../pkg/board/testdata/backlog_order.json", import.meta.url)), "utf8"),
  ) as {
    cards: Raw[];
    wire: Record<string, { lineage?: string[]; subtask?: boolean }>;
    order: string[];
    subtask: Record<string, boolean>;
  };
  const cards = fx.cards.map((c) => {
    const w = fx.wire[c.itemId];
    const lineage: LineageRef[] | undefined = w?.lineage?.map((s) => {
      const [type, uid] = s.split(":");
      return { uid, type, title: uid } as LineageRef;
    });
    return card({
      parked: false,
      ...c,
      ...(lineage?.length ? { lineage } : {}),
      ...(w ? { subtask: w.subtask ?? false } : {}),
    });
  });

  it("reads the cards in the same order as board.BacklogOrder", () => {
    expect(backlogList(cards, ["alpha"], fx.cards.map((c) => c.itemId)).map((c) => c.itemId)).toEqual(fx.order);
  });

  // board's TestWhoRidesAParentTaskMatchesTheSharedFixture: a taken review
  // whose stray parent names a plan item is no subtask, and so stays on the
  // day boards that hide subtasks (teamgrid, meboard).
  it("reads who rides a parent task as board.IsSubtask does", () => {
    const got = Object.fromEntries(Object.keys(fx.subtask).map((id) => [id, isSubtask(cards.find((c) => c.itemId === id)!)]));
    expect(got).toEqual(fx.subtask);
  });
});
