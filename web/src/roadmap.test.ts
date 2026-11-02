import { describe, expect, it } from "vitest";

import {
  DELETE_INTENT,
  allowedParentKinds,
  barOf,
  buildTree,
  childrenNote,
  daysBetween,
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
  statusOf,
  ticks,
  visibleLevels,
} from "./roadmap";
import type { Card } from "./providers/types";

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

  it("reads a task's own stage and bar", () => {
    expect(statusOf(card({ progress: 100 }))).toBe("done");
    expect(statusOf(card({ stage: "done", progress: 40 }))).toBe("done");
    expect(statusOf(card({ progress: 40 }))).toBe("in-progress");
    expect(statusOf(card({ stage: "review", progress: 50 }))).toBe("in-progress");
    expect(statusOf(card({ progress: 0 }))).toBe("todo");
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

  it("says what deleting does to what sits under the card", () => {
    expect(childrenNote([goal, ini, epic, task, slot], "i")).toBe("The 2 cards under it move under «Grow».");
    expect(childrenNote([goal, ini], "g")).toBe("The card under it moves to the top.");
    expect(childrenNote([goal, ini], "i")).toBe("");
  });
});

// Drag and drop on the tree: where a drop lands, what may land where, and
// the preview — the listing as it would read after the drop, which the tree
// draws before the pointer is released. The level rule holds throughout: a
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
