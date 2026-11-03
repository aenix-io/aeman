import { keepPlaced, parkedLocally, type PlaceFields } from "./backlog";
import type { Card } from "./providers/types";
import type { LineageRef } from "./providers/types";
import { isSubtask } from "./subtasks";

export interface RemovableCard {
  sprintStart?: string;
  startDate?: string;
  progress?: number;
  /** A Project-board column is the PAIR (project, scope); either side filed
   *  means the card belongs to one. */
  scope?: string;
  project?: string;
  /** The card this one sits under. Under a TASK it is a subtask and rides
   *  its parent, with no history of its own; under a planning card it is a
   *  task like any other (subtasks.ts isSubtask reads the lineage). */
  parent?: string;
  lineage?: LineageRef[];
}

export interface RemovalContext {
  /** The team's current sprint, and the one before it. */
  current?: string;
  previous?: string;
  today: string;
}

/** HomesOf is what the grid × needs to know about a card: where else it is,
 *  and what a delete would take with it. */
export interface HomesOf {
  scope?: string;
  project?: string;
  progress?: number;
  title: string;
}



/** deleteWarning is the question the grid × asks before deleting a card
 *  that carries something a delete throws away: progress, or a linked review
 *  card (deleted along with it). Subtasks are not lost — they are freed into
 *  standalone cards — so they are not asked about. Null means nothing to
 *  lose: a card made a moment ago is deleted without ceremony. */
export function deleteWarning(
  c: Pick<HomesOf, "progress" | "title">,
  linkedReview: string | null,
): string | null {
  const parts: string[] = [];
  if ((c.progress ?? 0) > 0) {
    parts.push(`${c.progress}% of work on it`);
  }
  if (linkedReview) {
    parts.push(`its linked review card «${linkedReview}»`);
  }
  if (parts.length === 0) {
    return null;
  }
  return `Delete «${c.title}»? This also removes ${parts.join(" and ")}.`;
}

/** Freed is one subtask patched loose from a deleted parent. */
export interface Freed {
  itemId: string;
  patch: { parent: undefined; assignees?: string[]; parked?: true };
}

/** freeSubtasks is what deleting a parent does to its subtasks on the server
 *  (deleteWithCascade): they become standalone cards, and one nobody had
 *  takes the parent's person so it lands in the cell the parent stood in.
 *  One left with no date and no sprint — it rode a parked parent out of
 *  every day — is parked in its team's backlog (K1, keepPlaced).
 *  The optimistic client applies the same, or the subtasks — still pointing
 *  at a parent that is gone — vanish from the grid until a refresh. */
export function freeSubtasks(
  cards: readonly ({ itemId: string; parent?: string; assignees: string[] } & Partial<PlaceFields>)[],
  parentId: string,
): Freed[] {
  const parent = cards.find((c) => c.itemId === parentId);
  const inherit = parent?.assignees[0];
  return cards
    .filter((c) => c.parent === parentId)
    .map((c) => ({
      itemId: c.itemId,
      patch: {
        parent: undefined,
        ...(c.assignees.length === 0 && inherit ? { assignees: [inherit] } : {}),
        ...keepPlaced(c, false),
      },
    }));
}

/** RemovalHomes is where a card is, as the × sees it. */
export interface RemovalHomes {
  /** Who is carrying the card. Empty means it stands in Unassigned, which is
   *  where the × would otherwise offer to put it. */
  assignees?: string[];
  scope?: string;
  /** Read by nothing here on purpose: a bare project name is a label, not a
   *  home. It is in the shape so a card can be passed whole. */
  project?: string;
  sprintStart?: string;
  startDate?: string;
  day?: string;
}

/** inWorkingArea: the card is on a day grid — in a sprint, or by its dates. */
export function inWorkingArea(c: RemovalHomes): boolean {
  return !!c.sprintStart || !!c.startDate || !!c.day;
}

/** hasColumn: the card sits in a Project-board column, which the board
 *  renders by its scope — a PROJECT card, drawn there and nowhere else (H4). */
export function hasColumn(c: RemovalHomes): boolean {
  return !!c.scope;
}

/** Outcome is what an × does to a card: leave it in a home it still has,
 *  or — its last home emptied — delete it. (It used to take a subtask out
 *  of its group as well, when a subtask could carry a column of its own; a
 *  card in a column is nobody's subtask now, H4.) */
