// A task's WORK STATUS, as the Roadmap's Status filter reads it. Mirrors
// pkg/board board.WorkStatus: the server applies the filter (the `status`
// selector of view=roadmap) and this copy is what the client says about a
// card, so the two must agree case by case (workstatus.test.ts).

import type { Card } from "./providers/types";
import { isComplete } from "./stages";

export type WorkStatus = "in-progress" | "backlog" | "done";

/** WORK_STATUSES in the order the filter offers them. */
export const WORK_STATUSES: readonly WorkStatus[] = ["in-progress", "backlog", "done"];

/** STATUS_LABELS are what the Status filter calls each one. */
export const STATUS_LABELS: Record<WorkStatus, string> = {
  "in-progress": "In progress",
  backlog: "Backlog",
  done: "Done",
};

/** workStatus mirrors board.WorkStatus: done when complete; else backlog
 *  when parked or placed nowhere (no date, no sprint); else in progress —
 *  in hand by a date or a sprint, review, locked and refused included, since
 *  somebody holds them. */
export function workStatus(
  c: Partial<Pick<Card, "stage" | "progress" | "parked" | "startDate" | "day" | "sprintStart">>,
): WorkStatus {
  if (isComplete({ stage: c.stage, progress: c.progress })) {
    return "done";
  }
  if (c.parked || !(c.startDate || c.day || c.sprintStart)) {
    return "backlog";
  }
  return "in-progress";
}

/** HIDE_DONE is the one-click "Hide done": every status but done. */
export const HIDE_DONE: string[] = ["backlog", "in-progress"];

/** statusSet writes a selection as the selector's set: known values,
 *  sorted, comma-joined. */
export function statusSet(selected: readonly string[]): string {
  return selected
    .filter((s): s is WorkStatus => (WORK_STATUSES as readonly string[]).includes(s))
    .sort()
    .join(",");
}
