import { todayIso } from "./date";
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

/** sprintForDate is the sprint a card joins when its dates are set to `day`,
 *  mirroring boardservice.SetDates.
 *
 *  A FUTURE day parks the card: no sprint covers it yet, and the carry-over
 *  that reaches its day adopts it. A day the team can still reach takes that
 *  day's sprint. A day OLDER than the team's reach used to make a sprint of
 *  its own, starting there — a sprint that closed, which no board draws and
 *  no carry-over ever moves, so the card left the board entirely. The dates
 *  are the person's to choose; the card stays in the sprint the team is
 *  working now. */
export function sprintForDate(
  board: Board,
  team: string | null,
  day: string,
  today = todayIso(),
): string | null {
  if (!day) {
    return null;
  }
  if (day > today) {
    return null;
  }
  return activeSprint(board, team, day) || currentSprint(board, team) || day;
}
