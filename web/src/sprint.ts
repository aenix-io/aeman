import type { Board } from "./providers/types";

/** currentSprint returns a team's current sprint start from its sprint pointer
 * (null when the team has no sprint yet). team = null is the no-team group. */
export function currentSprint(board: Board, team: string | null): string | null {
  return board.sprintStates[team ?? ""]?.current ?? null;
}

/** previousSprint returns a team's previous sprint start from its sprint pointer
 * (null when the team has no prior sprint). team = null is the no-team group. */
export function previousSprint(
  board: Board,
  team: string | null,
): string | null {
  return board.sprintStates[team ?? ""]?.previous ?? null;
}

/** activeSprint returns which sprint was current for a team on a given day: the
 * team's current sprint when day is on or after it, else the previous sprint when
 * day is on or after that, else "" (only the last two sprints are tracked). The
 * Me view groups a day's cards by this. It mirrors ActiveSprint in
 * internal/board/sprint.go. team = null is the no-team group. */
export function activeSprint(
  board: Board,
  team: string | null,
  day: string,
): string {
  const cur = currentSprint(board, team);
  const prev = previousSprint(board, team);
  if (cur && day >= cur) {
    return cur;
  }
  if (prev && day >= prev) {
    return prev;
  }
  return "";
}
