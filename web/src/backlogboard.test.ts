import { describe, expect, it } from "vitest";

import {
  addBox,
  anchorFor,
  backlogList,
  backlogSections,
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

  it("lists no planning card, no project card, no review card a reviewer has", () => {
    expect(onBacklog(card({ type: "epic" }))).toBe(false);
    expect(onBacklog(card({ scope: "Storage", project: "P" }))).toBe(false);
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
  it("reorders by the neighbours at the drop in the merged list, of any team", () => {
    expect(dropPlan("x", ["a", "x", "b"], 3)).toEqual({ kind: "reorder", order: ["a", "b", "x"], anchor: { after: "b" } });
    expect(dropPlan("x", ["a", "b", "x"], 0)).toEqual({ kind: "reorder", order: ["x", "a", "b"], anchor: { before: "a" } });
  });

  it("writes nothing for a card put back where it was", () => {
    expect(dropPlan("x", ["a", "x", "b"], 1)).toEqual({ kind: "none" });
  });
});

// ONE add box at the foot of the list. With one team selected it adds to
// that team and asks nothing; with several, a team selector stands beside it
// and starts on the FIRST selected team. The task is parked on the chosen
// team, last.
describe("the add box", () => {
  it("adds to the only selected team and offers no selector", () => {
    expect(addBox(["portal"])).toEqual({ forcedTeam: "portal" });
    expect(addBox([""])).toEqual({ forcedTeam: null });
  });

  it("offers the selected teams, starting on the first", () => {
    expect(addBox(["cozystack", "portal"])).toEqual({ teams: ["cozystack", "portal"], allowNoTeam: false, initialTeam: "cozystack" });
  });

  it("offers the no-team group when it is selected, and starts on it when it is first", () => {
    expect(addBox(["portal", ""])).toEqual({ teams: ["portal"], allowNoTeam: true, initialTeam: "portal" });
    expect(addBox(["", "portal"])).toEqual({ teams: ["portal"], allowNoTeam: true, initialTeam: null });
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

  it("is pinned above the ranked tasks, oldest first, whatever the board's order", () => {
    const t1 = card({ itemId: "t1", team: "alpha" });
    const t2 = card({ itemId: "t2", team: "beta" });
    const rNew = waiting({ itemId: "r-new", team: "alpha", createdAt: "2026-11-03T10:00:00Z" });
    const rOld = waiting({ itemId: "r-old", team: "beta", createdAt: "2026-11-02T10:00:00Z" });
    const { reviews, tasks } = backlogSections([t1, rNew, t2, rOld], ["alpha", "beta"]);
    expect(reviews.map((c) => c.itemId)).toEqual(["r-old", "r-new"]);
    expect(tasks.map((c) => c.itemId)).toEqual(["t1", "t2"]);
  });

  it("drops out the moment a reviewer has it", () => {
    const r = waiting({ itemId: "r", team: "alpha" });
    const taken = { ...r, assignees: ["bob"], parked: false, startDate: "2026-11-03" };
    expect(backlogSections([taken], ["alpha"]).reviews).toEqual([]);
  });

  it("takes a person as its reviewer, and nothing else", () => {
    expect(reviewOwnPlan({ team: "alpha", person: "bob" })).toBe("take");
    expect(reviewOwnPlan({ team: "beta", person: "" })).toBe("none");
    expect(reviewOwnPlan({ team: "", person: "" })).toBe("none");
  });
});
