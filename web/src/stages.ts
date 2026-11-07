import type { Card, StageKey } from "./providers/types";

export type { StageKey };

export interface StageDef {
  key: StageKey;
  label: string;
  /** Colour the progress bar takes when the card is in this stage. */
  color: string;
}

// Colours are CSS custom properties (defined in styles.css) rather than literal
// hex, so the opt-in colour-blind mode repaints stages through one palette
// override. The stage name shown alongside the bar is the redundant channel
// that carries the meaning when hues are hard to tell apart.
export const STAGES: Record<StageKey, StageDef> = {
  locked: { key: "locked", label: "Locked", color: "var(--stage-locked)" },
  review: { key: "review", label: "Review", color: "var(--stage-review)" },
  // Recurrent marks a repeating task: unlike review/locked its progress spans
  // the full 0–100%, and Carry Over reseeds a finished one as a fresh copy.
  recurrent: { key: "recurrent", label: "Recurrent", color: "var(--stage-recurrent)" },
  // Refuse is the answer a person gives on their OWN board: "not me, not
  // this". The card is not deleted and not hidden — it goes back to the
  // team's grid wearing the darkest bar there is, for the lead to take off
  // the board or put back to work.
  refuse: { key: "refuse", label: "Refuse", color: "var(--stage-refuse)" },
  done: { key: "done", label: "Done", color: "var(--stage-done)" },
};

export const STAGE_ORDER: StageKey[] = ["locked", "review", "recurrent", "refuse", "done"];

/** ME_ONLY_STAGES are the stages a person may set only on their OWN board.
 *  Refusing is a first-person act — "I am not doing this" — and a lead
 *  marking somebody else's card refused would be putting words in their
 *  mouth; the lead's answer is the × or a stage that puts the card back to
 *  work. */
export const ME_ONLY_STAGES: StageKey[] = ["refuse"];

/** Progress bar colour for a stage; the default (no stage) bar is green. */
export const DEFAULT_BAR_COLOR = "var(--bar-default)";

/** barColor is the fill colour of a card's progress bar: the stage's own
 *  colour — locked, in review, recurrent, done — and the default green for
 *  a card that is simply being worked on. Every board that draws a bar
 *  asks this, so a card reads the same wherever it is met. */
export function barColor(stage?: StageKey): string {
  return stage ? STAGES[stage].color : DEFAULT_BAR_COLOR;
}

/** CLAMPED_STAGES hold their progress inside the 10–90 band: work that is
 *  parked on somebody else's answer — a reviewer's, whoever locked it, the
 *  lead's — is neither untouched nor finished, and a bar at either end would
 *  read as one of those two. */
const CLAMPED_STAGES: StageKey[] = ["locked", "review", "refuse"];

/** clampsProgress reports whether a stage holds the band. Asked in one place
 *  because the answer is asked in FIVE — the slider's reach, the bar it
 *  draws, the value a drag writes, the value a stage pick stores — and a
 *  stage added to some of them and not others drags past its own limit and
 *  snaps back when the server disagrees. Mirrors board.ClampProgress. */
export function clampsProgress(stage?: StageKey | null): boolean {
  return !!stage && CLAMPED_STAGES.includes(stage);
}

/** clampProgress is board.ClampProgress: a clamped stage holds [10, 90],
 *  every other stage keeps the value it was given. */
export function clampProgress(stage: StageKey | undefined | null, value: number): number {
  return clampsProgress(stage) ? Math.min(90, Math.max(10, value)) : value;
}

/** isComplete mirrors board.Complete: a card is finished when it has an explicit
 *  done stage, or is 100% with no stage, or is a recurrent card at 100%. */
/** deferredPast reports a card put off to a day later than the one given: it is
 *  off the board until that day arrives. Mirrors board.deferredPast. */
export function deferredPast(c: Partial<Card>, day: string): boolean {
  return !!c.startDate && c.startDate > day;
}

/** finishedOn reports whether a FINISHED card belongs to the day being looked at:
 *  the day it RECORDED being finished on, and no other. It guessed at first —
 *  doneAt, else the card's end date, else its start — and a card stretched three
 *  weeks ahead and closed today then stood on a day three weeks out, as though it
 *  had been finished then. A plan is not a record.
 *
 *  Nothing is lost by refusing to guess: a day already gone is served as a
 *  snapshot of the board's history, and there the day's own commits say what they
 *  finished. Mirrors board.finishedOn. */
export function finishedOn(c: Partial<Card>, day: string): boolean {
  return !!c.doneAt && c.doneAt === day;
}

/** doneAtAfter is the `doneAt` a patch leaves on a card, derived here exactly as
 *  the server writes it (P6): the board day the card reached done, cleared when it
 *  drops below. Only a patch that touches the stage or the bar can change it.
 *
 *  The optimistic copy has to carry it, because a day board draws finished work by
 *  `doneAt` and nothing else (finishedOn): a card marked done without one is
 *  complete, belongs to no day, and VANISHES from the board between the click and
 *  the server's answer. */
export function doneAtAfter(
  before: Partial<Card>,
  patch: Partial<Card>,
  today: string,
): string | undefined {
  if (patch.progress === undefined && patch.stage === undefined) {
    return before.doneAt;
  }
  const after = { ...before, ...patch };
  if (!isComplete(after)) {
    return undefined;
  }
  return after.doneAt || today;
}

export function isComplete(card: { stage?: StageKey; progress?: number }): boolean {
  if (card.stage === "done") {
    return true;
  }
  const p = card.progress ?? 0;
  return p >= 100 && (!card.stage || card.stage === "recurrent");
}

/** isWorkable mirrors board.Workable: a card can be picked up right now when it
 *  is neither finished nor parked awaiting someone else — it drops done,
 *  on-review, locked and refused cards (a refused one waits on the lead),
 *  keeping in-progress, not-started and recurrent. */
export function isWorkable(card: { stage?: StageKey; progress?: number }): boolean {
  if (isComplete(card)) {
    return false;
  }
  return (
    card.stage !== "locked" && card.stage !== "review" && card.stage !== "refuse"
  );
}

/** isInProgress reports the implicit "In Progress" status: a card with no stored
 *  stage whose progress sits in [10, 90] inclusive. It is deliberately NOT a
 *  StageKey — there is no stored option for it. */
export function isInProgress(card: {
  stage?: StageKey;
  progress?: number;
}): boolean {
  const p = card.progress ?? 0;
  return !card.stage && p >= 10 && p <= 90;
}
