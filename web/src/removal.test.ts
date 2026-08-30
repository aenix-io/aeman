import { describe, expect, it } from "vitest";
import { boardAsksAbout, deleteWarning, freeSubtasks, gridRemoval, personalRemovalKind, planRemoval, gridGesture, removalKind } from "./removal";

const ctx = { current: "2026-08-26", previous: "2026-08-25", today: "2026-08-26" };
const card = { sprintStart: "2026-08-26", startDate: "2026-08-20", progress: 0 };

describe("removalKind", () => {
  it("demotes a card with sprint history behind it", () => {
    expect(removalKind(card, ctx)).toBe("demote");
  });

  it("deletes a card created today — there is no history worth keeping", () => {
    expect(removalKind({ ...card, startDate: "2026-08-26" }, ctx)).toBe("delete");
  });

  it("deletes when the team has no previous sprint to fall back to", () => {
    expect(removalKind(card, { ...ctx, previous: undefined })).toBe("delete");
  });

  it("deletes a card that is not in the current sprint", () => {
    expect(removalKind({ ...card, sprintStart: "2026-08-20" }, ctx)).toBe("delete");
  });

  // The choice: a demote silently takes the card (and its subtasks) off
  // today's board, which reads as deletion. When there is work to lose, the
  // person decides instead of the rule.
  it("asks when the card would be demoted AND carries progress", () => {
    expect(removalKind({ ...card, progress: 40 }, ctx)).toBe("ask");
  });

  it("does not ask for an untouched card — nothing is lost either way", () => {
    expect(removalKind({ ...card, progress: 0 }, ctx)).toBe("demote");
  });

  it("does not ask when the answer is a plain delete", () => {
    expect(removalKind({ ...card, startDate: "2026-08-26", progress: 40 }, ctx)).toBe("delete");
  });
});

// The two rules meet here. A card filed under a Project-board column must
// never be destroyed by the ×, so the choice dialog must not offer deletion
// for one: with a column there is nothing to choose, and the server hands the
// card back (demote, or release to the plan) either way.
describe("removalKind on a card that belongs to a column", () => {
  const worked = { sprintStart: "2026-08-26", startDate: "2026-08-20", progress: 40 };

  it("never asks for a card with an epic", () => {
    expect(removalKind({ ...worked, epic: "Cozystack Core" }, ctx)).toBe("demote");
  });

  // A bare project name is not a column: the Project board renders columns
  // by epic, so such a card is on no board and the × can destroy it — which
  // is exactly when the person must be asked.
  it("still asks for a card that only names a project", () => {
    expect(removalKind({ ...worked, project: "freedom" }, ctx)).toBe("ask");
  });

  it("still asks for a worked card that belongs to no column", () => {
    expect(removalKind(worked, ctx)).toBe("ask");
  });
})

describe("personalRemovalKind", () => {
  const today = "2026-08-28";
  it("asks about a worked-on card that did not start today — its history is at stake", () => {
    expect(personalRemovalKind({ progress: 40, startDate: "2026-08-20" }, today)).toBe("ask");
    expect(personalRemovalKind({ progress: 100, startDate: "2026-08-20" }, today)).toBe("ask");
    expect(personalRemovalKind({ progress: 10 }, today)).toBe("ask");
  });
  it("deletes an untouched card, or one that started today, without asking", () => {
    expect(personalRemovalKind({ progress: 0, startDate: "2026-08-20" }, today)).toBe("delete");
    expect(personalRemovalKind({ startDate: "2026-08-20" }, today)).toBe("delete");
    expect(personalRemovalKind({ progress: 60, startDate: today }, today)).toBe("delete");
    expect(personalRemovalKind({ progress: 20, startDate: "2026-08-30" }, today)).toBe("delete");
  });
});

