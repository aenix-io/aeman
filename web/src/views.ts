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

/** createView is the board a create belongs to, read off what the add-box
 *  filled in. Every board of the app calls one provider method, so the shape
 *  IS the board: a personal card comes from the personal column, a parked one
 *  from the drawer, a week card from a Triage cell, a column card from the
 *  Project grid. Everything else is a day board's card, and the Me board's own
 *  add-box says so by naming its person (the server fills the caller in
 *  either way, so `team` is the safe reading for a card typed for somebody). */
export function createView(input: {
  personal?: boolean | null;
  parked?: boolean | null;
  week?: string | null;
  epic?: string | null;
  start?: string | null;
  day?: string | null;
}): ViewName {
  if (input.personal) {
    return "personal";
  }
  if (input.epic) {
    return "project";
  }
  if (input.parked) {
    return "backlog";
  }
  if (input.week && !input.start && !input.day) {
    return "triage";
  }
  return "team";
}
