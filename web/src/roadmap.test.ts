import { describe, expect, it } from "vitest";

import {
  allowedParentKinds,
  barOf,
  buildTree,
  daysBetween,
  kindOf,
  place,
  planningRemoval,
  rangeFor,
  rows,
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
