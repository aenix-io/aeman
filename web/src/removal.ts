/** What the × on a grid card should do. */
export type Removal = "delete" | "demote" | "ask";

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
}

/** removalKind decides what the × means for a card, mirroring
 *  boardservice.Remove.
 *
 *  A card sitting in the team's CURRENT sprint, with a previous sprint to fall
 *  back to, and not created today, has history worth keeping: the × demotes it
 *  there instead of deleting it. Anything else is deleted outright.
 *
 *  "ask" is the one case where the rule must not decide alone. A demote takes
 *  the card — and every subtask riding it — off today's board silently, which
 *  reads exactly like deletion; when the card carries progress there is work
 *  to lose either way, so the person chooses.
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

export function removalKind(c: RemovableCard, ctx: RemovalContext): Removal {
  const inCurrent = !!c.sprintStart && !!ctx.current && c.sprintStart === ctx.current;
  const demotable =
    inCurrent &&
    !!ctx.previous &&
    !!ctx.current &&
    ctx.previous < ctx.current &&
    c.startDate !== ctx.today;
  if (!demotable) {
    return "delete";
  }
  // A card in a Project-board column is never destroyed by the ×: the
  // server hands it back to that column. With no destructive option there
  // is nothing to ask. The column is the EPIC side of the (project, epic)
  // pair — the board renders columns by epic, so a bare project name puts
  // the card on no board and cannot spare it.
  if (c.epic) {
    return "demote";
  }
  return (c.progress ?? 0) > 0 ? "ask" : "demote";
}

/** HomesOf is what the grid × needs to know about a card: where else it is,
 *  and what a delete would take with it. */
export interface HomesOf {
  epic?: string;
  project?: string;
  plan?: string;
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
  plan?: string;
  sprintStart?: string;
  startDate?: string;
  day?: string;
}

/** inWorkingArea: the card is on a day grid — in a sprint, or by its dates. */
export function inWorkingArea(c: RemovalHomes): boolean {
  return !!c.sprintStart || !!c.startDate || !!c.day;
}

/** hasColumn: the card sits in a Project-board column, which the board
 *  renders by its epic. (weekly.isSlot is a different question — whether a
 *  band-less slot occupies a week's panel — and the two must not be
 *  confused.) */
export function hasColumn(c: RemovalHomes): boolean {
  return !!c.epic;
}

/** Outcome is what an × does to a card: send it back to the sprint before
 *  this one, leave it in a home it still has, or — its last home emptied —
 *  delete it. */
export type Outcome = "demote" | "leave" | "delete" | "ungroup";

/** gridRemoval mirrors boardservice.Remove(from="grid"), in its order: a
 *  card in the weekly plan goes back to it; one with sprint history demotes;
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
  // which is a home of its own (S4). Then the × takes it OUT OF THE GROUP
  // and leaves it there: releasing it while still a subtask would break
  // the person/sprint pair its parent owns, or be undone by the next
  // carry-over. The work stays planned in its column.
  if (c.parent) {
    return hasColumn(c) ? "ungroup" : "delete";
  }
  if (c.plan) {
    return "leave";
  }
  if (removalKind(c, ctx) !== "delete") {
    return "demote";
  }
  return hasColumn(c) ? "leave" : "delete";
}

/** planRemoval mirrors boardservice.Remove(from="plan"): the card leaves the
 *  band and stays wherever else it is — its column, or the working area.
 *  Being on a person or carrying progress is not a place to be. */
export function planRemoval(c: RemovalHomes): Outcome {
  return hasColumn(c) || inWorkingArea(c) ? "leave" : "delete";
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