export type Outcome = "leave" | "delete";

/** gridRemoval mirrors boardservice.Remove with no intent, in its order: a
 *  subtask is deleted; a project card's column keeps it; anything else had
 *  nowhere else to be and is deleted. One function so every board asks the
 *  same question — the Me board used to answer it differently and
 *  hard-deleted what the Team board kept.
 *
 *  A task has no week to be sent back to (K7). Keeping the work is the
 *  BACKLOG answer, which the dialog offers (removeChoices) and the board
 *  sends as the unassign intent — never something this × does unasked. */
export function gridRemoval(
  c: RemovalHomes & Pick<RemovableCard, "progress" | "startDate" | "sprintStart" | "parent" | "lineage">,
  _ctx: RemovalContext,
): Outcome {
  // A subtask has no sprint history of its own: it rides its parent, and
  // demoting it alone would split the family across two sprints — which is
  // what syncChildrenSprint exists to prevent. The × deletes it, and it is
  // gone from under its parent at once. (It used to be released into a
  // column it carried; a subtask carries none now — a card in a column is
  // nobody's subtask, H4.) A task under an epic is not a subtask: the × on
  // it is the grid's ordinary ×, and the hierarchy is not where it is being
  // taken off (H1).
  if (isSubtask(c)) {
    return "delete";
  }
  return hasColumn(c) ? "leave" : "delete";
}

/** asksFirst reports that the × must put its question before it acts.
 *
 *  It acts alone in one case only: a card the reader made in the view they are
 *  looking at, that nobody has moved off 0%. Such a card is one they have the
 *  whole of in their head — they typed it, they can see it, and the × means
 *  "no, not that". A dialog there is ceremony over a line that took a second
 *  to write.
 *
 *  Everywhere else it asks. The × has three answers now, one of which destroys
 *  nothing, and choosing among them in silence is choosing for the person.
 *
 *  The old reading was "made TODAY and still at 0%", which sounds like the
 *  same thing and is not: a card typed at nine in the morning is still "today"
 *  at six in the evening, long after the reader stopped holding it, and the ×
 *  went on deleting it without a word. Reported from the live board as "the ×
 *  does not open the dialog". Whether a card was made here is justmade.ts.
 */
export function asksFirst(
  c: { progress?: number },
  madeHere = false,
): boolean {
  return !(madeHere && (c.progress ?? 0) === 0);
}

/** RemoveChoice is one of the things an × can mean, and the dialog offers the
 *  card's own list of them (removeChoices). "backlog" is a TASK's answer that
 *  destroys nothing: the work goes back to its team's backlog — parked,
 *  nobody, no day, no sprint (K3); the board sends it as the unassign
 *  intent. "unassign" is a PROJECT card's: its column keeps it.
 *
 *  The hierarchy's × has two answers of its own (roadmap.hierarchyChoices):
 *  "detach" takes the card out from under its parent and changes nothing
 *  else; "delete" destroys it — a planning card's children move up a level
 *  (roadmap.planningRemoval), as the server does (reparentChildren). */
export type RemoveChoice =
  | "off-board"
  | "unassign"
  | "ungroup"
  | "backlog"
  | "finished-earlier"
  | "detach"
  | "delete"
  | "withdraw";

/** removeChoices is what the × may do to this card WHERE IT STANDS, in the
 *  order the dialog lists them, and the × is drawn only where the list is
 *  not empty.
 *
 *  - A TASK can go OFF THE BOARD, or back to its team's BACKLOG (K3): the
 *    answer that destroys nothing, and one that is always there to go to —
 *    so it is offered whoever holds the card and whatever dates it has. It
 *    used to be "move it to Unassigned, back in its week", offered only to
 *    a card with a week; a task has no week now (K7), and the two answers
 *    that kept the work are one.
 *  - A PROJECT card can only be unassigned: it is the Project board's
 *    commitment and not this board's to destroy, and its column keeps it.
 *    Once already unassigned it has no × at all — an × that does nothing
 *    reads as a delete that failed.
 *  - A SUBTASK goes off the board: standing under its parent is not a place
 *    of its own to be kept in.
 *
 *  - A REVIEW somebody has is offered the backlog too (K10): "back to
 *    backlog" takes the reviewer off and the review waits for one again in
 *    the reviewed task's team backlog; the task stays on review.
 *
 *  The backlog is FIRST: it destroys nothing, and it is read above taking
 *  the card off the board. The × acting unasked does not take the first
 *  answer for that reason — it takes unaskedRemoval's, so a card made here
 *  at 0% is still deleted without ceremony rather than quietly parked. It
 *  is not offered for a card already in its backlog.
 *
 *  FINISHED EARLIER is the last of them, and the narrowest: work that was done
 *  in the sprint before this one and only marked done now. An engineer
 *  finishes a card and never moves the bar; Carry Over takes it for open work
 *  and pulls it into the new sprint; somebody notices and marks it 100 — and
 *  there it stands, in a sprint it was never worked in, counting against it.
 *  The board goes on showing it deliberately: that is the only prompt anybody
 *  gets that this happened, so it is not dropped quietly. This answer is how
 *  a person clears the prompt by saying where the work belongs. */
