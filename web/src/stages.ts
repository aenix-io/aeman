import type { StageKey } from "./providers/types";

export type { StageKey };

export interface StageDef {
  key: StageKey;
  label: string;
  /** Colour the progress bar takes when the card is in this stage. */
  color: string;
}

export const STAGES: Record<StageKey, StageDef> = {
  locked: { key: "locked", label: "Locked", color: "#cf222e" },
  review: { key: "review", label: "Review", color: "#d4a72c" },
  // Recurrent marks a repeating task: unlike review/locked its progress spans
  // the full 0–100%, and Carry Over reseeds a finished one as a fresh copy.
  recurrent: { key: "recurrent", label: "Recurrent", color: "#58a6ff" },
  done: { key: "done", label: "Done", color: "#1f883d" },
};

export const STAGE_ORDER: StageKey[] = ["locked", "review", "recurrent", "done"];

/** Progress bar colour for a stage; the default (no stage) bar is green. */
export const DEFAULT_BAR_COLOR = "#3fb950";

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
