import { describe, expect, it } from "vitest";
import {
  asksFirst,
  deleteWarning,
  freeSubtasks,
  gridRemoval,
  offersRemoval,
  removeChoices,
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

  // The week counts as somewhere to go only while the card is ALSO in the
  // working area. One that is nothing but its week — already standing in
  // Unassigned — has no second home, and the × empties its last one. This
  // rule drifted from the server's once, and the dialog then promised such a
  // card it would be moved to Unassigned while the server deleted it.
  it("deletes a card that is nothing but its week", () => {
    expect(gridRemoval({ week: "2026-08-24" }, ctx)).toBe("delete");
  });

  // A PROCESS TURN never runs out: its week is its process's record of what
  // that week was owed, and destroying it would lose the record.
  it("always leaves a process turn in its week", () => {
    expect(gridRemoval({ ...inSprint, week: "2026-08-24", task: "t1" }, ctx)).toBe(
      "leave",
    );
    expect(gridRemoval({ week: "2026-08-24", task: "t1" }, ctx)).toBe("leave");
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

// The × asks before it acts — every time, whatever it is about to do, so
// that a card handed back to Unassigned and a card taken off the board are
// told apart BEFORE either happens. Four rules used to decide this between
// them (removalKind, personalRemovalKind, gridGesture, boardAsksAbout) and
// they disagreed: an × that hands a card back went in silence on one board
// and asked on another, and a worked card could be destroyed unasked.
describe("when the × asks first", () => {
  const today = "2026-08-28";
  const bornToday = `${today}T09:00:00Z`;

  it("does not ask about a card made today that nobody has touched", () => {
    expect(asksFirst({ progress: 0, createdAt: bornToday }, today)).toBe(false);
    expect(asksFirst({ createdAt: bornToday }, today)).toBe(false);
  });

  it("asks once there is work on it, however fresh the card", () => {
    expect(asksFirst({ progress: 10, createdAt: bornToday }, today)).toBe(true);
  });

  it("asks about a card that has been on the board since yesterday", () => {
    expect(asksFirst({ progress: 0, createdAt: "2026-08-27T18:00:00Z" }, today)).toBe(true);
  });

  it("asks when nothing says when the card was made", () => {
    // A card whose age the board cannot vouch for is not one to remove in
    // silence: the silent case is the mis-typed card of a moment ago, and
    // this is not known to be one.
    expect(asksFirst({ progress: 0 }, today)).toBe(true);
  });
});

// What the × may do to a card WHERE IT STANDS. The gesture used to work this
// out for itself and do the one thing it decided, so taking a card with a
// week off the board took two presses — the second landing on a card that no
// longer looked like the one the person meant to remove. The person chooses
// now, out of the card's own list, and the × is drawn only where that list is
// not empty.
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

  it("offers an ordinary card the board, and its week beside it", () => {
    expect(removeChoices({ ...held, week: "2026-08-24" }, ctx)).toEqual([
      "off-board",
      "unassign",
    ]);
  });

  it("offers only the board to a card with nowhere to be left", () => {
    // No week and no column: unassigning would leave it nowhere at all.
    expect(removeChoices(held, ctx)).toEqual(["off-board"]);
  });

  it("offers only the board to a card standing in Unassigned", () => {
    // Nobody is carrying it, dates or no dates: it is already there.
    expect(removeChoices({ week: "2026-08-24" }, ctx)).toEqual(["off-board"]);
    expect(
      removeChoices({ week: "2026-08-24", sprintStart: "2026-08-24" }, ctx),
    ).toEqual(["off-board"]);
  });

  it("offers a PROJECT card its column and nothing else", () => {
    // The Project board's commitment is not this board's to destroy.
    expect(removeChoices({ ...held, epic: "Auth" }, ctx)).toEqual(["unassign"]);
  });

  it("offers a PROCESS TURN its week and nothing else", () => {
    expect(removeChoices({ ...held, task: "t1", week: "2026-08-24" }, ctx)).toEqual([
      "unassign",
    ]);
  });

  it("offers a project card in Unassigned nothing at all — so no × is drawn", () => {
    expect(removeChoices({ epic: "Auth" }, ctx)).toEqual([]);
    expect(offersRemoval({ epic: "Auth" }, ctx)).toBe(false);
    // Dated, and still nobody's: the × has nothing to do here either.
    expect(offersRemoval({ epic: "Auth", sprintStart: "2026-08-24" }, ctx)).toBe(false);
    expect(removeChoices({ task: "t1", week: "2026-08-24" }, ctx)).toEqual([]);
    expect(offersRemoval({ task: "t1", week: "2026-08-24" }, ctx)).toBe(false);
  });

  it("keeps drawing the × wherever there is something to do", () => {
    expect(offersRemoval({ ...held, epic: "Auth" }, ctx)).toBe(true);
    expect(offersRemoval({ week: "2026-08-24" }, ctx)).toBe(true);
    expect(offersRemoval({}, ctx)).toBe(true);
  });

  // A SUBTASK is its own case: out of the group where it has a column to be
  // left in, off the board where it has not.
  it("takes a subtask out of its group, or off the board", () => {
    expect(removeChoices({ ...held, parent: "p", epic: "Auth" }, ctx)).toEqual([
      "ungroup",
    ]);
    expect(removeChoices({ ...held, parent: "p" }, ctx)).toEqual(["off-board"]);
  });
});

