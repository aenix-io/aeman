// Which BOARD a gesture is made from, and which board a create belongs to.
//
// Mirrors pkg/board/views.go and boardservice.createArgsFor: a card typed into
// the Me board is mine and today's, one typed into a Backlog column is parked
// on that team, last (K3). The server now takes the BOARD rather
// than the fields that used to encode it, so the client has to say which board
// each create was made on — and that is a property of the add-box, not a guess.

export type ViewName =
  | "me"
  | "team"
  | "backlog"
  | "strategy"
  | "all";

/** createView is the board a create belongs to: the one the reader is STANDING
 *  on, unless what the add-box filled in names another. A parked card comes
 *  from a Backlog column — it says its own
 *  board whatever is open behind them. Everything else is a day card, and
 *  which day board it was typed into is the thing only the open view knows:
 *  the Me board and the Team grid send the same shape, and they do not mean
 *  the same thing (the Me board adds work as unplanned and files it on the
 *  reader; the grid plans, and files into Unassigned).
 *
 *  Reading the SHAPE alone sent the Me board's own add form to the team's
 *  grid, where the server cannot tell the two apart — so the one rule that
 *  separates them held for agents and not for the board it was written on. */
export function createView(input: {
  parked?: boolean | null;
  start?: string | null;
  day?: string | null;
  type?: string | null;
  underPlanning?: boolean | null;
}, standing?: ViewName): ViewName {
  // A TYPE is the Strategy board's whole gesture: a goal, an initiative or an epic
  // is a planning card, and no other board makes one (H7). A TASK born
  // under a planning card — the "add" in an epic's own pane — is the
  // Strategy's too: it is parked in the no-team backlog until somebody takes
  // it into work (K3), and no day board would accept a create with no day
  // and a parent of that kind.
  if (input.type || input.underPlanning) {
    return "strategy";
  }
  // Typed into a Backlog column, or born parked wherever it was typed: the
  // Backlog board's create — parked on its team, last.
  if (input.parked || standing === "backlog") {
    return "backlog";
  }
  return standing === "me" ? "me" : "team";
}
