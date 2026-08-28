// The team roster is the server's: GET /board metadata.teams lists the teams
// the board declares (each has a teams/<id>.yaml), in the shared board order,
// and a team is declared by writing that file — which is what adding one
// does. Nothing about teams is remembered in this browser except the filter,
// a preference; a remembered roster once leaked from one board onto the next
// served from the same origin. The helpers here are pure so the rules are
// testable: what the UI offers, and how the saved filter is kept honest.

/** teamRoster is what the UI shows and offers: the board's declared teams in
 *  the server's order, then the teams added here that the board does not
 *  declare yet (their create is in flight — the Board frame over the watch
 *  brings them within a moment). Each team once; blanks skipped. */
export function teamRoster(declared: readonly string[], pending: readonly string[]): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const t of [...declared, ...pending]) {
    if (t && !seen.has(t)) {
      seen.add(t);
      out.push(t);
    }
  }
  return out;
}

/** settlePendingTeams drops from the just-added list the teams the board now
 *  declares. The same array comes back when nothing landed, so a state setter
 *  can bail out. */
export function settlePendingTeams(pending: string[], declared: readonly string[]): string[] {
  const next = pending.filter((t) => !declared.includes(t));
  return next.length === pending.length ? pending : next;
}

/** pruneTeamFilter keeps a saved filter to the teams that exist: entries no
 *  team backs are dropped ("" — the no-team group — always stays), the order
 *  of the rest is kept, and a filter left empty is null (= all) rather than a
 *  selection that would blank the board. null stays null. The same array
 *  comes back when nothing is dropped, so a state setter can bail out. */
export function pruneTeamFilter(
  filter: string[] | null,
  teams: readonly string[],
): string[] | null {
  if (filter === null) {
    return null;
  }
  const next = filter.filter((t) => t === "" || teams.includes(t));
  if (next.length === 0) {
    return null;
  }
  return next.length === filter.length ? filter : next;
}
