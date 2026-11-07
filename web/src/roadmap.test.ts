import { describe, expect, it } from "vitest";

import {
  DEFAULT_DEPTH,
  roadmapLines,
  DELETE_INTENT,
  allowedParentKinds,
  barOf,
  buildTree,
  childrenNote,
  daysBetween,
  DropWriteError,
  dropCommit,
  droppable,
  hierarchyChoices,
  isUnder,
  kindOf,
  place,
  placementFor,
  planningRemoval,
  planningRemovalPlan,
  previewCards,
  rangeFor,
  rows,
  zoneOf,
  dropTargetAt,
  dropMark,
  AIM_SLACK,
  type DropTarget,
  type RowBox,
  statusOf,
  ticks,
  visibleLevels,
  sectionOf,
  sectionsOf,
  writeDrop,
  PROJECT_CARDS_DEFAULT,
  storedProjectCards,
} from "./roadmap";
import type { Card } from "./providers/types";
import { resourceToCard, type CardResource } from "./api/resources";

// The Roadmap's rule file, case by case: the tree the listing makes, the
// rows a depth range draws, a row's status, a card's bar and where it lands.
// Mirrors the server's HasPlanningAncestor / HierarchyStatus for what it
// draws and what it says (hierarchy.md H3, H5); the geometry is the
// client's own.

const card = (over: Partial<Card>): Card =>
  ({ itemId: "c", title: "a card", assignees: [], progress: 0, ...over }) as Card;

const goal = card({ itemId: "g", title: "Grow", type: "goal", startDate: "2026-01-05", day: "2026-06-30" });
const ini = card({ itemId: "i", title: "Billing v2", type: "initiative", parent: "g", startDate: "2026-02-02", day: "2026-04-30" });
const epic = card({ itemId: "e", title: "Invoices", type: "epic", parent: "i", startDate: "2026-02-09", day: "2026-03-20" });
const task = card({
  itemId: "t", title: "Render PDF", parent: "e", startDate: "2026-02-10", day: "2026-02-14",
  lineage: [{ uid: "g", type: "goal", title: "Grow" }, { uid: "i", type: "initiative", title: "Billing v2" }, { uid: "e", type: "epic", title: "Invoices" }],
});
const sub = card({
  itemId: "s", title: "Fonts", parent: "t", startDate: "2026-02-11",
  lineage: [{ uid: "e", type: "epic", title: "Invoices" }, { uid: "t", title: "Render PDF" }],
});
const slot = card({ itemId: "p", title: "Q2 plan", scope: "Storage", project: "cozy", parent: "i" });
const loose = card({ itemId: "x", title: "Stray", parent: "nowhere" });

describe("kindOf", () => {
  it("is the planning type, a project card by its column, a subtask by the parent as a task, else a task", () => {
    expect(kindOf(goal)).toBe("goal");
    expect(kindOf(ini)).toBe("initiative");
    expect(kindOf(epic)).toBe("epic");
    expect(kindOf(task)).toBe("task");
    expect(kindOf(sub)).toBe("subtask");
    expect(kindOf(slot)).toBe("project");
  });
});

describe("buildTree", () => {
  it("nests every card under the parent the listing holds, planning roots first", () => {
    const tree = buildTree([task, sub, epic, ini, goal, slot]);
    expect(tree.map((n) => n.card.itemId)).toEqual(["g"]);
    const [g] = tree;
    expect(g.children.map((n) => n.card.itemId)).toEqual(["i"]);
    expect(g.children[0].children.map((n) => n.card.itemId)).toEqual(["e", "p"]);
    expect(g.children[0].children[0].children.map((n) => n.card.itemId)).toEqual(["t"]);
    expect(g.children[0].children[0].children[0].children.map((n) => n.card.itemId)).toEqual(["s"]);
  });

  it("keeps listing order among siblings and draws a card whose parent is not here as a root", () => {
    const a = card({ itemId: "a", parent: "e" });
    const b = card({ itemId: "b", parent: "e" });
    const tree = buildTree([epic, b, a, loose]);
    expect(tree.map((n) => n.card.itemId)).toEqual(["e", "x"]);
    expect(tree[0].children.map((n) => n.card.itemId)).toEqual(["b", "a"]);
  });
});

