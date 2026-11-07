import { describe, expect, it } from "vitest";
import {
  deleteWarning,
  freeSubtasks,
  offersRemoval,
  removeIntent,
  removeChoices,
  backlogPatch,
  unaskedRemoval,
  withdrawPatch,
  reviewAssigneePatch,
  subtaskRemovalPatch,
  subtaskRemovalUndo,
  type RemovableCard,
  type RemovalHomes,
} from "./removal";

/** mk builds the slice of a card these rules read, TYPED. `as never`
 *  silences the checker instead of asking it — `never` is assignable to
 *  everything — so a field renamed in RemovalHomes or RemovableCard would
 *  leave every case here passing while the function read an empty object. */
type Removable = RemovalHomes &
  Pick<RemovableCard, "progress" | "startDate" | "sprintStart" | "parent"> & {
    title?: string;
    assignees?: string[];
  };
const mk = (c: Removable): Removable => c;


// Deleting is silent for a card with nothing to lose and a question for one
// with work or a review card on it — the loss is named, so the answer means
// something. Subtasks survive a delete as standalone cards and are not a
// loss to warn about.
describe("the question before a delete", () => {
  it("is not asked for a card just made", () => {
    expect(deleteWarning({ title: "fresh" }, null)).toBeNull();
    expect(deleteWarning({ title: "fresh", progress: 0 }, null)).toBeNull();
  });

  it("names the work that goes with the card", () => {
    expect(deleteWarning({ title: "Deploy", progress: 40 }, null)).toBe(
      "Delete «Deploy»? This also removes 40% of work on it.",
    );
  });

  it("names the linked review card", () => {
    expect(deleteWarning({ title: "Deploy" }, "Review: Deploy")).toBe(
      "Delete «Deploy»? This also removes its linked review card «Review: Deploy».",
    );
  });

  it("names both when both would go", () => {
    expect(deleteWarning({ title: "Deploy", progress: 40 }, "Review: Deploy")).toBe(
      "Delete «Deploy»? This also removes 40% of work on it and its linked review card «Review: Deploy».",
    );
  });
});

// Deleting a parent frees its subtasks into standalone cards on the server
// (they take the parent's person, so they land in the cell it stood in). The
// optimistic client must do the same, or the subtasks — still pointing at a
// parent that is gone — vanish from the grid until a refresh: the very
// "silently disappeared" shape the × rules exist to prevent.
describe("freeing the subtasks of a deleted parent", () => {
  const cards = [
    { itemId: "p", assignees: ["kvaps"], sprintStart: "2026-09-01" },
    { itemId: "s1", parent: "p", assignees: [], sprintStart: "2026-09-01" },
    { itemId: "s2", parent: "p", assignees: ["bob"], sprintStart: "2026-09-01" },
    { itemId: "other", parent: "q", assignees: [], sprintStart: "2026-09-01" },
  ];

  it("clears the parent and hands an unassigned subtask the parent's person", () => {
    expect(freeSubtasks(cards, "p")).toEqual([
      { itemId: "s1", patch: { parent: undefined, assignees: ["kvaps"] } },
      { itemId: "s2", patch: { parent: undefined } },
    ]);
  });

  it("leaves other parents' subtasks alone, and a childless parent frees nothing", () => {
    expect(freeSubtasks(cards, "q").map((f) => f.itemId)).toEqual(["other"]);
    expect(freeSubtasks(cards, "nobody")).toEqual([]);
  });

  it("frees a subtask as its own card when the parent had nobody either", () => {
    const orphan = [
      { itemId: "p", assignees: [], startDate: "2026-09-01" },
      { itemId: "s", parent: "p", assignees: [], startDate: "2026-09-01" },
    ];
    expect(freeSubtasks(orphan, "p")).toEqual([{ itemId: "s", patch: { parent: undefined } }]);
  });

  // The subtask of a PARKED parent rode it out of every day: freed, it has no
  // place of its own and lands in its team's backlog, as the server parks it
  // (K1, boardservice freeChildren) — a plain park, nothing else moves.
  it("parks a freed subtask that has no date and no sprint (K1)", () => {
    const shelved = [
      { itemId: "p", assignees: [], parked: true },
      { itemId: "s", parent: "p", assignees: [] },
    ];
    expect(freeSubtasks(shelved, "p")).toEqual([{ itemId: "s", patch: { parent: undefined, parked: true } }]);
  });
});

