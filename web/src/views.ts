// Which BOARD a gesture is made from, and which board a create belongs to.
//
// Mirrors pkg/board/views.go and boardservice.createArgsFor: a card typed into
// the Me board is mine and today's, one typed into a Triage week is scheduled
// for that week and stands on no day, one typed into the drawer is parked, one
// typed into a Project column is a slot. The server now takes the BOARD rather
// than the fields that used to encode it, so the client has to say which board
// each create was made on — and that is a property of the add-box, not a guess.

export type ViewName =
  | "me"
  | "team"
  | "triage"
  | "backlog"
  | "project"
  | "personal"
  | "all";

/** createView is the board a create belongs to: the one the reader is STANDING
 *  on, unless what the add-box filled in names another. A personal card comes
 *  from the personal column, a parked one from the drawer, a week card from a
 *  Triage cell, a column card from the Project grid — those say their own
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
  personal?: boolean | null;
  parked?: boolean | null;
  week?: string | null;
  epic?: string | null;
  start?: string | null;
  day?: string | null;
}, standing?: ViewName): ViewName {
  if (input.personal) {
    return "personal";
  }
  if (input.epic) {
    return "project";
  }
  if (input.parked) {
    return "backlog";
  }
  // A WEEK is the Triage board's whole gesture, and it says so even when the
  // card carries days too: a card started in the row that IS NOW belongs to
  // today as well (TriageBoard's add form), and it is still a card of that
  // board. Reading "week and no dates" as the test sent that create to the
  // team's grid, which refuses a week.
  if (input.week) {
    return "triage";
  }
  return standing === "me" ? "me" : "team";
}