describe("rows and the depth range", () => {
  const tree = buildTree([goal, ini, epic, task, sub, slot]);
  const all = () => true;

  it("draws initiative to task by default: no goals, no subtasks, project cards with the tasks", () => {
    const ids = rows(tree, { from: "initiative", to: "task" }, all).map((r) => `${r.node.card.itemId}@${r.depth}`);
    expect(ids).toEqual(["i@0", "e@1", "t@2", "p@1"]);
  });

  it("stands a hidden level's children in its place, and cuts a level below the range with its subtree", () => {
    expect(visibleLevels({ from: "epic", to: "epic" }).has("task")).toBe(false);
    const ids = rows(tree, { from: "epic", to: "epic" }, all).map((r) => r.node.card.itemId);
    expect(ids).toEqual(["e"]);
  });

  it("goes goal to subtask when asked, whichever way the range is given", () => {
    const ids = rows(tree, { from: "subtask", to: "goal" }, all).map((r) => `${r.node.card.itemId}@${r.depth}`);
    expect(ids).toEqual(["g@0", "i@1", "e@2", "t@3", "s@4", "p@2"]);
  });

  it("draws a collapsed row and nothing under it, and says how many it folds", () => {
    const out = rows(tree, { from: "goal", to: "task" }, (id) => id !== "i");
    expect(out.map((r) => r.node.card.itemId)).toEqual(["g", "i"]);
    expect(out[1].foldable).toBe(2); // the epic and the project card
  });
});

describe("statusOf", () => {
  it("is the server's derived standing on a planning card, no items when it says nothing", () => {
    expect(statusOf(card({ type: "epic", hierarchy: { status: "in-progress", done: 1, total: 3 } }))).toBe("in-progress");
    expect(statusOf(card({ type: "epic", hierarchy: { status: "todo", done: 0, total: 2 } }))).toBe("todo");
    expect(statusOf(card({ type: "epic", hierarchy: { status: "no-items", done: 0, total: 0 } }))).toBe("no-items");
    expect(statusOf(card({ type: "goal" }))).toBe("no-items");
  });

  it("reads a task's own standing, in order: done, the stage holding it, to do before it moved, else in progress", () => {
    expect(statusOf(card({ progress: 100 }))).toBe("done");
    expect(statusOf(card({ stage: "done", progress: 40 }))).toBe("done");
    // A stage that holds the card says who it waits on, and beats the bar.
    expect(statusOf(card({ stage: "review", progress: 50 }))).toBe("review");
    expect(statusOf(card({ stage: "locked", progress: 10 }))).toBe("locked");
    expect(statusOf(card({ stage: "refuse", progress: 10 }))).toBe("refused");
    // To do while nothing has started; in progress once the bar has moved.
    expect(statusOf(card({ progress: 0 }))).toBe("todo");
    expect(statusOf(card({ progress: 5 }))).toBe("in-progress");
    expect(statusOf(card({ progress: 40 }))).toBe("in-progress");
    expect(statusOf(card({ stage: "recurrent", progress: 0 }))).toBe("todo");
    expect(statusOf(card({ stage: "recurrent", progress: 30 }))).toBe("in-progress");
    // Done wins over a held stage: a finished review is finished.
    expect(statusOf(card({ stage: "done", progress: 100 }))).toBe("done");
  });
});

describe("barOf", () => {
  it("spans start to day, is a point on one date, and is nothing on none", () => {
    expect(barOf(task)).toEqual({ start: "2026-02-10", end: "2026-02-14", point: false });
    expect(barOf(sub)).toEqual({ start: "2026-02-11", end: "2026-02-11", point: true });
    expect(barOf(card({ day: "2026-03-01" }))).toEqual({ start: "2026-03-01", end: "2026-03-01", point: true });
    expect(barOf(card({}))).toBeNull();
  });

  it("does not draw a plan that reads backwards: a point at its start", () => {
    expect(barOf(card({ startDate: "2026-03-10", day: "2026-03-01" }))).toEqual({
      start: "2026-03-10", end: "2026-03-10", point: true,
    });
  });
});

