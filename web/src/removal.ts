import { localDateIso } from "./date";

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

/** asksFirst reports that the × must put its question before it acts. It
 *  always does — whatever the × is about to do, the person sees it named and
 *  agrees to it — except on a card there is nothing to think about: one made
 *  today that nobody has moved off 0%. A mis-typed card added a moment ago
 *  goes without ceremony; anything else is a decision, and a gesture that
 *  acts in silence is how an × comes to be feared. */
export function asksFirst(
  c: { progress?: number; createdAt?: string },
  today: string,
): boolean {
  const untouched = (c.progress ?? 0) === 0;
  const bornToday = !!c.createdAt && localDateIso(c.createdAt) === today;
  return !(untouched && bornToday);
}

/** offersRemoval reports whether the × has anything to do for this card where
 *  it stands. A PROJECT card and a PROCESS TURN are never destroyed by it —
 *  the one is handed back to its column, the other to the week its process
 *  owes — so once the card is already there, out of the working area, the ×
 *  would write nothing at all, and an × that does nothing is worse than no ×
 *  (it reads as a delete that failed). Every other card keeps its ×: its last
 *  home going IS something the × does. */
export function offersRemoval(
  c: RemovalHomes & Pick<RemovableCard, "parent"> & { task?: string },
): boolean {
  if (hasColumn(c) || c.task) {
    return inWorkingArea(c);
  }
  return true;
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
