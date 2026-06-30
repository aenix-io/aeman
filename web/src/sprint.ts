import type { Board, Card } from "./providers/types";

const teamKey = (team: string | null | undefined): string => team ?? "";

/** currentSprint returns a team's current sprint start from its state card
 * (null when the team has no sprint yet). team = null is the no-team group. */
export function currentSprint(board: Board, team: string | null): string | null {
  return board.sprintStates[team ?? ""]?.current ?? null;
}

/** previousSprint returns a team's previous sprint start from its state card
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

/** currentSprintByTeam maps each team key (team name, or "" for no team) to its
 * current sprint date: the latest sprintStart on or before `asOf`. */
export function currentSprintByTeam(
  cards: Card[],
  asOf: string,
): Map<string, string> {
  const m = new Map<string, string>();
  for (const c of cards) {
    if (!c.sprintStart || c.sprintStart > asOf) {
      continue;
    }
    const key = teamKey(c.team);
    const cur = m.get(key);
    if (!cur || c.sprintStart > cur) {
      m.set(key, c.sprintStart);
    }
  }
  return m;
}

/** sprintForNewCard returns the sprint a new card should join: its team's
 * current sprint (latest sprintStart ≤ asOf), or `asOf` when the team has none
 * yet. New cards join the running sprint instead of starting a fresh one. */
export function sprintForNewCard(
  cards: Card[],
  team: string | null | undefined,
  asOf: string,
): string {
  const key = teamKey(team);
  let latest = "";
  for (const c of cards) {
    if (teamKey(c.team) !== key) {
      continue;
    }
    if (c.sprintStart && c.sprintStart <= asOf && c.sprintStart > latest) {
      latest = c.sprintStart;
    }
  }
  return latest || asOf;
}