describe("the window", () => {
  it("opens two weeks before the anchor's Monday for three months and a year", () => {
    expect(rangeFor("3m", "2026-02-11")).toEqual({ from: "2026-01-26", to: "2026-04-27" });
    expect(rangeFor("1y", "2026-02-11")).toEqual({ from: "2026-01-26", to: "2027-01-26" });
  });

  it("fits every bar with a week of margin, and falls back to three months with none", () => {
    expect(rangeFor("fit", "2026-02-11", [barOf(task), barOf(epic), null])).toEqual({
      from: "2026-02-02", to: "2026-03-27",
    });
    expect(rangeFor("fit", "2026-02-11", [])).toEqual(rangeFor("3m", "2026-02-11"));
  });

  it("ticks weeks inside four months and months beyond", () => {
    const weekly = ticks({ from: "2026-02-04", to: "2026-02-25" });
    expect(weekly.map((t) => t.label)).toEqual(["9 Feb", "16 Feb", "23 Feb"]);
    const monthly = ticks({ from: "2026-01-26", to: "2026-07-01" });
    expect(monthly.map((t) => t.label)).toEqual(["Feb 2026", "Mar 2026", "Apr 2026", "May 2026", "Jun 2026", "Jul 2026"]);
  });

  it("places a bar as fractions of the window and clips what runs out of it", () => {
    const range = { from: "2026-02-01", to: "2026-02-10" }; // ten days
    expect(place({ start: "2026-02-03", end: "2026-02-04", point: false }, range)).toEqual({
      left: 0.2, width: 0.2, clippedLeft: false, clippedRight: false,
    });
    const clipped = place({ start: "2026-01-20", end: "2026-02-20", point: false }, range)!;
    expect(clipped.left).toBe(0);
    expect(clipped.width).toBe(1);
    expect(clipped.clippedLeft && clipped.clippedRight).toBe(true);
    expect(place({ start: "2026-03-01", end: "2026-03-02", point: false }, range)).toBeNull();
    expect(daysBetween("2026-02-28", "2026-03-01")).toBe(1);
  });
});

describe("the rest of the Roadmap's rules", () => {
  it("moves a deleted planning card's children up to its own parent, or to the top", () => {
    expect(planningRemoval([goal, ini, epic, task, slot], "i")).toEqual([
      { itemId: "e", parent: "g" },
      { itemId: "p", parent: "g" },
    ]);
    expect(planningRemoval([goal, ini], "g")).toEqual([{ itemId: "i", parent: undefined }]);
  });

  it("offers a parent of a higher level only — a task under anything, a goal under nothing", () => {
    expect(allowedParentKinds("goal")).toEqual([]);
    expect(allowedParentKinds("initiative")).toEqual(["goal"]);
    expect(allowedParentKinds("epic")).toEqual(["initiative", "goal"]);
    expect(allowedParentKinds("task")).toEqual(["epic", "initiative", "goal"]);
    expect(allowedParentKinds("project")).toEqual(["epic", "initiative", "goal"]);
  });
});

// The hierarchy's ×: the Team board's own question, with two answers. The
// delete answer must ask the server for the OFF-BOARD intent — without one
// the remove gesture defaults to the day boards' "unassign", which a planning
// card refuses (422), and the × looked like it did nothing. Pinned here as a
// plan the component only executes.
describe("the hierarchy's × and its two answers", () => {
  it("offers delete and detach under a parent, delete alone at the top", () => {
    expect(hierarchyChoices(ini)).toEqual(["delete", "detach"]);
    expect(hierarchyChoices(goal)).toEqual(["delete"]);
  });

  it("delete moves the children up and asks the remove gesture for off-board", () => {
    const plan = planningRemovalPlan([goal, ini, epic, task, slot], "i", "delete");
    expect(plan.gone).toBe(true);
    expect(plan.patches).toEqual([
      { itemId: "e", parent: "g" },
      { itemId: "p", parent: "g" },
    ]);
    expect(plan.request).toEqual({ kind: "remove", uid: "i", intent: "off-board" });
    expect(DELETE_INTENT).toBe("off-board");
  });

  it("detach clears the card's own parent and nothing under it moves", () => {
    const plan = planningRemovalPlan([goal, ini, epic, task, slot], "i", "detach");
    expect(plan.gone).toBe(false);
    expect(plan.patches).toEqual([{ itemId: "i", parent: undefined }]);
    expect(plan.request).toEqual({ kind: "unlink", uid: "i" });
  });

  // A task detached from its epic with no date and no sprint would stand on
  // no board: the server parks it (K1), and so does the optimistic patch.
  it("detaching a task with no place of its own parks it (K1)", () => {
    const loose = { itemId: "lt", title: "loose", assignees: [], parent: "e" } as unknown as typeof task;
    const plan = planningRemovalPlan([goal, ini, epic, loose], "lt", "detach");
    expect(plan.patches).toEqual([{ itemId: "lt", parent: undefined, parked: true }]);
  });

  it("says what deleting does to what sits under the card", () => {
    expect(childrenNote([goal, ini, epic, task, slot], "i")).toBe("The 2 cards under it move under «Grow».");
    expect(childrenNote([goal, ini], "g")).toBe("The card under it moves to the top.");
    expect(childrenNote([goal, ini], "i")).toBe("");
  });
});

