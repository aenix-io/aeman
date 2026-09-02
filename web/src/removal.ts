/** What the × on a grid card should do — once nothing else is keeping it. */
export type Removal = "delete" | "ask";

export interface RemovableCard {
  sprintStart?: string;
  startDate?: string;
  progress?: number;
  /** A Project-board column is the PAIR (project, epic); either side filed
   *  means the card belongs to one. */
  epic?: string;
  project?: string;
  /** A subtask rides its parent: it has no history of its own. */
  parent?: string;
}

export interface RemovalContext {
  /** The team's current sprint, and the one before it. */
  current?: string;
  previous?: string;
  today: string;
  /** Whether the column a SUBTASK carries can come with it out of the
   *  group (placements.columnFollows, mirroring the server's rule of the
   *  same name). False only on a multi-repository board, where the column
   *  can belong to the parent's repository and the card cannot keep it;
   *  absent means "it can", which is the answer everywhere else. */
  columnFollows?: boolean;
}

/** removalKind decides whether a destructive × may go ahead unasked,
 *  mirroring boardservice.Remove: the card's last home is being emptied, so
 *  the card goes. It used to have a third answer — a card of the current
 *  sprint was DEMOTED into the one before instead of deleted — and that is
 *  where the board's invisible pile of open work came from: no live view
 *  reaches a card in a sprint two behind, and no carry-over ever takes it.
 *  The day the card stood on keeps it now (G60), so there is one outcome.
 *
 *  "ask" is the case the rule must not decide alone: a card carrying work
 *  takes that work off the board with it, and the person chooses.
 */
/** personalRemovalKind is the × on a personal card, which has no sprint to
 *  demote into (mirrors boardservice.removePersonal): a card that has been
 *  worked on and did not start today is left behind on yesterday's board —
 *  history kept, off the column from today — and that reads like deletion,
 *  so the person is asked; an untouched card, or one that started today, is
 *  simply deleted. */
export function personalRemovalKind(
  c: Pick<RemovableCard, "progress" | "startDate">,
  today: string,
): "ask" | "delete" {
  const worked = (c.progress ?? 0) > 0;
  const startedEarlier = !c.startDate || c.startDate < today;
  return worked && startedEarlier ? "ask" : "delete";
}

export function removalKind(c: RemovableCard, _ctx: RemovalContext): Removal {
  // A card in a Project-board column is never destroyed by the ×: the server
  // hands it back to that column. With no destructive option there is
  // nothing to ask. The column is the EPIC side of the (project, epic) pair
  // — the board renders columns by epic, so a bare project name puts the
  // card on no board and cannot spare it.
  if (c.epic) {
    return "delete";
  }
  return (c.progress ?? 0) > 0 ? "ask" : "delete";
}

/** HomesOf is what the grid × needs to know about a card: where else it is,
 *  and what a delete would take with it. */
export interface HomesOf {
  epic?: string;
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
  patch: { parent: undefined; assignees?: string[] };
}

/** freeSubtasks is what deleting a parent does to its subtasks on the server
 *  (deleteWithCascade): they become standalone cards, and one nobody had
 *  takes the parent's person so it lands in the cell the parent stood in.
 *  The optimistic client applies the same, or the subtasks — still pointing
 *  at a parent that is gone — vanish from the grid until a refresh. */
export function freeSubtasks(
  cards: readonly { itemId: string; parent?: string; assignees: string[] }[],
  parentId: string,
): Freed[] {
  const parent = cards.find((c) => c.itemId === parentId);
  const inherit = parent?.assignees[0];
  return cards
    .filter((c) => c.parent === parentId)
    .map((c) => ({
      itemId: c.itemId,
      patch:
        c.assignees.length === 0 && inherit
          ? { parent: undefined, assignees: [inherit] }
          : { parent: undefined },
    }));
}

/** RemovalHomes is where a card is, as the × sees it. */
export interface RemovalHomes {
  epic?: string;
  /** Read by nothing here on purpose: a bare project name is a label, not a
   *  home. It is in the shape so a card can be passed whole. */
  project?: string;
  /** A week is a home: the Triage board draws every card that has one. */
  week?: string;
  sprintStart?: string;
  startDate?: string;
  day?: string;
}

/** inWorkingArea: the card is on a day grid — in a sprint, or by its dates. */
export function inWorkingArea(c: RemovalHomes): boolean {
  return !!c.sprintStart || !!c.startDate || !!c.day;
}

/** hasColumn: the card sits in a Project-board column, which the board
 *  renders by its epic. (slots.isSlot is a different question — whether the
 *  card has taken a ROW on that board — and the two must not be confused.) */
export function hasColumn(c: RemovalHomes): boolean {
  return !!c.epic;
}

/** Outcome is what an × does to a card: leave it in a home it still has,
 *  take it out of its group, or — its last home emptied — delete it. */
