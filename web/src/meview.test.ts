import { describe, expect, it } from "vitest";

import { hiddenByFilters, meFilterPasses, moreTasksLabel, onMyDay } from "./meview";
import type { Board, Card } from "./providers/types";

const card = (over: Partial<Card>): Card =>
  ({ itemId: "c", title: "a card", assignees: ["u"], progress: 0, team: "alpha", ...over }) as Card;

// The team's sprint began on the Monday of the week under test.
const board = { sprintStates: { alpha: { current: "2026-09-14", previous: "2026-09-07" } } } as unknown as Board;

const TODAY = "2026-09-16"; // a Wednesday; its week begins 2026-09-14
const THIS_WEEK = "2026-09-14";
const NEXT_WEEK = "2026-09-21";
const LAST_WEEK = "2026-09-07";

// The Me board's rule for a day, card by card — the mirror of board.MeView.
// The two answers must agree before the server has spoken, or the board draws
// a card the next frame takes away (or never draws one the server sent).
//
// D17 is retired: a card PLANNED FOR A WEEK used to stand on its owner's board
// once its Monday came. A task has no week any more (K7) — it is in work (a
// date or a sprint) or parked in its team's backlog (K1) — and the Me board
// draws the first kind only.
describe("the Me board draws work in hand, never the backlog (K1, K7)", () => {
  it("leaves a parked task off every day, dates or no dates", () => {
    expect(onMyDay(card({ parked: true }), board, TODAY, TODAY)).toBe(false);
    expect(onMyDay(card({ parked: true, startDate: THIS_WEEK }), board, TODAY, TODAY)).toBe(false);
    expect(onMyDay(card({ parked: true, sprintStart: THIS_WEEK }), board, TODAY, TODAY)).toBe(false);
  });

  it("leaves a task with neither a date nor a sprint off: there is no strip", () => {
    expect(onMyDay(card({}), board, TODAY, TODAY)).toBe(false);
  });

  it("reads a week an older repository left on a task as nothing", () => {
    // Its week has come, it is gone by, it is ahead: none of it puts a task
    // with no date and no sprint on the board, and none of it holds back a
    // task that HAS them.
    for (const week of [THIS_WEEK, LAST_WEEK, NEXT_WEEK]) {
      const weekOnly = { ...card({}), week } as Card;
      expect(onMyDay(weekOnly, board, TODAY, TODAY)).toBe(false);
    }
    const inSprint = { ...card({ sprintStart: THIS_WEEK, startDate: THIS_WEEK }), week: NEXT_WEEK } as Card;
    expect(onMyDay(inSprint, board, TODAY, TODAY)).toBe(true);
  });
});

// The rule it sits beside is unchanged, and pinned here because it now lives
// in this file rather than inside the component.
describe("the rest of a day on the Me board", () => {
  it("draws the sprint's work once its day has come", () => {
    expect(onMyDay(card({ sprintStart: THIS_WEEK, startDate: THIS_WEEK }), board, TODAY, TODAY)).toBe(
      true,
    );
    expect(onMyDay(card({ sprintStart: THIS_WEEK, startDate: "2026-09-17" }), board, TODAY, TODAY)).toBe(
      false,
    );
  });

  it("shows a card deferred ahead from its day on", () => {
    const later = card({ sprintStart: THIS_WEEK, startDate: "2026-09-18" });
    expect(onMyDay(later, board, TODAY, TODAY)).toBe(false);
    expect(onMyDay(later, board, "2026-09-18", TODAY)).toBe(true);
  });

  it("spans a range across its days", () => {
    const range = card({ startDate: LAST_WEEK, day: "2026-09-15" });
    expect(onMyDay(range, board, "2026-09-15", TODAY)).toBe(true);
  });

  it("leaves a subtask to its parent", () => {
    expect(onMyDay(card({ parent: "p", sprintStart: THIS_WEEK }), board, TODAY, TODAY)).toBe(false);
  });

  // An open card whose sprint fell behind the team's current one — left out of
  // a carry-over, its end date already past — still stands on its owner's board
  // today, the way the Team board keeps overran work. A finished one carried
  // past does not, and neither does an old sprint-less stray; a day to come is
  // a plan. Mirrors TestMeBoardKeepsStrandedOpenWork.
  it("keeps the person's open work whose sprint fell behind, on today", () => {
    const STALE = "2026-08-31"; // older than the previous sprint
    const stranded = card({ sprintStart: STALE, startDate: STALE, day: "2026-09-02", progress: 90 });
    expect(onMyDay(stranded, board, TODAY, TODAY)).toBe(true);
    // Finished and carried past: it belongs to its own sprint, not to today.
    const done = card({ sprintStart: STALE, startDate: STALE, day: "2026-09-02", progress: 100, doneAt: "2026-09-01" });
    expect(onMyDay(done, board, TODAY, TODAY)).toBe(false);
    // An old sprint-less stray must not resurface on today.
    const stray = card({ startDate: STALE, day: "2026-09-02" });
    expect(onMyDay(stray, board, TODAY, TODAY)).toBe(false);
    // A day still to come is a plan, not a dumping ground for open work.
    expect(onMyDay(stranded, board, NEXT_WEEK, TODAY)).toBe(false);
  });
});