// Drag and drop on the tree: where a drop lands, what may land where, and
// the listing as it would read after the drop (previewCards), which is what
// the drop commits. The level rule holds throughout: a
// card never changes its type by being dragged (H2).
describe("dragging a row on the Roadmap", () => {
  const all = [goal, ini, epic, task, sub, slot];
  const goal2 = card({ itemId: "g2", title: "Retain", type: "goal" });
  const ini2 = card({ itemId: "i2", title: "Support", type: "initiative", parent: "g2" });
  const task2 = card({ itemId: "t2", title: "Mail", parent: "e" });
  const wide = [...all, goal2, ini2, task2];
  const drawn = (cards = wide) => rows(buildTree(cards), { from: "goal", to: "subtask" }, () => true);

  it("reads the zone off the row's thirds: top before, middle into, bottom after", () => {
    expect(zoneOf(0.1)).toBe("before");
    expect(zoneOf(0.5)).toBe("into");
    expect(zoneOf(0.9)).toBe("after");
  });

  it("holds the zone it stands in a little past the boundary, both ways", () => {
    // Fresh, the boundaries are at 0.28 and 0.72.
    expect(zoneOf(0.3)).toBe("into");
    expect(zoneOf(0.7)).toBe("into");
    // Standing before, the pointer must go AIM_SLACK past 0.28 to read into…
    expect(zoneOf(0.28 + AIM_SLACK - 0.01, "before")).toBe("before");
    expect(zoneOf(0.28 + AIM_SLACK + 0.01, "before")).toBe("into");
    // …and standing into, as far back the other way to read before again.
    expect(zoneOf(0.28 - AIM_SLACK + 0.01, "into")).toBe("into");
    expect(zoneOf(0.28 - AIM_SLACK - 0.01, "into")).toBe("before");
    expect(zoneOf(0.72 + AIM_SLACK - 0.01, "into")).toBe("into");
    expect(zoneOf(0.72 + AIM_SLACK + 0.01, "into")).toBe("after");
    expect(zoneOf(0.72 - AIM_SLACK + 0.01, "after")).toBe("after");
    expect(zoneOf(0.72 - AIM_SLACK - 0.01, "after")).toBe("into");
  });

  it("lands before or after the hovered row among its parent's children, or under it", () => {
    const r = drawn();
    expect(placementFor(wide, r, "t2", "t", "before")).toEqual({ parent: "e", index: 0 });
    expect(placementFor(wide, r, "t2", "t", "into")).toEqual({ parent: "t", index: 1 }); // after its subtask
    // After an unfolded row with rows under it is the first place UNDER it.
    expect(placementFor(wide, r, "t2", "e", "after")).toEqual({ parent: "e", index: 0 });
    // After a leaf is the next place beside it.
    expect(placementFor(wide, r, "t", "t2", "after")).toEqual({ parent: "e", index: 1 });
    expect(placementFor(wide, r, "t", "t", "into")).toBeNull();
  });

  it("knows what is under what", () => {
    expect(isUnder(wide, "s", "g")).toBe(true);
    expect(isUnder(wide, "g", "s")).toBe(false);
    expect(isUnder(wide, "t2", "i2")).toBe(false);
  });

  it("holds the level rule: a parent of a higher level, the top for planning cards only", () => {
    expect(droppable(wide, "g2", null)).toBe(true);
    expect(droppable(wide, "g2", "g")).toBe(false); // a goal under nothing
    expect(droppable(wide, "i2", "g")).toBe(true);
    expect(droppable(wide, "i2", "e")).toBe(false);
    expect(droppable(wide, "e", "g2")).toBe(true);
    expect(droppable(wide, "e", "i2")).toBe(true);
    expect(droppable(wide, "e", "t")).toBe(false);
    expect(droppable(wide, "t2", "i2")).toBe(true);
    expect(droppable(wide, "t2", "g2")).toBe(true);
    expect(droppable(wide, "t2", null)).toBe(false); // a task at the top would leave the Roadmap
    expect(droppable(wide, "p", "e")).toBe(true);
    expect(droppable(wide, "p", "t")).toBe(false); // a project card is nobody's subtask
  });

  it("makes a subtask of a task dropped on a task, one level deep", () => {
    expect(droppable(wide, "t2", "t")).toBe(true);
    expect(droppable(wide, "t", "t2")).toBe(false); // t has a subtask of its own
    expect(droppable(wide, "t2", "s")).toBe(false); // s is a subtask
  });

  it("never drops a card on itself or under its own subtree", () => {
    expect(droppable(wide, "i", "i")).toBe(false);
    expect(droppable(wide, "i", "e")).toBe(false);
    expect(droppable(wide, "g", "t")).toBe(false);
  });

  it("previews the listing as it would read after the drop", () => {
    const after = previewCards(wide, "e", { parent: "i2", index: 0 });
    const moved = after.find((c) => c.itemId === "e")!;
    expect(moved.parent).toBe("i2");
    expect(moved.lineage).toEqual([
      { uid: "g2", type: "goal", title: "Retain" },
      { uid: "i2", type: "initiative", title: "Support" },
    ]);
    // The subtree follows by its own links: the task is still the epic's.
    const tree = buildTree(after);
    const g2 = tree.find((n) => n.card.itemId === "g2")!;
    expect(g2.children[0].children.map((n) => n.card.itemId)).toEqual(["e"]);
    expect(g2.children[0].children[0].children.map((n) => n.card.itemId)).toEqual(["t", "t2"]);
  });

  it("orders the moved card among its new siblings at the place asked", () => {
    const first = previewCards(wide, "t2", { parent: "e", index: 0 }).filter((c) => c.parent === "e");
    expect(first.map((c) => c.itemId)).toEqual(["t2", "t"]);
    const last = previewCards(wide, "t", { parent: "e", index: 5 }).filter((c) => c.parent === "e");
    expect(last.map((c) => c.itemId)).toEqual(["t2", "t"]);
    const top = previewCards(wide, "g2", { parent: null, index: 0 }).filter((c) => !c.parent);
    expect(top.map((c) => c.itemId)).toEqual(["g2", "g"]);
  });

  it("commits one parent write and one ordering move", () => {
    const toFirst = dropCommit(wide, "t2", { parent: "e", index: 0 });
    expect(toFirst.parentChanged).toBe(false);
    expect(toFirst.after).toBeNull();
    expect(toFirst.before).toBe("t");
    const under = dropCommit(wide, "e", { parent: "i2", index: 0 });
    expect(under.parentChanged).toBe(true);
    expect(under.parent).toBe("i2");
    expect(under.after).toBeNull();
    expect(under.before).toBeNull(); // the only child: nothing to order against
    const second = dropCommit(wide, "t2", { parent: "e", index: 1 });
    expect(second.after).toBe("t");
    expect(second.order.indexOf("t")).toBeLessThan(second.order.indexOf("t2"));
  });
});

