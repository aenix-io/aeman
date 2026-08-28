// Roster names — teams, projects, processes — are one namespace across the
// board's repositories: a card refers to its team and project by name, so the
// same name cannot be declared twice. The server refuses a taken name (422);
// this is the same check up front, against the roster the visitor sees, so a
// form says so before anything is sent.

/** RosterKind names what is being created or renamed, for the message. */
export type RosterKind = "team" | "project" | "process" | "epic";

/** nameConflict is why `name` cannot be used for a new or renamed entry, or
 *  null when it can: another entry already carries it, compared trimmed and
 *  case-insensitively — "Portal" and "portal" would be one team to a person
 *  reading the board. `except` is the entry being renamed; its own current
 *  name is no conflict. An empty name is not a conflict either — the form
 *  simply has nothing to submit. */
export function nameConflict(
  kind: RosterKind,
  existing: readonly string[],
  name: string,
  except?: string,
): string | null {
  const wanted = name.trim().toLowerCase();
  if (!wanted) {
    return null;
  }
  const taken = existing.find(
    (e) => e !== except && e.trim().toLowerCase() === wanted,
  );
  if (taken === undefined) {
    return null;
  }
  const article = /^[aeiou]/i.test(kind) ? "An" : "A";
  return `${article} ${kind} named “${taken}” already exists`;
}
