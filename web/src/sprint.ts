import type { Card } from "./providers/types";

const teamKey = (team: string | null | undefined): string => team ?? "";

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