describe("the sections below the tree", () => {
  const loosePlanned = card({ itemId: "w1", title: "Fix the build", team: "core", startDate: "2026-03-02", day: "2026-03-02" });
  const looseSprint = card({ itemId: "w2", title: "Write the post", team: "core", sprintStart: "2026-03-02" });
  const looseSub = card({ itemId: "w1s", title: "A step", parent: "w1", lineage: [{ uid: "w1", title: "Fix the build" }] });
  const parkedCore = card({ itemId: "b1", title: "Someday", team: "core", parked: true });
  const parkedNone = card({ itemId: "b2", title: "Nobody's shelf", parked: true });
  const orphanUnder = card({ itemId: "o1", title: "Under an epic I cannot see", parent: "hidden", lineage: [{ uid: "hidden", type: "epic", title: "Hidden" }] });

  // K1: a task is in work or in its team's backlog, and the sections are
  // exactly those two. There is no "unsorted" any more — the Triage strip it
  // drew is gone, and every door that left a task there parks it (K3).
  it("files a loose root in work or in its team's backlog, and in nothing else (K1)", () => {
    expect(sectionOf(goal)).toBe("tree");
    expect(sectionOf(task)).toBe("tree"); // under an epic
    expect(sectionOf(orphanUnder)).toBe("tree"); // its lineage names a planning card
    expect(sectionOf(loosePlanned)).toBe("in-work");
    expect(sectionOf(looseSprint)).toBe("in-work");
    expect(sectionOf(parkedCore)).toBe("backlog");
    // Parked wins over dates a direct writer left beside the flag: the
    // backlog is where it is drawn, once (board.InBacklog).
    expect(sectionOf(card({ itemId: "x", parked: true, startDate: "2026-03-02" }))).toBe("backlog");
  });

  it("reads a week left on a task as nothing (K7)", () => {
    const weekParked = { ...card({ itemId: "w3", parked: true }), week: "2026-03-02" } as Card;
    expect(sectionOf(weekParked)).toBe("backlog");
  });

  it("splits the forest's roots into the sections, the backlogs grouped by team, and a subtask rides its task", () => {
    const s = sectionsOf(buildTree([goal, ini, epic, task, loosePlanned, looseSub, parkedNone, parkedCore, looseSprint]));
    expect(s.tree.map((n) => n.card.itemId)).toEqual(["g"]);
    expect(s.inWork.map((n) => n.card.itemId)).toEqual(["w1", "w2"]);
    expect(s.inWork[0].children.map((n) => n.card.itemId)).toEqual(["w1s"]);
    expect(s.backlog.map((b) => [b.team, b.nodes.map((n) => n.card.itemId)])).toEqual([
      ["", ["b2"]],
      ["core", ["b1"]],
    ]);
    expect(s).not.toHaveProperty("unsorted");
  });
});

