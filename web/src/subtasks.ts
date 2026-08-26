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