export function removeChoices(
  c: RemovalHomes &
    Pick<RemovableCard, "progress" | "startDate" | "sprintStart" | "parent" | "lineage"> & {
      parked?: boolean;
      reviewOf?: string;
    },
  ctx: RemovalContext,
): RemoveChoice[] {
  // A subtask carries no column (a card in a column is nobody's subtask,
  // H4), so the grid × has one answer for it: off the board. The pull-out
  // of the group is the drawer's own choice, not the ×'s.
  if (isSubtask(c)) {
    return ["off-board"];
  }
  // A review NOBODY HAS TAKEN — the Backlog's needs-a-reviewer item — has
  // one answer: WITHDRAW the review (K10). The card goes and the task leaves
  // review, back to In progress (boardservice.withdrawReview). Deleting the
  // card alone would leave the task on review with no review card.
  if (c.reviewOf && c.parked && (c.assignees?.length ?? 0) === 0) {
    return ["withdraw"];
  }
  // A review somebody HAS is withdrawn the same way, never taken off the
  // board by itself — that would leave its task on review with no review
  // card (K10) — or goes back to the backlog with nobody on it (K12).
  if (c.reviewOf && !hasColumn(c)) {
    return c.parked ? ["withdraw"] : ["backlog", "withdraw"];
  }
  // A REVIEW somebody has is offered it too (K10): back to the backlog takes
  // the reviewer off, and it waits for one again in the reviewed task's team
  // backlog (boardservice.returnReview).
  const shelve: RemoveChoice[] = !c.parked && !hasColumn(c) ? ["backlog"] : [];
  // Only where it would MOVE the card: finished work, standing in the current
  // sprint, with an earlier sprint to send it to. Never for work another board
  // owns — a slot carries its own dates, and the sprint is not what places it.
  const finishedEarlier: RemoveChoice[] =
    (c.progress ?? 0) >= 100 &&
    !!ctx.previous &&
    !!ctx.current &&
    c.sprintStart === ctx.current &&
    !hasColumn(c)
      ? ["finished-earlier"]
      : [];
  // A PROJECT card's answer is its column. Unassigned is where one with
  // NOBODY on it stands, and that — not its dates — is what the option would
  // change, so one already there is offered nothing.
  if (hasColumn(c)) {
    return (c.assignees?.length ?? 0) > 0 ? ["unassign"] : [];
  }
  return [...shelve, "off-board", ...finishedEarlier];
}

/** unaskedRemoval is the answer the × takes when it acts WITHOUT asking — a
 *  card made here that nobody touched (asksFirst): off the board, deleted
 *  without ceremony, as it always was. The backlog is listed first in the
 *  dialog, which is a reading order and not a default. */
export function unaskedRemoval(choices: readonly RemoveChoice[]): RemoveChoice | undefined {
  return choices.includes("off-board") ? "off-board" : choices[0];
}

/** backlogPatch is what the board shows the moment the backlog answer is
 *  given: the card parked — nobody, no day, no sprint, planned (K3,
 *  parkedLocally). A REVIEW also moves to the reviewed task's team, whose
 *  backlog it waits in for a reviewer (K10, boardservice.returnReview). */
export function backlogPatch(
  c: { reviewOf?: string; team?: string | null },
  cards: readonly { itemId: string; team?: string | null }[],
): ReturnType<typeof parkedLocally> & { team?: string } {
  const patch = parkedLocally();
  if (!c.reviewOf) {
    return patch;
  }
  const original = cards.find((x) => x.itemId === c.reviewOf);
  if (!original || (original.team ?? "") === (c.team ?? "")) {
    return patch;
  }
  return { ...patch, team: original.team ?? "" };
}