// A drop is two writes — the parent, then the place among the new siblings —
// and they can fail apart. When the PARENT was taken and only the move
// failed, the card IS under its new parent on the server: putting it back
// under the old one in the board, and then letting the reload snap it under
// the new one again, showed the reader the opposite of what happened. The
// writer says how far it got, and only what the server did not take is
// rolled back.
describe("writeDrop", () => {
  const commit = { parent: "e", parentChanged: true, after: "t1", before: null, order: [] };
  it("reports that the parent was written when only the move failed", async () => {
    const calls: string[] = [];
    const provider = {
      patchCard: async () => {
        calls.push("parent");
        return {} as never;
      },
      moveCard: async () => {
        calls.push("move");
        throw new Error("409");
      },
      moveCardBefore: async () => undefined,
    };
    const err = await writeDrop("t2", commit, provider).catch((e: unknown) => e);
    expect(calls).toEqual(["parent", "move"]);
    expect(err).toBeInstanceOf(DropWriteError);
    expect((err as DropWriteError).parentWritten).toBe(true);
  });
  it("reports nothing written when the parent itself was refused", async () => {
    const provider = {
      patchCard: async () => {
        throw new Error("422 parentLevel");
      },
      moveCard: async () => undefined,
      moveCardBefore: async () => undefined,
    };
    const err = await writeDrop("t2", commit, provider).catch((e: unknown) => e);
    expect((err as DropWriteError).parentWritten).toBe(false);
  });
  it("writes only the move when the parent did not change", async () => {
    const calls: string[] = [];
    const provider = {
      patchCard: async () => {
        calls.push("parent");
        return {} as never;
      },
      moveCard: async () => {
        calls.push("move");
      },
      moveCardBefore: async () => undefined,
    };
    await writeDrop("t2", { ...commit, parentChanged: false }, provider);
    expect(calls).toEqual(["move"]);
  });
});


// The Roadmap hides PROJECT cards (cards in a Scope column) unless the
// toolbar's "Project cards" toggle is on: OFF by default. Hiding only takes
// rows out of the drawing — the planning rows' counts and status come from
// the server and do not change — and every other card under the same epic
// stays where it is.
// The toggle's memory: OFF until the viewer turns it on, and a stored value
// read back as it was written.
describe("the project cards toggle", () => {
  it("is off by default and reads back what was stored", () => {
    expect(PROJECT_CARDS_DEFAULT).toBe(false);
    expect(storedProjectCards(null)).toBe(false);
    expect(storedProjectCards("1")).toBe(true);
    expect(storedProjectCards("0")).toBe(false);
  });
});

