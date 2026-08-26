/** What the × on a grid card should do. */
export type Removal = "delete" | "demote" | "ask";

export interface RemovableCard {
  sprintStart?: string;
  startDate?: string;
  progress?: number;
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
  return (c.progress ?? 0) > 0 ? "ask" : "demote";
}