// The optimistic state the × leaves on a subtask taken out of its group — the server's own
// fields and no others. Both boards patch from here, so one gesture cannot
// read two ways on two boards. A subtask the × deletes is not patched: the
// board drops the row.
describe("subtaskRemovalPatch", () => {
  const ctx = { today: "2026-08-24", current: "2026-08-24", previous: "2026-08-17" };
  const kid = {
    parent: "p",
    sprintStart: "2026-08-24",
    startDate: "2026-08-20",
    day: "2026-08-24",
  };

  it("takes the person and the sprint, and leaves the dates", () => {
    expect(subtaskRemovalPatch(mk(kid), ctx)).toEqual({
      assignees: [],
      sprintStart: undefined,
      parent: undefined,
    });
  });

  // The inverse has to cover the gesture: a board that rolls back fewer
  // fields than it patched leaves the card ungrouped on screen while the
  // server still holds it as a subtask.
  it("has an undo for every field it can write", () => {
    const written = subtaskRemovalPatch(mk(kid), ctx);
    const undo = subtaskRemovalUndo(kid);
    for (const key of Object.keys(written)) {
      expect(undo).toHaveProperty(key);
    }
    // …with the card's own values, not empty ones.
    expect(undo.parent).toBe("p");
    expect(undo.day).toBe("2026-08-24");
  });

  it("leaves a card with no end day without one", () => {
    const noDay = { ...kid, day: undefined };
    expect(subtaskRemovalPatch(mk(noDay), ctx)).not.toHaveProperty("day");
  });
});

// Work finished LATE belongs to the sprint it was done in, not the one it was
// carried into.
//
// The shape it comes in: an engineer finished a card and never moved the bar.
// Carry Over took it for open work and pulled it into the new sprint. Somebody
// then noticed and marked it 100 — and there it stands, in a sprint it was
// never worked in, counting against it.
//
// The board KEEPS showing it, deliberately: a finished card standing in the
// current sprint is the signal that this happened, and dropping it quietly
// would lose the only prompt anybody gets. What clears the signal is somebody
// SAYING where the work belongs, which is this answer.
describe("sending finished work back to the sprint it was done in", () => {
  const ctx = { current: "2026-08-24", previous: "2026-08-17", today: "2026-08-29" };
  const done = { progress: 100, sprintStart: "2026-08-24", startDate: "2026-08-28" };

  it("is offered on a finished card standing in the current sprint", () => {
    expect(removeChoices(done, ctx)).toContain("finished-earlier");
  });

  it("is not offered on work that is still going", () => {
    // Unfinished work in this sprint is this sprint's, whatever else is true
    // of it: there is nothing to record as done anywhere else.
    expect(removeChoices({ ...done, progress: 60 }, ctx)).not.toContain("finished-earlier");
  });

  it("is not offered when there is no earlier sprint to send it to", () => {
    // A team's first sprint has nothing behind it.
    expect(
      removeChoices(done, { current: "2026-08-24", today: "2026-08-29" }),
    ).not.toContain("finished-earlier");
  });

  it("is not offered on a card already in the earlier sprint", () => {
    // It is where the answer would put it, and an option that does nothing
    // reads as one that failed.
    expect(
      removeChoices({ ...done, sprintStart: "2026-08-17" }, ctx),
    ).not.toContain("finished-earlier");
  });

  it("stands after the answers that destroy, and before the backlog", () => {
    // Most destructive first, and the shelf is for work still to be done —
    // which this is not.
    const got = removeChoices(done, ctx);
    expect(got.indexOf("finished-earlier")).toBeGreaterThan(got.indexOf("off-board"));
  });
});

