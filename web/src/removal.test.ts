import { describe, expect, it } from "vitest";
import { boardAsksAbout, deleteWarning, freeSubtasks, gridRemoval, personalRemovalKind, gridGesture, removalKind, subtaskRemovalPatch, subtaskRemovalUndo, type RemovableCard, type RemovalHomes } from "./removal";

const ctx = { current: "2026-08-26", previous: "2026-08-25", today: "2026-08-26" };
const card = { sprintStart: "2026-08-26", startDate: "2026-08-20", progress: 0 };

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


// One outcome now, whatever sprint the card is in: the × empties its last
// home and the card goes. The demote it replaced is where the board's
// invisible pile came from — cards alive in a sprint no live view reaches
// (mirrors boardservice.Remove).
describe("removalKind", () => {
  it("deletes a card of the current sprint", () => {
    expect(removalKind(card, ctx)).toBe("delete");
  });

  it("deletes a card created today", () => {
    expect(removalKind({ ...card, startDate: "2026-08-26" }, ctx)).toBe("delete");
  });

  it("deletes whether or not the team has a previous sprint", () => {
    expect(removalKind(card, { ...ctx, previous: undefined })).toBe("delete");
  });

  it("deletes a card that is not in the current sprint", () => {
    expect(removalKind({ ...card, sprintStart: "2026-08-20" }, ctx)).toBe("delete");
  });

  // The one question left: the card carries work, and the × takes it off
  // the board. The day it stood on keeps it, but today's board will not.
  it("asks when the card carries progress", () => {
    expect(removalKind({ ...card, progress: 40 }, ctx)).toBe("ask");
    // Created today or not — work is work.
    expect(removalKind({ ...card, startDate: "2026-08-26", progress: 40 }, ctx)).toBe("ask");
  });

  it("does not ask for an untouched card — nothing is lost", () => {
    expect(removalKind({ ...card, progress: 0 }, ctx)).toBe("delete");
  });
});

