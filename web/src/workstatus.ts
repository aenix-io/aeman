// A task's WORK STATUS, and the rest of what the Roadmap's Status filter
// reads (locked, no-items). Mirrors pkg/board board.WorkStatus,
// TaskStatuses and PlanStatuses. The SERVER applies the filter (the `status`
// selector of view=roadmap); this is the TS copy of its rule, kept in step
// with the Go one case by case by workstatus.test.ts over the shared
// pkg/board/testdata/work_status.json. workStatus also places a row's badge
// (roadmap.statusOf); taskStatuses and planStatuses draw nothing — they are
// the rule written down where the client's tests can hold it to the
// server's.

import type { Card } from "./providers/types";
import { isComplete } from "./stages";

export type WorkStatus = "in-progress" | "backlog" | "done";

/** WORK_STATUSES are the work statuses, the placement-based three. */
export const WORK_STATUSES: readonly WorkStatus[] = ["in-progress", "backlog", "done"];

/** StatusFilter is a value of the Status filter: a work status, locked (a
 *  dependency holds the card — status.locked, not the stage) or no-items (a
 *  plan item with nothing under it). */
export type StatusFilter = WorkStatus | "locked" | "no-items";

/** STATUS_FILTERS in the order the filter offers them (board.StatusFilters). */
export const STATUS_FILTERS: readonly StatusFilter[] = ["in-progress", "backlog", "locked", "no-items", "done"];

/** STATUS_LABELS are what the Status filter calls each one. */
export const STATUS_LABELS: Record<StatusFilter, string> = {
  "in-progress": "In progress",
  backlog: "Backlog",
  locked: "Locked",
  "no-items": "No items",
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

/** taskStatuses is the TS copy of board.TaskStatuses (the server filters;
 *  the shared fixture keeps the two in step): the values a task answers —
 *  its work status, and locked while a dependency holds it and it is not
 *  done (done wins, so Hide done hides exactly the finished work). */
export function taskStatuses(c: Parameters<typeof workStatus>[0], locked: boolean): StatusFilter[] {
  const ws = workStatus(c);
  return locked && ws !== "done" ? [ws, "locked"] : [ws];
}

/** planStatuses is the TS copy of board.PlanStatuses (the server filters;
 *  the shared fixture keeps the two in step): what a plan item answers BY
 *  ITSELF from its roll-up and its lock — no-items when nothing is under it,
 *  locked while held and not done. The rest it answers by its tasks. */
export function planStatuses(rollUp: "no-items" | "backlog" | "in-progress" | "done", locked: boolean): StatusFilter[] {
  const out: StatusFilter[] = [];
  if (locked && rollUp !== "done") {
    out.push("locked");
  }
  if (rollUp === "no-items") {
    out.push("no-items");
  }
  return out;
}

/** isStatusFilter reports one of the filter's values. */
export function isStatusFilter(s: string): s is StatusFilter {
  return (STATUS_FILTERS as readonly string[]).includes(s);
}

/** HIDE_DONE is the one-click "Hide done": every value but done — so a plan
 *  item with no tasks and a card waiting on a dependency stay. */
export const HIDE_DONE: StatusFilter[] = STATUS_FILTERS.filter((s) => s !== "done");

/** statusSet writes a selection as the selector's set: known values,
 *  sorted, comma-joined. */
export function statusSet(selected: readonly string[]): string {
  return selected.filter(isStatusFilter).sort().join(",");
}
