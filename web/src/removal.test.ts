import { describe, expect, it } from "vitest";
import {
  deleteWarning,
  freeSubtasks,
  gridRemoval,
  offersRemoval,
  removeIntent,
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

  // A task has no week to be sent back to any more (K7): the × off the board
  // deletes what has no column, whatever an older repository left on it. The
  // answer that keeps the work is the BACKLOG, which the dialog offers
  // (removeChoices) and which is not this function's to take unasked.
  it("has no week to send a card back to (K7)", () => {
    const withAStaleWeek = { ...inSprint, week: "2026-08-24" } as Removable;
    expect(gridRemoval(withAStaleWeek, ctx)).toBe("delete");
    expect(gridRemoval({ ...withAStaleWeek, progress: 40 } as Removable, ctx)).toBe("delete");
  });

  // The Me board asked this question on its own and answered it differently
  // for a subtask, which the server then applied to the child alone —
  // splitting it from its parent, and looking like the × had done nothing.
  it("deletes a subtask, which has no home of its own", () => {
    expect(gridRemoval({ ...inSprint, parent: "p" }, ctx)).toBe("delete");
    // A column written on a subtask (a repository from before schema 2)
    // spares it nothing: a card in a column is nobody's subtask (H4), so
    // the pair is not a home and the × answers for the subtask.
    expect(gridRemoval({ ...inSprint, parent: "p", scope: "Auth" }, ctx)).toBe("delete");
  });

  it("deletes a card of the current sprint, which is nowhere else", () => {
    expect(gridRemoval({ sprintStart: "2026-08-24", startDate: "2026-08-24" }, ctx)).toBe(
      "delete",
    );
  });

  it("leaves a slot to its column and deletes a card with nowhere else", () => {
    expect(gridRemoval({ ...inSprint, scope: "Auth" }, ctx)).toBe("leave");
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

// The grid × obeys the two-homes rule for a subtask too: with a column to
// fall back on it lets the card go there, and only a subtask with nowhere
// else is deleted. Before the Project board drew such a card, nobody could
// see what the × destroyed.
describe("gridRemoval on a subtask", () => {
  const ctx = { today: "2026-08-24", current: "2026-08-24", previous: "2026-08-17" };

  it("deletes a subtask whatever column is written on it (H4: a project card is nobody's subtask)", () => {
    expect(
      gridRemoval(
        mk({ parent: "p1", project: "engineering", scope: "Cozystack", sprintStart: "2026-08-24" }),
        ctx,
      ),
    ).toBe("delete");
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
    scope: "Closed",
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

  // The inverse has to cover the gesture: a board that rolls back fewer
  // fields than it patched leaves the card ungrouped and stripped of its
  // column on screen while the server still holds it as a subtask in it.
  it("has an undo for every field it can write", () => {
    const written = subtaskRemovalPatch(mk(kid), ctx);
    const undo = subtaskRemovalUndo(kid);
    for (const key of Object.keys(written)) {
      expect(undo).toHaveProperty(key);
    }
    // …with the card's own values, not empty ones.
    expect(undo.parent).toBe("p");
    expect(undo.scope).toBe("Closed");
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

  it("is not offered for work another board owns", () => {
    // A Project-board slot carries its own dates, and the sprint is not what
    // places it.
    expect(removeChoices({ ...done, scope: "Auth" }, ctx)).not.toContain("finished-earlier");
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
  it("offers a task the board and its backlog, nothing in between (K3)", () => {
    expect(removeChoices(held, ctx)).toEqual(["off-board", "backlog"]);
    // Nobody holding it changes nothing: the backlog is still somewhere else.
    expect(removeChoices({ sprintStart: "2026-08-24" }, ctx)).toEqual(["off-board", "backlog"]);
    expect(removeChoices({}, ctx)).toEqual(["off-board", "backlog"]);
  });

  it("offers the backlog once: never as Unassigned beside it", () => {
    // Unassigned is the PROJECT card's answer (its column keeps it); a task's
    // keeping answer is the backlog, and offering both read as two ways of
    // saying the same thing that the server answered differently.
    const stale = { ...held, week: "2026-08-24" } as Removable;
    expect(removeChoices(stale, ctx)).not.toContain("unassign");
    expect(removeChoices(stale, ctx).filter((c) => c === "backlog")).toHaveLength(1);
  });

  // The BACKLOG is the answer that destroys nothing, so it comes last — the ×
  // unasked still means what it always meant (a card just typed is deleted
  // without ceremony), and the dialog is where the backlog becomes an
  // option. Never for work another board owns, never for a card already in
  // the backlog, and never for a subtask or a review, which follow the card
  // they belong to and would be parked out of sight of it.
  it("offers the backlog last, as the answer that destroys nothing", () => {
    const got = removeChoices({ ...held, progress: 100 }, ctx);
    expect(got).toEqual(["off-board", "finished-earlier", "backlog"]);
  });

  it("does not offer it to work another board owns", () => {
    expect(removeChoices({ ...held, scope: "Auth" }, ctx)).not.toContain("backlog");
  });

  it("does not offer it to a card already in its backlog", () => {
    expect(removeChoices({ ...held, parked: true }, ctx)).not.toContain("backlog");
  });

  it("does not offer it to a subtask or a review", () => {
    // Both follow the card they belong to; parking one alone would put it
    // where nothing draws it, out of sight of its own parent.
    expect(removeChoices({ ...held, parent: "p1" }, ctx)).not.toContain("backlog");
    expect(removeChoices({ ...held, reviewOf: "c9" }, ctx)).not.toContain("backlog");
  });

  it("offers a PROJECT card its column and nothing else", () => {
    // The Project board's commitment is not this board's to destroy.
    expect(removeChoices({ ...held, scope: "Auth" }, ctx)).toEqual(["unassign"]);
  });

  it("offers a project card in Unassigned nothing at all — so no × is drawn", () => {
    expect(removeChoices({ scope: "Auth" }, ctx)).toEqual([]);
    expect(offersRemoval({ scope: "Auth" }, ctx)).toBe(false);
    // Dated, and still nobody's: the × has nothing to do here either.
    expect(offersRemoval({ scope: "Auth", sprintStart: "2026-08-24" }, ctx)).toBe(false);
  });

  it("keeps drawing the × wherever there is something to do", () => {
    expect(offersRemoval({ ...held, scope: "Auth" }, ctx)).toBe(true);
    expect(offersRemoval({ sprintStart: "2026-08-24" }, ctx)).toBe(true);
    expect(offersRemoval({}, ctx)).toBe(true);
  });

  // A SUBTASK has no home of its own to be left in: the × takes it off the
  // board. (It used to be left in a column it carried; a card in a column
  // is nobody's subtask now, H4.)
  it("takes a subtask off the board", () => {
    expect(removeChoices({ ...held, parent: "p", scope: "Auth" }, ctx)).toEqual(["off-board"]);
    expect(removeChoices({ ...held, parent: "p" }, ctx)).toEqual(["off-board"]);
  });
});


// What the × sends. Both keeping answers are the server's UNASSIGN intent —
// it parks a task in its team's backlog and leaves a project card in its
// column (boardservice.Remove, K3) — so the board never says "park" through a
// second door that judges the card differently.
describe("the intent an answer is sent as", () => {
  it("sends the backlog and Unassigned as unassign, off the board as off-board", () => {
    expect(removeIntent("backlog")).toBe("unassign");
    expect(removeIntent("unassign")).toBe("unassign");
    expect(removeIntent("off-board")).toBe("off-board");
  });

  it("sends nothing for an answer that is a gesture of its own", () => {
    expect(removeIntent("finished-earlier")).toBeNull();
    expect(removeIntent("detach")).toBeNull();
  });
});