// The toggle on a REAL listing: the roadmap's wire cards, mapped the way the
// board maps them — an epic, a task under it and a project card under it
// (spec.scope, spec.parent the epic). The lines the table draws are one pure
// function of the listing, the depth and the folds (roadmapLines); the
// toggle is the listing's selector (viewquery, projectCards).
describe("the lines the Roadmap draws, with the project cards toggle", () => {
  const wire = (uid: string, spec: Partial<CardResource["spec"]>, lineage?: { uid: string; type: string; title: string }[]): CardResource =>
    ({
      kind: "Card",
      metadata: { uid },
      spec: { title: uid, assignees: [], progress: 0, ...spec },
      status: { complete: false, inProgress: false, ...(lineage ? { lineage } : {}) },
    }) as CardResource;
  const up = [{ uid: "e", type: "epic", title: "Billing v2" }];
  const listing = [
    wire("e", { title: "Billing v2", type: "epic" }),
    wire("t", { title: "Render PDF", parent: "e", team: "portal" }, up),
    wire("s", { title: "Q1 payments slot", parent: "e", scope: "Payments", project: "Core", team: "portal" }, up),
  ].map(resourceToCard);
  const titles = (cards: readonly Card[]) =>
    roadmapLines(cards, { depth: DEFAULT_DEPTH, expanded: () => true })
      .filter((l) => l.kind === "row")
      .map((l) => (l.kind === "row" ? `${l.row.depth}:${l.row.node.card.title}` : ""));

  // The toggle is the server's selector: off, the listing carries no
  // project card; on, it does, and it is drawn under its epic. The lines
  // draw what the listing holds and filter nothing.
  it("draws no project card when the listing has none", () => {
    expect(titles(listing.filter((c) => !c.scope))).toEqual(["0:Billing v2", "1:Render PDF"]);
  });

  it("draws a listed project card under its epic", () => {
    expect(titles(listing)).toEqual(["0:Billing v2", "1:Render PDF", "1:Q1 payments slot"]);
  });
});

