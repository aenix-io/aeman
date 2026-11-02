import type { Card } from "./providers/types";

/** isPlanning reports a goal, an initiative or an epic — a card the Roadmap
 *  draws and no day board does. Mirrors board.IsPlanning. */
export function isPlanning(c: Pick<Card, "type">): boolean {
  return !!c.type;
}

/** isSubtask reports a task whose parent is a TASK — the one kind of card
 *  that rides another: hidden from the boards, drawn under its parent. A
 *  task under an epic has a parent too and is NOT one (hierarchy.md, H1).
 *
 *  The parent's kind is read off the card's lineage, whose last entry is the
 *  parent, because the day boards never deliver a planning card to look it
 *  up by. No lineage at all — a card made optimistically a moment ago, or a
 *  parent that is not on the board — reads as a subtask, which is what every
 *  card with a parent read as before, and what the server reads a dangling
 *  parent as (board.IsSubtask). */
export function isSubtask(c: Pick<Card, "parent" | "lineage">): boolean {
  if (!c.parent) {
    return false;
  }
  const up = c.lineage;
  if (!up || up.length === 0) {
    return true;
  }
  const parent = up[up.length - 1];
  return parent.uid !== c.parent || !parent.type;
}

/** The slice of a card the subtask-visibility rule reads. */
export interface SubtaskLike {
  team?: string;
  parent?: string;
  startDate?: string;
  sprintStart?: string;
  progress?: number;
}

export interface DayContext {
  today: string;
  day: string;
}

/** subtaskShows decides whether a subtask row belongs under its parent on the
 *  viewed day.
 *
 *  A subtask deferred to a later day stays hidden until that day — scheduling
 *  work ahead is exactly the act of taking it off today's board.
 *
 *  A subtask whose sprint had not STARTED by the viewed day is not there yet
 *  either: rolling the board back must not show work that had not begun.
 *
 *  But a subtask left BEHIND in an earlier sprint does show. It stays in the
 *  sprint it was finished in by design, while its parent carries on — and the
 *  parent's progress bar is derived from exactly these children. Hiding them
 *  left an inherited card reading 90% with no expand arrow and no way to learn
 *  why: the log named subtasks their new owner could not open.
 */
export function subtaskShows(c: SubtaskLike, ctx: DayContext): boolean {
  if (c.startDate && c.startDate > ctx.today && ctx.day < c.startDate) {
    return false;
  }
  if (c.sprintStart && ctx.day < c.sprintStart) {
    return false;
  }
  return true;
}