// The two rules meet here. A card filed under a Project-board column must
// never be destroyed by the ×, so the dialog must not stand in front of one:
// with a column there is nothing to lose, and the server hands the card back
// to it.
describe("removalKind on a card that belongs to a column", () => {
  const worked = { sprintStart: "2026-08-26", startDate: "2026-08-20", progress: 40 };

  it("never asks for a card with an epic", () => {
    expect(removalKind({ ...worked, epic: "Cozystack Core" }, ctx)).toBe("delete");
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

// The grid × empties one of a card's homes — the working area — and a card
// that was nowhere else is deleted by it. What the card carries does not
// change that; it decides whether the × asks first.
describe("what each × does, one answer for every board", () => {
  const ctx = { current: "2026-08-24", previous: "2026-08-17", today: "2026-08-29" };
  const inSprint = { sprintStart: "2026-08-24", startDate: "2026-08-29" };

  it("sends a card back to the week it is scheduled for", () => {
    expect(gridRemoval({ ...inSprint, week: "2026-08-24" }, ctx)).toBe("leave");
    // Even one carrying work: the week is where it goes, not the bin.
    expect(gridRemoval({ ...inSprint, week: "2026-08-24", progress: 40 }, ctx)).toBe(
      "leave",
    );
  });

  // The Me board asked this question on its own and answered it differently
  // for a subtask, which the server then applied to the child alone —
  // splitting it from its parent, and looking like the × had done nothing.
  it("deletes a subtask, which has no home of its own", () => {
    expect(gridRemoval({ ...inSprint, parent: "p" }, ctx)).toBe("delete");
    expect(gridRemoval({ ...inSprint, parent: "p", week: "2026-08-24" }, ctx)).toBe(
      "delete",
    );
    // A COLUMN is the exception: it is a home of its own, drawn and counted
    // on the Project board (G57), and a card filed under one is never
    // deleted by either ×.
    expect(gridRemoval({ ...inSprint, parent: "p", epic: "Auth" }, ctx)).toBe("ungroup");
  });

  it("deletes a card of the current sprint, which is nowhere else", () => {
    expect(gridRemoval({ sprintStart: "2026-08-24", startDate: "2026-08-24" }, ctx)).toBe(
      "delete",
    );
  });

  it("leaves a slot to its column and deletes a card with nowhere else", () => {
    expect(gridRemoval({ ...inSprint, epic: "Auth" }, ctx)).toBe("leave");
    expect(gridRemoval(inSprint, ctx)).toBe("delete");
    // A bare project name is a label, on no board: it spares nothing.
    expect(gridRemoval({ ...inSprint, project: "core" }, ctx)).toBe("delete");
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
    expect(boardAsksAbout({ title: "x" }, "leave", null)).toBe(true);
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
        mk({ parent: "p1", project: "engineering", epic: "Cozystack", sprintStart: "2026-08-24" }),
        ctx,
      ),
    ).toBe("ungroup");
  });

  it("deletes a subtask with nowhere else to be", () => {
    expect(gridRemoval(mk({ parent: "p1", sprintStart: "2026-08-24" }), ctx)).toBe("delete");
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
    expect(gridRemoval(mk(worked), ctx)).toBe("delete");
    // The board takes the question on — and must actually put it.
    expect(boardAsksAbout(worked, "delete", null)).toBe(true);
    expect(deleteWarning(worked, null)).toContain("60%");
  });

  // The × on a subtask whose column belongs to the PARENT's repository: the
  // pull-out cannot take the column along, so the card has no home left and
  // is deleted. The board must know, or it patches the card as kept and
  // destroys work without asking (the server's
  // TestAStrandedColumnDoesNotLeaveACardNowhere).
  it("scores a columned subtask whose column cannot follow it out", () => {
    const kid = { title: "child", parent: "p", epic: "Closed", progress: 60,
      sprintStart: "2026-08-24" };
    expect(gridRemoval(mk(kid), { ...ctx, columnFollows: false })).toBe("delete");
    expect(gridRemoval(mk(kid), {
      today: "2026-08-24", current: "2026-08-24", columnFollows: false,
    })).toBe("delete");
    // ...and the board takes the question on, with the loss named.
    expect(boardAsksAbout(kid, "delete", null)).toBe(true);
    expect(deleteWarning(kid, null)).toContain("60%");
    // The column CAN follow on a single-repository board, where the
    // question does not arise: the × ungroups and keeps it.
    expect(gridRemoval(mk(kid), ctx)).toBe("ungroup");
  });
});

// Which gesture the day grid's × performs — the routing itself, lifted out
// of the boards. A columned subtask is RELEASED (ungrouped into its
// column); a card with no home left is DELETED, and asked about first when
// it carries work.
describe("gridGesture", () => {
  const ctx = { today: "2026-08-24", current: "2026-08-24", previous: "2026-08-17" };

  it("releases a columned subtask, previous sprint or not", () => {
    const card = { parent: "p", epic: "Cozystack", sprintStart: "2026-08-24" };
    expect(gridGesture(mk(card), ctx)).toBe("release");
  });

  it("deletes a columnless subtask", () => {
    expect(gridGesture(mk({ parent: "p", sprintStart: "2026-08-24" }), ctx)).toBe("delete");
  });

  it("deletes a columned subtask whose column stays behind", () => {
    const kid = { parent: "p", epic: "Closed", sprintStart: "2026-08-24" };
    expect(gridGesture(mk(kid), { ...ctx, columnFollows: false })).toBe("delete");
  });

  // The question applies here like anywhere else: the × takes the card off
  // the board AND drops the column it stood in — and a subtask exempted
  // from the question was the one card on the grid whose work could leave
  // without one being asked.
  it("asks about a worked subtask whose column stays behind", () => {
    const worked = { parent: "p", epic: "Closed", progress: 60, sprintStart: "2026-08-24" };
    expect(gridGesture(mk(worked), { ...ctx, columnFollows: false })).toBe("ask");
    // Its column can follow on a board with one repository: nothing is
    // lost, so nothing is asked.
    expect(gridGesture(mk(worked), ctx)).toBe("release");
  });

  it("asks before an × that takes work off the board", () => {
    expect(
      gridGesture(
        mk({ sprintStart: "2026-08-24", startDate: "2026-08-20", progress: 40 }),
        ctx,
      ),
    ).toBe("ask");
  });

  it("deletes an untouched card with nowhere else to be, unasked", () => {
    expect(
      gridGesture(mk({ sprintStart: "2026-08-24", startDate: "2026-08-20" }), ctx),
    ).toBe("delete");
  });
});

// The optimistic state the × leaves on a RELEASED subtask — the server's own
// fields and no others. Both boards patch from here, so one gesture cannot
// read two ways on two boards. A subtask the × deletes is not patched: the
// board drops the row.
describe("subtaskRemovalPatch", () => {
  const ctx = { today: "2026-08-24", current: "2026-08-24", previous: "2026-08-17" };
  const kid = {
    parent: "p",
    epic: "Closed",
    project: "strategy",
    sprintStart: "2026-08-24",
    startDate: "2026-08-20",
    day: "2026-08-24",
  };

  it("releases into the column: the person and the sprint go, the dates stay", () => {
    expect(subtaskRemovalPatch(mk(kid), ctx)).toEqual({
      assignees: [],
      sprintStart: undefined,
      parent: undefined,
    });
  });

  // A subtask whose column cannot follow it out is DELETED now (the board
  // drops the row rather than patching it), so the release patch is the
  // only one there is — and it must not depend on the context, or a board
  // would patch a card it is about to remove.
  it("is the same patch whether or not the column can follow", () => {
    expect(subtaskRemovalPatch(mk(kid), { ...ctx, columnFollows: false })).toEqual(
      subtaskRemovalPatch(mk(kid), ctx),
    );
  });

  // The inverse has to cover the gesture: a board that rolls back fewer
  // fields than it patched leaves the card ungrouped and stripped of its
  // column on screen while the server still holds it as a subtask in it.
  it("has an undo for every field it can write", () => {
    const written = {
      ...subtaskRemovalPatch(mk(kid), ctx),
      ...subtaskRemovalPatch(mk(kid), { ...ctx, columnFollows: false }),
    };
    const undo = subtaskRemovalUndo(kid);
    for (const key of Object.keys(written)) {
      expect(undo).toHaveProperty(key);
    }
    // …with the card's own values, not empty ones.
    expect(undo.parent).toBe("p");
    expect(undo.epic).toBe("Closed");
    expect(undo.day).toBe("2026-08-24");
  });

  it("leaves a card with no end day without one", () => {
    const noDay = { ...kid, day: undefined };
    expect(
      subtaskRemovalPatch(mk(noDay), { ...ctx, columnFollows: false }),
    ).not.toHaveProperty("day");
  });
});