// Aiming a drag. The target is read off the rows as they stood when the drag
// began — the list does not move under the pointer while it is dragged, the
// landing place is a mark drawn over it — and the pointer has to go a little
// past a boundary before the target changes, so a slow aim does not flicker
// between two readings.
describe("aiming a drag on the Roadmap", () => {
  const g = card({ itemId: "g", title: "Grow", type: "goal" });
  const i = card({ itemId: "i", title: "Billing", type: "initiative", parent: "g" });
  const e1 = card({ itemId: "e1", title: "Invoices", type: "epic", parent: "i" });
  const e2 = card({ itemId: "e2", title: "Payments", type: "epic", parent: "i" });
  const tasks = ["a", "b", "c"].flatMap((n) => [
    card({ itemId: `${n}1`, title: `${n}1`, parent: "e1" }),
    card({ itemId: `${n}2`, title: `${n}2`, parent: "e2" }),
  ]);
  const cards = [g, i, e1, e2, ...tasks];
  const drawn = rows(buildTree(cards), { from: "goal", to: "subtask" }, () => true);
  // The snapshot: one 30px box per row, in the order drawn at drag start.
  const H = 30;
  const boxes: RowBox[] = drawn.map((r, k) => ({ id: r.node.card.itemId, top: k * H, height: H }));
  const at = (id: string, fraction: number) => boxes.find((b) => b.id === id)!.top + fraction * H;
  const order = drawn.map((r) => r.node.card.itemId);
  const none = new Set<string>();

  it("draws the tree the cases below aim at", () => {
    expect(order).toEqual(["g", "i", "e1", "a1", "b1", "c1", "e2", "a2", "b2", "c2"]);
  });

  it("holds one target while the pointer moves within one zone of one row", () => {
    const seen = new Set<string>();
    let prev: DropTarget | null = null;
    for (let f = 0.35; f <= 0.65; f += 0.01) {
      prev = dropTargetAt(boxes, at("e2", f), prev, none);
      seen.add(JSON.stringify(prev));
    }
    expect([...seen]).toEqual([JSON.stringify({ overId: "e2", zone: "into" })]);
  });

  it("does not flip back and forth when the pointer wobbles a few px across a boundary", () => {
    // Into e2, then the pointer wobbles ±2px around the into/after line.
    let prev = dropTargetAt(boxes, at("e2", 0.5), null, none);
    const line = at("e2", 0.72);
    for (const dy of [-2, 2, -2, 2, -1, 1]) {
      prev = dropTargetAt(boxes, line + dy, prev, none);
      expect(prev).toEqual({ overId: "e2", zone: "into" });
    }
    // The same wobble around the line between two rows keeps the row.
    prev = dropTargetAt(boxes, at("b2", 0.9), null, none);
    const edge = at("c2", 0);
    for (const dy of [-2, 2, -2, 2]) {
      prev = dropTargetAt(boxes, edge + dy, prev, none);
      expect(prev).toEqual({ overId: "b2", zone: "after" });
    }
    // Going on past the slack, it does change.
    expect(dropTargetAt(boxes, edge + AIM_SLACK * H + 1, prev, none)).toEqual({ overId: "c2", zone: "before" });
  });

  it("reads every row once, top to bottom, under a slow sweep: no reading comes back", () => {
    // A 1px sweep from the first row to the last against the snapshot. Each
    // reading is a function of the snapshot and the pointer alone, so the
    // rows come in the order drawn and each zone of a row comes once.
    const trail: string[] = [];
    let prev: DropTarget | null = null;
    for (let y = 0; y < boxes.length * H; y++) {
      prev = dropTargetAt(boxes, y, prev, none);
      const key = prev ? `${prev.overId}:${prev.zone}` : "-";
      if (trail[trail.length - 1] !== key) {
        trail.push(key);
      }
    }
    expect(new Set(trail).size).toBe(trail.length);
    expect(trail.filter((k) => k.endsWith(":into")).map((k) => k.split(":")[0])).toEqual(order);
  });

  it("gives the same reading for the same pointer whatever was read before it", () => {
    // Away from any boundary, history does not matter: the snapshot decides.
    for (const prev of [null, { overId: "g", zone: "before" as const }, { overId: "c2", zone: "after" as const }]) {
      expect(dropTargetAt(boxes, at("e2", 0.5), prev, none)).toEqual({ overId: "e2", zone: "into" });
    }
  });

  it("reads nothing over the dragged row or the rows it carries", () => {
    const held = new Set(["e1", "a1", "b1", "c1"]);
    expect(dropTargetAt(boxes, at("e1", 0.5), null, held)).toBeNull();
    expect(dropTargetAt(boxes, at("b1", 0.5), null, held)).toBeNull();
    expect(dropTargetAt(boxes, at("e2", 0.5), null, held)).toEqual({ overId: "e2", zone: "into" });
  });

  it("reads the nearest row just off the ends of the tree, and nothing far from it", () => {
    expect(dropTargetAt(boxes, -5, null, none)).toEqual({ overId: "g", zone: "before" });
    expect(dropTargetAt(boxes, boxes.length * H + 5, null, none)).toEqual({ overId: "c2", zone: "after" });
    expect(dropTargetAt(boxes, -3 * H, null, none)).toBeNull();
    expect(dropTargetAt(boxes, (boxes.length + 3) * H, null, none)).toBeNull();
    expect(dropTargetAt([], 10, null, none)).toBeNull();
  });

  it("dropping a task into the middle of another epic files it under that epic, last", () => {
    const t = dropTargetAt(boxes, at("e2", 0.5), null, new Set(["b1"]))!;
    const placement = placementFor(cards, drawn, "b1", t.overId, t.zone)!;
    expect(placement).toEqual({ parent: "e2", index: 3 });
    expect(droppable(cards, "b1", placement.parent)).toBe(true);
    expect(dropCommit(cards, "b1", placement).parentChanged).toBe(true);
  });

  it("dropping a task between two tasks of another epic files it there", () => {
    const t = dropTargetAt(boxes, at("b2", 0.1), null, new Set(["a1"]))!;
    expect(t).toEqual({ overId: "b2", zone: "before" });
    expect(placementFor(cards, drawn, "a1", t.overId, t.zone)).toEqual({ parent: "e2", index: 1 });
  });

  it("marks the landing place over the rows, taking no room: a line in the gap or the row itself", () => {
    // Into a row: the row is marked.
    expect(dropMark(drawn, { overId: "e2", zone: "into" }, { parent: "e2", index: 3 })).toEqual({ row: "e2", edge: "inside", depth: 2 });
    // Before a row: a line on its top edge at its depth.
    expect(dropMark(drawn, { overId: "b2", zone: "before" }, { parent: "e2", index: 1 })).toEqual({ row: "b2", edge: "top", depth: 3 });
    // After a leaf: a line on its bottom edge at its depth.
    expect(dropMark(drawn, { overId: "c1", zone: "after" }, { parent: "e1", index: 3 })).toEqual({ row: "c1", edge: "bottom", depth: 3 });
    // After an unfolded row is the first place under it: the line steps in.
    expect(dropMark(drawn, { overId: "e2", zone: "after" }, { parent: "e2", index: 0 })).toEqual({ row: "e2", edge: "bottom", depth: 3 });
    expect(dropMark(drawn, { overId: "zz", zone: "after" }, { parent: "e2", index: 0 })).toBeNull();
  });
});