// A card in a Scope column is a PROJECT CARD and is drawn on the Project
// board alone (H4): it has no owner to look for it here, and one written
// before schema 2 that still names one is a plan all the same. Mirrors
// board.MeView.
describe("a project card is on nobody's Me board", () => {
  it("is not drawn whatever its dates, sprint or stale owner say", () => {
    const plan = card({ scope: "Storage", project: "P", startDate: TODAY, day: TODAY, sprintStart: THIS_WEEK });
    expect(onMyDay(plan, board, TODAY, TODAY)).toBe(false);
    expect(onMyDay({ ...plan, assignees: ["u"] }, board, TODAY, TODAY)).toBe(false);
  });

  it("leaves a card that merely names a project as an ordinary task", () => {
    expect(onMyDay(card({ project: "P", startDate: TODAY, day: TODAY }), board, TODAY, TODAY)).toBe(true);
  });
});

// The Me board's Team filter (O12) hides the viewer's own cards of other
// teams — and says how many it hides: the "N more tasks" button, counted
// over exactly what the board would draw on the day with no filter, which
// clears it. The Me board has no Customer filter: a customer chosen on the
// Team board or the Backlog hides nothing here.
describe("the Me board's filter", () => {
  const cards = [
    card({ itemId: "a", team: "alpha" }),
    card({ itemId: "b", team: "beta" }),
    card({ itemId: "n", team: "" }),
  ];

  it("passes everything while no team is chosen", () => {
    expect(cards.every((c) => meFilterPasses(c, cards, null))).toBe(true);
    expect(cards.every((c) => meFilterPasses(c, cards, []))).toBe(true);
  });

  it("keeps the chosen teams, the no-team group being a team", () => {
    expect(cards.filter((c) => meFilterPasses(c, cards, ["alpha", ""])).map((c) => c.itemId)).toEqual(["a", "n"]);
  });

  it("counts what it hides of what the day would draw", () => {
    expect(hiddenByFilters(cards, () => [], (c) => meFilterPasses(c, cards, ["alpha"]))).toBe(2);
    expect(hiddenByFilters(cards, () => [], () => true)).toBe(0);
    expect(moreTasksLabel(1)).toBe("1 more task");
    expect(moreTasksLabel(3)).toBe("3 more tasks");
  });

  // What the board draws is rows AND the subtasks nested under them, so the
  // count is of both: a hidden row takes its subtasks with it, and a shown
  // row's subtask of another team is hidden on its own.
  it("counts the subtasks it hides, under a hidden row and a shown one", () => {
    const kids: Record<string, ReturnType<typeof card>[]> = {
      a: [card({ itemId: "a1", team: "alpha" }), card({ itemId: "a2", team: "beta" })],
      b: [card({ itemId: "b1", team: "beta" }), card({ itemId: "b2", team: "alpha" })],
    };
    const passes = (c: ReturnType<typeof card>) => meFilterPasses(c, cards, ["alpha"]);
    // b and both its subtasks; n; a's beta subtask.
    expect(hiddenByFilters(cards, (id) => kids[id] ?? [], passes)).toBe(5);
  });
});

// O13 on the Me board: a task with no team of its own stands under its plan
// item's team — in the Team filter, and for the sprint its day is read by.
describe("the Me board by the team a task stands under", () => {
  it("keeps an inheriting task under its plan item's team", () => {
    const inherited = card({ itemId: "i", team: "", parent: "e", effectiveTeam: "alpha" });
    expect(meFilterPasses(inherited, [inherited], ["alpha"])).toBe(true);
    expect(meFilterPasses(inherited, [inherited], [""])).toBe(false);
  });

  it("reads the day by the sprint of the team it stands under", () => {
    // In alpha's current sprint, a past day of it: on the board by alpha's
    // sprint, which the no-team group does not have.
    const inherited = card({
      itemId: "i",
      team: "",
      parent: "e",
      lineage: [{ uid: "e", type: "epic", title: "Billing" }],
      effectiveTeam: "alpha",
      sprintStart: THIS_WEEK,
    });
    expect(onMyDay(inherited, board, TODAY, TODAY)).toBe(true);
    expect(onMyDay(inherited, board, "2026-09-15", TODAY)).toBe(true);
  });
});
