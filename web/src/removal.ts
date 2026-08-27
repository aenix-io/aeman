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
  // A card filed under a Project-board column is never destroyed by the ×:
  // the server hands it back — demoted, or released to the plan — whatever
  // the person picked. With no destructive option there is nothing to ask.
  if (c.epic || c.project) {
    return "demote";
  }
  return (c.progress ?? 0) > 0 ? "ask" : "demote";
}
