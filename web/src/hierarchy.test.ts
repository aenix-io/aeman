import { describe, expect, it } from "vitest";

import { parkable } from "./backlog";
import { onBacklog } from "./backlogboard";
import { onMyDay } from "./meview";
import { removeChoices } from "./removal";
import { mayRemove } from "./meboard";
import { pointsOf } from "./size";
import { isPlanning, isSubtask } from "./subtasks";
import { inHandOn } from "./teamgrid";
import { createView } from "./views";
import { viewQueries, watchQuery } from "./viewquery";
import type { Board, Card } from "./providers/types";

// The hierarchy's mirror, card by card: the same answers pkg/board gives in
// TestASubtaskIsATaskUnderATask and TestPlanningCardsAreOnNoDayBoardAndTasksUnderThemAre,
// or the optimistic board draws a card the next frame takes away.

const TODAY = "2026-11-02";
const card = (over: Partial<Card>): Card =>
  ({ itemId: "c", title: "a card", assignees: ["u"], progress: 0, team: "alpha", ...over }) as Card;

const epic = card({ itemId: "epic", title: "Billing", type: "epic", startDate: TODAY, day: TODAY, sprintStart: TODAY });
// A task under the epic: a parent, and a lineage whose last entry is that epic.
const underEpic = card({
  itemId: "t1", parent: "epic", startDate: TODAY, day: TODAY, sprintStart: TODAY,
  lineage: [{ uid: "epic", type: "epic", title: "Billing" }],
});
// A subtask: a parent that is a task — the lineage ends in a task.
const subtask = card({
  itemId: "t1a", parent: "t1", startDate: TODAY, day: TODAY, sprintStart: TODAY,
  lineage: [{ uid: "epic", type: "epic", title: "Billing" }, { uid: "t1", title: "Invoices" }],
});
// A parent and no lineage at all: a card made a moment ago, before the
// server has said what its parent is — read as a subtask, as it always was.
// (A parent the board does not hold never arrives: the server drops it.)
const dangling = card({ itemId: "t4", parent: "gone", startDate: TODAY, day: TODAY, sprintStart: TODAY });

describe("isSubtask and isPlanning", () => {
  it("a subtask is a task whose parent is a task; a task under an epic is not", () => {
    expect(isSubtask(subtask)).toBe(true);
    expect(isSubtask(underEpic)).toBe(false);
    expect(isSubtask(dangling)).toBe(true);
    expect(isSubtask(epic)).toBe(false);
    expect(isSubtask(card({}))).toBe(false);
  });
  it("isPlanning is a type", () => {
    expect(isPlanning(epic)).toBe(true);
    expect(isPlanning(underEpic)).toBe(false);
  });
});

describe("the day boards draw tasks", () => {
  const board = { sprintStates: { alpha: { current: TODAY, previous: "" } } } as unknown as Board;

  it("the Team grid: the task under the epic, not the epic, not the subtask", () => {
    expect(inHandOn(underEpic, TODAY, TODAY, TODAY)).toBe(true);
    expect(inHandOn(epic, TODAY, TODAY, TODAY)).toBe(false);
    expect(inHandOn(subtask, TODAY, TODAY, TODAY)).toBe(false);
    expect(inHandOn(dangling, TODAY, TODAY, TODAY)).toBe(false);
  });

  it("the Me board: the same", () => {
    expect(onMyDay(underEpic, board, TODAY, TODAY)).toBe(true);
    expect(onMyDay(epic, board, TODAY, TODAY)).toBe(false);
    expect(onMyDay(subtask, board, TODAY, TODAY)).toBe(false);
  });

  it("the backlog: a parked task under an epic, never the epic, never a subtask (K2)", () => {
    expect(onBacklog(card({ parked: true, parent: "epic", lineage: [{ uid: "epic", type: "epic", title: "Billing" }] }))).toBe(true);
    expect(onBacklog(card({ parked: true, type: "goal" }))).toBe(false);
    expect(onBacklog(card({ parked: true, parent: "t1" }))).toBe(false);
  });
});

describe("what a task under an epic may do, that a subtask may not", () => {
  it("is parked, filed into a column, removed like any task; a planning card none of it", () => {
    expect(parkable(underEpic)).toBe(true);
    expect(parkable(subtask)).toBe(false);
    expect(parkable(epic)).toBe(false);
    const ctx = { current: TODAY, today: TODAY };
    // The × offers a task under an epic its team's backlog, as it does any
    // task (K3); a subtask has the board alone and is deleted.
    expect(removeChoices(underEpic, ctx)).toContain("backlog");
    expect(removeChoices(subtask, ctx)).toEqual(["off-board"]);
    expect(mayRemove(subtask, "u")).toBe(true);
    expect(mayRemove({ ...underEpic, author: "x", zone: "red" }, "u")).toBe(false);
  });

  it("weighs itself: a goal is no umbrella over its tasks", () => {
    const goal = card({ itemId: "goal", type: "goal" });
    const t = card({ itemId: "t", parent: "goal", size: "L" });
    expect(pointsOf([goal, t], goal)).toBe(pointsOf([], goal));
    const umbrella = card({ itemId: "u" });
    const piece = card({ itemId: "p", parent: "u", size: "L" });
    expect(pointsOf([umbrella, piece], umbrella)).toBe(pointsOf([], piece));
  });
});

describe("the Roadmap is a board", () => {
  it("a create with a type belongs to it", () => {
    expect(createView({ type: "epic" })).toBe("roadmap");
    expect(createView({}, "team")).toBe("team");
  });
  it("lists the hierarchy board-wide, and watches it, never as a snapshot", () => {
    expect(viewQueries("roadmap", "2026-01-01", ["alpha"], undefined, TODAY)).toEqual([{ view: "roadmap" }]);
    expect(watchQuery("roadmap", TODAY, ["alpha"])).toEqual({ view: "roadmap" });
  });
});

// subtaskParent is the parent AS A TASK: what a board nests a row under,
// offers the subtask × for, and keeps when a drag says "standalone". A task
// under an epic has a parent and no subtask parent — reading the raw field
// drew it indented under nothing and unlinked it from its epic on a drag.
describe("subtaskParent", () => {
  it("is the parent of a subtask, and nothing for a task under an epic", async () => {
    const { subtaskParent } = await import("./subtasks");
    const sub = card({ itemId: "s", parent: "t", lineage: [{ uid: "t", title: "a task" }] });
    expect(subtaskParent(sub)).toBe("t");
    expect(subtaskParent(underEpic)).toBeUndefined();
    expect(subtaskParent(card({ itemId: "root" }))).toBeUndefined();
  });

  it("reads a card with a parent and no lineage as a subtask, as the server does", async () => {
    const { subtaskParent } = await import("./subtasks");
    expect(subtaskParent(card({ itemId: "fresh", parent: "t" }))).toBe("t");
  });
});