// The grid × empties one of a card's two homes — the working area — and a
// card that was nowhere else is deleted by it. What the card carries does
// not change that; it decides whether the × asks first.
describe("what each × does, one answer for every board", () => {
  const ctx = { current: "2026-08-24", previous: "2026-08-17", today: "2026-08-29" };
  const inSprint = { sprintStart: "2026-08-24", startDate: "2026-08-29" };

  it("sends a card back to the weekly plan it is in", () => {
    expect(gridRemoval({ ...inSprint, plan: "fri" }, ctx)).toBe("leave");
    // Even one carrying work: the plan is where it goes, not the bin.
    expect(gridRemoval({ ...inSprint, plan: "fri", progress: 40 }, ctx)).toBe("leave");
  });

  // The Me board asked this question on its own and got "demote" for a
  // subtask, which the server then applied to the child alone — splitting it
  // from its parent, and, since a subtask left in an earlier sprint still
  // renders under its parent, looking like the × had done nothing.
  it("deletes a subtask, which has no history of its own", () => {
    expect(gridRemoval({ ...inSprint, parent: "p" }, ctx)).toBe("delete");
    expect(gridRemoval({ ...inSprint, parent: "p", plan: "fri" }, ctx)).toBe("delete");
    // A COLUMN is the exception: it is a home of its own, drawn and counted
    // on the Project board (G57), and a card filed under one is never
    // deleted by either ×.
    expect(gridRemoval({ ...inSprint, parent: "p", epic: "Auth" }, ctx)).toBe("ungroup");
  });

  it("demotes a card with sprint history", () => {
    expect(gridRemoval({ sprintStart: "2026-08-24", startDate: "2026-08-24" }, ctx)).toBe(
      "demote",
    );
  });

  it("leaves a slot to its column and deletes a card with nowhere else", () => {
    expect(gridRemoval({ ...inSprint, epic: "Auth" }, ctx)).toBe("leave");
    expect(gridRemoval(inSprint, ctx)).toBe("delete");
    // A bare project name is a label, on no board: it spares nothing.
    expect(gridRemoval({ ...inSprint, project: "core" }, ctx)).toBe("delete");
  });

  it("keeps a plan card that is somewhere else, deletes one that is not", () => {
    expect(planRemoval({ sprintStart: "2026-08-24" })).toBe("leave");
    expect(planRemoval({ startDate: "2026-08-29" })).toBe("leave");
    expect(planRemoval({ epic: "Auth" })).toBe("leave");
    expect(planRemoval({})).toBe("delete");
    // Being on a person or carrying work is not a place to be — the server
    // deletes these, and a client that spares them shows a card that is
    // already gone.
    expect(planRemoval({ day: undefined })).toBe("delete");
  });
});


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
    { itemId: "p", assignees: ["kvaps"] },
    { itemId: "s1", parent: "p", assignees: [] },
    { itemId: "s2", parent: "p", assignees: ["bob"] },
    { itemId: "other", parent: "q", assignees: [] },
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
    const orphan = [{ itemId: "p", assignees: [] }, { itemId: "s", parent: "p", assignees: [] }];
    expect(freeSubtasks(orphan, "p")).toEqual([{ itemId: "s", patch: { parent: undefined } }]);
  });
});

// The card's own "Delete?" prompt is for the plain case. It must stand down
// wherever the board puts its own question — the two-way choice for a worked
// card, and the named-loss warning before a delete — or the person answers
// two prompts for one ×, the first of them the anonymous one W5 exists to
// suppress.
describe("who asks about a delete", () => {
  it("stands down when the board opens its two-way choice", () => {
    expect(boardAsksAbout({ title: "x" }, "ask", null)).toBe(true);
  });

  it("stands down when the board will name what the delete takes", () => {
    expect(boardAsksAbout({ title: "x", progress: 40 }, "delete", null)).toBe(true);
    expect(boardAsksAbout({ title: "x" }, "delete", "Review: x")).toBe(true);
  });

  // A prompt reading "Delete?" in front of something that keeps the card is
  // how the × came to be read as deletion in the first place.
  it("stands down when the × does not delete at all", () => {
    expect(boardAsksAbout({ title: "x" }, "leave", null)).toBe(true);
    expect(boardAsksAbout({ title: "x" }, "demote", null)).toBe(true);
  });

  it("leaves the plain delete to the card", () => {
    expect(boardAsksAbout({ title: "x" }, "delete", null)).toBe(false);
  });
});