/** offersRemoval reports whether the × is drawn at all: only where it has
 *  something it could do. */
export function offersRemoval(
  c: RemovalHomes & Pick<RemovableCard, "progress" | "startDate" | "sprintStart" | "parent">,
  ctx: RemovalContext,
): boolean {
  return removeChoices(c, ctx).length > 0;
}

/** SubtaskPatch is the optimistic state the grid's × leaves on a SUBTASK:
 *  the fields the server writes, and only those. A key present with
 *  `undefined` clears it. */
export interface SubtaskPatch {
  parent: undefined;
  assignees?: string[];
  scope?: undefined;
  project?: undefined;
  sprintStart?: string;
  startDate?: string;
  day?: string;
}

/** subtaskRemovalPatch is what the × does to a subtask, in the server's own
 *  fields — one shape, so the two boards cannot show a gesture differently
 *  (which is how a row jumped to Unassigned on one of them and jumped back
 *  on the next reload).
 *
 *  RELEASED into its column: the person goes and the sprint with it
 *  (releaseToColumn → leaveWorkingArea), and the dates stay — they are the
 *  slot's row. A subtask the × DELETES is not patched at all: the board
 *  drops the row. */
export function subtaskRemovalPatch(
  _c: RemovalHomes & Pick<RemovableCard, "progress" | "startDate" | "sprintStart" | "parent">,
  _ctx: RemovalContext,
): SubtaskPatch {
  return { assignees: [], sprintStart: undefined, parent: undefined };
}

/** subtaskRemovalUndo is the state to put back when the request fails: the
 *  card's own value for every field subtaskRemovalPatch can write. One
 *  shape for the gesture and one for its inverse, or a board rolls back
 *  less than it patched — which left a card visibly ungrouped and stripped
 *  of its column while the server still held it as a subtask in it. */
export interface SubtaskUndo {
  assignees?: string[];
  parent?: string;
  scope?: string;
  project?: string;
  sprintStart?: string;
  startDate?: string;
  day?: string;
}

export function subtaskRemovalUndo(c: SubtaskUndo): SubtaskUndo {
  return {
    assignees: c.assignees,
    parent: c.parent,
    scope: c.scope,
    project: c.project,
    sprintStart: c.sprintStart,
    startDate: c.startDate,
    day: c.day,
  };
}

/** withdrawPatch is what the TASK shows the moment its review is withdrawn:
 *  back to In progress the way the stage menu's In progress puts it
 *  (board.ApplyInProgress, SetInProgress) — the stage cleared, the progress
 *  nudged into its band at the edges only. */
export function withdrawPatch(task: { stage?: string; progress?: number }): {
  stage: undefined;
  progress: number;
} {
  const cur = task.progress ?? 0;
  let progress = cur;
  if (cur < 10) {
    progress = 10;
  } else if (task.stage === "done" || cur >= 100) {
    progress = 90;
  }
  return { stage: undefined, progress };
}

/** removeIntent is the intent an × answer is sent to the remove action as,
 *  or null for an answer that is a gesture of its own (finished-earlier, the
 *  hierarchy's two). Both KEEPING answers are the server's unassign: it parks
 *  a task in its team's backlog — nobody, no day, no sprint (K3) — and leaves
 *  a project card in its column. */
export function removeIntent(choice: RemoveChoice): "unassign" | "off-board" | null {
  switch (choice) {
    case "backlog":
    case "unassign":
      return "unassign";
    case "off-board":
    case "withdraw":
      return "off-board";
    default:
      return null;
  }
}

/** reviewAssigneePatch is what a change of person shows at once, the way
 *  the server answers it (boardservice.SetAssignee): on a REVIEW card nobody
 *  named takes a taken review back to the backlog — parked, nobody, no day
 *  (K12) — and a person on a review nobody has taken takes it into work
 *  (K10). Any other change is the person alone. */
export function reviewAssigneePatch(
  c: { reviewOf?: string; assignees: string[]; parked?: boolean },
  want: string[],
): Partial<Card> {
  if (c.reviewOf && want.length === 0 && c.assignees.length > 0) {
    return parkedLocally();
  }
  if (c.reviewOf && want.length > 0 && c.parked && c.assignees.length === 0) {
    return { assignees: want, parked: false };
  }
  return { assignees: want };
}
