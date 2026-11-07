import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

import {
  DEFAULT_DEPTH,
  roadmapLines,
  dropShown,
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
  pointerContentY,
  boxesInContent,
  revealStep,
  AIM_SLACK,
  type DropTarget,
  type RowBox,
  statusOf,
  ticks,
  visibleLevels,
  sectionOf,
  sectionsOf,
  writeDrop,
  childLevels,
  subtreeRows,
  parentChainOf,
  pickerMatches,
  cardsAfterDelete,
  parentOptions,
  unassignTargets,
  statusMenu,
  rollUp,
  rolledUpAfter,
  workPatchOf,
  PRIORITY_CHOICES,
  effectivePriorityIn,
  priorityAfter,
  badgeOf,
  inheritedTitle,
  latestWins,
  priorityLabel,
  refusalRollback,
  unassignPreview,
  unassignQuestion,
  STATUS_LABEL,
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
// A second task, straight under the initiative.
const slot = card({
  itemId: "p", title: "Q2 plan", parent: "i",
  lineage: [{ uid: "g", type: "goal", title: "Grow" }, { uid: "i", type: "initiative", title: "Billing v2" }],
});
const loose = card({ itemId: "x", title: "Stray", parent: "nowhere" });

describe("kindOf", () => {
  it("is the planning type, a subtask by the parent as a task, else a task", () => {
    expect(kindOf(goal)).toBe("goal");
    expect(kindOf(ini)).toBe("initiative");
    expect(kindOf(epic)).toBe("epic");
    expect(kindOf(task)).toBe("task");
    expect(kindOf(sub)).toBe("subtask");
    expect(kindOf(slot)).toBe("task");
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

  it("draws initiative to task by default: no goals, no subtasks", () => {
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
    expect(out[1].foldable).toBe(2); // the epic and the task under it
  });
});

describe("statusOf", () => {
  it("is the server's derived standing on a planning card, no items when it says nothing", () => {
    expect(statusOf(card({ type: "epic", hierarchy: { status: "in-progress", done: 1, total: 3 } }))).toBe("in-progress");
    expect(statusOf(card({ type: "epic", hierarchy: { status: "backlog", done: 0, total: 2 } }))).toBe("backlog");
    expect(statusOf(card({ type: "epic", hierarchy: { status: "no-items", done: 0, total: 0 } }))).toBe("no-items");
    expect(statusOf(card({ type: "goal" }))).toBe("no-items");
  });

  // The work status only (K16): where the task stands, not how far its bar
  // has moved — BACKLOG while it waits, IN PROGRESS once it is in work, DONE;
  // a stage that holds it still says who it waits on.
  it("reads a task's own standing, in order: done, the stage holding it, in work, else backlog", () => {
    const today = "2026-11-09";
    expect(statusOf(card({ progress: 100 }))).toBe("done");
    expect(statusOf(card({ stage: "done", progress: 40 }))).toBe("done");
    expect(statusOf(card({ stage: "review", progress: 50, sprintStart: today }))).toBe("review");
    expect(statusOf(card({ stage: "locked", progress: 10, parked: true }))).toBe("locked");
    expect(statusOf(card({ stage: "refuse", progress: 10, sprintStart: today }))).toBe("refused");
    // In work, started or not: in progress.
    expect(statusOf(card({ sprintStart: today, progress: 0 }))).toBe("in-progress");
    expect(statusOf(card({ startDate: today, day: today, progress: 40 }))).toBe("in-progress");
    // Waiting — parked, whatever its bar says, or placed nowhere: backlog.
    expect(statusOf(card({ parked: true, progress: 49 }))).toBe("backlog");
    expect(statusOf(card({ progress: 0 }))).toBe("backlog");
    expect(statusOf(card({ stage: "done", progress: 100 }))).toBe("done");
    expect(STATUS_LABEL.backlog).toBe("Backlog");
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
    expect(droppable(wide, "p", "s")).toBe(false); // s is a subtask: one level deep
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


// The lines the Roadmap draws from a REAL listing: the roadmap's wire cards,
// mapped the way the board maps them — an epic and a task under it. The
// lines are one pure function of the listing, the depth and the folds
// (roadmapLines), and they draw what the listing holds.
describe("the lines the Roadmap draws", () => {
  const wire = (uid: string, spec: Partial<CardResource["spec"]>, lineage?: { uid: string; type: string; title: string }[]): CardResource =>
    ({
      kind: "Card",
      metadata: { uid },
      spec: { title: uid, assignees: [], progress: 0, ...spec },
      status: { complete: false, inProgress: false, ...(lineage ? { lineage } : {}) },
    }) as CardResource;
  const up = [{ uid: "e", type: "epic", title: "Billing v2" }];
  const listing: Card[] = [
    wire("e", { title: "Billing v2", type: "epic" }),
    wire("t", { title: "Render PDF", parent: "e", team: "portal" }, up),
  ].map(resourceToCard);

  it("draws the task under its epic", () => {
    const titles = roadmapLines(listing, { depth: DEFAULT_DEPTH, expanded: () => true })
      .filter((l) => l.kind === "row")
      .map((l) => (l.kind === "row" ? `${l.row.depth}:${l.row.node.card.title}` : ""));
    expect(titles).toEqual(["0:Billing v2", "1:Render PDF"]);
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

// The frame a drag is read in: the list's own content, so a row keeps its
// place however the board or the page scrolls, and a snapshot re-taken in
// the middle of a drag (a watch frame changed the rows) lands in the same
// frame as the one it replaces.
describe("the frame a Roadmap drag is read in", () => {
  it("shifts the reading by N px when the container is scrolled N px", () => {
    const still = pointerContentY(300, 0, 40)!;
    expect(pointerContentY(300, 120, 40)).toBe(still + 120);
    // The board and the page scroll alike: what counts is the sum.
    expect(pointerContentY(300, 90 + 30, 40)).toBe(still + 120);
  });

  it("puts a row measured at any scroll in the same place", () => {
    // A row 200px down the content, measured unscrolled and after a 120px
    // scroll (where the viewport shows it 120px higher), is one box.
    const a = boxesInContent([{ id: "r", top: 200, height: 30 }], 0);
    const b = boxesInContent([{ id: "r", top: 80, height: 30 }], 120);
    expect(a).toEqual(b);
    expect(a).toEqual([{ id: "r", top: 200, height: 30 }]);
  });

  it("reads the same row after a scroll, pointer and snapshot moving together", () => {
    const boxes = boxesInContent([{ id: "x", top: 100, height: 30 }, { id: "y", top: 130, height: 30 }], 0);
    // Unscrolled, viewport 145 is the middle of y; scrolled 30px, y is at
    // viewport 115 and the same reading comes from there.
    expect(dropTargetAt(boxes, pointerContentY(145, 0, 40)!, null, new Set())).toEqual({ overId: "y", zone: "into" });
    expect(dropTargetAt(boxes, pointerContentY(115, 30, 40)!, null, new Set())).toEqual({ overId: "y", zone: "into" });
  });

  it("reads nothing over the sticky header: a row scrolled under it is not a target", () => {
    expect(pointerContentY(39, 500, 40)).toBeNull();
    expect(pointerContentY(40, 500, 40)).toBe(540);
  });

  it("reads nothing without a pointer position (a keyboard drag has none)", () => {
    expect(pointerContentY(null, 0, 40)).toBeNull();
  });
});

// A breadcrumb followed to the Roadmap scrolls its row into view ONCE: the
// row stays marked, but later re-renders (a watch frame, a drag) do not pull
// the list back to it, and nothing scrolls while a drag runs.
describe("revealing a row followed to the Roadmap", () => {
  it("scrolls once the row is drawn, then is done", () => {
    expect(revealStep("t", false, true)).toEqual({ scroll: true, pending: null });
    expect(revealStep(null, false, true)).toEqual({ scroll: false, pending: null });
  });

  it("waits for a row not drawn yet (its parents still unfolding)", () => {
    expect(revealStep("t", false, false)).toEqual({ scroll: false, pending: "t" });
  });

  it("never scrolls while a drag runs, and still owes the scroll after it", () => {
    expect(revealStep("t", true, true)).toEqual({ scroll: false, pending: "t" });
  });
});

// The drawer's "Under it" is the Roadmap's tree rooted one level below the
// card the drawer shows: every level under it — plan items, tasks,
// subtasks — folded as the reader folds it, with
// no depth range (the drawer shows all there is under the card).
describe("the tree under a card", () => {
  const all = [goal, ini, epic, task, sub, slot];

  it("starts one level below the card, with every level under it", () => {
    const open = () => true;
    const got = subtreeRows(all, "g", open).map((r) => [r.node.card.itemId, r.depth]);
    expect(got).toEqual([
      ["i", 0],
      ["e", 1],
      ["t", 2],
      ["s", 3],
      ["p", 1],
    ]);
    expect(subtreeRows(all, "e", open).map((r) => r.node.card.itemId)).toEqual(["t", "s"]);
  });

  it("folds what the reader folds, and is empty for a card with nothing under it", () => {
    expect(subtreeRows(all, "g", (id) => id !== "i").map((r) => r.node.card.itemId)).toEqual(["i"]);
    expect(subtreeRows(all, "t", () => true).map((r) => r.node.card.itemId)).toEqual(["s"]);
    expect(subtreeRows(all, "s", () => true)).toEqual([]);
    expect(subtreeRows(all, "missing", () => true)).toEqual([]);
  });

  it("offers the levels below a card to add, the next one first", () => {
    expect(childLevels("goal")).toEqual(["initiative", "epic"]);
    expect(childLevels("initiative")).toEqual(["epic"]);
    expect(childLevels("epic")).toEqual(["task"]);
    expect(childLevels("task")).toEqual([]);
  });
});

// The Under picker names each option's place: its ancestors, root first, so
// two epics of one name under two goals are told apart; and a search finds a
// plan item by its ancestors' titles too.
describe("a plan item's place in a picker", () => {
  const all = [goal, ini, epic];

  it("is its ancestors, root first", () => {
    expect(parentChainOf(epic, all).map((c) => c.title)).toEqual(["Grow", "Billing v2"]);
    expect(parentChainOf(goal, all)).toEqual([]);
  });

  it("is read off the server's lineage when the ancestors are not held", () => {
    const far = card({
      itemId: "f", title: "Far", type: "epic", parent: "i",
      lineage: [{ uid: "g", type: "goal", title: "Grow" }, { uid: "i", type: "initiative", title: "Billing v2" }],
    });
    expect(parentChainOf(far, [far]).map((c) => [c.title, c.type])).toEqual([["Grow", "goal"], ["Billing v2", "initiative"]]);
  });

  it("is searched with its own title and its ancestors'", () => {
    expect(pickerMatches(epic, all, "invo")).toBe(true);
    expect(pickerMatches(epic, all, "billing")).toBe(true);
    expect(pickerMatches(epic, all, "grow")).toBe(true);
    expect(pickerMatches(epic, all, "nothing")).toBe(false);
    expect(pickerMatches(epic, all, "")).toBe(true);
  });
});

// Deleting a plan item from the drawer moves what was under it up to its
// own parent — the same moves the server makes (reparentChildren) — in the
// drawer's own copy of the hierarchy too, or a child would keep a dead
// parent and fall out of the tree.
describe("the hierarchy after a delete", () => {
  it("moves the card's children up, with its lineage, and drops it", () => {
    const iniL = { ...ini, lineage: [{ uid: "g", type: "goal" as const, title: "Grow" }] };
    const got = cardsAfterDelete([goal, iniL, epic, task], "i");
    expect(got.map((c) => c.itemId)).toEqual(["g", "e", "t"]);
    const e = got.find((c) => c.itemId === "e")!;
    expect(e.parent).toBe("g");
    expect(e.lineage).toEqual([{ uid: "g", type: "goal", title: "Grow" }]);
    expect(got.find((c) => c.itemId === "t")!.parent).toBe("e");
  });

  it("makes roots of a root's children", () => {
    const got = cardsAfterDelete([goal, ini], "g");
    expect(got.find((c) => c.itemId === "i")!.parent).toBeUndefined();
  });
});

describe("a chain that is not whole", () => {
  it("stops at a cycle", () => {
    const a = card({ itemId: "a", title: "A", type: "epic", parent: "b" });
    const b = card({ itemId: "b", title: "B", type: "initiative", parent: "a" });
    expect(parentChainOf(a, [a, b]).map((c) => c.itemId)).toEqual(["b"]);
  });

  it("is what is held when an ancestor is missing and no lineage says more", () => {
    const e2 = card({ itemId: "e2", title: "E", type: "epic", parent: "i" });
    const i2 = card({ itemId: "i", title: "I", type: "initiative", parent: "gone" });
    expect(parentChainOf(e2, [e2, i2]).map((c) => c.title)).toEqual(["I"]);
  });
});

// The Under picker's list: the levels a card may sit under, never itself or
// a card under it — whatever the search — each option with its chain, found
// by its own title or an ancestor's.
describe("the Under picker's options", () => {
  const under = card({ itemId: "u", title: "Billing child", type: "epic", parent: "e", lineage: [{ uid: "e", type: "epic", title: "Invoices" }] });
  const all = [goal, ini, epic, under];

  it("leaves out the card and what stands under it, even when the search names their ancestor", () => {
    const got = parentOptions("epic", all, "i", "grow");
    expect(got.flatMap((g) => g.options.map((o) => o.card.itemId))).toEqual(["g"]);
  });

  it("finds an option by an ancestor's title, and carries its chain", () => {
    const got = parentOptions("task", all, "x", "billing");
    const ids = got.flatMap((g) => g.options.map((o) => o.card.itemId));
    expect(ids).toEqual(["i", "e", "u"]);
    const e = got.flatMap((g) => g.options).find((o) => o.card.itemId === "e")!;
    expect(e.chain.map((c) => c.title)).toEqual(["Grow", "Billing v2"]);
  });
});

// What a drop changes on the board the moment it is let go: the dragged
// card's parent (and the lineage its breadcrumb reads) when that changed,
// and the listing's order — always, filter or no filter. The board's
// listing is what the server sent for the filters chosen, the drop is
// anchored on the cards shown, and the watch never echoes a tab's own
// writes back to it: an order not applied here is not shown until a reload.
describe("what a drop shows at once", () => {
  const g1 = card({ itemId: "g1", title: "One", type: "goal", team: "core" });
  const i1 = card({ itemId: "i1", title: "One-a", type: "initiative", parent: "g1", team: "core" });
  const g2 = card({ itemId: "g2", title: "Two", type: "goal", team: "core" });
  const i2 = card({ itemId: "i2", title: "Two-a", type: "initiative", parent: "g2", team: "core" });
  const g3 = card({ itemId: "g3", title: "Three", type: "goal", team: "core" });
  const i3 = card({ itemId: "i3", title: "Three-a", type: "initiative", parent: "g3", team: "core" });
  const e1 = card({ itemId: "e1", title: "E1", type: "epic", parent: "i1" });
  const e2 = card({ itemId: "e2", title: "E2", type: "epic", parent: "i1" });
  // The listing as the server sends it: board order, children interleaved.
  const listed = [i1, g2, g1, i2, g3, i3, e1, e2];
  const drawnAfter = (cards: Card[], from: "goal" | "initiative" = "goal") =>
    roadmapLines(cards, { depth: { from, to: "task" }, expanded: () => true })
      .filter((l) => l.kind === "row")
      .map((l) => (l.kind === "row" ? l.row.node.card.itemId : ""));
  const apply = (cards: Card[], id: string, placement: { parent: string | null; index: number }) => {
    const shown = dropShown(cards, id, placement);
    const order = new Map(shown.order.map((u, k) => [u, k]));
    return cards
      .map((c) => (c.itemId === id && shown.patch ? { ...c, ...shown.patch } : c))
      .sort((a, b) => order.get(a.itemId)! - order.get(b.itemId)!);
  };

  it("reorders two goals at the top of the tree, with no parent written", () => {
    expect(drawnAfter(listed)).toEqual(["g2", "i2", "g1", "i1", "e1", "e2", "g3", "i3"]);
    // Two goes between One and Three: index 1 among the parentless cards.
    const shown = dropShown(listed, "g2", { parent: null, index: 1 });
    expect(shown.patch).toBeNull();
    expect(drawnAfter(apply(listed, "g2", { parent: null, index: 1 }))).toEqual(["g1", "i1", "e1", "e2", "g2", "i2", "g3", "i3"]);
  });

  it("reorders the goals of a filtered listing the same way: the order is never left out", () => {
    // A team filter: only these cards were sent.
    const filtered = [g2, i2, g1, i1, g3, i3];
    const after = apply(filtered, "g2", { parent: null, index: 2 });
    expect(drawnAfter(after)).toEqual(["g1", "i1", "g3", "i3", "g2", "i2"]);
    expect(dropShown(filtered, "g2", { parent: null, index: 2 }).order).toEqual(after.map((c) => c.itemId));
  });

  it("moves initiatives that stand at the top of the drawn tree when the goals are not drawn", () => {
    // Depth from initiative: the initiatives are the drawn roots, each under
    // its own hidden goal; Two-a dropped before Three-a joins Three, first.
    expect(drawnAfter(listed, "initiative")).toEqual(["i2", "i1", "e1", "e2", "i3"]);
    const moved = apply(listed, "i2", { parent: "g3", index: 0 });
    expect(drawnAfter(moved, "initiative")).toEqual(["i1", "e1", "e2", "i2", "i3"]);
  });

  it("reorders epics under one initiative at once", () => {
    expect(drawnAfter(apply(listed, "e1", { parent: "i1", index: 1 }))).toEqual(["g2", "i2", "g1", "i1", "e2", "e1", "g3", "i3"]);
  });

  it("writes the new parent and its lineage when the card changes parent", () => {
    const shown = dropShown(listed, "e1", { parent: "i2", index: 0 });
    expect(shown.patch).toEqual({
      parent: "i2",
      lineage: [
        { uid: "g2", type: "goal", title: "Two" },
        { uid: "i2", type: "initiative", title: "Two-a" },
      ],
    });
    expect(drawnAfter(apply(listed, "e1", { parent: "i2", index: 0 }))).toEqual(["g2", "i2", "e1", "g1", "i1", "e2", "g3", "i3"]);
  });
});

// The badge menu (K15, K16): the card's OWN priority — P0 to P4, or none —
// and UNASSIGNED, which takes the work in hand under the row back to its
// backlog. Mirrors board.UnassignTargets, case for case with boardservice's
// TestUnassignedOnAPlanItem…
describe("the badge menu on the Roadmap (K15, K16)", () => {
  const today = "2026-11-09";
  const ip = { status: "in-progress" as const, done: 0, total: 3 };
  const g = card({ itemId: "g", type: "goal", team: "alpha", priority: "P2", hierarchy: ip });
  const e = card({ itemId: "e", type: "epic", parent: "g", hierarchy: ip });
  const p1 = card({ itemId: "p1", parent: "e", parked: true });
  const p2 = card({ itemId: "p2", parent: "g", parked: true });
  const w1 = card({ itemId: "w1", parent: "e", team: "alpha", assignees: ["kvaps"], sprintStart: today, startDate: today, day: today });
  const w2 = card({ itemId: "w2", parent: "g", team: "alpha", assignees: ["bob"], sprintStart: today, day: today });
  const doneWork = card({ itemId: "done-work", parent: "e", sprintStart: today, day: today, progress: 100 });
  const sub = card({ itemId: "sub", parent: "w1", sprintStart: today });
  const all = [g, e, p1, p2, w1, w2, doneWork, sub];
  const ids = (cs: Card[]) => cs.map((c) => c.itemId);

  it("UNASSIGNED reaches every open task in work under a plan item; finished work and subtasks never", () => {
    expect(ids(unassignTargets(all, g))).toEqual(["w1", "w2"]);
    expect(ids(unassignTargets(all, w2))).toEqual(["w2"]);
    expect(unassignTargets(all, p1)).toEqual([]);
    expect(unassignTargets(all, doneWork)).toEqual([]);
    expect(unassignTargets(all, sub)).toEqual([]);
  });

  it("offers the four priorities with the card's OWN one checked, and UNASSIGNED where work is in hand", () => {
    expect(statusMenu(all, g)).toEqual({ own: "P2", unassign: true });
    // e has none of its own (it counts under g's P2): "No priority" is checked.
    expect(statusMenu(all, e)).toEqual({ own: "", unassign: true });
    expect(statusMenu(all, doneWork)).toEqual({ own: "", unassign: false });
    expect(PRIORITY_CHOICES.map((c) => c.label)).toEqual(["P0", "P1", "P2", "P3", "P4", "No priority"]);
    expect(PRIORITY_CHOICES.map((c) => c.value)).toEqual(["P0", "P1", "P2", "P3", "P4", ""]);
  });

  it("names the tasks it counted, so the server acts on exactly those, and asks nothing of none", () => {
    expect(unassignPreview(unassignTargets(all, g))).toEqual(["w1", "w2"]);
    // The server found nothing in work under the row (the board moved since
    // the menu opened): there is no "Take 0 tasks out of work?" to ask.
    expect(unassignPreview([])).toBeNull();
  });

  it("puts a refused priority back only when it was the latest press", () => {
    const seq = latestWins();
    const p1 = seq.next("g");
    const p2 = seq.next("g");
    const before = [{ id: "g", patch: { priority: undefined } }];
    // The older press (P1) failing must not undo the newer optimistic P2.
    expect(refusalRollback(seq, "g", p1, before)).toEqual([]);
    expect(refusalRollback(seq, "g", p2, before)).toEqual(before);
  });

  it("asks how many tasks leave Team and Me before UNASSIGNED takes them", () => {
    expect(unassignQuestion(2)).toBe("Take 2 tasks out of work? They leave Team and Me and go back to their team's backlog.");
    expect(unassignQuestion(1)).toBe("Take 1 task out of work? It leaves Team and Me and goes back to its team's backlog.");
  });
});

// The effective priority (K15) — mirrors board.Index.EffectivePriority, case
// for case with TestTheEffectivePriorityIsOwnElseTheNearestAncestors: its own
// when it has one, else the nearest ancestor's, else none; a review card's is
// its task's.
describe("the effective priority", () => {
  const g = card({ itemId: "g", type: "goal", title: "Grow", priority: "P3" });
  const i = card({ itemId: "i", type: "initiative", title: "Portugal", parent: "g", priority: "P2" });
  const e = card({ itemId: "e", type: "epic", title: "Billing", parent: "i" });
  const t = card({ itemId: "t", parent: "e" });
  const own = card({ itemId: "own", parent: "e", priority: "P1" });
  const bare = card({ itemId: "bare", type: "goal" });
  const u = card({ itemId: "u", parent: "bare" });
  const o = card({ itemId: "o", parent: "e", stage: "review" });
  const r = card({ itemId: "r", reviewOf: "o" });
  // A review of a review counts under the review it reviews; two reviews of
  // each other are a cycle that ends, under nobody.
  const rr = card({ itemId: "rr", reviewOf: "r" });
  const x = card({ itemId: "x", reviewOf: "y" });
  const y = card({ itemId: "y", reviewOf: "x" });
  const all = [g, i, e, t, own, bare, u, o, r, rr, x, y];
  const read = (c: Card) => {
    const got = effectivePriorityIn(all, c);
    return [got.priority ?? "", got.from?.itemId ?? ""];
  };

  it("is its own, else the nearest ancestor's, else none", () => {
    expect(read(g)).toEqual(["P3", ""]);
    expect(read(i)).toEqual(["P2", ""]);
    expect(read(e)).toEqual(["P2", "i"]);
    expect(read(t)).toEqual(["P2", "i"]);
    expect(read(own)).toEqual(["P1", ""]);
    expect(read(bare)).toEqual(["", ""]);
    expect(read(u)).toEqual(["", ""]);
    expect(read(r)).toEqual(["P2", "i"]);
    expect(read(rr)).toEqual(["P2", "i"]);
    expect(read(x)).toEqual(["", ""]);
  });

  it("re-reads the card and every card under it when its own priority changes", () => {
    const after = priorityAfter(all, "i", "");
    // i has none now: it and what is under it count under the goal's P3.
    expect(after.get("i")).toEqual({ effectivePriority: "P3", effectivePriorityFrom: { uid: "g", type: "goal", title: "Grow" } });
    expect(after.get("t")).toEqual({ effectivePriority: "P3", effectivePriorityFrom: { uid: "g", type: "goal", title: "Grow" } });
    // A card with its own keeps it.
    expect(after.get("own")).toEqual({ effectivePriority: "P1", effectivePriorityFrom: undefined });
    expect(after.has("g") || after.has("bare")).toBe(false);
    expect(priorityAfter(all, "bare", "P1").get("u")).toEqual({ effectivePriority: "P1", effectivePriorityFrom: { uid: "bare", type: "goal", title: "a card" } });
  });

  it("re-reads the review cards of the tasks it reaches: a review counts under its task's", () => {
    // o is under e, which counts under i's P2: its review r goes with it.
    expect(priorityAfter(all, "i", "P0").get("r")).toEqual({ effectivePriority: "P0", effectivePriorityFrom: { uid: "i", type: "initiative", title: "Portugal" } });
    // A task's own priority reaches its review card too.
    expect(priorityAfter(all, "o", "P4").get("r")).toEqual({ effectivePriority: "P4", effectivePriorityFrom: { uid: "o", type: undefined, title: "a card" } });
  });
});

// The badge is ONE pill in two parts (K15): the effective priority on the
// left when there is one — drawn lighter, naming where it comes from, when it
// is inherited — and the work status on the right.
describe("the badge", () => {
  it("is the priority and the status, or the status alone", () => {
    const today = "2026-11-09";
    expect(badgeOf(card({ priority: "P1", effectivePriority: "P1", sprintStart: today }))).toEqual({
      priority: { value: "P1", from: undefined },
      status: "in-progress",
    });
    expect(
      badgeOf(card({ effectivePriority: "P2", effectivePriorityFrom: { uid: "e", type: "epic", title: "Billing" }, parked: true })),
    ).toEqual({ priority: { value: "P2", from: { uid: "e", type: "epic", title: "Billing" } }, status: "backlog" });
    expect(badgeOf(card({ parked: true }))).toEqual({ priority: undefined, status: "backlog" });
    expect(inheritedTitle({ uid: "e", type: "epic", title: "Billing" })).toBe("from «Billing»");
  });
});

// A plan item rolls up from where its OPEN tasks stand (H5, amended with
// K16) — mirrors board.hierarchyStatus, case for case with
// TestAPlanItemRollsUpFromWhereItsOpenTasksStand: any open task in work → in
// progress; open tasks, none in work → backlog; all done → done; none → no
// items. Progress, a person, a priority alone do not make it in progress.
describe("a plan item's roll-up", () => {
  const today = "2026-11-09";
  const inWork = card({ itemId: "w", parent: "e", sprintStart: today, day: today });
  const parked = card({ itemId: "p", parent: "e", parked: true });
  const done = card({ itemId: "d", parent: "e", progress: 100 });
  const st = (leaves: Card[]) => rollUp(leaves).status;

  it("reads the open tasks: in work, or waiting", () => {
    expect(st([])).toBe("no-items");
    expect(st([done])).toBe("done");
    expect(st([inWork, parked])).toBe("in-progress");
    expect(st([parked])).toBe("backlog");
    expect(st([done, parked])).toBe("backlog");
    expect(st([{ ...parked, priority: "P1" }])).toBe("backlog");
    expect(st([{ ...parked, progress: 49 }])).toBe("backlog");
    expect(st([{ ...parked, assignees: ["ann"] }])).toBe("backlog");
    expect(rollUp([done, parked])).toEqual({ status: "backlog", done: 1, total: 2 });
  });

  it("re-reads the plan items above the tasks a gesture changed, before the server answers", () => {
    const e = card({ itemId: "e", type: "epic", parent: "g", hierarchy: { status: "in-progress", done: 0, total: 2 } });
    const g = card({ itemId: "g", type: "goal", hierarchy: { status: "in-progress", done: 0, total: 2 } });
    const t1 = card({ itemId: "t1", parent: "e", assignees: ["ann"], sprintStart: today, day: today, progress: 49 });
    const t2 = card({ itemId: "t2", parent: "e", parked: true });
    const before = [g, e, t1, t2];
    // UNASSIGNED on the epic: t1 back to its backlog, its 49% kept.
    const after = before.map((c) => (c.itemId === "t1" ? { ...c, parked: true, assignees: [], sprintStart: undefined, day: undefined } : c));
    expect(rolledUpAfter(after, ["t1"])).toEqual(
      new Map([
        ["e", { status: "backlog", done: 0, total: 2 }],
        ["g", { status: "backlog", done: 0, total: 2 }],
      ]),
    );
  });
});

// What the acting tab takes from the server's answer (WorkChange.items): the
// fields a status gesture changes, every one of them — a field the server
// cleared is cleared here too, not left as it was.
describe("patching what a status gesture changed", () => {
  it("takes exactly where the task stands, cleared fields included", () => {
    const parked = card({ itemId: "t", parked: true, assignees: [], progress: 49, title: "renamed meanwhile" });
    expect(workPatchOf(parked)).toEqual({
      parked: true,
      assignees: [],
      startDate: undefined,
      day: undefined,
      sprintStart: undefined,
    });
  });
});


// UNASSIGNED reaches exactly the open tasks in work under a row (K16) — the
// same fixture as board's TestUnassignTargetsMatchTheSharedFixture, read from
// the Go package's testdata: the same cards give the same tasks on both
// sides.
describe("UNASSIGNED's reach, shared with the server", () => {
  const fx = JSON.parse(
    readFileSync(fileURLToPath(new URL("../../pkg/board/testdata/unassign_targets.json", import.meta.url)), "utf8"),
  ) as { cards: Partial<Card>[]; targets: Record<string, string[]> };
  const cards = fx.cards.map((c) => card(c));

  it("names the same tasks as board.UnassignTargets, for every row of the fixture", () => {
    for (const [root, want] of Object.entries(fx.targets)) {
      const c = cards.find((x) => x.itemId === root)!;
      expect([root, unassignTargets(cards, c).map((t) => t.itemId)]).toEqual([root, want]);
    }
  });
});

// The Roadmap may be filtered: the cards it holds are fewer than the board's,
// and only the server knows every task in work under a plan item. So a plan
// item offers UNASSIGNED by its server roll-up — IN PROGRESS means there is
// work in hand under it — and the confirm asks the server (a dry run) what it
// would take; a task offers it by its own place.
describe("UNASSIGNED under a filter", () => {
  it("is offered on a plan item the server rolls up IN PROGRESS, whatever the listing holds", () => {
    const epic = card({ itemId: "e", type: "epic", hierarchy: { status: "in-progress", done: 0, total: 3 } });
    // The filter left none of its tasks in the listing.
    expect(statusMenu([epic], epic).unassign).toBe(true);
    const waiting = { ...epic, hierarchy: { status: "backlog" as const, done: 0, total: 3 } };
    expect(statusMenu([waiting], waiting).unassign).toBe(false);
    const task = card({ itemId: "t", sprintStart: "2026-11-09" });
    expect(statusMenu([task], task).unassign).toBe(true);
    expect(statusMenu([{ ...task, parked: true, sprintStart: undefined }], { ...task, parked: true, sprintStart: undefined }).unassign).toBe(false);
  });
});

// A later choice wins (K15): the badge applies a server answer only when it
// answers the latest choice made on that card, so an older response arriving
// late cannot overwrite a newer one.
describe("the latest choice wins", () => {
  it("applies the answer to the newest request per card only", () => {
    const seq = latestWins();
    const first = seq.next("c");
    const second = seq.next("c");
    const other = seq.next("d");
    expect(seq.isLatest("c", first)).toBe(false);
    expect(seq.isLatest("c", second)).toBe(true);
    expect(seq.isLatest("d", other)).toBe(true);
  });
});

// What the badge says to a screen reader: the priority and where it comes
// from, or that none is set.
describe("the priority's label", () => {
  it("names the priority and the card it is inherited from", () => {
    expect(priorityLabel("P1")).toBe("Priority P1");
    expect(priorityLabel("P2", { uid: "g", type: "goal", title: "Grow" })).toBe("Priority P2, from «Grow»");
    expect(priorityLabel(undefined)).toBe("Set a priority");
  });
});