// The grid × obeys the two-homes rule for a subtask too: with a column to
// fall back on it lets the card go there, and only a subtask with nowhere
// else is deleted. Before the Project board drew such a card, nobody could
// see what the × destroyed.
describe("gridRemoval on a subtask", () => {
  const ctx = { today: "2026-08-24", current: "2026-08-24", previous: "2026-08-17" };

  it("ungroups a subtask that stands in a column, leaving it there", () => {
    expect(
      gridRemoval(
        { parent: "p1", project: "engineering", epic: "Cozystack", sprintStart: "2026-08-24" } as never,
        ctx,
      ),
    ).toBe("ungroup");
  });

  it("deletes a subtask with nowhere else to be", () => {
    expect(gridRemoval({ parent: "p1", sprintStart: "2026-08-24" } as never, ctx)).toBe("delete");
  });
});

// The board's own question stands down for every outcome that KEEPS the
// card. "Delete «…»?" in front of an × that ungroups and files the card in
// its column is how the × came to be read as deletion.
describe("boardAsksAbout for the ungroup outcome", () => {
  it("asks nothing: the card is kept", () => {
    expect(boardAsksAbout({ title: "x", progress: 0 }, "ungroup", null)).toBe(true);
  });
});

// Who asks before a destructive ×. `boardAsks` true means "the BOARD will
// ask, so the card's own anonymous prompt stands down" — a board that
// claims it and then deletes in silence is worse than one that never
// claimed it: that is a one-click loss of worked cards.
describe("who asks about a subtask", () => {
  const ctx = { today: "2026-08-24", current: "2026-08-24", previous: "2026-08-17" };
  const worked = { title: "half-done subtask", parent: "p", progress: 60, sprintStart: "2026-08-24" };

  it("scores a worked columnless subtask as a delete the board must name", () => {
    expect(gridRemoval(worked as never, ctx)).toBe("delete");
    // The board takes the question on — and must actually put it.
    expect(boardAsksAbout(worked, "delete", null)).toBe(true);
    expect(deleteWarning(worked, null)).toContain("60%");
  });

  it("scores a weekly-panel subtask by the gesture that will run", () => {
    // The panel dispatches a subtask to the GRID handler, so scoring it by
    // the plan's rule described an action nobody was going to take.
    expect(planRemoval(worked as never)).not.toBe(gridRemoval(worked as never, ctx));
  });
});

// Which gesture the day grid's × performs — the routing itself, lifted out
// of the boards. A columned subtask is RELEASED (ungrouped into its
// column), and a board that read "demote" for it sent the card through the
// demote path, rewriting its Project-board dates to the previous sprint
// and leaving it nested under its parent until a reload.
describe("gridGesture", () => {
  const ctx = { today: "2026-08-24", current: "2026-08-24", previous: "2026-08-17" };

  it("releases a columned subtask, previous sprint or not", () => {
    const card = { parent: "p", epic: "Cozystack", sprintStart: "2026-08-24" };
    expect(gridGesture(card as never, ctx)).toBe("release");
  });

  it("deletes a columnless subtask", () => {
    expect(gridGesture({ parent: "p", sprintStart: "2026-08-24" } as never, ctx)).toBe("delete");
  });

  it("asks when the board opens its two-way choice", () => {
    // A worked card in the current sprint with a previous one to fall back
    // on is W5's question, not a gesture: the board must put it.
    expect(
      gridGesture(
        { sprintStart: "2026-08-24", startDate: "2026-08-20", progress: 40 } as never,
        ctx,
      ),
    ).toBe("ask");
  });

  it("still demotes an ordinary card with sprint history", () => {
    // Not created today: a card whose start IS today has no history worth
    // keeping and is deleted instead (removalKind).
    expect(
      gridGesture({ sprintStart: "2026-08-24", startDate: "2026-08-20" } as never, ctx),
    ).toBe("demote");
  });
});

// The plan's × never deletes a subtask: its home is its parent, so the
// plan cannot be its last one (G57). Stated in this copy too, because a
// rule kept on one side only is how the boards drifted before.
describe("planRemoval on a subtask", () => {
  it("leaves it, whatever else it carries", () => {
    expect(planRemoval({ parent: "p" } as never)).toBe("leave");
  });
});
