import type { StageKey } from "./providers/types";

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
  done: { key: "done", label: "Done", color: "var(--stage-done)" },
};

export const STAGE_ORDER: StageKey[] = ["locked", "review", "recurrent", "done"];

/** Progress bar colour for a stage; the default (no stage) bar is green. */
export const DEFAULT_BAR_COLOR = "var(--bar-default)";

/** isComplete mirrors board.Complete: a card is finished when it has an explicit
 *  done stage, or is 100% with no stage, or is a recurrent card at 100%. */
export function isComplete(card: { stage?: StageKey; progress?: number }): boolean {
  if (card.stage === "done") {
    return true;
  }
  const p = card.progress ?? 0;
  return p >= 100 && (!card.stage || card.stage === "recurrent");
}

/** isWorkable mirrors board.Workable: a card can be picked up right now when it
 *  is neither finished nor parked awaiting someone else — it drops done,
 *  on-review and locked cards, keeping in-progress, not-started and recurrent. */
export function isWorkable(card: { stage?: StageKey; progress?: number }): boolean {
  if (isComplete(card)) {
    return false;
  }
  return card.stage !== "locked" && card.stage !== "review";
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