export type Outcome = "leave" | "delete" | "ungroup";

/** looseOf is the card as the × leaves it when the column it carried
 *  cannot follow it out of the group: an ordinary, columnless card, which
 *  is what the server then answers for (boardservice removeFromGrid). */
function looseOf<T extends RemovalHomes>(c: T): T {
  return { ...c, epic: undefined, project: undefined };
}

/** gridRemoval mirrors boardservice.Remove(from="grid"), in its order: a
 *  card scheduled for a WEEK goes back to that week on the Triage board;
 *  otherwise its column keeps it, or nothing does and it is deleted. One
 *  function so every board asks the same question — the Me board used to
 *  answer it differently and hard-deleted what the Team board kept. */
export function gridRemoval(
  c: RemovalHomes & Pick<RemovableCard, "progress" | "startDate" | "sprintStart" | "parent">,
  ctx: RemovalContext,
): Outcome {
  // A subtask has no sprint history of its own: it rides its parent, and
  // demoting it alone would split the family across two sprints — which is
  // what syncChildrenSprint exists to prevent. The × deletes it, and it is
  // gone from under its parent at once — unless it stands in a COLUMN,
  // which is a home of its own (G57). Then the × takes it OUT OF THE GROUP
  // and leaves it there: releasing it while still a subtask would break
  // the person/sprint pair its parent owns, or be undone by the next
  // carry-over. The work stays planned in its column.
  if (c.parent) {
    if (!hasColumn(c)) {
      return "delete";
    }
    if (ctx.columnFollows === false) {
      // The column belongs to the repository the card is LEAVING, so the
      // pull-out cannot take it along: the server repairs it away and the
      // card is answered like any other columnless one, which is deletion.
      // Reading it as an ungroup here made a board patch the card as kept
      // and let a destructive × through with no question.
      return "delete";
    }
    return "ungroup";
  }
  // A card with a WEEK still has the Triage board to be on: taking it off
  // the day grid puts it back in that week rather than destroying it.
  if (c.week) {
    return "leave";
  }
  return hasColumn(c) ? "leave" : "delete";
}

/** boardAsksAbout reports that the card's own anonymous "Delete?" prompt
 *  must stand down: because the board opens its two-way choice (W5), because
 *  it will name what the delete takes (deleteWarning), or because the × does
 *  not delete at all — a prompt reading "Delete?" in front of something that
 *  KEEPS the card is how the × came to be read as deletion. The card asks
 *  only for a plain delete with nothing to name. */
export function boardAsksAbout(
  c: Pick<HomesOf, "progress" | "title">,
  outcome: Outcome | "ask",
  linkedReview: string | null,
): boolean {
  if (outcome !== "delete") {
    return true;
  }
  return deleteWarning(c, linkedReview) !== null;
}

/** SubtaskPatch is the optimistic state the grid's × leaves on a SUBTASK:
 *  the fields the server writes, and only those. A key present with
 *  `undefined` clears it. */
export interface SubtaskPatch {
  parent: undefined;
  assignees?: string[];
  epic?: undefined;
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
  epic?: string;
  project?: string;
  sprintStart?: string;
  startDate?: string;
  day?: string;
}

export function subtaskRemovalUndo(c: SubtaskUndo): SubtaskUndo {
  return {
    assignees: c.assignees,
    parent: c.parent,
    epic: c.epic,
    project: c.project,
    sprintStart: c.sprintStart,
    startDate: c.startDate,
    day: c.day,
  };
}

/** GridGesture is what the day grid's × actually DOES to a card — the
 *  routing the boards used to each work out for themselves, which is how
 *  they came to disagree about the same card. "release" is the smart × on
 *  the server (POST actions/remove): the card leaves the working area for
 *  whatever home it has, and a subtask standing in a column leaves the
 *  GROUP with it (G57). "delete" is the last home going — the card, and the
 *  subtasks that were pieces of it — and "ask" is the confirmation that
 *  stands in front of it when there is work to lose. */
export type GridGesture = "ask" | "release" | "delete";

/** gridGesture routes one × press: a card with another home is RELEASED
 *  into it, one with none is DELETED, and a delete that would take work
 *  with it is ASKED about first. A columned subtask is released whatever
 *  it carries — the server hands it to its column. */
export function gridGesture(
  c: RemovalHomes &
    Pick<RemovableCard, "progress" | "startDate" | "sprintStart" | "parent">,
  ctx: RemovalContext,
): GridGesture {
  if (gridRemoval(c, ctx) !== "delete") {
    return "release";
  }
  // The card's last home is going. A subtask reaches this only with no
  // column of its own (or one that cannot follow it out), and its work
  // deserves the same question every other card's does — a subtask
  // exempted from it was the one card on the grid whose work left in
  // silence.
  return removalKind(looseOf(c), ctx) === "ask" ? "ask" : "delete";
}