describe("what the × offers", () => {
  const ctx = { current: "2026-08-24", previous: "2026-08-17", today: "2026-08-29" };
  // Somebody is carrying it: that, and not its dates, is what "move it to
  // Unassigned" would change. A dated card nobody has taken is DRAWN in
  // Unassigned too, and reading the working area as "not in Unassigned"
  // offered such a card a move to the column it was already standing in.
  const held = {
    assignees: ["kvaps"],
    sprintStart: "2026-08-24",
    startDate: "2026-08-28",
  };

  // A task's × has two answers that keep or lose it: off the board, or back
  // in its team's BACKLOG (K3) — parked, nobody, no day, no sprint. The
  // backlog is always there to go to, so it is offered to every task that
  // is not in it already, whoever holds it and whatever dates it has. It used
  // to be "move it to Unassigned, back in its week", offered only to a card
  // with a week and refused (nowhereToLeaveIt) to one without.
  it("offers a task its backlog and the board, nothing in between (K3)", () => {
    expect(removeChoices(held, ctx)).toEqual(["backlog", "off-board"]);
    // Nobody holding it changes nothing: the backlog is still somewhere else.
    expect(removeChoices({ sprintStart: "2026-08-24" }, ctx)).toEqual(["backlog", "off-board"]);
    expect(removeChoices({}, ctx)).toEqual(["backlog", "off-board"]);
  });

  it("offers the backlog once", () => {
    const stale = { ...held, week: "2026-08-24" } as Removable;
    expect(removeChoices(stale, ctx).filter((c) => c === "backlog")).toHaveLength(1);
  });

  // The BACKLOG is the answer that destroys nothing, so it is read FIRST,
  // above taking the card off the board. Never for a card already in the backlog, and never for a subtask, which
  // stands inside its parent and would be parked out of sight of it.
  it("offers the backlog first, above the board, as the answer that destroys nothing", () => {
    const got = removeChoices({ ...held, progress: 100 }, ctx);
    expect(got).toEqual(["backlog", "off-board", "finished-earlier"]);
  });

  // The × acting UNASKED — a card made here that nobody touched — still
  // means what it always meant: off the board, deleted without ceremony. The
  // backlog's place at the top of the dialog does not make it the default.
  it("takes a card off the board when it acts unasked, the backlog first in the list or not", () => {
    expect(unaskedRemoval(["backlog", "off-board"])).toBe("off-board");
    expect(unaskedRemoval(["backlog", "off-board", "finished-earlier"])).toBe("off-board");
    expect(unaskedRemoval(["off-board"])).toBe("off-board");
    expect(unaskedRemoval(["withdraw"])).toBe("withdraw");
    expect(unaskedRemoval([])).toBeUndefined();
  });

  it("does not offer it to a card already in its backlog", () => {
    expect(removeChoices({ ...held, parked: true }, ctx)).not.toContain("backlog");
  });

  it("does not offer it to a subtask", () => {
    // It stands inside its parent; parking it alone would put it where
    // nothing draws it, out of sight of its own parent.
    expect(removeChoices({ ...held, parent: "p1" }, ctx)).not.toContain("backlog");
  });

  // A REVIEW somebody has is offered "back to the backlog" (K10): the
  // reviewer is taken off and it waits for one again in the reviewed task's
  // team backlog. One already waiting there is not offered it again.
  it("offers a review somebody has the backlog, above withdrawing it", () => {
    expect(removeChoices({ ...held, reviewOf: "c9" }, ctx)).toEqual(["backlog", "withdraw"]);
    expect(removeChoices({ reviewOf: "c9", parked: true }, ctx)).not.toContain("backlog");
  });

  it("keeps drawing the × wherever there is something to do", () => {
    expect(offersRemoval(held, ctx)).toBe(true);
    expect(offersRemoval({ sprintStart: "2026-08-24" }, ctx)).toBe(true);
    expect(offersRemoval({}, ctx)).toBe(true);
  });

  // A SUBTASK has no home of its own to be left in: the × takes it off the
  // board.
  it("takes a subtask off the board", () => {
    expect(removeChoices({ ...held, parent: "p" }, ctx)).toEqual(["off-board"]);
  });
});


// What the × sends. The keeping answer is the server's UNASSIGN intent —
// it parks a task in its team's backlog (boardservice.Remove, K3) — so the board never says "park" through a
// second door that judges the card differently.
describe("the intent an answer is sent as", () => {
  it("sends the backlog as unassign, off the board as off-board", () => {
    expect(removeIntent("backlog")).toBe("unassign");
    expect(removeIntent("off-board")).toBe("off-board");
  });

  it("sends nothing for an answer that is a gesture of its own", () => {
    expect(removeIntent("finished-earlier")).toBeNull();
    expect(removeIntent("detach")).toBeNull();
  });
});

// What the board shows the moment the backlog answer is given, before the
// server answers: a task parked — nobody, no day, no sprint, planned (K3) —
// and a REVIEW the same, and moved to the reviewed task's team, whose
// backlog it waits in for a reviewer (K10).
describe("the backlog answer, optimistically", () => {
  const cards = [{ itemId: "t", team: "alpha" }];

  it("parks a task where it is", () => {
    const got = backlogPatch({ team: "beta" }, cards);
    expect(got).toMatchObject({ parked: true, zone: "gray", assignees: [] });
    expect(got).not.toHaveProperty("team");
  });

  it("parks a review in the reviewed task's team backlog, nobody on it", () => {
    const got = backlogPatch({ team: "beta", reviewOf: "t" }, cards);
    expect(got).toMatchObject({ parked: true, assignees: [], team: "alpha" });
    expect(got.startDate).toBeUndefined();
    expect(got.sprintStart).toBeUndefined();
  });

  it("leaves a review's team when its task is not on this board", () => {
    expect(backlogPatch({ team: "beta", reviewOf: "gone" }, cards)).not.toHaveProperty("team");
  });
});

// The × on a review NOBODY HAS TAKEN — the Backlog's needs-a-reviewer item —
// WITHDRAWS the review (K10): the card goes and the task leaves review, back
// to In progress. It is the only answer, sent as off-board; the server turns
// it into the withdrawal (boardservice.withdrawReview).
describe("the × on a review nobody has taken", () => {
  const ctx = { today: "2026-08-28" };

  it("offers the withdrawal and nothing else", () => {
    expect(removeChoices({ reviewOf: "t", parked: true }, ctx)).toEqual(["withdraw"]);
    expect(removeChoices({ reviewOf: "t", parked: true, assignees: [] }, ctx)).toEqual(["withdraw"]);
  });

  // A review somebody HAS is withdrawn the same way — never taken off the
  // board by itself, which would leave its task on review with no review
  // card (K10) — and can go back to the backlog instead (K12).
  it("offers a review somebody has the backlog and the withdrawal, and never a bare off-the-board", () => {
    const got = removeChoices({ reviewOf: "t", assignees: ["bob"], sprintStart: "2026-08-24" }, ctx);
    expect(got).toEqual(["backlog", "withdraw"]);
    expect(got).not.toContain("off-board");
  });

  it("sends the withdrawal as off-board", () => {
    expect(removeIntent("withdraw")).toBe("off-board");
  });

  // The task goes back to In progress the way the stage menu's In progress
  // puts it (board.ApplyInProgress): the stage cleared, the progress nudged
  // into its band at the edges only.
  it("puts the task back to In progress, its progress nudged into the band", () => {
    expect(withdrawPatch({ stage: "review", progress: 60 })).toEqual({ stage: undefined, progress: 60 });
    expect(withdrawPatch({ stage: "review", progress: 5 })).toEqual({ stage: undefined, progress: 10 });
    expect(withdrawPatch({ stage: "review", progress: 100 })).toEqual({ stage: undefined, progress: 90 });
  });
});

// The Team drag of a REVIEW card into a column names its reviewer, and the
// server reads that as the owner picker's gesture (K10, K12): into
// Unassigned, a taken review goes back to the backlog with nobody on it; a
// person on a review nobody has taken takes it into work. The board shows
// the same at once.
describe("a review card's person, set by a drag", () => {
  it("hands a taken review back to the backlog when nobody is named", () => {
    expect(reviewAssigneePatch({ reviewOf: "t", assignees: ["bob"] }, [])).toMatchObject({ parked: true, assignees: [], zone: "gray" });
  });

  it("takes an unclaimed review into work when a person is named", () => {
    expect(reviewAssigneePatch({ reviewOf: "t", assignees: [], parked: true }, ["ann"])).toMatchObject({ parked: false, assignees: ["ann"] });
  });

  it("is a plain change of person on anything else", () => {
    expect(reviewAssigneePatch({ assignees: ["bob"] }, [])).toEqual({ assignees: [] });
    expect(reviewAssigneePatch({ reviewOf: "t", assignees: ["bob"] }, ["ann"])).toEqual({ assignees: ["ann"] });
  });
});
